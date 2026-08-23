package validation

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/kaiau00/aux-cli/internal/permission"
)

// defaultCommandTimeout bounds a single validation command. A hung test suite
// must not wedge the task that triggered it.
const defaultCommandTimeout = 10 * time.Minute

// Approver asks the user to approve running a validation command. It is the
// same shape as permission.Service.Request, narrowed to what this package
// needs so validation does not depend on the whole permission surface.
type Approver interface {
	Request(opts permission.CreatePermissionRequest) bool
}

// ShellRunner executes validation commands in a working directory, after the
// user approves each distinct command.
//
// Approval is not optional. Validation commands come from the project's
// compiled profile, which is derived by scanning repo content (package.json
// scripts, Makefiles, and so on) — so running one without asking would let a
// checked-out repository execute arbitrary code just by being opened. The
// approval carries the command as its permission Fingerprint, so approving
// "go test ./..." for the session never authorizes a different command.
type ShellRunner struct {
	WorkDir   string
	SessionID string
	Approver  Approver
	// Timeout bounds one command; zero means defaultCommandTimeout.
	Timeout time.Duration
}

// Run implements Runner.
func (r ShellRunner) Run(ctx context.Context, command string) (CommandResult, error) {
	if command == "" {
		return CommandResult{}, fmt.Errorf("empty validation command")
	}
	if r.Approver == nil || r.SessionID == "" {
		return CommandResult{}, fmt.Errorf("%w: running validation command %q requires approval",
			permission.ErrorPermissionDenied, command)
	}
	granted := r.Approver.Request(permission.CreatePermissionRequest{
		SessionID:   r.SessionID,
		ToolName:    "validation",
		Action:      "execute",
		Path:        r.WorkDir,
		Description: fmt.Sprintf("Run validation command: %s", command),
		Fingerprint: command,
	})
	if !granted {
		return CommandResult{}, permission.ErrorPermissionDenied
	}

	timeout := r.Timeout
	if timeout <= 0 {
		timeout = defaultCommandTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	cmd := exec.CommandContext(runCtx, "sh", "-c", command)
	cmd.Dir = r.WorkDir
	out, err := cmd.CombinedOutput()
	result := CommandResult{DurationMS: time.Since(start).Milliseconds()}

	if err != nil {
		var exitErr *exec.ExitError
		if ok := asExitError(err, &exitErr); ok {
			// A non-zero exit is a validation failure, not a runner error: the
			// caller records it as failing evidence rather than an outage.
			result.ExitCode = exitErr.ExitCode()
			return result, nil
		}
		return result, fmt.Errorf("failed to run %q: %w (output: %s)", command, err, truncate(string(out), 2000))
	}
	return result, nil
}

func asExitError(err error, target **exec.ExitError) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		*target = ee
		return true
	}
	return false
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

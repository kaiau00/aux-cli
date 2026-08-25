package evalsuite

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Executor runs a shell command in a directory and reports whether it
// succeeded. It is injected so the runner's control flow — reset, setup, agent,
// success checks, metric capture — can be tested without a provider, a network,
// or a real repository.
type Executor interface {
	Run(ctx context.Context, dir string, command string) (stdout string, err error)
}

// MetricsReader supplies what a task run cost, read from the durable ledger
// after the agent finishes rather than parsed out of its output.
type MetricsReader interface {
	TaskMetrics(ctx context.Context, sessionID string) (inputTokens, outputTokens, turns int64, cost float64, costUnknown bool, err error)
}

// ShellExecutor runs commands with the system shell.
type ShellExecutor struct {
	// Timeout bounds a single command. A benchmark that hangs on one task is
	// worse than one that fails it, because nobody watches a suite run.
	Timeout time.Duration
}

func (e ShellExecutor) Run(ctx context.Context, dir, command string) (string, error) {
	timeout := e.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(out), fmt.Errorf("command timed out after %s: %s", timeout, command)
	}
	return string(out), err
}

// Runner executes a suite.
type Runner struct {
	Exec Executor
	// Harness is the agent CLI under measurement.
	Harness Harness

	// Isolate, when set, resets each repository to the task's base revision
	// before running. Defaults on: without it, task N runs against the mess
	// task N-1 left, and the suite measures order rather than capability.
	//
	// It runs `git reset --hard` and `git clean -fd`, which is destructive, so
	// suite repositories must be scratch checkouts.
	Isolate bool
}

// NewRunner returns a runner with isolation enabled.
func NewRunner(exec Executor, h Harness) *Runner {
	return &Runner{Exec: exec, Harness: h, Isolate: true}
}

// RunSuite executes every task and returns the measured result.
//
// A task that errors is recorded as failed rather than aborting the suite: a
// partial result with a named failure is more useful than no result, and one
// broken task should not cost the whole run.
func (r *Runner) RunSuite(ctx context.Context, s Suite, label string) (SuiteRun, error) {
	if err := s.Validate(); err != nil {
		return SuiteRun{}, err
	}
	if r.Exec == nil || r.Harness == nil {
		return SuiteRun{}, fmt.Errorf("runner needs both an executor and a harness")
	}

	name := ""
	if r.Harness != nil {
		name = r.Harness.Name()
	}
	out := SuiteRun{Suite: s.Name, Label: label, Harness: name, RanAt: time.Now()}
	for _, t := range s.Tasks {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		out.Runs = append(out.Runs, r.runTask(ctx, t))
	}
	return out, nil
}

func (r *Runner) runTask(ctx context.Context, t Task) Run {
	started := time.Now()
	run := Run{TaskID: t.ID}
	finish := func(reason string) Run {
		run.FailureReason = reason
		run.DurationMS = time.Since(started).Milliseconds()
		return run
	}

	if r.Isolate {
		for _, cmd := range []string{
			"git reset --hard " + t.BaseRevision,
			"git clean -fd",
			// aux self-protects its data directory with a nested .gitignore
			// (`*`), so `git clean -fd` treats it as ignored and leaves it
			// standing. Without this, task N inherits task N-1's session
			// database -- the exact contamination isolation exists to prevent,
			// just one layer down from the code under test.
			"rm -rf .aux",
		} {
			if out, err := r.Exec.Run(ctx, t.Repo, cmd); err != nil {
				return finish(fmt.Sprintf("isolation failed (%s): %v: %s", cmd, err, out))
			}
		}
	}

	for _, cmd := range t.Setup {
		if out, err := r.Exec.Run(ctx, t.Repo, cmd); err != nil {
			return finish(fmt.Sprintf("setup failed (%s): %v: %s", cmd, err, out))
		}
	}

	// The agent's own exit status is not the measurement — a task can exit
	// cleanly having done nothing, or exit non-zero having done the work. Only
	// the success commands decide. A hard failure is still recorded, because it
	// explains an otherwise mysterious red.
	//
	agentOut, agentErr := r.Exec.Run(ctx, t.Repo, r.Harness.Command(t.Prompt))

	if usage, err := r.Harness.Metrics(ctx, agentOut); err == nil {
		run.InputTokens, run.OutputTokens, run.Turns = usage.InputTokens, usage.OutputTokens, usage.Turns
		run.Cost, run.CostUnknown = usage.Cost, usage.CostUnknown
	} else {
		// Unmeasured cost must be visible, not silently zero: a run with no
		// metrics would otherwise look like the cheapest in the suite.
		run.CostUnknown = true
	}

	for _, cmd := range t.Success {
		out, err := r.Exec.Run(ctx, t.Repo, cmd)
		if err != nil {
			reason := fmt.Sprintf("success command failed (%s): %v: %s", cmd, err, truncate(out, 2000))
			if agentErr != nil {
				reason += fmt.Sprintf(" [agent also exited with error: %v]", agentErr)
			}
			return finish(reason)
		}
	}

	run.Succeeded = true
	run.DurationMS = time.Since(started).Milliseconds()
	return run
}

// shellQuote wraps a string in single quotes for `sh -c`. Prompts are arbitrary
// text from a suite file and routinely contain quotes and newlines.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// sessionIDFrom pulls the session id out of `aux -p --output-format json`.
//
// The agent's stdout may carry other lines ahead of the JSON object, so this
// scans for the last well-formed object carrying a session id rather than
// assuming the whole of stdout parses.
func sessionIDFrom(out string) (string, error) {
	start := strings.LastIndex(out, "{")
	for start >= 0 {
		var payload struct {
			SessionID string `json:"sessionId"`
		}
		if err := json.Unmarshal([]byte(out[start:]), &payload); err == nil && payload.SessionID != "" {
			return payload.SessionID, nil
		}
		start = strings.LastIndex(out[:start], "{")
	}
	return "", fmt.Errorf("no session id in agent output")
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "... [truncated]"
}

// RunSeries executes the suite repeat times under one configuration.
//
// Repetition is not optional rigour here; it is the only way any number from
// this suite means anything. See Series.
func (r *Runner) RunSeries(ctx context.Context, s Suite, label string, repeat int) (Series, error) {
	if repeat < 1 {
		repeat = 1
	}
	name := ""
	if r.Harness != nil {
		name = r.Harness.Name()
	}
	series := Series{Label: label, Harness: name}
	for i := 0; i < repeat; i++ {
		run, err := r.RunSuite(ctx, s, fmt.Sprintf("%s#%d", label, i+1))
		if err != nil {
			return series, err
		}
		series.Runs = append(series.Runs, run)
	}
	return series, nil
}

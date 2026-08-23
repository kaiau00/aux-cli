package shell

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/kaiau00/aux-cli/internal/config"
)

type PersistentShell struct {
	cmd          *exec.Cmd
	stdin        *os.File
	isAlive      bool
	cwd          string
	mu           sync.Mutex
	commandQueue chan *commandExecution
}

type commandExecution struct {
	command    string
	timeout    time.Duration
	resultChan chan commandResult
	ctx        context.Context
}

type commandResult struct {
	stdout      string
	stderr      string
	exitCode    int
	interrupted bool
	err         error
}

var (
	shellInstance     *PersistentShell
	shellInstanceOnce sync.Once
)

func GetPersistentShell(workingDir string) *PersistentShell {
	shellInstanceOnce.Do(func() {
		shellInstance = newPersistentShell(workingDir)
	})

	if shellInstance == nil {
		shellInstance = newPersistentShell(workingDir)
	} else if !shellInstance.isAlive {
		shellInstance = newPersistentShell(shellInstance.cwd)
	}

	return shellInstance
}

func newPersistentShell(cwd string) *PersistentShell {
	// Get shell configuration from config
	cfg := config.Get()

	// Default to environment variable if config is not set or nil
	var shellPath string
	var shellArgs []string

	if cfg != nil {
		shellPath = cfg.Shell.Path
		shellArgs = cfg.Shell.Args
	}

	if shellPath == "" {
		shellPath = os.Getenv("SHELL")
		if shellPath == "" {
			shellPath = "/bin/bash"
		}
	}

	// Default shell args
	if len(shellArgs) == 0 {
		shellArgs = []string{"-l"}
	}

	cmd := exec.Command(shellPath, shellArgs...)
	cmd.Dir = cwd

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil
	}

	cmd.Env = append(os.Environ(), "GIT_EDITOR=true")

	err = cmd.Start()
	if err != nil {
		return nil
	}

	shell := &PersistentShell{
		cmd:          cmd,
		stdin:        stdinPipe.(*os.File),
		isAlive:      true,
		cwd:          cwd,
		commandQueue: make(chan *commandExecution, 10),
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintf(os.Stderr, "Panic in shell command processor: %v\n", r)
				shell.isAlive = false
				close(shell.commandQueue)
			}
		}()
		shell.processCommands()
	}()

	go func() {
		err := cmd.Wait()
		if err != nil {
			// Log the error if needed
		}
		shell.isAlive = false
		close(shell.commandQueue)
	}()

	return shell
}

func (s *PersistentShell) processCommands() {
	for cmd := range s.commandQueue {
		result := s.execCommand(cmd.command, cmd.timeout, cmd.ctx)
		cmd.resultChan <- result
	}
}

func (s *PersistentShell) execCommand(command string, timeout time.Duration, ctx context.Context) commandResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isAlive {
		return commandResult{
			stderr:   "Shell is not alive",
			exitCode: 1,
			err:      errors.New("shell is not alive"),
		}
	}

	tempDir := os.TempDir()
	stdoutFile := filepath.Join(tempDir, fmt.Sprintf("aux-stdout-%d", time.Now().UnixNano()))
	stderrFile := filepath.Join(tempDir, fmt.Sprintf("aux-stderr-%d", time.Now().UnixNano()))
	statusFile := filepath.Join(tempDir, fmt.Sprintf("aux-status-%d", time.Now().UnixNano()))
	cwdFile := filepath.Join(tempDir, fmt.Sprintf("aux-cwd-%d", time.Now().UnixNano()))

	defer func() {
		os.Remove(stdoutFile)
		os.Remove(stderrFile)
		os.Remove(statusFile)
		os.Remove(cwdFile)
	}()

	fullCommand := fmt.Sprintf(`
eval %s < /dev/null > %s 2> %s
EXEC_EXIT_CODE=$?
pwd > %s
echo $EXEC_EXIT_CODE > %s
`,
		shellQuote(command),
		shellQuote(stdoutFile),
		shellQuote(stderrFile),
		shellQuote(cwdFile),
		shellQuote(statusFile),
	)

	_, err := s.stdin.Write([]byte(fullCommand + "\n"))
	if err != nil {
		return commandResult{
			stderr:   fmt.Sprintf("Failed to write command to shell: %v", err),
			exitCode: 1,
			err:      err,
		}
	}

	interrupted := false

	startTime := time.Now()

	done := make(chan bool)
	go func() {
		for {
			select {
			case <-ctx.Done():
				s.killChildren()
				interrupted = true
				done <- true
				return

			case <-time.After(10 * time.Millisecond):
				if fileExists(statusFile) && fileSize(statusFile) > 0 {
					done <- true
					return
				}

				if timeout > 0 {
					elapsed := time.Since(startTime)
					if elapsed > timeout {
						s.killChildren()
						interrupted = true
						done <- true
						return
					}
				}
			}
		}
	}()

	<-done

	stdout := readFileOrEmpty(stdoutFile)
	stderr := readFileOrEmpty(stderrFile)
	exitCodeStr := readFileOrEmpty(statusFile)
	newCwd := readFileOrEmpty(cwdFile)

	exitCode := 0
	if exitCodeStr != "" {
		fmt.Sscanf(exitCodeStr, "%d", &exitCode)
	} else if interrupted {
		exitCode = 143
		stderr += "\nCommand execution timed out or was interrupted"
	}

	if newCwd != "" {
		s.cwd = strings.TrimSpace(newCwd)
	}

	return commandResult{
		stdout:      stdout,
		stderr:      stderr,
		exitCode:    exitCode,
		interrupted: interrupted,
	}
}

func (s *PersistentShell) killChildren() {
	if s.cmd == nil || s.cmd.Process == nil {
		return
	}

	pgrepCmd := exec.Command("pgrep", "-P", fmt.Sprintf("%d", s.cmd.Process.Pid))
	output, err := pgrepCmd.Output()
	if err != nil {
		return
	}

	for pidStr := range strings.SplitSeq(string(output), "\n") {
		if pidStr = strings.TrimSpace(pidStr); pidStr != "" {
			var pid int
			fmt.Sscanf(pidStr, "%d", &pid)
			if pid > 0 {
				proc, err := os.FindProcess(pid)
				if err == nil {
					proc.Signal(syscall.SIGTERM)
				}
			}
		}
	}
}

func (s *PersistentShell) Exec(ctx context.Context, command string, timeoutMs int) (string, string, int, bool, error) {
	if !s.isAlive {
		return "", "Shell is not alive", 1, false, errors.New("shell is not alive")
	}

	timeout := time.Duration(timeoutMs) * time.Millisecond

	resultChan := make(chan commandResult)
	s.commandQueue <- &commandExecution{
		command:    command,
		timeout:    timeout,
		resultChan: resultChan,
		ctx:        ctx,
	}

	result := <-resultChan
	return result.stdout, result.stderr, result.exitCode, result.interrupted, result.err
}

func (s *PersistentShell) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isAlive {
		return
	}

	s.stdin.Write([]byte("exit\n"))

	s.cmd.Process.Kill()
	s.isAlive = false
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// MaxCaptureBytes bounds how much of a command's stdout/stderr is read into
// memory.
//
// Commands write their output to a temp file, which is then read back whole.
// Unbounded, `cat huge.log` or a runaway loop pulls the entire file into the
// agent's address space before any downstream truncation gets a chance to
// discard it — the process can die on output it was always going to throw away.
//
// The cap is far above the ~30KB the tool layer ultimately sends to the model,
// so it changes nothing for realistic output and only engages where the old
// behaviour was a liability.
const MaxCaptureBytes = 256 * 1024

// readFileOrEmpty reads a file, keeping at most MaxCaptureBytes: the head and
// tail of oversized output, with a marker naming what was dropped. Head and tail
// are kept because the useful parts of command output live at the ends — the
// command echo and early errors at the start, the summary and exit context at
// the end.
func readFileOrEmpty(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return ""
	}

	size := info.Size()
	if size <= MaxCaptureBytes {
		content, err := io.ReadAll(f)
		if err != nil {
			return ""
		}
		return string(content)
	}

	half := MaxCaptureBytes / 2
	head := make([]byte, half)
	if _, err := io.ReadFull(f, head); err != nil {
		return ""
	}
	tail := make([]byte, half)
	if _, err := f.ReadAt(tail, size-int64(half)); err != nil {
		return ""
	}

	dropped := size - int64(MaxCaptureBytes)
	return fmt.Sprintf(
		"%s\n\n... [%d bytes of output dropped; the command produced %d bytes total] ...\n\n%s",
		trimPartialRuneSuffix(head), dropped, size, trimPartialRunePrefix(tail),
	)
}

// trimPartialRuneSuffix drops a multi-byte rune left incomplete by cutting at a
// fixed byte offset, so the result is always valid UTF-8.
func trimPartialRuneSuffix(b []byte) string {
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	return string(b)
}

// trimPartialRunePrefix is trimPartialRuneSuffix for a cut at the front.
func trimPartialRunePrefix(b []byte) string {
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[1:]
	}
	return string(b)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

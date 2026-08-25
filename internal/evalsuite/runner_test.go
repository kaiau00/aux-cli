package evalsuite

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeExec records commands and fails the ones matching a substring, so the
// runner's control flow can be exercised without a provider or a repository.
type fakeExec struct {
	commands  []string
	failOn    string
	agentOut  string
	failAgent bool
}

func (f *fakeExec) Run(_ context.Context, _ string, command string) (string, error) {
	f.commands = append(f.commands, command)

	if strings.Contains(command, " -p ") {
		out := f.agentOut
		if out == "" {
			out = `{"response":"done","sessionId":"sess-1"}`
		}
		if f.failAgent {
			return out, errors.New("agent exited non-zero")
		}
		return out, nil
	}
	if f.failOn != "" && strings.Contains(command, f.failOn) {
		return "boom", errors.New("command failed")
	}
	return "ok", nil
}

func (f *fakeExec) ran(substr string) bool {
	for _, c := range f.commands {
		if strings.Contains(c, substr) {
			return true
		}
	}
	return false
}

type fakeMetrics struct {
	in, out, turns int64
	cost           float64
	err            error
	sawSessionID   string
}

func (m *fakeMetrics) TaskMetrics(_ context.Context, sessionID string) (int64, int64, int64, float64, bool, error) {
	m.sawSessionID = sessionID
	if m.err != nil {
		return 0, 0, 0, 0, false, m.err
	}
	return m.in, m.out, m.turns, m.cost, false, nil
}

// auxRunner builds a runner wired to the Aux harness, which is what most of
// these tests exercise.
func auxRunner(exec Executor, metrics MetricsReader) *Runner {
	return NewRunner(exec, AuxHarness{Binary: "aux", Metrics_: metrics})
}

func testTask() Task {
	return Task{
		ID:           "t1",
		Repo:         "/tmp/repo",
		BaseRevision: "abc123",
		Prompt:       "fix the bug",
		Success:      []string{"go test ./..."},
	}
}

func testSuite(tasks ...Task) Suite {
	if len(tasks) == 0 {
		tasks = []Task{testTask()}
	}
	return Suite{Name: "test-suite", Tasks: tasks}
}

func TestRunnerRecordsSuccessAndMetrics(t *testing.T) {
	exec := &fakeExec{}
	metrics := &fakeMetrics{in: 500, out: 200, turns: 4, cost: 0.05}
	r := auxRunner(exec, metrics)

	got, err := r.RunSuite(t.Context(), testSuite(), "baseline")
	if err != nil {
		t.Fatalf("RunSuite: %v", err)
	}
	if len(got.Runs) != 1 || !got.Runs[0].Succeeded {
		t.Fatalf("expected one successful run, got %+v", got.Runs)
	}
	if got.Runs[0].TotalTokens() != 700 || got.Runs[0].Turns != 4 {
		t.Fatalf("metrics not captured: %+v", got.Runs[0])
	}
	if metrics.sawSessionID != "sess-1" {
		t.Fatalf("metrics should be read for the session the agent reported, got %q", metrics.sawSessionID)
	}
}

// Isolation is what stops task N from measuring the mess task N-1 left.
func TestRunnerResetsRepoToBaseRevision(t *testing.T) {
	exec := &fakeExec{}
	r := auxRunner(exec, nil)

	if _, err := r.RunSuite(t.Context(), testSuite(), ""); err != nil {
		t.Fatalf("RunSuite: %v", err)
	}
	if !exec.ran("git reset --hard abc123") {
		t.Fatalf("expected a reset to the pinned revision, ran: %v", exec.commands)
	}
	if !exec.ran("git clean -fd") {
		t.Fatalf("expected untracked files to be cleaned, ran: %v", exec.commands)
	}
}

// aux's data directory self-protects with a nested .gitignore, so `git clean
// -fd` alone leaves it standing and task N would inherit task N-1's session
// database. Isolation must remove it explicitly.
func TestRunnerRemovesAuxDataDirBetweenTasks(t *testing.T) {
	exec := &fakeExec{}
	r := auxRunner(exec, nil)

	if _, err := r.RunSuite(t.Context(), testSuite(), ""); err != nil {
		t.Fatalf("RunSuite: %v", err)
	}
	if !exec.ran("rm -rf .aux") {
		t.Fatalf("expected the aux data directory to be removed, ran: %v", exec.commands)
	}
}

// The success commands decide, not the agent's exit status: an agent can exit
// non-zero having done the work.
func TestAgentExitStatusDoesNotDecideSuccess(t *testing.T) {
	exec := &fakeExec{failAgent: true}
	r := auxRunner(exec, nil)

	got, err := r.RunSuite(t.Context(), testSuite(), "")
	if err != nil {
		t.Fatalf("RunSuite: %v", err)
	}
	if !got.Runs[0].Succeeded {
		t.Fatal("the success command passed, so the task passed regardless of the agent's exit code")
	}
}

// ...and the converse: a clean agent exit having done nothing must fail.
func TestFailingSuccessCommandFailsTheTask(t *testing.T) {
	exec := &fakeExec{failOn: "go test"}
	r := auxRunner(exec, nil)

	got, err := r.RunSuite(t.Context(), testSuite(), "")
	if err != nil {
		t.Fatalf("RunSuite: %v", err)
	}
	if got.Runs[0].Succeeded {
		t.Fatal("a failing success command must fail the task")
	}
	if !strings.Contains(got.Runs[0].FailureReason, "go test") {
		t.Fatalf("the reason should name the failing command, got %q", got.Runs[0].FailureReason)
	}
}

func TestSetupFailureIsReportedDistinctly(t *testing.T) {
	task := testTask()
	task.Setup = []string{"npm ci"}
	exec := &fakeExec{failOn: "npm ci"}
	r := auxRunner(exec, nil)

	got, _ := r.RunSuite(t.Context(), testSuite(task), "")
	if got.Runs[0].Succeeded {
		t.Fatal("a failed setup must fail the task")
	}
	if !strings.Contains(got.Runs[0].FailureReason, "setup failed") {
		t.Fatalf("a setup failure should be distinguishable from a real one, got %q", got.Runs[0].FailureReason)
	}
	if exec.ran(" -p ") {
		t.Fatal("the agent must not run when setup failed")
	}
}

// One broken task must not cost the whole suite.
func TestOneFailingTaskDoesNotAbortTheSuite(t *testing.T) {
	a, b, c := testTask(), testTask(), testTask()
	a.ID, b.ID, c.ID = "a", "b", "c"
	b.Success = []string{"this-one-fails"}

	exec := &fakeExec{failOn: "this-one-fails"}
	r := auxRunner(exec, nil)

	got, err := r.RunSuite(t.Context(), testSuite(a, b, c), "")
	if err != nil {
		t.Fatalf("RunSuite: %v", err)
	}
	if len(got.Runs) != 3 {
		t.Fatalf("every task should be attempted, got %d", len(got.Runs))
	}
	if got.Summarize().Succeeded != 2 {
		t.Fatalf("expected 2 of 3 to pass, got %+v", got.Summarize())
	}
}

// A run whose cost could not be read must not look like the cheapest in the
// suite.
func TestUnreadableMetricsMarkCostUnknown(t *testing.T) {
	r := auxRunner(&fakeExec{}, &fakeMetrics{err: errors.New("no ledger")})

	got, _ := r.RunSuite(t.Context(), testSuite(), "")
	if !got.Runs[0].CostUnknown {
		t.Fatal("unreadable metrics must be flagged, not recorded as zero cost")
	}
	if !got.Summarize().CostUnknown {
		t.Fatal("the aggregate must carry the taint")
	}
}

func TestMissingSessionIDMarksCostUnknown(t *testing.T) {
	exec := &fakeExec{agentOut: "plain text, no json"}
	r := auxRunner(exec, &fakeMetrics{in: 100})

	got, _ := r.RunSuite(t.Context(), testSuite(), "")
	if !got.Runs[0].CostUnknown {
		t.Fatal("without a session id there is nothing to measure, and that must be visible")
	}
}

func TestSessionIDParsing(t *testing.T) {
	for _, tc := range []struct {
		name, out, want string
		wantErr         bool
	}{
		{name: "plain object", out: `{"response":"hi","sessionId":"s1"}`, want: "s1"},
		{name: "preceded by noise", out: "loading...\n{\"response\":\"hi\",\"sessionId\":\"s2\"}", want: "s2"},
		{name: "pretty printed", out: "{\n  \"response\": \"hi\",\n  \"sessionId\": \"s3\"\n}", want: "s3"},
		{name: "no json", out: "nothing here", wantErr: true},
		{name: "json without session", out: `{"response":"hi"}`, wantErr: true},
	} {
		got, err := sessionIDFrom(tc.out)
		if tc.wantErr {
			if err == nil {
				t.Errorf("%s: expected an error, got %q", tc.name, got)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("%s: got (%q, %v), want %q", tc.name, got, err, tc.want)
		}
	}
}

// Prompts are arbitrary text and routinely contain quotes.
func TestPromptsWithQuotesSurviveShellQuoting(t *testing.T) {
	task := testTask()
	task.Prompt = `fix the "off by one" in Bob's loop; don't touch $HOME`

	exec := &fakeExec{}
	r := auxRunner(exec, nil)
	if _, err := r.RunSuite(t.Context(), testSuite(task), ""); err != nil {
		t.Fatalf("RunSuite: %v", err)
	}

	var agentCmd string
	for _, c := range exec.commands {
		if strings.Contains(c, " -p ") {
			agentCmd = c
		}
	}
	if agentCmd == "" {
		t.Fatal("the agent was never invoked")
	}
	// Single-quoted, with the embedded quote escaped: nothing can break out.
	if !strings.Contains(agentCmd, `'\''`) {
		t.Fatalf("the apostrophe should be escaped for sh, got %q", agentCmd)
	}
	if strings.Contains(agentCmd, "$HOME'") && !strings.Contains(agentCmd, `'fix`) {
		t.Fatalf("prompt does not look single-quoted: %q", agentCmd)
	}
}

func TestRunnerRejectsAnInvalidSuite(t *testing.T) {
	r := auxRunner(&fakeExec{}, nil)
	bad := Suite{Name: "bad", Tasks: []Task{{ID: "x"}}}

	if _, err := r.RunSuite(t.Context(), bad, ""); err == nil {
		t.Fatal("an invalid suite must be rejected before spending anything on it")
	}
}

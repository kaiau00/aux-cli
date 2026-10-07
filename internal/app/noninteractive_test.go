package app

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kaiau00/aux-cli/internal/db"
	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/llm/agent"
	"github.com/kaiau00/aux-cli/internal/message"
	"github.com/kaiau00/aux-cli/internal/permission"
	"github.com/kaiau00/aux-cli/internal/session"
)

// bashAgent stands in for the coder agent: it asks to run a command in the
// session it was given and in a linked subagent session, and records what the
// permission service answered.
type bashAgent struct {
	agent.Service
	perms   permission.Service
	granted []bool
}

func (f *bashAgent) Run(_ context.Context, sessionID string, _ string, _ ...message.Attachment) (<-chan agent.AgentEvent, error) {
	child := sessionID + "-sub"
	f.perms.LinkSession(child, sessionID)
	for _, sid := range []string{sessionID, child} {
		f.granted = append(f.granted, f.perms.Request(permission.CreatePermissionRequest{
			SessionID:   sid,
			ToolName:    "bash",
			Action:      "execute",
			Path:        "/repo",
			Description: "Execute command: rm -rf .",
			Fingerprint: "rm -rf .",
		}))
	}
	done := make(chan agent.AgentEvent, 1)
	done <- agent.AgentEvent{Message: message.Message{Parts: []message.ContentPart{message.TextContent{Text: "done"}}}}
	return done, nil
}

func runNonInteractive(t *testing.T, yes bool) (*bashAgent, string) {
	t.Helper()
	perms := permission.NewPermissionService()
	fake := &bashAgent{perms: perms}
	var stderr bytes.Buffer
	a := &App{
		Sessions:    session.NewService(db.New(dbtest.New(t))),
		Permissions: perms,
		CoderAgent:  fake,
		stderr:      &stderr,
	}

	errCh := make(chan error, 1)
	go func() { errCh <- a.RunNonInteractive(context.Background(), "run ls", "text", true, yes, "") }()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("RunNonInteractive: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunNonInteractive blocked: a permission request waited for a prompt nobody can answer")
	}
	return fake, stderr.String()
}

func TestRunNonInteractiveDeniesWithoutYes(t *testing.T) {
	fake, stderr := runNonInteractive(t, false)
	for i, g := range fake.granted {
		if g {
			t.Fatalf("request %d was granted without --yes", i)
		}
	}
	if !strings.Contains(stderr, "2 action(s) were denied because --yes was not given:") {
		t.Fatalf("stderr missing the denial summary:\n%s", stderr)
	}
	if strings.Count(stderr, "bash execute: rm -rf .") != 2 {
		t.Fatalf("stderr should list both denied requests, parent and subagent:\n%s", stderr)
	}
}

func TestRunNonInteractiveApprovesWithYes(t *testing.T) {
	fake, stderr := runNonInteractive(t, true)
	if len(fake.granted) != 2 || !fake.granted[0] || !fake.granted[1] {
		t.Fatalf("granted = %v; want both requests approved with --yes", fake.granted)
	}
	if stderr != "" {
		t.Fatalf("nothing was denied, so stderr should be empty:\n%s", stderr)
	}
}

package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/kaiau00/aux-cli/internal/db"
	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/session"
	"github.com/spf13/cobra"
)

// parse runs argv through a command carrying the real flag definitions and
// returns what the resume flags asked for.
func parse(t *testing.T, argv ...string) (resumeRequest, error) {
	t.Helper()
	var got resumeRequest
	var parseErr error
	cmd := &cobra.Command{
		Use:          "aux",
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			got, parseErr = parseResumeFlags(c, args)
			return nil
		},
	}
	registerResumeFlags(cmd)
	cmd.SetArgs(argv)
	cmd.SetOut(&strings.Builder{})
	cmd.SetErr(&strings.Builder{})
	if err := cmd.Execute(); err != nil {
		return resumeRequest{}, err
	}
	return got, parseErr
}

// Every spelling a user might reach for has to land somewhere sensible. The
// dangerous one is `--resume abc`: pflag's optional-value handling leaves the
// id in the positional arguments, so without the recovery in parseResumeFlags
// this silently opens the picker and the id the user typed is discarded.
func TestResumeFlagsAcceptEverySpelling(t *testing.T) {
	for _, tc := range []struct {
		name string
		argv []string
		want resumeRequest
	}{
		{"no flags", nil, resumeRequest{}},
		{"continue long", []string{"--continue"}, resumeRequest{mostRecent: true}},
		{"continue short", []string{"-C"}, resumeRequest{mostRecent: true}},
		{"resume with id, space", []string{"--resume", "abc"}, resumeRequest{sessionID: "abc"}},
		{"resume with id, equals", []string{"--resume=abc"}, resumeRequest{sessionID: "abc"}},
		{"resume short with id", []string{"-r", "abc"}, resumeRequest{sessionID: "abc"}},
		{"resume bare", []string{"--resume"}, resumeRequest{picker: true}},
		{"resume short bare", []string{"-r"}, resumeRequest{picker: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parse(t, tc.argv...)
			if err != nil {
				t.Fatalf("parse %v: %v", tc.argv, err)
			}
			if got != tc.want {
				t.Errorf("parse %v = %+v, want %+v", tc.argv, got, tc.want)
			}
			if got.wanted() != (tc.want != resumeRequest{}) {
				t.Errorf("parse %v: wanted() = %v", tc.argv, got.wanted())
			}
		})
	}
}

func TestContinueAndResumeTogetherIsRejected(t *testing.T) {
	if _, err := parse(t, "--continue", "--resume", "abc"); err == nil {
		t.Fatal("--continue with --resume should be refused, both name a different session")
	}
}

// --continue must skip the sessions a subagent or the title generator creates.
// Those are written constantly during a turn, so they are usually the newest
// rows in the table and are never what the user meant to return to.
func TestMostRecentSkipsTaskAndTitleSessions(t *testing.T) {
	ctx := context.Background()
	svc := session.NewService(db.New(dbtest.New(t)))

	older, err := svc.Create(ctx, "older")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	wanted, err := svc.Create(ctx, "what the user was doing")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Created last, so any ordering that ignores parent_session_id returns one
	// of these instead.
	if _, err := svc.CreateTaskSession(ctx, "tool-call-1", wanted.ID, "subagent"); err != nil {
		t.Fatalf("CreateTaskSession: %v", err)
	}
	if _, err := svc.CreateTitleSession(ctx, wanted.ID); err != nil {
		t.Fatalf("CreateTitleSession: %v", err)
	}

	got, err := resolveResume(ctx, svc, resumeRequest{mostRecent: true})
	if err != nil {
		t.Fatalf("resolveResume: %v", err)
	}
	if got.ID != wanted.ID {
		t.Errorf("--continue opened %q (%s), want %q (%s)", got.Title, got.ID, wanted.Title, wanted.ID)
	}
	if got.ID == older.ID {
		t.Error("--continue opened the older session")
	}
}

// A first run in a project has nothing to resume. That is the normal case, not
// a failure: the caller starts a new session instead.
func TestContinueWithNoSessionsIsNotAnError(t *testing.T) {
	ctx := context.Background()
	svc := session.NewService(db.New(dbtest.New(t)))

	got, err := resolveResume(ctx, svc, resumeRequest{mostRecent: true})
	if err != nil {
		t.Fatalf("resolveResume on an empty project: %v", err)
	}
	if got.ID != "" {
		t.Errorf("expected no session, got %q", got.ID)
	}
}

func TestResumeByIDOpensThatSessionAndRejectsAnUnknownOne(t *testing.T) {
	ctx := context.Background()
	svc := session.NewService(db.New(dbtest.New(t)))

	target, err := svc.Create(ctx, "the one asked for")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.Create(ctx, "newer, not asked for"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := resolveResume(ctx, svc, resumeRequest{sessionID: target.ID})
	if err != nil {
		t.Fatalf("resolveResume: %v", err)
	}
	if got.ID != target.ID {
		t.Errorf("resumed %q, want %q", got.ID, target.ID)
	}

	_, err = resolveResume(ctx, svc, resumeRequest{sessionID: "does-not-exist"})
	if err == nil {
		t.Fatal("an unknown id should fail before the TUI starts")
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Errorf("error should name the id the user typed, got %v", err)
	}
}

// The picker asks for no session of its own; the TUI lists them.
func TestPickerResolvesToNoSession(t *testing.T) {
	ctx := context.Background()
	svc := session.NewService(db.New(dbtest.New(t)))
	if _, err := svc.Create(ctx, "something"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := resolveResume(ctx, svc, resumeRequest{picker: true})
	if err != nil {
		t.Fatalf("resolveResume: %v", err)
	}
	if got.ID != "" {
		t.Errorf("picker should select nothing up front, got %q", got.ID)
	}
}

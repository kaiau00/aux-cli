package session_test

import (
	"context"
	"testing"

	"github.com/kaiau00/aux-cli/internal/db"
	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/session"
)

// ContextTokens has to survive a write and come back on both read paths.
// The generated query layer lists columns explicitly, so a column that is
// added to the schema but missed in a SELECT or a Scan silently reads zero --
// which for this field is indistinguishable from a session that has not run a
// turn yet.
func TestContextTokensRoundTripsThroughEveryReadPath(t *testing.T) {
	conn := dbtest.New(t)
	svc := session.NewService(db.New(conn))
	ctx := context.Background()

	created, err := svc.Create(ctx, "occupancy")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ContextTokens != 0 {
		t.Fatalf("a new session should hold nothing, got %d", created.ContextTokens)
	}

	created.ContextTokens = 21472
	created.PromptTokens = 147875
	created.CompletionTokens = 395
	saved, err := svc.Save(ctx, created)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if saved.ContextTokens != 21472 {
		t.Errorf("Save returned ContextTokens = %d, want 21472", saved.ContextTokens)
	}

	got, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ContextTokens != 21472 {
		t.Errorf("Get returned ContextTokens = %d, want 21472", got.ContextTokens)
	}
	// The cumulative figures must be untouched: they are still what cost is
	// computed from.
	if got.PromptTokens != 147875 || got.CompletionTokens != 395 {
		t.Errorf("cumulative totals were disturbed: prompt=%d completion=%d",
			got.PromptTokens, got.CompletionTokens)
	}

	listed, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var found bool
	for _, s := range listed {
		if s.ID == created.ID {
			found = true
			if s.ContextTokens != 21472 {
				t.Errorf("List returned ContextTokens = %d, want 21472", s.ContextTokens)
			}
		}
	}
	if !found {
		t.Fatal("the saved session did not come back from List")
	}
}

package session_test

import (
	"context"
	"testing"

	"github.com/kaiau00/aux-cli/internal/db"
	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/session"
)

// Title generation runs in a goroutine started on a session's first message,
// and the turn that triggered it keeps reconciling tokens and cost while the
// title model call is in flight. Renaming must therefore touch the title and
// nothing else: a read-modify-write of the whole row carries the pre-turn zeros
// back over whatever the turn recorded, which is how first turns ended up
// stored with no tokens and no cost.
func TestSetTitleDoesNotClobberWhatTheTurnRecorded(t *testing.T) {
	conn := dbtest.New(t)
	svc := session.NewService(db.New(conn))
	ctx := context.Background()

	created, err := svc.Create(ctx, "New Session")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// What the title goroutine read before its model call: everything zero.
	stale := created

	// Meanwhile the turn finishes and reconciles.
	reconciled := created
	reconciled.PromptTokens = 20997
	reconciled.CompletionTokens = 143
	reconciled.ContextTokens = 21140
	reconciled.Cost = 0.42
	if _, err := svc.Save(ctx, reconciled); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Now the title lands.
	if _, err := svc.SetTitle(ctx, stale.ID, "Friendly Greeting"); err != nil {
		t.Fatalf("SetTitle: %v", err)
	}

	got, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != "Friendly Greeting" {
		t.Errorf("title = %q, want %q", got.Title, "Friendly Greeting")
	}
	if got.PromptTokens != 20997 || got.CompletionTokens != 143 {
		t.Errorf("the turn's token totals were clobbered: prompt=%d completion=%d",
			got.PromptTokens, got.CompletionTokens)
	}
	if got.ContextTokens != 21140 {
		t.Errorf("context occupancy was clobbered: %d, want 21140", got.ContextTokens)
	}
	if got.Cost != 0.42 {
		t.Errorf("cost was clobbered: %v, want 0.42", got.Cost)
	}
}

// The regression this replaces, kept explicit: saving a whole stale Session is
// exactly what loses the turn's numbers. If someone reintroduces a
// read-modify-write rename, this documents what it costs.
func TestSavingAStaleSessionLosesTheTurnsNumbers(t *testing.T) {
	conn := dbtest.New(t)
	svc := session.NewService(db.New(conn))
	ctx := context.Background()

	created, err := svc.Create(ctx, "New Session")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	stale := created

	reconciled := created
	reconciled.PromptTokens = 20997
	reconciled.ContextTokens = 21140
	if _, err := svc.Save(ctx, reconciled); err != nil {
		t.Fatalf("Save: %v", err)
	}

	stale.Title = "Friendly Greeting"
	if _, err := svc.Save(ctx, stale); err != nil {
		t.Fatalf("Save stale: %v", err)
	}

	got, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.PromptTokens != 0 || got.ContextTokens != 0 {
		t.Fatalf("expected the stale write to have zeroed the totals, got prompt=%d context=%d; "+
			"if this now passes, Save has gained protection and SetTitle may be redundant",
			got.PromptTokens, got.ContextTokens)
	}
}

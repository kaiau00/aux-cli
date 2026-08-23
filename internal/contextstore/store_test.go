package contextstore_test

import (
	"context"
	"testing"

	"github.com/kaiau00/aux-cli/internal/contextstore"
	"github.com/kaiau00/aux-cli/internal/db/dbtest"
)

func TestPageAndVersionDedup(t *testing.T) {
	store := contextstore.NewStore(dbtest.New(t))
	ctx := context.Background()

	p1, err := store.UpsertPage(ctx, "proj", contextstore.KindFileRegion, "file:/a.go", "")
	if err != nil {
		t.Fatalf("UpsertPage: %v", err)
	}
	p2, err := store.UpsertPage(ctx, "proj", contextstore.KindFileRegion, "file:/a.go", "")
	if err != nil {
		t.Fatalf("UpsertPage 2: %v", err)
	}
	if p1.ID != p2.ID {
		t.Fatalf("same page identity should dedup: %s != %s", p1.ID, p2.ID)
	}

	v1, err := store.UpsertVersion(ctx, p1.ID, "hashA", "rev1", 100)
	if err != nil {
		t.Fatalf("UpsertVersion: %v", err)
	}
	v2, _ := store.UpsertVersion(ctx, p1.ID, "hashA", "rev1", 100)
	if v1.ID != v2.ID {
		t.Fatalf("same content hash should dedup version")
	}
	v3, _ := store.UpsertVersion(ctx, p1.ID, "hashB", "rev2", 120)
	if v3.ID == v1.ID {
		t.Fatalf("different content hash should be a new version")
	}
}

func TestBindAndExplainByPage(t *testing.T) {
	store := contextstore.NewStore(dbtest.New(t))
	ctx := context.Background()

	page, _ := store.UpsertPage(ctx, "proj", contextstore.KindToolDigest, "msg:1", "")
	ver, _ := store.UpsertVersion(ctx, page.ID, "hash1", "", 42)
	if err := store.Bind(ctx, contextstore.Binding{
		TaskID: "task-1", ModelCallID: "call-1", PageVersionID: ver.ID,
		State: contextstore.StateResident, Rank: 0, Reason: "transcript", TokenCount: 42,
	}); err != nil {
		t.Fatalf("Bind: %v", err)
	}

	// A second, available page for the same call.
	page2, _ := store.UpsertPage(ctx, "proj", contextstore.KindProjectManifest, "project_manifest", "")
	ver2, _ := store.UpsertVersion(ctx, page2.ID, "hash2", "", 10)
	_ = store.Bind(ctx, contextstore.Binding{
		ModelCallID: "call-1", PageVersionID: ver2.ID, State: contextstore.StateAvailable, Rank: 1,
	})

	bound, err := store.BindingsForCall(ctx, "call-1")
	if err != nil {
		t.Fatalf("BindingsForCall: %v", err)
	}
	if len(bound) != 2 {
		t.Fatalf("expected 2 bound pages for the call, got %d", len(bound))
	}
	// Ordered by state (available < resident alphabetically).
	var residentFound, availableFound bool
	for _, bp := range bound {
		if bp.State == contextstore.StateResident && bp.PageType == contextstore.KindToolDigest {
			residentFound = true
		}
		if bp.State == contextstore.StateAvailable && bp.PageType == contextstore.KindProjectManifest {
			availableFound = true
		}
	}
	if !residentFound || !availableFound {
		t.Fatalf("bindings did not explain the prompt page by page: %+v", bound)
	}
}

func TestRecordAccess(t *testing.T) {
	store := contextstore.NewStore(dbtest.New(t))
	ctx := context.Background()
	page, _ := store.UpsertPage(ctx, "", contextstore.KindFileRegion, "file:/x", "")
	ver, _ := store.UpsertVersion(ctx, page.ID, "h", "", 1)
	if err := store.RecordAccess(ctx, "task", "call", ver.ID, "read", "later_edit"); err != nil {
		t.Fatalf("RecordAccess: %v", err)
	}
}

func TestPinUnpinAndClear(t *testing.T) {
	store := contextstore.NewStore(dbtest.New(t))
	ctx := context.Background()

	if err := store.Pin(ctx, "task-1", "call-a"); err != nil {
		t.Fatalf("Pin: %v", err)
	}
	if err := store.Pin(ctx, "task-1", "call-b"); err != nil {
		t.Fatalf("Pin: %v", err)
	}
	// A second pin of the same (task, call) is idempotent.
	if err := store.Pin(ctx, "task-1", "call-a"); err != nil {
		t.Fatalf("Pin (repeat): %v", err)
	}
	// Pinning under a different task must not interfere.
	if err := store.Pin(ctx, "task-2", "call-a"); err != nil {
		t.Fatalf("Pin (other task): %v", err)
	}

	pins, err := store.Pins(ctx, "task-1")
	if err != nil {
		t.Fatalf("Pins: %v", err)
	}
	if len(pins) != 2 || !pins["call-a"] || !pins["call-b"] {
		t.Fatalf("unexpected pins: %+v", pins)
	}

	if err := store.Unpin(ctx, "task-1", "call-a"); err != nil {
		t.Fatalf("Unpin: %v", err)
	}
	pins, err = store.Pins(ctx, "task-1")
	if err != nil {
		t.Fatalf("Pins after Unpin: %v", err)
	}
	if len(pins) != 1 || pins["call-a"] {
		t.Fatalf("Unpin did not clear call-a: %+v", pins)
	}

	if err := store.ClearPins(ctx, "task-1"); err != nil {
		t.Fatalf("ClearPins: %v", err)
	}
	pins, err = store.Pins(ctx, "task-1")
	if err != nil {
		t.Fatalf("Pins after Clear: %v", err)
	}
	if len(pins) != 0 {
		t.Fatalf("ClearPins did not clear all: %+v", pins)
	}
}

func TestExcludeIncludeAndClear(t *testing.T) {
	store := contextstore.NewStore(dbtest.New(t))
	ctx := context.Background()

	if err := store.Exclude(ctx, "task-1", "call-a"); err != nil {
		t.Fatalf("Exclude: %v", err)
	}
	if err := store.Exclude(ctx, "task-1", "call-b"); err != nil {
		t.Fatalf("Exclude: %v", err)
	}
	// A second exclude of the same (task, call) is idempotent.
	if err := store.Exclude(ctx, "task-1", "call-a"); err != nil {
		t.Fatalf("Exclude (repeat): %v", err)
	}
	// Excluding under a different task must not interfere.
	if err := store.Exclude(ctx, "task-2", "call-a"); err != nil {
		t.Fatalf("Exclude (other task): %v", err)
	}

	excl, err := store.Exclusions(ctx, "task-1")
	if err != nil {
		t.Fatalf("Exclusions: %v", err)
	}
	if len(excl) != 2 || !excl["call-a"] || !excl["call-b"] {
		t.Fatalf("unexpected exclusions: %+v", excl)
	}

	if err := store.Include(ctx, "task-1", "call-a"); err != nil {
		t.Fatalf("Include: %v", err)
	}
	excl, err = store.Exclusions(ctx, "task-1")
	if err != nil {
		t.Fatalf("Exclusions after Include: %v", err)
	}
	if len(excl) != 1 || excl["call-a"] {
		t.Fatalf("Include did not clear call-a: %+v", excl)
	}

	if err := store.ClearExclusions(ctx, "task-1"); err != nil {
		t.Fatalf("ClearExclusions: %v", err)
	}
	excl, err = store.Exclusions(ctx, "task-1")
	if err != nil {
		t.Fatalf("Exclusions after Clear: %v", err)
	}
	if len(excl) != 0 {
		t.Fatalf("expected no exclusions after Clear, got %+v", excl)
	}

	// task-2's exclusion must be unaffected by task-1's Clear.
	other, err := store.Exclusions(ctx, "task-2")
	if err != nil {
		t.Fatalf("Exclusions(task-2): %v", err)
	}
	if len(other) != 1 || !other["call-a"] {
		t.Fatalf("expected task-2 exclusion to survive task-1's clear: %+v", other)
	}
}

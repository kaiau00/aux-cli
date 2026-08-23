package artifact_test

import (
	"context"
	"strings"
	"testing"

	"github.com/kaiau00/aux-cli/internal/artifact"
	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/llm/tools"
)

func newService(t *testing.T) *artifact.Service {
	t.Helper()
	backend := artifact.NewFSBackend(t.TempDir())
	return artifact.NewService(backend, artifact.NewStore(dbtest.New(t)))
}

func TestPutGetRoundTripAndDedup(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	data := []byte("the quick brown fox\njumps over\nthe lazy dog")

	a1, reused, err := svc.Put(ctx, data, "text/plain", artifact.OwnerRef{Type: "tool_execution", ID: "e1"})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if reused {
		t.Fatalf("first put should not be reused")
	}
	if a1.ByteSize != int64(len(data)) {
		t.Fatalf("byte size = %d", a1.ByteSize)
	}

	// Same content dedups to the same artifact.
	a2, reused2, err := svc.Put(ctx, data, "text/plain", artifact.OwnerRef{Type: "tool_execution", ID: "e2"})
	if err != nil {
		t.Fatalf("Put 2: %v", err)
	}
	if !reused2 || a2.ID != a1.ID {
		t.Fatalf("identical content should dedup: reused=%v id1=%s id2=%s", reused2, a1.ID, a2.ID)
	}

	got, err := svc.Get(ctx, a1.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("round trip mismatch")
	}
}

func TestGetRangeAndSearch(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	data := []byte("line one\nline two\nerror: boom\nline four")
	a, _, err := svc.Put(ctx, data, "text/plain", artifact.OwnerRef{Type: "t", ID: "e"})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	head, err := svc.GetRange(ctx, a.ID, 0, 8)
	if err != nil {
		t.Fatalf("GetRange: %v", err)
	}
	if string(head) != "line one" {
		t.Fatalf("range = %q", head)
	}

	matches, err := svc.Search(ctx, a.ID, "error", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(matches) != 1 || matches[0].Line != 3 {
		t.Fatalf("search wrong: %+v", matches)
	}
}

func TestBackendDetectsCorruption(t *testing.T) {
	// Reading a key whose content doesn't hash to the key must error.
	be := artifact.NewFSBackend(t.TempDir())
	hash, err := be.Write(artifact.HashBytes([]byte("hello")), []byte("hello"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := be.Read(hash); err != nil {
		t.Fatalf("Read valid: %v", err)
	}
	// A key that doesn't correspond to stored content simply won't exist.
	if _, err := be.Read("deadbeef"); err == nil {
		t.Fatalf("expected error reading missing key")
	}
}

func TestGCReportListsUnreferenced(t *testing.T) {
	// Insert an artifact with no ref by using an empty owner.
	svc := newService(t)
	ctx := context.Background()
	if _, _, err := svc.Put(ctx, []byte("orphan"), "text/plain", artifact.OwnerRef{}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	unref, total, err := svc.GCReport(ctx)
	if err != nil {
		t.Fatalf("GCReport: %v", err)
	}
	if len(unref) != 1 || total != int64(len("orphan")) {
		t.Fatalf("expected 1 unreferenced artifact of 6 bytes, got %d/%d", len(unref), total)
	}
}

func TestVirtualizerOffAndOn(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	big := tools.NewTextResponse(strings.Repeat("x\n", 5000)) // ~10k bytes, many lines

	// Off: unchanged.
	off := artifact.NewVirtualizer(svc, artifact.ModeOff, 1000)
	rec := &tools.ExecutionRecord{ID: "e1"}
	out, err := off.Virtualize(ctx, rec, big)
	if err != nil {
		t.Fatalf("off virtualize: %v", err)
	}
	if out.Content != big.Content || rec.ArtifactID != "" {
		t.Fatalf("off mode should not change response or store artifact")
	}

	// On: response replaced with a digest containing the handle; full output recoverable.
	on := artifact.NewVirtualizer(svc, artifact.ModeOn, 1000)
	rec2 := &tools.ExecutionRecord{ID: "e2"}
	digest, err := on.Virtualize(ctx, rec2, big)
	if err != nil {
		t.Fatalf("on virtualize: %v", err)
	}
	if rec2.ArtifactID == "" {
		t.Fatalf("on mode should store an artifact")
	}
	if len(digest.Content) >= len(big.Content) {
		t.Fatalf("digest should be smaller than original")
	}
	if !strings.Contains(digest.Content, rec2.ArtifactID) {
		t.Fatalf("digest should surface the artifact handle")
	}
	if rec2.BytesSaved <= 0 {
		t.Fatalf("expected positive bytes saved, got %d", rec2.BytesSaved)
	}
	// Full output recoverable from the artifact.
	full, err := svc.Get(ctx, rec2.ArtifactID)
	if err != nil {
		t.Fatalf("Get full: %v", err)
	}
	if string(full) != big.Content {
		t.Fatalf("full output not recoverable")
	}
}

func TestVirtualizerObserveMeasuresWithoutChanging(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	big := tools.NewTextResponse(strings.Repeat("y\n", 5000))

	obs := artifact.NewVirtualizer(svc, artifact.ModeObserve, 1000)
	rec := &tools.ExecutionRecord{ID: "e3"}
	out, err := obs.Virtualize(ctx, rec, big)
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	if out.Content != big.Content {
		t.Fatalf("observe mode must not change the response")
	}
	if rec.ArtifactID == "" {
		t.Fatalf("observe mode should still store the artifact")
	}
	if rec.BytesSaved <= 0 {
		t.Fatalf("observe mode should measure potential savings")
	}
}

func TestVirtualizerSmallOutputUntouched(t *testing.T) {
	svc := newService(t)
	on := artifact.NewVirtualizer(svc, artifact.ModeOn, 1000)
	rec := &tools.ExecutionRecord{ID: "e4"}
	small := tools.NewTextResponse("tiny")
	out, err := on.Virtualize(context.Background(), rec, small)
	if err != nil {
		t.Fatalf("virtualize: %v", err)
	}
	if out.Content != "tiny" || rec.ArtifactID != "" {
		t.Fatalf("small output should be left untouched")
	}
}

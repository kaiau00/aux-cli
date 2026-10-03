package bundle_test

import (
	"context"
	"testing"

	"github.com/kaiau00/aux-cli/internal/bundle"
	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/skill"
)

// seedActive creates one active skill in a source db.
func seedActive(t *testing.T) *skill.Store {
	t.Helper()
	conn := dbtest.New(t)
	ctx := context.Background()

	skStore := skill.NewStore(conn)
	skSvc := skill.NewService(skStore, nil)
	sk, ver, err := skSvc.Candidate(ctx, "user", "", skill.Content{Name: "run-tests", Purpose: "run the suite"}, "manual", nil)
	if err != nil {
		t.Fatalf("skill Candidate: %v", err)
	}
	_ = skSvc.Evaluate(ctx, ver.ID, "", "r", skill.EvalResult("pass"), "{}")
	if err := skSvc.Promote(ctx, sk.ID, ver.ID); err != nil {
		t.Fatalf("skill Promote: %v", err)
	}
	return skStore
}

func TestExportImportRoundTripArrivesAsCandidates(t *testing.T) {
	ctx := context.Background()
	src := seedActive(t)

	b, err := bundle.Export(ctx, src)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if len(b.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(b.Skills))
	}

	// Round-trip through the wire format.
	data, err := bundle.Marshal(b)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	parsed, err := bundle.Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	// Import into a fresh destination project.
	dstSkills := skill.NewService(skill.NewStore(dbtest.New(t)), nil)
	res, err := bundle.Import(ctx, parsed, dstSkills)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if res.SkillsImported != 1 {
		t.Fatalf("import result = %+v", res)
	}

	// The safety property: imported skills arrive as candidates, NOT active.
	if active, _ := dstSkills.Active(ctx); len(active) != 0 {
		t.Fatalf("imported skill must not be active, got %d active", len(active))
	}
	if cands, _ := dstSkills.Candidates(ctx); len(cands) != 1 {
		t.Fatalf("imported skill should be a candidate, got %d", len(cands))
	}
}

func TestVerifyRejectsTamperedBundle(t *testing.T) {
	ctx := context.Background()
	b, _ := bundle.Export(ctx, seedActive(t))

	// Tamper with content after the hash was computed.
	b.Skills[0].Content.Name = "malicious"
	if err := b.Verify(); err == nil {
		t.Fatal("Verify must reject a bundle whose content no longer matches its hash")
	}
	// Import must refuse the tampered bundle.
	if _, err := bundle.Import(ctx, b, skill.NewService(skill.NewStore(dbtest.New(t)), nil)); err == nil {
		t.Fatal("Import must refuse a tampered bundle")
	}
}

func TestVerifyRejectsUnknownFormat(t *testing.T) {
	for _, v := range []int{1, 999} {
		b := bundle.Bundle{FormatVersion: v}
		if err := b.Verify(); err == nil {
			t.Fatalf("Verify must reject format version %d", v)
		}
	}
}

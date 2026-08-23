package bundle_test

import (
	"context"
	"testing"

	"github.com/kaiau00/aux-cli/internal/bundle"
	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/govpolicy"
	"github.com/kaiau00/aux-cli/internal/skill"
)

// seedActive creates one active skill and one active policy in a source db.
func seedActive(t *testing.T) (*skill.Store, *govpolicy.Store) {
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

	polStore := govpolicy.NewStore(conn)
	polSvc := govpolicy.NewService(polStore, nil)
	p, _ := polSvc.Candidate(ctx, "project", "proj-1", "implementation", `{"mode":"on"}`)
	_ = polSvc.Evaluate(ctx, p.ID, "", "r", govpolicy.Pass, "{}")
	if err := polSvc.Promote(ctx, p.ID); err != nil {
		t.Fatalf("policy Promote: %v", err)
	}
	return skStore, polStore
}

func TestExportImportRoundTripArrivesAsCandidates(t *testing.T) {
	ctx := context.Background()
	srcSkills, srcPolicies := seedActive(t)

	b, err := bundle.Export(ctx, srcSkills, srcPolicies)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if len(b.Skills) != 1 || len(b.Policies) != 1 {
		t.Fatalf("expected 1 skill + 1 policy, got %d/%d", len(b.Skills), len(b.Policies))
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
	dstConn := dbtest.New(t)
	dstSkills := skill.NewService(skill.NewStore(dstConn), nil)
	dstPolicies := govpolicy.NewService(govpolicy.NewStore(dstConn), nil)

	res, err := bundle.Import(ctx, parsed, dstSkills, dstPolicies)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if res.SkillsImported != 1 || res.PoliciesImported != 1 {
		t.Fatalf("import result = %+v", res)
	}

	// The safety property: imported optimizations arrive as candidates, NOT active.
	if active, _ := dstSkills.Active(ctx); len(active) != 0 {
		t.Fatalf("imported skill must not be active, got %d active", len(active))
	}
	if cands, _ := dstSkills.Candidates(ctx); len(cands) != 1 {
		t.Fatalf("imported skill should be a candidate, got %d", len(cands))
	}
	if active, _ := dstPolicies.Active(ctx); len(active) != 0 {
		t.Fatalf("imported policy must not be active, got %d active", len(active))
	}
	if cands, _ := dstPolicies.Candidates(ctx); len(cands) != 1 {
		t.Fatalf("imported policy should be a candidate, got %d", len(cands))
	}
}

func TestVerifyRejectsTamperedBundle(t *testing.T) {
	ctx := context.Background()
	srcSkills, srcPolicies := seedActive(t)
	b, _ := bundle.Export(ctx, srcSkills, srcPolicies)

	// Tamper with content after the hash was computed.
	b.Skills[0].Content.Name = "malicious"
	if err := b.Verify(); err == nil {
		t.Fatal("Verify must reject a bundle whose content no longer matches its hash")
	}
	// Import must refuse the tampered bundle.
	dstConn := dbtest.New(t)
	if _, err := bundle.Import(ctx, b,
		skill.NewService(skill.NewStore(dstConn), nil),
		govpolicy.NewService(govpolicy.NewStore(dstConn), nil)); err == nil {
		t.Fatal("Import must refuse a tampered bundle")
	}
}

func TestVerifyRejectsUnknownFormat(t *testing.T) {
	b := bundle.Bundle{FormatVersion: 999}
	if err := b.Verify(); err == nil {
		t.Fatal("Verify must reject an unknown format version")
	}
}

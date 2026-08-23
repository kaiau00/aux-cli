package skill_test

import (
	"context"
	"strings"
	"testing"

	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/eventstore"
	"github.com/kaiau00/aux-cli/internal/skill"
)

// A skill version whose stored JSON will not parse must be reported as an
// error, not returned as a valid version with empty content.
//
// The empty-but-valid outcome is indistinguishable, from the caller's side,
// from a skill that legitimately has nothing to say: both inject nothing into
// the prompt and both report success. This project's own note on skills is that
// a wrong skill is worse than no skill, and a corrupt one that reports itself
// healthy is the worst case of that.
func TestCorruptVersionContentIsNotReportedAsValid(t *testing.T) {
	conn := dbtest.New(t)
	store := skill.NewStore(conn)
	svc := skill.NewService(store, eventstore.NewService(conn))
	ctx := context.Background()

	sk, ver, err := svc.Candidate(ctx, "project", "proj-1", skill.Content{Name: "add-endpoint"}, "demonstration", []string{"task-1"})
	if err != nil {
		t.Fatalf("Candidate: %v", err)
	}

	// Corrupt the stored content the way a partial write or a schema change
	// would: valid row, unparseable payload.
	if _, err := conn.ExecContext(ctx,
		`UPDATE skill_versions SET content_json = ? WHERE skill_version_id = ?`,
		`{"name": "add-endpoint"`, ver.ID); err != nil {
		t.Fatalf("corrupt the row: %v", err)
	}

	got, found, err := store.LatestVersion(ctx, sk.ID)
	if err == nil {
		t.Fatalf("unreadable content was reported as valid (found=%v, content=%+v)", found, got.Content)
	}
	if !strings.Contains(err.Error(), ver.ID) {
		t.Fatalf("the error must name the version so it can be found; got: %v", err)
	}
	if found {
		t.Fatal("a version that could not be read must not also be reported as found")
	}
}

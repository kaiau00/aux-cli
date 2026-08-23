package relatedproject_test

import (
	"context"
	"testing"

	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/relatedproject"
)

func TestDeriveFromModules(t *testing.T) {
	known := map[string]string{
		"github.com/acme/lib": "proj-lib",
		"github.com/acme/api": "proj-api",
	}
	rels := relatedproject.DeriveFromModules(
		"proj-app", "github.com/acme/app",
		[]string{"github.com/acme/lib", "github.com/acme/api", "github.com/third/party", "github.com/acme/app"},
		known,
	)
	if len(rels) != 2 {
		t.Fatalf("expected 2 relations (lib, api), got %d: %+v", len(rels), rels)
	}
	for _, r := range rels {
		if r.RelationType != relatedproject.LibraryConsumer || r.Source != relatedproject.SourceDeps {
			t.Fatalf("unexpected relation: %+v", r)
		}
		if r.FromProject != "proj-app" || r.ToProject == "proj-app" {
			t.Fatalf("relation must preserve distinct identities: %+v", r)
		}
	}
}

func TestDeriveSkipsSelfAndUnknown(t *testing.T) {
	rels := relatedproject.DeriveFromModules("p1", "mod/self",
		[]string{"mod/self", "mod/unknown"}, map[string]string{"mod/self": "p1"})
	if len(rels) != 0 {
		t.Fatalf("self-module and unknown deps must not create relations, got %+v", rels)
	}
}

func TestStoreAddAndQueryPreservesIdentity(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.Background()
	store := relatedproject.NewStore(conn)

	if err := store.Add(ctx, relatedproject.Relation{
		FromProject: "app", ToProject: "lib", RelationType: relatedproject.LibraryConsumer, Source: relatedproject.SourceDeps,
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	// Duplicate is ignored.
	if err := store.Add(ctx, relatedproject.Relation{
		FromProject: "app", ToProject: "lib", RelationType: relatedproject.LibraryConsumer,
	}); err != nil {
		t.Fatalf("Add dup: %v", err)
	}

	from, err := store.From(ctx, "app")
	if err != nil {
		t.Fatalf("From: %v", err)
	}
	if len(from) != 1 || from[0].ToProject != "lib" {
		t.Fatalf("cross-project edge must keep the target identity: %+v", from)
	}
	to, _ := store.To(ctx, "lib")
	if len(to) != 1 || to[0].FromProject != "app" {
		t.Fatalf("reverse lookup must keep the source identity: %+v", to)
	}
}

func TestStoreRejectsSelfEdge(t *testing.T) {
	conn := dbtest.New(t)
	store := relatedproject.NewStore(conn)
	if err := store.Add(context.Background(), relatedproject.Relation{FromProject: "p", ToProject: "p"}); err == nil {
		t.Fatal("a project must never relate to itself")
	}
}

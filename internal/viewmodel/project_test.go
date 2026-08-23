package viewmodel_test

import (
	"context"
	"testing"

	"github.com/kaiau00/aux-cli/internal/profile"
	"github.com/kaiau00/aux-cli/internal/project"
	"github.com/kaiau00/aux-cli/internal/relatedproject"
	"github.com/kaiau00/aux-cli/internal/viewmodel"
)

type fakeProjectReader struct{ res project.Resolution }

func (f fakeProjectReader) Resolve(context.Context, string) (project.Resolution, error) {
	return f.res, nil
}

type fakeProfileCompiler struct{ eff profile.Effective }

func (f fakeProfileCompiler) CompileEffective(context.Context, string, string, string, string, string) (profile.Effective, error) {
	return f.eff, nil
}

type fakeRelatedStore struct {
	from, to []relatedproject.Relation
}

func (f fakeRelatedStore) From(context.Context, string) ([]relatedproject.Relation, error) {
	return f.from, nil
}
func (f fakeRelatedStore) To(context.Context, string) ([]relatedproject.Relation, error) {
	return f.to, nil
}

func TestProjectBrainViewAssemblesIdentityProfileAndGraph(t *testing.T) {
	res := project.Resolution{
		Project:  project.Project{ID: "proj-1", CanonicalName: "widget", VCSType: "git"},
		Root:     project.Root{CanonicalPath: "/tmp/widget"},
		Revision: project.Revision{VCSRevision: "abc123", BranchName: "main"},
	}
	eff := profile.Effective{
		Entries: []profile.EffectiveEntry{
			{Type: profile.EntryLanguage, Key: "go"},
			{Type: profile.EntryLanguage, Key: "go", Conflict: true},
		},
		Manifest:      "# profile",
		TokenEstimate: 42,
	}
	stores := viewmodel.ProjectStores{
		Projects: fakeProjectReader{res: res},
		Profiles: fakeProfileCompiler{eff: eff},
		Related: fakeRelatedStore{
			from: []relatedproject.Relation{{FromProject: "proj-1", ToProject: "proj-lib", RelationType: relatedproject.LibraryConsumer, Source: relatedproject.SourceDeps}},
			to:   []relatedproject.Relation{{FromProject: "proj-app", ToProject: "proj-1", RelationType: relatedproject.ServiceClient, Source: relatedproject.SourceDeclared}},
		},
	}

	view, err := stores.ProjectBrainView(context.Background(), "/tmp/widget")
	if err != nil {
		t.Fatalf("ProjectBrainView: %v", err)
	}
	if view.ProjectID != "proj-1" || view.Name != "widget" || view.Branch != "main" {
		t.Fatalf("identity not projected: %+v", view)
	}
	if view.Profile.TokenEstimate != 42 || len(view.Profile.Conflicts) == 0 {
		t.Fatalf("profile summary wrong: %+v", view.Profile)
	}
	if len(view.DependsOn) != 1 || view.DependsOn[0].ProjectID != "proj-lib" {
		t.Fatalf("dependsOn wrong: %+v", view.DependsOn)
	}
	if len(view.DependedOnBy) != 1 || view.DependedOnBy[0].ProjectID != "proj-app" {
		t.Fatalf("dependedOnBy wrong: %+v", view.DependedOnBy)
	}
}

func TestResolveProjectID(t *testing.T) {
	stores := viewmodel.ProjectStores{
		Projects: fakeProjectReader{res: project.Resolution{Project: project.Project{ID: "proj-1"}}},
	}
	id, err := stores.ResolveProjectID(context.Background(), "/tmp/widget")
	if err != nil {
		t.Fatalf("ResolveProjectID: %v", err)
	}
	if id != "proj-1" {
		t.Fatalf("id = %q, want proj-1", id)
	}
}

func TestProjectBrainViewWithoutRelatedStoreLeavesGraphEmpty(t *testing.T) {
	stores := viewmodel.ProjectStores{
		Projects: fakeProjectReader{res: project.Resolution{Project: project.Project{ID: "proj-1"}}},
		Profiles: fakeProfileCompiler{},
	}
	view, err := stores.ProjectBrainView(context.Background(), "/tmp/x")
	if err != nil {
		t.Fatalf("ProjectBrainView: %v", err)
	}
	if len(view.DependsOn) != 0 || len(view.DependedOnBy) != 0 {
		t.Fatalf("expected empty graph without a related store: %+v", view)
	}
}

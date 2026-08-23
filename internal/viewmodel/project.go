package viewmodel

import (
	"context"

	"github.com/kaiau00/aux-cli/internal/profile"
	"github.com/kaiau00/aux-cli/internal/project"
	"github.com/kaiau00/aux-cli/internal/relatedproject"
)

// ProjectBrainVM is the project identity, compiled effective profile, and
// related-project graph — the dashboard's "Project Brain" view. Every field is read from durable state; nothing here is
// inferred.
type ProjectBrainVM struct {
	ProjectID    string             `json:"projectId"`
	Name         string             `json:"name"`
	Root         string             `json:"root"`
	VCSType      string             `json:"vcsType"`
	Revision     string             `json:"revision,omitempty"`
	Branch       string             `json:"branch,omitempty"`
	Dirty        bool               `json:"dirty"`
	Profile      ProfileSummaryVM   `json:"profile"`
	DependsOn    []RelatedProjectVM `json:"dependsOn"`
	DependedOnBy []RelatedProjectVM `json:"dependedOnBy"`
}

// ProfileSummaryVM is the compiled effective profile, summarized for display.
type ProfileSummaryVM struct {
	Entries       int      `json:"entries"`
	TokenEstimate int      `json:"tokenEstimate"`
	Conflicts     []string `json:"conflicts,omitempty"`
	Manifest      string   `json:"manifest"`
}

// RelatedProjectVM is one edge of the related-project graph, from the current
// project's perspective.
type RelatedProjectVM struct {
	ProjectID    string `json:"projectId"`
	RelationType string `json:"relationType"`
	Source       string `json:"source"`
}

// BuildProfileSummary projects an effective profile into its display summary.
func BuildProfileSummary(eff profile.Effective) ProfileSummaryVM {
	conflicts := eff.Conflicts()
	keys := make([]string, 0, len(conflicts))
	for _, c := range conflicts {
		keys = append(keys, c.Type+"/"+c.Key)
	}
	return ProfileSummaryVM{
		Entries:       len(eff.Entries),
		TokenEstimate: eff.TokenEstimate,
		Conflicts:     keys,
		Manifest:      eff.Manifest,
	}
}

// BuildRelatedProjects projects related-project edges into display rows.
func BuildRelatedProjects(rels []relatedproject.Relation, otherProjectFn func(relatedproject.Relation) string) []RelatedProjectVM {
	out := make([]RelatedProjectVM, 0, len(rels))
	for _, r := range rels {
		out = append(out, RelatedProjectVM{
			ProjectID:    otherProjectFn(r),
			RelationType: string(r.RelationType),
			Source:       string(r.Source),
		})
	}
	return out
}

// ProjectReader resolves the current working directory to a project identity.
type ProjectReader interface {
	Resolve(ctx context.Context, dir string) (project.Resolution, error)
}

// ProfileCompiler compiles a project's effective profile.
type ProfileCompiler interface {
	CompileEffective(ctx context.Context, projectID, revisionID, root, sourceRevision, taskMode string) (profile.Effective, error)
}

// RelatedProjectStore reads the related-project graph.
type RelatedProjectStore interface {
	From(ctx context.Context, projectID string) ([]relatedproject.Relation, error)
	To(ctx context.Context, projectID string) ([]relatedproject.Relation, error)
}

// ProjectStores bundles the read dependencies needed to assemble a
// ProjectBrainVM.
type ProjectStores struct {
	Projects ProjectReader
	Profiles ProfileCompiler
	Related  RelatedProjectStore
}

// ResolveProjectID resolves the project at workdir to its stable project id,
// without compiling its profile — a cheap lookup for callers (like the
// Memory/Impact/Optimization dashboard views) that only need the id.
func (s ProjectStores) ResolveProjectID(ctx context.Context, workdir string) (string, error) {
	res, err := s.Projects.Resolve(ctx, workdir)
	if err != nil {
		return "", err
	}
	return res.Project.ID, nil
}

// ProjectBrainView resolves the project at workdir and assembles its Project
// Brain view: identity, effective profile, and related-project graph.
func (s ProjectStores) ProjectBrainView(ctx context.Context, workdir string) (ProjectBrainVM, error) {
	res, err := s.Projects.Resolve(ctx, workdir)
	if err != nil {
		return ProjectBrainVM{}, err
	}
	eff, err := s.Profiles.CompileEffective(ctx, res.Project.ID, res.Revision.ID, res.Root.CanonicalPath, res.Revision.VCSRevision, "")
	if err != nil {
		return ProjectBrainVM{}, err
	}

	view := ProjectBrainVM{
		ProjectID: res.Project.ID,
		Name:      res.Project.CanonicalName,
		Root:      res.Root.CanonicalPath,
		VCSType:   res.Project.VCSType,
		Revision:  res.Revision.VCSRevision,
		Branch:    res.Revision.BranchName,
		Dirty:     res.Revision.DirtyTreeHash != "",
		Profile:   BuildProfileSummary(eff),
	}

	if s.Related != nil {
		if from, ferr := s.Related.From(ctx, res.Project.ID); ferr == nil {
			view.DependsOn = BuildRelatedProjects(from, func(r relatedproject.Relation) string { return r.ToProject })
		}
		if to, terr := s.Related.To(ctx, res.Project.ID); terr == nil {
			view.DependedOnBy = BuildRelatedProjects(to, func(r relatedproject.Relation) string { return r.FromProject })
		}
	}

	return view, nil
}

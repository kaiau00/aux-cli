// Package multirepo compiles one product task into repository-specific child
// specs. Each child is bound to its own project (and thus
// its own effective profile and working set); cross-repository acceptance
// criteria live on the parent, including interface compatibility at integration
// boundaries. Compilation is deterministic so a coordinated multi-repo task is
// reproducible and its per-repository validation is traceable.
package multirepo

import "fmt"

// RepoTarget is one repository participating in a product task. Mode is the task
// mode inferred for that repository; ProjectID binds the child to its project so
// it resolves the correct profile and working set.
type RepoTarget struct {
	ProjectID string
	Name      string
	Mode      string
	Root      string
}

// ChildSpec is a repository-scoped child task compiled from the parent objective.
type ChildSpec struct {
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	Objective string `json:"objective"`
	Mode      string `json:"mode"`
}

// Criterion is a parent-level, cross-repository acceptance criterion.
type Criterion struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

// ParentPlan is the compiled product task: per-repository children plus
// cross-repository acceptance criteria that belong to the parent.
type ParentPlan struct {
	Objective          string      `json:"objective"`
	Children           []ChildSpec `json:"children"`
	AcceptanceCriteria []Criterion `json:"acceptanceCriteria"`
}

// Compile turns a product objective and a set of repository targets into a
// parent plan. Duplicate targets (same project) are collapsed so a child is
// never compiled twice for one repository.
func Compile(objective string, targets []RepoTarget) ParentPlan {
	plan := ParentPlan{Objective: objective}

	seen := map[string]bool{}
	for _, tgt := range targets {
		if tgt.ProjectID == "" || seen[tgt.ProjectID] {
			continue
		}
		seen[tgt.ProjectID] = true
		mode := tgt.Mode
		if mode == "" {
			mode = "implementation"
		}
		plan.Children = append(plan.Children, ChildSpec{
			ProjectID: tgt.ProjectID,
			Name:      tgt.Name,
			Objective: scopedObjective(objective, tgt.Name),
			Mode:      mode,
		})
	}

	// Every child must complete and validate.
	plan.AcceptanceCriteria = append(plan.AcceptanceCriteria, Criterion{
		ID:          "all-children-validated",
		Description: "All repository child tasks complete with passing validation.",
	})
	// Interface compatibility only exists when there is more than one boundary.
	if len(plan.Children) > 1 {
		plan.AcceptanceCriteria = append(plan.AcceptanceCriteria, Criterion{
			ID:          "interface-compatibility",
			Description: "Interface compatibility is validated at every cross-repository integration boundary.",
		})
	}
	return plan
}

func scopedObjective(objective, repo string) string {
	if repo == "" {
		return objective
	}
	return fmt.Sprintf("%s (in %s)", objective, repo)
}

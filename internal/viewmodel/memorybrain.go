package viewmodel

import (
	"context"

	"github.com/kaiau00/aux-cli/internal/memory"
	"github.com/kaiau00/aux-cli/internal/skill"
)

// MemoryVM is one memory record for the dashboard's Memory view.
type MemoryVM struct {
	Type       string  `json:"type"`
	Scope      string  `json:"scope,omitempty"`
	StableKey  string  `json:"stableKey"`
	State      string  `json:"state"`
	Confidence float64 `json:"confidence"`
	UpdatedAt  int64   `json:"updatedAt"`
}

// SkillVM is one skill for the dashboard's Memory view.
type SkillVM struct {
	Name      string `json:"name"`
	Scope     string `json:"scope,omitempty"`
	State     string `json:"state"`
	UpdatedAt int64  `json:"updatedAt"`
}

// MemoryBrainVM is the project's active/candidate memory and skills:
// what Aux has learned about this project.
type MemoryBrainVM struct {
	Active     []MemoryVM `json:"active"`
	Candidates []MemoryVM `json:"candidates"`
	Stale      []MemoryVM `json:"stale"`
	Skills     []SkillVM  `json:"skills"`
}

// BuildMemoryVM projects a memory record for display.
func BuildMemoryVM(m memory.Memory) MemoryVM {
	return MemoryVM{
		Type:       string(m.Type),
		Scope:      m.Scope,
		StableKey:  m.StableKey,
		State:      string(m.State),
		Confidence: m.Confidence,
		UpdatedAt:  m.UpdatedAt,
	}
}

// BuildSkillVM projects a skill record for display.
func BuildSkillVM(sk skill.Skill) SkillVM {
	return SkillVM{Name: sk.Name, Scope: sk.Scope, State: string(sk.State), UpdatedAt: sk.UpdatedAt}
}

// MemoryReader lists a project's memories by lifecycle state.
type MemoryReader interface {
	ListByState(ctx context.Context, projectID string, state memory.State) ([]memory.Memory, error)
}

// SkillReader lists skills.
type SkillReader interface {
	Active(ctx context.Context) ([]skill.Skill, error)
	Candidates(ctx context.Context) ([]skill.Skill, error)
}

// MemoryStores bundles the read dependencies needed to assemble a
// MemoryBrainVM.
type MemoryStores struct {
	Memories MemoryReader
	Skills   SkillReader
}

// MemoryBrainView assembles a project's memory + skill state.
func (s MemoryStores) MemoryBrainView(ctx context.Context, projectID string) (MemoryBrainVM, error) {
	var view MemoryBrainVM
	if s.Memories != nil {
		if active, err := s.Memories.ListByState(ctx, projectID, memory.StateActive); err == nil {
			for _, m := range active {
				view.Active = append(view.Active, BuildMemoryVM(m))
			}
		}
		if candidates, err := s.Memories.ListByState(ctx, projectID, memory.StateCandidate); err == nil {
			for _, m := range candidates {
				view.Candidates = append(view.Candidates, BuildMemoryVM(m))
			}
		}
		if stale, err := s.Memories.ListByState(ctx, projectID, memory.StateStale); err == nil {
			for _, m := range stale {
				view.Stale = append(view.Stale, BuildMemoryVM(m))
			}
		}
	}
	if s.Skills != nil {
		if active, err := s.Skills.Active(ctx); err == nil {
			for _, sk := range active {
				view.Skills = append(view.Skills, BuildSkillVM(sk))
			}
		}
		if candidates, err := s.Skills.Candidates(ctx); err == nil {
			for _, sk := range candidates {
				view.Skills = append(view.Skills, BuildSkillVM(sk))
			}
		}
	}
	return view, nil
}

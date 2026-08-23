package skill

import "strings"

// AgentSkillManifest is the canonical Agent Skills interchange shape used at
// import/export boundaries. Aux-specific metadata
// (evaluation state, source trajectories) is kept separately in the store.
type AgentSkillManifest struct {
	Name        string   `json:"name" yaml:"name"`
	Description string   `json:"description" yaml:"description"`
	Triggers    []string `json:"triggers,omitempty" yaml:"triggers,omitempty"`
	Steps       []string `json:"steps,omitempty" yaml:"steps,omitempty"`
}

// ToAgentSkill exports Content to the canonical interchange shape.
func ToAgentSkill(c Content) AgentSkillManifest {
	m := AgentSkillManifest{Name: c.Name, Description: c.Purpose, Triggers: c.Triggers}
	for _, s := range c.Procedure {
		title := strings.TrimSpace(s.Title)
		if title != "" {
			m.Steps = append(m.Steps, title)
		}
	}
	return m
}

// FromAgentSkill imports the canonical interchange shape to Content.
func FromAgentSkill(m AgentSkillManifest) Content {
	c := Content{Name: m.Name, Purpose: m.Description, Triggers: m.Triggers}
	for _, s := range m.Steps {
		c.Procedure = append(c.Procedure, Step{Title: s})
	}
	return c
}

package profile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// LayerInput is one precedence layer feeding the effective-profile merge.
type LayerInput struct {
	OwnerType  string
	Precedence int
	VersionID  string
	Entries    []Entry
}

// Provenance records a lower-precedence entry that was overridden.
type Provenance struct {
	OwnerType  string `json:"ownerType"`
	SourceType string `json:"sourceType"`
	SourceRef  string `json:"sourceRef"`
	ValueJSON  string `json:"valueJson"`
}

// EffectiveEntry is a merged entry with its winning source and any overridden
// lower-precedence contributions.
type EffectiveEntry struct {
	Type          string       `json:"type"`
	Key           string       `json:"key"`
	ValueJSON     string       `json:"valueJson"`
	OwnerType     string       `json:"ownerType"`
	SourceType    string       `json:"sourceType"`
	SourceRef     string       `json:"sourceRef"`
	Confidence    float64      `json:"confidence"`
	TokenEstimate int          `json:"tokenEstimate"`
	Overrides     []Provenance `json:"overrides,omitempty"`
	// Conflict is true when a lower layer set the same key to a different value.
	Conflict bool `json:"conflict,omitempty"`
}

// Effective is the merged, layered profile for a project revision + task mode.
type Effective struct {
	ProjectID      string           `json:"projectId"`
	RevisionID     string           `json:"revisionId"`
	TaskMode       string           `json:"taskMode"`
	VersionSetHash string           `json:"versionSetHash"`
	Entries        []EffectiveEntry `json:"entries"`
	Manifest       string           `json:"manifest"`
	TokenEstimate  int              `json:"tokenEstimate"`
}

// Compile merges layers into an effective profile. Higher precedence wins per
// (type,key); overridden lower entries are retained as provenance and flagged as
// conflicts when their value differed. List-like knowledge is modeled as
// distinct keys, so union happens naturally. Deterministic for identical inputs.
func Compile(projectID, revisionID, taskMode string, layers []LayerInput) Effective {
	sorted := append([]LayerInput(nil), layers...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Precedence < sorted[j].Precedence })

	// Winner map keyed by type\x1fkey; iterate low→high precedence so the last
	// writer (highest precedence) wins and lower ones become provenance.
	type winner struct {
		entry     EffectiveEntry
		overrides []Provenance
	}
	winners := map[string]*winner{}
	var order []string

	for _, layer := range sorted {
		for _, e := range layer.Entries {
			k := e.Type + "\x1f" + e.Key
			cand := EffectiveEntry{
				Type: e.Type, Key: e.Key, ValueJSON: e.ValueJSON,
				OwnerType: layer.OwnerType, SourceType: e.SourceType, SourceRef: e.SourceRef,
				Confidence: e.Confidence, TokenEstimate: e.TokenEstimate,
			}
			if existing, ok := winners[k]; ok {
				// Existing is lower precedence; it becomes provenance of the new winner.
				prov := Provenance{
					OwnerType: existing.entry.OwnerType, SourceType: existing.entry.SourceType,
					SourceRef: existing.entry.SourceRef, ValueJSON: existing.entry.ValueJSON,
				}
				overrides := append(existing.overrides, prov)
				conflict := existing.entry.ValueJSON != cand.ValueJSON
				cand.Overrides = overrides
				cand.Conflict = conflict || existing.entry.Conflict
				winners[k] = &winner{entry: cand, overrides: overrides}
			} else {
				winners[k] = &winner{entry: cand}
				order = append(order, k)
			}
		}
	}

	sort.Strings(order)
	entries := make([]EffectiveEntry, 0, len(order))
	for _, k := range order {
		entries = append(entries, winners[k].entry)
	}

	manifest := renderManifest(entries)
	return Effective{
		ProjectID:      projectID,
		RevisionID:     revisionID,
		TaskMode:       taskMode,
		VersionSetHash: versionSetHash(sorted),
		Entries:        entries,
		Manifest:       manifest,
		TokenEstimate:  estimateTokens(manifest),
	}
}

func versionSetHash(layers []LayerInput) string {
	h := sha256.New()
	for _, l := range layers {
		fmt.Fprintf(h, "%s:%s\n", l.OwnerType, l.VersionID)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// renderManifest produces a compact, model-facing summary. It deliberately does
// not paste full instruction bodies (only references), keeping the manifest much
// smaller than the raw imported sources.
func renderManifest(entries []EffectiveEntry) string {
	var langs, commands, workspace, instructions, other []string
	for _, e := range entries {
		switch e.Type {
		case EntryLanguage, EntryFramework:
			langs = append(langs, e.Key)
		case EntryValidationCommand, EntryBuildCommand:
			commands = append(commands, fmt.Sprintf("  %s: %s", e.Key, commandOf(e.ValueJSON)))
		case EntryWorkspace, EntryArchitecture:
			workspace = append(workspace, "  "+e.Key)
		case EntryInstruction:
			instructions = append(instructions, fmt.Sprintf("  %s: %s", e.Key, firstLine(instructionExcerpt(e.ValueJSON))))
		default:
			other = append(other, fmt.Sprintf("  %s/%s", e.Type, e.Key))
		}
	}
	var b strings.Builder
	b.WriteString("# Project profile\n")
	if len(langs) > 0 {
		b.WriteString("Languages: " + strings.Join(langs, ", ") + "\n")
	}
	if len(commands) > 0 {
		b.WriteString("Commands:\n" + strings.Join(commands, "\n") + "\n")
	}
	if len(workspace) > 0 {
		b.WriteString("Workspace:\n" + strings.Join(workspace, "\n") + "\n")
	}
	if len(instructions) > 0 {
		b.WriteString("Instructions:\n" + strings.Join(instructions, "\n") + "\n")
	}
	if len(other) > 0 {
		b.WriteString("Other:\n" + strings.Join(other, "\n") + "\n")
	}
	return b.String()
}

func commandOf(valueJSON string) string {
	var v struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal([]byte(valueJSON), &v)
	if v.Command == "" {
		return valueJSON
	}
	return v.Command
}

func instructionExcerpt(valueJSON string) string {
	var v struct {
		Excerpt string `json:"excerpt"`
	}
	_ = json.Unmarshal([]byte(valueJSON), &v)
	return v.Excerpt
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

// Conflicts returns only the effective entries that overrode a differing value.
func (e Effective) Conflicts() []EffectiveEntry {
	var out []EffectiveEntry
	for _, entry := range e.Entries {
		if entry.Conflict {
			out = append(out, entry)
		}
	}
	return out
}

// DiffEffective compares two effective profiles by (type,key), returning the
// keys added, removed, and changed (value differs) between a and b.
func DiffEffective(a, b Effective) (added, removed, changed []string) {
	index := func(e Effective) map[string]string {
		m := map[string]string{}
		for _, x := range e.Entries {
			m[x.Type+"/"+x.Key] = x.ValueJSON
		}
		return m
	}
	am, bm := index(a), index(b)
	for k, bv := range bm {
		av, ok := am[k]
		if !ok {
			added = append(added, k)
		} else if av != bv {
			changed = append(changed, k)
		}
	}
	for k := range am {
		if _, ok := bm[k]; !ok {
			removed = append(removed, k)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	sort.Strings(changed)
	return added, removed, changed
}

// ValidationCommand is a runnable command discovered for the project, with the
// entry key it came from so a caller can report which one ran.
type ValidationCommand struct {
	Key     string
	Command string
	Type    string
}

// ValidationCommands returns the effective profile's test and build commands.
// These are the concrete, project-specific ways to check whether work is
// actually done, extracted here so validation does not
// need to know how profile entries are encoded.
func (e Effective) ValidationCommands() []ValidationCommand {
	var out []ValidationCommand
	for _, entry := range e.Entries {
		switch entry.Type {
		case EntryValidationCommand:
			out = append(out, ValidationCommand{Key: entry.Key, Command: commandOf(entry.ValueJSON), Type: "test"})
		case EntryBuildCommand:
			out = append(out, ValidationCommand{Key: entry.Key, Command: commandOf(entry.ValueJSON), Type: "build"})
		}
	}
	return out
}

// Package bundle exports and imports shareable skill bundles, for sharing
// across projects or an organization. Bundles are content-addressed for
// integrity, and the import path is deliberately safe: everything comes in as
// a candidate, never active, so imported skills must earn local evaluation
// evidence before they can become defaults (the evaluation gate is never
// bypassed by import).
package bundle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kaiau00/aux-cli/internal/skill"
)

// FormatVersion is the bundle wire-format version. Version 1 also carried
// governor policies, which were removed; its hash covers them, so it is
// refused rather than misreported as tampered.
const FormatVersion = 2

// Bundle is a portable, content-addressed set of skills.
type Bundle struct {
	FormatVersion int          `json:"formatVersion"`
	CreatedAt     int64        `json:"createdAt"`
	Skills        []SkillEntry `json:"skills"`
	Hash          string       `json:"hash"`
}

// SkillEntry is one exported skill's portable content.
type SkillEntry struct {
	OwnerType string        `json:"ownerType"`
	Content   skill.Content `json:"content"`
}

// SkillReader reads active skills and their latest content for export.
type SkillReader interface {
	ListByState(ctx context.Context, state skill.State) ([]skill.Skill, error)
	LatestVersion(ctx context.Context, skillID string) (skill.Version, bool, error)
}

// SkillWriter imports a skill as a candidate.
type SkillWriter interface {
	Candidate(ctx context.Context, ownerType, ownerID string, content skill.Content, sourceType string, sourceIDs []string) (skill.Skill, skill.Version, error)
}

// Export collects the active skills into a content-addressed bundle.
func Export(ctx context.Context, skills SkillReader) (Bundle, error) {
	b := Bundle{FormatVersion: FormatVersion, CreatedAt: time.Now().UnixMilli()}

	sks, err := skills.ListByState(ctx, skill.StateActive)
	if err != nil {
		return Bundle{}, err
	}
	for _, sk := range sks {
		ver, ok, err := skills.LatestVersion(ctx, sk.ID)
		if err != nil {
			return Bundle{}, err
		}
		if !ok {
			continue
		}
		b.Skills = append(b.Skills, SkillEntry{OwnerType: sk.OwnerType, Content: ver.Content})
	}

	b.Hash = b.computeHash()
	return b, nil
}

// ImportResult reports what an import created.
type ImportResult struct {
	SkillsImported int
}

// Import verifies the bundle's integrity and creates every skill as a
// CANDIDATE — never active. Imported skills must be evaluated locally before
// promotion.
func Import(ctx context.Context, b Bundle, skills SkillWriter) (ImportResult, error) {
	if err := b.Verify(); err != nil {
		return ImportResult{}, err
	}
	var res ImportResult
	for _, e := range b.Skills {
		owner := e.OwnerType
		if owner == "" {
			owner = "user"
		}
		if _, _, err := skills.Candidate(ctx, owner, "", e.Content, "bundle", nil); err != nil {
			return res, fmt.Errorf("import skill %q: %w", e.Content.Name, err)
		}
		res.SkillsImported++
	}
	return res, nil
}

// Marshal serializes a bundle to JSON.
func Marshal(b Bundle) ([]byte, error) { return json.MarshalIndent(b, "", "  ") }

// Unmarshal parses a bundle from JSON.
func Unmarshal(data []byte) (Bundle, error) {
	var b Bundle
	if err := json.Unmarshal(data, &b); err != nil {
		return Bundle{}, err
	}
	return b, nil
}

// Verify checks the bundle's format version and content hash.
func (b Bundle) Verify() error {
	if b.FormatVersion != FormatVersion {
		return fmt.Errorf("unsupported bundle format version %d (want %d)", b.FormatVersion, FormatVersion)
	}
	if b.Hash != b.computeHash() {
		return fmt.Errorf("bundle integrity check failed: content hash mismatch")
	}
	return nil
}

// computeHash is the sha256 of the canonical content (skills), excluding the
// hash and timestamp so the same skills always address identically.
func (b Bundle) computeHash() string {
	canonical := struct {
		Skills []SkillEntry `json:"skills"`
	}{Skills: b.Skills}
	data, _ := json.Marshal(canonical)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

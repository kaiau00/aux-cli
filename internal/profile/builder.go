package profile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/kaiau00/aux-cli/internal/ids"
)

// Builder compiles a profile version for a single profile from a set of scanners.
type Builder struct {
	store    *Store
	scanners []Scanner
}

// NewBuilder returns a builder. Pass DefaultScanners() for production.
func NewBuilder(store *Store, scanners []Scanner) *Builder {
	return &Builder{store: store, scanners: scanners}
}

// Build runs the scanners over root and returns a content-addressed profile
// version. If a version with the same content already exists for the profile it
// is reused (Version.Reused == true) rather than re-inserted, satisfying the PR 5
// invariant that unchanged inputs reuse a profile version.
func (b *Builder) Build(ctx context.Context, profileID, root, sourceRevision string) (Version, []Entry, error) {
	drafts, inputFP, err := b.runScanners(ctx, root)
	if err != nil {
		return Version{}, nil, err
	}
	entries := draftsToEntries(drafts)
	contentHash := hashEntries(entries)

	if existing, ok, err := b.store.GetVersionByContentHash(ctx, profileID, contentHash); err != nil {
		return Version{}, nil, err
	} else if ok {
		existing.Reused = true
		stored, err := b.store.ListEntries(ctx, existing.ID)
		if err != nil {
			return Version{}, nil, err
		}
		return existing, stored, nil
	}

	version := Version{
		ID:              ids.New(),
		ProfileID:       profileID,
		SourceRevision:  sourceRevision,
		ContentHash:     contentHash,
		CompilerVersion: CompilerVersion,
		CreatedAt:       time.Now().UnixMilli(),
	}
	for i := range entries {
		entries[i].ID = ids.New()
		entries[i].ProfileVersionID = version.ID
	}
	if err := b.store.InsertVersion(ctx, version, entries); err != nil {
		return Version{}, nil, err
	}
	// InputFingerprint feeds the revision's profile_input_hash upstream.
	_ = inputFP
	return version, entries, nil
}

// InputFingerprint returns a hash of the raw inputs the scanners read, without
// building a version. Useful for cheap staleness checks.
func (b *Builder) InputFingerprint(ctx context.Context, root string) (string, error) {
	_, fp, err := b.runScanners(ctx, root)
	return fp, err
}

func (b *Builder) runScanners(ctx context.Context, root string) ([]EntryDraft, string, error) {
	var all []EntryDraft
	fingerprints := make([]string, 0, len(b.scanners))
	for _, sc := range b.scanners {
		res, err := sc.Scan(ctx, root)
		if err != nil {
			return nil, "", fmt.Errorf("scanner %s failed: %w", sc.Name(), err)
		}
		if res.Fingerprint != "" {
			fingerprints = append(fingerprints, sc.Name()+":"+res.Fingerprint)
		}
		all = append(all, res.Entries...)
	}
	sort.Strings(fingerprints)
	h := sha256.New()
	for _, fp := range fingerprints {
		h.Write([]byte(fp))
		h.Write([]byte{0})
	}
	return all, hex.EncodeToString(h.Sum(nil)), nil
}

func draftsToEntries(drafts []EntryDraft) []Entry {
	entries := make([]Entry, 0, len(drafts))
	for _, d := range drafts {
		valueJSON := "{}"
		if d.Value != nil {
			if b, err := json.Marshal(d.Value); err == nil {
				valueJSON = string(b)
			}
		}
		entries = append(entries, Entry{
			Type: d.Type, Key: d.Key, ValueJSON: valueJSON,
			SourceType: d.SourceType, SourceRef: d.SourceRef,
			Confidence: d.Confidence, TokenEstimate: d.TokenEstimate,
		})
	}
	// Deterministic order for stable content hashing.
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Type != entries[j].Type {
			return entries[i].Type < entries[j].Type
		}
		if entries[i].Key != entries[j].Key {
			return entries[i].Key < entries[j].Key
		}
		return entries[i].SourceRef < entries[j].SourceRef
	})
	return entries
}

// hashEntries produces a deterministic content hash independent of entry ids.
func hashEntries(entries []Entry) string {
	h := sha256.New()
	for _, e := range entries {
		fmt.Fprintf(h, "%s\x1f%s\x1f%s\x1f%s\x1f%.4f\x1e",
			e.Type, e.Key, e.ValueJSON, e.SourceRef, e.Confidence)
	}
	return hex.EncodeToString(h.Sum(nil))
}

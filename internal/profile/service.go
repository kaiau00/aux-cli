package profile

import (
	"context"
	"time"

	"github.com/kaiau00/aux-cli/internal/ids"
)

// Service compiles and reads project profiles.
type Service struct {
	store   *Store
	builder *Builder
}

// NewService returns a profile service.
func NewService(store *Store, builder *Builder) *Service {
	return &Service{store: store, builder: builder}
}

// CompileProject builds (or reuses) the project-layer profile version for a
// project root at a given source revision.
func (s *Service) CompileProject(ctx context.Context, projectID, root, sourceRevision string) (Version, []Entry, error) {
	now := time.Now().UnixMilli()
	profile, err := s.store.GetOrCreateProfile(ctx, Profile{
		ID:         ids.New(),
		OwnerType:  OwnerProject,
		OwnerID:    projectID,
		Name:       "project",
		Precedence: Precedence[OwnerProject],
		Enabled:    true,
		CreatedAt:  now,
		UpdatedAt:  now,
	})
	if err != nil {
		return Version{}, nil, err
	}
	return s.builder.Build(ctx, profile.ID, root, sourceRevision)
}

// CompileEffective compiles the project profile, merges it with the builtin
// defaults layer, and persists the resulting effective profile for a revision +
// task mode. The result is deterministic and deduplicated by version-set hash.
func (s *Service) CompileEffective(ctx context.Context, projectID, revisionID, root, sourceRevision, taskMode string) (Effective, error) {
	version, entries, err := s.CompileProject(ctx, projectID, root, sourceRevision)
	if err != nil {
		return Effective{}, err
	}
	layers := []LayerInput{
		builtinLayer(),
		{
			OwnerType:  OwnerProject,
			Precedence: Precedence[OwnerProject],
			VersionID:  version.ID,
			Entries:    entries,
		},
	}
	eff := Compile(projectID, revisionID, taskMode, layers)
	if _, err := s.store.SaveEffective(ctx, ids.New(), eff); err != nil {
		return Effective{}, err
	}
	return eff, nil
}

// builtinLayer is the lowest-precedence default layer. It contributes a small
// baseline of Aux conventions that any higher layer may override.
func builtinLayer() LayerInput {
	return LayerInput{
		OwnerType:  OwnerBuiltin,
		Precedence: Precedence[OwnerBuiltin],
		VersionID:  "builtin-" + CompilerVersion,
		Entries: []Entry{{
			Type: EntryConvention, Key: "validation.strategy",
			ValueJSON:     `{"strategy":"targeted-then-broad"}`,
			SourceType:    "builtin",
			SourceRef:     "aux-defaults",
			Confidence:    0.5,
			TokenEstimate: estimateTokens("targeted-then-broad"),
		}},
	}
}

// InputFingerprint returns the current profile-input fingerprint for a root.
func (s *Service) InputFingerprint(ctx context.Context, root string) (string, error) {
	return s.builder.InputFingerprint(ctx, root)
}

// Store exposes the underlying store for read-only callers.
func (s *Service) Store() *Store { return s.store }

package artifact

import (
	"context"
	"strings"
	"time"

	"github.com/kaiau00/aux-cli/internal/ids"
)

// OwnerRef identifies who a stored artifact belongs to.
type OwnerRef struct {
	Type     string
	ID       string
	Relation string
}

// Match is a search hit within an artifact.
type Match struct {
	Line    int    `json:"line"`
	Content string `json:"content"`
}

// Service stores and retrieves artifacts, deduplicating by content hash.
type Service struct {
	backend Backend
	store   *Store
}

// NewService returns an artifact service.
func NewService(backend Backend, store *Store) *Service {
	return &Service{backend: backend, store: store}
}

// Put stores data (deduplicated by content hash), records an ownership ref, and
// returns the artifact and whether it was reused.
func (s *Service) Put(ctx context.Context, data []byte, mediaType string, owner OwnerRef) (Artifact, bool, error) {
	hash := HashBytes(data)
	now := time.Now().UnixMilli()

	existing, found, err := s.store.GetByHash(ctx, hash)
	if err != nil {
		return Artifact{}, false, err
	}
	if found {
		_ = s.store.Touch(ctx, existing.ID, now)
		if err := s.addRef(ctx, existing.ID, owner, now); err != nil {
			return Artifact{}, false, err
		}
		return existing, true, nil
	}

	key, err := s.backend.Write(hash, data)
	if err != nil {
		return Artifact{}, false, err
	}
	a := Artifact{
		ID:             ids.New(),
		ContentHash:    hash,
		StorageBackend: "fs",
		StorageKey:     key,
		MediaType:      mediaType,
		ByteSize:       int64(len(data)),
		Compression:    "none",
		Sensitivity:    "normal",
		CreatedAt:      now,
		LastAccessedAt: now,
	}
	if err := s.store.Insert(ctx, a); err != nil {
		return Artifact{}, false, err
	}
	if err := s.addRef(ctx, a.ID, owner, now); err != nil {
		return Artifact{}, false, err
	}
	return a, false, nil
}

func (s *Service) addRef(ctx context.Context, artifactID string, owner OwnerRef, now int64) error {
	if owner.Type == "" {
		return nil
	}
	relation := owner.Relation
	if relation == "" {
		relation = "produced"
	}
	return s.store.AddRef(ctx, Ref{
		ID: ids.New(), ArtifactID: artifactID, OwnerType: owner.Type, OwnerID: owner.ID,
		Relation: relation, CreatedAt: now,
	})
}

// Get returns the full bytes of an artifact by id.
func (s *Service) Get(ctx context.Context, id string) ([]byte, error) {
	a, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	_ = s.store.Touch(ctx, id, time.Now().UnixMilli())
	return s.backend.Read(a.StorageKey)
}

// GetRange returns a byte range [offset, offset+length) of an artifact.
func (s *Service) GetRange(ctx context.Context, id string, offset, length int) ([]byte, error) {
	data, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(data) {
		return nil, nil
	}
	end := offset + length
	if length <= 0 || end > len(data) {
		end = len(data)
	}
	return data[offset:end], nil
}

// Search returns lines containing query (case-insensitive), up to maxHits.
func (s *Service) Search(ctx context.Context, id, query string, maxHits int) ([]Match, error) {
	data, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if maxHits <= 0 {
		maxHits = 50
	}
	needle := strings.ToLower(query)
	var matches []Match
	for i, line := range strings.Split(string(data), "\n") {
		if strings.Contains(strings.ToLower(line), needle) {
			matches = append(matches, Match{Line: i + 1, Content: line})
			if len(matches) >= maxHits {
				break
			}
		}
	}
	return matches, nil
}

// Store exposes the metadata store for read-only callers.
func (s *Service) Store() *Store { return s.store }

// GCReport lists unreferenced artifacts and their reclaimable bytes (dry run).
func (s *Service) GCReport(ctx context.Context) ([]Artifact, int64, error) {
	unref, err := s.store.Unreferenced(ctx)
	if err != nil {
		return nil, 0, err
	}
	var total int64
	for _, a := range unref {
		total += a.ByteSize
	}
	return unref, total, nil
}

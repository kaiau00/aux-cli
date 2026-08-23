package impact

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
)

// Service builds and queries the impact graph.
type Service struct {
	store   *Store
	indexer *Indexer
}

// NewService returns an impact service.
func NewService(store *Store) *Service {
	return &Service{store: store, indexer: NewIndexer(store)}
}

// Index performs a full graph build for a project root.
func (s *Service) Index(ctx context.Context, projectID, root, revision string) (int, error) {
	return s.indexer.IndexProject(ctx, projectID, root, revision)
}

// Reindex incrementally updates the graph for changed paths.
func (s *Service) Reindex(ctx context.Context, projectID, root, revision string, changedPaths []string) error {
	return s.indexer.Reindex(ctx, projectID, root, revision, changedPaths)
}

// Analyze returns the impact of a set of changed (repo-relative) paths. It never
// treats impact selection as the sole validation basis: when the graph is empty,
// stale, or a changed path is uncovered, it broadens validation. currentRevision is the revision the caller is analyzing against.
func (s *Service) Analyze(ctx context.Context, projectID, currentRevision string, changedPaths []string) (Result, error) {
	res := Result{ChangedPaths: changedPaths}

	nodeCount, err := s.store.CountNodes(ctx, projectID)
	if err != nil {
		return Result{}, err
	}
	graphEmpty := nodeCount == 0

	st, hasState, err := s.store.GetIndexState(ctx, projectID)
	if err != nil {
		return Result{}, err
	}
	stale := !hasState || (currentRevision != "" && st.SourceRevision != "" && st.SourceRevision != currentRevision)

	pkgSet := map[string]struct{}{}       // affected package import paths
	dependentSet := map[string]struct{}{} // importing file relpaths
	testSet := map[string]TestRec{}
	// affectedPkgs maps package node id -> (key, reason) for test collection.
	type pkgRef struct{ key, reason string }
	affectedPkgs := map[string]pkgRef{}
	uncovered := 0

	for _, rel := range changedPaths {
		rel = filepath.ToSlash(rel)
		if !strings.HasSuffix(rel, ".go") {
			uncovered++ // non-Go change: the AST indexer does not cover it
			continue
		}
		fileID, ok, err := s.store.NodeByKey(ctx, projectID, fileNodeType(rel), rel)
		if err != nil {
			return Result{}, err
		}
		if !ok {
			uncovered++
			continue
		}
		// The package that contains this file (contains edges point package->file).
		pkgEdges, err := s.store.EdgesTo(ctx, fileID, EdgeContains)
		if err != nil {
			return Result{}, err
		}
		for _, pe := range pkgEdges {
			_, pkgKey, err := s.store.NodeName(ctx, pe.FromNodeID)
			if err != nil {
				return Result{}, err
			}
			pkgSet[pkgKey] = struct{}{}
			affectedPkgs[pe.FromNodeID] = pkgRef{pkgKey, "changed package"}

			// Files that import this package are direct dependents; their own
			// packages are affected too.
			importers, err := s.store.EdgesTo(ctx, pe.FromNodeID, EdgeImports)
			if err != nil {
				return Result{}, err
			}
			for _, ie := range importers {
				_, depKey, err := s.store.NodeName(ctx, ie.FromNodeID)
				if err != nil {
					return Result{}, err
				}
				dependentSet[depKey] = struct{}{}
				depPkgEdges, err := s.store.EdgesTo(ctx, ie.FromNodeID, EdgeContains)
				if err != nil {
					return Result{}, err
				}
				for _, dpe := range depPkgEdges {
					_, depPkgKey, _ := s.store.NodeName(ctx, dpe.FromNodeID)
					pkgSet[depPkgKey] = struct{}{}
					if _, seen := affectedPkgs[dpe.FromNodeID]; !seen {
						affectedPkgs[dpe.FromNodeID] = pkgRef{depPkgKey, "imports the changed package"}
					}
				}
			}
		}
	}

	// Tests in every affected package (changed + dependent).
	for pkgID, ref := range affectedPkgs {
		s.collectTests(ctx, pkgID, ref.key, ref.reason, testSet)
	}

	res.AffectedPackages = sortedKeys(pkgSet)
	res.DirectDependents = sortedKeys(dependentSet)
	for _, tr := range testSet {
		res.RelatedTests = append(res.RelatedTests, tr)
	}
	sort.Slice(res.RelatedTests, func(i, j int) bool { return res.RelatedTests[i].Path < res.RelatedTests[j].Path })

	res.Uncertainty = uncertainty(graphEmpty, stale, uncovered, len(changedPaths))
	res.Risk = riskFrom(len(res.DirectDependents), len(res.AffectedPackages))
	res.BroadenValidation = graphEmpty || stale || uncovered > 0 || res.Uncertainty >= 0.5
	res.Recommended, res.Reason = recommend(res, graphEmpty, stale, uncovered)
	return res, nil
}

func (s *Service) collectTests(ctx context.Context, pkgID, pkgKey, reason string, out map[string]TestRec) {
	contained, err := s.store.EdgesFrom(ctx, pkgID, EdgeContains)
	if err != nil {
		return
	}
	for _, ce := range contained {
		nodeType, key, err := s.store.NodeName(ctx, ce.ToNodeID)
		if err != nil {
			continue
		}
		if nodeType == NodeTest {
			out[key] = TestRec{Path: key, Reason: reason + " (" + pkgKey + ")"}
		}
	}
}

func uncertainty(graphEmpty, stale bool, uncovered, total int) float64 {
	u := 0.0
	if graphEmpty {
		u += 0.6
	}
	if stale {
		u += 0.3
	}
	if total > 0 {
		u += 0.5 * float64(uncovered) / float64(total)
	}
	if u > 1 {
		u = 1
	}
	return u
}

func riskFrom(dependents, packages int) RiskLevel {
	switch {
	case dependents >= 8 || packages >= 4:
		return RiskHigh
	case dependents >= 2 || packages >= 2:
		return RiskMedium
	default:
		return RiskLow
	}
}

func recommend(res Result, graphEmpty, stale bool, uncovered int) ([]string, string) {
	if res.BroadenValidation {
		reason := "broad validation: "
		switch {
		case graphEmpty:
			reason += "graph not built"
		case stale:
			reason += "graph stale vs current revision"
		case uncovered > 0:
			reason += "change includes paths the graph does not cover"
		default:
			reason += "impact uncertainty above threshold"
		}
		return []string{"go build ./...", "go test ./..."}, reason
	}
	// Targeted validation: `go test` accepts full import paths, so run exactly the
	// affected packages.
	var cmds []string
	for _, pkg := range res.AffectedPackages {
		cmds = append(cmds, "go test "+pkg)
	}
	if len(cmds) == 0 {
		cmds = []string{"go test ./..."}
	}
	return cmds, "targeted validation of affected packages"
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

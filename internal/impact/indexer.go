package impact

import (
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aux-ai/aux-cli/internal/logging"
)

// Indexer builds the impact graph from deterministic Go source analysis
// (roadmapplan.md §8.5). Non-Go files are still tracked as file nodes so the
// analyzer can flag uncovered changes and broaden validation.
// indexStore is the slice of the graph store the indexer needs. It exists so a
// failing store can be substituted in tests: the writes below are the ones that
// keep the graph honest, and a fake is the only way to prove a failure in them
// is not swallowed.
type indexStore interface {
	SetIndexState(ctx context.Context, st IndexState) error
	NodeByKey(ctx context.Context, projectID, nodeType, stableKey string) (string, bool, error)
	DeleteNodeEdges(ctx context.Context, nodeID string) error
	UpsertNode(ctx context.Context, n Node) (string, error)
	UpsertEdge(ctx context.Context, e Edge) error
}

type Indexer struct {
	store indexStore
}

// NewIndexer returns a graph indexer.
func NewIndexer(store *Store) *Indexer { return &Indexer{store: store} }

type parsedFile struct {
	relPath   string
	pkgImport string // import path of the package this file belongs to
	isTest    bool
	imports   []string // imported package import paths
}

// IndexProject performs a full build of the graph for a project root. It is the
// repair path; normal refresh uses Reindex on changed paths.
func (ix *Indexer) IndexProject(ctx context.Context, projectID, root, revision string) (int, error) {
	// Best effort: the in-progress marker only changes what a concurrent reader
	// sees while the walk runs. The terminal write below is the one that has to
	// land, so a failure here is reported and not fatal.
	if err := ix.store.SetIndexState(ctx, IndexState{ProjectID: projectID, SourceRevision: revision, IndexerVersion: IndexerVersion, Status: "indexing", LastIndexedAt: time.Now().UnixMilli()}); err != nil {
		logging.Warn("failed to record index start", "project", projectID, "error", err)
	}

	modulePath := readModulePath(root)
	count := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			if skipDir(d.Name()) && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		pf := parseGoFile(root, path, modulePath)
		if pf == nil {
			return nil
		}
		if err := ix.indexFile(ctx, projectID, revision, modulePath, *pf); err != nil {
			return err
		}
		count++
		return nil
	})
	status := "indexed"
	if err != nil {
		status = "error"
	}
	// Returning success while this write fails would tell the caller the graph
	// is current at this revision when the recorded state says otherwise. A walk
	// error is the more important one, so it keeps precedence.
	if stateErr := ix.store.SetIndexState(ctx, IndexState{ProjectID: projectID, SourceRevision: revision, IndexerVersion: IndexerVersion, Status: status, LastIndexedAt: time.Now().UnixMilli()}); stateErr != nil && err == nil {
		return count, fmt.Errorf("indexed %d files but failed to record index state: %w", count, stateErr)
	}
	return count, err
}

// Reindex updates only the partitions for changed paths (roadmapplan.md §8.5:
// "update affected partitions based on changed paths").
func (ix *Indexer) Reindex(ctx context.Context, projectID, root, revision string, changedPaths []string) error {
	modulePath := readModulePath(root)
	for _, rel := range changedPaths {
		if !strings.HasSuffix(rel, ".go") {
			continue
		}
		abs := filepath.Join(root, rel)
		// Reset this file's edges before re-adding. Neither failure here can be
		// skipped past: indexFile re-adds this file's edges below, so old edges
		// left in place accumulate rather than being replaced, and impact
		// analysis goes on reporting dependencies that no longer exist.
		id, ok, err := ix.store.NodeByKey(ctx, projectID, fileNodeType(rel), rel)
		if err != nil {
			return fmt.Errorf("failed to look up the graph node for %s: %w", rel, err)
		}
		if ok {
			if err := ix.store.DeleteNodeEdges(ctx, id); err != nil {
				return fmt.Errorf("failed to clear stale edges for %s: %w", rel, err)
			}
		}
		if _, err := os.Stat(abs); err != nil {
			continue // deleted; leave the (now edgeless) node for history
		}
		pf := parseGoFile(root, abs, modulePath)
		if pf == nil {
			continue
		}
		if err := ix.indexFile(ctx, projectID, revision, modulePath, *pf); err != nil {
			return err
		}
	}
	if err := ix.store.SetIndexState(ctx, IndexState{ProjectID: projectID, SourceRevision: revision, IndexerVersion: IndexerVersion, Status: "indexed", LastIndexedAt: time.Now().UnixMilli()}); err != nil {
		return fmt.Errorf("reindexed but failed to record index state: %w", err)
	}
	return nil
}

func (ix *Indexer) indexFile(ctx context.Context, projectID, revision, modulePath string, pf parsedFile) error {
	fileType := NodeFile
	if pf.isTest {
		fileType = NodeTest
	}
	fileID, err := ix.store.UpsertNode(ctx, Node{ProjectID: projectID, Type: fileType, StableKey: pf.relPath, DisplayName: filepath.Base(pf.relPath), SourceRevision: revision})
	if err != nil {
		return err
	}
	if pf.pkgImport != "" {
		pkgID, err := ix.store.UpsertNode(ctx, Node{ProjectID: projectID, Type: NodePackage, StableKey: pf.pkgImport, DisplayName: pf.pkgImport, SourceRevision: revision})
		if err != nil {
			return err
		}
		// package contains file.
		if err := ix.store.UpsertEdge(ctx, Edge{ProjectID: projectID, FromNodeID: pkgID, ToNodeID: fileID, Type: EdgeContains, Weight: 1, Source: SourceAST, SourceRevision: revision}); err != nil {
			return err
		}
		if pf.isTest {
			if err := ix.store.UpsertEdge(ctx, Edge{ProjectID: projectID, FromNodeID: fileID, ToNodeID: pkgID, Type: EdgeTests, Weight: 1, Source: SourceAST, SourceRevision: revision}); err != nil {
				return err
			}
		}
	}
	// imports edges (intra-module only, so impact stays focused on this repo).
	for _, imp := range pf.imports {
		if modulePath == "" || !strings.HasPrefix(imp, modulePath) {
			continue
		}
		impID, err := ix.store.UpsertNode(ctx, Node{ProjectID: projectID, Type: NodePackage, StableKey: imp, DisplayName: imp, SourceRevision: revision})
		if err != nil {
			return err
		}
		if err := ix.store.UpsertEdge(ctx, Edge{ProjectID: projectID, FromNodeID: fileID, ToNodeID: impID, Type: EdgeImports, Weight: 1, Source: SourceAST, SourceRevision: revision}); err != nil {
			return err
		}
	}
	return nil
}

func parseGoFile(root, abs, modulePath string) *parsedFile {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, abs, nil, parser.ImportsOnly)
	if err != nil {
		return nil
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return nil
	}
	rel = filepath.ToSlash(rel)
	pf := &parsedFile{relPath: rel, isTest: strings.HasSuffix(abs, "_test.go")}
	dir := filepath.ToSlash(filepath.Dir(rel))
	if modulePath != "" {
		if dir == "." {
			pf.pkgImport = modulePath
		} else {
			pf.pkgImport = modulePath + "/" + dir
		}
	}
	for _, spec := range f.Imports {
		if spec.Path != nil {
			pf.imports = append(pf.imports, strings.Trim(spec.Path.Value, `"`))
		}
	}
	return pf
}

func fileNodeType(rel string) string {
	if strings.HasSuffix(rel, "_test.go") {
		return NodeTest
	}
	return NodeFile
}

func skipDir(name string) bool {
	switch name {
	case "vendor", "node_modules", ".git", "dist", "bin", ".aux":
		return true
	}
	return strings.HasPrefix(name, ".") && name != "."
}

func readModulePath(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(after)
		}
	}
	return ""
}

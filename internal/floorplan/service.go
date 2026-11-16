package floorplan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"
)

// Service builds a repository's map and keeps it until something on disk moves.
//
// The map is built on first use, not at startup. Scanning and analysing this
// repository costs about 200ms, which is cheap for a page somebody asked to
// look at and expensive for a command that never opens one. Nothing that does
// not want the map pays for it.
//
// Rebuilding is decided by listing the files and comparing each one's size and
// modification time against the previous scan. Measured on this repository:
//
//	cold build (read and parse 618 files)   ~235ms
//	warm check, nothing changed              ~28ms
//	  of which `git ls-files`                ~27ms
//	  of which statting 618 files           ~1.2ms
//
// So the check is one git subprocess plus almost nothing, and serving an
// unchanged map costs about an eighth of building one. The subprocess is the
// floor here, not the statting; listing from directory mtimes instead would
// remove most of the remaining 28ms, and is not worth the complexity until
// something calls this far more often than a page load does.
//
// Measurement also chose this over keying the cache on git state, which was the
// original plan: reading head, branch, remote and the dirty set costs four
// subprocesses and ~84ms, three times the price of the check above, and says
// nothing at all about a directory that is not a repository.
type Service struct {
	root string

	mu   sync.Mutex
	snap *snapshot
	doc  *Graph

	builds   int // completed builds, for tests and for status
	analyzed int // files read and parsed, for tests and for status
}

// snapshot is one scan's results, held so the next scan can reuse the parts
// that did not change.
type snapshot struct {
	order []string               // scanned paths, in scan order
	files map[string]ScannedFile // path -> measurement
	facts map[string]FileFacts   // path -> what its contents say
	marks map[string]string      // path -> size and modification time
}

// NewService returns a service that maps root. Nothing is read until the map
// is first asked for.
func NewService(root string) *Service { return &Service{root: root} }

// Builds reports how many times the map has been built. It exists so a caller
// can show that a request was served from the cache, and so tests can prove it.
func (s *Service) Builds() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.builds
}

// Analyzed reports how many files have been read and parsed across every build,
// which is the cost the cache exists to avoid.
func (s *Service) Analyzed() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.analyzed
}

// Map returns the repository's map, building it if it has not been built or if
// any file has changed since it was.
//
// The returned document must be treated as read-only. It shares its slices and
// maps with the cached copy, so writing through it would corrupt what the next
// caller is served. Callers serialise it or read it; none needs to change it.
func (s *Service) Map(ctx context.Context) (Graph, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mapLocked(ctx, false)
}

// Rebuild discards the cache and builds the map again, re-reading every file.
//
// This is the answer to the one case stat-based invalidation misses: a file
// rewritten to the same length within a single filesystem timestamp tick.
func (s *Service) Rebuild(ctx context.Context) (Graph, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snap, s.doc = nil, nil
	return s.mapLocked(ctx, true)
}

// mapLocked does the work of Map with s.mu already held.
func (s *Service) mapLocked(ctx context.Context, force bool) (Graph, error) {
	rels, err := listFiles(ctx, s.root)
	if err != nil {
		return Graph{}, err
	}
	marks := s.markAll(rels)

	if !force && s.doc != nil && s.snap != nil && sameMarks(s.snap.marks, marks) {
		return *s.doc, nil
	}

	scanAt := time.Now()
	next, reused := s.rescan(rels, marks)
	s.analyzed += len(next.order) - reused

	doc, err := s.build(ctx, next, scanAt)
	if err != nil {
		return Graph{}, err
	}
	s.snap, s.doc = next, &doc
	s.builds++
	return doc, nil
}

// markAll stats every candidate path. Paths in a skipped directory are left
// out here rather than later, so the marks describe exactly the set the scan
// will cover and a change check cannot disagree with the scan about membership.
func (s *Service) markAll(rels []string) map[string]string {
	marks := make(map[string]string, len(rels))
	for _, rel := range rels {
		if inSkippedDir(rel) {
			continue
		}
		info, err := os.Stat(filepath.Join(s.root, filepath.FromSlash(rel)))
		if err != nil || info.IsDir() {
			continue
		}
		marks[rel] = mark(info)
	}
	return marks
}

// rescan produces the next snapshot, carrying over the measurement and analysis
// of every file whose mark is unchanged and re-reading the rest. It reports how
// many were reused.
func (s *Service) rescan(rels []string, marks map[string]string) (*snapshot, int) {
	next := &snapshot{
		order: make([]string, 0, len(marks)),
		files: make(map[string]ScannedFile, len(marks)),
		facts: make(map[string]FileFacts, len(marks)),
		marks: marks,
	}
	reused := 0
	for _, rel := range rels {
		m, ok := marks[rel]
		if !ok {
			continue // skipped, unreadable, or a directory
		}
		if s.snap != nil && s.snap.marks[rel] == m {
			if f, had := s.snap.files[rel]; had {
				next.order = append(next.order, rel)
				next.files[rel] = f
				if fx, hadFacts := s.snap.facts[rel]; hadFacts {
					next.facts[rel] = fx
				}
				reused++
				continue
			}
		}
		f, data, ok := measure(filepath.Join(s.root, filepath.FromSlash(rel)), rel)
		if !ok {
			delete(marks, rel) // it went away between the stat and the read
			continue
		}
		next.order = append(next.order, rel)
		next.files[rel] = f
		if data != nil {
			next.facts[rel] = analyzeFile(rel, f.Lang, data)
		}
	}
	return next, reused
}

// build assembles the document from a snapshot and the authored layer.
func (s *Service) build(ctx context.Context, snap *snapshot, scanAt time.Time) (Graph, error) {
	files := make([]ScannedFile, 0, len(snap.order))
	for _, rel := range snap.order {
		files = append(files, snap.files[rel])
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	facts := Facts(snap.facts)

	arch, err := LoadArchitecture(s.root)
	if err != nil && !errors.Is(err, ErrNoMap) {
		return Graph{}, err // a damaged architecture is not an absent one
	}
	descriptions, err := LoadDescriptions(s.root)
	if err != nil && !errors.Is(err, ErrNoMap) {
		return Graph{}, err
	}

	routes := GoRoutes(files, facts)
	links := BuildLinks(s.root, files, facts)
	links = append(links, ExternalLinks(files, facts, arch)...)
	links = append(links, HostLinks(files, facts, arch)...)
	links = append(links, RouteLinks(files, facts, routes)...)

	var repos []Repo
	if st, err := RepoState(ctx, s.root); err == nil && st.Head != "" {
		repos = []Repo{st.Record(s.root)}
	}

	return Build(BuildInput{
		Root:         s.root,
		Arch:         arch,
		Files:        files,
		Facts:        facts,
		Links:        links,
		Routes:       routes,
		Repos:        repos,
		ScanAt:       scanAt,
		Now:          time.Now(),
		Descriptions: descriptions,
	})
}

// mark identifies what is at a path right now: its size and modification time.
//
// This is not a content hash. Hashing every file to decide whether to re-read
// every file would cost what re-reading costs, which is the thing being
// avoided. Size and mtime is what build systems use, and it is wrong in the
// same narrow case -- a file rewritten to the same length within one timestamp
// tick -- which Rebuild exists to resolve.
func mark(info os.FileInfo) string {
	return info.ModTime().UTC().Format(time.RFC3339Nano) + ":" + strconv.FormatInt(info.Size(), 10)
}

// sameMarks reports whether two mark sets describe the same files in the same
// state. A file that appeared, vanished or changed makes them differ.
func sameMarks(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

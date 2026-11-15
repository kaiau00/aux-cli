package floorplan

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// hashLen is how much of the content digest is kept. Twelve hex characters is
// enough to notice a file changed, which is all the hash is for, and it keeps
// the served document small across several hundred files.
const hashLen = 12

// ScannedFile is one file as measured on disk. Nothing here is authored: every
// field is read from the file itself, so a scan can always be repeated and
// compared.
type ScannedFile struct {
	Path     string // repo-relative, slash-separated
	Category string
	Lang     string
	LOC      int
	Bytes    int64
	Hash     string
}

// skipDirs are never part of a project's own code: dependencies, package
// caches, build output and tool state. node_modules or vendor alone would
// outweigh everything the map exists to show.
//
// The list is explicit rather than a rule about dot-directories, because
// .github holds a project's CI workflows and .vscode its editor settings --
// both of which are the project's own configuration, and both of which a
// blanket dot-directory rule would silently drop from the map.
var skipDirs = map[string]bool{
	// dependencies and package caches
	"node_modules": true, "vendor": true, ".pnpm-store": true, ".yarn": true,
	"site-packages": true, ".venv": true, "venv": true, "__pycache__": true,
	// tool and framework caches
	".git": true, ".tox": true, ".mypy_cache": true, ".pytest_cache": true,
	".ruff_cache": true, ".next": true, ".nuxt": true, ".svelte-kit": true,
	".turbo": true, ".parcel-cache": true, ".gradle": true, ".idea": true,
	// build output and local state
	"dist": true, "build": true, "bin": true, "target": true, ".aux": true,
}

// gitListTimeout bounds the one git call the scan makes. A repository large
// enough to exceed this still gets a map, from the directory walk below.
const gitListTimeout = 20 * time.Second

// Scan measures every source file under root.
//
// Which files count is decided by git when root is a repository: the working
// tree minus whatever .gitignore excludes, which is already the project's own
// statement about what is and is not part of it. Untracked files are included,
// because a file written a minute ago and not yet staged is precisely the one
// someone is working on. Outside a repository the scan walks the tree and
// falls back to a fixed list of directories to skip.
//
// An entry it cannot read is left out rather than failing the scan: a map of
// the files that could be read is worth far more than no map, and a file
// missing from the counts is visible, whereas an aborted scan leaves the
// dashboard with nothing to show.
func Scan(ctx context.Context, root string) ([]ScannedFile, error) {
	rels, err := listFiles(ctx, root)
	if err != nil {
		return nil, err
	}
	out := make([]ScannedFile, 0, len(rels))
	for _, rel := range rels {
		if inSkippedDir(rel) {
			continue
		}
		f, ok := measure(filepath.Join(root, filepath.FromSlash(rel)), rel)
		if !ok {
			continue
		}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// listFiles returns the repo-relative paths to measure, preferring git's own
// account of the working tree and walking the directory otherwise.
func listFiles(ctx context.Context, root string) ([]string, error) {
	if rels, ok := gitFiles(ctx, root); ok {
		return rels, nil
	}
	return walkFiles(root)
}

// gitFiles asks git for the working tree: tracked files plus untracked ones,
// minus everything the repository's ignore rules exclude. It reports failure
// rather than an error, because "this is not a git repository" is an ordinary
// case that the directory walk handles.
func gitFiles(ctx context.Context, root string) ([]string, bool) {
	ctx, cancel := context.WithTimeout(ctx, gitListTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "-co", "--exclude-standard", "-z")
	out, err := cmd.Output()
	if err != nil {
		return nil, false
	}
	seen := make(map[string]bool)
	var rels []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		rels = append(rels, filepath.ToSlash(p))
	}
	return rels, true
}

// walkFiles lists every file under root, skipping dependency and output
// directories by name. This is the path taken when root is not a repository.
func walkFiles(root string) ([]string, error) {
	var rels []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entry: skip it, keep walking
		}
		if d.IsDir() {
			if path != root && skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		rels = append(rels, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return rels, nil
}

// inSkippedDir reports whether any segment of a path is a directory the map
// excludes. git happily tracks vendor/ and node_modules/, so the check has to
// apply to git's list too, not only to the walk.
func inSkippedDir(rel string) bool {
	segs := strings.Split(rel, "/")
	for _, seg := range segs[:len(segs)-1] { // the last segment is the filename
		if skipDir(seg) {
			return true
		}
	}
	return false
}

// measure reads one file and reports what it is, how long it is, and a hash
// of its contents.
//
// An asset is classified from its name and never opened: a font or an archive
// has no line count, and reading a hundred megabytes to hash bytes nobody will
// compare is pure waste.
func measure(abs, rel string) (ScannedFile, bool) {
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		return ScannedFile{}, false
	}
	category, lang := classify(rel, info.Size())
	f := ScannedFile{Path: rel, Category: category, Lang: lang, Bytes: info.Size()}
	if category == CategoryAsset || category == CategoryGenerated {
		return f, true
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return ScannedFile{}, false
	}
	f.LOC = countLines(data)
	sum := sha256.Sum256(data)
	f.Hash = hex.EncodeToString(sum[:])[:hashLen]
	return f, true
}

// countLines counts the lines that have something on them.
//
// Blank lines are spacing, not content. Counting them makes a generously
// spaced file look larger than a dense one of the same substance, and on this
// repository it inflates the total by about eleven per cent -- so a line count
// that included them would not measure what anyone means by the size of the
// code. A final line with no trailing newline still counts.
func countLines(data []byte) int {
	n := 0
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) > 0 {
			n++
		}
	}
	return n
}

// skipDir reports whether a directory is excluded from the map.
func skipDir(name string) bool { return skipDirs[name] }

package floorplan

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MapDir is the directory inside a repository that holds its map.
//
// The authored layer lives in the repository rather than in Aux's database on
// one machine. It is the expensive half of the map -- prose that costs model
// calls to produce -- and keeping it here means it can be reviewed in a pull
// request, shared with everyone working on the project, and survives a
// database being thrown away.
const MapDir = "floorplan"

const (
	architectureFile = "architecture.json"
	descriptionsDir  = "descriptions"
)

// ErrNoMap reports that a repository has no map yet. That is the ordinary
// first case rather than a failure, and callers have to be able to tell it
// apart from a map that exists but cannot be read.
var ErrNoMap = errors.New("no floor plan has been written for this repository")

// diskDescription is one file's authored prose as stored.
//
// The stored key is "description" and the built document's is "d": these are
// two schemas, not one, and conflating them silently loses every description.
type diskDescription struct {
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	Doc         string   `json:"doc,omitempty"`
	Note        string   `json:"note,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Sig         string   `json:"sig,omitempty"`
}

// descriptionFile is one area's description document. Keys this version does
// not understand are carried through untouched, so a map written by a newer
// version -- or by Floorplan itself -- does not lose data by being loaded and
// saved here.
type descriptionFile struct {
	Files map[string]diskDescription `json:"files,omitempty"`
	Extra map[string]json.RawMessage `json:"-"`
}

// LoadArchitecture reads a repository's authored architecture.
func LoadArchitecture(root string) (Architecture, error) {
	path := filepath.Join(root, MapDir, architectureFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Architecture{}, ErrNoMap
	}
	if err != nil {
		return Architecture{}, fmt.Errorf("failed to read %s: %w", path, err)
	}
	var arch Architecture
	if err := json.Unmarshal(data, &arch); err != nil {
		// Deliberately not ErrNoMap: a damaged file reported as a missing one
		// would be overwritten with an empty map, destroying authored work.
		return Architecture{}, fmt.Errorf("failed to parse %s: %w", path, err)
	}
	return arch, nil
}

// SaveArchitecture writes a repository's authored architecture.
func SaveArchitecture(root string, arch Architecture) error {
	if arch.Schema == "" {
		arch.Schema = ArchitectureSchema
	}
	return writeJSON(filepath.Join(root, MapDir, architectureFile), arch)
}

// LoadDescriptions reads every area's descriptions, keyed by file path.
//
// A repository with no descriptions yet returns an empty set rather than an
// error: an undescribed map is a normal state, and the dashboard shows it as
// undescribed.
func LoadDescriptions(root string) (map[string]FileDescription, error) {
	dir := filepath.Join(root, MapDir, descriptionsDir)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]FileDescription{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", dir, err)
	}

	out := map[string]FileDescription{}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	// Sorted so that a path described in two areas resolves the same way every
	// time rather than depending on directory order.
	sort.Strings(names)

	for _, name := range names {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", path, err)
		}
		var doc descriptionFile
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", path, err)
		}
		for p, d := range doc.Files {
			out[p] = FileDescription{
				Name: d.Name, D: d.Description, Doc: d.Doc,
				Note: d.Note, Tags: d.Tags, Sig: d.Sig,
			}
		}
	}
	return out, nil
}

// SaveAreaDescriptions writes one area's descriptions.
//
// One file per area is what keeps a describe run over a single area from
// rewriting every other area's prose, so a partial run cannot damage work it
// never looked at.
func SaveAreaDescriptions(root, areaID string, entries map[string]FileDescription) error {
	if err := safeAreaID(areaID); err != nil {
		return err
	}
	files := make(map[string]diskDescription, len(entries))
	for p, d := range entries {
		files[p] = diskDescription{
			Name: d.Name, Description: d.D, Doc: d.Doc,
			Note: d.Note, Tags: d.Tags, Sig: d.Sig,
		}
	}
	path := filepath.Join(root, MapDir, descriptionsDir, areaID+".json")
	return writeJSON(path, descriptionFile{Files: files})
}

// safeAreaID rejects an id that could address a file outside the map
// directory. Area ids come from an authored document, and a document is not a
// trustworthy source of filenames.
func safeAreaID(areaID string) error {
	switch {
	case areaID == "":
		return errors.New("an area id cannot be empty")
	case areaID == "." || areaID == "..":
		return fmt.Errorf("%q is not a usable area id", areaID)
	case strings.ContainsAny(areaID, `/\`):
		return fmt.Errorf("the area id %q contains a path separator", areaID)
	case strings.Contains(areaID, ".."):
		return fmt.Errorf("the area id %q contains %q", areaID, "..")
	case strings.HasPrefix(areaID, "."):
		return fmt.Errorf("the area id %q starts with a dot", areaID)
	default:
		return nil
	}
}

// writeJSON writes a document indented, so the map reads as a reviewable diff
// in a pull request rather than as one long line.
func writeJSON(path string, doc any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", filepath.Dir(path), err)
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode %s: %w", path, err)
	}
	data = append(data, '\n')
	// Written through a temporary file in the same directory so an interrupted
	// write cannot leave a half-written map in place of a good one.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create a temporary file beside %s: %w", path, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close the temporary file for %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("failed to replace %s: %w", path, err)
	}
	return nil
}

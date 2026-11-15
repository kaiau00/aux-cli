package floorplan

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// wildcardChars begin a glob's variable part. Everything before the first one
// is the literal path prefix the pattern commits to.
const wildcardChars = "*?[{"

// literalPrefixLen is the length of the leading literal path prefix of a glob.
// It measures how specific a pattern is: "internal/db/**" commits to more of
// the tree than "internal/**", so it is the better claim on a path both match.
func literalPrefixLen(glob string) int {
	if i := strings.IndexAny(glob, wildcardChars); i >= 0 {
		return i
	}
	return len(glob)
}

// compiledArea is one area with its depth and its full exclude set (its own
// plus every ancestor's) resolved once, so Assign does no tree walking.
type compiledArea struct {
	id      string
	depth   int
	paths   []string
	exclude []string
}

// Assigner decides which area of the map owns a given repo-relative path.
//
// Three rules settle every contest, in order:
//
//  1. The deepest area wins. A child area exists to carve a subtree out of its
//     parent, so when both match, the child meant it.
//  2. At equal depth, the longer literal prefix wins. "internal/db/**" is a
//     more specific claim than "internal/**".
//  3. Failing both, the lexicographically smaller id wins, so the result never
//     depends on the order areas happen to be declared in.
//
// An exclude is inherited by descendants. That is what lets one top-level area
// release a whole category -- every "**/*_test.go" -- to a dedicated area
// without each of its children having to repeat the exclusion.
type Assigner struct {
	areas []compiledArea
}

// NewAssigner compiles the areas, rejecting a set it cannot assign against
// deterministically. Every one of these errors would otherwise put files in
// rooms they were never placed in, with nothing to show something went wrong.
func NewAssigner(areas []Area) (*Assigner, error) {
	byID := make(map[string]Area, len(areas))
	for _, a := range areas {
		if a.ID == "" {
			return nil, fmt.Errorf("area with label %q has no id", a.Label)
		}
		if _, dup := byID[a.ID]; dup {
			return nil, fmt.Errorf("duplicate area id %q", a.ID)
		}
		byID[a.ID] = a
	}
	for _, a := range areas {
		if a.Parent != "" {
			if _, ok := byID[a.Parent]; !ok {
				return nil, fmt.Errorf("area %q names parent %q, which does not exist", a.ID, a.Parent)
			}
		}
		for _, g := range append(append([]string{}, a.Paths...), a.Exclude...) {
			if !doublestar.ValidatePattern(g) {
				return nil, fmt.Errorf("area %q has an unparsable glob %q", a.ID, g)
			}
		}
	}

	depths, err := resolveDepths(areas, byID)
	if err != nil {
		return nil, err
	}

	compiled := make([]compiledArea, 0, len(areas))
	for _, a := range areas {
		compiled = append(compiled, compiledArea{
			id:      a.ID,
			depth:   depths[a.ID],
			paths:   a.Paths,
			exclude: inheritedExcludes(a, byID),
		})
	}
	// Sort so the scan order itself encodes the precedence rules and the
	// first match found is the winner.
	sort.Slice(compiled, func(i, j int) bool {
		if compiled[i].depth != compiled[j].depth {
			return compiled[i].depth > compiled[j].depth
		}
		return compiled[i].id < compiled[j].id
	})
	return &Assigner{areas: compiled}, nil
}

// resolveDepths walks each area up to a root, reporting a cycle rather than
// looping on one.
func resolveDepths(areas []Area, byID map[string]Area) (map[string]int, error) {
	depths := make(map[string]int, len(areas))
	for _, a := range areas {
		depth := 0
		seen := map[string]bool{a.ID: true}
		for cur := a; cur.Parent != ""; {
			parent := byID[cur.Parent]
			if seen[parent.ID] {
				return nil, fmt.Errorf("area parent cycle through %q", parent.ID)
			}
			seen[parent.ID] = true
			depth++
			cur = parent
		}
		depths[a.ID] = depth
	}
	return depths, nil
}

// inheritedExcludes is an area's own excludes plus every ancestor's.
func inheritedExcludes(a Area, byID map[string]Area) []string {
	out := append([]string{}, a.Exclude...)
	seen := map[string]bool{a.ID: true}
	for cur := a; cur.Parent != ""; {
		parent := byID[cur.Parent]
		if seen[parent.ID] {
			break // resolveDepths reports the cycle; do not loop on it here
		}
		seen[parent.ID] = true
		out = append(out, parent.Exclude...)
		cur = parent
	}
	return out
}

// Assign returns the id of the area owning rel, or "" when no area claims it.
// An unclaimed path is reported as unclaimed: inventing an owner would place a
// file in a room the map never put it in, and the count of unassigned files is
// how an incomplete map announces itself.
func (a *Assigner) Assign(rel string) string {
	rel = strings.TrimPrefix(rel, "./")
	best, bestDepth, bestPrefix := "", -1, -1
	for _, area := range a.areas {
		// areas are sorted deepest-first, so once a winner is found no later
		// area can beat it on depth.
		if best != "" && area.depth < bestDepth {
			break
		}
		prefix, ok := a.bestMatch(area, rel)
		if !ok {
			continue
		}
		if best == "" || area.depth > bestDepth || (area.depth == bestDepth && prefix > bestPrefix) {
			best, bestDepth, bestPrefix = area.id, area.depth, prefix
		}
	}
	return best
}

// bestMatch reports whether the area claims rel, and the specificity of the
// most specific glob that matched.
func (a *Assigner) bestMatch(area compiledArea, rel string) (int, bool) {
	prefix, matched := -1, false
	for _, g := range area.paths {
		if ok, err := doublestar.Match(g, rel); err == nil && ok {
			matched = true
			if p := literalPrefixLen(g); p > prefix {
				prefix = p
			}
		}
	}
	if !matched {
		return 0, false
	}
	for _, g := range area.exclude {
		if ok, err := doublestar.Match(g, rel); err == nil && ok {
			return 0, false
		}
	}
	return prefix, true
}

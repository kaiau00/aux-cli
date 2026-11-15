package floorplan

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// BuildInput is everything needed to assemble one map: the authored account of
// the repository, the measured scan, the derived links, and the git state.
type BuildInput struct {
	Root   string
	Arch   Architecture
	Files  []ScannedFile
	Facts  Facts
	Links  []Link
	Routes []Route
	Repos  []Repo
	Agent  string
	// ScanAt is when Files was measured; Now is when the document is built.
	// Both are passed in rather than read from the clock so a build is
	// reproducible and can be asserted on.
	ScanAt time.Time
	Now    time.Time
	// Descriptions carries the authored prose for individual files, keyed by
	// repo-relative path. Absent entries stay absent: an undescribed file is
	// reported as undescribed.
	Descriptions map[string]FileDescription
}

// FileDescription is the authored prose for one file. Sig records the content
// hash it was written against, so a description can be flagged when the file
// has moved on without it.
type FileDescription struct {
	Name string   `json:"name,omitempty"`
	D    string   `json:"d,omitempty"`
	Doc  string   `json:"doc,omitempty"`
	Note string   `json:"note,omitempty"`
	Tags []string `json:"tags,omitempty"`
	Sig  string   `json:"sig,omitempty"`
}

// Build assembles a graph@1 document.
//
// Only code is mapped: a README is part of the repository but not of the
// codebase, and counting it would overstate the project. Every file that no
// area claimed is counted as unassigned, which is how an incomplete map
// announces itself instead of quietly absorbing the file somewhere plausible.
func Build(in BuildInput) (Graph, error) {
	assigner, err := NewAssigner(in.Arch.Nodes)
	if err != nil {
		return Graph{}, fmt.Errorf("the architecture cannot be used to assign files: %w", err)
	}

	repoName := "."
	if len(in.Repos) > 0 {
		repoName = in.Repos[0].Path
	}

	files := make([]FileRec, 0, len(in.Files))
	counts := Counts{}
	// direct holds each area's own files, before anything is rolled up.
	direct := map[string]*Stats{}
	for _, sf := range in.Files {
		if !CountsTowardLOC(sf.Category) {
			continue
		}
		area := assigner.Assign(sf.Path)
		rec := FileRec{P: sf.Path, N: area, R: repoName, L: sf.Lang, LOC: sf.LOC, H: sf.Hash, S: in.Facts[sf.Path].Signature}
		if d, ok := in.Descriptions[sf.Path]; ok {
			rec.Name, rec.D, rec.Doc, rec.Note, rec.Tags, rec.Sig = d.Name, d.D, d.Doc, d.Note, d.Tags, d.Sig
		}
		files = append(files, rec)

		counts.Files++
		counts.LOC += sf.LOC
		if area == "" {
			counts.Unassigned++
			continue
		}
		st, ok := direct[area]
		if !ok {
			st = &Stats{Langs: map[string]int{}}
			direct[area] = st
		}
		st.Files++
		st.LOC += sf.LOC
		if sf.Lang != "" {
			st.Langs[sf.Lang] += sf.LOC
		}
	}

	nodes, err := buildNodes(in.Arch.Nodes, direct, repoName)
	if err != nil {
		return Graph{}, err
	}

	links := append([]Link{}, in.Links...)
	links = append(links, authoredEdges(in.Arch.Edges)...)

	flows, tour, err := resolveWalks(in.Arch, nodes, files)
	if err != nil {
		return Graph{}, err
	}

	return Graph{
		Schema: GraphSchema,
		Meta: Meta{
			GeneratedAt: rfc3339(in.Now),
			ScanAt:      rfc3339(in.ScanAt),
			RootName:    rootName(in.Root, in.Arch),
			Root:        in.Root,
			Audience:    in.Arch.Audience,
			Agent:       in.Agent,
			Repos:       nonNilRepos(in.Repos),
			Counts:      counts,
		},
		Project: in.Arch.Project,
		Nodes:   nodes,
		Files:   files,
		Routes:  nonNilRoutes(in.Routes),
		Links:   links,
		Flows:   flows,
		Tour:    tour,
		Recent:  Recent{Tasks: []RecentTask{}, Items: []RecentItem{}},
		Health:  Health{Issues: []HealthIssue{}},
	}, nil
}

// buildNodes turns authored areas into built nodes, rolling each area's own
// files up through its ancestors.
//
// A parent's size is the size of everything inside it: an area that holds only
// child areas owns no files directly, and reporting zero for it would read as
// an empty room rather than a wing of the building.
func buildNodes(areas []Area, direct map[string]*Stats, repoName string) ([]Node, error) {
	byID := make(map[string]Area, len(areas))
	for _, a := range areas {
		byID[a.ID] = a
	}

	rolled := make(map[string]*Stats, len(areas))
	for _, a := range areas {
		rolled[a.ID] = &Stats{Langs: map[string]int{}}
	}
	for id, own := range direct {
		// Walk from the owning area up to the root, adding to each.
		seen := map[string]bool{}
		for cur := id; cur != "" && !seen[cur]; {
			seen[cur] = true
			target, ok := rolled[cur]
			if !ok {
				break
			}
			target.Files += own.Files
			target.LOC += own.LOC
			for lang, n := range own.Langs {
				target.Langs[lang] += n
			}
			cur = byID[cur].Parent
		}
	}

	nodes := make([]Node, 0, len(areas))
	for _, a := range areas {
		var parent *string
		if a.Parent != "" {
			p := a.Parent
			parent = &p
		}
		st := rolled[a.ID]
		repos := []string{}
		if st.Files > 0 {
			repos = append(repos, repoName)
		}
		nodes = append(nodes, Node{
			ID: a.ID, Parent: parent, Label: a.Label, Kind: a.Kind,
			Summary: a.Summary, Details: a.Details,
			Tags:     []string{},
			Paths:    nonNilStrings(a.Paths),
			Packages: nonNilStrings(a.Packages),
			Hosts:    nonNilStrings(a.Hosts),
			Stats:    Stats{Files: st.Files, LOC: st.LOC, Langs: st.Langs, Repos: repos},
		})
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	return nodes, nil
}

// authoredEdges turns area-to-area claims into links. These cross boundaries
// no parser can see -- a child process, an HTTP call, a queue -- so they exist
// only because somebody wrote them down.
func authoredEdges(edges []Edge) []Link {
	out := make([]Link, 0, len(edges))
	for _, e := range edges {
		l := Link{A: "n:" + e.From, B: "n:" + e.To, K: e.Kind, W: 1, Desc: e.Description}
		if e.Label != "" {
			l.Labels = []string{e.Label}
		}
		out = append(out, l)
	}
	return out
}

// resolveWalks turns each authored flow step and tour stop into a resolved
// reference, by looking its name up among the areas and then the mapped files.
//
// A name that matches neither is reported rather than dropped: a flow missing
// a stage still reads as a complete account of how something works, which is
// worse than a build that refuses and says which step is broken.
func resolveWalks(arch Architecture, nodes []Node, files []FileRec) ([]BuiltFlow, []BuiltTourStop, error) {
	areas := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		areas[n.ID] = true
	}
	paths := make(map[string]bool, len(files))
	for _, f := range files {
		paths[f.P] = true
	}
	resolve := func(node string) (string, bool) {
		switch {
		case areas[node]:
			return "n:" + node, true
		case paths[node]:
			return "f:" + node, true
		default:
			return "", false
		}
	}

	var bad []string
	flows := make([]BuiltFlow, 0, len(arch.Flows))
	for _, fl := range arch.Flows {
		built := BuiltFlow{ID: fl.ID, Title: fl.Title, Description: fl.Description, Steps: []BuiltFlowStep{}}
		for i, st := range fl.Steps {
			ref, ok := resolve(st.Node)
			if !ok {
				bad = append(bad, fmt.Sprintf("flow %q step %d names %q", fl.ID, i+1, st.Node))
				continue
			}
			built.Steps = append(built.Steps, BuiltFlowStep{Ref: ref, Note: st.Note})
		}
		flows = append(flows, built)
	}

	tour := make([]BuiltTourStop, 0, len(arch.Tour))
	for i, st := range arch.Tour {
		ref, ok := resolve(st.Node)
		if !ok {
			bad = append(bad, fmt.Sprintf("tour stop %d (%q) names %q", i+1, st.Title, st.Node))
			continue
		}
		tour = append(tour, BuiltTourStop{Ref: ref, Title: st.Title, Note: st.Note})
	}

	for _, a := range arch.Annotations {
		if !paths[a.Path] {
			bad = append(bad, fmt.Sprintf("an annotation names %q, which is not a mapped file", a.Path))
		}
	}
	if len(bad) > 0 {
		return nil, nil, fmt.Errorf("the map names %d things that do not exist: %s", len(bad), strings.Join(bad, "; "))
	}
	return flows, tour, nil
}

// rootName is the display name for the mapped checkout.
func rootName(root string, arch Architecture) string {
	if arch.Project.Name != "" {
		return arch.Project.Name
	}
	if i := strings.LastIndex(root, "/"); i >= 0 && i < len(root)-1 {
		return root[i+1:]
	}
	return root
}

// rfc3339 formats a time for the document, or returns "" for the zero time so
// an unknown timestamp is absent rather than claiming the year 1.
func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func nonNilRoutes(r []Route) []Route {
	if r == nil {
		return []Route{}
	}
	return r
}

func nonNilRepos(r []Repo) []Repo {
	if r == nil {
		return []Repo{}
	}
	return r
}

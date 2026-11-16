// Package floorplan builds a navigable map of a repository: a deterministic
// scan of what is on disk, joined to an agent-authored account of what each
// area is for.
//
// The two layers are kept apart on purpose. The scan (files, languages, line
// counts, import links, git state) is measured and can always be rebuilt. The
// prose (area labels, summaries, flows, annotations) is written by the agent
// after reading the code, and cannot be derived from file names. A map whose
// prose is missing renders as undescribed rather than guessing, because a
// plausible-sounding summary that nobody verified is worse than a blank one.
//
// The design is Floorplan's (https://github.com/haydenschmitty/floorplan, MIT,
// Copyright (c) 2026 Hayden Smith): its two schemas, its file classification
// rules, its area-assignment precedence, and its definition of the signature a
// description depends on. This is a new Go implementation of them, verified by
// measuring this repository both ways and diffing the results. Keeping to the
// schemas means a map Aux builds can be read by that project's tooling and
// vice versa. See NOTICE at the repository root for the licence.
package floorplan

// Schema identifiers written into the documents Aux produces and expects.
const (
	// ArchitectureSchema identifies the agent-authored input document.
	ArchitectureSchema = "floorplan/architecture@1"
	// GraphSchema identifies the built output document.
	GraphSchema = "floorplan/graph@1"
)

// Area kinds. These classify what an area of the repository is for, and the
// dashboard colours nodes by them.
const (
	KindUI       = "ui"
	KindService  = "service"
	KindWorker   = "worker"
	KindLibrary  = "library"
	KindFeature  = "feature"
	KindData     = "data"
	KindConfig   = "config"
	KindTest     = "test"
	KindScript   = "script"
	KindInfra    = "infra"
	KindExternal = "external"
	KindMisc     = "misc"
)

// Link kinds. An import link is derived from source; the rest are only ever
// agent-authored, because they cross a boundary no parser can see (a child
// process, an HTTP call, a queue).
const (
	LinkImport   = "import"
	LinkHTTP     = "http"
	LinkCall     = "call"
	LinkRPC      = "rpc"
	LinkEvent    = "event"
	LinkQueue    = "queue"
	LinkData     = "data"
	LinkConfig   = "config"
	LinkContains = "contains"
)

// Architecture is the agent-authored layer: the account of the repository that
// has to be written by something that has read it.
type Architecture struct {
	Schema      string       `json:"$schema"`
	Audience    string       `json:"audience,omitempty"`
	Project     Project      `json:"project"`
	Nodes       []Area       `json:"nodes"`
	Edges       []Edge       `json:"edges,omitempty"`
	Annotations []Annotation `json:"annotations,omitempty"`
	Flows       []Flow       `json:"flows,omitempty"`
	Tour        []TourStop   `json:"tour,omitempty"`
}

// Project describes the repository as a whole.
type Project struct {
	Name        string   `json:"name"`
	Summary     string   `json:"summary,omitempty"`
	Entrypoints []string `json:"entrypoints,omitempty"`
}

// Area is one region of the map. Paths claim files into it; Exclude releases
// them, and is inherited by descendants so a top-level area can hand a whole
// category (tests, generated code) to a sibling.
type Area struct {
	ID       string   `json:"id"`
	Parent   string   `json:"parent,omitempty"`
	Label    string   `json:"label"`
	Kind     string   `json:"kind"`
	Summary  string   `json:"summary,omitempty"`
	Details  string   `json:"details,omitempty"`
	Paths    []string `json:"paths,omitempty"`
	Exclude  []string `json:"exclude,omitempty"`
	Packages []string `json:"packages,omitempty"`
	Hosts    []string `json:"hosts,omitempty"`
}

// Edge is an agent-authored link between two areas.
type Edge struct {
	From        string `json:"from"`
	To          string `json:"to"`
	Kind        string `json:"kind"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
}

// Annotation marks one file as worth knowing about before reading it.
type Annotation struct {
	Path string   `json:"path"`
	Note string   `json:"note"`
	Tags []string `json:"tags,omitempty"`
}

// Flow is an ordered walk through the map that follows one real operation.
type Flow struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	Steps       []FlowStep `json:"steps"`
}

// FlowStep names one stop of a flow.
//
// Node holds either an area id or a repo-relative file path; which one is
// decided when the map is built, by looking it up. Authoring a step should not
// require remembering a prefix, so the document the agent writes carries the
// bare name and the built document carries the resolved reference.
type FlowStep struct {
	Node string `json:"node"`
	Note string `json:"note,omitempty"`
}

// TourStop is one stop in the orientation tour. Node follows the same rule as
// a flow step's.
type TourStop struct {
	Node  string `json:"node"`
	Title string `json:"title"`
	Note  string `json:"note,omitempty"`
}

// BuiltFlow is a flow with every step resolved to a reference.
type BuiltFlow struct {
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	Description string          `json:"description,omitempty"`
	Steps       []BuiltFlowStep `json:"steps"`
}

// BuiltFlowStep references an area ("n:<id>") or a file ("f:<path>").
type BuiltFlowStep struct {
	Ref  string `json:"ref"`
	Note string `json:"note,omitempty"`
}

// BuiltTourStop is a tour stop with its reference resolved.
type BuiltTourStop struct {
	Ref   string `json:"ref"`
	Title string `json:"title"`
	Note  string `json:"note,omitempty"`
}

// Graph is the built map: the agent-authored layer joined to the scan. Field
// names are short because this document is served to the browser whole.
type Graph struct {
	Schema  string          `json:"schema"`
	Meta    Meta            `json:"meta"`
	Project Project         `json:"project"`
	Nodes   []Node          `json:"nodes"`
	Files   []FileRec       `json:"files"`
	Routes  []Route         `json:"routes"`
	Links   []Link          `json:"links"`
	Flows   []BuiltFlow     `json:"flows"`
	Tour    []BuiltTourStop `json:"tour"`
	Recent  Recent          `json:"recent"`
	Health  Health          `json:"health"`
}

// Meta records how and when the map was built, so a stale map can be spotted.
type Meta struct {
	GeneratedAt string `json:"generatedAt"`
	ScanAt      string `json:"scanAt,omitempty"`
	RootName    string `json:"rootName"`
	Root        string `json:"root"`
	Audience    string `json:"audience,omitempty"`
	Agent       string `json:"agent,omitempty"`
	Repos       []Repo `json:"repos"`
	Counts      Counts `json:"counts"`
}

// Repo is the git state of one checkout the map covers.
type Repo struct {
	Path   string `json:"path"`
	Name   string `json:"name"`
	Branch string `json:"branch,omitempty"`
	Head   string `json:"head,omitempty"`
	Remote string `json:"remote,omitempty"`
}

// Counts are the headline totals. Unassigned is the honesty check: files the
// architecture never claimed.
type Counts struct {
	Files      int `json:"files"`
	LOC        int `json:"loc"`
	Unassigned int `json:"unassigned"`
}

// Node is a built area: its authored prose plus the measured stats of the
// files assigned to it.
type Node struct {
	ID       string   `json:"id"`
	Parent   *string  `json:"parent"`
	Label    string   `json:"label"`
	Kind     string   `json:"kind"`
	Summary  string   `json:"summary,omitempty"`
	Details  string   `json:"details,omitempty"`
	Tags     []string `json:"tags"`
	Paths    []string `json:"paths"`
	Packages []string `json:"packages"`
	Hosts    []string `json:"hosts"`
	Stats    Stats    `json:"stats"`
}

// Stats are measured, never authored.
type Stats struct {
	Files int            `json:"files"`
	LOC   int            `json:"loc"`
	Langs map[string]int `json:"langs"`
	Repos []string       `json:"repos"`
}

// FileRec is one scanned file. P is the repo-relative path, N the area that
// claimed it, L the language, Loc the line count, H a content hash. Name and D
// are authored; Sig records the content the description was written against,
// so a description can be flagged when the file moves on without it.
type FileRec struct {
	P    string   `json:"p"`
	N    string   `json:"n,omitempty"`
	R    string   `json:"r,omitempty"`
	L    string   `json:"l,omitempty"`
	LOC  int      `json:"loc"`
	Name string   `json:"name,omitempty"`
	D    string   `json:"d,omitempty"`
	Doc  string   `json:"doc,omitempty"`
	Note string   `json:"note,omitempty"`
	Tags []string `json:"tags,omitempty"`
	S    string   `json:"s,omitempty"`
	H    string   `json:"h,omitempty"`
	Sig  string   `json:"sig,omitempty"`
}

// Route is one served HTTP route found in the source.
type Route struct {
	F    string  `json:"f"`
	M    string  `json:"m"`
	P    string  `json:"p"`
	L    int     `json:"l"`
	H    *string `json:"h"`
	Name string  `json:"name,omitempty"`
	D    string  `json:"d,omitempty"`
}

// Link is one relationship. A and B are prefixed refs ("f:<path>" for a file,
// "n:<id>" for an area), and A depends on B.
type Link struct {
	A        string   `json:"a"`
	B        string   `json:"b"`
	K        string   `json:"k"`
	W        float64  `json:"w"`
	Desc     string   `json:"desc,omitempty"`
	Labels   []string `json:"labels,omitempty"`
	Names    []string `json:"names,omitempty"`
	Calls    int      `json:"calls,omitempty"`
	Inferred bool     `json:"inferred,omitempty"`
}

// Recent is the record of what was last worked on and why.
type Recent struct {
	UpdatedAt string       `json:"updatedAt,omitempty"`
	Tasks     []RecentTask `json:"tasks"`
	Items     []RecentItem `json:"items"`
}

// RecentTask is one unit of recent work.
type RecentTask struct {
	Title   string `json:"title"`
	Request string `json:"request,omitempty"`
	Summary string `json:"summary,omitempty"`
}

// RecentItem ties one file to the task that touched it.
type RecentItem struct {
	ID     string `json:"id"`
	Task   int    `json:"task"`
	Change string `json:"change,omitempty"`
	Note   string `json:"note,omitempty"`
}

// Health reports problems found by measurement, not by opinion.
type Health struct {
	Summary HealthSummary `json:"summary"`
	Issues  []HealthIssue `json:"issues"`
}

// HealthSummary counts issues by severity.
type HealthSummary struct {
	High   int `json:"high"`
	Medium int `json:"medium"`
	Low    int `json:"low"`
}

// HealthIssue is one measured problem.
type HealthIssue struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"`
	Severity string   `json:"severity"`
	Title    string   `json:"title"`
	Detail   string   `json:"detail,omitempty"`
	Refs     []string `json:"refs,omitempty"`
}

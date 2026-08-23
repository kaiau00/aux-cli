package viewmodel

import (
	"context"

	"github.com/kaiau00/aux-cli/internal/impact"
)

// ImpactNodeVM is one impact-graph node for display.
type ImpactNodeVM struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	StableKey   string `json:"stableKey"`
	DisplayName string `json:"displayName,omitempty"`
}

// ImpactEdgeVM is one impact-graph edge for display.
type ImpactEdgeVM struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"`
}

// ImpactGraphVM is the project's change-impact graph state — the dashboard's
// Impact view (roadmapplan.md §13.14 item 6). Deterministic AST-derived state,
// never an LLM guess (§8.5).
type ImpactGraphVM struct {
	Built          bool           `json:"built"`
	SourceRevision string         `json:"sourceRevision,omitempty"`
	Status         string         `json:"status,omitempty"`
	LastIndexedAt  int64          `json:"lastIndexedAt,omitempty"`
	NodeCount      int            `json:"nodeCount"`
	Nodes          []ImpactNodeVM `json:"nodes"`
	Edges          []ImpactEdgeVM `json:"edges"`
}

// BuildImpactNode projects a graph node for display.
func BuildImpactNode(n impact.Node) ImpactNodeVM {
	return ImpactNodeVM{ID: n.ID, Type: n.Type, StableKey: n.StableKey, DisplayName: n.DisplayName}
}

// BuildImpactEdge projects a graph edge for display.
func BuildImpactEdge(e impact.Edge) ImpactEdgeVM {
	return ImpactEdgeVM{From: e.FromNodeID, To: e.ToNodeID, Type: e.Type}
}

// ImpactReader reads the impact graph's nodes, edges, and index state.
type ImpactReader interface {
	CountNodes(ctx context.Context, projectID string) (int, error)
	GetIndexState(ctx context.Context, projectID string) (impact.IndexState, bool, error)
	ListNodes(ctx context.Context, projectID string, limit int) ([]impact.Node, error)
	ListEdges(ctx context.Context, projectID string, limit int) ([]impact.Edge, error)
}

// ImpactStores bundles the read dependency needed to assemble an
// ImpactGraphVM.
type ImpactStores struct {
	Impact ImpactReader
}

// ImpactGraphView assembles a project's impact-graph state for display.
func (s ImpactStores) ImpactGraphView(ctx context.Context, projectID string) (ImpactGraphVM, error) {
	if s.Impact == nil {
		return ImpactGraphVM{}, nil
	}
	count, err := s.Impact.CountNodes(ctx, projectID)
	if err != nil {
		return ImpactGraphVM{}, err
	}
	view := ImpactGraphVM{Built: count > 0, NodeCount: count}

	if st, ok, serr := s.Impact.GetIndexState(ctx, projectID); serr == nil && ok {
		view.SourceRevision = st.SourceRevision
		view.Status = st.Status
		view.LastIndexedAt = st.LastIndexedAt
	}
	if nodes, nerr := s.Impact.ListNodes(ctx, projectID, 0); nerr == nil {
		for _, n := range nodes {
			view.Nodes = append(view.Nodes, BuildImpactNode(n))
		}
	}
	if edges, eerr := s.Impact.ListEdges(ctx, projectID, 0); eerr == nil {
		for _, e := range edges {
			view.Edges = append(view.Edges, BuildImpactEdge(e))
		}
	}
	return view, nil
}

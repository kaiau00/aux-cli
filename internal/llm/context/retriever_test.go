package context

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaiau00/aux-cli/internal/config"
)

func enabledCfg() config.SemanticRetrievalConfig {
	return config.SemanticRetrievalConfig{
		Enabled:        true,
		MaxFiles:       3,
		MaxLines:       150,
		MinScore:       0.05,
		Damping:        0.85,
		MaxIterations:  50,
		Epsilon:        1e-6,
		TimeoutSeconds: 5,
	}
}

func newTestState(t *testing.T, prompt string, cfg config.SemanticRetrievalConfig, q graphQuerier) (*State, string) {
	t.Helper()
	dir := t.TempDir()
	if cfg.MaxLines <= 0 {
		cfg.MaxLines = 150
	}
	s := newStateWithConfig(prompt, dir, cfg)
	if s == nil {
		t.Fatalf("newStateWithConfig returned nil; cfg.Enabled=%v", cfg.Enabled)
	}
	if q != nil {
		s.queryGraph = q
	}
	return s, dir
}

func TestNewStateDisabledReturnsNil(t *testing.T) {
	dir := t.TempDir()
	cfg := enabledCfg()
	cfg.Enabled = false
	s := newStateWithConfig("anything", dir, cfg)
	if s != nil {
		t.Fatalf("expected nil state when retrieval disabled, got %#v", s)
	}
}

func TestCheckFileDirectRefAllowed(t *testing.T) {
	_ = t.TempDir()
	rel := "internal/auth.go"
	prompt := "please review internal/auth.go for the auth flow"
	s, _ := newTestState(t, prompt, enabledCfg(), func(ctx context.Context, p string) (Graph, error) {
		return Graph{}, nil
	})

	dec := s.CheckFile(context.Background(), rel)
	if !dec.Allowed {
		t.Fatalf("expected allow, got reject: %s", dec.Reason)
	}
	if !strings.Contains(dec.Reason, "directly referenced") {
		t.Fatalf("expected direct-ref reason, got %q", dec.Reason)
	}
	if dec.MaxLines != 150 {
		t.Fatalf("expected MaxLines=150, got %d", dec.MaxLines)
	}

	m := s.Metrics()
	if m.DirectRefBypasses != 1 {
		t.Fatalf("expected DirectRefBypasses=1, got %d", m.DirectRefBypasses)
	}
	if m.Allowed != 1 || m.Rejected != 0 || m.GraphErrors != 0 {
		t.Fatalf("unexpected metrics: %+v", m)
	}
	// Direct refs bypass the graph entirely, so no rank should be recorded.
	if m.GraphLatencyMS != 0 || m.PageRankLatencyMS != 0 {
		t.Fatalf("expected zero graph/pagerank latency on direct-ref bypass, got %+v", m)
	}
}

func TestCheckFileAllowedByPageRank(t *testing.T) {
	dir := t.TempDir()
	graph := Graph{
		Nodes: []GraphNode{
			{ID: "auth", Name: "Auth", Path: filepath.Join(dir, "internal/auth.go")},
			{ID: "db", Name: "DB", Path: filepath.Join(dir, "internal/db.go")},
			{ID: "ui", Name: "UI", Path: filepath.Join(dir, "internal/ui.go")},
		},
		Edges: []GraphEdge{
			{From: "auth", To: "db"},
			{From: "ui", To: "db"},
		},
	}
	s, _ := newTestState(t, "fix the authenticate token flow", enabledCfg(), func(ctx context.Context, p string) (Graph, error) {
		return graph, nil
	})

	dec := s.CheckFile(context.Background(), filepath.Join(dir, "internal/auth.go"))
	if !dec.Allowed {
		t.Fatalf("expected allow for top-ranked auth.go, got reject: %s", dec.Reason)
	}
	if dec.Score <= 0 {
		t.Fatalf("expected positive score, got %f", dec.Score)
	}
	if !strings.Contains(dec.Reason, "PageRank") {
		t.Fatalf("expected PageRank reason, got %q", dec.Reason)
	}

	m := s.Metrics()
	if m.AllowedScoreCount != 1 || m.AllowedScoreSum <= 0 {
		t.Fatalf("expected AllowedScoreCount=1 with positive sum, got %+v", m)
	}
	if m.AllowedScoreMin <= 0 || m.AllowedScoreMax <= 0 {
		t.Fatalf("expected positive min/max, got %+v", m)
	}
	if m.GraphLatencyMS < 0 || m.PageRankLatencyMS < 0 {
		t.Fatalf("latency must be non-negative, got %+v", m)
	}
}

func TestCheckFileRejectedBelowThreshold(t *testing.T) {
	s, dir := newTestState(t, "target", enabledCfg(), nil)
	// Inject graph after we know the workingDir.
	var nodes []GraphNode
	for i := 0; i < 10; i++ {
		nodes = append(nodes, GraphNode{
			ID:   "noise" + string(rune('a'+i)),
			Name: "noise",
			Path: filepath.Join(dir, "noise", string(rune('a'+i))+".go"),
		})
	}
	nodes = append(nodes, GraphNode{ID: "target", Name: "target", Path: filepath.Join(dir, "target.go")})
	s.queryGraph = func(ctx context.Context, p string) (Graph, error) {
		return Graph{Nodes: nodes}, nil
	}

	dec := s.CheckFile(context.Background(), filepath.Join(dir, "noise", "a.go"))
	if dec.Allowed {
		t.Fatalf("expected reject for low-ranked noise file, got allow with reason %q", dec.Reason)
	}
	if !strings.Contains(dec.Reason, "below PageRank threshold") {
		t.Fatalf("expected threshold reason, got %q", dec.Reason)
	}

	m := s.Metrics()
	if m.Rejected < 1 {
		t.Fatalf("expected at least one rejection, got %+v", m)
	}
	if m.LinesRejected <= 0 {
		t.Fatalf("expected LinesRejected > 0, got %+v", m)
	}
}

func TestCheckFileEmptyGraphFallsBackToAllow(t *testing.T) {
	s, dir := newTestState(t, "anything", enabledCfg(), func(ctx context.Context, p string) (Graph, error) {
		return Graph{}, nil
	})

	dec := s.CheckFile(context.Background(), filepath.Join(dir, "x.go"))
	if !dec.Allowed {
		t.Fatalf("expected fallback allow on empty graph, got reject: %s", dec.Reason)
	}
	if !strings.Contains(dec.Reason, "fallback") {
		t.Fatalf("expected fallback reason, got %q", dec.Reason)
	}
	m := s.Metrics()
	if m.GraphErrors == 0 {
		t.Fatalf("expected empty graph to count as a graph error, got %+v", m)
	}
	if m.FallbackBypasses == 0 {
		t.Fatalf("expected fallback bypass recorded, got %+v", m)
	}
}

func TestCheckFileGraphUnavailableFallsBackToAllow(t *testing.T) {
	calls := 0
	s, dir := newTestState(t, "anything", enabledCfg(), func(ctx context.Context, p string) (Graph, error) {
		calls++
		return Graph{}, errors.New("mcp down")
	})

	dec := s.CheckFile(context.Background(), filepath.Join(dir, "whatever.go"))
	if !dec.Allowed {
		t.Fatalf("expected fallback allow when graph unavailable, got reject: %s", dec.Reason)
	}
	if !strings.Contains(dec.Reason, "fallback") {
		t.Fatalf("expected fallback reason, got %q", dec.Reason)
	}
	if dec.MaxLines != 150 {
		t.Fatalf("expected MaxLines=150 on fallback, got %d", dec.MaxLines)
	}

	m := s.Metrics()
	if m.GraphErrors != 1 {
		t.Fatalf("expected GraphErrors=1, got %d", m.GraphErrors)
	}
	if m.FallbackBypasses != 1 {
		t.Fatalf("expected FallbackBypasses=1, got %d", m.FallbackBypasses)
	}

	// Second check must NOT re-query the graph (sync.Once) and must still allow.
	dec2 := s.CheckFile(context.Background(), filepath.Join(dir, "another.go"))
	if !dec2.Allowed {
		t.Fatalf("expected fallback allow on second check, got reject: %s", dec2.Reason)
	}
	if calls != 1 {
		t.Fatalf("expected graph queried exactly once, got %d", calls)
	}
	if m2 := s.Metrics(); m2.FallbackBypasses != 2 {
		t.Fatalf("expected FallbackBypasses=2, got %d", m2.FallbackBypasses)
	}
}

func TestCheckFileNormalizesRelativeAndAbsolutePaths(t *testing.T) {
	relPath := "internal/auth.go"
	var capturedAbs string
	var capturedDir string
	var s *State
	s, capturedDir = newTestState(t, "auth", enabledCfg(), nil)
	capturedAbs = filepath.Join(capturedDir, relPath)
	s.queryGraph = func(ctx context.Context, p string) (Graph, error) {
		return Graph{Nodes: []GraphNode{{ID: "auth", Name: "Auth", Path: capturedAbs}}}, nil
	}

	if dec := s.CheckFile(context.Background(), relPath); !dec.Allowed {
		t.Fatalf("expected allow for relative path %q (resolved against %s), got reject: %s", relPath, capturedDir, dec.Reason)
	}
	if dec := s.CheckFile(context.Background(), capturedAbs); !dec.Allowed {
		t.Fatalf("expected allow for absolute path %q, got reject: %s", capturedAbs, dec.Reason)
	}
}

func TestCheckFileMaxLinesClamps(t *testing.T) {
	cfg := enabledCfg()
	cfg.MaxLines = 42
	s, dir := newTestState(t, "target", cfg, func(ctx context.Context, p string) (Graph, error) {
		return Graph{}, nil
	})

	dec := s.CheckFile(context.Background(), filepath.Join(dir, "x.go"))
	if dec.MaxLines != 42 {
		t.Fatalf("expected MaxLines=42 from config, got %d", dec.MaxLines)
	}
}

func TestMetricsAccumulateAcrossChecks(t *testing.T) {
	// Prompt names auth.go by path so the first lookup is a direct-ref bypass.
	s, dir := newTestState(t, "look at internal/auth.go please", enabledCfg(), nil)
	absAuth := filepath.Join(dir, "internal/auth.go")
	absDB := filepath.Join(dir, "internal/db.go")
	absNoise := filepath.Join(dir, "noise/x.go")

	graph := Graph{
		Nodes: []GraphNode{
			{ID: "auth", Name: "Auth", Path: absAuth},
			{ID: "db", Name: "DB", Path: absDB},
			{ID: "ui", Name: "UI", Path: absNoise},
		},
		Edges: []GraphEdge{
			{From: "auth", To: "db"},
		},
	}
	cfg := s.cfg
	cfg.MaxFiles = 1 // only top file is allowed; others fall to reject path
	s.cfg = cfg
	s.queryGraph = func(ctx context.Context, p string) (Graph, error) {
		return graph, nil
	}

	// 1) direct ref: bypass (prompt mentions internal/auth.go)
	s.CheckFile(context.Background(), "internal/auth.go")
	// 2) ranked allow (auth still on top even though we re-check it; this call goes through PageRank)
	s.CheckFile(context.Background(), absDB) // db is referenced via auth edge, but with MaxFiles=1 only auth is allowed -> reject
	// Wait, we need a file that IS allowed but isn't a direct ref. The auth file is the direct-ref target.
	// Use a non-prompt-named file from the top-K. Since MaxFiles=1 and only auth is top-1, db is rejected.
	// Adjust: count this as a rejection, not an allow.

	m := s.Metrics()
	if m.Checks != 2 {
		t.Fatalf("expected Checks=2, got %d", m.Checks)
	}
	if m.Allowed != 1 {
		t.Fatalf("expected Allowed=1, got %d", m.Allowed)
	}
	if m.Rejected != 1 {
		t.Fatalf("expected Rejected=1, got %d", m.Rejected)
	}
	if m.DirectRefBypasses != 1 {
		t.Fatalf("expected DirectRefBypasses=1, got %d", m.DirectRefBypasses)
	}
	if m.AllowedScoreCount != 0 {
		t.Fatalf("expected AllowedScoreCount=0 (only direct-ref bypass occurred), got %d", m.AllowedScoreCount)
	}
	if m.LinesGranted != 150 || m.LinesRejected != 150 {
		t.Fatalf("unexpected line totals: granted=%d rejected=%d", m.LinesGranted, m.LinesRejected)
	}
	if m.GraphErrors != 0 {
		t.Fatalf("expected no graph errors, got %d", m.GraphErrors)
	}
}

func TestNilStateCheckFileAllows(t *testing.T) {
	var s *State
	dec := s.CheckFile(context.Background(), "anything")
	if !dec.Allowed {
		t.Fatalf("nil state must allow reads, got reject: %s", dec.Reason)
	}
	if dec.MaxLines != 200 {
		t.Fatalf("nil state MaxLines should fall back to 200, got %d", dec.MaxLines)
	}
	if m := s.Metrics(); m.Checks != 0 {
		t.Fatalf("nil state metrics should be zero-valued, got %+v", m)
	}
}

func TestRankedFilesReturnsStableOrder(t *testing.T) {
	s, dir := newTestState(t, "auth flow", enabledCfg(), nil)
	graph := Graph{
		Nodes: []GraphNode{
			{ID: "auth", Name: "Auth", Path: filepath.Join(dir, "a.go")},
			{ID: "db", Name: "DB", Path: filepath.Join(dir, "b.go")},
			{ID: "ui", Name: "UI", Path: filepath.Join(dir, "c.go")},
		},
		Edges: []GraphEdge{
			{From: "auth", To: "db"},
			{From: "ui", To: "db"},
		},
	}
	s.queryGraph = func(ctx context.Context, p string) (Graph, error) {
		return graph, nil
	}

	ranked := s.RankedFiles(context.Background())
	if len(ranked) != 3 {
		t.Fatalf("expected 3 ranked files, got %d", len(ranked))
	}
	if !strings.HasSuffix(ranked[0].Path, "a.go") {
		t.Fatalf("expected auth file ranked first, got %s", ranked[0].Path)
	}
	for i := 1; i < len(ranked); i++ {
		if ranked[i].Score > ranked[i-1].Score {
			t.Fatalf("ranked files not in descending score order: %+v", ranked)
		}
	}
}

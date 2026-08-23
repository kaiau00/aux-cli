package context

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/kaiau00/aux-cli/internal/logging"
)

type stateContextKey string

const retrievalStateKey stateContextKey = "semantic_retrieval_state"

type Decision struct {
	Allowed  bool
	Reason   string
	Score    float64
	MaxLines int
}

// Metrics summarizes the gate's decisions for one State lifetime. It is the
// primary signal we use to validate that semantic retrieval is net positive:
// gate decisions, line budgets granted vs. rejected, score distribution, and
// fallback / error counts.
type Metrics struct {
	Checks            int
	Allowed           int
	Rejected          int
	DirectRefBypasses int
	FallbackBypasses  int
	DisabledBypasses  int
	LinesGranted      int
	LinesRejected     int
	AllowedScoreMin   float64
	AllowedScoreMax   float64
	AllowedScoreSum   float64
	AllowedScoreCount int
	GraphLatencyMS    int64
	PageRankLatencyMS int64
	GraphErrors       int
}

type graphQuerier func(ctx context.Context, prompt string) (Graph, error)

type State struct {
	prompt     string
	workingDir string
	cfg        config.SemanticRetrievalConfig
	queryGraph graphQuerier

	once       sync.Once
	ranked     []RankedFile
	allowed    map[string]RankedFile
	directRefs map[string]struct{}
	err        error

	metricsMu sync.Mutex
	metrics   Metrics
}

func NewState(prompt string) *State {
	cfg := config.Get()
	if cfg == nil {
		return nil
	}
	return newStateWithConfig(prompt, cfg.WorkingDir, cfg.SemanticRetrieval)
}

func newStateWithConfig(prompt, workingDir string, cfg config.SemanticRetrievalConfig) *State {
	if !cfg.Enabled {
		return nil
	}
	return &State{
		prompt:     prompt,
		workingDir: workingDir,
		cfg:        cfg,
		queryGraph: QueryCodebaseGraph,
		directRefs: directFileReferences(prompt, workingDir),
	}
}

func WithState(ctx context.Context, state *State) context.Context {
	if state == nil {
		return ctx
	}
	return context.WithValue(ctx, retrievalStateKey, state)
}

func StateFromContext(ctx context.Context) *State {
	state, _ := ctx.Value(retrievalStateKey).(*State)
	return state
}

func (s *State) CheckFile(ctx context.Context, path string) Decision {
	if s == nil {
		return Decision{Allowed: true, Reason: "semantic retrieval disabled", MaxLines: 200}
	}
	if !s.cfg.Enabled {
		s.recordDecision(false, 0, true)
		return Decision{Allowed: true, Reason: "semantic retrieval disabled", MaxLines: s.maxLines()}
	}
	normalized := s.normalizePath(path)
	maxLines := s.maxLines()
	requestedLines := maxLines

	if _, ok := s.directRefs[normalized]; ok {
		s.recordDecision(true, requestedLines, false)
		s.recordDirectRefBypass()
		return Decision{Allowed: true, Reason: "file directly referenced by prompt", MaxLines: maxLines}
	}

	s.ensureRanked(ctx)
	if s.err != nil {
		logging.Debug("semantic retrieval unavailable; allowing bounded read", "error", s.err)
		s.recordDecision(true, requestedLines, false)
		s.recordFallbackBypass()
		return Decision{Allowed: true, Reason: "semantic graph unavailable; bounded fallback", MaxLines: maxLines}
	}
	if ranked, ok := s.allowed[normalized]; ok && ranked.Score >= s.cfg.MinScore {
		s.recordDecision(true, requestedLines, false)
		s.recordAllowedScore(ranked.Score)
		return Decision{Allowed: true, Reason: "file selected by PageRank", Score: ranked.Score, MaxLines: maxLines}
	}
	s.recordDecision(false, requestedLines, false)
	return Decision{Allowed: false, Reason: "file below PageRank threshold", MaxLines: maxLines}
}

func (s *State) RankedFiles(ctx context.Context) []RankedFile {
	if s == nil {
		return nil
	}
	s.ensureRanked(ctx)
	return s.ranked
}

// Metrics returns a snapshot of the gate's decisions since this State was
// created. Safe to call concurrently with CheckFile.
func (s *State) Metrics() Metrics {
	if s == nil {
		return Metrics{}
	}
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()
	return s.metrics
}

func (s *State) recordDecision(allowed bool, requestedLines int, disabled bool) {
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()
	s.metrics.Checks++
	if disabled {
		s.metrics.DisabledBypasses++
		s.metrics.Allowed++
		s.metrics.LinesGranted += requestedLines
		return
	}
	if allowed {
		s.metrics.Allowed++
		s.metrics.LinesGranted += requestedLines
		return
	}
	s.metrics.Rejected++
	s.metrics.LinesRejected += requestedLines
}

func (s *State) ensureRanked(ctx context.Context) {
	s.once.Do(func() {
		queryCtx, cancel := context.WithTimeout(ctx, time.Duration(s.cfg.TimeoutSeconds)*time.Second)
		defer cancel()

		querier := s.queryGraph
		if querier == nil {
			querier = QueryCodebaseGraph
		}

		graphStart := time.Now()
		graph, err := querier(queryCtx, s.prompt)
		graphLatency := time.Since(graphStart)
		s.recordGraphLatency(graphLatency)
		if err != nil {
			s.recordGraphError()
			s.err = err
			return
		}
		if len(graph.Nodes) == 0 {
			s.recordGraphError()
			s.err = fmt.Errorf("codebase memory returned no graph nodes")
			return
		}

		rankStart := time.Now()
		s.ranked = RankFiles(graph, s.prompt, PageRankOptions{
			Damping:       s.cfg.Damping,
			MaxIterations: s.cfg.MaxIterations,
			Epsilon:       s.cfg.Epsilon,
		})
		s.recordPageRankLatency(time.Since(rankStart))
		s.allowed = make(map[string]RankedFile)
		for i, file := range s.ranked {
			if i >= s.cfg.MaxFiles {
				break
			}
			normalized := s.normalizePath(file.Path)
			if normalized == "" {
				continue
			}
			s.allowed[normalized] = file
		}
	})
}

func (s *State) recordDirectRefBypass() {
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()
	s.metrics.DirectRefBypasses++
}

func (s *State) recordFallbackBypass() {
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()
	s.metrics.FallbackBypasses++
}

func (s *State) recordAllowedScore(score float64) {
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()
	if s.metrics.AllowedScoreCount == 0 || score < s.metrics.AllowedScoreMin {
		s.metrics.AllowedScoreMin = score
	}
	if s.metrics.AllowedScoreCount == 0 || score > s.metrics.AllowedScoreMax {
		s.metrics.AllowedScoreMax = score
	}
	s.metrics.AllowedScoreSum += score
	s.metrics.AllowedScoreCount++
}

func (s *State) recordGraphLatency(d time.Duration) {
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()
	s.metrics.GraphLatencyMS += d.Milliseconds()
}

func (s *State) recordPageRankLatency(d time.Duration) {
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()
	s.metrics.PageRankLatencyMS += d.Milliseconds()
}

func (s *State) recordGraphError() {
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()
	s.metrics.GraphErrors++
}

func (s *State) maxLines() int {
	if s == nil || s.cfg.MaxLines <= 0 {
		return 200
	}
	return s.cfg.MaxLines
}

func (s *State) normalizePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) && s != nil && s.workingDir != "" {
		path = filepath.Join(s.workingDir, path)
	}
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	return filepath.Clean(path)
}

func directFileReferences(prompt string, workingDir string) map[string]struct{} {
	refs := make(map[string]struct{})
	for _, term := range promptTerms(prompt) {
		if !strings.Contains(term, ".") && !strings.Contains(term, "/") {
			continue
		}
		path := term
		if !filepath.IsAbs(path) {
			path = filepath.Join(workingDir, path)
		}
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
		refs[filepath.Clean(path)] = struct{}{}
	}
	return refs
}

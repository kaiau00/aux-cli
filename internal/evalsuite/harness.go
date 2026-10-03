package evalsuite

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Harness is one agent CLI under measurement.
//
// The abstraction exists so the suite can compare Aux against other harnesses
// on identical tasks. That is a more useful question than comparing Aux to
// itself: it measures the harness rather than the model, provided both sides
// run the same model, which is the caller's responsibility to arrange.
type Harness interface {
	// Name identifies the harness in reports.
	Name() string
	// Command returns the shell command that runs one prompt non-interactively.
	Command(prompt string) string
	// Metrics extracts what the run cost. Some harnesses report this on stdout;
	// others need a durable store, which is why ctx and the raw output are both
	// available.
	Metrics(ctx context.Context, stdout string) (Usage, error)
}

// Usage is what one agent invocation consumed.
type Usage struct {
	InputTokens  int64
	OutputTokens int64
	Turns        int64
	Cost         float64
	CostUnknown  bool
}

// AuxHarness measures Aux. Aux does not report token counts on stdout, so the
// numbers come from its durable ledger, keyed by the session id it prints.
type AuxHarness struct {
	Binary   string
	Metrics_ MetricsReader
}

func (h AuxHarness) Name() string { return "aux" }

func (h AuxHarness) Command(prompt string) string {
	// JSON output is requested for the session id, which is the handle used to
	// read what the run cost from the ledger. --yes because a benchmark task
	// has to edit files and run commands, as `opencode run` does unprompted.
	return fmt.Sprintf("%s -p %s --quiet --output-format json --yes", h.Binary, shellQuote(prompt))
}

func (h AuxHarness) Metrics(ctx context.Context, stdout string) (Usage, error) {
	sessionID, err := sessionIDFrom(stdout)
	if err != nil {
		return Usage{CostUnknown: true}, err
	}
	if h.Metrics_ == nil {
		return Usage{CostUnknown: true}, fmt.Errorf("no metrics reader configured")
	}
	in, out, turns, cost, unknown, err := h.Metrics_.TaskMetrics(ctx, sessionID)
	if err != nil {
		return Usage{CostUnknown: true}, err
	}
	return Usage{InputTokens: in, OutputTokens: out, Turns: turns, Cost: cost, CostUnknown: unknown}, nil
}

// OpenCodeHarness measures opencode, which streams JSON events on stdout with
// per-step token counts, so no store lookup is needed.
type OpenCodeHarness struct {
	Binary string
	// Model is passed explicitly rather than left to opencode's config, so a
	// comparison cannot silently become a comparison of two different models.
	Model string
}

func (h OpenCodeHarness) Name() string { return "opencode" }

func (h OpenCodeHarness) Command(prompt string) string {
	cmd := fmt.Sprintf("%s run --format json", h.Binary)
	if h.Model != "" {
		cmd += " --model " + shellQuote(h.Model)
	}
	return cmd + " " + shellQuote(prompt)
}

// Metrics sums opencode's step_finish events.
//
// Cache reads are counted as input because they are input the model processed;
// excluding them would flatter any harness that caches well, which is exactly
// the effect a harness comparison is trying to measure honestly.
func (h OpenCodeHarness) Metrics(_ context.Context, stdout string) (Usage, error) {
	var u Usage
	var steps int64

	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var ev struct {
			Type string `json:"type"`
			Part struct {
				Tokens struct {
					Input     int64 `json:"input"`
					Output    int64 `json:"output"`
					Reasoning int64 `json:"reasoning"`
					Cache     struct {
						Read  int64 `json:"read"`
						Write int64 `json:"write"`
					} `json:"cache"`
				} `json:"tokens"`
				Cost float64 `json:"cost"`
			} `json:"part"`
		}
		if json.Unmarshal([]byte(line), &ev) != nil || ev.Type != "step_finish" {
			continue
		}
		steps++
		t := ev.Part.Tokens
		u.InputTokens += t.Input + t.Cache.Read + t.Cache.Write
		u.OutputTokens += t.Output + t.Reasoning
		u.Cost += ev.Part.Cost
	}

	if steps == 0 {
		// No step_finish means nothing was measured. Saying so beats reporting
		// a zero-token run, which would look like the cheapest in the suite.
		return Usage{CostUnknown: true}, fmt.Errorf("no step_finish events in opencode output")
	}
	u.Turns = steps
	return u, nil
}

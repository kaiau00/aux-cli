package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/kaiau00/aux-cli/internal/cost"
	"github.com/kaiau00/aux-cli/internal/db"
	"github.com/kaiau00/aux-cli/internal/evalsuite"
	"github.com/spf13/cobra"
)

// ledgerMetrics adapts the cost ledger to evalsuite's MetricsReader. Metrics
// come from the durable ledger rather than being parsed out of agent output, so
// a run's cost is measured the same way whether it was watched or not.
//
// It reconnects for every call rather than holding one connection for the
// whole suite run. Each task's isolation step removes and lets the agent
// recreate .aux (see runner.go), which is a different file on disk each time;
// a connection opened before that would keep reading the deleted one and see
// nothing.
type ledgerMetrics struct{}

func (m ledgerMetrics) TaskMetrics(ctx context.Context, sessionID string) (int64, int64, int64, float64, bool, error) {
	conn, err := db.Connect()
	if err != nil {
		return 0, 0, 0, 0, false, err
	}
	defer conn.Close()

	totals, err := cost.NewService(conn).SessionTotals(ctx, sessionID)
	if err != nil {
		return 0, 0, 0, 0, false, err
	}
	// Cached and cache-creation tokens count towards input, matching how the
	// opencode harness sums them. The ledger stores InputTokens net of cache, so
	// reporting it raw would measure the two harnesses differently: after
	// cached-token accounting was fixed, the same workload appeared to drop from
	// ~588k tokens to ~67k, which is the cache becoming visible rather than any
	// change in what was sent.
	//
	// Total prompt tokens is the right figure here because the benchmark asks
	// how much context a harness sends. What that costs is a separate question,
	// answered by Cost, which prices cached tokens at the cache rate.
	input := totals.InputTokens + totals.CacheReadTokens + totals.CacheCreationTokens
	return input, totals.OutputTokens, totals.Calls, totals.Cost, totals.CostUnknown, nil
}

var evalSuiteCmd = &cobra.Command{
	Use:   "suite <suite.json>",
	Short: "Run a task benchmark suite and record what it cost",
	Long: `Run a suite of real coding tasks and measure tokens, turns, and cost.

Each task resets its repository to a pinned revision, runs the agent
non-interactively, and then runs the task's success commands. Those commands
decide whether the task passed — not the agent's exit code, and not a model
judging its own work.

This spends real API budget. Run --validate first to check a suite without
executing anything.

To gate a change, record a baseline, make the change, run again, and compare:

  aux eval suite bench/suite.json --save bench/baseline.json
  aux eval suite bench/suite.json --save bench/candidate.json
  aux eval gate bench/baseline.json bench/candidate.json`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		suite, err := evalsuite.LoadSuite(args[0])
		if err != nil {
			return err
		}

		validateOnly, _ := cmd.Flags().GetBool("validate")
		if validateOnly {
			fmt.Printf("%s: %d task(s) valid, %d from real corrections.\n",
				suite.Name, len(suite.Tasks), suite.CorrectedCount())
			if suite.CorrectedCount() == 0 {
				fmt.Println("\nNote: no tasks are marked \"corrected\". A suite built only from" +
					"\ntasks the agent already handles measures what it is good at, not what" +
					"\nit gets wrong.")
			}
			return nil
		}

		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		if _, err := config.Load(cwd, false); err != nil {
			return err
		}
		conn, err := db.Connect()
		if err != nil {
			return err
		}
		defer conn.Close()

		binary, _ := cmd.Flags().GetString("binary")
		label, _ := cmd.Flags().GetString("label")
		timeout, _ := cmd.Flags().GetDuration("timeout")
		which, _ := cmd.Flags().GetString("harness")
		model, _ := cmd.Flags().GetString("model")
		repeat, _ := cmd.Flags().GetInt("repeat")

		var h evalsuite.Harness
		switch which {
		case "aux":
			if binary == "" {
				binary = "aux"
			}
			h = evalsuite.AuxHarness{Binary: binary, Metrics_: ledgerMetrics{}}
		case "opencode":
			if binary == "" {
				binary = "opencode"
			}
			h = evalsuite.OpenCodeHarness{Binary: binary, Model: model}
		default:
			return fmt.Errorf("unknown harness %q (want aux or opencode)", which)
		}

		if repeat < 2 {
			fmt.Println("Note: fewer than 2 runs cannot show a noise floor, and single-run" +
				"\ncomparisons of an agent are not reproducible. Use --repeat 3 or more" +
				"\nbefore drawing any conclusion from these numbers.")
		}

		runner := evalsuite.NewRunner(evalsuite.ShellExecutor{Timeout: timeout}, h)

		fmt.Printf("Running %s with %s: %d task(s) x %d run(s).\n\n",
			suite.Name, h.Name(), len(suite.Tasks), max(repeat, 1))
		series, err := runner.RunSeries(cmd.Context(), suite, label, repeat)
		if err != nil {
			return err
		}

		for _, r := range series.Runs {
			fmt.Print(evalsuite.RenderRun(r))
			fmt.Println()
		}
		fmt.Print(evalsuite.RenderSeries(series.Stats()))

		if path, _ := cmd.Flags().GetString("save"); path != "" {
			if err := series.Save(path); err != nil {
				return err
			}
			fmt.Printf("\nSaved to %s\n", path)
		}
		return nil
	},
}

var evalGateCmd = &cobra.Command{
	Use:   "gate <baseline.json> <candidate.json>",
	Short: "Decide whether a candidate run may ship",
	Long: `Compare a candidate suite run against a baseline.

Success rate is a hard floor: a candidate that uses fewer tokens while solving
fewer tasks fails, because that is not a cheaper harness, it is a worse one.
Token and turn budgets are only considered once capability is known to hold.

Exits non-zero when the gate fails, so it can be used in CI.`,
	Args: cobra.ExactArgs(2),
	// A failing gate is a verdict, not a usage error; printing the help text
	// after it buries the reason the change was rejected.
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		baseline, err := evalsuite.LoadRun(args[0])
		if err != nil {
			return err
		}
		candidate, err := evalsuite.LoadRun(args[1])
		if err != nil {
			return err
		}

		th := evalsuite.DefaultThresholds()
		if v, _ := cmd.Flags().GetFloat64("token-ratio"); v > 0 {
			th.TokenRatio = v
		}
		if v, _ := cmd.Flags().GetFloat64("turn-ratio"); v > 0 {
			th.TurnRatio = v
		}

		verdict := evalsuite.Gate(baseline, candidate, th)
		fmt.Print(evalsuite.RenderVerdict(verdict))
		if !verdict.Passed {
			// Silent failure in CI is the whole thing this is meant to prevent.
			return fmt.Errorf("gate failed")
		}
		return nil
	},
}

var evalCompareCmd = &cobra.Command{
	Use:   "compare <a.json> <b.json>",
	Short: "Compare two series against the noise each actually exhibits",
	Long: `Compare two saved series.

A difference smaller than the run-to-run noise floor is reported as
inconclusive rather than as a result. On this repository two runs of an
identical configuration differed by 49% in tokens, so an unqualified percentage
taken from single runs is a coin flip wearing a number.`,
	Args:         cobra.ExactArgs(2),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		a, err := evalsuite.LoadSeries(args[0])
		if err != nil {
			return err
		}
		b, err := evalsuite.LoadSeries(args[1])
		if err != nil {
			return err
		}
		fmt.Print(evalsuite.RenderSeriesComparison(evalsuite.CompareSeries(a, b)))
		return nil
	},
}

func init() {
	evalSuiteCmd.Flags().Bool("validate", false, "Check the suite without running anything")
	evalSuiteCmd.Flags().String("harness", "aux", "Which agent CLI to measure (aux, opencode)")
	evalSuiteCmd.Flags().String("binary", "", "Path to the agent binary (defaults to the harness name)")
	evalSuiteCmd.Flags().String("model", "", "Force a model, so a harness comparison does not become a model comparison")
	evalSuiteCmd.Flags().Int("repeat", 1, "Runs per configuration; 3+ is needed for any conclusion")
	evalSuiteCmd.Flags().String("label", "", "Label recorded with the run (e.g. \"paging-on\")")
	evalSuiteCmd.Flags().String("save", "", "Write the run to this path for later comparison")
	evalSuiteCmd.Flags().Duration("timeout", 10*time.Minute, "Per-command timeout")

	evalGateCmd.Flags().Float64("token-ratio", 0, "Token budget as a fraction of baseline (default 0.7)")
	evalGateCmd.Flags().Float64("turn-ratio", 0, "Turn ceiling as a multiple of baseline (default 1.1)")

	evalCmd.AddCommand(evalSuiteCmd)
	evalCmd.AddCommand(evalGateCmd)
	evalCmd.AddCommand(evalCompareCmd)
}

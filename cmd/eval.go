package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/kaiau00/aux-cli/internal/checkpoint"
	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/kaiau00/aux-cli/internal/cost"
	"github.com/kaiau00/aux-cli/internal/db"
	"github.com/kaiau00/aux-cli/internal/eval"
	"github.com/kaiau00/aux-cli/internal/eventstore"
	"github.com/kaiau00/aux-cli/internal/task"
	"github.com/kaiau00/aux-cli/internal/validation"
	"github.com/spf13/cobra"
)

// evalCmd runs the deterministic, network-free evaluation comparing the
// compatibility and paging prompt compilers over baseline fixtures.
var evalCmd = &cobra.Command{
	Use:   "eval",
	Short: "Run the local prompt-compiler evaluation (control vs optimized)",
}

var evalCompilerCmd = &cobra.Command{
	Use:   "compiler",
	Short: "Compare compatibility vs paging prompt compilation on baseline fixtures",
	RunE: func(cmd *cobra.Command, _ []string) error {
		fmt.Print(eval.RenderReport(eval.RunBaseline()))
		return nil
	},
}

var evalExperimentCmd = &cobra.Command{
	Use:   "experiment",
	Short: "Run and persist the compiler experiment (compatibility vs paging)",
	RunE: func(cmd *cobra.Command, _ []string) error {
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

		exp, results, err := eval.RunCompilerExperiment(context.Background(), eval.NewExperimentStore(conn), "")
		if err != nil {
			return err
		}
		fmt.Printf("Experiment %s persisted (%d runs).\n\n", exp.ID, len(results))
		fmt.Print(eval.RenderReport(results))
		return nil
	},
}

var evalReplayCmd = &cobra.Command{
	Use:   "replay <task-id>",
	Short: "Deterministically replay a task's state from its durable events",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
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

		ctx := context.Background()
		events, err := eventstore.NewService(conn).List(ctx, eventstore.Filter{TaskID: args[0]})
		if err != nil {
			return err
		}
		rt := eval.ReplayTaskState(args[0], events)
		fmt.Printf("Task %s replayed from %d events:\n", rt.TaskID, rt.Events)
		fmt.Printf("  status=%s turns=%d modelCalls=%d toolCalls=%d failures=%d validated=%v\n",
			rt.Status, rt.Turns, rt.ModelCalls, rt.ToolCalls, rt.Failures, rt.Validated)
		return nil
	},
}

var evalABCmd = &cobra.Command{
	Use:   "ab <baseline-task-id> <variant-task-id>",
	Short: "Compare accepted validated changes per dollar for two recorded runs",
	Long: "Computes the changes-per-dollar metric for a baseline and a variant task from " +
		"durable records (ledger, proof-of-done, checkpoints) and reports whether the variant improved. " +
		"Record the two runs on the same preferred model with the capability off vs on; " +
		"then compare them with this command.",
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
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

		stores := eval.ABStores{
			Tasks:       task.NewStore(conn),
			Validations: validation.NewService(validation.NewStore(conn), nil),
			Ledger:      cost.NewService(conn),
			Checkpoints: checkpoint.NewStore(conn),
		}
		name := fmt.Sprintf("A/B: %s vs %s", args[0], args[1])
		c, err := stores.CompareAndRecord(context.Background(), eval.NewExperimentStore(conn), "", name, args[0], args[1])
		if err != nil {
			return err
		}
		printRun := func(label string, m eval.RunMetrics) {
			fmt.Printf("  %-9s task=%s cost=$%.4f validated=%d changed=%d changes/$=%.3f%s\n",
				label, m.TaskID, m.Cost, m.ValidatedCriteria, m.ChangedFiles, m.ChangesPerDollar,
				map[bool]string{true: " (cost unknown)"}[m.CostUnknown])
		}
		fmt.Println("A/B comparison (accepted validated changes per dollar, same model):")
		printRun("baseline", c.Baseline)
		printRun("variant", c.Variant)
		fmt.Printf("  delta=%.3f improved=%v\n", c.Delta, c.Improved)
		fmt.Println("(persisted — visible on the dashboard's Optimization view)")
		return nil
	},
}

func init() {
	evalCmd.AddCommand(evalCompilerCmd)
	evalCmd.AddCommand(evalExperimentCmd)
	evalCmd.AddCommand(evalReplayCmd)
	evalCmd.AddCommand(evalABCmd)
	rootCmd.AddCommand(evalCmd)
}

package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/kaiau00/aux-cli/internal/cost"
	"github.com/kaiau00/aux-cli/internal/db"
	"github.com/kaiau00/aux-cli/internal/eventstore"
	"github.com/kaiau00/aux-cli/internal/toolexec"
	"github.com/spf13/cobra"
)

// costCmd reports a task's budget usage, trajectory, and waste warnings from
// durable runtime data.
var costCmd = &cobra.Command{
	Use:   "cost <task-id>",
	Short: "Show a task's budget usage, trajectory, and waste warnings",
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
		taskID := args[0]
		ledger := cost.NewService(conn)
		events := eventstore.NewService(conn)
		toolStore := toolexec.NewStore(conn)

		totals, err := ledger.TaskTotals(ctx, taskID)
		if err != nil {
			return err
		}
		evs, err := events.List(ctx, eventstore.Filter{TaskID: taskID})
		if err != nil {
			return err
		}
		execs, err := toolStore.ListByTask(ctx, taskID)
		if err != nil {
			return err
		}

		trajectory := cost.CompileTrajectory(taskID, evs, totals)
		waste := cost.DetectWaste(cost.WasteInput{ToolExecutions: execs})
		budget := cost.DefaultBudget(cost.ModeBalanced)
		usage := cost.Usage{
			InputTokens:  totals.PromptTokens,
			OutputTokens: totals.CompletionTokens,
			Cost:         totals.Cost,
			ToolCalls:    int64(trajectory.ToolCalls),
		}
		// Always compute the report in observe mode.
		assessment := cost.NewGovernor(cost.GovObserve).Assess(budget, usage, waste)

		if jsonOut, _ := cmd.Flags().GetBool("json"); jsonOut {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(map[string]any{"trajectory": trajectory, "assessment": assessment})
		}

		fmt.Printf("Task %s\n", taskID)
		fmt.Printf("Cost: $%.4f%s   Input: %d   Output: %d\n",
			totals.Cost, unknownFlag(totals.CostUnknown), totals.InputTokens, totals.OutputTokens)
		fmt.Printf("Model calls: %d   Tool calls: %d   Failures: %d\n", trajectory.ModelCalls, trajectory.ToolCalls, trajectory.Failures)
		fmt.Printf("Budget pressure: %.0f%%   Exhausted: %v\n", assessment.Pressure.Max*100, assessment.Exhausted)
		if len(assessment.Warnings) > 0 {
			fmt.Println("Waste warnings:")
			for _, w := range assessment.Warnings {
				fmt.Printf("  - [%s] %s\n", w.Detector, w.Detail)
			}
		}
		if len(assessment.Decisions) > 0 {
			fmt.Println("Governor decisions:")
			for _, d := range assessment.Decisions {
				fmt.Printf("  - %s: %s\n", d.Action, d.Reason)
			}
		}
		if len(assessment.DegradationPlan) > 0 {
			fmt.Println("Degradation plan:")
			for _, s := range assessment.DegradationPlan {
				fmt.Printf("  - %s\n", s)
			}
		}
		return nil
	},
}

func unknownFlag(unknown bool) string {
	if unknown {
		return " (cost_unknown)"
	}
	return ""
}

func init() {
	costCmd.Flags().Bool("json", false, "output as JSON")
	rootCmd.AddCommand(costCmd)
}

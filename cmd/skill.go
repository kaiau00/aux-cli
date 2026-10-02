package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/kaiau00/aux-cli/internal/db"
	"github.com/kaiau00/aux-cli/internal/eventstore"
	"github.com/kaiau00/aux-cli/internal/skill"
	"github.com/spf13/cobra"
)

func skillService() (*skill.Service, func() error, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, nil, err
	}
	if _, err := config.Load(cwd, false); err != nil {
		return nil, nil, err
	}
	conn, err := db.Connect()
	if err != nil {
		return nil, nil, err
	}
	svc := skill.NewService(skill.NewStore(conn), eventstore.NewService(conn))
	return svc, conn.Close, nil
}

// learnCmd records a workflow as a skill candidate. It
// never activates the skill — promotion requires a passing evaluation.
var learnCmd = &cobra.Command{
	Use:   "learn",
	Short: "Record a workflow as a skill candidate (activated only after evaluation)",
	RunE: func(cmd *cobra.Command, _ []string) error {
		// Past flag parsing: a failure here is a runtime problem, not command-line
		// misuse, and the usage dump would bury the line that matters. Same
		// reasoning as rootCmd.
		cmd.SilenceUsage = true

		name, _ := cmd.Flags().GetString("name")
		purpose, _ := cmd.Flags().GetString("purpose")
		if name == "" || purpose == "" {
			return fmt.Errorf("--name and --purpose are required")
		}
		svc, closer, err := skillService()
		if err != nil {
			return err
		}
		defer closer()

		sk, _, err := svc.Candidate(context.Background(), "user", "", skill.Content{Name: name, Purpose: purpose}, "aux_learn", nil)
		if err != nil {
			return err
		}
		fmt.Printf("Recorded skill candidate %q (%s).\n", sk.Name, sk.ID)
		fmt.Println("It is a candidate only — promotion requires a passing evaluation on held-out cases.")
		return nil
	},
}

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Inspect learned skills",
}

var skillListCmd = &cobra.Command{
	Use:   "list",
	Short: "List skill candidates and active skills",
	RunE: func(cmd *cobra.Command, _ []string) error {
		// Past flag parsing: a failure here is a runtime problem, not command-line
		// misuse, and the usage dump would bury the line that matters. Same
		// reasoning as rootCmd.
		cmd.SilenceUsage = true

		svc, closer, err := skillService()
		if err != nil {
			return err
		}
		defer closer()
		ctx := context.Background()
		candidates, err := svc.Candidates(ctx)
		if err != nil {
			return fmt.Errorf("failed to list candidate skills: %w", err)
		}
		active, err := svc.Active(ctx)
		if err != nil {
			return fmt.Errorf("failed to list active skills: %w", err)
		}
		fmt.Printf("Active skills (%d):\n", len(active))
		for _, s := range active {
			fmt.Printf("  - %s  (%s)\n", s.Name, s.ID)
		}
		// Say which candidates are actually promotable. Without this the only
		// way to find out is to run promote and be refused.
		fmt.Printf("Candidate skills (%d, awaiting evaluation):\n", len(candidates))
		for _, s := range candidates {
			status := "no passing evaluation yet"
			if ver, ok, err := svc.LatestVersion(ctx, s.ID); err == nil && ok {
				if passing, err := svc.HasPassingEvaluation(ctx, ver.ID); err == nil && passing {
					status = "ready to promote"
				}
			}
			fmt.Printf("  - %s  (%s) — %s\n", s.Name, s.ID, status)
		}
		// Rolled-back skills keep their version history precisely so they can be
		// re-promoted with fresh evidence. Omitting them from the only listing
		// command would make that impossible to act on.
		rolledBack, err := svc.RolledBack(ctx)
		if err != nil {
			return fmt.Errorf("failed to list rolled-back skills: %w", err)
		}
		if len(rolledBack) > 0 {
			fmt.Printf("Rolled back (%d, re-promotable with fresh evidence):\n", len(rolledBack))
			for _, s := range rolledBack {
				fmt.Printf("  - %s  (%s)\n", s.Name, s.ID)
			}
		}
		return nil
	},
}

// skillEvaluateCmd records the evidence that promotion requires. Without it the
// lifecycle terminates one step short: candidates accumulate, Promote keeps
// refusing for want of an evaluation, and nothing can supply one.
var skillEvaluateCmd = &cobra.Command{
	Use:   "evaluate <skill-id>",
	Short: "Record an evaluation result for a skill's latest version",
	Long: "Records a baseline-vs-candidate evaluation outcome against a skill's latest version.\n\n" +
		"Only a 'pass' unlocks promotion. Nothing here measures anything: this records a result you " +
		"obtained elsewhere (for example with `aux eval suite`), which is why --eval-run and --metrics " +
		"exist — they are the trail back to the evidence.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Past flag parsing: a failure here is a runtime problem, not command-line
		// misuse, and the usage dump would bury the line that matters. Same
		// reasoning as rootCmd.
		cmd.SilenceUsage = true

		skillID := args[0]
		resultFlag, _ := cmd.Flags().GetString("result")
		baseline, _ := cmd.Flags().GetString("baseline")
		evalRun, _ := cmd.Flags().GetString("eval-run")
		metrics, _ := cmd.Flags().GetString("metrics")

		result, err := skill.ParseEvalResult(resultFlag)
		if err != nil {
			return err
		}

		svc, closer, err := skillService()
		if err != nil {
			return err
		}
		defer closer()
		ctx := context.Background()

		ver, ok, err := svc.LatestVersion(ctx, skillID)
		if err != nil {
			return fmt.Errorf("failed to look up the skill's latest version: %w", err)
		}
		if !ok {
			return fmt.Errorf("no version found for skill %s — check the id with `aux skill list`", skillID)
		}

		if err := svc.Evaluate(ctx, ver.ID, baseline, evalRun, result, metrics); err != nil {
			return fmt.Errorf("failed to record the evaluation: %w", err)
		}

		fmt.Printf("Recorded %s for version %s of skill %s.\n", result, ver.ID, skillID)
		if result == skill.EvalPass {
			fmt.Println("Promotion is now unlocked: `aux skill promote " + skillID + "`.")
		} else {
			fmt.Println("Promotion stays blocked — only a passing evaluation unlocks it.")
		}
		return nil
	},
}

var skillPromoteCmd = &cobra.Command{
	Use:   "promote <skill-id>",
	Short: "Activate a skill whose latest version has a passing evaluation",
	Long: "Activates a skill, refusing unless its latest version has a passing evaluation on record. " +
		"The prior version is retained as a rollback target.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Past flag parsing: a failure here is a runtime problem, not command-line
		// misuse, and the usage dump would bury the line that matters. Same
		// reasoning as rootCmd.
		cmd.SilenceUsage = true

		skillID := args[0]
		svc, closer, err := skillService()
		if err != nil {
			return err
		}
		defer closer()
		ctx := context.Background()

		ver, ok, err := svc.LatestVersion(ctx, skillID)
		if err != nil {
			return fmt.Errorf("failed to look up the skill's latest version: %w", err)
		}
		if !ok {
			return fmt.Errorf("no version found for skill %s — check the id with `aux skill list`", skillID)
		}

		if err := svc.Promote(ctx, skillID, ver.ID); err != nil {
			if errors.Is(err, skill.ErrNoEvaluationEvidence) {
				// Say how to satisfy the gate rather than only that it refused.
				return fmt.Errorf("%w\n\nRecord one with:\n  aux skill evaluate %s --result pass --eval-run <id>", err, skillID)
			}
			return err
		}
		fmt.Printf("Promoted skill %s (version %s) to active.\n", skillID, ver.ID)
		return nil
	},
}

var skillRollbackCmd = &cobra.Command{
	Use:   "rollback <skill-id>",
	Short: "Demote an active skill, keeping its version history",
	Long: "Demotes an active skill in response to a regression. Version history is preserved, so a " +
		"version can be re-promoted once it has fresh passing evidence.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Past flag parsing: a failure here is a runtime problem, not command-line
		// misuse, and the usage dump would bury the line that matters. Same
		// reasoning as rootCmd.
		cmd.SilenceUsage = true

		svc, closer, err := skillService()
		if err != nil {
			return err
		}
		defer closer()
		if err := svc.Rollback(context.Background(), args[0]); err != nil {
			return err
		}
		fmt.Printf("Rolled back skill %s. Its versions are retained.\n", args[0])
		return nil
	},
}

func init() {
	learnCmd.Flags().String("name", "", "skill name")
	learnCmd.Flags().String("purpose", "", "what the workflow accomplishes")

	skillEvaluateCmd.Flags().String("result", "", "evaluation outcome: pass, fail, or inconclusive (required)")
	skillEvaluateCmd.Flags().String("baseline", "", "version id the candidate was compared against")
	skillEvaluateCmd.Flags().String("eval-run", "", "id of the evaluation run this result came from")
	skillEvaluateCmd.Flags().String("metrics", "", "metrics as JSON, kept as the evidence trail")
	_ = skillEvaluateCmd.MarkFlagRequired("result")

	skillCmd.AddCommand(skillListCmd)
	skillCmd.AddCommand(skillEvaluateCmd)
	skillCmd.AddCommand(skillPromoteCmd)
	skillCmd.AddCommand(skillRollbackCmd)
	rootCmd.AddCommand(learnCmd)
	rootCmd.AddCommand(skillCmd)
}

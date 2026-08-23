package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/kaiau00/aux-cli/internal/db"
	"github.com/kaiau00/aux-cli/internal/permission"
	"github.com/kaiau00/aux-cli/internal/profile"
	"github.com/kaiau00/aux-cli/internal/project"
	"github.com/kaiau00/aux-cli/internal/task"
	"github.com/kaiau00/aux-cli/internal/validation"
	"github.com/spf13/cobra"
)

// cliApprover approves validation commands on the terminal. The TUI has its own
// permission dialog; this is the non-interactive path, where --yes is the
// user's explicit, up-front consent to run the project's own commands.
type cliApprover struct{ assumeYes bool }

func (a cliApprover) Request(opts permission.CreatePermissionRequest) bool {
	if a.assumeYes {
		fmt.Printf("  running: %s\n", opts.Fingerprint)
		return true
	}
	fmt.Printf("  skipped (needs --yes): %s\n", opts.Fingerprint)
	return false
}

var validateCmd = &cobra.Command{
	Use:   "validate <task-id>",
	Short: "Run the project's validation commands and record proof-of-done evidence",
	Long: "Runs the effective profile's test and build commands for a task's acceptance criteria, " +
		"records each run as executable evidence, and prints the resulting proof-of-done state.\n\n" +
		"Commands come from the project profile, which is derived by scanning repo content, so each " +
		"one must be approved before it runs; pass --yes to approve them all up front.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		taskID := args[0]
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
		tasks := task.NewStore(conn)
		spec, ok, err := tasks.LatestSpec(ctx, taskID)
		if err != nil {
			return fmt.Errorf("failed to read task spec: %w", err)
		}
		if !ok || len(spec.AcceptanceCriteria) == 0 {
			return fmt.Errorf("task %s has no acceptance criteria to validate", taskID)
		}

		projects := project.NewService(project.NewStore(conn), project.GitVCS{})
		res, err := projects.Resolve(ctx, config.WorkingDirectory())
		if err != nil {
			return fmt.Errorf("failed to resolve project: %w", err)
		}
		pstore := profile.NewStore(conn)
		profiles := profile.NewService(pstore, profile.NewBuilder(pstore, profile.DefaultScanners()))
		eff, err := profiles.CompileEffective(ctx, res.Project.ID, res.Revision.ID, res.Root.CanonicalPath, res.Revision.VCSRevision, "")
		if err != nil {
			return fmt.Errorf("failed to compile profile: %w", err)
		}

		var specs []validation.CommandSpec
		for _, c := range eff.ValidationCommands() {
			specs = append(specs, validation.CommandSpec{Key: c.Key, Command: c.Command, ValidatorType: c.Type})
		}
		criterionIDs := make([]string, 0, len(spec.AcceptanceCriteria))
		for _, c := range spec.AcceptanceCriteria {
			criterionIDs = append(criterionIDs, c.ID)
		}
		intents := validation.PlanIntents(specs, criterionIDs)
		if len(intents) == 0 {
			return fmt.Errorf("no validation commands found in the project profile")
		}

		assumeYes, _ := cmd.Flags().GetBool("yes")
		svc := validation.NewService(validation.NewStore(conn), nil)
		runner := validation.ShellRunner{
			WorkDir:   config.WorkingDirectory(),
			SessionID: "cli:" + taskID,
			Approver:  cliApprover{assumeYes: assumeYes},
		}

		fmt.Printf("Validating task %s (%d criteria, %d commands):\n", taskID, len(criterionIDs), len(intents))
		for _, intent := range intents {
			result, err := svc.RunIntent(ctx, taskID, intent, res.Revision.VCSRevision, runner)
			switch {
			case err != nil:
				fmt.Printf("  %-8s %s (%v)\n", "error", intent.Command, err)
			case result.Cached:
				fmt.Printf("  %-8s %s (cached)\n", "pass", intent.Command)
			default:
				fmt.Printf("  %-8s %s\n", result.Run.Status, intent.Command)
			}
		}

		states, err := svc.ProofOfDone(ctx, taskID, criterionIDs)
		if err != nil {
			return fmt.Errorf("failed to compute proof of done: %w", err)
		}
		fmt.Println("\nProof of done:")
		for _, c := range spec.AcceptanceCriteria {
			fmt.Printf("  %-12s %s\n", states[c.ID], c.Description)
		}
		return nil
	},
}

func init() {
	validateCmd.Flags().Bool("yes", false, "approve every validation command without prompting")
	rootCmd.AddCommand(validateCmd)
}

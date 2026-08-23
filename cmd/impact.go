package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/kaiau00/aux-cli/internal/db"
	"github.com/kaiau00/aux-cli/internal/impact"
	"github.com/kaiau00/aux-cli/internal/project"
	"github.com/spf13/cobra"
)

// impactCmd is a read-only surface over the change-impact graph (§8.6/§8.7).
var impactCmd = &cobra.Command{
	Use:   "impact <changed-path>...",
	Short: "Show the change impact (dependents, affected packages, tests) for paths",
	Args:  cobra.MinimumNArgs(1),
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
		projects := project.NewService(project.NewStore(conn), project.GitVCS{})
		res, err := projects.Resolve(ctx, config.WorkingDirectory())
		if err != nil {
			return fmt.Errorf("failed to resolve project: %w", err)
		}

		svc := impact.NewService(impact.NewStore(conn))
		// Ensure the graph exists (index if this is the first run). A read error
		// here is surfaced rather than folded into "not indexed", which would
		// silently trigger a full reindex on every run.
		st, ok, err := impact.NewStore(conn).GetIndexState(ctx, res.Project.ID)
		if err != nil {
			return fmt.Errorf("failed to read impact index state: %w", err)
		}
		if !ok || st.Status != "indexed" {
			if _, err := svc.Index(ctx, res.Project.ID, res.Root.CanonicalPath, res.Revision.VCSRevision); err != nil {
				return fmt.Errorf("failed to build impact graph: %w", err)
			}
		}

		result, err := svc.Analyze(ctx, res.Project.ID, res.Revision.VCSRevision, args)
		if err != nil {
			return err
		}

		if jsonOut, _ := cmd.Flags().GetBool("json"); jsonOut {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(result)
		}

		fmt.Printf("Impact of %v\n", result.ChangedPaths)
		fmt.Printf("Risk: %s   Uncertainty: %.2f   Broaden validation: %v\n", result.Risk, result.Uncertainty, result.BroadenValidation)
		printList("Affected packages", result.AffectedPackages)
		printList("Direct dependents", result.DirectDependents)
		if len(result.RelatedTests) > 0 {
			fmt.Println("Related tests:")
			for _, tr := range result.RelatedTests {
				fmt.Printf("  - %s (%s)\n", tr.Path, tr.Reason)
			}
		}
		fmt.Printf("Recommended validation (%s):\n", result.Reason)
		for _, c := range result.Recommended {
			fmt.Printf("  - %s\n", c)
		}
		return nil
	},
}

func printList(title string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Println(title + ":")
	for _, it := range items {
		fmt.Printf("  - %s\n", it)
	}
}

func init() {
	impactCmd.Flags().Bool("json", false, "output as JSON")
	rootCmd.AddCommand(impactCmd)
}

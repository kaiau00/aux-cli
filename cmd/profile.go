package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/kaiau00/aux-cli/internal/db"
	"github.com/kaiau00/aux-cli/internal/profile"
	"github.com/kaiau00/aux-cli/internal/project"
	"github.com/spf13/cobra"
)

// profileCmd is a read-only surface over profile layers (roadmapplan.md §6.7).
var profileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Inspect project profile layers",
}

var profileShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the project profile layer, or the merged effective profile with --effective",
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

		ctx := context.Background()
		projects := project.NewService(project.NewStore(conn), project.GitVCS{})
		res, err := projects.Resolve(ctx, config.WorkingDirectory())
		if err != nil {
			return fmt.Errorf("failed to resolve project: %w", err)
		}

		pstore := profile.NewStore(conn)
		profiles := profile.NewService(pstore, profile.NewBuilder(pstore, profile.DefaultScanners()))

		jsonOut, _ := cmd.Flags().GetBool("json")
		effective, _ := cmd.Flags().GetBool("effective")

		if effective {
			eff, err := profiles.CompileEffective(ctx, res.Project.ID, res.Revision.ID, res.Root.CanonicalPath, res.Revision.VCSRevision, "")
			if err != nil {
				return fmt.Errorf("failed to compile profile: %w", err)
			}
			if jsonOut {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(eff)
			}
			fmt.Printf("Effective profile for %s: %d entries, ~%d manifest tokens\n", res.Project.CanonicalName, len(eff.Entries), eff.TokenEstimate)
			if conflicts := eff.Conflicts(); len(conflicts) > 0 {
				fmt.Printf("Conflicts (%d):\n", len(conflicts))
				for _, c := range conflicts {
					fmt.Printf("  [%s] %s (winner=%s/%s)\n", c.Type, c.Key, c.OwnerType, c.SourceRef)
					for _, ov := range c.Overrides {
						fmt.Printf("      overrides: %s/%s\n", ov.OwnerType, ov.SourceRef)
					}
				}
			}
			fmt.Println("\n--- Manifest ---")
			fmt.Print(eff.Manifest)
			return nil
		}

		version, entries, err := profiles.CompileProject(ctx, res.Project.ID, res.Root.CanonicalPath, res.Revision.VCSRevision)
		if err != nil {
			return fmt.Errorf("failed to compile project profile: %w", err)
		}
		if jsonOut {
			out := map[string]any{"version": version, "entries": entries}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(out)
		}
		fmt.Printf("Project profile layer for %s: %d entries (reused=%v)\n", res.Project.CanonicalName, len(entries), version.Reused)
		for _, e := range entries {
			fmt.Printf("  - [%s] %s (source=%s, confidence=%.2f)\n", e.Type, e.Key, e.SourceType, e.Confidence)
		}
		return nil
	},
}

func init() {
	profileShowCmd.Flags().Bool("effective", false, "show the merged effective profile instead of just the project layer")
	profileShowCmd.Flags().Bool("json", false, "output as JSON")
	profileCmd.AddCommand(profileShowCmd)
	rootCmd.AddCommand(profileCmd)
}

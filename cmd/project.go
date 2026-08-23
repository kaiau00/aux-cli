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
	"github.com/kaiau00/aux-cli/internal/relatedproject"
	"github.com/spf13/cobra"
)

// projectCmd is a read-only surface over resolved project identity and the
// compiled effective profile (roadmapplan.md §6.7 / §19 PR 5).
var projectCmd = &cobra.Command{
	Use:   "project",
	Short: "Inspect the resolved project identity and profile",
}

var projectShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the current project, revision, and compiled profile entries",
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
		eff, err := profiles.CompileEffective(ctx, res.Project.ID, res.Revision.ID, res.Root.CanonicalPath, res.Revision.VCSRevision, "")
		if err != nil {
			return fmt.Errorf("failed to compile profile: %w", err)
		}

		jsonOut, _ := cmd.Flags().GetBool("json")
		if jsonOut {
			out := map[string]any{
				"project":  res.Project,
				"root":     res.Root,
				"revision": res.Revision,
				"profile":  eff,
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(out)
		}

		fmt.Printf("Project:  %s (%s)\n", res.Project.CanonicalName, res.Project.VCSType)
		fmt.Printf("Root:     %s\n", res.Root.CanonicalPath)
		fmt.Printf("Revision: %s", short(res.Revision.VCSRevision))
		if res.Revision.BranchName != "" {
			fmt.Printf(" (%s)", res.Revision.BranchName)
		}
		if res.Revision.DirtyTreeHash != "" {
			fmt.Printf(" [dirty]")
		}
		fmt.Println()
		fmt.Printf("Effective profile: %d entries, ~%d manifest tokens\n", len(eff.Entries), eff.TokenEstimate)
		for _, e := range eff.Entries {
			conflict := ""
			if e.Conflict {
				conflict = " [conflict]"
			}
			fmt.Printf("  - [%s] %s (owner=%s, source=%s, confidence=%.2f)%s\n", e.Type, e.Key, e.OwnerType, e.SourceType, e.Confidence, conflict)
		}
		fmt.Println("\n--- Manifest ---")
		fmt.Print(eff.Manifest)
		return nil
	},
}

var projectRefreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Rescan the project and recompile the effective profile, reporting what changed",
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

		prev, hadPrev, err := pstore.GetLatestEffective(ctx, res.Project.ID, "")
		if err != nil {
			return fmt.Errorf("failed to load prior effective profile: %w", err)
		}

		eff, err := profiles.CompileEffective(ctx, res.Project.ID, res.Revision.ID, res.Root.CanonicalPath, res.Revision.VCSRevision, "")
		if err != nil {
			return fmt.Errorf("failed to recompile profile: %w", err)
		}

		var added, removed, changed []string
		if hadPrev {
			added, removed, changed = profile.DiffEffective(prev, eff)
		}

		jsonOut, _ := cmd.Flags().GetBool("json")
		if jsonOut {
			out := map[string]any{
				"profile": eff,
				"added":   added,
				"removed": removed,
				"changed": changed,
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(out)
		}

		fmt.Printf("Refreshed profile for %s: %d entries, ~%d manifest tokens\n", res.Project.CanonicalName, len(eff.Entries), eff.TokenEstimate)
		if !hadPrev {
			fmt.Println("No prior profile recorded; this is the first compile.")
			return nil
		}
		if len(added)+len(removed)+len(changed) == 0 {
			fmt.Println("No changes since last compile.")
			return nil
		}
		for _, k := range added {
			fmt.Printf("  + %s\n", k)
		}
		for _, k := range removed {
			fmt.Printf("  - %s\n", k)
		}
		for _, k := range changed {
			fmt.Printf("  ~ %s\n", k)
		}
		return nil
	},
}

var projectRelatedCmd = &cobra.Command{
	Use:   "related",
	Short: "List related-project edges derived from the current project's dependencies",
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

		related := relatedproject.NewStore(conn)
		from, err := related.From(ctx, res.Project.ID)
		if err != nil {
			return fmt.Errorf("failed to list outgoing relations: %w", err)
		}
		to, err := related.To(ctx, res.Project.ID)
		if err != nil {
			return fmt.Errorf("failed to list incoming relations: %w", err)
		}

		jsonOut, _ := cmd.Flags().GetBool("json")
		if jsonOut {
			out := map[string]any{"dependsOn": from, "dependedOnBy": to}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(out)
		}

		fmt.Printf("Related projects for %s:\n", res.Project.CanonicalName)
		if len(from) == 0 && len(to) == 0 {
			fmt.Println("  (none known yet — run this after aux has resolved the project at least once)")
			return nil
		}
		if len(from) > 0 {
			fmt.Println("Depends on:")
			for _, r := range from {
				fmt.Printf("  - [%s] %s (via %s)\n", r.RelationType, r.ToProject, r.Source)
			}
		}
		if len(to) > 0 {
			fmt.Println("Depended on by:")
			for _, r := range to {
				fmt.Printf("  - [%s] %s (via %s)\n", r.RelationType, r.FromProject, r.Source)
			}
		}
		return nil
	},
}

func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	if s == "" {
		return "(none)"
	}
	return s
}

func init() {
	projectShowCmd.Flags().Bool("json", false, "output as JSON")
	projectRefreshCmd.Flags().Bool("json", false, "output as JSON")
	projectRelatedCmd.Flags().Bool("json", false, "output as JSON")
	projectCmd.AddCommand(projectShowCmd)
	projectCmd.AddCommand(projectRefreshCmd)
	projectCmd.AddCommand(projectRelatedCmd)
	rootCmd.AddCommand(projectCmd)
}

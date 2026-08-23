package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/kaiau00/aux-cli/internal/db"
	"github.com/kaiau00/aux-cli/internal/eventstore"
	"github.com/kaiau00/aux-cli/internal/ids"
	"github.com/kaiau00/aux-cli/internal/multirepo"
	"github.com/kaiau00/aux-cli/internal/profile"
	"github.com/kaiau00/aux-cli/internal/project"
	"github.com/kaiau00/aux-cli/internal/relatedproject"
	"github.com/kaiau00/aux-cli/internal/task"
	"github.com/spf13/cobra"
)

// taskCmd is a read-only surface over first-class tasks (roadmapplan.md §6.7).
var taskCmd = &cobra.Command{
	Use:   "task",
	Short: "Inspect first-class tasks and their specs",
}

var taskShowCmd = &cobra.Command{
	Use:   "show <task-id>",
	Short: "Show a task, its compiled spec, and acceptance criteria",
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
		store := task.NewStore(conn)
		tk, err := store.GetTask(ctx, args[0])
		if err != nil {
			return fmt.Errorf("task not found: %w", err)
		}
		spec, _, err := store.LatestSpec(ctx, tk.ID)
		if err != nil {
			return err
		}

		if jsonOut, _ := cmd.Flags().GetBool("json"); jsonOut {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(map[string]any{"task": tk, "spec": spec})
		}

		fmt.Printf("Task:      %s\n", tk.ID)
		fmt.Printf("Objective: %s\n", tk.Objective)
		fmt.Printf("Mode:      %s\n", tk.Mode)
		fmt.Printf("Status:    %s\n", tk.Status)
		if len(spec.Constraints) > 0 {
			fmt.Println("Constraints:")
			for _, c := range spec.Constraints {
				fmt.Printf("  - %s\n", c)
			}
		}
		if len(spec.AcceptanceCriteria) > 0 {
			fmt.Println("Acceptance criteria:")
			for _, c := range spec.AcceptanceCriteria {
				fmt.Printf("  - [%s] %s\n", c.State, c.Description)
			}
		}
		if len(spec.ValidationIntents) > 0 {
			fmt.Println("Validation intents:")
			for _, v := range spec.ValidationIntents {
				fmt.Printf("  - %s: %s\n", v.Intent, v.Command)
			}
		}
		return nil
	},
}

var taskBeginCmd = &cobra.Command{
	Use:   "begin <objective>",
	Short: "Begin a task; pass --repo more than once to compile a multi-repository task (roadmapplan.md §11.4)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		objective := args[0]

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
		events := eventstore.NewService(conn)
		projects := project.NewService(project.NewStore(conn), project.GitVCS{})
		profiles := profile.NewService(profile.NewStore(conn), profile.NewBuilder(profile.NewStore(conn), profile.DefaultScanners()))
		taskStore := task.NewStore(conn)
		coord := task.NewCoordinator(projects, profiles, taskStore, events, config.WorkingDirectory())

		jsonOut, _ := cmd.Flags().GetBool("json")
		repoPaths, _ := cmd.Flags().GetStringArray("repo")
		autoRelated, _ := cmd.Flags().GetBool("auto-related")
		sessionID := "cli-" + ids.New()

		if len(repoPaths) == 0 && !autoRelated {
			_, taskID, err := coord.Begin(ctx, sessionID, objective)
			if err != nil {
				return fmt.Errorf("failed to begin task: %w", err)
			}
			if jsonOut {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(map[string]any{"taskId": taskID})
			}
			fmt.Printf("Began task %s\n", taskID)
			return nil
		}

		targets := make([]multirepo.RepoTarget, 0, len(repoPaths)+1)
		var currentProjectID string
		if here, err := projects.Resolve(ctx, config.WorkingDirectory()); err == nil {
			currentProjectID = here.Project.ID
			targets = append(targets, multirepo.RepoTarget{
				ProjectID: here.Project.ID,
				Name:      here.Project.CanonicalName,
				Root:      here.Root.CanonicalPath,
			})
		}
		for _, p := range repoPaths {
			abs, err := filepath.Abs(p)
			if err != nil {
				return fmt.Errorf("failed to resolve repo path %q: %w", p, err)
			}
			if info, statErr := os.Stat(abs); statErr != nil || !info.IsDir() {
				return fmt.Errorf("--repo %q is not a directory", p)
			}
			res, err := projects.Resolve(ctx, abs)
			if err != nil {
				return fmt.Errorf("failed to resolve project at %s: %w", abs, err)
			}
			targets = append(targets, multirepo.RepoTarget{
				ProjectID: res.Project.ID,
				Name:      res.Project.CanonicalName,
				Root:      res.Root.CanonicalPath,
			})
		}
		if autoRelated && currentProjectID != "" {
			relStore := relatedproject.NewStore(conn)
			rels, err := relStore.From(ctx, currentProjectID)
			if err != nil {
				return fmt.Errorf("failed to load related projects: %w", err)
			}
			for _, r := range rels {
				// Every relation type is a candidate target: today's only automated
				// derivation (module dependencies, roadmapplan.md §11.2) produces
				// library_consumer edges, and a lib+consumer cross-repo change is
				// exactly the kind of task --auto-related exists for.
				roots, err := projects.Store().ListRoots(ctx, r.ToProject)
				if err != nil || len(roots) == 0 {
					continue
				}
				relatedProj, err := projects.Store().GetProject(ctx, r.ToProject)
				if err != nil {
					continue
				}
				targets = append(targets, multirepo.RepoTarget{
					ProjectID: r.ToProject,
					Name:      relatedProj.CanonicalName,
					Root:      roots[0].CanonicalPath,
				})
			}
		}
		if len(targets) < 2 {
			return fmt.Errorf("multi-repo task needs at least 2 resolvable repositories; got %d (use --repo to add more, or check --auto-related has known edges via `aux project related`)", len(targets))
		}

		plan, children, err := coord.BeginMultiRepo(ctx, sessionID, objective, targets)
		if jsonOut {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			encErr := ""
			if err != nil {
				encErr = err.Error()
			}
			return enc.Encode(map[string]any{"plan": plan, "children": children, "error": encErr})
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		}
		fmt.Printf("Began multi-repo task across %d/%d repositories:\n", len(children), len(targets))
		for _, c := range children {
			fmt.Printf("  - [%s] %s -> task %s\n", c.ProjectID, c.Objective, c.ID)
		}
		if len(children) == 0 {
			return err
		}
		return nil
	},
}

func init() {
	taskShowCmd.Flags().Bool("json", false, "output as JSON")
	taskBeginCmd.Flags().Bool("json", false, "output as JSON")
	taskBeginCmd.Flags().StringArray("repo", nil, "path to a repository participating in this task; pass more than once for a multi-repo task")
	taskBeginCmd.Flags().Bool("auto-related", false, "also target repositories related to the current project's dependency graph; see `aux project related`")
	taskCmd.AddCommand(taskShowCmd)
	taskCmd.AddCommand(taskBeginCmd)
	rootCmd.AddCommand(taskCmd)
}

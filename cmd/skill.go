package cmd

import (
	"context"
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

// learnCmd records a workflow as a skill candidate (roadmapplan.md §10.4). It
// never activates the skill — promotion requires a passing evaluation.
var learnCmd = &cobra.Command{
	Use:   "learn",
	Short: "Record a workflow as a skill candidate (activated only after evaluation)",
	RunE: func(cmd *cobra.Command, _ []string) error {
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
			fmt.Printf("  - %s\n", s.Name)
		}
		fmt.Printf("Candidate skills (%d, awaiting evaluation):\n", len(candidates))
		for _, s := range candidates {
			fmt.Printf("  - %s\n", s.Name)
		}
		return nil
	},
}

func init() {
	learnCmd.Flags().String("name", "", "skill name")
	learnCmd.Flags().String("purpose", "", "what the workflow accomplishes")
	skillCmd.AddCommand(skillListCmd)
	rootCmd.AddCommand(learnCmd)
	rootCmd.AddCommand(skillCmd)
}

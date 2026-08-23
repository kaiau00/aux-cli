package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"github.com/kaiau00/aux-cli/internal/bundle"
	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/kaiau00/aux-cli/internal/db"
	"github.com/kaiau00/aux-cli/internal/govpolicy"
	"github.com/kaiau00/aux-cli/internal/skill"
	"github.com/spf13/cobra"
)

// bundleCmd exports/imports shareable optimization bundles (roadmapplan.md §12.5).
var bundleCmd = &cobra.Command{
	Use:   "bundle",
	Short: "Export or import shareable optimization bundles (active skills + policies)",
}

var bundleExportCmd = &cobra.Command{
	Use:   "export <file>",
	Short: "Export active skills and governor policies to a content-addressed bundle",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		conn, err := openBundleDB()
		if err != nil {
			return err
		}
		defer conn.Close()

		b, err := bundle.Export(context.Background(), skill.NewStore(conn), govpolicy.NewStore(conn))
		if err != nil {
			return err
		}
		data, err := bundle.Marshal(b)
		if err != nil {
			return err
		}
		if err := os.WriteFile(args[0], data, 0o644); err != nil {
			return err
		}
		fmt.Printf("Exported %d skill(s) and %d policy(ies) to %s (hash %s)\n",
			len(b.Skills), len(b.Policies), args[0], b.Hash[:12])
		return nil
	},
}

var bundleImportCmd = &cobra.Command{
	Use:   "import <file>",
	Short: "Import a bundle; entries arrive as candidates and must be evaluated before use",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		conn, err := openBundleDB()
		if err != nil {
			return err
		}
		defer conn.Close()

		data, err := os.ReadFile(args[0])
		if err != nil {
			return err
		}
		b, err := bundle.Unmarshal(data)
		if err != nil {
			return err
		}
		res, err := bundle.Import(context.Background(), b,
			skill.NewService(skill.NewStore(conn), nil),
			govpolicy.NewService(govpolicy.NewStore(conn), nil))
		if err != nil {
			return err
		}
		fmt.Printf("Imported %d skill candidate(s) and %d policy candidate(s). "+
			"They are inactive until evaluated locally.\n", res.SkillsImported, res.PoliciesImported)
		return nil
	},
}

func openBundleDB() (*sql.DB, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	if _, err := config.Load(cwd, false); err != nil {
		return nil, err
	}
	return db.Connect()
}

func init() {
	bundleCmd.AddCommand(bundleExportCmd)
	bundleCmd.AddCommand(bundleImportCmd)
	rootCmd.AddCommand(bundleCmd)
}

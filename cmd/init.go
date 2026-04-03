package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/SKE-Labs/lightrace-cli/internal/config"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new Lightrace project",
	Long:  "Creates the lightrace/ directory with a config.toml containing auto-generated secrets.",
	RunE: func(cmd *cobra.Command, args []string) error {
		path := filepath.Join(config.ConfigDir, config.ConfigFile)

		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists. Delete it to re-initialize.", path)
		}

		projectID := "lightrace"
		if len(args) > 0 {
			projectID = args[0]
		}

		cfg := config.GenerateDefault(projectID)
		if err := cfg.Write(); err != nil {
			return fmt.Errorf("writing config: %w", err)
		}

		fmt.Printf("Created %s\n", path)
		fmt.Println()
		fmt.Println("Next steps:")
		fmt.Println("  lightrace start    # Start the server")
		fmt.Println("  lightrace status   # Show service URLs")

		return nil
	},
}

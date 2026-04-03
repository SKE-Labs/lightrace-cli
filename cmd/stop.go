package cmd

import (
	"fmt"

	"github.com/SKE-Labs/lightrace-cli/internal/config"
	"github.com/SKE-Labs/lightrace-cli/internal/docker"
	"github.com/spf13/cobra"
)

var stopBackup bool

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the Lightrace server",
	Long:  "Stops and removes all Lightrace containers. Use --backup to also remove data volumes.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		ctx := cmd.Context()

		fmt.Println("Stopping Lightrace...")
		if err := docker.StopContainers(ctx, cfg.ProjectID); err != nil {
			return err
		}

		if err := docker.RemoveNetwork(ctx, cfg.ProjectID); err != nil {
			// Network removal is best-effort
			fmt.Printf("  Warning: could not remove network: %v\n", err)
		}

		fmt.Println("Lightrace stopped.")

		if stopBackup {
			fmt.Println("Note: Data volumes were NOT removed. Use 'docker volume rm' to clean up manually.")
		}

		return nil
	},
}

func init() {
	stopCmd.Flags().BoolVar(&stopBackup, "backup", false, "Also remove data volumes")
}

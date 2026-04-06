package cmd

import (
	"fmt"

	"github.com/SKE-Labs/lightrace-cli/internal/config"
	"github.com/SKE-Labs/lightrace-cli/internal/docker"
	"github.com/spf13/cobra"
)

var stopPurge bool

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the Lightrace server",
	Long:  "Stops and removes all Lightrace containers. Use --purge to also remove data volumes.",
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
			fmt.Printf("  Warning: could not remove network: %v\n", err)
		}

		if stopPurge {
			fmt.Println("Removing data volumes...")
			for _, vol := range []string{
				docker.PgVolumeName(cfg.ProjectID),
				docker.RedisVolumeName(cfg.ProjectID),
			} {
				if err := docker.RemoveVolume(ctx, vol); err != nil {
					fmt.Printf("  Warning: could not remove volume %s: %v\n", vol, err)
				} else {
					fmt.Printf("  Removed %s\n", vol)
				}
			}
		}

		fmt.Println("Lightrace stopped.")

		return nil
	},
}

func init() {
	stopCmd.Flags().BoolVar(&stopPurge, "purge", false, "Also remove data volumes")
}

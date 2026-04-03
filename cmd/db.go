package cmd

import (
	"fmt"

	"github.com/SKE-Labs/lightrace-cli/internal/config"
	"github.com/SKE-Labs/lightrace-cli/internal/docker"
	"github.com/spf13/cobra"
)

var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Database management commands",
}

var dbResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset database, re-run migrations, and seed demo data",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		return runMigrator(cmd, cfg, []string{
			"pnpm", "--filter", "@lightrace/shared", "exec",
			"prisma", "migrate", "reset", "--schema", "./prisma/schema.prisma", "--force",
		})
	},
}

var dbMigrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Run pending database migrations",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		// Default CMD in migrator image is prisma migrate deploy, so no override needed.
		return runMigrator(cmd, cfg, nil)
	},
}

func init() {
	dbCmd.AddCommand(dbResetCmd)
	dbCmd.AddCommand(dbMigrateCmd)
}

func runMigrator(cmd *cobra.Command, cfg *config.Config, cmdOverride []string) error {
	ctx := cmd.Context()
	networkName := docker.NetworkName(cfg.ProjectID)

	exitCode, err := docker.RunOnce(ctx, docker.RunConfig{
		ProjectID:    cfg.ProjectID,
		Service:      "migrate",
		Image:        config.DefaultMigratorImage,
		NetworkName:  networkName,
		NetworkAlias: "lightrace-migrate",
		Env: []string{
			fmt.Sprintf("DATABASE_URL=postgresql://lightrace:%s@lightrace-db:5432/lightrace", cfg.DB.Password),
		},
		Cmd: cmdOverride,
	})
	if err != nil {
		return err
	}
	if exitCode != 0 {
		return fmt.Errorf("migrator exited with code %d", exitCode)
	}
	return nil
}

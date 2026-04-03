package cmd

import (
	"fmt"

	"github.com/docker/docker/api/types"
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

		return execInBackend(cmd, cfg, []string{
			"sh", "-c",
			"prisma migrate reset --schema ./prisma/schema.prisma --force",
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

		return execInBackend(cmd, cfg, []string{
			"sh", "-c",
			"prisma migrate deploy --schema ./prisma/schema.prisma",
		})
	},
}

func init() {
	dbCmd.AddCommand(dbResetCmd)
	dbCmd.AddCommand(dbMigrateCmd)
}

func execInBackend(cmd *cobra.Command, cfg *config.Config, execCmd []string) error {
	ctx := cmd.Context()
	c, err := docker.Client()
	if err != nil {
		return err
	}

	name := docker.ContainerName(cfg.ProjectID, "backend")

	exec, err := c.ContainerExecCreate(ctx, name, types.ExecConfig{
		Cmd:          execCmd,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return fmt.Errorf("cannot exec in %s: %w", name, err)
	}

	resp, err := c.ContainerExecAttach(ctx, exec.ID, types.ExecStartCheck{})
	if err != nil {
		return err
	}
	defer resp.Close()

	// Stream output
	buf := make([]byte, 4096)
	for {
		n, readErr := resp.Reader.Read(buf)
		if n > 0 {
			fmt.Print(string(buf[:n]))
		}
		if readErr != nil {
			break
		}
	}

	// Check exit code
	inspect, err := c.ContainerExecInspect(ctx, exec.ID)
	if err != nil {
		return err
	}
	if inspect.ExitCode != 0 {
		return fmt.Errorf("command exited with code %d", inspect.ExitCode)
	}

	return nil
}

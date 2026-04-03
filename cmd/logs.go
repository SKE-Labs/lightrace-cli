package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/docker/docker/api/types"
	"github.com/SKE-Labs/lightrace-cli/internal/config"
	"github.com/SKE-Labs/lightrace-cli/internal/docker"
	"github.com/spf13/cobra"
)

var logsTail string

var logsCmd = &cobra.Command{
	Use:   "logs [service]",
	Short: "Tail logs from Lightrace services",
	Long:  "Show logs from all or specific services. Valid services: db, redis, backend, frontend, caddy.",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		ctx := cmd.Context()
		c, err := docker.Client()
		if err != nil {
			return err
		}

		services := []string{"db", "redis", "backend", "frontend", "caddy"}
		if len(args) > 0 {
			services = []string{args[0]}
		}

		for _, svc := range services {
			name := docker.ContainerName(cfg.ProjectID, svc)
			reader, err := c.ContainerLogs(ctx, name, types.ContainerLogsOptions{
				ShowStdout: true,
				ShowStderr: true,
				Tail:       logsTail,
				Follow:     len(args) > 0, // Only follow if specific service
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "  [%s] %v\n", svc, err)
				continue
			}

			if len(args) > 0 {
				// Streaming mode for single service
				io.Copy(os.Stdout, reader)
			} else {
				// Dump last N lines for all services
				fmt.Printf("── %s ──\n", svc)
				io.Copy(os.Stdout, reader)
				fmt.Println()
			}
			reader.Close()
		}

		return nil
	},
}

func init() {
	logsCmd.Flags().StringVar(&logsTail, "tail", "50", "Number of lines to show from end of logs")
}

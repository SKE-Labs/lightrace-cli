package cmd

import (
	"github.com/SKE-Labs/lightrace-cli/internal/config"
	"github.com/SKE-Labs/lightrace-cli/internal/start"
	"github.com/spf13/cobra"
)

var (
	startExclude    []string
	startIgnoreHealth bool
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the Lightrace server",
	Long:  "Pulls Docker images and starts all services (PostgreSQL, Redis, Backend, Frontend, Caddy).",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		return start.Run(cmd.Context(), cfg, start.Options{
			IgnoreHealthCheck: startIgnoreHealth,
			Exclude:           startExclude,
		})
	},
}

func init() {
	startCmd.Flags().StringSliceVar(&startExclude, "exclude", nil, "Services to skip (e.g. --exclude frontend)")
	startCmd.Flags().BoolVar(&startIgnoreHealth, "ignore-health-check", false, "Start even if health checks fail")
}

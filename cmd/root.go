package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "lightrace",
	Short: "Lightrace CLI — self-host your LLM tracing server",
	Long:  "Lightrace CLI orchestrates Docker containers to run the Lightrace server stack (PostgreSQL, Redis, Backend, Frontend, Caddy).",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(dbCmd)
}

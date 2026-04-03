package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/SKE-Labs/lightrace-cli/internal/config"
	"github.com/SKE-Labs/lightrace-cli/internal/docker"
	"github.com/spf13/cobra"
)

var statusOutput string

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show Lightrace service status",
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

		containers, err := c.ContainerList(ctx, types.ContainerListOptions{
			All:     true,
			Filters: filters.NewArgs(filters.Arg("label", docker.LabelProject+"="+cfg.ProjectID)),
		})
		if err != nil {
			return err
		}

		if len(containers) == 0 {
			fmt.Println("Lightrace is not running.")
			fmt.Println("Run 'lightrace start' to start the server.")
			return nil
		}

		url := cfg.PublicURL()

		switch statusOutput {
		case "json":
			return printJSON(cfg, containers, url)
		case "env":
			return printEnv(cfg, url)
		default:
			return printPretty(cfg, containers, url)
		}
	},
}

func init() {
	statusCmd.Flags().StringVarP(&statusOutput, "output", "o", "pretty", "Output format: pretty, json, env")
}

func printPretty(cfg *config.Config, containers []types.Container, url string) error {
	fmt.Println("Lightrace Status")
	fmt.Println()

	// Services table
	fmt.Printf("  %-12s %-10s %s\n", "SERVICE", "STATUS", "IMAGE")
	fmt.Printf("  %-12s %-10s %s\n", "-------", "------", "-----")
	for _, ctr := range containers {
		name := "unknown"
		if len(ctr.Names) > 0 {
			name = ctr.Names[0][1:] // strip leading /
		}
		status := ctr.State
		if ctr.Status != "" {
			status = ctr.Status
		}
		fmt.Printf("  %-12s %-10s %s\n", name, status, ctr.Image)
	}

	fmt.Println()
	fmt.Printf("  Dashboard:    %s\n", url)
	fmt.Printf("  API:          %s/api/public\n", url)
	fmt.Printf("  OTLP:         %s/api/public/otel/v1/traces\n", url)
	fmt.Println()
	fmt.Println("  Login:        demo@lightrace.dev / password")
	fmt.Printf("  Public Key:   %s\n", cfg.APIKeys.PublicKey)
	fmt.Printf("  Secret Key:   %s\n", cfg.APIKeys.SecretKey)

	return nil
}

func printJSON(cfg *config.Config, containers []types.Container, url string) error {
	services := make([]map[string]string, 0, len(containers))
	for _, ctr := range containers {
		name := "unknown"
		if len(ctr.Names) > 0 {
			name = ctr.Names[0][1:]
		}
		services = append(services, map[string]string{
			"name":   name,
			"state":  ctr.State,
			"status": ctr.Status,
			"image":  ctr.Image,
		})
	}

	out := map[string]interface{}{
		"url":        url,
		"api_url":    url + "/api/public",
		"otlp_url":   url + "/api/public/otel/v1/traces",
		"public_key": cfg.APIKeys.PublicKey,
		"secret_key": cfg.APIKeys.SecretKey,
		"services":   services,
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func printEnv(cfg *config.Config, url string) error {
	fmt.Printf("LIGHTRACE_HOST=%s\n", url)
	fmt.Printf("LIGHTRACE_PUBLIC_KEY=%s\n", cfg.APIKeys.PublicKey)
	fmt.Printf("LIGHTRACE_SECRET_KEY=%s\n", cfg.APIKeys.SecretKey)
	return nil
}

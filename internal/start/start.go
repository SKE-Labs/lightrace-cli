package start

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/SKE-Labs/lightrace-cli/internal/config"
	"github.com/SKE-Labs/lightrace-cli/internal/docker"
	"github.com/SKE-Labs/lightrace-cli/internal/gateway"
)

type Options struct {
	IgnoreHealthCheck bool
	Exclude           []string
}

func isExcluded(name string, exclude []string) bool {
	for _, e := range exclude {
		if e == name {
			return true
		}
	}
	return false
}

func Run(ctx context.Context, cfg *config.Config, opts Options) error {
	networkName := docker.NetworkName(cfg.ProjectID)

	// 1. Ensure Docker network
	fmt.Println("Creating network...")
	if err := docker.EnsureNetwork(ctx, cfg.ProjectID); err != nil {
		return fmt.Errorf("network: %w", err)
	}

	// 2. Pull images in sequence (could be parallelized later)
	images := []string{config.PostgresImage, config.RedisImage, cfg.BackendImage(), cfg.FrontendImage(), config.CaddyImage}
	for _, img := range images {
		fmt.Printf("Pulling %s...\n", img)
		if err := docker.PullImage(ctx, img); err != nil {
			return err
		}
	}

	// 3. Start PostgreSQL
	if !isExcluded("postgres", opts.Exclude) {
		fmt.Println("Starting PostgreSQL...")
		ports := map[string]string{}
		if cfg.DB.Port > 0 {
			ports["5432/tcp"] = fmt.Sprintf("%d", cfg.DB.Port)
		}
		if _, err := docker.RunContainer(ctx, docker.RunConfig{
			ProjectID:    cfg.ProjectID,
			Service:      "db",
			Image:        config.PostgresImage,
			NetworkName:  networkName,
			NetworkAlias: "lightrace-db",
			Env: []string{
				"POSTGRES_USER=lightrace",
				fmt.Sprintf("POSTGRES_PASSWORD=%s", cfg.DB.Password),
				"POSTGRES_DB=lightrace",
			},
			Ports:     ports,
			HealthCmd: []string{"pg_isready -U lightrace"},
		}); err != nil {
			return err
		}
		fmt.Println("  Waiting for PostgreSQL...")
		if err := docker.WaitHealthy(ctx, cfg.ProjectID, "db", 60*time.Second); err != nil && !opts.IgnoreHealthCheck {
			return fmt.Errorf("PostgreSQL health check failed: %w", err)
		}
	}

	// 4. Start Redis
	if !isExcluded("redis", opts.Exclude) {
		fmt.Println("Starting Redis...")
		ports := map[string]string{}
		if cfg.Redis.Port > 0 {
			ports["6379/tcp"] = fmt.Sprintf("%d", cfg.Redis.Port)
		}
		if _, err := docker.RunContainer(ctx, docker.RunConfig{
			ProjectID:    cfg.ProjectID,
			Service:      "redis",
			Image:        config.RedisImage,
			NetworkName:  networkName,
			NetworkAlias: "lightrace-redis",
			Ports:        ports,
			HealthCmd:    []string{"redis-cli ping"},
		}); err != nil {
			return err
		}
		fmt.Println("  Waiting for Redis...")
		if err := docker.WaitHealthy(ctx, cfg.ProjectID, "redis", 30*time.Second); err != nil && !opts.IgnoreHealthCheck {
			return fmt.Errorf("Redis health check failed: %w", err)
		}
	}

	// 5. Start Backend
	if !isExcluded("backend", opts.Exclude) {
		fmt.Println("Starting Backend...")
		if _, err := docker.RunContainer(ctx, docker.RunConfig{
			ProjectID:    cfg.ProjectID,
			Service:      "backend",
			Image:        cfg.BackendImage(),
			NetworkName:  networkName,
			NetworkAlias: "lightrace-backend",
			Env: []string{
				fmt.Sprintf("DATABASE_URL=postgresql://lightrace:%s@lightrace-db:5432/lightrace", cfg.DB.Password),
				"REDIS_URL=redis://lightrace-redis:6379",
				"PORT=3002",
				"WS_PORT=3003",
				fmt.Sprintf("INTERNAL_SECRET=%s", cfg.Internal.Secret),
			},
			HealthCmd: []string{"wget -qO- http://localhost:3002/health || exit 1"},
		}); err != nil {
			return err
		}
		fmt.Println("  Waiting for Backend...")
		if err := docker.WaitHealthy(ctx, cfg.ProjectID, "backend", 90*time.Second); err != nil && !opts.IgnoreHealthCheck {
			return fmt.Errorf("Backend health check failed: %w", err)
		}
	}

	// 6. Start Frontend
	if !isExcluded("frontend", opts.Exclude) {
		publicURL := cfg.PublicURL()

		fmt.Println("Starting Frontend...")
		if _, err := docker.RunContainer(ctx, docker.RunConfig{
			ProjectID:    cfg.ProjectID,
			Service:      "frontend",
			Image:        cfg.FrontendImage(),
			NetworkName:  networkName,
			NetworkAlias: "lightrace-frontend",
			Env: []string{
				fmt.Sprintf("DATABASE_URL=postgresql://lightrace:%s@lightrace-db:5432/lightrace", cfg.DB.Password),
				"REDIS_URL=redis://lightrace-redis:6379",
				fmt.Sprintf("AUTH_SECRET=%s", cfg.Auth.Secret),
				fmt.Sprintf("AUTH_URL=%s", publicURL),
				fmt.Sprintf("NEXTAUTH_URL=%s", publicURL),
				"BACKEND_URL=http://lightrace-backend:3002",
				fmt.Sprintf("INTERNAL_SECRET=%s", cfg.Internal.Secret),
				"WS_PORT=3003",
			},
			HealthCmd: []string{"wget -qO- http://localhost:3001 || exit 1"},
		}); err != nil {
			return err
		}
		fmt.Println("  Waiting for Frontend...")
		if err := docker.WaitHealthy(ctx, cfg.ProjectID, "frontend", 90*time.Second); err != nil && !opts.IgnoreHealthCheck {
			return fmt.Errorf("Frontend health check failed: %w", err)
		}
	}

	// 7. Generate Caddyfile and start Caddy
	if !isExcluded("caddy", opts.Exclude) {
		fmt.Println("Starting Caddy gateway...")
		caddyfilePath, err := gateway.GenerateCaddyfile(cfg)
		if err != nil {
			return fmt.Errorf("generating Caddyfile: %w", err)
		}

		absCaddyfile, _ := filepath.Abs(caddyfilePath)
		if _, err := docker.RunContainer(ctx, docker.RunConfig{
			ProjectID:    cfg.ProjectID,
			Service:      "caddy",
			Image:        config.CaddyImage,
			NetworkName:  networkName,
			NetworkAlias: "lightrace-caddy",
			Ports: map[string]string{
				fmt.Sprintf("%d/tcp", cfg.Gateway.Port): fmt.Sprintf("%d", cfg.Gateway.Port),
			},
			Volumes: map[string]string{
				absCaddyfile: "/etc/caddy/Caddyfile",
			},
			HealthCmd: []string{fmt.Sprintf("wget -qO- http://localhost:%d/health || exit 1", cfg.Gateway.Port)},
		}); err != nil {
			return err
		}
		fmt.Println("  Waiting for Caddy...")
		if err := docker.WaitHealthy(ctx, cfg.ProjectID, "caddy", 30*time.Second); err != nil && !opts.IgnoreHealthCheck {
			return fmt.Errorf("Caddy health check failed: %w", err)
		}
	}

	// 8. Print status
	fmt.Println()
	printStatus(cfg, os.Stdout)

	return nil
}

func printStatus(cfg *config.Config, w *os.File) {
	url := cfg.PublicURL()

	fmt.Fprintln(w, "  Lightrace is running.")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  Dashboard:    %s\n", url)
	fmt.Fprintf(w, "  API:          %s/api/public\n", url)
	fmt.Fprintf(w, "  OTLP:         %s/api/public/otel/v1/traces\n", url)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  Login:        demo@lightrace.dev / password")
	fmt.Fprintf(w, "  Public Key:   %s\n", cfg.APIKeys.PublicKey)
	fmt.Fprintf(w, "  Secret Key:   %s\n", cfg.APIKeys.SecretKey)
	fmt.Fprintln(w)
}

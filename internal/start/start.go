package start

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/SKE-Labs/lightrace-cli/internal/config"
	"github.com/SKE-Labs/lightrace-cli/internal/docker"
	"github.com/SKE-Labs/lightrace-cli/internal/gateway"
	"github.com/SKE-Labs/lightrace-cli/internal/prompt"
	"golang.org/x/crypto/bcrypt"
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
	// 0. First-time user setup
	if !cfg.UserConfigured() {
		if err := promptUserSetup(cfg); err != nil {
			return fmt.Errorf("user setup: %w", err)
		}
	}

	networkName := docker.NetworkName(cfg.ProjectID)

	// 1. Ensure Docker network
	fmt.Println("Creating network...")
	if err := docker.EnsureNetwork(ctx, cfg.ProjectID); err != nil {
		return fmt.Errorf("network: %w", err)
	}

	// 2. Pull images in sequence (could be parallelized later)
	fmt.Println("Pulling images...")
	images := []string{config.PostgresImage, config.RedisImage, config.DefaultMigratorImage, cfg.BackendImage(), cfg.FrontendImage(), config.CaddyImage}
	for _, img := range images {
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
			Ports: ports,
			Volumes: map[string]string{
				docker.PgVolumeName(cfg.ProjectID): "/var/lib/postgresql/data",
			},
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
			Volumes: map[string]string{
				docker.RedisVolumeName(cfg.ProjectID): "/data",
			},
			Cmd:       []string{"redis-server", "--appendonly", "yes"},
			HealthCmd: []string{"redis-cli ping"},
		}); err != nil {
			return err
		}
		fmt.Println("  Waiting for Redis...")
		if err := docker.WaitHealthy(ctx, cfg.ProjectID, "redis", 30*time.Second); err != nil && !opts.IgnoreHealthCheck {
			return fmt.Errorf("Redis health check failed: %w", err)
		}
	}

	// 5. Run database migrations and seed
	if !isExcluded("postgres", opts.Exclude) {
		migratorEnv := cfg.SeedEnv()

		fmt.Println("Running migrations...")
		exitCode, err := docker.RunOnce(ctx, docker.RunConfig{
			ProjectID:    cfg.ProjectID,
			Service:      "migrate",
			Image:        config.DefaultMigratorImage,
			NetworkName:  networkName,
			NetworkAlias: "lightrace-migrate",
			Env:          migratorEnv,
		})
		if err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
		if exitCode != 0 {
			return fmt.Errorf("migration exited with code %d", exitCode)
		}

		fmt.Println("Seeding database...")
		seedCode, seedErr := docker.RunOnce(ctx, docker.RunConfig{
			ProjectID:    cfg.ProjectID,
			Service:      "seed",
			Image:        config.DefaultMigratorImage,
			NetworkName:  networkName,
			NetworkAlias: "lightrace-seed",
			Env:          migratorEnv,
			Cmd: []string{
				"pnpm", "--filter", "@lightrace/shared", "exec",
				"tsx", "./prisma/seed.ts",
			},
		})
		if seedErr != nil {
			return fmt.Errorf("seeding failed: %w", seedErr)
		}
		if seedCode != 0 {
			return fmt.Errorf("seed exited with code %d", seedCode)
		}
	}

	// 6. Start backend
	if !isExcluded("backend", opts.Exclude) {
		fmt.Println("Starting Backend...")
		if _, err := docker.RunContainer(ctx, docker.RunConfig{
			ProjectID:    cfg.ProjectID,
			Service:      "backend",
			Image:        cfg.BackendImage(),
			NetworkName:  networkName,
			NetworkAlias: "lightrace-backend",
			Env: []string{
				fmt.Sprintf("DATABASE_URL=%s", cfg.DatabaseURL()),
				"REDIS_URL=redis://lightrace-redis:6379",
				"PORT=3002",
				"WS_PORT=3003",
				fmt.Sprintf("INTERNAL_SECRET=%s", cfg.Internal.Secret),
			},
			HealthCmd:  []string{"wget -qO- http://127.0.0.1:3002/health || exit 1"},
			ExtraHosts: []string{"host.docker.internal:host-gateway"},
		}); err != nil {
			return err
		}
		fmt.Println("  Waiting for Backend...")
		if err := docker.WaitHealthy(ctx, cfg.ProjectID, "backend", 90*time.Second); err != nil && !opts.IgnoreHealthCheck {
			return fmt.Errorf("Backend health check failed: %w", err)
		}
	}

	// 7. Start frontend
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
				fmt.Sprintf("DATABASE_URL=%s", cfg.DatabaseURL()),
				"REDIS_URL=redis://lightrace-redis:6379",
				fmt.Sprintf("AUTH_SECRET=%s", cfg.Auth.Secret),
				fmt.Sprintf("AUTH_URL=%s", publicURL),
				fmt.Sprintf("NEXTAUTH_URL=%s", publicURL),
				"BACKEND_URL=http://lightrace-backend:3002",
				fmt.Sprintf("INTERNAL_SECRET=%s", cfg.Internal.Secret),
				"WS_PORT=3003",
			},
			HealthCmd: []string{"wget -qO- http://127.0.0.1:3001/api/public/health || exit 1"},
		}); err != nil {
			return err
		}
		fmt.Println("  Waiting for Frontend...")
		if err := docker.WaitHealthy(ctx, cfg.ProjectID, "frontend", 90*time.Second); err != nil && !opts.IgnoreHealthCheck {
			return fmt.Errorf("Frontend health check failed: %w", err)
		}
	}

	// 8. Generate Caddyfile and start Caddy
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
			HealthCmd: []string{fmt.Sprintf("wget -qO- http://127.0.0.1:%d/health || exit 1", cfg.Gateway.Port)},
		}); err != nil {
			return err
		}
		fmt.Println("  Waiting for Caddy...")
		if err := docker.WaitHealthy(ctx, cfg.ProjectID, "caddy", 30*time.Second); err != nil && !opts.IgnoreHealthCheck {
			return fmt.Errorf("Caddy health check failed: %w", err)
		}
	}

	// 9. Print status
	fmt.Println()
	printStatus(cfg, os.Stdout)

	return nil
}

func promptUserSetup(cfg *config.Config) error {
	if !prompt.IsInteractive() {
		return nil
	}

	fmt.Println("First-time setup — configure your Lightrace user:")
	fmt.Println()

	email, err := prompt.ReadLine("Email", "")
	if err != nil {
		return err
	}
	if email == "" {
		return fmt.Errorf("email cannot be empty")
	}

	password, err := prompt.ReadLine("Password", "")
	if err != nil {
		return err
	}
	if password == "" {
		return fmt.Errorf("password cannot be empty")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return fmt.Errorf("hashing password: %w", err)
	}

	projectName, err := prompt.ReadLine("Project name", "My Project")
	if err != nil {
		return err
	}

	projectDBID := slugify(projectName) + "-" + config.RandomHex(4)

	cfg.User = config.UserConfig{
		Email:        email,
		PasswordHash: string(hash),
		ProjectName:  projectName,
		ProjectDBID:  projectDBID,
	}

	if err := cfg.Write(); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Println("  Saved to config.toml")
	fmt.Println()

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
	if cfg.UserConfigured() {
		fmt.Fprintf(w, "  Login:        %s / **********\n", cfg.User.Email)
	} else {
		fmt.Fprintln(w, "  Login:        demo@lightrace.dev / password")
	}
	fmt.Fprintf(w, "  Public Key:   %s\n", cfg.APIKeys.PublicKey)
	fmt.Fprintf(w, "  Secret Key:   %s\n", cfg.APIKeys.SecretKey)
	fmt.Fprintln(w)
}

var nonAlphaNum = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	slug := strings.ToLower(strings.TrimSpace(s))
	slug = nonAlphaNum.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "project"
	}
	return slug
}


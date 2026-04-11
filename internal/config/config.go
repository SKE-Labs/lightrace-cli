package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

const (
	ConfigDir  = ".lightrace"
	ConfigFile = "config.toml"
)

type Config struct {
	ProjectID string        `toml:"project_id"`
	Gateway   GatewayConfig `toml:"gateway"`
	DB        DBConfig      `toml:"db"`
	Redis     RedisConfig   `toml:"redis"`
	Auth      AuthConfig    `toml:"auth"`
	APIKeys   APIKeysConfig `toml:"api_keys"`
	Images    ImagesConfig  `toml:"images"`
	Internal  InternalConfig `toml:"internal"`
	User      UserConfig     `toml:"user,omitempty"`
}

type GatewayConfig struct {
	Port   int    `toml:"port"`
	Domain string `toml:"domain,omitempty"`
}

type DBConfig struct {
	Password string `toml:"password"`
	Port     int    `toml:"port,omitempty"` // 0 = not exposed to host
}

type RedisConfig struct {
	Port int `toml:"port,omitempty"` // 0 = not exposed to host
}

type AuthConfig struct {
	Secret string `toml:"secret"`
}

type APIKeysConfig struct {
	PublicKey string `toml:"public_key"`
	SecretKey string `toml:"secret_key"`
}

type ImagesConfig struct {
	Backend  string `toml:"backend,omitempty"`
	Frontend string `toml:"frontend,omitempty"`
}

type InternalConfig struct {
	Secret string `toml:"secret"`
}

type UserConfig struct {
	Email        string `toml:"email,omitempty"`
	PasswordHash string `toml:"password_hash,omitempty"`
	Name         string `toml:"name,omitempty"`
	ProjectName  string `toml:"project_name,omitempty"`
	ProjectDBID  string `toml:"project_db_id,omitempty"`
}

// Default image versions — pinned per CLI release.
const (
	DefaultBackendImage  = "ghcr.io/ske-labs/backend:latest"
	DefaultFrontendImage = "ghcr.io/ske-labs/frontend:latest"
	DefaultMigratorImage = "ghcr.io/ske-labs/migrator:latest"
	PostgresImage        = "postgres:16-alpine"
	RedisImage           = "redis:7-alpine"
	CaddyImage           = "caddy:2-alpine"
)

func (c *Config) BackendImage() string {
	if c.Images.Backend != "" {
		return c.Images.Backend
	}
	return DefaultBackendImage
}

func (c *Config) PublicURL() string {
	if c.Gateway.Domain != "" {
		return fmt.Sprintf("https://%s", c.Gateway.Domain)
	}
	return fmt.Sprintf("http://localhost:%d", c.Gateway.Port)
}

func (c *Config) UserConfigured() bool {
	return c.User.Email != "" && c.User.PasswordHash != ""
}

func (c *Config) DatabaseURL() string {
	return fmt.Sprintf("postgresql://lightrace:%s@lightrace-db:5432/lightrace", c.DB.Password)
}

func (c *Config) SeedEnv() []string {
	env := []string{
		fmt.Sprintf("DATABASE_URL=%s", c.DatabaseURL()),
	}
	if c.UserConfigured() {
		env = append(env,
			fmt.Sprintf("SEED_USER_EMAIL=%s", c.User.Email),
			fmt.Sprintf("SEED_USER_PASSWORD_HASH=%s", c.User.PasswordHash),
			fmt.Sprintf("SEED_USER_NAME=%s", c.User.Name),
			fmt.Sprintf("SEED_PROJECT_NAME=%s", c.User.ProjectName),
		)
		if c.User.ProjectDBID != "" {
			env = append(env, fmt.Sprintf("SEED_PROJECT_ID=%s", c.User.ProjectDBID))
		}
	}
	env = append(env,
		fmt.Sprintf("SEED_PUBLIC_KEY=%s", c.APIKeys.PublicKey),
		fmt.Sprintf("SEED_SECRET_KEY=%s", c.APIKeys.SecretKey),
	)
	return env
}

func (c *Config) FrontendImage() string {
	if c.Images.Frontend != "" {
		return c.Images.Frontend
	}
	return DefaultFrontendImage
}

func Load() (*Config, error) {
	path := filepath.Join(ConfigDir, ConfigFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w\nRun 'lightrace init' first.", path, err)
	}

	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Apply defaults
	if cfg.Gateway.Port == 0 {
		cfg.Gateway.Port = 3000
	}
	if cfg.DB.Password == "" {
		cfg.DB.Password = "lightrace"
	}
	if cfg.ProjectID == "" {
		cfg.ProjectID = "lightrace"
	}

	return &cfg, nil
}

func GenerateDefault(projectID string) *Config {
	return &Config{
		ProjectID: projectID,
		Gateway: GatewayConfig{
			Port: 3000,
		},
		DB: DBConfig{
			Password: "lightrace",
		},
		Auth: AuthConfig{
			Secret: RandomHex(32),
		},
		APIKeys: APIKeysConfig{
			PublicKey: "pk-lt-demo",
			SecretKey: "sk-lt-demo",
		},
		Internal: InternalConfig{
			Secret: RandomHex(32),
		},
	}
}

func (c *Config) Write() error {
	if err := os.MkdirAll(ConfigDir, 0o755); err != nil {
		return err
	}

	path := filepath.Join(ConfigDir, ConfigFile)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	encoder := toml.NewEncoder(f)
	return encoder.Encode(c)
}

func RandomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

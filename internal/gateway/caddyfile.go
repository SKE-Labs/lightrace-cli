package gateway

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/SKE-Labs/lightrace-cli/internal/config"
)

// GenerateCaddyfile creates a Caddyfile for the given config and writes it
// to a temporary file, returning the path. The caller is responsible for cleanup.
func GenerateCaddyfile(cfg *config.Config) (string, error) {
	var globalOpts, address string
	if cfg.Gateway.Domain != "" {
		address = cfg.Gateway.Domain
	} else {
		address = fmt.Sprintf(":%d", cfg.Gateway.Port)
		globalOpts = "{\n\tauto_https off\n}\n\n"
	}

	content := fmt.Sprintf(`%s%s {
	handle /api/public/* {
		reverse_proxy lightrace-backend:3002
	}

	handle /trpc/* {
		reverse_proxy lightrace-backend:3002
	}

	handle /ws {
		reverse_proxy lightrace-backend:3003
	}

	handle {
		reverse_proxy lightrace-frontend:3001
	}
}
`, globalOpts, address)

	dir := filepath.Join(config.ConfigDir, ".runtime")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	path := filepath.Join(dir, "Caddyfile")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}

	return path, nil
}

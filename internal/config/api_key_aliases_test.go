package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigOptional_SanitizesAPIKeyAliases(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	content := []byte(`
api-keys:
  - "  sk-primary  "
  - "sk-secondary"
  - "sk-primary"
api-key-aliases:
  - api-key: " sk-primary "
    alias: " Alpha "
  - api-key: "sk-missing"
    alias: "Missing"
  - api-key: "sk-secondary"
    alias: ""
  - api-key: ""
    alias: "Empty"
`)
	if err := os.WriteFile(configPath, content, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfigOptional(configPath, false)
	if err != nil {
		t.Fatalf("LoadConfigOptional() error = %v", err)
	}

	if len(cfg.APIKeys) != 2 {
		t.Fatalf("len(APIKeys) = %d, want 2", len(cfg.APIKeys))
	}
	if cfg.APIKeys[0] != "sk-primary" || cfg.APIKeys[1] != "sk-secondary" {
		t.Fatalf("APIKeys = %#v, want [sk-primary sk-secondary]", cfg.APIKeys)
	}

	if len(cfg.APIKeyAliases) != 1 {
		t.Fatalf("len(APIKeyAliases) = %d, want 1", len(cfg.APIKeyAliases))
	}
	if cfg.APIKeyAliases[0].APIKey != "sk-primary" {
		t.Fatalf("APIKeyAliases[0].APIKey = %q, want sk-primary", cfg.APIKeyAliases[0].APIKey)
	}
	if cfg.APIKeyAliases[0].Alias != "Alpha" {
		t.Fatalf("APIKeyAliases[0].Alias = %q, want Alpha", cfg.APIKeyAliases[0].Alias)
	}
}

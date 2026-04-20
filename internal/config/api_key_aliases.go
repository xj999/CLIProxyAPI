package config

import "strings"

// SanitizeClientAPIKeys trims, deduplicates, and normalizes top-level client API keys
// together with their optional aliases. Aliases for missing keys are discarded.
func (cfg *Config) SanitizeClientAPIKeys() {
	if cfg == nil {
		return
	}

	seenKeys := make(map[string]struct{}, len(cfg.APIKeys))
	keys := make([]string, 0, len(cfg.APIKeys))
	for _, raw := range cfg.APIKeys {
		key := strings.TrimSpace(raw)
		if key == "" {
			continue
		}
		if _, exists := seenKeys[key]; exists {
			continue
		}
		seenKeys[key] = struct{}{}
		keys = append(keys, key)
	}
	cfg.APIKeys = keys

	if len(cfg.APIKeyAliases) == 0 || len(cfg.APIKeys) == 0 {
		cfg.APIKeyAliases = nil
		return
	}

	seenAliasKeys := make(map[string]struct{}, len(cfg.APIKeyAliases))
	aliases := make([]APIKeyAliasEntry, 0, len(cfg.APIKeyAliases))
	for _, entry := range cfg.APIKeyAliases {
		key := strings.TrimSpace(entry.APIKey)
		alias := strings.TrimSpace(entry.Alias)
		if key == "" || alias == "" {
			continue
		}
		if _, ok := seenKeys[key]; !ok {
			continue
		}
		if _, exists := seenAliasKeys[key]; exists {
			continue
		}
		seenAliasKeys[key] = struct{}{}
		aliases = append(aliases, APIKeyAliasEntry{
			APIKey: key,
			Alias:  alias,
		})
	}
	if len(aliases) == 0 {
		cfg.APIKeyAliases = nil
		return
	}
	cfg.APIKeyAliases = aliases
}

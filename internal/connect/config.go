package connect

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// config is the client config file: defaults only — connection
// configuration lives in context files (hits-hq 02-DESIGN/idp-auth.md,
// issue 005-cli-config-file).
type config struct {
	Defaults struct {
		Context    string `json:"context"`
		Actor      string `json:"actor"`
		Initiative string `json:"initiative"`
	} `json:"defaults"`
}

func configPath() string { return filepath.Join(configDir(), "hits", "config.json") }

// loadConfig reads the client config; a missing file is the zero config,
// not an error.
func loadConfig() (config, error) {
	var cfg config
	b, err := os.ReadFile(configPath())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("client config: %w", err)
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, fmt.Errorf("client config: parse %s: %w", configPath(), err)
	}
	return cfg, nil
}

// DefaultActor is the client config's default acting handle, "" when unset
// or the config is unreadable — callers fall through to their own error.
func DefaultActor() string {
	cfg, err := loadConfig()
	if err != nil {
		return ""
	}
	return cfg.Defaults.Actor
}

// DefaultInitiative is the client config's selected initiative — the
// filing default behind hits initiative select (decision 0016), never a
// read scope. "" when unset or the config is unreadable.
func DefaultInitiative() string {
	cfg, err := loadConfig()
	if err != nil {
		return ""
	}
	return cfg.Defaults.Initiative
}

// SaveDefaultInitiative records the initiative select verb's one write:
// defaults.initiative. Verification that the slug is registered is the
// caller's job — this layer has no connection.
func SaveDefaultInitiative(slug string) error {
	return saveDefault("initiative", slug)
}

// saveDefaultContext records the select verb's one write: defaults.context.
func saveDefaultContext(name string) error {
	return saveDefault("context", name)
}

// saveDefault sets one defaults.<field> value, preserving every other
// field the file carries — the config schema may grow around this writer.
func saveDefault(field, value string) error {
	path := configPath()
	doc := map[string]any{}
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(b, &doc); err != nil {
			return fmt.Errorf("client config: parse %s: %w", path, err)
		}
	case errors.Is(err, fs.ErrNotExist):
	default:
		return fmt.Errorf("client config: %w", err)
	}
	defaults, _ := doc["defaults"].(map[string]any)
	if defaults == nil {
		defaults = map[string]any{}
	}
	defaults[field] = value
	doc["defaults"] = defaults
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("client config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("client config: %w", err)
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o600); err != nil {
		return fmt.Errorf("client config: %w", err)
	}
	return nil
}

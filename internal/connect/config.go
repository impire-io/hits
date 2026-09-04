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
		Context string `json:"context"`
		Actor   string `json:"actor"`
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

// saveDefaultContext records the select verb's one write: defaults.context,
// preserving every other field the file carries — the config schema may
// grow around this writer.
func saveDefaultContext(name string) error {
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
	defaults["context"] = name
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

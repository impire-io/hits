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

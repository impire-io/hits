// Package config is the client's own configuration: one file,
// $XDG_CONFIG_HOME/hits/config.json, holding the selected initiative —
// the filing default `hits create` mints from when the call names none
// (hits-hq decision 0016). Nothing about a connection lives here: the
// connection is a chronicle context (hits-hq decision 0021).
package config

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

type config struct {
	Initiative string `json:"initiative,omitempty"`
}

func configDir() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".config")
}

func configPath() string { return filepath.Join(configDir(), "hits", "config.json") }

func load() (config, error) {
	var c config
	b, err := os.ReadFile(configPath())
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	return c, json.Unmarshal(b, &c)
}

// DefaultInitiative is the selected initiative, "" when none is selected.
func DefaultInitiative() string {
	c, err := load()
	if err != nil {
		return ""
	}
	return c.Initiative
}

// SaveDefaultInitiative selects an initiative as the filing default.
func SaveDefaultInitiative(slug string) error {
	c, err := load()
	if err != nil {
		return err
	}
	c.Initiative = slug
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(configPath()), 0o700); err != nil {
		return err
	}
	return os.WriteFile(configPath(), b, 0o600)
}

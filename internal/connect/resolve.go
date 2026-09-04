package connect

import (
	"os"
	"path/filepath"
	"strings"
)

// effectiveName resolves which context a connection means: the explicit
// name, else the client config's default. "" — nothing configured — means
// the default connection; the nats CLI's selection marker is never
// consulted (decision 0011).
func effectiveName(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	cfg, err := loadConfig()
	if err != nil {
		return "", err
	}
	return cfg.Defaults.Context, nil
}

// configDir is $XDG_CONFIG_HOME, else ~/.config.
func configDir() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config")
}

// stateDir is $XDG_STATE_HOME, else ~/.local/state.
func stateDir() string {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state")
}

func hitsContextDir() string { return filepath.Join(configDir(), "hits", "context") }

// natsContextDir is the nats CLI's context directory — read by
// `hits context import` only, never at connect time (decision 0011).
func natsContextDir() string { return filepath.Join(configDir(), "nats", "context") }

// validName mirrors natscontext's rule: non-empty, no traversal, no
// separators.
func validName(name string) bool {
	return name != "" && !strings.Contains(name, "..") && !strings.Contains(name, string(os.PathSeparator))
}

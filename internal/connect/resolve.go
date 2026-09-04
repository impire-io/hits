package connect

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// contextRef is one resolved lookup: hits is set when the name matched a
// file in hits' own context directory, nil when the name belongs to the
// nats CLI (or to nobody — natscontext owns that error).
type contextRef struct {
	hits *hitsContext
}

// effectiveName resolves which context a connection means: the explicit
// name, else the client config's default, else the nats CLI's
// selected-context marker ("" when none of the three name one).
func effectiveName(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	cfg, err := loadConfig()
	if err != nil {
		return "", err
	}
	if cfg.Defaults.Context != "" {
		return cfg.Defaults.Context, nil
	}
	return selectedNatsContext(), nil
}

// lookup finds the named context: hits' context directory first, then the
// nats CLI's. A name defined in both is a hard error — one source of truth
// per name.
func lookup(name string) (contextRef, error) {
	if !validName(name) {
		// Includes "": natscontext handles the no-context case natively.
		return contextRef{}, nil
	}
	hitsPath := filepath.Join(hitsContextDir(), name+".json")
	natsPath := filepath.Join(natsContextDir(), name+".json")
	hitsExists := fileExists(hitsPath)
	if hitsExists && fileExists(natsPath) {
		return contextRef{}, fmt.Errorf(
			"context %q is defined twice — %s and %s: remove one", name, hitsPath, natsPath)
	}
	if !hitsExists {
		return contextRef{}, nil
	}
	hc, err := loadHitsContext(name, hitsPath)
	if err != nil {
		return contextRef{}, err
	}
	return contextRef{hits: hc}, nil
}

// configDir is $XDG_CONFIG_HOME, else ~/.config — the same root the nats
// CLI resolves, so the two context directories sit side by side.
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
func natsContextDir() string { return filepath.Join(configDir(), "nats", "context") }

// selectedNatsContext reads the nats CLI's selection marker; empty when no
// context is selected.
func selectedNatsContext() string {
	b, err := os.ReadFile(filepath.Join(configDir(), "nats", "context.txt"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// validName mirrors natscontext's rule: non-empty, no traversal, no
// separators.
func validName(name string) bool {
	return name != "" && !strings.Contains(name, "..") && !strings.Contains(name, string(os.PathSeparator))
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

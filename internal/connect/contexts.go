package connect

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The management API the `hits context` verb family is thin over
// (hits-hq 02-DESIGN/idp-auth.md § the verbs). Everything here operates
// on hits' own context directory; ImportContext's read of the nats CLI's
// directory is the one exception, and it is read-only.

// ContextInfo is one row of `hits context ls`.
type ContextInfo struct {
	Name    string `json:"name"`
	Default bool   `json:"default"`
}

// ListContexts names every context in hits' directory, the config default
// marked. A missing directory is an empty list.
func ListContexts() ([]ContextInfo, error) {
	entries, err := os.ReadDir(hitsContextDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("context directory: %w", err)
	}
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	var out []ContextInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".json")
		out = append(out, ContextInfo{Name: name, Default: name == cfg.Defaults.Context})
	}
	return out, nil
}

// AddContext scaffolds a new context file: the nats block with the url,
// plus an oauth block when one is given. It refuses an existing name.
func AddContext(name, url string, oauth *OAuthConfig) (string, error) {
	path, err := newContextPath(name)
	if err != nil {
		return "", err
	}
	natsBlock := map[string]any{}
	if url != "" {
		natsBlock["url"] = url
	}
	doc := map[string]any{"nats": natsBlock}
	if oauth != nil {
		if oauth.Issuer == "" || oauth.ClientID == "" {
			return "", fmt.Errorf("context %q: oauth needs both issuer and client_id", name)
		}
		oauthBlock := map[string]any{"issuer": oauth.Issuer, "client_id": oauth.ClientID}
		if len(oauth.Scopes) > 0 {
			oauthBlock["scopes"] = oauth.Scopes
		}
		doc["oauth"] = oauthBlock
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("context %q: %w", name, err)
	}
	return path, writeContextDoc(path, b)
}

// ImportContext copies a nats CLI context into hits' directory, its
// content wrapped byte-for-byte under "nats" — the one read hits ever
// does from the nats CLI's directory (decision 0011). An empty newName
// keeps the nats name.
func ImportContext(natsName, newName string) (string, error) {
	if !validName(natsName) {
		return "", fmt.Errorf("invalid context name %q", natsName)
	}
	src := filepath.Join(natsContextDir(), natsName+".json")
	b, err := os.ReadFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no nats CLI context %q (%s)", natsName, src)
		}
		return "", fmt.Errorf("nats CLI context %q: %w", natsName, err)
	}
	if !json.Valid(b) {
		return "", fmt.Errorf("nats CLI context %q is not valid JSON (%s)", natsName, src)
	}
	if newName == "" {
		newName = natsName
	}
	path, err := newContextPath(newName)
	if err != nil {
		return "", err
	}
	doc := map[string]json.RawMessage{"nats": json.RawMessage(b)}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("context %q: %w", newName, err)
	}
	return path, writeContextDoc(path, out)
}

// RemoveContext deletes the named context file.
func RemoveContext(name string) error {
	path, err := ContextPath(name)
	if err != nil {
		return err
	}
	return os.Remove(path)
}

// ContextPath returns the named context's file path, requiring it to
// exist — the edit verb's lookup.
func ContextPath(name string) (string, error) {
	if !validName(name) {
		return "", fmt.Errorf("invalid context name %q", name)
	}
	path := filepath.Join(hitsContextDir(), name+".json")
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no context %q in %s", name, hitsContextDir())
		}
		return "", fmt.Errorf("context %q: %w", name, err)
	}
	return path, nil
}

// SelectContext verifies the context exists and records it as the config
// default — the only selection state there is (decision 0011).
func SelectContext(name string) error {
	if _, err := ContextPath(name); err != nil {
		return err
	}
	return saveDefaultContext(name)
}

// newContextPath validates the name, ensures the directory, and refuses
// an existing file.
func newContextPath(name string) (string, error) {
	if !validName(name) {
		return "", fmt.Errorf("invalid context name %q", name)
	}
	dir := hitsContextDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("context directory: %w", err)
	}
	path := filepath.Join(dir, name+".json")
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("context %q already exists (%s)", name, path)
	}
	return path, nil
}

// writeContextDoc writes a context document 0600 with a trailing
// newline — context files can carry credentials.
func writeContextDoc(path string, b []byte) error {
	if err := os.WriteFile(path, append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

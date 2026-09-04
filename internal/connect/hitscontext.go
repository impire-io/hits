package connect

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/synadia-io/orbit.go/natscontext"
)

// hitsContext is a hits-owned context document (hits-hq decision 0011):
// hits fields at the top level, the NATS connection nested under "nats"
// in the exact natscontext Settings schema. The subtree stays raw bytes —
// natscontext loads it through the shim below, so the connection schema
// never lives in this package.
type hitsContext struct {
	path string

	NATS  json.RawMessage `json:"nats"`
	OAuth *OAuthConfig    `json:"oauth"`
}

// OAuthConfig is the oauth block of a hits context.
type OAuthConfig struct {
	Issuer   string   `json:"issuer"`
	ClientID string   `json:"client_id"`
	Scopes   []string `json:"scopes"`
}

// defaultScopes carries the refresh token: offline_access is what most IDPs
// key it on.
var defaultScopes = []string{"openid", "offline_access"}

// loadHitsContext resolves a name in hits' context directory — the only
// place contexts live (decision 0011). A missing file names the verbs
// that create one.
func loadHitsContext(name string) (*hitsContext, error) {
	if !validName(name) {
		return nil, fmt.Errorf("invalid context name %q", name)
	}
	path := filepath.Join(hitsContextDir(), name+".json")
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("context %q: %w", name, err)
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf(
				"no context %q in %s: create one with 'hits context add %s' or bring a nats CLI context over with 'hits context import %s'",
				name, hitsContextDir(), name, name)
		}
		return nil, fmt.Errorf("context %q: %w", name, err)
	}
	hc := &hitsContext{path: abs}
	if err := json.Unmarshal(b, hc); err != nil {
		return nil, fmt.Errorf("context %q: parse %s: %w", name, abs, err)
	}
	if oc := hc.OAuth; oc != nil {
		if oc.Issuer == "" || oc.ClientID == "" {
			return nil, fmt.Errorf("context %q: oauth needs both issuer and client_id (%s)", name, abs)
		}
		if len(oc.Scopes) == 0 {
			oc.Scopes = defaultScopes
		}
	}
	if err := hc.checkTokenConflict(name); err != nil {
		return nil, err
	}
	return hc, nil
}

// checkTokenConflict guards the one cross-field rule: an oauth context
// leaves nats.token empty. The subtree is read through natscontext's own
// Settings type, so the schema stays upstream's.
func (hc *hitsContext) checkTokenConflict(name string) error {
	if hc.OAuth == nil || len(hc.NATS) == 0 {
		return nil
	}
	var st natscontext.Settings
	if err := json.Unmarshal(hc.NATS, &st); err != nil {
		return fmt.Errorf("context %q: parse nats block in %s: %w", name, hc.path, err)
	}
	if st.Token != "" {
		return fmt.Errorf(
			"context %q sets both oauth and a static nats.token (%s): an oauth context leaves token empty", name, hc.path)
	}
	return nil
}

// natsSubtreeFile writes the nats subtree to a 0600 file in a fresh 0700
// temp directory for natscontext's loader, which accepts only a name or a
// file path — no exported Settings entry point (decision 0011 proposes
// one upstream; this shim disappears if it lands). The caller removes the
// directory via the returned func.
func (hc *hitsContext) natsSubtreeFile() (string, func(), error) {
	dir, err := os.MkdirTemp("", "hits-context-")
	if err != nil {
		return "", nil, fmt.Errorf("context temp dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	subtree := hc.NATS
	if len(subtree) == 0 {
		subtree = []byte("{}")
	}
	path := filepath.Join(dir, "nats.json")
	if err := os.WriteFile(path, subtree, 0o600); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("context temp file: %w", err)
	}
	return path, cleanup, nil
}

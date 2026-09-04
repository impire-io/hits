package connect

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// hitsContext is the slice of a hits context file this package reads
// itself: the oauth block (hits' one extension over the nats context
// schema) and the fields it must guard against. Everything else — url,
// creds, TLS, the whole nats context schema — is natscontext's to load,
// straight from the same file by path.
type hitsContext struct {
	path string

	Token string       `json:"token"`
	OAuth *OAuthConfig `json:"oauth"`
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

func loadHitsContext(name, path string) (*hitsContext, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("context %q: %w", name, err)
	}
	b, err := os.ReadFile(abs)
	if err != nil {
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
		if hc.Token != "" {
			return nil, fmt.Errorf(
				"context %q sets both oauth and a static token (%s): an oauth context leaves token empty", name, abs)
		}
		if len(oc.Scopes) == 0 {
			oc.Scopes = defaultScopes
		}
	}
	return hc, nil
}

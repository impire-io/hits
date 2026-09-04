package connect

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"time"
)

// resolveOAuth resolves a name to a hits context carrying an oauth block —
// the only shape the auth verbs act on.
func resolveOAuth(explicit string) (string, *hitsContext, error) {
	name, err := effectiveName(explicit)
	if err != nil {
		return "", nil, err
	}
	ref, err := lookup(name)
	if err != nil {
		return "", nil, err
	}
	if ref.hits == nil {
		if name == "" {
			return "", nil, errors.New("no context: pass --context or set a default")
		}
		return "", nil, fmt.Errorf(
			"context %q is not a hits context: auth works on %s/<name>.json with an oauth block", name, hitsContextDir())
	}
	if ref.hits.OAuth == nil {
		return "", nil, fmt.Errorf("context %q has no oauth block (%s): nothing to log in to", name, ref.hits.path)
	}
	return name, ref.hits, nil
}

// Login runs the device authorization grant for the named context and
// writes the token cache. The one interactive moment: connect paths never
// call this.
func Login(ctx context.Context, explicit string, out io.Writer) error {
	name, hc, err := resolveOAuth(explicit)
	if err != nil {
		return err
	}
	tok, err := deviceLogin(ctx, hc.OAuth, out)
	if err != nil {
		return err
	}
	if err := cacheFor(name).write(stored(tok)); err != nil {
		return fmt.Errorf("token cache: %w", err)
	}
	fmt.Fprintf(out, "logged in: context %q\n", name)
	return nil
}

// AuthStatus is what `hits auth status` prints.
type AuthStatus struct {
	Context    string    `json:"context"`
	Path       string    `json:"path"`
	Subject    string    `json:"subject,omitempty"`
	Expiry     time.Time `json:"expiry,omitzero"`
	HasRefresh bool      `json:"has_refresh"`
	NoCache    bool      `json:"no_cache,omitempty"`
}

// Status reports the named context's auth state; NoCache set means no one
// has logged in.
func Status(explicit string) (*AuthStatus, error) {
	name, hc, err := resolveOAuth(explicit)
	if err != nil {
		return nil, err
	}
	st := &AuthStatus{Context: name, Path: hc.path}
	tok, err := cacheFor(name).read()
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			st.NoCache = true
			return st, nil
		}
		return nil, fmt.Errorf("token cache: %w", err)
	}
	st.Subject = jwtSubject(tok.AccessToken)
	st.Expiry = tok.Expiry
	st.HasRefresh = tok.RefreshToken != ""
	return st, nil
}

// Logout deletes the named context's token cache; nothing is revoked at
// the IDP — that is the deployment's lever.
func Logout(explicit string) (string, error) {
	name, _, err := resolveOAuth(explicit)
	if err != nil {
		return "", err
	}
	if err := cacheFor(name).remove(); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return name, nil
		}
		return "", fmt.Errorf("token cache: %w", err)
	}
	return name, nil
}

// jwtSubject pulls the sub claim out of a JWT-shaped access token for
// display only — nothing is verified, and an opaque token yields "".
func jwtSubject(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	return claims.Sub
}

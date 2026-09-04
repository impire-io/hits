package connect

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
	"github.com/nats-io/nats.go"
	"golang.org/x/oauth2"
)

// refreshSkew is how close to expiry a token is treated as stale.
const refreshSkew = 60 * time.Second

// refreshTimeout bounds one IDP round-trip inside the token handler, which
// nats.go calls without a context.
const refreshTimeout = 15 * time.Second

// storedToken is one context's cached token pair.
type storedToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
}

// fresh reports whether the access token is still worth presenting. A zero
// expiry means the IDP declared none; presenting it is all there is.
func (t *storedToken) fresh(now time.Time) bool {
	return t.Expiry.IsZero() || now.Add(refreshSkew).Before(t.Expiry)
}

// tokenCache is one context's cache entry: a file under
// $XDG_STATE_HOME/hits/tokens, written by atomic rename, with a sibling
// lock file serializing refresh across processes.
type tokenCache struct {
	path string
}

func cacheFor(contextName string) tokenCache {
	return tokenCache{path: filepath.Join(stateDir(), "hits", "tokens", contextName+".json")}
}

// read is lock-free; refresh is what serializes.
func (c tokenCache) read() (*storedToken, error) {
	b, err := os.ReadFile(c.path)
	if err != nil {
		return nil, err
	}
	var t storedToken
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, fmt.Errorf("parse %s: %w", c.path, err)
	}
	return &t, nil
}

// write lands the pair with tight modes: 0700 directory, 0600 file,
// write-to-temp then rename.
func (c tokenCache) write(t *storedToken) error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(t)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(c.path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), c.path)
}

func (c tokenCache) remove() error { return os.Remove(c.path) }

// lock takes the cross-process refresh lock; the returned func releases it.
func (c tokenCache) lock() (func(), error) {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return nil, err
	}
	fl := flock.New(c.path + ".lock")
	if err := fl.Lock(); err != nil {
		return nil, err
	}
	return func() { _ = fl.Unlock() }, nil
}

// tokenHandler feeds the connection: nats.go invokes it on every connect
// and reconnect attempt. It returns the cached access token, first
// refreshing when stale — serialized under the file lock with a freshness
// re-check, so exactly one process spends the (possibly rotating) refresh
// token. On failure it degrades in the designed order: return the stale
// token and let nats.go's reconnect loop re-invoke.
func tokenHandler(contextName string, oc *OAuthConfig) nats.AuthTokenHandler {
	c := cacheFor(contextName)
	return func() string {
		tok, err := c.read()
		if err != nil {
			warnf("token cache for context %q: %v", contextName, err)
			return ""
		}
		if tok.fresh(time.Now()) {
			return tok.AccessToken
		}
		return c.refresh(contextName, oc, tok)
	}
}

// refresh is the locked slow path: re-check under the lock, spend the
// refresh token, rewrite the cache.
func (c tokenCache) refresh(contextName string, oc *OAuthConfig, stale *storedToken) string {
	unlock, err := c.lock()
	if err != nil {
		warnf("token lock for context %q: %v", contextName, err)
		return stale.AccessToken
	}
	defer unlock()

	if tok, err := c.read(); err == nil && tok.fresh(time.Now()) {
		return tok.AccessToken // someone else refreshed while we waited
	} else if err == nil {
		stale = tok // spend the latest pair, not the one read before the lock
	}

	ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
	defer cancel()
	fresh, err := refreshToken(ctx, oc, stale.RefreshToken)
	if err != nil {
		warnf("token refresh for context %q failed (will retry on next attempt): %v", contextName, err)
		return stale.AccessToken
	}
	next := stored(fresh)
	if next.RefreshToken == "" {
		next.RefreshToken = stale.RefreshToken // IDP did not rotate
	}
	if err := c.write(next); err != nil {
		warnf("token cache for context %q: %v", contextName, err)
	}
	return next.AccessToken
}

func stored(t *oauth2.Token) *storedToken {
	return &storedToken{AccessToken: t.AccessToken, RefreshToken: t.RefreshToken, Expiry: t.Expiry}
}

// warnf goes to stderr: the handler runs inside nats.go's (re)connect path
// where there is no error return — the operator log is the surface.
func warnf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "hits: "+format+"\n", args...)
}

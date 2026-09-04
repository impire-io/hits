package connect

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/impire-io/hits/internal/natstest"
)

func TestEffectiveNameResolution(t *testing.T) {
	cfgHome := setXDG(t)

	if name, _ := effectiveName("explicit"); name != "explicit" {
		t.Fatalf("explicit name: got %q", name)
	}
	if name, _ := effectiveName(""); name != "" {
		t.Fatalf("nothing configured: got %q", name)
	}

	// The nats CLI's selection marker is never consulted (decision 0011).
	if err := os.MkdirAll(filepath.Join(cfgHome, "nats"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgHome, "nats", "context.txt"), []byte("selected\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if name, _ := effectiveName(""); name != "" {
		t.Fatalf("nats selection marker must be ignored: got %q", name)
	}

	// The client config default is the one fallback.
	if err := os.MkdirAll(filepath.Join(cfgHome, "hits"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := []byte(`{"defaults": {"context": "configured"}}`)
	if err := os.WriteFile(filepath.Join(cfgHome, "hits", "config.json"), cfg, 0o600); err != nil {
		t.Fatal(err)
	}
	if name, _ := effectiveName(""); name != "configured" {
		t.Fatalf("config default: got %q", name)
	}
	if name, _ := effectiveName("explicit"); name != "explicit" {
		t.Fatalf("explicit still wins: got %q", name)
	}
}

// TestNatsCliContextsDoNotResolve: a name that exists only in the nats
// CLI's directory is not a hits context — the error names the import verb
// (decision 0011).
func TestNatsCliContextsDoNotResolve(t *testing.T) {
	cfgHome := setXDG(t)
	writeNatsContext(t, cfgHome, "plain", map[string]any{"url": "nats://x:4222"})

	_, err := Connect("plain", "hits-test")
	if err == nil || !strings.Contains(err.Error(), "hits context import plain") {
		t.Fatalf("want the import-naming error, got %v", err)
	}
}

func TestOAuthContextValidation(t *testing.T) {
	cfgHome := setXDG(t)

	writeHitsContext(t, cfgHome, "conflicted", map[string]any{
		"nats":  map[string]any{"url": "nats://x:4222", "token": "static"},
		"oauth": map[string]any{"issuer": "https://idp", "client_id": "c"},
	})
	if _, err := loadHitsContext("conflicted"); err == nil || !strings.Contains(err.Error(), "both oauth and a static nats.token") {
		t.Fatalf("want oauth+token conflict, got %v", err)
	}

	writeHitsContext(t, cfgHome, "halfway", map[string]any{
		"nats": map[string]any{"url": "nats://x:4222"}, "oauth": map[string]any{"issuer": "https://idp"},
	})
	if _, err := loadHitsContext("halfway"); err == nil || !strings.Contains(err.Error(), "issuer and client_id") {
		t.Fatalf("want missing-field error, got %v", err)
	}

	writeHitsContext(t, cfgHome, "scoped", nested("nats://x:4222", map[string]any{"issuer": "https://idp", "client_id": "c"}))
	hc, err := loadHitsContext("scoped")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(hc.OAuth.Scopes, " "); got != "openid offline_access" {
		t.Fatalf("default scopes: got %q", got)
	}
}

// TestConnectNestedAndImported: a nested-shape context connects, and an
// imported nats CLI context connects identically — the migration path
// (FR-10).
func TestConnectNestedAndImported(t *testing.T) {
	url := natstest.Start(t)

	cfgHome := setXDG(t)
	writeHitsContext(t, cfgHome, "direct", nested(url, nil))
	writeNatsContext(t, cfgHome, "legacy", map[string]any{"url": url})
	if _, err := ImportContext("legacy", ""); err != nil {
		t.Fatalf("import: %v", err)
	}

	for _, name := range []string{"direct", "legacy"} {
		nc, err := Connect(name, "hits-test")
		if err != nil {
			t.Fatalf("connect %s: %v", name, err)
		}
		nc.Close()
	}
}

func TestConnectOAuthFreshToken(t *testing.T) {
	const token = "tok-fresh"
	url := natstest.StartWithToken(t, token)
	idp := startIDP(t, "never-used")

	cfgHome := setXDG(t)
	writeHitsContext(t, cfgHome, "sso", nested(url, oauthBlock(idp)))
	if err := cacheFor("sso").write(&storedToken{
		AccessToken: token, RefreshToken: "rt-1", Expiry: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	nc, err := Connect("sso", "hits-test")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	nc.Close()
	if idp.refreshCount() != 0 {
		t.Fatalf("fresh token must not refresh, idp saw %d calls", idp.refreshCount())
	}
}

func TestConnectOAuthRefreshesStaleToken(t *testing.T) {
	const token = "tok-new"
	url := natstest.StartWithToken(t, token)
	idp := startIDP(t, token)

	cfgHome := setXDG(t)
	writeHitsContext(t, cfgHome, "sso", nested(url, oauthBlock(idp)))
	if err := cacheFor("sso").write(&storedToken{
		AccessToken: "tok-stale", RefreshToken: "rt-old", Expiry: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	nc, err := Connect("sso", "hits-test")
	if err != nil {
		t.Fatalf("connect (stale cache should refresh): %v", err)
	}
	nc.Close()

	tok, err := cacheFor("sso").read()
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != token || tok.RefreshToken == "rt-old" {
		t.Fatalf("cache not rotated: %+v", tok)
	}
}

func TestConnectOAuthNoCacheFailsFast(t *testing.T) {
	idp := startIDP(t, "unused")
	cfgHome := setXDG(t)
	writeHitsContext(t, cfgHome, "sso", nested("nats://127.0.0.1:1", oauthBlock(idp)))

	_, err := Connect("sso", "hits-test")
	if err == nil || !strings.Contains(err.Error(), "hits auth login --context sso") {
		t.Fatalf("want login-naming error before any dial, got %v", err)
	}
}

// TestConcurrentRefreshSpendsOnce: rotation plus reuse detection means a
// second spend of the same refresh token kills the grant — the lock must
// make the spend exclusive (FR-08 of spec 008, unchanged here).
func TestConcurrentRefreshSpendsOnce(t *testing.T) {
	const token = "tok-rotated"
	idp := startIDP(t, token)

	cfgHome := setXDG(t)
	writeHitsContext(t, cfgHome, "sso", nested("nats://127.0.0.1:1", oauthBlock(idp)))
	oc := &OAuthConfig{Issuer: idp.srv.URL, ClientID: "hits-test", Scopes: defaultScopes}
	if err := cacheFor("sso").write(&storedToken{
		AccessToken: "tok-stale", RefreshToken: "rt-old", Expiry: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	results := make([]string, 8)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = tokenHandler("sso", oc)()
		}()
	}
	wg.Wait()

	for i, got := range results {
		if got != token {
			t.Fatalf("handler %d returned %q, want %q", i, got, token)
		}
	}
	if n := idp.refreshCount(); n != 1 {
		t.Fatalf("refresh token spent %d times, want exactly 1", n)
	}
}

func TestLoginWritesCache(t *testing.T) {
	idp := startIDP(t, "tok-login")
	cfgHome := setXDG(t)
	writeHitsContext(t, cfgHome, "sso", nested("nats://127.0.0.1:1", oauthBlock(idp)))

	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := Login(ctx, "sso", &out); err != nil {
		t.Fatalf("login: %v", err)
	}
	if !strings.Contains(out.String(), "UC-1234") {
		t.Fatalf("login output names no user code: %q", out.String())
	}
	tok, err := cacheFor("sso").read()
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "tok-login" || tok.RefreshToken == "" {
		t.Fatalf("cache after login: %+v", tok)
	}
	if info, err := os.Stat(cacheFor("sso").path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("cache mode: %v %v", info.Mode(), err)
	}
}

func TestAuthVerbsRequireOAuthContext(t *testing.T) {
	cfgHome := setXDG(t)
	writeNatsContext(t, cfgHome, "plain", map[string]any{"url": "nats://x:4222"})
	writeHitsContext(t, cfgHome, "nooauth", nested("nats://x:4222", nil))

	if _, err := Status("plain"); err == nil || !strings.Contains(err.Error(), "no context \"plain\"") {
		t.Fatalf("nats-only context: got %v", err)
	}
	if _, err := Status("nooauth"); err == nil || !strings.Contains(err.Error(), "no oauth block") {
		t.Fatalf("hits context without oauth: got %v", err)
	}
}

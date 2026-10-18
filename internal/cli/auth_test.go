package cli_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// startIDP is a minimal OIDC provider granting the device flow on the
// first poll; the full IDP behavior (rotation, reuse detection) is proved
// in internal/connect's own tests — here the verbs are the subject.
func startIDP(t *testing.T) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	mux := http.NewServeMux()
	writeJSON := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"issuer":                        srv.URL,
			"device_authorization_endpoint": srv.URL + "/device",
			"token_endpoint":                srv.URL + "/token",
		})
	})
	mux.HandleFunc("/device", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"device_code": "dev-code", "user_code": "UC-9876",
			"verification_uri": srv.URL + "/activate", "interval": 1, "expires_in": 600,
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"access_token": "tok-cli", "token_type": "Bearer",
			"refresh_token": "rt-cli", "expires_in": 3600,
		})
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func writeAuthContext(t *testing.T, cfgHome string, idp *httptest.Server) {
	t.Helper()
	dir := filepath.Join(cfgHome, "hits", "context")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{
		"nats":  map[string]any{"url": "nats://127.0.0.1:1"},
		"oauth": map[string]any{"issuer": idp.URL, "client_id": "hits-test"},
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sso.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestAuthVerbs drives login → status → logout → status through the CLI.
// The auth verbs never touch NATS, so the guard connector stands watch.
func TestAuthVerbs(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	idp := startIDP(t)
	writeAuthContext(t, cfgHome, idp)

	out := run(t, guardConnector(t), "auth", "login", "--context", "sso")
	if !strings.Contains(out, "UC-9876") || !strings.Contains(out, `logged in: context "sso"`) {
		t.Fatalf("login output: %q", out)
	}

	out = run(t, guardConnector(t), "auth", "status", "--context", "sso")
	if !strings.Contains(out, "sso") || !strings.Contains(out, "refresh  true") {
		t.Fatalf("status output: %q", out)
	}

	out = run(t, guardConnector(t), "auth", "logout", "--context", "sso")
	if !strings.Contains(out, `logged out: context "sso"`) {
		t.Fatalf("logout output: %q", out)
	}

	out = run(t, guardConnector(t), "auth", "status", "--context", "sso")
	if !strings.Contains(out, "not logged in") {
		t.Fatalf("status after logout: %q", out)
	}
}

// TestAuthGlobalContextFlag: the global --context placement works too.
func TestAuthGlobalContextFlag(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	idp := startIDP(t)
	writeAuthContext(t, cfgHome, idp)

	out := run(t, guardConnector(t), "--context", "sso", "auth", "status")
	if !strings.Contains(out, "not logged in") {
		t.Fatalf("status output: %q", out)
	}
}

// TestAuthRejectsNonOAuthTargets: a name that lives only in the nats
// CLI's directory does not resolve at all (decision 0011), and a hits
// context without an oauth block is refused before anything interactive
// starts.
func TestAuthRejectsNonOAuthTargets(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("EDITOR", "") // context add must not spawn the ambient editor

	dir := filepath.Join(cfgHome, "nats", "context")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plain.json"), []byte(`{"url":"nats://x:4222"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	err := runErr(t, guardConnector(t), "auth", "login", "--context", "plain")
	if !strings.Contains(err.Error(), "hits context import plain") {
		t.Fatalf("login on a nats-only name: got %v", err)
	}

	run(t, guardConnector(t), "context", "add", "nooauth", "--url", "nats://x:4222")
	err = runErr(t, guardConnector(t), "auth", "login", "--context", "nooauth")
	if !strings.Contains(err.Error(), "no oauth block") {
		t.Fatalf("login without oauth block: got %v", err)
	}
}

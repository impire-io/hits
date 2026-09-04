package connect

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// fakeIDP is an httptest OIDC provider speaking discovery, device
// authorization, and the token endpoint — refresh rotation with reuse
// detection included, the behavior the cache lock exists for.
type fakeIDP struct {
	srv *httptest.Server

	mu           sync.Mutex
	accessToken  string // what the next grant hands out
	refreshCalls int
	spent        map[string]bool // refresh tokens already used
	nextRefresh  int
}

func startIDP(t *testing.T, accessToken string) *fakeIDP {
	t.Helper()
	idp := &fakeIDP{accessToken: accessToken, spent: map[string]bool{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"issuer":                        idp.srv.URL,
			"device_authorization_endpoint": idp.srv.URL + "/device",
			"token_endpoint":                idp.srv.URL + "/token",
		})
	})
	mux.HandleFunc("/device", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"device_code":      "dev-code",
			"user_code":        "UC-1234",
			"verification_uri": idp.srv.URL + "/activate",
			"interval":         1,
			"expires_in":       600,
		})
	})
	mux.HandleFunc("/token", idp.token)
	idp.srv = httptest.NewServer(mux)
	t.Cleanup(idp.srv.Close)
	return idp
}

func (idp *fakeIDP) token(w http.ResponseWriter, r *http.Request) {
	idp.mu.Lock()
	defer idp.mu.Unlock()
	switch r.FormValue("grant_type") {
	case "urn:ietf:params:oauth:grant-type:device_code":
		idp.grant(w, "rt-1")
	case "refresh_token":
		rt := r.FormValue("refresh_token")
		if idp.spent[rt] {
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]any{"error": "invalid_grant"})
			return
		}
		idp.spent[rt] = true
		idp.refreshCalls++
		idp.nextRefresh++
		idp.grant(w, fmt.Sprintf("rt-%d", idp.nextRefresh+1))
	default:
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]any{"error": "unsupported_grant_type"})
	}
}

// grant answers with the current access token and the given (rotated)
// refresh token. Callers hold idp.mu.
func (idp *fakeIDP) grant(w http.ResponseWriter, refresh string) {
	writeJSON(w, map[string]any{
		"access_token":  idp.accessToken,
		"token_type":    "Bearer",
		"refresh_token": refresh,
		"expires_in":    3600,
	})
}

func (idp *fakeIDP) refreshCount() int {
	idp.mu.Lock()
	defer idp.mu.Unlock()
	return idp.refreshCalls
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// setXDG points both XDG roots at per-test directories.
func setXDG(t *testing.T) (cfgHome string) {
	t.Helper()
	cfgHome = t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	return cfgHome
}

func writeContextFile(t *testing.T, dir, name string, body map[string]any) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name+".json")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeHitsContext(t *testing.T, cfgHome, name string, body map[string]any) string {
	t.Helper()
	return writeContextFile(t, filepath.Join(cfgHome, "hits", "context"), name, body)
}

func writeNatsContext(t *testing.T, cfgHome, name string, body map[string]any) string {
	t.Helper()
	return writeContextFile(t, filepath.Join(cfgHome, "nats", "context"), name, body)
}

func oauthBlock(idp *fakeIDP) map[string]any {
	return map[string]any{"issuer": idp.srv.URL, "client_id": "hits-test"}
}

// nested builds a context body in the 0011 document shape: the connection
// under "nats", hits blocks at the top level.
func nested(url string, oauth map[string]any) map[string]any {
	body := map[string]any{"nats": map[string]any{"url": url}}
	if oauth != nil {
		body["oauth"] = oauth
	}
	return body
}

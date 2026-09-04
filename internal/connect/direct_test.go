package connect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/impire-io/hits/internal/natstest"
)

// clearNATSEnv blanks the nats CLI's environment variables so a
// developer's real settings never leak into a test, restoring them when
// the test ends.
func clearNATSEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"NATS_URL", "NATS_CREDS", "NATS_USER", "NATS_PASSWORD",
		"NATS_NKEY", "NATS_CERT", "NATS_KEY", "NATS_CA",
	} {
		t.Setenv(key, "")
	}
}

// TestDialEnvURL: with nothing flagged, $NATS_URL alone carries the
// connection — the CI-and-container path.
func TestDialEnvURL(t *testing.T) {
	url := natstest.Start(t)
	setXDG(t)
	clearNATSEnv(t)
	t.Setenv("NATS_URL", url)

	nc, err := Dial("", Direct{}, "hits-test")
	if err != nil {
		t.Fatalf("dial from $NATS_URL: %v", err)
	}
	nc.Close()
}

// TestDialServerFlagBeatsEnv: a flagged server outranks a $NATS_URL
// pointing somewhere dead.
func TestDialServerFlagBeatsEnv(t *testing.T) {
	url := natstest.Start(t)
	setXDG(t)
	clearNATSEnv(t)
	t.Setenv("NATS_URL", "nats://127.0.0.1:1")

	nc, err := Dial("", Direct{Servers: url}, "hits-test")
	if err != nil {
		t.Fatalf("dial with --server: %v", err)
	}
	nc.Close()
}

// TestDialContextBeatsEnvURL: an explicit context outranks the
// environment — $NATS_URL pointing somewhere dead must not matter.
func TestDialContextBeatsEnvURL(t *testing.T) {
	url := natstest.Start(t)
	cfgHome := setXDG(t)
	clearNATSEnv(t)
	t.Setenv("NATS_URL", "nats://127.0.0.1:1")
	writeHitsContext(t, cfgHome, "real", nested(url, nil))

	nc, err := Dial("real", Direct{}, "hits-test")
	if err != nil {
		t.Fatalf("dial through the context: %v", err)
	}
	nc.Close()
}

// TestDialEnvAuthStaysDormantWithoutServer: an exported $NATS_CREDS must
// not bleed into a context connection — without a server URL in play the
// auth variables are inert, and the configured default context wins.
func TestDialEnvAuthStaysDormantWithoutServer(t *testing.T) {
	url := natstest.Start(t)
	cfgHome := setXDG(t)
	clearNATSEnv(t)
	t.Setenv("NATS_CREDS", filepath.Join(t.TempDir(), "does-not-exist.creds"))

	writeHitsContext(t, cfgHome, "configured", nested(url, nil))
	if err := os.WriteFile(filepath.Join(cfgHome, "hits", "config.json"),
		[]byte(`{"defaults": {"context": "configured"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	nc, err := Dial("", Direct{}, "hits-test")
	if err != nil {
		t.Fatalf("dial through the default context: %v", err)
	}
	nc.Close()
}

// TestDialUserPassword: user and password reach the wire — a server that
// requires them refuses the anonymous dial and accepts the authorized one.
func TestDialUserPassword(t *testing.T) {
	url := natstest.StartWithUserPass(t, "svc", "hunter2")
	setXDG(t)
	clearNATSEnv(t)

	if nc, err := Dial("", Direct{Servers: url}, "hits-test"); err == nil {
		nc.Close()
		t.Fatal("anonymous dial to a user/pass server must fail")
	}

	nc, err := Dial("", Direct{Servers: url, User: "svc", Password: "hunter2"}, "hits-test")
	if err != nil {
		t.Fatalf("dial with user/password: %v", err)
	}
	nc.Close()

	t.Setenv("NATS_USER", "svc")
	t.Setenv("NATS_PASSWORD", "hunter2")
	nc, err = Dial("", Direct{Servers: url}, "hits-test")
	if err != nil {
		t.Fatalf("dial with $NATS_USER/$NATS_PASSWORD: %v", err)
	}
	nc.Close()
}

// TestDialRefusesHalfPairsAndMixing: the mistakes natscontext would
// swallow silently are hard errors, before anything dials.
func TestDialRefusesHalfPairsAndMixing(t *testing.T) {
	setXDG(t)
	clearNATSEnv(t)

	for name, tc := range map[string]struct {
		contextName string
		d           Direct
		want        string
	}{
		"context plus server":   {"prod", Direct{Servers: "nats://x:4222"}, "not both"},
		"context plus creds":    {"prod", Direct{Creds: "f.creds"}, "not both"},
		"auth without a server": {"", Direct{Creds: "f.creds"}, "need a server"},
		"password without user": {"", Direct{Servers: "nats://x:4222", Password: "p"}, "--password goes with --user"},
		"cert without key":      {"", Direct{Servers: "nats://x:4222", TLSCert: "c.pem"}, "go together"},
		"key without cert":      {"", Direct{Servers: "nats://x:4222", TLSKey: "k.pem"}, "go together"},
	} {
		if _, err := Dial(tc.contextName, tc.d, "hits-test"); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error = %v, want %q", name, err, tc.want)
		}
	}
}

// TestDirectSettingsSubtree: the settings marshal into the exact
// natscontext Settings keys a context's nats block carries — the
// by-construction guarantee the design leans on.
func TestDirectSettingsSubtree(t *testing.T) {
	d := Direct{
		Servers: "nats://a:4222", Creds: "f.creds", User: "u", Password: "p",
		Nkey: "seed.nk", TLSCert: "c.pem", TLSKey: "k.pem", TLSCA: "ca.pem",
	}
	b, err := json.Marshal(d.settings())
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"url": "nats://a:4222", "creds": "f.creds", "user": "u", "password": "p",
		"nkey": "seed.nk", "cert": "c.pem", "key": "k.pem", "ca": "ca.pem",
	} {
		if got[key] != want {
			t.Errorf("settings[%q] = %v, want %q", key, got[key], want)
		}
	}
}

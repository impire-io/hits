package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestContextVerbs drives the family end to end: add → import → select →
// ls → rm. The verbs never touch NATS, so the guard connector stands
// watch; $EDITOR stays unset, so add just leaves the scaffold.
func TestContextVerbs(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("EDITOR", "")

	out := run(t, guardConnector(t), "context", "add", "dev", "--url", "nats://127.0.0.1:4222")
	if !strings.Contains(out, `created context "dev"`) {
		t.Fatalf("add output: %q", out)
	}
	if err := runErr(t, guardConnector(t), "context", "add", "dev"); !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("re-add: got %v", err)
	}

	natsDir := filepath.Join(cfgHome, "nats", "context")
	if err := os.MkdirAll(natsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(natsDir, "legacy.json"), []byte(`{"url":"nats://x:4222"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out = run(t, guardConnector(t), "context", "import", "legacy")
	if !strings.Contains(out, `imported nats context "legacy" as hits context "legacy"`) {
		t.Fatalf("import output: %q", out)
	}

	out = run(t, guardConnector(t), "context", "select", "dev")
	if !strings.Contains(out, `default context is now "dev"`) {
		t.Fatalf("select output: %q", out)
	}

	out = run(t, guardConnector(t), "context", "ls")
	if !strings.Contains(out, "* dev") || !strings.Contains(out, "  legacy") {
		t.Fatalf("ls output: %q", out)
	}

	out = run(t, guardConnector(t), "--json", "context", "ls")
	var infos []struct {
		Name    string `json:"name"`
		Default bool   `json:"default"`
	}
	if err := json.Unmarshal([]byte(out), &infos); err != nil || len(infos) != 2 {
		t.Fatalf("ls --json: %q (%v)", out, err)
	}

	out = run(t, guardConnector(t), "context", "rm", "legacy")
	if !strings.Contains(out, `removed context "legacy"`) {
		t.Fatalf("rm output: %q", out)
	}
	out = run(t, guardConnector(t), "context", "ls")
	if strings.Contains(out, "legacy") {
		t.Fatalf("ls after rm: %q", out)
	}
}

// TestContextEditRequiresEditor: edit is nothing but $EDITOR on the file,
// so its absence is the verb's one hard error.
func TestContextEditRequiresEditor(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("EDITOR", "")

	run(t, guardConnector(t), "context", "add", "dev")
	if err := runErr(t, guardConnector(t), "context", "edit", "dev"); !strings.Contains(err.Error(), "$EDITOR") {
		t.Fatalf("edit without EDITOR: got %v", err)
	}
	if err := runErr(t, guardConnector(t), "context", "edit", "absent"); !strings.Contains(err.Error(), "no context") {
		t.Fatalf("edit missing context: got %v", err)
	}
}

// TestContextSelectMissing: select refuses a name with no file behind it.
func TestContextSelectMissing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	if err := runErr(t, guardConnector(t), "context", "select", "absent"); !strings.Contains(err.Error(), "no context") {
		t.Fatalf("select missing: got %v", err)
	}
}

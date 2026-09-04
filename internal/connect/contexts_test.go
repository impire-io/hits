package connect

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestImportPreservesUnknownFields: the wrap is byte-for-byte — fields
// hits knows nothing about survive the trip under "nats" (FR-07).
func TestImportPreservesUnknownFields(t *testing.T) {
	cfgHome := setXDG(t)
	writeNatsContext(t, cfgHome, "legacy", map[string]any{
		"url": "nats://x:4222", "creds": "~/x.creds", "color_scheme": "dark", "made_up_field": "survives",
	})

	path, err := ImportContext("legacy", "brought-over")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		NATS map[string]any `json:"nats"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{"url": "nats://x:4222", "creds": "~/x.creds", "made_up_field": "survives"} {
		if got := doc.NATS[k]; got != want {
			t.Fatalf("nats.%s = %v, want %q", k, got, want)
		}
	}

	if _, err := ImportContext("legacy", "brought-over"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("re-import over an existing name: got %v", err)
	}
	if _, err := ImportContext("absent", ""); err == nil || !strings.Contains(err.Error(), "no nats CLI context") {
		t.Fatalf("missing source: got %v", err)
	}
}

// TestSelectPreservesConfig: select writes defaults.context and nothing
// else — the actor default and fields hits does not know survive (FR-08).
func TestSelectPreservesConfig(t *testing.T) {
	cfgHome := setXDG(t)
	writeHitsContext(t, cfgHome, "dev", nested("nats://x:4222", nil))

	if err := os.WriteFile(configPath(), []byte(`{"defaults":{"context":"old","actor":"daan"},"future":"kept"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SelectContext("dev"); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(configPath())
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	defaults := doc["defaults"].(map[string]any)
	if defaults["context"] != "dev" || defaults["actor"] != "daan" || doc["future"] != "kept" {
		t.Fatalf("config after select: %s", b)
	}

	if err := SelectContext("absent"); err == nil || !strings.Contains(err.Error(), "no context") {
		t.Fatalf("selecting a missing context: got %v", err)
	}
}

// TestListContextsMarksDefault: ls names hits' directory only, the config
// default marked (FR-05).
func TestListContextsMarksDefault(t *testing.T) {
	cfgHome := setXDG(t)
	if infos, err := ListContexts(); err != nil || len(infos) != 0 {
		t.Fatalf("empty directory: %v %v", infos, err)
	}
	writeHitsContext(t, cfgHome, "one", nested("nats://x:4222", nil))
	writeHitsContext(t, cfgHome, "two", nested("nats://y:4222", nil))
	writeNatsContext(t, cfgHome, "invisible", map[string]any{"url": "nats://z:4222"})
	if err := SelectContext("two"); err != nil {
		t.Fatal(err)
	}

	infos, err := ListContexts()
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 2 || infos[0].Name != "one" || infos[1].Name != "two" {
		t.Fatalf("list: %+v", infos)
	}
	if infos[0].Default || !infos[1].Default {
		t.Fatalf("default mark: %+v", infos)
	}
}

// TestAddScaffoldsAndRefuses: add writes the nested scaffold 0600 and
// refuses an existing name (FR-06).
func TestAddScaffoldsAndRefuses(t *testing.T) {
	setXDG(t)

	path, err := AddContext("dev", "nats://x:4222", &OAuthConfig{Issuer: "https://idp", ClientID: "cli"})
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("scaffold mode: %v %v", info, err)
	}
	hc, err := loadHitsContext("dev")
	if err != nil {
		t.Fatalf("scaffold does not load back: %v", err)
	}
	if hc.OAuth == nil || hc.OAuth.Issuer != "https://idp" {
		t.Fatalf("scaffold oauth: %+v", hc.OAuth)
	}

	if _, err := AddContext("dev", "", nil); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("re-add: got %v", err)
	}
	if _, err := AddContext("half", "", &OAuthConfig{Issuer: "https://idp"}); err == nil || !strings.Contains(err.Error(), "issuer and client_id") {
		t.Fatalf("half oauth: got %v", err)
	}
}

// TestRemoveContext: rm deletes the file; a missing name errs plainly.
func TestRemoveContext(t *testing.T) {
	cfgHome := setXDG(t)
	writeHitsContext(t, cfgHome, "gone", nested("nats://x:4222", nil))

	if err := RemoveContext("gone"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveContext("gone"); err == nil || !strings.Contains(err.Error(), "no context") {
		t.Fatalf("double rm: got %v", err)
	}
}

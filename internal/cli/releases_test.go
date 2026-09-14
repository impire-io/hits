package cli_test

import (
	"strings"
	"testing"
)

// TestReleaseCommands drives the release verbs end to end: register and
// list under the resolved initiative, targets set / cleared / frozen by
// the straggler triage, the gate refusing the ship until the pass runs,
// and the claim hand-back keeping its verb (decision 0017).
func TestReleaseCommands(t *testing.T) {
	h := startStore(t)
	connect := h.connector()
	t.Setenv("HITS_ACTOR", "daan")

	out := run(t, connect, "release", "register", "0.5", "The 0.5 release", "--description", "first cut")
	wantContains(t, out, "hits 0.5", "The 0.5 release", "first cut")

	out = run(t, connect, "release", "list")
	wantContains(t, out, "0.5", "open", "The 0.5 release")

	// Targets ride create and edit; the empty string clears.
	a := itemID(t, run(t, connect, "create", "--type", "bug", "--target", "0.5", "must land"))
	wantContains(t, run(t, connect, "get", a), "target: 0.5")
	b := itemID(t, run(t, connect, "create", "--type", "bug", "targeted later"))
	wantContains(t, run(t, connect, "edit", b, "--target", "0.5"), "target: 0.5")

	// The straggler gate names the offenders; the cut is the triage pass.
	err := runErr(t, connect, "release", "ship", "0.5", "--ref", "tag:v0.5.0 the evidence")
	if !strings.Contains(err.Error(), "open-targets") ||
		!strings.Contains(err.Error(), a) || !strings.Contains(err.Error(), b) {
		t.Errorf("ship with stragglers = %v, want open-targets naming %s and %s", err, a, b)
	}
	run(t, connect, "resolve", a, "--fixed-by", "commit:abc123 landed")
	if out = run(t, connect, "edit", b, "--target", ""); strings.Contains(out, "target:") {
		t.Errorf("cleared target still prints: %s", out)
	}

	out = run(t, connect, "release", "ship", "0.5", "--ref", "tag:v0.5.0 the evidence", "--note", "cut clean")
	wantContains(t, out, "shipped: tag:v0.5.0", "ship-note: cut clean")
	out = run(t, connect, "release", "list")
	wantContains(t, out, "0.5", "shipped")

	// Terminal slugs refuse new targets; a ship needs its refs before
	// anything dials.
	if err := runErr(t, connect, "edit", b, "--target", "0.5"); !strings.Contains(err.Error(), "release-shipped") {
		t.Errorf("target a shipped release: %v", err)
	}
	if err := runErr(t, guardConnector(t), "release", "ship", "0.6"); !strings.Contains(err.Error(), "--ref is required") {
		t.Errorf("ship without refs: %v", err)
	}
	if err := runErr(t, guardConnector(t), "release", "retire", "0.6"); !strings.Contains(err.Error(), "--reason is required") {
		t.Errorf("retire without reason: %v", err)
	}

	// Retirement drops the slug from the list and refuses re-registration.
	run(t, connect, "release", "register", "0.6", "Mistyped")
	out = run(t, connect, "release", "retire", "0.6", "--reason", "mistyped")
	wantContains(t, out, "retired: mistyped")
	if out = run(t, connect, "release", "list"); strings.Contains(out, "0.6") {
		t.Errorf("retired slug still listed: %s", out)
	}
	if err := runErr(t, connect, "release", "register", "0.6", "Again"); !strings.Contains(err.Error(), "slug-retired") {
		t.Errorf("re-register retired slug: %v", err)
	}

	// The claim hand-back keeps its verb beside the vocabulary.
	c := itemID(t, run(t, connect, "create", "--type", "bug", "claimed and released"))
	run(t, connect, "claim", c)
	if out = run(t, connect, "release", c); strings.Contains(out, "claimed-by") {
		t.Errorf("released item still claimed: %s", out)
	}
}

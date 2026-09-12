package contract_test

import (
	"testing"

	"github.com/impire-io/hits/contract"
)

// TestParseItemID: the trailing-digits rule of decision 0016 — bare
// decimals are legacy IDs, <initiative>-<n> carries its initiative, and
// nothing else parses.
func TestParseItemID(t *testing.T) {
	cases := []struct {
		id         string
		initiative string
		ok         bool
	}{
		{"17", "", true},
		{"1", "", true},
		{"hits-19", "hits", true},
		{"chronicle-hq-4", "chronicle-hq", true},
		{"", "", false},
		{"hits", "", false},
		{"hits-", "", false},
		{"-19", "", false},
		{"team-42-7", "", false}, // team-42 is not a valid initiative slug
		{"Bad-1", "", false},
		{"hits-19x", "", false},
	}
	for _, tc := range cases {
		initiative, ok := contract.ParseItemID(tc.id)
		if initiative != tc.initiative || ok != tc.ok {
			t.Errorf("ParseItemID(%q) = %q,%v; want %q,%v", tc.id, initiative, ok, tc.initiative, tc.ok)
		}
	}
}

// TestValidInitiativeSlug: slugs are refused a trailing all-digit segment
// so item IDs stay unambiguous.
func TestValidInitiativeSlug(t *testing.T) {
	for slug, want := range map[string]bool{
		"hits":         true,
		"chronicle-hq": true,
		"a1-b2c":       true,
		"team-42":      false,
		"42":           false,
		"hits-":        false,
		"Bad":          false,
		"":             false,
	} {
		if got := contract.ValidInitiativeSlug(slug); got != want {
			t.Errorf("ValidInitiativeSlug(%q) = %v, want %v", slug, got, want)
		}
	}
}

// TestInitiativeLifecycle: register and retire, the 0015 lifecycle on the
// second vocabulary — with the stricter slug rule at the door.
func TestInitiativeLifecycle(t *testing.T) {
	reg := mkOp(t, contract.OpRegistered, "hits", "daan", contract.RegisteredPayload{Name: "The HITS platform"})
	if err := contract.CheckInitiativeOp(nil, reg); err != nil {
		t.Fatalf("check register: %v", err)
	}
	i, err := contract.ApplyInitiative(nil, reg, 1)
	if err != nil {
		t.Fatalf("apply register: %v", err)
	}
	if i.Slug != "hits" || i.Name != "The HITS platform" || i.Seq != 1 {
		t.Fatalf("initiative = %+v", i)
	}
	wantInvariant(t, contract.CheckInitiativeOp(i, reg), "already-registered")
	wantInvariant(t, contract.CheckInitiativeOp(nil, mkOp(t, contract.OpRegistered, "team-42", "daan",
		contract.RegisteredPayload{Name: "x"})), "invalid-initiative")
	wantInvariant(t, contract.CheckInitiativeOp(nil, mkOp(t, contract.OpRegistered, "hits", "daan",
		contract.RegisteredPayload{})), "empty-name")

	ret := mkOp(t, contract.OpRetired, "hits", "daan", contract.RetiredPayload{Reason: "wound down"})
	wantInvariant(t, contract.CheckInitiativeOp(nil, ret), "unregistered-initiative")
	if err := contract.CheckInitiativeOp(i, ret); err != nil {
		t.Fatalf("check retire: %v", err)
	}
	ri, err := contract.ApplyInitiative(i, ret, 2)
	if err != nil {
		t.Fatalf("apply retire: %v", err)
	}
	if !ri.Retired || ri.RetireReason != "wound down" || ri.Name != "The HITS platform" {
		t.Fatalf("retired initiative = %+v", ri)
	}
	wantInvariant(t, contract.CheckInitiativeOp(ri, ret), "already-retired")
	wantInvariant(t, contract.CheckInitiativeOp(ri, reg), "slug-retired")
	if again, err := contract.ApplyInitiative(ri, ret, 2); err != nil || again != ri {
		t.Fatalf("replayed retire = %+v, %v; want the snapshot unchanged", again, err)
	}
}

// TestProjectAssignment: assigned moves a project's initiative — a move
// or the pre-0016 backfill — and folds onto the kept key.
func TestProjectAssignment(t *testing.T) {
	// A pre-0016 registration: no initiative in the payload.
	legacy, err := contract.ApplyProject(nil, mkOp(t, contract.OpRegistered, "hits", "daan",
		contract.RegisteredPayload{Name: "HITS product repo"}), 1)
	if err != nil {
		t.Fatalf("apply legacy register: %v", err)
	}
	if legacy.Initiative != "" {
		t.Fatalf("legacy registration carries an initiative: %+v", legacy)
	}

	assign := mkOp(t, contract.OpAssigned, "hits", "daan", contract.AssignedPayload{Initiative: "hits"})
	wantInvariant(t, contract.CheckProjectOp(nil, assign), "unregistered-project")
	wantInvariant(t, contract.CheckProjectOp(legacy, mkOp(t, contract.OpAssigned, "hits", "daan",
		contract.AssignedPayload{})), "initiative-required")
	wantInvariant(t, contract.CheckProjectOp(legacy, mkOp(t, contract.OpAssigned, "hits", "daan",
		contract.AssignedPayload{Initiative: "team-42"})), "invalid-initiative")
	if err := contract.CheckProjectOp(legacy, assign); err != nil {
		t.Fatalf("check assign: %v", err)
	}
	p, err := contract.ApplyProject(legacy, assign, 2)
	if err != nil {
		t.Fatalf("apply assign: %v", err)
	}
	if p.Initiative != "hits" || p.Name != "HITS product repo" || p.Seq != 2 {
		t.Fatalf("assigned project = %+v", p)
	}

	retired, _ := contract.ApplyProject(p, mkOp(t, contract.OpRetired, "hits", "daan",
		contract.RetiredPayload{Reason: "superseded"}), 3)
	wantInvariant(t, contract.CheckProjectOp(retired, assign), "retired-project")
}

// TestItemInitiativeAssignment: the initiative is writable by edit only
// on legacy bare-ID items; a prefixed item carries it in the ID.
func TestItemInitiativeAssignment(t *testing.T) {
	legacy := newBug(t) // ID "1"
	init := "hits"
	if err := contract.CheckOp(legacy, mkOp(t, contract.OpEdited, "1", "daan",
		contract.EditedPayload{Initiative: &init})); err != nil {
		t.Fatalf("assign legacy item: %v", err)
	}
	bad := "team-42"
	wantInvariant(t, contract.CheckOp(legacy, mkOp(t, contract.OpEdited, "1", "daan",
		contract.EditedPayload{Initiative: &bad})), "invalid-initiative")

	edited, err := contract.Apply(legacy, mkOp(t, contract.OpEdited, "1", "daan",
		contract.EditedPayload{Initiative: &init}), 2)
	if err != nil {
		t.Fatalf("apply assignment: %v", err)
	}
	if edited.Initiative != "hits" {
		t.Fatalf("assigned item = %+v", edited)
	}

	prefixed := step(t, nil, mkOp(t, contract.OpCreated, "hits-1", "daan", contract.CreatedPayload{
		Type: contract.Bug, Report: "prefixed from birth", Initiative: "hits",
	}), 1)
	if prefixed.Initiative != "hits" {
		t.Fatalf("created item lost its initiative: %+v", prefixed)
	}
	wantInvariant(t, contract.CheckOp(prefixed, mkOp(t, contract.OpEdited, "hits-1", "daan",
		contract.EditedPayload{Initiative: &init})), "initiative-immutable")
}

// TestTerminalItemsAcceptTheBackfill: a closed legacy item still takes
// its one-time initiative assignment — the 0016 backfill touches history
// the way notes and links do — while every other edit stays refused.
func TestTerminalItemsAcceptTheBackfill(t *testing.T) {
	it := newBug(t)
	it = step(t, it, mkOp(t, contract.OpTransitioned, "1", "daan", contract.TransitionedPayload{
		To: contract.Resolved, Closed: "2026-09-12", FixedBy: []contract.FixRef{{Commit: "abc"}},
	}), 2)

	init := "hits"
	assign := mkOp(t, contract.OpEdited, "1", "daan", contract.EditedPayload{Initiative: &init})
	if err := contract.CheckOp(it, assign); err != nil {
		t.Fatalf("initiative-only edit on a closed item: %v", err)
	}
	after, err := contract.Apply(it, assign, 3)
	if err != nil {
		t.Fatalf("apply assignment: %v", err)
	}
	if after.Initiative != "hits" || after.Status != contract.Resolved {
		t.Fatalf("assigned closed item = %+v", after)
	}

	// Anything beyond the assignment still bounces off terminal.
	prio := contract.High
	wantInvariant(t, contract.CheckOp(it, mkOp(t, contract.OpEdited, "1", "daan",
		contract.EditedPayload{Initiative: &init, Priority: &prio})), "terminal-status")
	wantInvariant(t, contract.CheckOp(it, mkOp(t, contract.OpEdited, "1", "daan",
		contract.EditedPayload{Priority: &prio})), "terminal-status")
}

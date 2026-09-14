package contract_test

import (
	"strings"
	"testing"

	"github.com/impire-io/hits/contract"
)

// TestValidReleaseSlug: dot-separated segments, each a well-formed slug —
// versions are the case dots exist for (decision 0017).
func TestValidReleaseSlug(t *testing.T) {
	for slug, want := range map[string]bool{
		"0.5":                          true,
		"0.5.0":                        true,
		"v0.5":                         true,
		"beta":                         true,
		"0":                            true,
		"0-5.rc-1":                     true,
		".5":                           false,
		"0.":                           false,
		"0..5":                         false,
		"A.5":                          false,
		"":                             false,
		strings.Repeat("0.", 40) + "0": false, // over one label's length
	} {
		if got := contract.ValidReleaseSlug(slug); got != want {
			t.Errorf("ValidReleaseSlug(%q) = %v, want %v", slug, got, want)
		}
	}
}

// TestParseReleaseEntity: initiative up to the first dot (initiative
// slugs are dot-free), release slug thereafter, dots literal.
func TestParseReleaseEntity(t *testing.T) {
	cases := []struct {
		entity     string
		initiative string
		slug       string
		ok         bool
	}{
		{"hits.0.5", "hits", "0.5", true},
		{"chronicle-hq.1.0", "chronicle-hq", "1.0", true},
		{"hits.beta", "hits", "beta", true},
		{"hits", "", "", false},
		{".0.5", "", "", false},
		{"hits.", "", "", false},
		{"team-42.0.5", "", "", false}, // team-42 is not a valid initiative slug
		{"hits.0..5", "", "", false},
	}
	for _, tc := range cases {
		initiative, slug, ok := contract.ParseReleaseEntity(tc.entity)
		if initiative != tc.initiative || slug != tc.slug || ok != tc.ok {
			t.Errorf("ParseReleaseEntity(%q) = %q,%q,%v; want %q,%q,%v",
				tc.entity, initiative, slug, ok, tc.initiative, tc.slug, tc.ok)
		}
	}
}

// TestReleaseLifecycle: register → shipped | retired, both terminal, the
// slug never reused (decision 0017).
func TestReleaseLifecycle(t *testing.T) {
	reg := mkOp(t, contract.OpRegistered, "hits.0.5", "daan", contract.RegisteredPayload{Name: "The 0.5 release"})
	if err := contract.CheckReleaseOp(nil, reg); err != nil {
		t.Fatalf("check register: %v", err)
	}
	r, err := contract.ApplyRelease(nil, reg, 1)
	if err != nil {
		t.Fatalf("apply register: %v", err)
	}
	if r.Initiative != "hits" || r.Slug != "0.5" || r.Name != "The 0.5 release" || r.Seq != 1 {
		t.Fatalf("release = %+v", r)
	}
	wantInvariant(t, contract.CheckReleaseOp(r, reg), "already-registered")
	wantInvariant(t, contract.CheckReleaseOp(nil, mkOp(t, contract.OpRegistered, "hits.0.5", "daan",
		contract.RegisteredPayload{})), "empty-name")
	wantInvariant(t, contract.CheckReleaseOp(nil, mkOp(t, contract.OpRegistered, "team-42.0.5", "daan",
		contract.RegisteredPayload{Name: "x"})), "invalid-release")

	ship := mkOp(t, contract.OpShipped, "hits.0.5", "daan", contract.ShippedPayload{
		Refs: []contract.ShipRef{{Tag: "v0.5.0", Note: "validated on the Synadia context"}},
		Note: "the straggler pass ran clean",
	})
	wantInvariant(t, contract.CheckReleaseOp(nil, ship), "unregistered-release")
	wantInvariant(t, contract.CheckReleaseOp(r, mkOp(t, contract.OpShipped, "hits.0.5", "daan",
		contract.ShippedPayload{})), "empty-refs")
	wantInvariant(t, contract.CheckReleaseOp(r, mkOp(t, contract.OpShipped, "hits.0.5", "daan",
		contract.ShippedPayload{Refs: []contract.ShipRef{{Note: "no ref at all"}}})), "empty-ref")
	if err := contract.CheckReleaseOp(r, ship); err != nil {
		t.Fatalf("check ship: %v", err)
	}
	shipped, err := contract.ApplyRelease(r, ship, 2)
	if err != nil {
		t.Fatalf("apply ship: %v", err)
	}
	if !shipped.Shipped || !shipped.Terminal() || len(shipped.ShipRefs) != 1 ||
		shipped.ShipRefs[0].Tag != "v0.5.0" || shipped.ShipNote != "the straggler pass ran clean" {
		t.Fatalf("shipped release = %+v", shipped)
	}
	wantInvariant(t, contract.CheckReleaseOp(shipped, ship), "already-shipped")
	wantInvariant(t, contract.CheckReleaseOp(shipped, mkOp(t, contract.OpRetired, "hits.0.5", "daan",
		contract.RetiredPayload{Reason: "no"})), "release-shipped")
	wantInvariant(t, contract.CheckReleaseOp(shipped, reg), "slug-shipped")
	if again, err := contract.ApplyRelease(shipped, ship, 2); err != nil || again != shipped {
		t.Fatalf("replayed ship = %+v, %v; want the snapshot unchanged", again, err)
	}

	// The retirement path, on a separate slug.
	typo, err := contract.ApplyRelease(nil, mkOp(t, contract.OpRegistered, "hits.0.6", "daan",
		contract.RegisteredPayload{Name: "A typo"}), 3)
	if err != nil {
		t.Fatalf("apply second register: %v", err)
	}
	ret := mkOp(t, contract.OpRetired, "hits.0.6", "daan", contract.RetiredPayload{Reason: "mistyped"})
	wantInvariant(t, contract.CheckReleaseOp(typo, mkOp(t, contract.OpRetired, "hits.0.6", "daan",
		contract.RetiredPayload{})), "empty-reason")
	if err := contract.CheckReleaseOp(typo, ret); err != nil {
		t.Fatalf("check retire: %v", err)
	}
	retired, err := contract.ApplyRelease(typo, ret, 4)
	if err != nil {
		t.Fatalf("apply retire: %v", err)
	}
	if !retired.Retired || !retired.Terminal() || retired.RetireReason != "mistyped" {
		t.Fatalf("retired release = %+v", retired)
	}
	wantInvariant(t, contract.CheckReleaseOp(retired, ret), "already-retired")
	wantInvariant(t, contract.CheckReleaseOp(retired, mkOp(t, contract.OpRegistered, "hits.0.6", "daan",
		contract.RegisteredPayload{Name: "again"})), "slug-retired")
	wantInvariant(t, contract.CheckReleaseOp(retired, mkOp(t, contract.OpShipped, "hits.0.6", "daan",
		contract.ShippedPayload{Refs: []contract.ShipRef{{Tag: "v0.6.0"}}})), "release-retired")
}

// TestItemTarget: one nullable property — set at creation or by edit,
// cleared by the empty string, frozen by terminal (decision 0017).
func TestItemTarget(t *testing.T) {
	it := step(t, nil, mkOp(t, contract.OpCreated, "hits-1", "daan", contract.CreatedPayload{
		Type: contract.Bug, Report: "aims at the release", Initiative: "hits", Target: "0.5",
	}), 1)
	if it.Target != "0.5" {
		t.Fatalf("created item target = %q, want 0.5", it.Target)
	}
	wantInvariant(t, contract.CheckOp(nil, mkOp(t, contract.OpCreated, "hits-2", "daan", contract.CreatedPayload{
		Type: contract.Bug, Report: "bad slug", Initiative: "hits", Target: ".5",
	})), "invalid-release")

	next := "0.6"
	it = step(t, it, mkOp(t, contract.OpEdited, "hits-1", "daan",
		contract.EditedPayload{Target: &next}), 2)
	if it.Target != "0.6" {
		t.Fatalf("edited item target = %q, want 0.6", it.Target)
	}
	bad := ".5"
	wantInvariant(t, contract.CheckOp(it, mkOp(t, contract.OpEdited, "hits-1", "daan",
		contract.EditedPayload{Target: &bad})), "invalid-release")

	empty := ""
	it = step(t, it, mkOp(t, contract.OpEdited, "hits-1", "daan",
		contract.EditedPayload{Target: &empty}), 3)
	if it.Target != "" {
		t.Fatalf("cleared item target = %q, want empty", it.Target)
	}

	// Terminal freezes the target: no explicit freeze op exists because
	// none is needed — and the legacy backfill exception stays
	// initiative-only.
	it = step(t, it, mkOp(t, contract.OpTransitioned, "hits-1", "daan", contract.TransitionedPayload{
		To: contract.Resolved, Closed: "2026-09-14",
	}), 4)
	again := "0.6"
	wantInvariant(t, contract.CheckOp(it, mkOp(t, contract.OpEdited, "hits-1", "daan",
		contract.EditedPayload{Target: &again})), "terminal-status")
	init := "hits"
	wantInvariant(t, contract.CheckOp(it, mkOp(t, contract.OpEdited, "hits-1", "daan",
		contract.EditedPayload{Initiative: &init, Target: &again})), "terminal-status")
}

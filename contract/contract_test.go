package contract_test

import (
	"errors"
	"testing"

	"github.com/impire-io/hits/contract"
)

func mkOp(t *testing.T, op contract.OpType, entity, actor string, payload any) contract.Op {
	t.Helper()
	o, err := contract.NewOp(op, entity, actor, payload)
	if err != nil {
		t.Fatalf("new %s op: %v", op, err)
	}
	return o
}

// step checks and applies one op, failing the test on either error.
func step(t *testing.T, current *contract.Item, op contract.Op, seq uint64) *contract.Item {
	t.Helper()
	if err := contract.CheckOp(current, op); err != nil {
		t.Fatalf("check %s: %v", op.Op, err)
	}
	next, err := contract.Apply(current, op, seq)
	if err != nil {
		t.Fatalf("apply %s: %v", op.Op, err)
	}
	return next
}

func wantInvariant(t *testing.T, err error, name string) {
	t.Helper()
	var ie *contract.InvariantError
	if !errors.As(err, &ie) {
		t.Fatalf("got %v, want invariant %q", err, name)
	}
	if ie.Name != name {
		t.Fatalf("got invariant %q (%s), want %q", ie.Name, ie.Message, name)
	}
}

func newBug(t *testing.T) *contract.Item {
	t.Helper()
	return step(t, nil, mkOp(t, contract.OpCreated, "1", "daan", contract.CreatedPayload{
		Type: contract.Bug, Report: "the projector lags",
	}), 1)
}

func TestCreateDefaults(t *testing.T) {
	it := newBug(t)
	if it.Status != contract.Open || it.Priority != contract.Normal {
		t.Errorf("new bug = %s/%s, want open/normal", it.Status, it.Priority)
	}
	if it.Reporter != "daan" || it.Seq != 1 {
		t.Errorf("reporter/seq = %s/%d, want daan/1", it.Reporter, it.Seq)
	}
}

func TestCreateInvariants(t *testing.T) {
	cases := []struct {
		name    string
		payload contract.CreatedPayload
		actor   string
		want    string
	}{
		{"bad actor", contract.CreatedPayload{Type: contract.Bug, Report: "x"}, "Daan", "invalid-actor"},
		{"bad type", contract.CreatedPayload{Type: "epic", Report: "x"}, "daan", "invalid-type"},
		{"no report", contract.CreatedPayload{Type: contract.Bug}, "daan", "empty-report"},
		{"task without location", contract.CreatedPayload{Type: contract.Task, Report: "x"}, "daan", "task-requires-location"},
		{"bad slug", contract.CreatedPayload{Type: contract.Task, Report: "x", LocatedIn: []string{"Bad Slug"}}, "daan", "invalid-slug"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wantInvariant(t, contract.CheckOp(nil, mkOp(t, contract.OpCreated, "1", tc.actor, tc.payload)), tc.want)
		})
	}
}

func TestLifecycle(t *testing.T) {
	it := newBug(t)
	it = step(t, it, mkOp(t, contract.OpTransitioned, "1", "daan", contract.TransitionedPayload{To: contract.Diagnosing}), 2)
	it = step(t, it, mkOp(t, contract.OpTransitioned, "1", "daan", contract.TransitionedPayload{To: contract.Located, LocatedIn: []string{"hits"}}), 3)
	it = step(t, it, mkOp(t, contract.OpTransitioned, "1", "daan", contract.TransitionedPayload{
		To: contract.Resolved, Closed: "2026-09-03", FixedBy: []contract.FixRef{{Commit: "abc123"}},
	}), 4)
	if it.Status != contract.Resolved || it.Closed != "2026-09-03" || len(it.FixedBy) != 1 {
		t.Fatalf("closed item = %+v", it)
	}

	// Terminal is terminal: no transition, edit, claim or block gets through.
	for op, payload := range map[contract.OpType]any{
		contract.OpTransitioned: contract.TransitionedPayload{To: contract.Open},
		contract.OpEdited:       contract.EditedPayload{},
		contract.OpClaimed:      contract.ClaimedPayload{},
		contract.OpBlocked:      contract.BlockedPayload{Interrupted: contract.Resolved},
	} {
		wantInvariant(t, contract.CheckOp(it, mkOp(t, op, "1", "daan", payload)), "terminal-status")
	}

	// The corpus is memory: notes and links stay legal on closed items.
	if err := contract.CheckOp(it, mkOp(t, contract.OpNoted, "1", "daan", contract.NotedPayload{Text: "seen again as 9"})); err != nil {
		t.Errorf("note on closed item: %v", err)
	}
	if err := contract.CheckOp(it, mkOp(t, contract.OpLinked, "1", "daan", contract.LinkedPayload{Type: contract.RelatesTo, To: "9"})); err != nil {
		t.Errorf("link on closed item: %v", err)
	}
}

func TestTransitionTable(t *testing.T) {
	it := newBug(t)
	wantInvariant(t, contract.CheckOp(it, mkOp(t, contract.OpTransitioned, "1", "daan",
		contract.TransitionedPayload{To: contract.Blocked})), "illegal-transition")
	wantInvariant(t, contract.CheckOp(it, mkOp(t, contract.OpTransitioned, "1", "daan",
		contract.TransitionedPayload{To: contract.Located})), "located-requires-location")
	wantInvariant(t, contract.CheckOp(it, mkOp(t, contract.OpTransitioned, "1", "daan",
		contract.TransitionedPayload{To: contract.Resolved})), "close-requires-date")
	wantInvariant(t, contract.CheckOp(it, mkOp(t, contract.OpTransitioned, "1", "daan",
		contract.TransitionedPayload{To: contract.Diagnosing, FixedBy: []contract.FixRef{{Commit: "x"}}})), "refs-only-on-close")

	task := step(t, nil, mkOp(t, contract.OpCreated, "2", "daan", contract.CreatedPayload{
		Type: contract.Task, Report: "sync the docs", LocatedIn: []string{"hits"},
	}), 1)
	wantInvariant(t, contract.CheckOp(task, mkOp(t, contract.OpTransitioned, "2", "daan",
		contract.TransitionedPayload{To: contract.Diagnosing})), "illegal-transition")
	if err := contract.CheckOp(task, mkOp(t, contract.OpTransitioned, "2", "daan",
		contract.TransitionedPayload{To: contract.Resolved, Closed: "2026-09-03"})); err != nil {
		t.Errorf("task open→resolved: %v", err)
	}
}

func TestBlockRestoresInterruptedStatus(t *testing.T) {
	it := newBug(t)
	it = step(t, it, mkOp(t, contract.OpTransitioned, "1", "daan", contract.TransitionedPayload{To: contract.Diagnosing}), 2)
	it = step(t, it, mkOp(t, contract.OpBlocked, "1", "daan", contract.BlockedPayload{BlockedBy: "#42", Interrupted: it.Status}), 3)
	if it.Status != contract.Blocked || it.BlockedBy != "#42" {
		t.Fatalf("blocked item = %s on %q", it.Status, it.BlockedBy)
	}
	wantInvariant(t, contract.CheckOp(it, mkOp(t, contract.OpTransitioned, "1", "daan",
		contract.TransitionedPayload{To: contract.Located, LocatedIn: []string{"hits"}})), "blocked")
	wantInvariant(t, contract.CheckOp(it, mkOp(t, contract.OpBlocked, "1", "daan",
		contract.BlockedPayload{Interrupted: contract.Blocked})), "already-blocked")

	it = step(t, it, mkOp(t, contract.OpUnblocked, "1", "daan", nil), 4)
	if it.Status != contract.Diagnosing || it.BlockedBy != "" || it.Interrupted != "" {
		t.Fatalf("unblocked item = %s (blocked-by %q, interrupted %q), want diagnosing restored", it.Status, it.BlockedBy, it.Interrupted)
	}
	wantInvariant(t, contract.CheckOp(it, mkOp(t, contract.OpUnblocked, "1", "daan", nil)), "not-blocked")
}

func TestClaimReleaseSteal(t *testing.T) {
	it := newBug(t)
	wantInvariant(t, contract.CheckOp(it, mkOp(t, contract.OpReleased, "1", "daan", nil)), "not-claimed")
	it = step(t, it, mkOp(t, contract.OpClaimed, "1", "daan", contract.ClaimedPayload{}), 2)
	wantInvariant(t, contract.CheckOp(it, mkOp(t, contract.OpClaimed, "1", "claude", contract.ClaimedPayload{})), "already-claimed")
	it = step(t, it, mkOp(t, contract.OpClaimed, "1", "claude", contract.ClaimedPayload{Steal: true, StolenFrom: "daan"}), 3)
	if it.Claim == nil || it.Claim.By != "claude" || it.Claim.StolenFrom != "daan" {
		t.Fatalf("stolen claim = %+v", it.Claim)
	}
	it = step(t, it, mkOp(t, contract.OpReleased, "1", "claude", nil), 4)
	if it.Claim != nil {
		t.Fatalf("released claim = %+v", it.Claim)
	}
}

func TestLinks(t *testing.T) {
	it := newBug(t)
	link := contract.LinkedPayload{Type: contract.RelatesTo, To: "7"}
	wantInvariant(t, contract.CheckOp(it, mkOp(t, contract.OpLinked, "1", "daan",
		contract.LinkedPayload{Type: "blocks", To: "7"})), "invalid-link-type")
	wantInvariant(t, contract.CheckOp(it, mkOp(t, contract.OpLinked, "1", "daan",
		contract.LinkedPayload{Type: contract.RelatesTo, To: "1"})), "self-link")
	wantInvariant(t, contract.CheckOp(it, mkOp(t, contract.OpUnlinked, "1", "daan", link)), "link-not-found")
	it = step(t, it, mkOp(t, contract.OpLinked, "1", "daan", link), 2)
	wantInvariant(t, contract.CheckOp(it, mkOp(t, contract.OpLinked, "1", "daan", link)), "link-exists")
	it = step(t, it, mkOp(t, contract.OpUnlinked, "1", "daan", link), 3)
	if len(it.Links) != 0 {
		t.Fatalf("links after unlink = %+v", it.Links)
	}
}

func TestTombstoneEndsEverything(t *testing.T) {
	it := newBug(t)
	wantInvariant(t, contract.CheckOp(it, mkOp(t, contract.OpTombstoned, "1", "daan",
		contract.TombstonedPayload{})), "empty-reason")
	it = step(t, it, mkOp(t, contract.OpTombstoned, "1", "daan", contract.TombstonedPayload{Reason: "filed twice"}), 2)
	wantInvariant(t, contract.CheckOp(it, mkOp(t, contract.OpNoted, "1", "daan",
		contract.NotedPayload{Text: "x"})), "tombstoned")
}

func TestApplyIsIdempotent(t *testing.T) {
	it := newBug(t)
	note := mkOp(t, contract.OpNoted, "1", "daan", contract.NotedPayload{Text: "first hypothesis"})
	it = step(t, it, note, 2)
	again, err := contract.Apply(it, note, 2)
	if err != nil {
		t.Fatalf("re-apply: %v", err)
	}
	if len(again.Notes) != 1 {
		t.Fatalf("re-applied op duplicated the note: %+v", again.Notes)
	}
	if again != it {
		t.Error("skipped apply should return the snapshot unchanged")
	}
}

func TestApplyDoesNotMutateCurrent(t *testing.T) {
	it := newBug(t)
	it = step(t, it, mkOp(t, contract.OpLinked, "1", "daan", contract.LinkedPayload{Type: contract.RelatesTo, To: "7"}), 2)
	before := len(it.Links)
	_ = step(t, it, mkOp(t, contract.OpLinked, "1", "daan", contract.LinkedPayload{Type: contract.Duplicates, To: "9"}), 3)
	if len(it.Links) != before {
		t.Fatal("Apply mutated the input snapshot")
	}
}

func TestProjects(t *testing.T) {
	reg := mkOp(t, contract.OpRegistered, "hits", "daan", contract.RegisteredPayload{Name: "HITS product repo"})
	if err := contract.CheckProjectOp(nil, reg); err != nil {
		t.Fatalf("check register: %v", err)
	}
	p, err := contract.ApplyProject(nil, reg, 1)
	if err != nil {
		t.Fatalf("apply register: %v", err)
	}
	if p.Slug != "hits" || p.Name != "HITS product repo" || p.Seq != 1 {
		t.Fatalf("project = %+v", p)
	}
	wantInvariant(t, contract.CheckProjectOp(p, reg), "already-registered")
	wantInvariant(t, contract.CheckProjectOp(nil, mkOp(t, contract.OpRegistered, "Bad Slug", "daan",
		contract.RegisteredPayload{Name: "x"})), "invalid-slug")
	wantInvariant(t, contract.CheckProjectOp(nil, mkOp(t, contract.OpRegistered, "hits", "daan",
		contract.RegisteredPayload{})), "empty-name")
}

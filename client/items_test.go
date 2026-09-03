package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/contract"
	"github.com/impire-io/hits/internal/natstest"
	"github.com/impire-io/hits/internal/node"
)

// harness is one running item store: embedded JetStream server, the node
// service on it, and a client connection.
type harness struct {
	url     string
	svcConn *nats.Conn
	c       *client.Client
}

func startStore(t *testing.T) *harness {
	t.Helper()
	url := natstest.StartJetStream(t)

	svcConn, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect service side: %v", err)
	}
	t.Cleanup(svcConn.Close)
	svc, err := node.Start(context.Background(), svcConn, node.Config{})
	if err != nil {
		t.Fatalf("start node: %v", err)
	}
	t.Cleanup(func() { _ = svc.Stop() })

	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect client side: %v", err)
	}
	t.Cleanup(nc.Close)
	return &harness{url: url, svcConn: svcConn, c: client.New(nc)}
}

func testCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// mustProject registers the named project, failing the test on error.
func (h *harness) mustProject(ctx context.Context, t *testing.T, slug string) {
	t.Helper()
	if _, err := h.c.RegisterProject(ctx, client.RegisterProjectRequest{
		Actor: "daan", Slug: slug, Name: slug + " repo",
	}); err != nil {
		t.Fatalf("register project %s: %v", slug, err)
	}
}

func wantAPIError(t *testing.T, err error, code string) {
	t.Helper()
	var ae *client.APIError
	if !errors.As(err, &ae) {
		t.Fatalf("got %v, want APIError %q", err, code)
	}
	if ae.Code != code {
		t.Fatalf("got APIError %q (%s), want %q", ae.Code, ae.Message, code)
	}
}

// TestItemRoundTrip drives one item through every endpoint over the wire.
func TestItemRoundTrip(t *testing.T) {
	h := startStore(t)
	ctx := testCtx(t)
	h.mustProject(ctx, t, "hits")

	it, err := h.c.CreateItem(ctx, client.CreateItemRequest{
		Actor: "daan", Type: contract.Bug, Report: "the projector lags behind the log",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if it.ID != "1" || it.Status != contract.Open || it.Priority != contract.Normal || it.Reporter != "daan" {
		t.Fatalf("created item = %+v", it)
	}

	if _, err := h.c.NoteItem(ctx, client.NoteItemRequest{Actor: "claude", ID: it.ID, Text: "reproduced on main"}); err != nil {
		t.Fatalf("note: %v", err)
	}
	if _, err := h.c.TransitionItem(ctx, client.TransitionItemRequest{Actor: "claude", ID: it.ID, To: contract.Diagnosing}); err != nil {
		t.Fatalf("diagnosing: %v", err)
	}

	// Block remembers diagnosing; unblock restores it (FR-21).
	blocked, err := h.c.BlockItem(ctx, client.BlockItemRequest{Actor: "claude", ID: it.ID, BlockedBy: "waiting on #7"})
	if err != nil {
		t.Fatalf("block: %v", err)
	}
	if blocked.Status != contract.Blocked || blocked.BlockedBy != "waiting on #7" || blocked.Interrupted != contract.Diagnosing {
		t.Fatalf("blocked item = %+v", blocked)
	}
	unblocked, err := h.c.UnblockItem(ctx, client.UnblockItemRequest{Actor: "claude", ID: it.ID})
	if err != nil {
		t.Fatalf("unblock: %v", err)
	}
	if unblocked.Status != contract.Diagnosing || unblocked.BlockedBy != "" || unblocked.Interrupted != "" {
		t.Fatalf("unblocked item = %+v, want diagnosing restored", unblocked)
	}

	// Claim, steal (attributed), release.
	if _, err := h.c.ClaimItem(ctx, client.ClaimItemRequest{Actor: "daan", ID: it.ID}); err != nil {
		t.Fatalf("claim: %v", err)
	}
	stolen, err := h.c.ClaimItem(ctx, client.ClaimItemRequest{Actor: "claude", ID: it.ID, Steal: true})
	if err != nil {
		t.Fatalf("steal: %v", err)
	}
	if stolen.Claim == nil || stolen.Claim.By != "claude" || stolen.Claim.StolenFrom != "daan" {
		t.Fatalf("stolen claim = %+v", stolen.Claim)
	}
	if _, err := h.c.ReleaseItem(ctx, client.ReleaseItemRequest{Actor: "claude", ID: it.ID}); err != nil {
		t.Fatalf("release: %v", err)
	}

	// A second item to link against, then unlink.
	other, err := h.c.CreateItem(ctx, client.CreateItemRequest{
		Actor: "daan", Type: contract.Improvement, Report: "projector lag metrics would help",
	})
	if err != nil {
		t.Fatalf("create other: %v", err)
	}
	linked, err := h.c.LinkItem(ctx, client.LinkItemRequest{Actor: "daan", ID: it.ID, Type: contract.RelatesTo, To: other.ID})
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if len(linked.Links) != 1 || linked.Links[0].To != other.ID {
		t.Fatalf("links = %+v", linked.Links)
	}
	if _, err := h.c.UnlinkItem(ctx, client.LinkItemRequest{Actor: "daan", ID: it.ID, Type: contract.RelatesTo, To: other.ID}); err != nil {
		t.Fatalf("unlink: %v", err)
	}

	// Locate, edit priority, close with refs.
	if _, err := h.c.TransitionItem(ctx, client.TransitionItemRequest{
		Actor: "claude", ID: it.ID, To: contract.Located, LocatedIn: []string{"hits"},
	}); err != nil {
		t.Fatalf("located: %v", err)
	}
	prio := contract.High
	if _, err := h.c.EditItem(ctx, client.EditItemRequest{Actor: "daan", ID: it.ID, Priority: &prio}); err != nil {
		t.Fatalf("edit: %v", err)
	}
	closed, err := h.c.TransitionItem(ctx, client.TransitionItemRequest{
		Actor: "claude", ID: it.ID, To: contract.Resolved,
		FixedBy: []contract.FixRef{{Commit: "abc1234"}},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if closed.Status != contract.Resolved || closed.Closed == "" || len(closed.FixedBy) != 1 {
		t.Fatalf("closed item = %+v", closed)
	}

	// The snapshot survives the wire intact.
	got, err := h.c.GetItem(ctx, it.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != contract.Resolved || got.Priority != contract.High || len(got.Notes) != 1 {
		t.Fatalf("final item = %+v", got)
	}

	// Tombstone voids the other item; the trail then refuses everything.
	if _, err := h.c.TombstoneItem(ctx, client.TombstoneItemRequest{Actor: "daan", ID: other.ID, Reason: "filed as a test"}); err != nil {
		t.Fatalf("tombstone: %v", err)
	}
	_, err = h.c.NoteItem(ctx, client.NoteItemRequest{Actor: "daan", ID: other.ID, Text: "x"})
	wantAPIError(t, err, "tombstoned")
}

// TestConcurrentClaimsAdmitOneWinner is FR-11 on the wire: racing claims
// serialize on the per-subject CAS and exactly one wins.
func TestConcurrentClaimsAdmitOneWinner(t *testing.T) {
	h := startStore(t)
	ctx := testCtx(t)

	it, err := h.c.CreateItem(ctx, client.CreateItemRequest{
		Actor: "daan", Type: contract.Bug, Report: "claims must serialize",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	actors := []string{"alfa", "bravo", "carol", "delta", "echo", "frank", "grace", "henry"}
	var wg sync.WaitGroup
	results := make([]error, len(actors))
	for i, actor := range actors {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := h.c.ClaimItem(ctx, client.ClaimItemRequest{Actor: actor, ID: it.ID})
			results[i] = err
		}()
	}
	wg.Wait()

	wins := 0
	for _, err := range results {
		if err == nil {
			wins++
			continue
		}
		wantAPIError(t, err, "already-claimed")
	}
	if wins != 1 {
		t.Fatalf("%d claims won, want exactly 1", wins)
	}
	got, err := h.c.GetItem(ctx, it.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Claim == nil {
		t.Fatal("no claim survived")
	}
}

// TestInvariantRejectionsByName is FR-20 on the wire: rejections carry the
// invariant name as the machine-legible error code.
func TestInvariantRejectionsByName(t *testing.T) {
	h := startStore(t)
	ctx := testCtx(t)
	h.mustProject(ctx, t, "hits")

	_, err := h.c.CreateItem(ctx, client.CreateItemRequest{Actor: "daan", Type: contract.Task, Report: "no home"})
	wantAPIError(t, err, "task-requires-location")

	_, err = h.c.CreateItem(ctx, client.CreateItemRequest{
		Actor: "daan", Type: contract.Task, Report: "ghost repo", LocatedIn: []string{"ghost"},
	})
	wantAPIError(t, err, "unregistered-project")

	_, err = h.c.GetItem(ctx, "999")
	wantAPIError(t, err, "not-found")

	it, err := h.c.CreateItem(ctx, client.CreateItemRequest{
		Actor: "daan", Type: contract.Task, Report: "close me", LocatedIn: []string{"hits"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := h.c.TransitionItem(ctx, client.TransitionItemRequest{Actor: "daan", ID: it.ID, To: contract.Resolved}); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	prio := contract.Low
	_, err = h.c.EditItem(ctx, client.EditItemRequest{Actor: "daan", ID: it.ID, Priority: &prio})
	wantAPIError(t, err, "terminal-status")
}

// TestDuplicateProjectRegistrationFails is FR-12's slug uniqueness: the
// expected-sequence-zero publish rejects the second registration.
func TestDuplicateProjectRegistrationFails(t *testing.T) {
	h := startStore(t)
	ctx := testCtx(t)
	h.mustProject(ctx, t, "hits")

	_, err := h.c.RegisterProject(ctx, client.RegisterProjectRequest{Actor: "claude", Slug: "hits", Name: "again"})
	wantAPIError(t, err, "already-registered")

	ps, err := h.c.ListProjects(ctx)
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	if len(ps) != 1 || ps[0].Slug != "hits" {
		t.Fatalf("projects = %+v", ps)
	}
}

// TestReplayReproducesProjections is FR-31: delete the projection buckets,
// restart the node (which replays the ops-log), and the projections match
// what the live folds built.
func TestReplayReproducesProjections(t *testing.T) {
	h := startStore(t)
	ctx := testCtx(t)
	h.mustProject(ctx, t, "hits")

	it, err := h.c.CreateItem(ctx, client.CreateItemRequest{
		Actor: "daan", Type: contract.Bug, Report: "replay must reproduce state",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := h.c.NoteItem(ctx, client.NoteItemRequest{Actor: "claude", ID: it.ID, Text: "first trail entry"}); err != nil {
		t.Fatalf("note: %v", err)
	}
	if _, err := h.c.ClaimItem(ctx, client.ClaimItemRequest{Actor: "claude", ID: it.ID}); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := h.c.BlockItem(ctx, client.BlockItemRequest{Actor: "claude", ID: it.ID, BlockedBy: "an external party"}); err != nil {
		t.Fatalf("block: %v", err)
	}

	before, err := h.c.GetItem(ctx, it.ID)
	if err != nil {
		t.Fatalf("get before: %v", err)
	}

	// Delete the projections out from under the store, then restart the
	// node: Start replays the ops-log into fresh buckets.
	js, err := jetstream.New(h.svcConn)
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	for _, bucket := range []string{"hits-items", "hits-projects"} {
		if err := js.DeleteKeyValue(ctx, bucket); err != nil {
			t.Fatalf("delete bucket %s: %v", bucket, err)
		}
	}
	svc, err := node.Start(ctx, h.svcConn, node.Config{})
	if err != nil {
		t.Fatalf("restart node: %v", err)
	}
	t.Cleanup(func() { _ = svc.Stop() })

	after, err := h.c.GetItem(ctx, it.ID)
	if err != nil {
		t.Fatalf("get after replay: %v", err)
	}
	if !reflect.DeepEqual(before, after) {
		b, _ := json.Marshal(before)
		a, _ := json.Marshal(after)
		t.Fatalf("replayed snapshot differs:\n before %s\n after  %s", b, a)
	}
	ps, err := h.c.ListProjects(ctx)
	if err != nil {
		t.Fatalf("list projects after replay: %v", err)
	}
	if len(ps) != 1 || ps[0].Slug != "hits" || ps[0].Name != "hits repo" {
		t.Fatalf("replayed projects = %+v", ps)
	}
}

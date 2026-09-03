package client_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/contract"
	"github.com/impire-io/hits/internal/index/search"
)

func startSearch(t *testing.T, h *harness) *search.Service {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	svc, err := search.Start(ctx, h.svcConn)
	if err != nil {
		t.Fatalf("start search: %v", err)
	}
	t.Cleanup(svc.Stop)
	return svc
}

// waitFor polls cond until it holds — the index tails the ops-log
// asynchronously, so visibility after a write is eventual by design.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func total(ctx context.Context, t *testing.T, h *harness, r client.SearchRequest) uint64 {
	t.Helper()
	reply, err := h.c.SearchItems(ctx, r)
	if err != nil {
		t.Fatalf("search %+v: %v", r, err)
	}
	return reply.Total
}

// TestSearchLiveTail: the service is off the wire until started, then items
// and notes written through hits.api become findable as the tail catches
// them, and a tombstone removes the item from every result.
func TestSearchLiveTail(t *testing.T) {
	h := startStore(t)
	ctx := testCtx(t)

	_, err := h.c.SearchItems(ctx, client.SearchRequest{Query: "anything"})
	if !errors.Is(err, nats.ErrNoResponders) {
		t.Fatalf("search before the service exists: got %v, want no responders", err)
	}

	startSearch(t, h)

	it, err := h.c.CreateItem(ctx, client.CreateItemRequest{
		Actor: "daan", Type: contract.Bug, Report: "the projector lags behind the ops log",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	waitFor(t, "report text to be indexed", func() bool {
		return total(ctx, t, h, client.SearchRequest{Query: "projector"}) == 1
	})

	if _, err := h.c.NoteItem(ctx, client.NoteItemRequest{Actor: "claude", ID: it.ID, Text: "the flux capacitor was misaligned"}); err != nil {
		t.Fatalf("note: %v", err)
	}
	waitFor(t, "note text to be indexed", func() bool {
		return total(ctx, t, h, client.SearchRequest{Query: "capacitor"}) == 1
	})

	if got := total(ctx, t, h, client.SearchRequest{Query: "zebra"}); got != 0 {
		t.Fatalf("query zebra matched %d items, want 0", got)
	}

	if _, err := h.c.TombstoneItem(ctx, client.TombstoneItemRequest{Actor: "daan", ID: it.ID, Reason: "filed as a test"}); err != nil {
		t.Fatalf("tombstone: %v", err)
	}
	waitFor(t, "tombstoned item to leave the index", func() bool {
		return total(ctx, t, h, client.SearchRequest{Query: "projector"}) == 0
	})
}

// TestSearchRebuildFiltersPaging: a service started after the corpus exists
// rebuilds it by replay before going on the wire — so everything below
// asserts immediately, no polling — and filters and paging behave.
func TestSearchRebuildFiltersPaging(t *testing.T) {
	h := startStore(t)
	ctx := testCtx(t)
	h.mustProject(ctx, t, "hits")

	if _, err := h.c.CreateItem(ctx, client.CreateItemRequest{
		Actor: "daan", Type: contract.Bug, Report: "alpha signal lost on reconnect",
	}); err != nil {
		t.Fatalf("create bug: %v", err)
	}
	task, err := h.c.CreateItem(ctx, client.CreateItemRequest{
		Actor: "daan", Type: contract.Task, Report: "sync the beta docs", LocatedIn: []string{"hits"},
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if _, err := h.c.TransitionItem(ctx, client.TransitionItemRequest{Actor: "daan", ID: task.ID, To: contract.Resolved}); err != nil {
		t.Fatalf("resolve task: %v", err)
	}

	startSearch(t, h) // blocks until caught up: the corpus is queryable now

	if got := total(ctx, t, h, client.SearchRequest{Query: "alpha"}); got != 1 {
		t.Fatalf("alpha matched %d, want 1", got)
	}
	// The kept-forever corpus is the point: the resolved task is findable.
	if got := total(ctx, t, h, client.SearchRequest{Query: "beta"}); got != 1 {
		t.Fatalf("resolved item matched %d, want 1", got)
	}
	if got := total(ctx, t, h, client.SearchRequest{Type: contract.Task}); got != 1 {
		t.Fatalf("type filter matched %d, want 1", got)
	}
	if got := total(ctx, t, h, client.SearchRequest{Status: contract.Resolved}); got != 1 {
		t.Fatalf("status filter matched %d, want 1", got)
	}
	if got := total(ctx, t, h, client.SearchRequest{Type: contract.Bug, Status: contract.Resolved}); got != 0 {
		t.Fatalf("conjoined filters matched %d, want 0", got)
	}

	page, err := h.c.SearchItems(ctx, client.SearchRequest{Limit: 1})
	if err != nil {
		t.Fatalf("paged search: %v", err)
	}
	if len(page.Hits) != 1 || page.Total != 2 {
		t.Fatalf("page = %d hits of %d total, want 1 of 2", len(page.Hits), page.Total)
	}
	rest, err := h.c.SearchItems(ctx, client.SearchRequest{Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("offset search: %v", err)
	}
	if len(rest.Hits) != 1 || rest.Hits[0].ID == page.Hits[0].ID {
		t.Fatalf("offset page = %+v, want the other item than %+v", rest.Hits, page.Hits)
	}
}

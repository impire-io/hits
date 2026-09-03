package client_test

import (
	"context"
	"testing"
	"time"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/contract"
	"github.com/impire-io/hits/internal/index/graph"
)

func startGraph(t *testing.T, h *harness) *graph.Service {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	svc, err := graph.Start(ctx, h.svcConn)
	if err != nil {
		t.Fatalf("start graph: %v", err)
	}
	t.Cleanup(svc.Stop)
	return svc
}

// hasEdge reports whether the node's neighbors include an edge of the given
// type to the given target.
func hasEdge(ctx context.Context, t *testing.T, h *harness, from client.NeighborsRequest, typ string, to client.NodeRef) bool {
	t.Helper()
	reply, err := h.c.GraphNeighbors(ctx, from)
	if err != nil {
		t.Fatalf("neighbors %+v: %v", from, err)
	}
	for _, e := range reply.Edges {
		if e.Type == typ && e.To.Kind == to.Kind && e.To.ID == to.ID {
			return true
		}
	}
	return false
}

// TestGraphEdgesFollowOps drives the derived-edge lifetimes over the live
// tail: reported-by is forever, claimed-by follows the claim, blocked-by
// derives only from an item-ID blocker and drops on unblock, asserted links
// come and go with link/unlink.
func TestGraphEdgesFollowOps(t *testing.T) {
	h := startStore(t)
	ctx := testCtx(t)
	h.mustProject(ctx, t, "hits")
	startGraph(t, h)

	a, err := h.c.CreateItem(ctx, client.CreateItemRequest{
		Actor: "daan", Type: contract.Bug, Report: "graph edges must follow ops",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	fromA := client.NeighborsRequest{Kind: client.NodeItem, ID: a.ID, Direction: "out"}
	daan := client.NodeRef{Kind: client.NodeActor, ID: "daan"}
	claude := client.NodeRef{Kind: client.NodeActor, ID: "claude"}

	waitFor(t, "reported-by edge", func() bool {
		return hasEdge(ctx, t, h, fromA, client.EdgeReportedBy, daan)
	})

	if _, err := h.c.ClaimItem(ctx, client.ClaimItemRequest{Actor: "claude", ID: a.ID}); err != nil {
		t.Fatalf("claim: %v", err)
	}
	waitFor(t, "claimed-by edge", func() bool {
		return hasEdge(ctx, t, h, fromA, client.EdgeClaimedBy, claude)
	})
	if _, err := h.c.ReleaseItem(ctx, client.ReleaseItemRequest{Actor: "claude", ID: a.ID}); err != nil {
		t.Fatalf("release: %v", err)
	}
	waitFor(t, "claimed-by edge to drop on release", func() bool {
		return !hasEdge(ctx, t, h, fromA, client.EdgeClaimedBy, claude)
	})

	// A blocker that is an item ID derives an edge — dangling refs are held
	// by design, so the target item need not exist.
	if _, err := h.c.BlockItem(ctx, client.BlockItemRequest{Actor: "daan", ID: a.ID, BlockedBy: "7"}); err != nil {
		t.Fatalf("block: %v", err)
	}
	waitFor(t, "blocked-by edge", func() bool {
		return hasEdge(ctx, t, h, fromA, client.EdgeBlockedBy, client.NodeRef{Kind: client.NodeItem, ID: "7"})
	})
	if _, err := h.c.UnblockItem(ctx, client.UnblockItemRequest{Actor: "daan", ID: a.ID}); err != nil {
		t.Fatalf("unblock: %v", err)
	}
	waitFor(t, "blocked-by edge to drop on unblock", func() bool {
		return !hasEdge(ctx, t, h, fromA, client.EdgeBlockedBy, client.NodeRef{Kind: client.NodeItem, ID: "7"})
	})

	// A prose blocker derives nothing.
	if _, err := h.c.BlockItem(ctx, client.BlockItemRequest{Actor: "daan", ID: a.ID, BlockedBy: "waiting on the vendor"}); err != nil {
		t.Fatalf("block with prose: %v", err)
	}
	waitFor(t, "the block to fold", func() bool {
		got, err := h.c.GetItem(ctx, a.ID)
		return err == nil && got.Status == contract.Blocked
	})
	if reply, err := h.c.GraphNeighbors(ctx, client.NeighborsRequest{
		Kind: client.NodeItem, ID: a.ID, Types: []string{client.EdgeBlockedBy},
	}); err != nil || len(reply.Edges) != 0 {
		t.Fatalf("prose blocker derived edges %+v (err %v), want none", reply.Edges, err)
	}
	if _, err := h.c.UnblockItem(ctx, client.UnblockItemRequest{Actor: "daan", ID: a.ID}); err != nil {
		t.Fatalf("unblock: %v", err)
	}

	b, err := h.c.CreateItem(ctx, client.CreateItemRequest{
		Actor: "daan", Type: contract.Improvement, Report: "a second node",
	})
	if err != nil {
		t.Fatalf("create b: %v", err)
	}
	if _, err := h.c.LinkItem(ctx, client.LinkItemRequest{Actor: "daan", ID: a.ID, Type: contract.RelatesTo, To: b.ID}); err != nil {
		t.Fatalf("link: %v", err)
	}
	bRef := client.NodeRef{Kind: client.NodeItem, ID: b.ID}
	waitFor(t, "asserted link edge", func() bool {
		return hasEdge(ctx, t, h, fromA, client.EdgeRelatesTo, bRef)
	})
	// The same edge is visible from the target's in-direction.
	inB := client.NeighborsRequest{Kind: client.NodeItem, ID: b.ID, Direction: "in"}
	if !hasEdge(ctx, t, h, inB, client.EdgeRelatesTo, bRef) {
		reply, _ := h.c.GraphNeighbors(ctx, inB)
		t.Fatalf("in-direction neighbors of b = %+v, want relates-to into b", reply.Edges)
	}
	if _, err := h.c.UnlinkItem(ctx, client.LinkItemRequest{Actor: "daan", ID: a.ID, Type: contract.RelatesTo, To: b.ID}); err != nil {
		t.Fatalf("unlink: %v", err)
	}
	waitFor(t, "asserted link edge to drop on unlink", func() bool {
		return !hasEdge(ctx, t, h, fromA, client.EdgeRelatesTo, bRef)
	})
}

// TestGraphRebuildWalkTombstone builds a corpus first, starts the graph
// after — rebuild-on-boot means everything asserts immediately — then
// exercises the project-neighbors query, the bounded walk, and tombstone
// removal.
func TestGraphRebuildWalkTombstone(t *testing.T) {
	h := startStore(t)
	ctx := testCtx(t)
	h.mustProject(ctx, t, "hits")

	a, err := h.c.CreateItem(ctx, client.CreateItemRequest{
		Actor: "daan", Type: contract.Bug, Report: "start of the chain",
	})
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	b, err := h.c.CreateItem(ctx, client.CreateItemRequest{
		Actor: "daan", Type: contract.Task, Report: "the located link", LocatedIn: []string{"hits"},
	})
	if err != nil {
		t.Fatalf("create b: %v", err)
	}
	if _, err := h.c.LinkItem(ctx, client.LinkItemRequest{Actor: "daan", ID: a.ID, Type: contract.RelatesTo, To: b.ID}); err != nil {
		t.Fatalf("link: %v", err)
	}

	startGraph(t, h) // caught up before going on the wire

	// The motivating query: everything located in one project, with the
	// project node carrying its registered name.
	reply, err := h.c.GraphNeighbors(ctx, client.NeighborsRequest{
		Kind: client.NodeProject, ID: "hits", Direction: "in", Types: []string{client.EdgeLocatedIn},
	})
	if err != nil {
		t.Fatalf("project neighbors: %v", err)
	}
	if len(reply.Edges) != 1 || reply.Edges[0].From.ID != b.ID || reply.Edges[0].To.Name != "hits repo" {
		t.Fatalf("project neighbors = %+v, want located-in from item %s with project name", reply.Edges, b.ID)
	}

	// Walk from a: depth 1 stops at b and daan; depth 3 reaches the project
	// through b.
	shallow, err := h.c.GraphWalk(ctx, client.WalkRequest{Kind: client.NodeItem, ID: a.ID, Depth: 1})
	if err != nil {
		t.Fatalf("shallow walk: %v", err)
	}
	if containsNode(shallow.Nodes, client.NodeProject, "hits") {
		t.Fatalf("depth-1 walk reached the project: %+v", shallow.Nodes)
	}
	deep, err := h.c.GraphWalk(ctx, client.WalkRequest{Kind: client.NodeItem, ID: a.ID, Depth: 3})
	if err != nil {
		t.Fatalf("deep walk: %v", err)
	}
	if !containsNode(deep.Nodes, client.NodeProject, "hits") || !containsNode(deep.Nodes, client.NodeActor, "daan") {
		t.Fatalf("depth-3 walk nodes = %+v, want project:hits and actor:daan", deep.Nodes)
	}

	// Filtered walk only traverses the named edge types.
	filtered, err := h.c.GraphWalk(ctx, client.WalkRequest{
		Kind: client.NodeItem, ID: a.ID, Depth: 3, Types: []string{client.EdgeRelatesTo},
	})
	if err != nil {
		t.Fatalf("filtered walk: %v", err)
	}
	if !containsNode(filtered.Nodes, client.NodeItem, b.ID) || containsNode(filtered.Nodes, client.NodeActor, "daan") {
		t.Fatalf("filtered walk nodes = %+v, want only the relates-to chain", filtered.Nodes)
	}

	// Tombstoning b removes its node, its edges, and every edge into it.
	if _, err := h.c.TombstoneItem(ctx, client.TombstoneItemRequest{Actor: "daan", ID: b.ID, Reason: "filed as a test"}); err != nil {
		t.Fatalf("tombstone: %v", err)
	}
	waitFor(t, "tombstoned node to leave the graph", func() bool {
		return !hasEdge(ctx, t, h,
			client.NeighborsRequest{Kind: client.NodeItem, ID: a.ID}, client.EdgeRelatesTo,
			client.NodeRef{Kind: client.NodeItem, ID: b.ID})
	})
	gone, err := h.c.GraphNeighbors(ctx, client.NeighborsRequest{Kind: client.NodeItem, ID: b.ID})
	if err != nil {
		t.Fatalf("neighbors of tombstoned: %v", err)
	}
	if len(gone.Edges) != 0 {
		t.Fatalf("tombstoned item still has edges: %+v", gone.Edges)
	}
}

func containsNode(nodes []client.NodeRef, kind client.NodeKind, id string) bool {
	for _, n := range nodes {
		if n.Kind == kind && n.ID == id {
			return true
		}
	}
	return false
}

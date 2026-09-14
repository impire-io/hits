package client_test

import (
	"testing"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/contract"
	"github.com/impire-io/hits/internal/node"
)

// TestReleasesEndToEnd drives the third vocabulary over the wire
// (decision 0017): per-initiative registration, targets set and cleared,
// the straggler gate on shipping, terminal slugs refusing new targets,
// guardless retirement leaving strays, and the registry surviving
// replay.
func TestReleasesEndToEnd(t *testing.T) {
	h := startStore(t)
	ctx := testCtx(t)

	r, err := h.c.RegisterRelease(ctx, client.RegisterReleaseRequest{
		Actor: "daan", Initiative: "hits", Slug: "0.5", Name: "The 0.5 release",
	})
	if err != nil {
		t.Fatalf("register release: %v", err)
	}
	if r.Initiative != "hits" || r.Slug != "0.5" || r.Shipped || r.Retired {
		t.Fatalf("release = %+v", r)
	}
	_, err = h.c.RegisterRelease(ctx, client.RegisterReleaseRequest{
		Actor: "daan", Initiative: "hits", Slug: "0.5", Name: "again",
	})
	wantAPIError(t, err, "already-registered")
	_, err = h.c.RegisterRelease(ctx, client.RegisterReleaseRequest{
		Actor: "daan", Initiative: "nowhere", Slug: "0.5", Name: "orphan",
	})
	wantAPIError(t, err, "unregistered-initiative")
	_, err = h.c.RegisterRelease(ctx, client.RegisterReleaseRequest{
		Actor: "daan", Initiative: "hits", Slug: ".5", Name: "bad slug",
	})
	wantAPIError(t, err, "invalid-release")

	// Targets: at creation, by edit, cleared by the empty string, and
	// always against the item's own initiative's registry.
	a, err := h.c.CreateItem(ctx, client.CreateItemRequest{
		Actor: "daan", Initiative: "hits", Type: contract.Bug,
		Report: "must land in 0.5", Target: "0.5",
	})
	if err != nil {
		t.Fatalf("create with target: %v", err)
	}
	if a.Target != "0.5" {
		t.Fatalf("created target = %q", a.Target)
	}
	_, err = h.c.CreateItem(ctx, client.CreateItemRequest{
		Actor: "daan", Initiative: "hits", Type: contract.Bug,
		Report: "aims at nothing", Target: "9.9",
	})
	wantAPIError(t, err, "unknown-release")

	b, err := h.c.CreateItem(ctx, client.CreateItemRequest{
		Actor: "daan", Initiative: "hits", Type: contract.Bug, Report: "targeted later",
	})
	if err != nil {
		t.Fatalf("create b: %v", err)
	}
	tgt := "0.5"
	if b, err = h.c.EditItem(ctx, client.EditItemRequest{Actor: "daan", ID: b.ID, Target: &tgt}); err != nil {
		t.Fatalf("edit target: %v", err)
	}
	if b.Target != "0.5" {
		t.Fatalf("edited target = %q", b.Target)
	}

	// The straggler gate: shipping is refused while non-terminal items
	// target the release, and the offenders are named.
	refs := []contract.ShipRef{{Tag: "v0.5.0", Note: "the evidence"}}
	_, err = h.c.ShipRelease(ctx, client.ShipReleaseRequest{
		Actor: "daan", Initiative: "hits", Slug: "0.5", Refs: refs,
	})
	wantAPIError(t, err, "open-targets")

	// The cut is the triage pass: one straggler resolves in, one moves
	// out — then the ship goes through with its evidence.
	if _, err := h.c.TransitionItem(ctx, client.TransitionItemRequest{
		Actor: "daan", ID: a.ID, To: contract.Resolved,
		FixedBy: []contract.FixRef{{Commit: "abc123"}},
	}); err != nil {
		t.Fatalf("resolve a: %v", err)
	}
	empty := ""
	if _, err := h.c.EditItem(ctx, client.EditItemRequest{Actor: "daan", ID: b.ID, Target: &empty}); err != nil {
		t.Fatalf("clear target: %v", err)
	}
	shipped, err := h.c.ShipRelease(ctx, client.ShipReleaseRequest{
		Actor: "daan", Initiative: "hits", Slug: "0.5", Refs: refs, Note: "cut clean",
	})
	if err != nil {
		t.Fatalf("ship: %v", err)
	}
	if !shipped.Shipped || len(shipped.ShipRefs) != 1 || shipped.ShipRefs[0].Tag != "v0.5.0" || shipped.ShipNote != "cut clean" {
		t.Fatalf("shipped release = %+v", shipped)
	}
	// The resolved item keeps its frozen target: composition is the
	// record — resolved, targeted 0.5, 0.5 shipped.
	got, err := h.c.GetItem(ctx, a.ID)
	if err != nil {
		t.Fatalf("get a: %v", err)
	}
	if got.Target != "0.5" || got.Status != contract.Resolved {
		t.Fatalf("record item = %+v", got)
	}

	// Terminal slugs refuse new targets and further ops.
	tgt5 := "0.5"
	_, err = h.c.EditItem(ctx, client.EditItemRequest{Actor: "daan", ID: b.ID, Target: &tgt5})
	wantAPIError(t, err, "release-shipped")
	_, err = h.c.ShipRelease(ctx, client.ShipReleaseRequest{
		Actor: "daan", Initiative: "hits", Slug: "0.5", Refs: refs,
	})
	wantAPIError(t, err, "already-shipped")

	// Guardless retirement: the targeted release retires, the stray
	// target stands, new targets refuse the retired slug, and the slug
	// is never reused.
	if _, err := h.c.RegisterRelease(ctx, client.RegisterReleaseRequest{
		Actor: "daan", Initiative: "hits", Slug: "0.6", Name: "Mistyped",
	}); err != nil {
		t.Fatalf("register 0.6: %v", err)
	}
	tgt6 := "0.6"
	if _, err := h.c.EditItem(ctx, client.EditItemRequest{Actor: "daan", ID: b.ID, Target: &tgt6}); err != nil {
		t.Fatalf("target 0.6: %v", err)
	}
	if _, err := h.c.RetireRelease(ctx, client.RetireReleaseRequest{
		Actor: "daan", Initiative: "hits", Slug: "0.6", Reason: "mistyped",
	}); err != nil {
		t.Fatalf("retire 0.6: %v", err)
	}
	if got, err := h.c.GetItem(ctx, b.ID); err != nil || got.Target != "0.6" {
		t.Fatalf("stray target = %+v, %v; want it standing", got, err)
	}
	c2, err := h.c.CreateItem(ctx, client.CreateItemRequest{
		Actor: "daan", Initiative: "hits", Type: contract.Bug, Report: "third",
	})
	if err != nil {
		t.Fatalf("create c: %v", err)
	}
	_, err = h.c.EditItem(ctx, client.EditItemRequest{Actor: "daan", ID: c2.ID, Target: &tgt6})
	wantAPIError(t, err, "release-retired")
	_, err = h.c.RegisterRelease(ctx, client.RegisterReleaseRequest{
		Actor: "daan", Initiative: "hits", Slug: "0.6", Name: "again",
	})
	wantAPIError(t, err, "slug-retired")

	// The list: shipped history stays with its refs, retired slugs drop.
	rs, err := h.c.ListReleases(ctx, client.ListReleasesRequest{Initiative: "hits"})
	if err != nil {
		t.Fatalf("list releases: %v", err)
	}
	if len(rs) != 1 || rs[0].Slug != "0.5" || !rs[0].Shipped || len(rs[0].ShipRefs) != 1 {
		t.Fatalf("release list = %+v", rs)
	}
	_, err = h.c.ListReleases(ctx, client.ListReleasesRequest{Initiative: "team-42"})
	wantAPIError(t, err, "invalid-initiative")

	// The registry is a projection: delete the bucket, restart the node,
	// and every refusal above still holds from the replayed state.
	js, err := jetstream.New(h.svcConn)
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	if err := js.DeleteKeyValue(ctx, contract.StateBucket); err != nil {
		t.Fatalf("delete bucket: %v", err)
	}
	svc, err := node.Start(ctx, h.svcConn, node.Config{})
	if err != nil {
		t.Fatalf("restart node: %v", err)
	}
	t.Cleanup(func() { _ = svc.Stop() })
	_, err = h.c.RegisterRelease(ctx, client.RegisterReleaseRequest{
		Actor: "daan", Initiative: "hits", Slug: "0.6", Name: "after replay",
	})
	wantAPIError(t, err, "slug-retired")
	_, err = h.c.EditItem(ctx, client.EditItemRequest{Actor: "daan", ID: c2.ID, Target: &tgt5})
	wantAPIError(t, err, "release-shipped")
	rs, err = h.c.ListReleases(ctx, client.ListReleasesRequest{Initiative: "hits"})
	if err != nil {
		t.Fatalf("list after replay: %v", err)
	}
	if len(rs) != 1 || rs[0].Slug != "0.5" || !rs[0].Shipped {
		t.Fatalf("replayed release list = %+v", rs)
	}
}

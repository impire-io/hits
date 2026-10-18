// Package clienttest is the surface every substrate must serve: the
// sentences and their refusals, the vocabularies, search, the graph and
// meaning, run against a fresh substrate per scenario (hits-hq decision
// 0023 point 1 — the surface is identical on chronicle and on the 0.7
// fleet). Each substrate's test calls Run with its own start.
package clienttest

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/contract"
)

// Run drives the surface against the client start boots: a fresh
// substrate with the initiative "hits" registered and nothing filed,
// whose Author is the actor every write records.
func Run(t *testing.T, start func(t *testing.T) client.Client) {
	t.Helper()
	t.Run("ItemRoundTrip", func(t *testing.T) {
		c := start(t)
		ctx := testCtx(t)
		call(c.RegisterProject(ctx, client.RegisterProjectRequest{Slug: "hits", Name: "HITS repo", Initiative: "hits"})).must(t)

		it := call(c.CreateItem(ctx, client.CreateItemRequest{Type: contract.Bug, Report: "the CLI panics on empty input", Priority: contract.High, Initiative: "hits", LocatedIn: []string{"hits"}, DiscoveredWhile: "building the CLI"})).must(t)
		if it.ID != "hits-1" || it.Status != contract.Open || it.Reporter != c.Author() || it.Created.IsZero() || it.Priority != contract.High {
			t.Fatalf("created item: %+v", it)
		}
		// The counter is dense: the next filing takes 2.
		if next := call(c.CreateItem(ctx, client.CreateItemRequest{Type: contract.Improvement, Report: "second", Initiative: "hits"})).must(t); next.ID != "hits-2" || next.Priority != contract.Normal {
			t.Fatalf("second item: %+v", next)
		}

		got := call(c.GetItem(ctx, it.ID)).must(t)
		if got.Report != it.Report || len(got.LocatedIn) != 1 || got.LocatedIn[0] != "hits" || got.DiscoveredWhile != "building the CLI" {
			t.Fatalf("get: %+v", got)
		}
		// The trail is the history: a note's author and time are the record's.
		noted := call(c.NoteItem(ctx, client.NoteItemRequest{ID: it.ID, Text: "reproduced on main"})).must(t)
		if len(noted.Notes) != 1 || noted.Notes[0].Author != c.Author() || noted.Notes[0].Text != "reproduced on main" || noted.Notes[0].At.IsZero() {
			t.Fatalf("note: %+v", noted.Notes)
		}
		// A note moved no state; the next guarded write still lands.
		claimed := call(c.ClaimItem(ctx, client.ClaimItemRequest{ID: it.ID})).must(t)
		if claimed.Claim == nil || claimed.Claim.By != c.Author() {
			t.Fatalf("claim: %+v", claimed.Claim)
		}
		wantCode(t, errOf(c.ClaimItem(ctx, client.ClaimItemRequest{ID: it.ID})), "already-claimed")
		stolen := call(c.ClaimItem(ctx, client.ClaimItemRequest{ID: it.ID, Steal: true})).must(t)
		if stolen.Claim == nil || stolen.Claim.StolenFrom != c.Author() {
			t.Fatalf("steal: %+v", stolen.Claim)
		}
		released := call(c.ReleaseItem(ctx, client.ReleaseItemRequest{ID: it.ID})).must(t)
		if released.Claim != nil {
			t.Fatalf("release left a claim: %+v", released.Claim)
		}
		wantCode(t, errOf(c.ReleaseItem(ctx, client.ReleaseItemRequest{ID: it.ID})), "not-claimed")

		blocked := call(c.BlockItem(ctx, client.BlockItemRequest{ID: it.ID, BlockedBy: "hits-2"})).must(t)
		if blocked.Status != contract.Blocked || blocked.BlockedBy != "hits-2" || blocked.Interrupted != contract.Open {
			t.Fatalf("block: %+v", blocked)
		}
		wantCode(t, errOf(c.TransitionItem(ctx, client.TransitionItemRequest{ID: it.ID, To: contract.Diagnosing})), "blocked")
		unblocked := call(c.UnblockItem(ctx, client.UnblockItemRequest{ID: it.ID})).must(t)
		if unblocked.Status != contract.Open || unblocked.BlockedBy != "" || unblocked.Interrupted != "" {
			t.Fatalf("unblock: %+v", unblocked)
		}
		wantCode(t, errOf(c.UnblockItem(ctx, client.UnblockItemRequest{ID: it.ID})), "not-blocked")

		linked := call(c.LinkItem(ctx, client.LinkItemRequest{ID: it.ID, Type: contract.RelatesTo, To: "hits-2"})).must(t)
		if len(linked.Links) != 1 || linked.Links[0].To != "hits-2" {
			t.Fatalf("link: %+v", linked.Links)
		}
		unlinked := call(c.UnlinkItem(ctx, client.LinkItemRequest{ID: it.ID, Type: contract.RelatesTo, To: "hits-2"})).must(t)
		if len(unlinked.Links) != 0 {
			t.Fatalf("unlink: %+v", unlinked.Links)
		}
		low := contract.Low
		edited := call(c.EditItem(ctx, client.EditItemRequest{ID: it.ID, Priority: &low, Lands: &[]contract.Land{{Repo: "hits", PR: "#1", After: []string{}}}})).must(t)
		if edited.Priority != contract.Low || len(edited.Lands) != 1 {
			t.Fatalf("edit: %+v", edited)
		}
		call(c.TransitionItem(ctx, client.TransitionItemRequest{ID: it.ID, To: contract.Diagnosing})).must(t)
		located := call(c.TransitionItem(ctx, client.TransitionItemRequest{ID: it.ID, To: contract.Located, LocatedIn: []string{"hits"}})).must(t)
		if located.Status != contract.Located {
			t.Fatalf("locate: %+v", located)
		}
		resolved := call(c.TransitionItem(ctx, client.TransitionItemRequest{ID: it.ID, To: contract.Resolved, FixedBy: []contract.FixRef{{Commit: "abc123", Note: "folded"}}, AmendedDesign: []string{"02-DESIGN/the-store.md"}})).must(t)
		if resolved.Status != contract.Resolved || resolved.Closed == "" || len(resolved.FixedBy) != 1 {
			t.Fatalf("resolve: %+v", resolved)
		}
		// Terminal is terminal; a note and a link still land.
		wantCode(t, errOf(c.TransitionItem(ctx, client.TransitionItemRequest{ID: it.ID, To: contract.Open})), "terminal-status")
		wantCode(t, errOf(c.EditItem(ctx, client.EditItemRequest{ID: it.ID, Priority: &low})), "terminal-status")
		call(c.NoteItem(ctx, client.NoteItemRequest{ID: it.ID, Text: "closing note"})).must(t)
		if final := call(c.GetItem(ctx, it.ID)).must(t); len(final.Notes) != 2 || final.Status != contract.Resolved {
			t.Fatalf("final: %+v", final)
		}

		// Tombstone: the item leaves every view.
		wantCode(t, errOf(c.GetItem(ctx, "hits-99")), "not-found")
		voided := call(c.TombstoneItem(ctx, client.TombstoneItemRequest{ID: "hits-2", Reason: "a test record"})).must(t)
		if !voided.Tombstoned || voided.TombstoneReason != "a test record" {
			t.Fatalf("tombstone: %+v", voided)
		}
		wantCode(t, errOf(c.GetItem(ctx, "hits-2")), "not-found")
		wantCode(t, errOf(c.NoteItem(ctx, client.NoteItemRequest{ID: "hits-2", Text: "too late"})), "tombstoned")
		items := call(c.ListItems(ctx)).must(t)
		if len(items) != 1 || items[0].ID != "hits-1" {
			t.Fatalf("list: %+v", items)
		}
	})

	t.Run("InvariantRejectionsByName", func(t *testing.T) {
		c := start(t)
		ctx := testCtx(t)
		wantCode(t, errOf(c.CreateItem(ctx, client.CreateItemRequest{Type: contract.Task, Report: "no project", Initiative: "hits"})), "task-requires-location")
		wantCode(t, errOf(c.CreateItem(ctx, client.CreateItemRequest{Type: contract.Bug, Report: "no such project", Initiative: "hits", LocatedIn: []string{"nope"}})), "unregistered-project")
		wantCode(t, errOf(c.CreateItem(ctx, client.CreateItemRequest{Type: contract.Bug, Report: "no such initiative", Initiative: "nope"})), "unregistered-initiative")
		wantCode(t, errOf(c.CreateItem(ctx, client.CreateItemRequest{Type: contract.Bug, Report: "no such release", Initiative: "hits", Target: "9.9"})), "unknown-release")
		wantCode(t, errOf(c.CreateItem(ctx, client.CreateItemRequest{Type: "feature", Report: "bad type", Initiative: "hits"})), "invalid-type")
		wantCode(t, errOf(c.NoteItem(ctx, client.NoteItemRequest{ID: "hits-1", Text: "nobody"})), "not-found")
		// A refused create burns no number.
		it := call(c.CreateItem(ctx, client.CreateItemRequest{Type: contract.Bug, Report: "first", Initiative: "hits"})).must(t)
		if it.ID != "hits-1" {
			t.Fatalf("refused creates must not spend numbers: %s", it.ID)
		}
	})

	t.Run("ConcurrentCreatesAreDense", func(t *testing.T) {
		c := start(t)
		ctx := testCtx(t)
		const n = 8
		ids := make([]string, n)
		errs := make([]error, n)
		var wg sync.WaitGroup
		for i := range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				it, err := c.CreateItem(ctx, client.CreateItemRequest{Type: contract.Bug, Report: "concurrent", Initiative: "hits"})
				ids[i], errs[i] = it.ID, err
			}()
		}
		wg.Wait()
		seen := map[string]bool{}
		for i := range n {
			if errs[i] != nil {
				t.Fatalf("create %d: %v", i, errs[i])
			}
			seen[ids[i]] = true
		}
		for i := 1; i <= n; i++ {
			if !seen["hits-"+itoa(i)] {
				t.Fatalf("the counter skipped hits-%d: %v", i, ids)
			}
		}
	})

	t.Run("ConcurrentClaimsAdmitOneWinner", func(t *testing.T) {
		c := start(t)
		ctx := testCtx(t)
		it := call(c.CreateItem(ctx, client.CreateItemRequest{Type: contract.Bug, Report: "contended", Initiative: "hits"})).must(t)
		const n = 4
		errs := make([]error, n)
		var wg sync.WaitGroup
		for i := range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, errs[i] = c.ClaimItem(ctx, client.ClaimItemRequest{ID: it.ID})
			}()
		}
		wg.Wait()
		won := 0
		for _, err := range errs {
			if err == nil {
				won++
				continue
			}
			wantCode(t, err, "already-claimed")
		}
		if won != 1 {
			t.Fatalf("want exactly one winner, got %d (%v)", won, errs)
		}
	})

	t.Run("VocabulariesEndToEnd", func(t *testing.T) {
		c := start(t)
		ctx := testCtx(t)
		p := call(c.RegisterProject(ctx, client.RegisterProjectRequest{Slug: "hits", Name: "HITS repo", Description: "the code", Initiative: "hits"})).must(t)
		if p.Initiative != "hits" || p.Name != "HITS repo" {
			t.Fatalf("project: %+v", p)
		}
		wantCode(t, errOf(c.RegisterProject(ctx, client.RegisterProjectRequest{Slug: "hits", Name: "again", Initiative: "hits"})), "already-registered")
		wantCode(t, errOf(c.RegisterProject(ctx, client.RegisterProjectRequest{Slug: "orphan", Name: "x", Initiative: "nope"})), "unregistered-initiative")
		call(c.RegisterInitiative(ctx, client.RegisterInitiativeRequest{Slug: "other", Name: "Other"})).must(t)
		wantCode(t, errOf(c.RegisterInitiative(ctx, client.RegisterInitiativeRequest{Slug: "other", Name: "Other"})), "already-registered")
		moved := call(c.AssignProject(ctx, client.AssignProjectRequest{Slug: "hits", Initiative: "other"})).must(t)
		if moved.Initiative != "other" {
			t.Fatalf("assign: %+v", moved)
		}
		// An item cannot span initiatives.
		wantCode(t, errOf(c.CreateItem(ctx, client.CreateItemRequest{Type: contract.Bug, Report: "spans", Initiative: "hits", LocatedIn: []string{"hits"}})), "initiative-mismatch")
		call(c.AssignProject(ctx, client.AssignProjectRequest{Slug: "hits", Initiative: "hits"})).must(t)
		retired := call(c.RetireProject(ctx, client.RetireProjectRequest{Slug: "hits", Reason: "mistyped"})).must(t)
		if !retired.Retired {
			t.Fatalf("retire: %+v", retired)
		}
		if list := call(c.ListProjects(ctx)).must(t); len(list) != 0 {
			t.Fatalf("a retired project is listed: %+v", list)
		}
		wantCode(t, errOf(c.CreateItem(ctx, client.CreateItemRequest{Type: contract.Bug, Report: "retired", Initiative: "hits", LocatedIn: []string{"hits"}})), "retired-project")
		wantCode(t, errOf(c.RegisterProject(ctx, client.RegisterProjectRequest{Slug: "hits", Name: "again", Initiative: "hits"})), "slug-retired")
		inits := call(c.ListInitiatives(ctx)).must(t)
		if len(inits) != 2 {
			t.Fatalf("initiatives: %+v", inits)
		}
		call(c.RetireInitiative(ctx, client.RetireInitiativeRequest{Slug: "other", Reason: "done"})).must(t)
		if inits = call(c.ListInitiatives(ctx)).must(t); len(inits) != 1 || inits[0].Slug != "hits" {
			t.Fatalf("initiatives after retire: %+v", inits)
		}
		wantCode(t, errOf(c.CreateItem(ctx, client.CreateItemRequest{Type: contract.Bug, Report: "retired initiative", Initiative: "other"})), "retired-initiative")

		// Releases: a child of the initiative, the ship gate, the target rules.
		rel := call(c.RegisterRelease(ctx, client.RegisterReleaseRequest{Initiative: "hits", Slug: "0.5.0", Name: "The 0.5.0 release", Description: "first cut"})).must(t)
		if rel.Initiative != "hits" || rel.Slug != "0.5.0" {
			t.Fatalf("release: %+v", rel)
		}
		wantCode(t, errOf(c.RegisterRelease(ctx, client.RegisterReleaseRequest{Initiative: "hits", Slug: "0.5.0", Name: "again"})), "already-registered")
		a := call(c.CreateItem(ctx, client.CreateItemRequest{Type: contract.Bug, Report: "must land", Initiative: "hits", Target: "0.5.0"})).must(t)
		if a.Target != "0.5.0" {
			t.Fatalf("target on create: %+v", a)
		}
		target := "0.5.0"
		b := call(c.CreateItem(ctx, client.CreateItemRequest{Type: contract.Bug, Report: "targeted later", Initiative: "hits"})).must(t)
		call(c.EditItem(ctx, client.EditItemRequest{ID: b.ID, Target: &target})).must(t)
		err := errOf(c.ShipRelease(ctx, client.ShipReleaseRequest{Initiative: "hits", Slug: "0.5.0", Refs: []contract.ShipRef{{Tag: "v0.5.0"}}}))
		wantCode(t, err, "open-targets")
		if !strings.Contains(err.Error(), a.ID) || !strings.Contains(err.Error(), b.ID) {
			t.Fatalf("the gate names the offenders: %v", err)
		}
		call(c.TransitionItem(ctx, client.TransitionItemRequest{ID: a.ID, To: contract.Resolved, FixedBy: []contract.FixRef{{Commit: "abc"}}})).must(t)
		empty := ""
		call(c.EditItem(ctx, client.EditItemRequest{ID: b.ID, Target: &empty})).must(t)
		shipped := call(c.ShipRelease(ctx, client.ShipReleaseRequest{Initiative: "hits", Slug: "0.5.0", Refs: []contract.ShipRef{{Tag: "v0.5.0"}}, Note: "the evidence"})).must(t)
		if !shipped.Shipped || len(shipped.ShipRefs) != 1 || shipped.ShipNote != "the evidence" {
			t.Fatalf("ship: %+v", shipped)
		}
		wantCode(t, errOf(c.EditItem(ctx, client.EditItemRequest{ID: b.ID, Target: &target})), "release-shipped")
		wantCode(t, errOf(c.ShipRelease(ctx, client.ShipReleaseRequest{Initiative: "hits", Slug: "0.5.0", Refs: []contract.ShipRef{{Tag: "again"}}})), "already-shipped")
		rels := call(c.ListReleases(ctx, client.ListReleasesRequest{Initiative: "hits"})).must(t)
		if len(rels) != 1 || !rels[0].Shipped {
			t.Fatalf("releases: %+v", rels)
		}
		call(c.RegisterRelease(ctx, client.RegisterReleaseRequest{Initiative: "hits", Slug: "0.6.0", Name: "next"})).must(t)
		call(c.RetireRelease(ctx, client.RetireReleaseRequest{Initiative: "hits", Slug: "0.6.0", Reason: "never cut"})).must(t)
		if rels = call(c.ListReleases(ctx, client.ListReleasesRequest{Initiative: "hits"})).must(t); len(rels) != 1 {
			t.Fatalf("a retired release is listed: %+v", rels)
		}
	})

	t.Run("SearchFiltersAndPaging", func(t *testing.T) {
		c := start(t)
		ctx := testCtx(t)
		call(c.RegisterInitiative(ctx, client.RegisterInitiativeRequest{Slug: "other", Name: "Other"})).must(t)
		call(c.RegisterRelease(ctx, client.RegisterReleaseRequest{Initiative: "hits", Slug: "0.5", Name: "0.5"})).must(t)
		a := call(c.CreateItem(ctx, client.CreateItemRequest{Type: contract.Bug, Report: "the login loop keeps repeating", Initiative: "hits", Target: "0.5"})).must(t)
		b := call(c.CreateItem(ctx, client.CreateItemRequest{Type: contract.Improvement, Report: "a faster login page", Initiative: "other"})).must(t)
		call(c.NoteItem(ctx, client.NoteItemRequest{ID: a.ID, Text: "the pumpernickel case reproduces it"})).must(t)
		call(c.TransitionItem(ctx, client.TransitionItemRequest{ID: b.ID, To: contract.Resolved})).must(t)

		until(t, "search over reports", func() bool {
			r, err := c.SearchItems(ctx, client.SearchRequest{Query: "login"})
			return err == nil && r.Total == 2
		})
		until(t, "search over the trail", func() bool {
			r, err := c.SearchItems(ctx, client.SearchRequest{Query: "pumpernickel"})
			return err == nil && r.Total == 1 && r.Hits[0].ID == a.ID
		})
		r := call(c.SearchItems(ctx, client.SearchRequest{Query: "login", Status: contract.Open})).must(t)
		if r.Total != 1 || r.Hits[0].ID != a.ID {
			t.Fatalf("status filter: %+v", r)
		}
		r = call(c.SearchItems(ctx, client.SearchRequest{Query: "login", Initiative: "other"})).must(t)
		if r.Total != 1 || r.Hits[0].ID != b.ID {
			t.Fatalf("initiative filter: %+v", r)
		}
		r = call(c.SearchItems(ctx, client.SearchRequest{Query: "login", Target: "0.5"})).must(t)
		if r.Total != 1 || r.Hits[0].ID != a.ID {
			t.Fatalf("target filter: %+v", r)
		}
		r = call(c.SearchItems(ctx, client.SearchRequest{Query: "login", Type: contract.Improvement})).must(t)
		if r.Total != 1 || r.Hits[0].ID != b.ID {
			t.Fatalf("type filter: %+v", r)
		}
		r = call(c.SearchItems(ctx, client.SearchRequest{Query: "login", Limit: 1})).must(t)
		if r.Total != 2 || len(r.Hits) != 1 {
			t.Fatalf("paging: %+v", r)
		}
		r = call(c.SearchItems(ctx, client.SearchRequest{Query: "login", Limit: 1, Offset: 1})).must(t)
		if r.Total != 2 || len(r.Hits) != 1 {
			t.Fatalf("paging offset: %+v", r)
		}
		// An empty query is the listing, with the same filters.
		r = call(c.SearchItems(ctx, client.SearchRequest{Status: contract.Resolved})).must(t)
		if r.Total != 1 || r.Hits[0].ID != b.ID {
			t.Fatalf("listing with a filter: %+v", r)
		}
		call(c.TombstoneItem(ctx, client.TombstoneItemRequest{ID: b.ID, Reason: "a test record"})).must(t)
		until(t, "a tombstoned item leaves the search", func() bool {
			r, err := c.SearchItems(ctx, client.SearchRequest{Query: "login"})
			return err == nil && r.Total == 1
		})
	})

	t.Run("GraphEdges", func(t *testing.T) {
		c := start(t)
		ctx := testCtx(t)
		call(c.RegisterProject(ctx, client.RegisterProjectRequest{Slug: "hits", Name: "HITS repo", Initiative: "hits"})).must(t)
		a := call(c.CreateItem(ctx, client.CreateItemRequest{Type: contract.Bug, Report: "one", Initiative: "hits", LocatedIn: []string{"hits"}})).must(t)
		b := call(c.CreateItem(ctx, client.CreateItemRequest{Type: contract.Bug, Report: "two", Initiative: "hits"})).must(t)
		call(c.ClaimItem(ctx, client.ClaimItemRequest{ID: a.ID})).must(t)
		call(c.LinkItem(ctx, client.LinkItemRequest{ID: a.ID, Type: contract.RelatesTo, To: b.ID})).must(t)
		call(c.BlockItem(ctx, client.BlockItemRequest{ID: b.ID, BlockedBy: a.ID})).must(t)

		edgesOf := func(r client.NeighborsReply) map[string]string {
			m := map[string]string{}
			for _, e := range r.Edges {
				m[e.Type] = string(e.To.Kind) + ":" + e.To.ID
			}
			return m
		}
		until(t, "the item's out-edges", func() bool {
			r, err := c.GraphNeighbors(ctx, client.NeighborsRequest{Kind: client.NodeItem, ID: a.ID, Direction: "out"})
			if err != nil {
				return false
			}
			m := edgesOf(r)
			return m[client.EdgeLocatedIn] == "project:hits" && m[client.EdgeInInitiative] == "initiative:hits" &&
				m[client.EdgeReportedBy] == "actor:"+c.Author() && m[client.EdgeClaimedBy] == "actor:"+c.Author() && m[client.EdgeRelatesTo] == "item:"+b.ID
		})
		r := call(c.GraphNeighbors(ctx, client.NeighborsRequest{Kind: client.NodeItem, ID: a.ID, Direction: "out", Types: []string{client.EdgeLocatedIn}})).must(t)
		if len(r.Edges) != 1 || r.Edges[0].To.Name != "HITS repo" {
			t.Fatalf("one type, with the project's name: %+v", r.Edges)
		}
		in := call(c.GraphNeighbors(ctx, client.NeighborsRequest{Kind: client.NodeProject, ID: "hits", Direction: "in"})).must(t)
		if len(in.Edges) != 1 || in.Edges[0].From.ID != a.ID {
			t.Fatalf("the project's in-edges: %+v", in.Edges)
		}
		blockers := call(c.GraphNeighbors(ctx, client.NeighborsRequest{Kind: client.NodeItem, ID: b.ID, Direction: "out", Types: []string{client.EdgeBlockedBy}})).must(t)
		if len(blockers.Edges) != 1 || blockers.Edges[0].To.ID != a.ID {
			t.Fatalf("blocked-by edge: %+v", blockers.Edges)
		}
		w := call(c.GraphWalk(ctx, client.WalkRequest{Kind: client.NodeInitiative, ID: "hits", Direction: "in", Depth: 1})).must(t)
		ids := map[string]bool{}
		for _, n := range w.Nodes {
			ids[string(n.Kind)+":"+n.ID] = true
		}
		if !ids["item:"+a.ID] || !ids["item:"+b.ID] || !ids["project:hits"] {
			t.Fatalf("walk from the initiative: %+v", w.Nodes)
		}
		call(c.UnblockItem(ctx, client.UnblockItemRequest{ID: b.ID})).must(t)
		until(t, "a lifted block drops its edge", func() bool {
			r, err := c.GraphNeighbors(ctx, client.NeighborsRequest{Kind: client.NodeItem, ID: b.ID, Direction: "out", Types: []string{client.EdgeBlockedBy}})
			return err == nil && len(r.Edges) == 0
		})
	})

	t.Run("SemanticSearch", func(t *testing.T) {
		c := start(t)
		ctx := testCtx(t)
		a := call(c.CreateItem(ctx, client.CreateItemRequest{Type: contract.Bug, Report: "the login page loops forever on a stale cookie", Initiative: "hits"})).must(t)
		b := call(c.CreateItem(ctx, client.CreateItemRequest{Type: contract.Bug, Report: "the invoice total is rounded the wrong way", Initiative: "hits"})).must(t)
		until(t, "the meaning index ranks the nearest item first", func() bool {
			rep, err := c.SemanticSearch(ctx, client.SemanticRequest{Text: "a stale cookie makes the login page loop"})
			return err == nil && len(rep.Hits) == 2 && rep.Hits[0].ID == a.ID && rep.Hits[1].ID == b.ID
		})
		call(c.TombstoneItem(ctx, client.TombstoneItemRequest{ID: b.ID, Reason: "a test record"})).must(t)
		until(t, "a tombstoned item leaves the semantic reply", func() bool {
			rep, err := c.SemanticSearch(ctx, client.SemanticRequest{Text: "invoice rounding"})
			return err == nil && len(rep.Hits) == 1 && rep.Hits[0].ID == a.ID
		})
	})
}

func testCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// wantCode fails unless err is an APIError with the given code.
func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != code {
		t.Fatalf("want APIError %q, got %v", code, err)
	}
}

// result holds a call's value and error so a test reads call(c.X(…)).must(t).
type result[T any] struct {
	v   T
	err error
}

func call[T any](v T, err error) result[T] { return result[T]{v, err} }
func (x result[T]) must(t *testing.T) T {
	t.Helper()
	if x.err != nil {
		t.Fatalf("unexpected error: %v", x.err)
	}
	return x.v
}

// until polls a derived read — the indexes trail the history — until it
// holds or the deadline passes.
func until(t *testing.T, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatalf("%s: never held", what)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
func errOf[T any](_ T, err error) error { return err }
func itoa(i int) string                 { return strconv.Itoa(i) }

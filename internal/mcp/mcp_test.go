package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	chron "github.com/impire-io/chronicle/client"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/client/chronicle"
	"github.com/impire-io/hits/contract"
	"github.com/impire-io/hits/internal/chronicletest"
	"github.com/impire-io/hits/internal/mcp"
)

// harness is chronicle's quick start with a HITS declared in it; tools
// run against the real wire, never a mock.
type harness struct {
	cc *chron.Client
}

func startStore(t *testing.T) *harness {
	t.Helper()
	return &harness{cc: chronicletest.Start(t)}
}

// session connects an in-process MCP client to a server built on the
// harness's client — the same server production runs over stdio.
func session(t *testing.T, h *harness) *sdk.ClientSession {
	t.Helper()
	srv := mcp.NewServer(chronicle.New(h.cc, chronicletest.Store), "hits")
	st, ct := sdk.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := srv.Connect(ctx, st, nil); err != nil {
		t.Fatalf("connect server transport: %v", err)
	}
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("connect client transport: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func testCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// call invokes one tool, failing the test on a protocol error.
func call(t *testing.T, cs *sdk.ClientSession, name string, args any) *sdk.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(testCtx(t), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	return res
}

// decode reads a successful call's structured content into T.
func decode[T any](t *testing.T, res *sdk.CallToolResult) T {
	t.Helper()
	var out T
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	return out
}

// callItem invokes an item tool and decodes the returned snapshot.
func callItem(t *testing.T, cs *sdk.ClientSession, name string, args any) contract.Item {
	t.Helper()
	res := call(t, cs, name, args)
	if res.IsError {
		t.Fatalf("call %s: tool error: %s", name, resultText(res))
	}
	return decode[contract.Item](t, res)
}

func resultText(res *sdk.CallToolResult) string {
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

// guardConnector fails the test the moment the server touches the wire —
// for boots that must be rejected during validation.
func guardConnector(t *testing.T) mcp.Connector {
	return func(string, string) (client.Client, func(), error) {
		t.Error("connector called: the server should fail before connecting")
		return nil, nil, errors.New("no connection in this test")
	}
}

// TestToolList pins the surface: exactly the design's tools, one per
// client endpoint, the query and list tools read-only.
func TestToolList(t *testing.T) {
	h := startStore(t)
	cs := session(t, h)

	res, err := cs.ListTools(testCtx(t), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	want := []string{
		"assign_project", "block_item", "claim_item", "create_item",
		"edit_item", "get_item", "graph_neighbors", "graph_walk",
		"link_items", "list_initiatives", "list_projects",
		"list_releases", "note_item", "register_initiative",
		"register_project", "register_release", "release_item",
		"retire_initiative", "retire_project", "retire_release",
		"search_items", "semantic_search", "ship_release",
		"tombstone_item", "transition_item", "unblock_item",
		"unlink_items",
	}
	var got []string
	readOnly := map[string]bool{}
	for _, tool := range res.Tools {
		got = append(got, tool.Name)
		readOnly[tool.Name] = tool.Annotations != nil && tool.Annotations.ReadOnlyHint
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("tool list = %v, want %v", got, want)
	}

	wantReadOnly := []string{"get_item", "graph_neighbors", "graph_walk", "list_initiatives", "list_projects", "list_releases", "search_items", "semantic_search"}
	for _, name := range want {
		if wantRO := slices.Contains(wantReadOnly, name); readOnly[name] != wantRO {
			t.Errorf("tool %s read-only = %v, want %v", name, readOnly[name], wantRO)
		}
	}
}

// TestItemLifecycleTools drives one item through the whole lifecycle tool
// by tool, decoding every reply into the contract snapshot.
func TestItemLifecycleTools(t *testing.T) {
	h := startStore(t)
	cs := session(t, h)

	res := call(t, cs, "register_project", map[string]any{"slug": "hits", "name": "HITS repo", "initiative": "hits"})
	if res.IsError {
		t.Fatalf("register_project: %s", resultText(res))
	}
	if p := decode[contract.Project](t, res); p.Slug != "hits" {
		t.Errorf("registered slug = %q, want hits", p.Slug)
	}

	item := callItem(t, cs, "create_item", map[string]any{
		"type": "bug", "report": "the MCP server panics on empty input",
		"priority": "high", "located-in": []string{"hits"},
		"discovered-while": "building hits-mcp",
	})
	id := item.ID
	if item.Status != contract.Open || item.Priority != contract.High ||
		item.Reporter != "admin" || item.DiscoveredWhile != "building hits-mcp" {
		t.Errorf("created item = %+v", item)
	}

	if got := callItem(t, cs, "get_item", map[string]any{"id": id}); got.Report != item.Report {
		t.Errorf("get_item report = %q, want %q", got.Report, item.Report)
	}

	item = callItem(t, cs, "claim_item", map[string]any{"id": id})
	if item.Claim == nil || item.Claim.By != "admin" {
		t.Errorf("claimed item = %+v", item)
	}

	if item = callItem(t, cs, "release_item", map[string]any{"id": id}); item.Claim != nil {
		t.Errorf("released item still claimed: %+v", item)
	}
	callItem(t, cs, "claim_item", map[string]any{"id": id})

	item = callItem(t, cs, "note_item", map[string]any{"id": id, "text": "reproduced on main"})
	if len(item.Notes) != 1 || item.Notes[0].Text != "reproduced on main" {
		t.Errorf("noted item = %+v", item)
	}

	item = callItem(t, cs, "block_item", map[string]any{"id": id, "blocked-by": "waiting on upstream fix"})
	if item.Status != contract.Blocked || item.BlockedBy != "waiting on upstream fix" || item.Interrupted != contract.Open {
		t.Errorf("blocked item = %+v", item)
	}

	if item = callItem(t, cs, "unblock_item", map[string]any{"id": id}); item.Status != contract.Open {
		t.Errorf("unblocked item = %+v", item)
	}

	callItem(t, cs, "transition_item", map[string]any{"id": id, "to": "diagnosing"})
	callItem(t, cs, "transition_item", map[string]any{"id": id, "to": "located"})
	item = callItem(t, cs, "transition_item", map[string]any{
		"id": id, "to": "resolved",
		"fixed-by":       []map[string]any{{"commit": "abc123", "note": "folded the fix"}},
		"amended-design": []string{"02-DESIGN/services.md"},
	})
	if item.Status != contract.Resolved || item.Closed == "" ||
		len(item.FixedBy) != 1 || item.FixedBy[0].Commit != "abc123" ||
		len(item.AmendedDesign) != 1 {
		t.Errorf("resolved item = %+v", item)
	}
}

func TestLinkEditTombstoneTools(t *testing.T) {
	h := startStore(t)
	cs := session(t, h)

	a := callItem(t, cs, "create_item", map[string]any{"type": "bug", "report": "the parser crashes"}).ID
	b := callItem(t, cs, "create_item", map[string]any{"type": "bug", "report": "parser crash on unicode"}).ID

	item := callItem(t, cs, "link_items", map[string]any{"id": a, "type": "duplicates", "to": b})
	if len(item.Links) != 1 || item.Links[0].To != b {
		t.Errorf("linked item = %+v", item)
	}
	if item = callItem(t, cs, "unlink_items", map[string]any{"id": a, "type": "duplicates", "to": b}); len(item.Links) != 0 {
		t.Errorf("unlinked item still has links: %+v", item)
	}

	if item = callItem(t, cs, "edit_item", map[string]any{"id": a, "priority": "low"}); item.Priority != contract.Low {
		t.Errorf("edited item = %+v", item)
	}

	item = callItem(t, cs, "tombstone_item", map[string]any{"id": b, "reason": "filed twice"})
	if !item.Tombstoned || item.TombstoneReason != "filed twice" {
		t.Errorf("tombstoned item = %+v", item)
	}

	res := call(t, cs, "list_projects", nil)
	if res.IsError {
		t.Fatalf("list_projects: %s", resultText(res))
	}
	if ps := decode[[]contract.Project](t, res); len(ps) != 0 {
		t.Errorf("projects = %+v, want none registered", ps)
	}
}

// TestRetireProjectTool retires a slug through the tool surface: the reply
// carries the retired snapshot and the listing no longer offers the slug.
func TestRetireProjectTool(t *testing.T) {
	h := startStore(t)
	cs := session(t, h)

	res := call(t, cs, "register_project", map[string]any{"slug": "001-hits", "name": "HITS", "initiative": "hits"})
	if res.IsError {
		t.Fatalf("register_project: %s", resultText(res))
	}

	res = call(t, cs, "retire_project", map[string]any{"slug": "001-hits", "reason": "setup validation artifact"})
	if res.IsError {
		t.Fatalf("retire_project: %s", resultText(res))
	}
	if p := decode[contract.Project](t, res); !p.Retired || p.RetireReason != "setup validation artifact" {
		t.Errorf("retired project = %+v", p)
	}

	res = call(t, cs, "list_projects", nil)
	if res.IsError {
		t.Fatalf("list_projects: %s", resultText(res))
	}
	if ps := decode[[]contract.Project](t, res); len(ps) != 0 {
		t.Errorf("projects = %+v, want the retired slug dropped", ps)
	}

	res = call(t, cs, "register_project", map[string]any{"slug": "001-hits", "name": "HITS again", "initiative": "hits"})
	if !res.IsError || !strings.Contains(resultText(res), "slug-retired") {
		t.Errorf("re-register retired slug = %v %q, want slug-retired", res.IsError, resultText(res))
	}
}

// TestInvariantErrorVerbatim proves rejections reach the agent as isError
// results carrying the machine-legible code unchanged.
func TestInvariantErrorVerbatim(t *testing.T) {
	h := startStore(t)
	cs := session(t, h)

	res := call(t, cs, "claim_item", map[string]any{"id": "no-such-item"})
	if !res.IsError {
		t.Fatal("claiming a missing item should be a tool error")
	}
	if text := resultText(res); !strings.Contains(text, "not-found") {
		t.Errorf("error text = %q, want the not-found code", text)
	}

	res = call(t, cs, "create_item", map[string]any{"type": "task", "report": "task without a home"})
	if !res.IsError {
		t.Fatal("a task without located-in should be a tool error")
	}
	if text := resultText(res); !strings.Contains(text, "task-requires-location") {
		t.Errorf("error text = %q, want the task-requires-location code", text)
	}
}

// TestQueryTools proves the four query tools find what the item tools
// created, against the real index services.
func TestQueryTools(t *testing.T) {
	h := startStore(t)
	cs := session(t, h)

	call(t, cs, "register_project", map[string]any{"slug": "hits", "name": "HITS repo", "initiative": "hits"})
	match := callItem(t, cs, "create_item", map[string]any{
		"type": "bug", "report": "auth login loop keeps repeating", "located-in": []string{"hits"},
	}).ID
	callItem(t, cs, "create_item", map[string]any{
		"type": "bug", "report": "the build cache misses every time",
	})
	callItem(t, cs, "claim_item", map[string]any{"id": match})

	// The indexes trail the history: poll until they have caught up.
	var searchReply client.SearchReply
	untilOK(t, "search_items finds the item", func() bool {
		res := call(t, cs, "search_items", map[string]any{"query": "login"})
		if res.IsError {
			return false
		}
		searchReply = decode[client.SearchReply](t, res)
		return searchReply.Total == 1
	})
	if searchReply.Total != 1 || len(searchReply.Hits) != 1 || searchReply.Hits[0].ID != match {
		t.Errorf("search reply = %+v, want just %s", searchReply, match)
	}

	var edges []client.GraphEdge
	untilOK(t, "graph_neighbors sees the edges", func() bool {
		res := call(t, cs, "graph_neighbors", map[string]any{"kind": "item", "id": match})
		if res.IsError {
			return false
		}
		edges = decode[client.NeighborsReply](t, res).Edges
		return len(edges) >= 4
	})
	res := call(t, cs, "graph_neighbors", map[string]any{"kind": "item", "id": match})
	if res.IsError {
		t.Fatalf("graph_neighbors: %s", resultText(res))
	}
	var hasProject, hasClaim bool
	for _, e := range edges {
		hasProject = hasProject || (e.Type == client.EdgeLocatedIn && e.To.ID == "hits")
		hasClaim = hasClaim || (e.Type == client.EdgeClaimedBy)
	}
	if !hasProject || !hasClaim {
		t.Errorf("neighbor edges = %+v, want located-in hits and a claimed-by edge", edges)
	}

	res = call(t, cs, "graph_walk", map[string]any{"kind": "item", "id": match, "depth": 2})
	if res.IsError {
		t.Fatalf("graph_walk: %s", resultText(res))
	}
	walk := decode[client.WalkReply](t, res)
	if len(walk.Edges) == 0 {
		t.Errorf("walk reply = %+v, want edges", walk)
	}
	var hasActor, hasProjectNode bool
	for _, n := range walk.Nodes {
		hasActor = hasActor || (n.Kind == client.NodeActor && n.ID == "admin")
		hasProjectNode = hasProjectNode || (n.Kind == client.NodeProject && n.ID == "hits")
	}
	if !hasActor || !hasProjectNode {
		t.Errorf("walk nodes = %+v, want actor admin and project hits reached", walk.Nodes)
	}
}

// TestRunFailsFast proves every boot precondition refuses to serve.
func TestRunFailsFast(t *testing.T) {
	var errOut strings.Builder

	err := mcp.Run(testCtx(t), []string{"--initiative", "Not Valid!"}, &errOut, guardConnector(t))
	if err == nil || !strings.Contains(err.Error(), "well-formed") {
		t.Errorf("bad initiative: err = %v, want a form error", err)
	}

	err = mcp.Run(testCtx(t), nil, &errOut, func(string, string) (client.Client, func(), error) {
		return nil, nil, errors.New("chronicle context missing")
	})
	if err == nil || !strings.Contains(err.Error(), "context missing") {
		t.Errorf("connect failure: err = %v, want the connector's error", err)
	}
}

// TestInitiativeTools rounds the four 0016 tools over the wire: register
// and list initiatives, assign a project, retire — and create_item
// honoring the server's startup default initiative.
func TestInitiativeTools(t *testing.T) {
	h := startStore(t)
	cs := session(t, h)

	res := call(t, cs, "register_initiative", map[string]any{"slug": "pra", "name": "PRA"})
	if res.IsError {
		t.Fatalf("register_initiative: %s", resultText(res))
	}
	if i := decode[contract.Initiative](t, res); i.Slug != "pra" || i.Name != "PRA" {
		t.Errorf("registered initiative = %+v", i)
	}

	res = call(t, cs, "list_initiatives", map[string]any{})
	if res.IsError {
		t.Fatalf("list_initiatives: %s", resultText(res))
	}
	if is := decode[[]contract.Initiative](t, res); len(is) != 2 {
		t.Errorf("initiative list = %+v, want hits and pra", is)
	}

	// The startup default feeds a create that names no initiative; an
	// explicit one outranks it.
	item := callItem(t, cs, "create_item", map[string]any{"type": "bug", "report": "default initiative"})
	if item.ID != "hits-1" || item.Initiative != "hits" {
		t.Errorf("default-initiative item = %s (%s)", item.ID, item.Initiative)
	}
	item = callItem(t, cs, "create_item", map[string]any{"type": "bug", "report": "explicit", "initiative": "pra"})
	if item.ID != "pra-1" {
		t.Errorf("explicit-initiative item = %s", item.ID)
	}

	res = call(t, cs, "register_project", map[string]any{"slug": "pra-c1c", "name": "PRA repo", "initiative": "pra"})
	if res.IsError {
		t.Fatalf("register_project: %s", resultText(res))
	}
	res = call(t, cs, "assign_project", map[string]any{"slug": "pra-c1c", "initiative": "hits"})
	if res.IsError {
		t.Fatalf("assign_project: %s", resultText(res))
	}
	if p := decode[contract.Project](t, res); p.Initiative != "hits" {
		t.Errorf("assigned project = %+v", p)
	}

	res = call(t, cs, "retire_initiative", map[string]any{"slug": "pra", "reason": "wound down"})
	if res.IsError {
		t.Fatalf("retire_initiative: %s", resultText(res))
	}
	if i := decode[contract.Initiative](t, res); !i.Retired {
		t.Errorf("retired initiative = %+v", i)
	}
}

// TestReleaseTools rounds the four 0017 tools over the wire: register a
// release, target items at it, the straggler gate refusing the ship, the
// clear that lets it through, and the list carrying shipped history.
func TestReleaseTools(t *testing.T) {
	h := startStore(t)
	cs := session(t, h)

	res := call(t, cs, "register_release", map[string]any{"initiative": "hits", "slug": "0.5", "name": "The 0.5 release"})
	if res.IsError {
		t.Fatalf("register_release: %s", resultText(res))
	}
	if r := decode[contract.Release](t, res); r.Slug != "0.5" || r.Initiative != "hits" {
		t.Errorf("registered release = %+v", r)
	}

	item := callItem(t, cs, "create_item", map[string]any{"type": "bug", "report": "aims at 0.5", "target": "0.5"})
	if item.Target != "0.5" {
		t.Errorf("created target = %q", item.Target)
	}

	res = call(t, cs, "ship_release", map[string]any{
		"initiative": "hits", "slug": "0.5",
		"refs": []map[string]any{{"tag": "v0.5.0"}},
	})
	if !res.IsError || !strings.Contains(resultText(res), "open-targets") {
		t.Fatalf("ship with straggler = %v (%s), want the open-targets refusal", res.IsError, resultText(res))
	}

	item = callItem(t, cs, "edit_item", map[string]any{"id": item.ID, "target": ""})
	if item.Target != "" {
		t.Errorf("cleared target = %q", item.Target)
	}

	res = call(t, cs, "ship_release", map[string]any{
		"initiative": "hits", "slug": "0.5",
		"refs": []map[string]any{{"tag": "v0.5.0", "note": "the evidence"}},
		"note": "cut clean",
	})
	if res.IsError {
		t.Fatalf("ship_release: %s", resultText(res))
	}
	if r := decode[contract.Release](t, res); !r.Shipped || len(r.ShipRefs) != 1 || r.ShipRefs[0].Tag != "v0.5.0" {
		t.Errorf("shipped release = %+v", r)
	}

	res = call(t, cs, "list_releases", map[string]any{"initiative": "hits"})
	if res.IsError {
		t.Fatalf("list_releases: %s", resultText(res))
	}
	if rs := decode[[]contract.Release](t, res); len(rs) != 1 || !rs[0].Shipped {
		t.Errorf("release list = %+v", rs)
	}

	res = call(t, cs, "retire_release", map[string]any{"initiative": "hits", "slug": "0.5", "reason": "no"})
	if !res.IsError || !strings.Contains(resultText(res), "release-shipped") {
		t.Errorf("retire shipped = %v (%s), want release-shipped", res.IsError, resultText(res))
	}
}

// untilOK polls a derived read — the indexes trail the history — until it
// holds or the deadline passes.
func untilOK(t *testing.T, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatalf("%s: never held", what)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

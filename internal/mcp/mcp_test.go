package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"hash/fnv"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nats-io/nats.go"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/contract"
	"github.com/impire-io/hits/internal/index/graph"
	"github.com/impire-io/hits/internal/index/search"
	"github.com/impire-io/hits/internal/index/semantic"
	"github.com/impire-io/hits/internal/mcp"
	"github.com/impire-io/hits/internal/natstest"
	"github.com/impire-io/hits/internal/node"
)

// harness is an embedded NATS server with the hits node on it; the index
// services join per test. Tools run against the real wire, never a mock.
type harness struct {
	url     string
	svcConn *nats.Conn
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

	return &harness{url: url, svcConn: svcConn}
}

// session connects an in-process MCP client to a server built on a real
// connection to the harness — the same server production runs over stdio.
func session(t *testing.T, h *harness, actor string) *sdk.ClientSession {
	t.Helper()
	nc, err := nats.Connect(h.url)
	if err != nil {
		t.Fatalf("connect mcp side: %v", err)
	}
	t.Cleanup(nc.Close)

	srv := mcp.NewServer(client.New(nc), actor)
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
	return func(string) (*nats.Conn, error) {
		t.Error("connector called: boot should fail before connecting")
		return nil, errors.New("no connection in this test")
	}
}

// TestToolList pins the surface: exactly the design's eighteen tools, the
// six query tools read-only.
func TestToolList(t *testing.T) {
	h := startStore(t)
	cs := session(t, h, "daan")

	res, err := cs.ListTools(testCtx(t), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	want := []string{
		"block_item", "claim_item", "create_item", "edit_item", "get_item",
		"graph_neighbors", "graph_walk", "link_items", "list_projects",
		"note_item", "register_project", "release_item", "search_items",
		"semantic_search", "tombstone_item", "transition_item",
		"unblock_item", "unlink_items",
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

	wantReadOnly := []string{"get_item", "graph_neighbors", "graph_walk", "list_projects", "search_items", "semantic_search"}
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
	cs := session(t, h, "daan")

	res := call(t, cs, "register_project", map[string]any{"slug": "hits", "name": "HITS repo"})
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
		item.Reporter != "daan" || item.DiscoveredWhile != "building hits-mcp" {
		t.Errorf("created item = %+v", item)
	}

	if got := callItem(t, cs, "get_item", map[string]any{"id": id}); got.Report != item.Report {
		t.Errorf("get_item report = %q, want %q", got.Report, item.Report)
	}

	item = callItem(t, cs, "claim_item", map[string]any{"id": id})
	if item.Claim == nil || item.Claim.By != "daan" {
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
	cs := session(t, h, "daan")

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

// TestInvariantErrorVerbatim proves rejections reach the agent as isError
// results carrying the machine-legible code unchanged.
func TestInvariantErrorVerbatim(t *testing.T) {
	h := startStore(t)
	cs := session(t, h, "daan")

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

// fakeProvider is an OpenAI-API-compatible embedding endpoint producing
// deterministic bag-of-words vectors, as in the client package's tests.
func fakeProvider(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": bagOfWords(req.Input)}},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func bagOfWords(text string) []float32 {
	const dims = 64
	vec := make([]float32, dims)
	for _, word := range strings.Fields(strings.ToLower(text)) {
		h := fnv.New32a()
		_, _ = h.Write([]byte(word))
		vec[h.Sum32()%dims]++
	}
	var norm float64
	for _, v := range vec {
		norm += float64(v) * float64(v)
	}
	if norm > 0 {
		n := float32(math.Sqrt(norm))
		for i := range vec {
			vec[i] /= n
		}
	}
	return vec
}

// TestQueryTools proves the four query tools find what the item tools
// created, against the real index services.
func TestQueryTools(t *testing.T) {
	h := startStore(t)
	cs := session(t, h, "daan")

	call(t, cs, "register_project", map[string]any{"slug": "hits", "name": "HITS repo"})
	match := callItem(t, cs, "create_item", map[string]any{
		"type": "bug", "report": "auth login loop keeps repeating", "located-in": []string{"hits"},
	}).ID
	callItem(t, cs, "create_item", map[string]any{
		"type": "bug", "report": "the build cache misses every time",
	})
	callItem(t, cs, "claim_item", map[string]any{"id": match})

	ctx := testCtx(t)
	searchSvc, err := search.Start(ctx, h.svcConn)
	if err != nil {
		t.Fatalf("start search: %v", err)
	}
	t.Cleanup(searchSvc.Stop)
	graphSvc, err := graph.Start(ctx, h.svcConn)
	if err != nil {
		t.Fatalf("start graph: %v", err)
	}
	t.Cleanup(graphSvc.Stop)
	provider := fakeProvider(t)
	semanticSvc, err := semantic.Start(ctx, h.svcConn, semantic.Config{
		BaseURL: provider.URL, APIKey: "test-key", Model: "fake-model",
	})
	if err != nil {
		t.Fatalf("start semantic: %v", err)
	}
	t.Cleanup(semanticSvc.Stop)

	res := call(t, cs, "search_items", map[string]any{"query": "login"})
	if res.IsError {
		t.Fatalf("search_items: %s", resultText(res))
	}
	searchReply := decode[client.SearchReply](t, res)
	if searchReply.Total != 1 || len(searchReply.Hits) != 1 || searchReply.Hits[0].ID != match {
		t.Errorf("search reply = %+v, want just %s", searchReply, match)
	}

	res = call(t, cs, "semantic_search", map[string]any{"text": "auth login loop", "limit": 1})
	if res.IsError {
		t.Fatalf("semantic_search: %s", resultText(res))
	}
	semReply := decode[client.SemanticReply](t, res)
	if len(semReply.Hits) != 1 || semReply.Hits[0].ID != match {
		t.Errorf("semantic reply = %+v, want %s first", semReply, match)
	}

	res = call(t, cs, "graph_neighbors", map[string]any{"kind": "item", "id": match})
	if res.IsError {
		t.Fatalf("graph_neighbors: %s", resultText(res))
	}
	edges := decode[client.NeighborsReply](t, res).Edges
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
		hasActor = hasActor || (n.Kind == client.NodeActor && n.ID == "daan")
		hasProjectNode = hasProjectNode || (n.Kind == client.NodeProject && n.ID == "hits")
	}
	if !hasActor || !hasProjectNode {
		t.Errorf("walk nodes = %+v, want actor daan and project hits reached", walk.Nodes)
	}
}

// TestRunFailsFast proves every boot precondition refuses to serve.
func TestRunFailsFast(t *testing.T) {
	var errOut strings.Builder

	t.Setenv("HITS_ACTOR", "")
	err := mcp.Run(testCtx(t), nil, &errOut, guardConnector(t))
	if err == nil || !strings.Contains(err.Error(), "actor") {
		t.Errorf("no actor: err = %v, want it named", err)
	}

	err = mcp.Run(testCtx(t), []string{"--actor", "Not Valid!"}, &errOut, guardConnector(t))
	if err == nil || !strings.Contains(err.Error(), "well-formed") {
		t.Errorf("bad actor: err = %v, want a form error", err)
	}

	err = mcp.Run(testCtx(t), []string{"--actor", "daan"}, &errOut, func(string) (*nats.Conn, error) {
		return nil, errors.New("nats context missing")
	})
	if err == nil || !strings.Contains(err.Error(), "connect") {
		t.Errorf("connect failure: err = %v, want connect named", err)
	}

	url := natstest.StartJetStream(t) // NATS up, but no hits service on it
	err = mcp.Run(testCtx(t), []string{"--actor", "daan"}, &errOut, func(string) (*nats.Conn, error) {
		return nats.Connect(url)
	})
	if err == nil || !strings.Contains(err.Error(), "ping") {
		t.Errorf("no fleet: err = %v, want the failed ping named", err)
	}
}

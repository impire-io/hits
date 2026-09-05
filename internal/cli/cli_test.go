package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"hash/fnv"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/impire-io/hits/contract"
	"github.com/impire-io/hits/internal/cli"
	"github.com/impire-io/hits/internal/index/graph"
	"github.com/impire-io/hits/internal/index/search"
	"github.com/impire-io/hits/internal/index/semantic"
	"github.com/impire-io/hits/internal/natstest"
	"github.com/impire-io/hits/internal/node"
	"github.com/impire-io/hits/internal/version"
)

// harness is an embedded NATS server with the hits node on it; the index
// services join per test. Commands run against the real wire, never a mock.
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

func (h *harness) connector() cli.Connector {
	return func(string) (*nats.Conn, error) { return nats.Connect(h.url) }
}

// guardConnector fails the test the moment a command touches the wire — for
// invocations that must be rejected during argument validation.
func guardConnector(t *testing.T) cli.Connector {
	return func(string) (*nats.Conn, error) {
		t.Error("connector called: command should fail before connecting")
		return nil, errors.New("no connection in this test")
	}
}

func testCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// run executes one CLI invocation, failing the test on error.
func run(t *testing.T, connect cli.Connector, args ...string) string {
	t.Helper()
	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), args, &out, &errOut, connect); err != nil {
		t.Fatalf("run %v: %v (stderr: %s)", args, err, errOut.String())
	}
	return out.String()
}

// runErr executes one CLI invocation expected to fail, returning the error.
func runErr(t *testing.T, connect cli.Connector, args ...string) error {
	t.Helper()
	var out, errOut bytes.Buffer
	err := cli.Run(context.Background(), args, &out, &errOut, connect)
	if err == nil {
		t.Fatalf("run %v: want error, got output %q", args, out.String())
	}
	return err
}

// itemID reads the item ID off a printed snapshot's head line.
func itemID(t *testing.T, out string) string {
	t.Helper()
	fields := strings.Fields(strings.SplitN(out, "\n", 2)[0])
	if len(fields) == 0 {
		t.Fatalf("no item ID in output %q", out)
	}
	return fields[0]
}

func wantContains(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("output missing %q:\n%s", w, out)
		}
	}
}

func TestPingCommand(t *testing.T) {
	h := startStore(t)

	out := run(t, h.connector(), "ping")
	if want := "hits " + version.Version + "\n"; out != want {
		t.Errorf("ping output = %q, want %q", out, want)
	}
}

func TestVersionCommand(t *testing.T) {
	out := run(t, nil, "version")
	if want := version.Version + "\n"; out != want {
		t.Errorf("version output = %q, want %q", out, want)
	}
}

func TestUnknownCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	err := cli.Run(context.Background(), []string{"bogus"}, &out, &errOut, nil)
	if err == nil {
		t.Fatal("unknown command: want error, got nil")
	}
	if !strings.Contains(errOut.String(), "Usage:") {
		t.Errorf("unknown command should print usage, stderr = %q", errOut.String())
	}
}

// TestItemLifecycle drives one item through the whole lifecycle command by
// command, asserting on the printed snapshots.
func TestItemLifecycle(t *testing.T) {
	h := startStore(t)
	connect := h.connector()
	t.Setenv("HITS_ACTOR", "daan")

	out := run(t, connect, "project", "register", "hits", "HITS repo")
	wantContains(t, out, "hits", "HITS repo")

	out = run(t, connect, "create",
		"--type", "bug", "--priority", "high", "--project", "hits",
		"--discovered-while", "building the CLI",
		"the CLI panics on empty input")
	wantContains(t, out, "bug", "open", "high",
		"report: the CLI panics on empty input",
		"reporter: daan",
		"located-in: hits",
		"discovered-while: building the CLI")
	id := itemID(t, out)

	out = run(t, connect, "get", id)
	wantContains(t, out, id, "the CLI panics on empty input")

	out = run(t, connect, "claim", id)
	wantContains(t, out, "claimed-by: daan")

	out = run(t, connect, "release", id)
	if strings.Contains(out, "claimed-by") {
		t.Errorf("released item still shows a claim:\n%s", out)
	}

	run(t, connect, "claim", id)

	out = run(t, connect, "note", id, "reproduced on main")
	wantContains(t, out, "notes:", "reproduced on main")

	out = run(t, connect, "block", id, "--by", "waiting on upstream fix")
	wantContains(t, out, "blocked", "blocked-by: waiting on upstream fix", "interrupted: open")

	out = run(t, connect, "unblock", id)
	wantContains(t, out, "open")
	if strings.Contains(out, "blocked-by") {
		t.Errorf("unblocked item still shows a block:\n%s", out)
	}

	out = run(t, connect, "transition", id, "--to", "diagnosing")
	wantContains(t, out, "diagnosing")

	out = run(t, connect, "transition", id, "--to", "located")
	wantContains(t, out, "located")

	out = run(t, connect, "resolve", id,
		"--fixed-by", "commit:abc123 folded the fix",
		"--amended-design", "02-DESIGN/services.md")
	wantContains(t, out, "resolved",
		"fixed-by: commit:abc123 — folded the fix",
		"amended-design: 02-DESIGN/services.md",
		"closed: ")
}

// TestWontfixCommand closes an item through the other sugar verb.
func TestWontfixCommand(t *testing.T) {
	h := startStore(t)
	connect := h.connector()
	t.Setenv("HITS_ACTOR", "daan")

	id := itemID(t, run(t, connect, "create", "--type", "bug", "cosmetic misalignment"))
	out := run(t, connect, "wontfix", id)
	wantContains(t, out, "wontfix", "closed: ")
}

func TestLinkEditTombstone(t *testing.T) {
	h := startStore(t)
	connect := h.connector()
	t.Setenv("HITS_ACTOR", "daan")

	a := itemID(t, run(t, connect, "create", "--type", "bug", "the parser crashes"))
	b := itemID(t, run(t, connect, "create", "--type", "bug", "parser crash on unicode"))

	out := run(t, connect, "link", a, "--type", "duplicates", b)
	wantContains(t, out, "link: duplicates "+b)

	out = run(t, connect, "unlink", a, "--type", "duplicates", b)
	if strings.Contains(out, "link: duplicates") {
		t.Errorf("unlinked item still shows the link:\n%s", out)
	}

	out = run(t, connect, "edit", a, "--priority", "low")
	wantContains(t, out, "low")

	out = run(t, connect, "tombstone", b, "filed twice")
	wantContains(t, out, "tombstoned: filed twice")
}

func TestWriteNeedsActor(t *testing.T) {
	// An empty config home keeps the machine's real defaults.actor from
	// satisfying the actor check.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HITS_ACTOR", "")
	err := runErr(t, guardConnector(t), "create", "--type", "bug", "some report")
	if !strings.Contains(err.Error(), "actor") {
		t.Errorf("error should name the missing actor, got %v", err)
	}
}

func TestActorFlag(t *testing.T) {
	h := startStore(t)
	t.Setenv("HITS_ACTOR", "")

	out := run(t, h.connector(), "--actor", "daan", "create", "--type", "bug", "some report")
	wantContains(t, out, "reporter: daan")
}

func TestAPIErrorCodePassesThrough(t *testing.T) {
	h := startStore(t)
	t.Setenv("HITS_ACTOR", "daan")

	err := runErr(t, h.connector(), "claim", "no-such-item")
	if !strings.Contains(err.Error(), "not-found") {
		t.Errorf("service rejection should carry its code, got %v", err)
	}
}

func TestBadFixedByFailsBeforeConnecting(t *testing.T) {
	t.Setenv("HITS_ACTOR", "daan")
	err := runErr(t, guardConnector(t), "transition", "some-id", "--to", "resolved", "--fixed-by", "nonsense")
	if !strings.Contains(err.Error(), "bad --fixed-by") {
		t.Errorf("want a --fixed-by parse error, got %v", err)
	}
}

func TestMissingIDArgument(t *testing.T) {
	err := runErr(t, guardConnector(t), "claim")
	if !strings.Contains(err.Error(), "missing <id>") {
		t.Errorf("want a missing-argument error, got %v", err)
	}
}

func TestJSONOutput(t *testing.T) {
	h := startStore(t)
	connect := h.connector()
	t.Setenv("HITS_ACTOR", "daan")

	id := itemID(t, run(t, connect, "create", "--type", "bug", "json roundtrip"))

	out := run(t, connect, "--json", "get", id)
	var item contract.Item
	if err := json.Unmarshal([]byte(out), &item); err != nil {
		t.Fatalf("--json output is not JSON: %v\n%s", err, out)
	}
	if item.ID != id || item.Report != "json roundtrip" {
		t.Errorf("decoded item = %+v, want ID %s", item, id)
	}
}

func TestSearchCommand(t *testing.T) {
	h := startStore(t)
	connect := h.connector()
	t.Setenv("HITS_ACTOR", "daan")

	match := itemID(t, run(t, connect, "create", "--type", "bug", "the parser crashes on unicode"))
	run(t, connect, "create", "--type", "bug", "docs typo in the readme")

	svc, err := search.Start(testCtx(t), h.svcConn)
	if err != nil {
		t.Fatalf("start search: %v", err)
	}
	t.Cleanup(svc.Stop)

	out := run(t, connect, "search", "parser")
	wantContains(t, out, match, "total: 1")
	if strings.Contains(out, "docs typo") {
		t.Errorf("search %q should not hit the other item:\n%s", "parser", out)
	}

	// the hit renders as a table row: populated columns show, carrying the
	// snapshot's fields; a column empty in every row is dropped.
	wantContains(t, out, "id", "score", "status", "report", "the parser crashes on unicode")
	if strings.Contains(out, "closed") {
		t.Errorf("empty-everywhere column should be dropped:\n%s", out)
	}
}

func TestSearchColumns(t *testing.T) {
	h := startStore(t)
	connect := h.connector()
	t.Setenv("HITS_ACTOR", "daan")

	id := itemID(t, run(t, connect, "create", "--type", "bug", "column selection fodder"))
	run(t, connect, "claim", id)

	svc, err := search.Start(testCtx(t), h.svcConn)
	if err != nil {
		t.Fatalf("start search: %v", err)
	}
	t.Cleanup(svc.Stop)

	// --columns names the columns exactly, in the given order — repeatable
	// and comma-separated, and a column empty in every row still shows when
	// asked for.
	out := run(t, connect, "search", "fodder", "--columns", "id,claimed-by", "--columns", "closed")
	wantContains(t, out, "claimed-by", "closed", "daan")
	if strings.Contains(out, "report") {
		t.Errorf("--columns should render only the named columns:\n%s", out)
	}
	header := strings.SplitN(out, "\n", 2)[0]
	if strings.Index(header, "id") >= strings.Index(header, "claimed-by") ||
		strings.Index(header, "claimed-by") >= strings.Index(header, "closed") {
		t.Errorf("columns out of order: %q", header)
	}
}

func TestSearchUnknownColumn(t *testing.T) {
	err := runErr(t, guardConnector(t), "search", "--columns", "nope")
	if !strings.Contains(err.Error(), `unknown column "nope"`) {
		t.Errorf("want an unknown-column error, got %v", err)
	}
}

func TestSearchJSON(t *testing.T) {
	h := startStore(t)
	connect := h.connector()
	t.Setenv("HITS_ACTOR", "daan")

	id := itemID(t, run(t, connect, "create", "--type", "bug", "enriched json fodder"))

	svc, err := search.Start(testCtx(t), h.svcConn)
	if err != nil {
		t.Fatalf("start search: %v", err)
	}
	t.Cleanup(svc.Stop)

	out := run(t, connect, "--json", "search", "enriched")
	var reply struct {
		Hits []struct {
			ID    string         `json:"id"`
			Score float64        `json:"score"`
			Item  *contract.Item `json:"item"`
		} `json:"hits"`
		Total uint64 `json:"total"`
	}
	if err := json.Unmarshal([]byte(out), &reply); err != nil {
		t.Fatalf("--json search output is not JSON: %v\n%s", err, out)
	}
	if reply.Total != 1 || len(reply.Hits) != 1 {
		t.Fatalf("want exactly one hit, got:\n%s", out)
	}
	hit := reply.Hits[0]
	if hit.ID != id || hit.Item == nil || hit.Item.Report != "enriched json fodder" {
		t.Errorf("hit = %+v, want item %s carrying its snapshot", hit, id)
	}
}

func TestGraphCommand(t *testing.T) {
	h := startStore(t)
	connect := h.connector()
	t.Setenv("HITS_ACTOR", "daan")

	run(t, connect, "project", "register", "hits", "HITS repo")
	id := itemID(t, run(t, connect, "create", "--type", "bug", "--project", "hits", "graph fodder"))
	run(t, connect, "claim", id)

	svc, err := graph.Start(testCtx(t), h.svcConn)
	if err != nil {
		t.Fatalf("start graph: %v", err)
	}
	t.Cleanup(svc.Stop)

	out := run(t, connect, "graph", "neighbors", id)
	wantContains(t, out, "-[located-in]->", "project:hits", "actor:daan")

	out = run(t, connect, "graph", "walk", id)
	wantContains(t, out, "nodes:", "edges:", "item:"+id)
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

func TestSemanticCommand(t *testing.T) {
	h := startStore(t)
	connect := h.connector()
	t.Setenv("HITS_ACTOR", "daan")

	match := itemID(t, run(t, connect, "create", "--type", "bug", "auth login loop keeps repeating"))
	itemID(t, run(t, connect, "create", "--type", "bug", "the build cache misses every time"))

	provider := fakeProvider(t)
	svc, err := semantic.Start(testCtx(t), h.svcConn, semantic.Config{
		BaseURL: provider.URL, APIKey: "test-key", Model: "fake-model",
	})
	if err != nil {
		t.Fatalf("start semantic: %v", err)
	}
	t.Cleanup(svc.Stop)

	out := run(t, connect, "semantic", "auth login loop", "--limit", "1")
	wantContains(t, out, match)
}

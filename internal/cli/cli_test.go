package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	chron "github.com/impire-io/chronicle/client"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/client/chronicle"
	"github.com/impire-io/hits/contract"
	"github.com/impire-io/hits/internal/chronicletest"
	"github.com/impire-io/hits/internal/cli"
	"github.com/impire-io/hits/internal/version"
)

// harness is chronicle's quick start with a HITS declared in it; commands
// run against the real wire, never a mock.
type harness struct {
	cc *chron.Client
}

func startStore(t *testing.T) *harness {
	t.Helper()
	return &harness{cc: chronicletest.Start(t)}
}

func (h *harness) connector() cli.Connector {
	return func(string) (client.Client, func(), error) {
		return chronicle.New(h.cc, chronicletest.Store), func() {}, nil
	}
}

// guardConnector fails the test the moment a command touches the wire — for
// invocations that must be rejected during argument validation.
func guardConnector(t *testing.T) cli.Connector {
	return func(string) (client.Client, func(), error) {
		t.Error("connector called: command should fail before connecting")
		return nil, nil, errors.New("no connection in this test")
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

	out := run(t, connect, "project", "register", "hits", "HITS repo", "--initiative", "hits")
	wantContains(t, out, "hits", "HITS repo")

	out = run(t, connect, "create",
		"--type", "bug", "--priority", "high", "--project", "hits",
		"--discovered-while", "building the CLI",
		"the CLI panics on empty input")
	wantContains(t, out, "bug", "open", "high",
		"report: the CLI panics on empty input",
		"reporter: admin",
		"located-in: hits",
		"discovered-while: building the CLI")
	id := itemID(t, out)

	out = run(t, connect, "get", id)
	wantContains(t, out, id, "the CLI panics on empty input")

	out = run(t, connect, "claim", id)
	wantContains(t, out, "claimed-by: admin")

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

// TestProjectRetire retires a slug end to end: the listing drops it, new
// located-in references are refused, the slug is never re-registered, and
// an item that already named it still closes.
func TestProjectRetire(t *testing.T) {
	h := startStore(t)
	connect := h.connector()

	run(t, connect, "project", "register", "001-hits", "HITS", "--initiative", "hits")
	run(t, connect, "project", "register", "hits", "HITS repo", "--initiative", "hits")
	id := itemID(t, run(t, connect, "create", "--type", "task", "--project", "001-hits", "cutover leftovers"))

	out := run(t, connect, "project", "retire", "001-hits", "--reason", "setup validation artifact")
	wantContains(t, out, "001-hits", "retired: setup validation artifact")

	out = run(t, connect, "project", "list")
	if strings.Contains(out, "001-hits") {
		t.Errorf("retired slug still listed:\n%s", out)
	}
	wantContains(t, out, "hits")

	// The vocabulary refuses the slug from now on, in every direction.
	if err := runErr(t, connect, "create", "--type", "task", "--project", "001-hits", "late reference"); !strings.Contains(err.Error(), "retired-project") {
		t.Errorf("create against retired slug = %v, want retired-project", err)
	}
	if err := runErr(t, connect, "project", "register", "001-hits", "HITS again", "--initiative", "hits"); !strings.Contains(err.Error(), "slug-retired") {
		t.Errorf("re-register retired slug = %v, want slug-retired", err)
	}
	if err := runErr(t, connect, "project", "retire", "001-hits", "--reason", "again"); !strings.Contains(err.Error(), "already-retired") {
		t.Errorf("double retire = %v, want already-retired", err)
	}

	// History stands: the item that named the slug before retirement closes.
	out = run(t, connect, "resolve", id, "--fixed-by", "commit:abc123 done")
	wantContains(t, out, "resolved")

	// A missing reason is rejected during argument validation, pre-dial.
	if err := runErr(t, guardConnector(t), "project", "retire", "001-hits"); !strings.Contains(err.Error(), "--reason is required") {
		t.Errorf("missing reason = %v", err)
	}
}

// TestWontfixCommand closes an item through the other sugar verb.
func TestWontfixCommand(t *testing.T) {
	h := startStore(t)
	connect := h.connector()

	id := itemID(t, run(t, connect, "create", "--type", "bug", "cosmetic misalignment"))
	out := run(t, connect, "wontfix", id)
	wantContains(t, out, "wontfix", "closed: ")
}

func TestLinkEditTombstone(t *testing.T) {
	h := startStore(t)
	connect := h.connector()

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

func TestAPIErrorCodePassesThrough(t *testing.T) {
	h := startStore(t)

	err := runErr(t, h.connector(), "claim", "no-such-item")
	if !strings.Contains(err.Error(), "not-found") {
		t.Errorf("service rejection should carry its code, got %v", err)
	}
}

func TestBadFixedByFailsBeforeConnecting(t *testing.T) {
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

	match := itemID(t, run(t, connect, "create", "--type", "bug", "the parser crashes on unicode"))
	run(t, connect, "create", "--type", "bug", "docs typo in the readme")

	waitSearch(t, h, "parser", 1)
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

	id := itemID(t, run(t, connect, "create", "--type", "bug", "column selection fodder"))
	run(t, connect, "claim", id)

	// --columns names the columns exactly, in the given order — repeatable
	// and comma-separated, and a column empty in every row still shows when
	// asked for.
	waitSearch(t, h, "fodder", 1)
	out := run(t, connect, "search", "fodder", "--columns", "id,claimed-by", "--columns", "closed")
	wantContains(t, out, "claimed-by", "closed", "admin")
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

func TestSearchFanIsConfigurable(t *testing.T) {
	h := startStore(t)
	connect := h.connector()

	run(t, connect, "create", "--type", "bug", "fan fodder one")
	run(t, connect, "create", "--type", "bug", "fan fodder two")

	waitSearch(t, h, "fodder", 2)
	out := run(t, connect, "search", "fodder", "--fan", "1")
	wantContains(t, out, "fan fodder one", "fan fodder two", "total: 2")

	if err := runErr(t, guardConnector(t), "search", "--fan", "0"); !strings.Contains(err.Error(), "--fan 0: want at least 1") {
		t.Errorf("zero fan: %v", err)
	}
}

func TestSearchJSON(t *testing.T) {
	h := startStore(t)
	connect := h.connector()

	id := itemID(t, run(t, connect, "create", "--type", "bug", "enriched json fodder"))

	waitSearch(t, h, "enriched", 1)
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

	run(t, connect, "project", "register", "hits", "HITS repo", "--initiative", "hits")
	id := itemID(t, run(t, connect, "create", "--type", "bug", "--project", "hits", "graph fodder"))
	run(t, connect, "claim", id)

	waitEdges(t, h, id, 4)
	out := run(t, connect, "graph", "neighbors", id)
	wantContains(t, out, "-[located-in]->", "project:hits", "actor:admin")

	out = run(t, connect, "graph", "walk", id)
	wantContains(t, out, "nodes:", "edges:", "item:"+id)
}

// TestInitiativeCommands drives the initiative verbs: register, list,
// select (verified against the live registry, stored as the filing
// default), and create's flag → env → selected-default resolution
// (decision 0016).
func TestInitiativeCommands(t *testing.T) {
	h := startStore(t)
	connect := h.connector()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HITS_INITIATIVE", "") // fall through to the selected default

	out := run(t, connect, "initiative", "register", "pra", "PRA effort", "--description", "a second effort")
	wantContains(t, out, "pra", "PRA effort", "a second effort")

	out = run(t, connect, "initiative", "list")
	wantContains(t, out, "pra", "hits")

	if err := runErr(t, connect, "initiative", "select", "nope"); !strings.Contains(err.Error(), "not a registered initiative") {
		t.Errorf("select unknown: %v", err)
	}
	out = run(t, connect, "initiative", "select", "pra")
	wantContains(t, out, "selected initiative: pra")

	// The selected default feeds the mint; the flag outranks it.
	id := itemID(t, run(t, connect, "create", "--type", "bug", "selected default works"))
	if id != "pra-1" {
		t.Errorf("selected-default mint = %q, want pra-1", id)
	}
	id = itemID(t, run(t, connect, "create", "--type", "bug", "--initiative", "hits", "the flag outranks"))
	if id != "hits-1" {
		t.Errorf("flag mint = %q, want hits-1", id)
	}

	// Selection is a filing default, not a read scope: a flagless search
	// still reaches the whole corpus (asserted here without the index —
	// get resolves both items).
	wantContains(t, run(t, connect, "get", "pra-1"), "selected default works")
	wantContains(t, run(t, connect, "get", "hits-1"), "the flag outranks")

	out = run(t, connect, "initiative", "retire", "pra", "--reason", "wound down")
	wantContains(t, out, "retired: wound down")
	if err := runErr(t, connect, "create", "--type", "bug", "too late"); !strings.Contains(err.Error(), "retired-initiative") {
		t.Errorf("create into retired initiative: %v", err)
	}
}

// TestCreateRequiresInitiative: with no flag, no env, and no selected
// default, the create fails before dialing.
func TestCreateRequiresInitiative(t *testing.T) {
	t.Setenv("HITS_INITIATIVE", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	err := runErr(t, guardConnector(t), "create", "--type", "bug", "no initiative anywhere")
	if !strings.Contains(err.Error(), "no initiative") {
		t.Errorf("want the no-initiative refusal, got %v", err)
	}
}

// waitSearch polls the text index — it trails the history — until the
// query finds the expected number of items.
func waitSearch(t *testing.T, h *harness, query string, total uint64) {
	t.Helper()
	c := chronicle.New(h.cc, chronicletest.Store)
	deadline := time.Now().Add(20 * time.Second)
	for {
		r, err := c.SearchItems(testCtx(t), client.SearchRequest{Query: query})
		if err == nil && r.Total == total {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("search %q never found %d items: %+v %v", query, total, r, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// waitEdges polls the graph index until the item has at least n edges.
func waitEdges(t *testing.T, h *harness, id string, n int) {
	t.Helper()
	c := chronicle.New(h.cc, chronicletest.Store)
	deadline := time.Now().Add(20 * time.Second)
	for {
		r, err := c.GraphNeighbors(testCtx(t), client.NeighborsRequest{Kind: client.NodeItem, ID: id})
		if err == nil && len(r.Edges) >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("item %s never had %d edges: %+v %v", id, n, r, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

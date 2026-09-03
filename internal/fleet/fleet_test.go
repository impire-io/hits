package fleet_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"hash/fnv"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/contract"
	"github.com/impire-io/hits/internal/fleet"
	"github.com/impire-io/hits/internal/index/semantic"
	"github.com/impire-io/hits/internal/natstest"
	"github.com/impire-io/hits/internal/node"
)

func testCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// connector dials the embedded server, honoring the per-service nats.Name
// exactly as the production ContextConnector does.
func connector(url string) fleet.Connector {
	return func(natsName string) (*nats.Conn, error) {
		return nats.Connect(url, nats.Name(natsName))
	}
}

// eventually polls for a condition the fleet reaches asynchronously — the
// index folds trail the write path by design.
func eventually(t *testing.T, what string, cond func() bool) {
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

// probe opens the client-side connection the assertions speak through.
func probe(t *testing.T, url string) (*nats.Conn, *client.Client) {
	t.Helper()
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect probe: %v", err)
	}
	t.Cleanup(nc.Close)
	return nc, client.New(nc)
}

// TestStartRunsTheFleet proves one Start yields all four services on the
// wire: an item created through the client is found by search, graph, and
// semantic query.
func TestStartRunsTheFleet(t *testing.T) {
	url := natstest.StartJetStream(t)
	provider := fakeProvider(t)
	ctx := testCtx(t)

	f, err := fleet.Start(ctx, connector(url), fleet.Config{Semantic: semantic.Config{
		BaseURL: provider.URL, APIKey: "test-key", Model: "fake-model",
	}})
	if err != nil {
		t.Fatalf("start fleet: %v", err)
	}
	t.Cleanup(f.Stop)

	want := []string{"hits-node", "hits-index-graph", "hits-index-search", "hits-index-semantic"}
	if !slices.Equal(f.Running, want) {
		t.Fatalf("running = %v, want %v", f.Running, want)
	}

	_, c := probe(t, url)
	if _, err := c.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if _, err := c.RegisterProject(ctx, client.RegisterProjectRequest{
		Actor: "daan", Slug: "hits", Name: "HITS repo",
	}); err != nil {
		t.Fatalf("register project: %v", err)
	}
	item, err := c.CreateItem(ctx, client.CreateItemRequest{
		Actor: "daan", Type: "bug", Report: "auth login loop keeps repeating",
		LocatedIn: []string{"hits"},
	})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}
	if _, err := c.CreateItem(ctx, client.CreateItemRequest{
		Actor: "daan", Type: "bug", Report: "the build cache misses every time",
	}); err != nil {
		t.Fatalf("create second item: %v", err)
	}

	eventually(t, "search to find the item", func() bool {
		reply, err := c.SearchItems(ctx, client.SearchRequest{Query: "login"})
		return err == nil && reply.Total == 1 && len(reply.Hits) == 1 && reply.Hits[0].ID == item.ID
	})
	eventually(t, "graph to hold the located-in edge", func() bool {
		reply, err := c.GraphNeighbors(ctx, client.NeighborsRequest{Kind: client.NodeItem, ID: item.ID})
		if err != nil {
			return false
		}
		for _, e := range reply.Edges {
			if e.Type == client.EdgeLocatedIn && e.To.ID == "hits" {
				return true
			}
		}
		return false
	})
	eventually(t, "semantic to find the item", func() bool {
		reply, err := c.SemanticSearch(ctx, client.SemanticRequest{Text: "auth login loop", Limit: 1})
		return err == nil && len(reply.Hits) == 1 && reply.Hits[0].ID == item.ID
	})
}

// TestStartWithoutEmbeddings proves the bare-bones boot: three services on
// the wire, semantic absent rather than broken.
func TestStartWithoutEmbeddings(t *testing.T) {
	url := natstest.StartJetStream(t)
	ctx := testCtx(t)

	f, err := fleet.Start(ctx, connector(url), fleet.Config{})
	if err != nil {
		t.Fatalf("start fleet: %v", err)
	}
	t.Cleanup(f.Stop)

	want := []string{"hits-node", "hits-index-graph", "hits-index-search"}
	if !slices.Equal(f.Running, want) {
		t.Fatalf("running = %v, want %v", f.Running, want)
	}

	nc, c := probe(t, url)
	if _, err := c.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if _, err := nc.Request(client.SemanticSubject, []byte("{}"), 500*time.Millisecond); !errors.Is(err, nats.ErrNoResponders) {
		t.Fatalf("semantic request error = %v, want no responders", err)
	}
}

// TestStartConnectRefused proves a refused connection surfaces as the
// Start error with no fleet returned.
func TestStartConnectRefused(t *testing.T) {
	refuse := errors.New("connection refused by test")
	f, err := fleet.Start(testCtx(t), func(string) (*nats.Conn, error) {
		return nil, refuse
	}, fleet.Config{})
	if !errors.Is(err, refuse) {
		t.Fatalf("start error = %v, want the refused connect", err)
	}
	if f != nil {
		t.Fatal("a failed Start must not return a fleet")
	}
}

// TestStartFailFast proves a service that cannot start takes the whole
// composition down: on a JetStream-less server the first service fails
// after the connection succeeded, and Start closes that connection on
// its way out.
func TestStartFailFast(t *testing.T) {
	url := natstest.Start(t) // no JetStream: hits-node cannot provision
	ctx := testCtx(t)

	var mu sync.Mutex
	var conns []*nats.Conn
	dial := connector(url)
	tracking := func(natsName string) (*nats.Conn, error) {
		nc, err := dial(natsName)
		if err == nil {
			mu.Lock()
			conns = append(conns, nc)
			mu.Unlock()
		}
		return nc, err
	}

	f, err := fleet.Start(ctx, tracking, fleet.Config{})
	if err == nil || !strings.Contains(err.Error(), "hits-node") {
		t.Fatalf("start error = %v, want hits-node failing to provision", err)
	}
	if f != nil {
		t.Fatal("a failed Start must not return a fleet")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(conns) != 1 {
		t.Fatalf("fleet opened %d connections, want the one shared connection", len(conns))
	}
	if !conns[0].IsClosed() {
		t.Error("the shared connection is still open after a failed Start")
	}
}

// TestStopStopsEverything proves Stop is total: no service answers and
// the one shared connection the fleet opened is closed.
func TestStopStopsEverything(t *testing.T) {
	url := natstest.StartJetStream(t)
	ctx := testCtx(t)

	var mu sync.Mutex
	var conns []*nats.Conn
	dial := connector(url)
	tracking := func(natsName string) (*nats.Conn, error) {
		nc, err := dial(natsName)
		if err == nil {
			mu.Lock()
			conns = append(conns, nc)
			mu.Unlock()
		}
		return nc, err
	}

	f, err := fleet.Start(ctx, tracking, fleet.Config{})
	if err != nil {
		t.Fatalf("start fleet: %v", err)
	}
	f.Stop()

	nc, _ := probe(t, url)
	eventually(t, "the node to be off the wire", func() bool {
		_, err := nc.Request(client.PingSubject, nil, 250*time.Millisecond)
		return errors.Is(err, nats.ErrNoResponders)
	})
	mu.Lock()
	defer mu.Unlock()
	if len(conns) != 1 {
		t.Fatalf("fleet opened %d connections, want the one shared connection (decision 0006)", len(conns))
	}
	if !conns[0].IsClosed() {
		t.Error("the shared connection is still open after Stop")
	}
}

// TestRunUpRefusesHalfConfiguredEmbeddings proves the usage error fires
// before anything dials.
func TestRunUpRefusesHalfConfiguredEmbeddings(t *testing.T) {
	guard := func(string) fleet.Connector {
		return func(string) (*nats.Conn, error) {
			t.Error("dialed despite invalid flags")
			return nil, errors.New("guard")
		}
	}
	for _, args := range [][]string{
		{"--embedding-url", "http://localhost:1"},
		{"--embedding-model", "some-model"},
	} {
		var out, errOut bytes.Buffer
		err := fleet.RunUp(testCtx(t), args, &out, &errOut, guard)
		if err == nil || !strings.Contains(err.Error(), "go together") {
			t.Errorf("RunUp(%v) error = %v, want the go-together usage error", args, err)
		}
	}
}

// TestRunUpNoticeLine proves the full up path: fleet started through RunUp,
// the semantic-off notice printed, and a clean exit on cancellation.
func TestRunUpNoticeLine(t *testing.T) {
	url := natstest.StartJetStream(t)
	ctx, cancel := context.WithCancel(testCtx(t))
	defer cancel()

	out := &syncBuffer{}
	done := make(chan error, 1)
	go func() {
		done <- fleet.RunUp(ctx, nil, out, out, func(string) fleet.Connector { return connector(url) })
	}()

	eventually(t, "the startup and notice lines", func() bool {
		s := out.String()
		return strings.Contains(s, "hits-node, hits-index-graph, hits-index-search serving on") &&
			strings.Contains(s, "semantic search is off — pass --embedding-url and --embedding-model")
	})
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunUp returned %v, want nil after cancellation", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RunUp did not return after cancellation")
	}
}

// syncBuffer keeps the race detector happy while RunUp writes from its own
// goroutine.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
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

// streamConfig reads one stream's live config through a probe connection.
func streamConfig(t *testing.T, js jetstream.JetStream, name string) jetstream.StreamConfig {
	t.Helper()
	s, err := js.Stream(testCtx(t), name)
	if err != nil {
		t.Fatalf("stream %s: %v", name, err)
	}
	return s.CachedInfo().Config
}

// TestStartOnMaxBytesRequiredAccount reproduces issue 003: an account that
// requires every stream config to declare max bytes — Synadia Cloud's
// shape — must boot the fleet on the default budgets, with the ops stream
// refusing (never trimming) at its cap.
func TestStartOnMaxBytesRequiredAccount(t *testing.T) {
	url := natstest.StartJetStreamMaxBytesRequired(t)
	ctx := testCtx(t)

	f, err := fleet.Start(ctx, connector(url), fleet.Config{})
	if err != nil {
		t.Fatalf("start fleet on a max-bytes-required account: %v", err)
	}
	t.Cleanup(f.Stop)

	nc, c := probe(t, url)
	if _, err := c.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	ops := streamConfig(t, js, contract.OpsStream)
	if ops.MaxBytes != node.DefaultMaxBytes {
		t.Errorf("ops stream budget = %d, want the default %d", ops.MaxBytes, int64(node.DefaultMaxBytes))
	}
	if ops.Discard != jetstream.DiscardNew {
		t.Errorf("ops stream discard = %v, want DiscardNew — full must refuse, never trim", ops.Discard)
	}
	if got := streamConfig(t, js, "KV_"+contract.ItemsBucket).MaxBytes; got != node.DefaultMaxBytes/4 {
		t.Errorf("items bucket budget = %d, want a quarter of the ops budget", got)
	}
	for _, bucket := range []string{contract.ProjectsBucket, contract.MetaBucket} {
		if got := streamConfig(t, js, "KV_"+bucket).MaxBytes; got != 8<<20 {
			t.Errorf("%s budget = %d, want 8 MiB", bucket, got)
		}
	}
}

// TestMaxBytesOverride proves the one knob reaches the stream and scales
// the items bucket with it.
func TestMaxBytesOverride(t *testing.T) {
	url := natstest.StartJetStreamMaxBytesRequired(t)
	ctx := testCtx(t)

	f, err := fleet.Start(ctx, connector(url), fleet.Config{MaxBytes: 64 << 20})
	if err != nil {
		t.Fatalf("start fleet with --max-bytes 64M: %v", err)
	}
	t.Cleanup(f.Stop)

	nc, _ := probe(t, url)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	if got := streamConfig(t, js, contract.OpsStream).MaxBytes; got != 64<<20 {
		t.Errorf("ops stream budget = %d, want the 64 MiB override", got)
	}
	if got := streamConfig(t, js, "KV_"+contract.ItemsBucket).MaxBytes; got != 16<<20 {
		t.Errorf("items bucket budget = %d, want 16 MiB (a quarter of the override)", got)
	}
}

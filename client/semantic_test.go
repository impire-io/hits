package client_test

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/contract"
	"github.com/impire-io/hits/internal/index/semantic"
)

// fakeProvider is an OpenAI-API-compatible embedding endpoint producing
// deterministic bag-of-words vectors, so similar texts get similar vectors
// with no external calls. Texts containing "unembeddable" get a 500 — the
// degradation case.
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
		if strings.Contains(req.Input, "unembeddable") {
			http.Error(w, "no vector for you", http.StatusInternalServerError)
			return
		}
		vec := bagOfWords(req.Input)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": vec}},
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

func startSemantic(t *testing.T, h *harness, providerURL string) *semantic.Service {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	svc, err := semantic.Start(ctx, h.svcConn, semantic.Config{
		BaseURL: providerURL, APIKey: "test-key", Model: "fake-model",
	})
	if err != nil {
		t.Fatalf("start semantic: %v", err)
	}
	t.Cleanup(svc.Stop)
	return svc
}

func firstHit(ctx context.Context, t *testing.T, h *harness, text string) string {
	t.Helper()
	reply, err := h.c.SemanticSearch(ctx, client.SemanticRequest{Text: text})
	if err != nil {
		t.Fatalf("semantic search %q: %v", text, err)
	}
	if len(reply.Hits) == 0 {
		return ""
	}
	return reply.Hits[0].ID
}

// TestSemanticRankingAndTombstone: live-tail embedding ranks the right item
// first, a note re-embeds its item, and a tombstone removes it.
func TestSemanticRankingAndTombstone(t *testing.T) {
	h := startStore(t)
	ctx := testCtx(t)
	provider := fakeProvider(t)
	startSemantic(t, h, provider.URL)

	a, err := h.c.CreateItem(ctx, client.CreateItemRequest{
		Actor: "daan", Type: contract.Bug, Report: "the projector lags behind the ops log",
	})
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	b, err := h.c.CreateItem(ctx, client.CreateItemRequest{
		Actor: "daan", Type: contract.Bug, Report: "billing invoice shows a wrong total",
	})
	if err != nil {
		t.Fatalf("create b: %v", err)
	}

	waitFor(t, "nearest item to the projector text", func() bool {
		return firstHit(ctx, t, h, "projector lags") == a.ID
	})
	if got := firstHit(ctx, t, h, "wrong invoice total"); got != b.ID {
		t.Fatalf("nearest to invoice text = %q, want %q", got, b.ID)
	}

	// A note changes the document: b becomes the flux-capacitor item.
	if _, err := h.c.NoteItem(ctx, client.NoteItemRequest{Actor: "claude", ID: b.ID, Text: "the flux capacitor was misaligned"}); err != nil {
		t.Fatalf("note: %v", err)
	}
	waitFor(t, "re-embedded note text to rank first", func() bool {
		return firstHit(ctx, t, h, "flux capacitor misaligned") == b.ID
	})

	if _, err := h.c.TombstoneItem(ctx, client.TombstoneItemRequest{Actor: "daan", ID: b.ID, Reason: "filed as a test"}); err != nil {
		t.Fatalf("tombstone: %v", err)
	}
	waitFor(t, "tombstoned item to leave the results", func() bool {
		reply, err := h.c.SemanticSearch(ctx, client.SemanticRequest{Text: "flux capacitor misaligned"})
		if err != nil {
			return false
		}
		for _, hit := range reply.Hits {
			if hit.ID == b.ID {
				return false
			}
		}
		return true
	})
}

// TestSemanticRebuildAndDegraded: a service started after the corpus exists
// embeds it before going on the wire, and an item whose embedding call
// fails degrades that item only.
func TestSemanticRebuildAndDegraded(t *testing.T) {
	h := startStore(t)
	ctx := testCtx(t)
	provider := fakeProvider(t)

	a, err := h.c.CreateItem(ctx, client.CreateItemRequest{
		Actor: "daan", Type: contract.Bug, Report: "database timeout on cold start",
	})
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	b, err := h.c.CreateItem(ctx, client.CreateItemRequest{
		Actor: "daan", Type: contract.Bug, Report: "this report is unembeddable on purpose",
	})
	if err != nil {
		t.Fatalf("create b: %v", err)
	}

	startSemantic(t, h, provider.URL) // rebuild happens before the wire

	reply, err := h.c.SemanticSearch(ctx, client.SemanticRequest{Text: "database timeout"})
	if err != nil {
		t.Fatalf("semantic search: %v", err)
	}
	if len(reply.Hits) != 1 || reply.Hits[0].ID != a.ID {
		t.Fatalf("hits = %+v, want only %s — the unembeddable item %s degrades alone", reply.Hits, a.ID, b.ID)
	}
}

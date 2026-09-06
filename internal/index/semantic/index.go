// Package semantic runs the hits-semantic micro service: embedding search
// over item reports and notes, a projection of the ops-log. Vectors come
// from a configurable OpenAI-API-compatible provider — the fleet's one
// external dependency; its outages degrade this service only.
package semantic

import (
	"context"
	"fmt"
	"sync"

	"github.com/philippgille/chromem-go"

	"github.com/impire-io/hits/client"
)

// Config names the embedding provider: an OpenAI-API-compatible endpoint
// (POST {BaseURL}/embeddings), its key, and the model to ask for.
type Config struct {
	BaseURL string
	APIKey  string
	Model   string
}

// indexer is the store boundary, same seam as the other index services.
// Items are indexed as chunks — the report and each note separately — so no
// document outgrows the provider's input cap as a trail lengthens, and a
// failed embed degrades one chunk, not the whole item.
type indexer interface {
	upsert(ctx context.Context, itemID, chunk, text string) error
	remove(ctx context.Context, itemID string) error
	query(ctx context.Context, text string, limit int) ([]client.SemanticHit, error)
}

// chromemIndex is the embedded, in-memory, pure-Go implementation. The lock
// serializes writes against queries; the embedding calls inside happen on
// the provider's clock.
type chromemIndex struct {
	mu  sync.Mutex
	col *chromem.Collection
}

func newChromemIndex(cfg Config) (*chromemIndex, error) {
	db := chromem.NewDB()
	embed := chromem.NewEmbeddingFuncOpenAICompat(cfg.BaseURL, cfg.APIKey, cfg.Model, nil)
	col, err := db.CreateCollection("items", nil, embed)
	if err != nil {
		return nil, fmt.Errorf("create embedding collection: %w", err)
	}
	return &chromemIndex{col: col}, nil
}

// upsert embeds one chunk of an item. A chunk the provider refuses — over
// its input cap, or any other rejection — is the caller's degraded-not-down
// case: skipped loudly, never truncated into the index, so what a query
// matches is always text someone actually wrote.
func (c *chromemIndex) upsert(ctx context.Context, itemID, chunk, text string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	docID := itemID + "#" + chunk
	// chromem has no upsert: clear any previous vector, then add. A failed
	// delete of a missing document is fine — the add is what matters.
	_ = c.col.Delete(ctx, nil, nil, docID)
	if err := c.col.AddDocument(ctx, chromem.Document{
		ID:       docID,
		Content:  text,
		Metadata: map[string]string{"item": itemID},
	}); err != nil {
		return fmt.Errorf("embed item %s chunk %s: %w", itemID, chunk, err)
	}
	return nil
}

func (c *chromemIndex) remove(ctx context.Context, itemID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.col.Delete(ctx, map[string]string{"item": itemID}, nil); err != nil {
		return fmt.Errorf("remove item %s: %w", itemID, err)
	}
	return nil
}

func (c *chromemIndex) query(ctx context.Context, text string, limit int) ([]client.SemanticHit, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}
	n := c.col.Count()
	if n == 0 {
		return []client.SemanticHit{}, nil
	}
	// chromem scores every document regardless of how many results are asked
	// for, so asking for all of them costs only the result slice — and the
	// per-item merge below needs to see every chunk that might outrank one.
	results, err := c.col.Query(ctx, text, n, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("semantic query: %w", err)
	}
	// One hit per item, scored by its best chunk: results arrive sorted by
	// similarity, so the first chunk seen for an item is its score.
	seen := make(map[string]bool, limit)
	hits := make([]client.SemanticHit, 0, limit)
	for _, r := range results {
		item := r.Metadata["item"]
		if seen[item] {
			continue
		}
		seen[item] = true
		hits = append(hits, client.SemanticHit{ID: item, Score: float64(r.Similarity)})
		if len(hits) == limit {
			break
		}
	}
	return hits, nil
}

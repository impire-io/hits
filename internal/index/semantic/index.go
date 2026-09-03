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
type indexer interface {
	upsert(ctx context.Context, id, text string) error
	remove(ctx context.Context, id string) error
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

func (c *chromemIndex) upsert(ctx context.Context, id, text string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	// chromem has no upsert: clear any previous vector, then add. A failed
	// delete of a missing document is fine — the add is what matters.
	_ = c.col.Delete(ctx, nil, nil, id)
	if err := c.col.AddDocument(ctx, chromem.Document{ID: id, Content: text}); err != nil {
		return fmt.Errorf("embed item %s: %w", id, err)
	}
	return nil
}

func (c *chromemIndex) remove(ctx context.Context, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.col.Delete(ctx, nil, nil, id); err != nil {
		return fmt.Errorf("remove item %s: %w", id, err)
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
	// chromem rejects asking for more results than the collection holds.
	if n := c.col.Count(); n < limit {
		limit = n
	}
	if limit == 0 {
		return []client.SemanticHit{}, nil
	}
	results, err := c.col.Query(ctx, text, limit, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("semantic query: %w", err)
	}
	hits := make([]client.SemanticHit, 0, len(results))
	for _, r := range results {
		hits = append(hits, client.SemanticHit{ID: r.ID, Score: float64(r.Similarity)})
	}
	return hits, nil
}

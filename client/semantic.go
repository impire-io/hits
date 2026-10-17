package client

import (
	"context"

	"github.com/impire-io/hits/contract"
)

// SemanticRequest asks for the items nearest to a text, through the
// meaning index — chronicle's semantic kind over reports and notes.
type SemanticRequest struct {
	Text  string `json:"text"`
	Limit int    `json:"limit,omitempty"` // default 10, capped at 100
}

// SemanticHit is one match: an item ID and its similarity score.
type SemanticHit struct {
	ID    string  `json:"id"`
	Score float64 `json:"score"`
}

// SemanticReply carries the nearest items, best first.
type SemanticReply struct {
	Hits []SemanticHit `json:"hits"`
}

// SemanticSearch queries the meaning index. Tombstoned items are dropped
// from the reply, as from every view.
func (c *Client) SemanticSearch(ctx context.Context, r SemanticRequest) (SemanticReply, error) {
	limit, _ := page(r.Limit, 0)
	st := c.cc.QuerySemantic(ctx, c.store, IndexMeaning, r.Text, limit)
	hits := []SemanticHit{}
	for h, err := range st.Items() {
		if err != nil {
			return SemanticReply{}, refusal(err)
		}
		kind, id := contract.ParsePath(h.Instance)
		if kind != contract.TypeItem {
			continue
		}
		if it, _, err := c.loadItem(ctx, id); err != nil {
			return SemanticReply{}, refusal(err)
		} else if it == nil || it.Tombstoned {
			continue
		}
		hits = append(hits, SemanticHit{ID: id, Score: h.Score})
	}
	return SemanticReply{Hits: hits}, nil
}

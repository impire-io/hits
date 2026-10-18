package chronicle

import (
	"context"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/contract"
)

// SemanticSearch queries the meaning index. Tombstoned items are dropped
// from the reply, as from every view.
func (c *Client) SemanticSearch(ctx context.Context, r client.SemanticRequest) (client.SemanticReply, error) {
	limit, _ := page(r.Limit, 0)
	st := c.cc.QuerySemantic(ctx, c.store, IndexMeaning, r.Text, limit)
	hits := []client.SemanticHit{}
	for h, err := range st.Items() {
		if err != nil {
			return client.SemanticReply{}, refusal(err)
		}
		kind, id := contract.ParsePath(h.Instance)
		if kind != contract.TypeItem {
			continue
		}
		if it, _, err := c.loadItem(ctx, id); err != nil {
			return client.SemanticReply{}, refusal(err)
		} else if it == nil || it.Tombstoned {
			continue
		}
		hits = append(hits, client.SemanticHit{ID: id, Score: h.Score})
	}
	return client.SemanticReply{Hits: hits}, nil
}

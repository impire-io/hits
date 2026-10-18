package chronicle

import (
	"context"
	"sort"
	"sync"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/contract"
)

const (
	defaultLimit = 10
	maxLimit     = 100
	loadFan      = 8
)

func matches(r client.SearchRequest, it *contract.Item) bool {
	if it == nil || it.Tombstoned {
		return false
	}
	if r.Type != "" && it.Type != r.Type {
		return false
	}
	if r.Status != "" && it.Status != r.Status {
		return false
	}
	if r.Initiative != "" && it.Initiative != r.Initiative {
		return false
	}
	if r.Target != "" && it.Target != r.Target {
		return false
	}
	return true
}

func page(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

// SearchItems queries the text index — or lists the items when the query
// is empty — and filters the hits by their state.
func (c *Client) SearchItems(ctx context.Context, r client.SearchRequest) (client.SearchReply, error) {
	limit, offset := page(r.Limit, r.Offset)
	var hits []client.SearchHit
	if r.Query == "" {
		items, err := c.ListItems(ctx)
		if err != nil {
			return client.SearchReply{}, err
		}
		for i := range items {
			if matches(r, &items[i]) {
				hits = append(hits, client.SearchHit{ID: items[i].ID})
			}
		}
	} else {
		st := c.cc.QueryIndex(ctx, c.store, IndexText, r.Query, 0)
		var found []client.SearchHit
		for h, err := range st.Items() {
			if err != nil {
				return client.SearchReply{}, refusal(err)
			}
			if kind, id := contract.ParsePath(h.Instance); kind == contract.TypeItem {
				found = append(found, client.SearchHit{ID: id, Score: h.Score})
			}
		}
		// The join: every hit's state, a window of loads at a time, so the
		// filters and the tombstone rule apply to what the index found.
		states := make([]*contract.Item, len(found))
		errs := make([]error, len(found))
		var wg sync.WaitGroup
		sem := make(chan struct{}, loadFan)
		for i, h := range found {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				it, _, err := c.loadItem(ctx, h.ID)
				states[i], errs[i] = it, err
			}()
		}
		wg.Wait()
		for i, h := range found {
			if errs[i] != nil {
				return client.SearchReply{}, refusal(errs[i])
			}
			if matches(r, states[i]) {
				hits = append(hits, h)
			}
		}
		sort.SliceStable(hits, func(a, b int) bool { return hits[a].Score > hits[b].Score })
	}
	total := uint64(len(hits))
	if offset >= len(hits) {
		return client.SearchReply{Hits: []client.SearchHit{}, Total: total}, nil
	}
	hits = hits[offset:]
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return client.SearchReply{Hits: hits, Total: total}, nil
}

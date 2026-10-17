package client

import (
	"context"
	"sort"
	"sync"

	"github.com/impire-io/hits/contract"
)

// SearchRequest queries the full-text index over reports and notes. Every
// field is optional; an empty query lists everything. The filters are the
// client's: chronicle's search takes none, so the hits are joined with
// their state (hits-hq 02-DESIGN/the-store.md § the indexes).
type SearchRequest struct {
	Query      string          `json:"query,omitempty"`
	Type       contract.Type   `json:"type,omitempty"`
	Status     contract.Status `json:"status,omitempty"`
	Initiative string          `json:"initiative,omitempty"`
	Target     string          `json:"target,omitempty"`
	Limit      int             `json:"limit,omitempty"`  // default 10, capped at 100
	Offset     int             `json:"offset,omitempty"` // for paging
}

// SearchHit is one match: an item ID and its relevance score. State comes
// from the store — the index is never authority.
type SearchHit struct {
	ID    string  `json:"id"`
	Score float64 `json:"score"`
}

// SearchReply carries the page of hits and the total match count.
type SearchReply struct {
	Hits  []SearchHit `json:"hits"`
	Total uint64      `json:"total"`
}

const (
	defaultLimit = 10
	maxLimit     = 100
	loadFan      = 8
)

func (r SearchRequest) matches(it *contract.Item) bool {
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
func (c *Client) SearchItems(ctx context.Context, r SearchRequest) (SearchReply, error) {
	limit, offset := page(r.Limit, r.Offset)
	var hits []SearchHit
	if r.Query == "" {
		items, err := c.ListItems(ctx)
		if err != nil {
			return SearchReply{}, err
		}
		for i := range items {
			if r.matches(&items[i]) {
				hits = append(hits, SearchHit{ID: items[i].ID})
			}
		}
	} else {
		st := c.cc.QueryIndex(ctx, c.store, IndexText, r.Query, 0)
		var found []SearchHit
		for h, err := range st.Items() {
			if err != nil {
				return SearchReply{}, refusal(err)
			}
			if kind, id := contract.ParsePath(h.Instance); kind == contract.TypeItem {
				found = append(found, SearchHit{ID: id, Score: h.Score})
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
				return SearchReply{}, refusal(errs[i])
			}
			if r.matches(states[i]) {
				hits = append(hits, h)
			}
		}
		sort.SliceStable(hits, func(a, b int) bool { return hits[a].Score > hits[b].Score })
	}
	total := uint64(len(hits))
	if offset >= len(hits) {
		return SearchReply{Hits: []SearchHit{}, Total: total}, nil
	}
	hits = hits[offset:]
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return SearchReply{Hits: hits, Total: total}, nil
}

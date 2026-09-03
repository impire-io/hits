package client

import (
	"context"

	"github.com/impire-io/hits/contract"
)

// SearchSubject is the wire subject of the hits-search query endpoint.
const SearchSubject = "hits.search.query"

// SearchRequest queries the full-text index over reports and notes. Every
// field is optional; an empty request matches everything, paged.
type SearchRequest struct {
	Query  string          `json:"query,omitempty"`
	Type   contract.Type   `json:"type,omitempty"`
	Status contract.Status `json:"status,omitempty"`
	Limit  int             `json:"limit,omitempty"`  // default 10, capped at 100
	Offset int             `json:"offset,omitempty"` // for paging
}

// SearchHit is one match: an item ID and its relevance score. State comes
// from the hits service — the index is never authority.
type SearchHit struct {
	ID    string  `json:"id"`
	Score float64 `json:"score"`
}

// SearchReply carries the page of hits and the total match count.
type SearchReply struct {
	Hits  []SearchHit `json:"hits"`
	Total uint64      `json:"total"`
}

// SearchItems queries the hits-search index service.
func (c *Client) SearchItems(ctx context.Context, r SearchRequest) (SearchReply, error) {
	return request[SearchReply](ctx, c, SearchSubject, r)
}

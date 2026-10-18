package client

import (
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

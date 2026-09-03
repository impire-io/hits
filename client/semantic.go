package client

import "context"

// SemanticSubject is the wire subject of the hits-semantic query endpoint.
const SemanticSubject = "hits.semantic.query"

// SemanticRequest asks for the items nearest to a text.
type SemanticRequest struct {
	Text  string `json:"text"`
	Limit int    `json:"limit,omitempty"` // default 10, capped at 100
}

// SemanticHit is one match: an item ID and its similarity. State comes from
// the hits service — the index is never authority.
type SemanticHit struct {
	ID    string  `json:"id"`
	Score float64 `json:"score"`
}

// SemanticReply carries the nearest items, best first.
type SemanticReply struct {
	Hits []SemanticHit `json:"hits"`
}

// SemanticSearch queries the hits-semantic index service.
func (c *Client) SemanticSearch(ctx context.Context, r SemanticRequest) (SemanticReply, error) {
	return request[SemanticReply](ctx, c, SemanticSubject, r)
}

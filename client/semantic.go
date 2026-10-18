package client

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

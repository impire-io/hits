package client

import "context"

// The wire subjects of the hits-graph endpoints.
const (
	GraphNeighborsSubject = "hits.graph.neighbors"
	GraphWalkSubject      = "hits.graph.walk"
)

// NodeKind classifies a graph node.
type NodeKind string

// The node kinds. Items and projects have identity in the system; actor
// nodes exist only by reference.
const (
	NodeItem    NodeKind = "item"
	NodeProject NodeKind = "project"
	NodeActor   NodeKind = "actor"
)

// The graph's edge types: the assertable links plus the edges derived from
// properties and ops (02-DESIGN/item-model.md § links).
const (
	EdgeDuplicates = "duplicates"
	EdgeRelatesTo  = "relates-to"
	EdgeLocatedIn  = "located-in"
	EdgeReportedBy = "reported-by"
	EdgeClaimedBy  = "claimed-by"
	EdgeBlockedBy  = "blocked-by"
)

// NodeRef identifies one node; Name is carried on project nodes only, from
// their registration.
type NodeRef struct {
	Kind NodeKind `json:"kind"`
	ID   string   `json:"id"`
	Name string   `json:"name,omitempty"`
}

// GraphEdge is one typed, directed edge.
type GraphEdge struct {
	From NodeRef `json:"from"`
	Type string  `json:"type"`
	To   NodeRef `json:"to"`
}

// NeighborsRequest asks for the edges at one node. Direction is "out",
// "in", or "both" (the default); Types narrows to the named edge types.
type NeighborsRequest struct {
	Kind      NodeKind `json:"kind"`
	ID        string   `json:"id"`
	Direction string   `json:"direction,omitempty"`
	Types     []string `json:"types,omitempty"`
}

// NeighborsReply carries the matching edges.
type NeighborsReply struct {
	Edges []GraphEdge `json:"edges"`
}

// WalkRequest asks for a bounded breadth-first expansion from one node.
// Depth defaults to 2 and is capped by the service; Direction and Types
// filter the traversed edges as in NeighborsRequest.
type WalkRequest struct {
	Kind      NodeKind `json:"kind"`
	ID        string   `json:"id"`
	Depth     int      `json:"depth,omitempty"`
	Direction string   `json:"direction,omitempty"`
	Types     []string `json:"types,omitempty"`
}

// WalkReply carries every node reached and the edges that reached them.
type WalkReply struct {
	Nodes []NodeRef   `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

// GraphNeighbors queries the hits-graph index for the edges at one node.
func (c *Client) GraphNeighbors(ctx context.Context, r NeighborsRequest) (NeighborsReply, error) {
	return request[NeighborsReply](ctx, c, GraphNeighborsSubject, r)
}

// GraphWalk expands the hits-graph index breadth-first from one node.
func (c *Client) GraphWalk(ctx context.Context, r WalkRequest) (WalkReply, error) {
	return request[WalkReply](ctx, c, GraphWalkSubject, r)
}

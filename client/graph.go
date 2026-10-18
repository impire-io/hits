package client

// NodeKind names what a graph node is.
type NodeKind string

// The node kinds. An actor is a member's handle: not an instance, an
// opaque target the graph walks from.
const (
	NodeItem       NodeKind = "item"
	NodeProject    NodeKind = "project"
	NodeInitiative NodeKind = "initiative"
	NodeRelease    NodeKind = "release"
	NodeActor      NodeKind = "actor"
)

// The edge types: the two asserted link types and the derived edges.
const (
	EdgeDuplicates   = "duplicates"
	EdgeRelatesTo    = "relates-to"
	EdgeLocatedIn    = "located-in"
	EdgeReportedBy   = "reported-by"
	EdgeClaimedBy    = "claimed-by"
	EdgeBlockedBy    = "blocked-by"
	EdgeInInitiative = "in-initiative"
	EdgeTargets      = "targets"
)

// NodeRef names one node: its kind and id, and the registered name of a
// project, an initiative or a release. A release's id is
// <initiative>.<slug>.
type NodeRef struct {
	Kind NodeKind `json:"kind"`
	ID   string   `json:"id"`
	Name string   `json:"name,omitempty"`
}

// GraphEdge is one directed, typed edge.
type GraphEdge struct {
	From NodeRef `json:"from"`
	Type string  `json:"type"`
	To   NodeRef `json:"to"`
}

// NeighborsRequest asks for the edges at a node.
type NeighborsRequest struct {
	Kind      NodeKind `json:"kind"`
	ID        string   `json:"id"`
	Direction string   `json:"direction,omitempty"` // out, in, or both (the default)
	Types     []string `json:"types,omitempty"`     // edge types to keep; all when empty
}

// NeighborsReply carries the edges found.
type NeighborsReply struct {
	Edges []GraphEdge `json:"edges"`
}

// WalkRequest asks for a bounded traversal from a node.
type WalkRequest struct {
	Kind      NodeKind `json:"kind"`
	ID        string   `json:"id"`
	Depth     int      `json:"depth,omitempty"` // default 2
	Direction string   `json:"direction,omitempty"`
	Types     []string `json:"types,omitempty"`
}

// WalkReply carries the nodes reached and the edges walked.
type WalkReply struct {
	Nodes []NodeRef   `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

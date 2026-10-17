package client

import (
	"context"
	"fmt"
	"strings"

	chron "github.com/impire-io/chronicle/client"

	"github.com/impire-io/hits/contract"
)

// The graph: chronicle's graph kind over the store's state, with the edges
// the-store.md § the indexes declares — the asserted links, and the edges
// derived from the fields that hold paths.

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

const defaultDepth = 2

// nodePath is the path a node ref names.
func nodePath(kind NodeKind, id string) (string, error) {
	switch kind {
	case NodeItem:
		return contract.ItemPath(id), nil
	case NodeProject:
		return contract.ProjectPath(id), nil
	case NodeInitiative:
		return contract.InitiativePath(id), nil
	case NodeRelease:
		initiative, slug, ok := strings.Cut(id, ".")
		if !ok {
			return "", &APIError{Code: "invalid-request", Message: fmt.Sprintf("a release node is <initiative>.<slug>, not %q", id)}
		}
		return contract.ReleasePath(initiative, slug), nil
	case NodeActor:
		return contract.MemberPath(id), nil
	}
	return "", &APIError{Code: "invalid-request", Message: fmt.Sprintf("unknown node kind %q", kind)}
}

// nodeRef reads a path back into a node ref, with the registered name
// where there is one.
func (c *Client) nodeRef(ctx context.Context, path string) NodeRef {
	kind, id := contract.ParsePath(path)
	switch kind {
	case contract.TypeItem:
		return NodeRef{Kind: NodeItem, ID: id}
	case contract.TypeProject:
		return NodeRef{Kind: NodeProject, ID: id, Name: c.nameOf(ctx, path)}
	case contract.TypeInitiative:
		return NodeRef{Kind: NodeInitiative, ID: id, Name: c.nameOf(ctx, path)}
	case contract.TypeRelease:
		return NodeRef{Kind: NodeRelease, ID: strings.Replace(id, "/", ".", 1), Name: c.nameOf(ctx, path)}
	case "member":
		return NodeRef{Kind: NodeActor, ID: id}
	}
	return NodeRef{Kind: NodeKind(kind), ID: id}
}

func direction(d string) string {
	if d == "" {
		return "both"
	}
	return d
}

func keeps(types []string, label string) bool {
	if len(types) == 0 {
		return true
	}
	for _, t := range types {
		if t == label {
			return true
		}
	}
	return false
}

// GraphNeighbors reads the edges at a node from the rel index.
func (c *Client) GraphNeighbors(ctx context.Context, r NeighborsRequest) (NeighborsReply, error) {
	path, err := nodePath(r.Kind, r.ID)
	if err != nil {
		return NeighborsReply{}, err
	}
	q := chron.GraphQueryRequest{Op: "neighbors", Instance: path, Direction: direction(r.Direction)}
	if len(r.Types) == 1 {
		q.Label = r.Types[0]
	}
	edges := []GraphEdge{}
	for e, err := range c.cc.GraphNeighbors(ctx, c.store, IndexRel, q).Items() {
		if err != nil {
			return NeighborsReply{}, refusal(err)
		}
		if !keeps(r.Types, e.Label) {
			continue
		}
		edges = append(edges, GraphEdge{From: c.nodeRef(ctx, e.From), Type: e.Label, To: c.nodeRef(ctx, e.To)})
	}
	return NeighborsReply{Edges: edges}, nil
}

// GraphWalk traverses from a node, breadth-first, to the depth asked. The
// nodes reached are returned with the edge each arrived by; chronicle's
// walk names the label an instance was reached through and not the
// instance it came from, so each edge carries the label and the node
// reached, its From left empty.
func (c *Client) GraphWalk(ctx context.Context, r WalkRequest) (WalkReply, error) {
	path, err := nodePath(r.Kind, r.ID)
	if err != nil {
		return WalkReply{}, err
	}
	depth := r.Depth
	if depth <= 0 {
		depth = defaultDepth
	}
	q := chron.GraphQueryRequest{Op: "walk", Instance: path, Direction: direction(r.Direction), Labels: r.Types, Depth: depth}
	start := c.nodeRef(ctx, path)
	nodes := []NodeRef{start}
	edges := []GraphEdge{}
	for v, err := range c.cc.GraphWalk(ctx, c.store, IndexRel, q).Items() {
		if err != nil {
			return WalkReply{}, refusal(err)
		}
		ref := c.nodeRef(ctx, v.Instance)
		nodes = append(nodes, ref)
		edges = append(edges, GraphEdge{Type: v.Via, To: ref})
	}
	return WalkReply{Nodes: nodes, Edges: edges}, nil
}

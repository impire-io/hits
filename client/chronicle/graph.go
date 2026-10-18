package chronicle

import (
	"context"
	"fmt"
	"strings"

	chron "github.com/impire-io/chronicle/client"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/contract"
)

// The graph: chronicle's graph kind over the store's state, with the edges
// the-store.md § the indexes declares — the asserted links, and the edges
// derived from the fields that hold paths.

const defaultDepth = 2

// nodePath is the path a node ref names.
func nodePath(kind client.NodeKind, id string) (string, error) {
	switch kind {
	case client.NodeItem:
		return contract.ItemPath(id), nil
	case client.NodeProject:
		return contract.ProjectPath(id), nil
	case client.NodeInitiative:
		return contract.InitiativePath(id), nil
	case client.NodeRelease:
		initiative, slug, ok := strings.Cut(id, ".")
		if !ok {
			return "", &client.APIError{Code: "invalid-request", Message: fmt.Sprintf("a release node is <initiative>.<slug>, not %q", id)}
		}
		return contract.ReleasePath(initiative, slug), nil
	case client.NodeActor:
		return contract.MemberPath(id), nil
	}
	return "", &client.APIError{Code: "invalid-request", Message: fmt.Sprintf("unknown node kind %q", kind)}
}

// nodeRef reads a path back into a node ref, with the registered name
// where there is one.
func (c *Client) nodeRef(ctx context.Context, path string) client.NodeRef {
	kind, id := contract.ParsePath(path)
	switch kind {
	case contract.TypeItem:
		return client.NodeRef{Kind: client.NodeItem, ID: id}
	case contract.TypeProject:
		return client.NodeRef{Kind: client.NodeProject, ID: id, Name: c.nameOf(ctx, path)}
	case contract.TypeInitiative:
		return client.NodeRef{Kind: client.NodeInitiative, ID: id, Name: c.nameOf(ctx, path)}
	case contract.TypeRelease:
		return client.NodeRef{Kind: client.NodeRelease, ID: strings.Replace(id, "/", ".", 1), Name: c.nameOf(ctx, path)}
	case "member":
		return client.NodeRef{Kind: client.NodeActor, ID: id}
	}
	return client.NodeRef{Kind: client.NodeKind(kind), ID: id}
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
func (c *Client) GraphNeighbors(ctx context.Context, r client.NeighborsRequest) (client.NeighborsReply, error) {
	path, err := nodePath(r.Kind, r.ID)
	if err != nil {
		return client.NeighborsReply{}, err
	}
	q := chron.GraphQueryRequest{Op: "neighbors", Instance: path, Direction: direction(r.Direction)}
	if len(r.Types) == 1 {
		q.Label = r.Types[0]
	}
	edges := []client.GraphEdge{}
	for e, err := range c.cc.GraphNeighbors(ctx, c.store, IndexRel, q).Items() {
		if err != nil {
			return client.NeighborsReply{}, refusal(err)
		}
		if !keeps(r.Types, e.Label) {
			continue
		}
		edges = append(edges, client.GraphEdge{From: c.nodeRef(ctx, e.From), Type: e.Label, To: c.nodeRef(ctx, e.To)})
	}
	return client.NeighborsReply{Edges: edges}, nil
}

// GraphWalk traverses from a node, breadth-first, to the depth asked. The
// nodes reached are returned with the edge each arrived by; chronicle's
// walk names the label an instance was reached through and not the
// instance it came from, so each edge carries the label and the node
// reached, its From left empty.
func (c *Client) GraphWalk(ctx context.Context, r client.WalkRequest) (client.WalkReply, error) {
	path, err := nodePath(r.Kind, r.ID)
	if err != nil {
		return client.WalkReply{}, err
	}
	depth := r.Depth
	if depth <= 0 {
		depth = defaultDepth
	}
	q := chron.GraphQueryRequest{Op: "walk", Instance: path, Direction: direction(r.Direction), Labels: r.Types, Depth: depth}
	start := c.nodeRef(ctx, path)
	nodes := []client.NodeRef{start}
	edges := []client.GraphEdge{}
	for v, err := range c.cc.GraphWalk(ctx, c.store, IndexRel, q).Items() {
		if err != nil {
			return client.WalkReply{}, refusal(err)
		}
		ref := c.nodeRef(ctx, v.Instance)
		nodes = append(nodes, ref)
		edges = append(edges, client.GraphEdge{Type: v.Via, To: ref})
	}
	return client.WalkReply{Nodes: nodes, Edges: edges}, nil
}

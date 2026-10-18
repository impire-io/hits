package fleet

import (
	"context"

	"github.com/impire-io/hits/client"
)

// SearchItems queries the fleet's search index.
func (c *Client) SearchItems(ctx context.Context, r client.SearchRequest) (client.SearchReply, error) {
	return request[client.SearchReply](ctx, c, SearchSubject, r)
}

// SemanticSearch queries the fleet's semantic index.
func (c *Client) SemanticSearch(ctx context.Context, r client.SemanticRequest) (client.SemanticReply, error) {
	return request[client.SemanticReply](ctx, c, SemanticSubject, r)
}

// GraphNeighbors queries the fleet's graph index for a node's edges.
func (c *Client) GraphNeighbors(ctx context.Context, r client.NeighborsRequest) (client.NeighborsReply, error) {
	return request[client.NeighborsReply](ctx, c, GraphNeighborsSubject, r)
}

// GraphWalk queries the fleet's graph index for a walk.
func (c *Client) GraphWalk(ctx context.Context, r client.WalkRequest) (client.WalkReply, error) {
	return request[client.WalkReply](ctx, c, GraphWalkSubject, r)
}

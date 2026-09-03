package mcp

import (
	"context"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/contract"
)

type searchItemsIn struct {
	Query  string          `json:"query,omitempty" jsonschema:"full-text query over reports and notes; empty matches everything"`
	Type   contract.Type   `json:"type,omitempty" jsonschema:"filter by item type"`
	Status contract.Status `json:"status,omitempty" jsonschema:"filter by status"`
	Limit  int             `json:"limit,omitempty" jsonschema:"page size (service default 10, capped at 100)"`
	Offset int             `json:"offset,omitempty" jsonschema:"page start"`
}

type semanticSearchIn struct {
	Text  string `json:"text" jsonschema:"the text to find the nearest items to"`
	Limit int    `json:"limit,omitempty" jsonschema:"result count (service default 10, capped at 100)"`
}

type graphNeighborsIn struct {
	Kind      client.NodeKind `json:"kind" jsonschema:"node kind: item, project, or actor"`
	ID        string          `json:"id" jsonschema:"the node's id"`
	Direction string          `json:"direction,omitempty" jsonschema:"edge direction: out, in, or both (the default)"`
	Types     []string        `json:"types,omitempty" jsonschema:"narrow to the named edge types"`
}

type graphWalkIn struct {
	Kind      client.NodeKind `json:"kind" jsonschema:"node kind: item, project, or actor"`
	ID        string          `json:"id" jsonschema:"the node's id"`
	Depth     int             `json:"depth,omitempty" jsonschema:"expansion depth (service default 2, capped)"`
	Direction string          `json:"direction,omitempty" jsonschema:"edge direction: out, in, or both (the default)"`
	Types     []string        `json:"types,omitempty" jsonschema:"narrow to the named edge types"`
}

func addQueryTools(s *sdk.Server, c *client.Client) {
	sdk.AddTool(s, &sdk.Tool{Name: "search_items", Description: "Full-text search over reports and notes; hits resolve to item ids — read state with get_item.", Annotations: readOnly},
		func(ctx context.Context, _ *sdk.CallToolRequest, in searchItemsIn) (*sdk.CallToolResult, client.SearchReply, error) {
			r, err := c.SearchItems(ctx, client.SearchRequest{
				Query: in.Query, Type: in.Type, Status: in.Status, Limit: in.Limit, Offset: in.Offset,
			})
			return nil, r, err
		})

	sdk.AddTool(s, &sdk.Tool{Name: "semantic_search", Description: "Find the items nearest to a text, best first; hits resolve to item ids — read state with get_item.", Annotations: readOnly},
		func(ctx context.Context, _ *sdk.CallToolRequest, in semanticSearchIn) (*sdk.CallToolResult, client.SemanticReply, error) {
			r, err := c.SemanticSearch(ctx, client.SemanticRequest{Text: in.Text, Limit: in.Limit})
			return nil, r, err
		})

	sdk.AddTool(s, &sdk.Tool{Name: "graph_neighbors", Description: "The typed edges at one node of the item graph.", Annotations: readOnly},
		func(ctx context.Context, _ *sdk.CallToolRequest, in graphNeighborsIn) (*sdk.CallToolResult, client.NeighborsReply, error) {
			r, err := c.GraphNeighbors(ctx, client.NeighborsRequest{
				Kind: in.Kind, ID: in.ID, Direction: in.Direction, Types: in.Types,
			})
			return nil, r, err
		})

	sdk.AddTool(s, &sdk.Tool{Name: "graph_walk", Description: "A bounded breadth-first expansion of the item graph from one node.", Annotations: readOnly},
		func(ctx context.Context, _ *sdk.CallToolRequest, in graphWalkIn) (*sdk.CallToolResult, client.WalkReply, error) {
			r, err := c.GraphWalk(ctx, client.WalkRequest{
				Kind: in.Kind, ID: in.ID, Depth: in.Depth, Direction: in.Direction, Types: in.Types,
			})
			return nil, r, err
		})
}

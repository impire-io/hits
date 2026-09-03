package mcp

import (
	"context"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/contract"
)

// The input types restate the client request types field for field, wire
// names included, minus actor — the server supplies it. Schemas stay loose
// (no enums): the service's invariants are the validator, so rejections
// keep their machine-legible names.

type itemRefIn struct {
	ID string `json:"id" jsonschema:"the item's id"`
}

type createItemIn struct {
	Type            contract.Type     `json:"type" jsonschema:"item type: bug, task, or improvement"`
	Report          string            `json:"report" jsonschema:"the symptom in plain terms"`
	Priority        contract.Priority `json:"priority,omitempty" jsonschema:"triage signal: high, normal, or low (default normal)"`
	LocatedIn       []string          `json:"located-in,omitempty" jsonschema:"registered project slugs; required for a task"`
	DiscoveredWhile string            `json:"discovered-while,omitempty" jsonschema:"the context the item was noticed in"`
}

type editItemIn struct {
	ID              string             `json:"id" jsonschema:"the item's id"`
	Priority        *contract.Priority `json:"priority,omitempty" jsonschema:"triage signal: high, normal, or low"`
	LocatedIn       *[]string          `json:"located-in,omitempty" jsonschema:"registered project slugs; replaces the list"`
	DiscoveredWhile *string            `json:"discovered-while,omitempty" jsonschema:"the context the item was noticed in; empty clears"`
	Lands           *[]contract.Land   `json:"lands,omitempty" jsonschema:"cross-repo landing order; empty list clears"`
}

type transitionItemIn struct {
	ID            string            `json:"id" jsonschema:"the item's id"`
	To            contract.Status   `json:"to" jsonschema:"target status: diagnosing, located, resolved, or wontfix"`
	LocatedIn     []string          `json:"located-in,omitempty" jsonschema:"registered project slugs, when locating sets them"`
	FixedBy       []contract.FixRef `json:"fixed-by,omitempty" jsonschema:"closing refs (pr, commit, or action with an evidence note); closing transitions only"`
	AmendedDesign []string          `json:"amended-design,omitempty" jsonschema:"design docs amended by the close; closing transitions only"`
}

type claimItemIn struct {
	ID    string `json:"id" jsonschema:"the item's id"`
	Steal bool   `json:"steal,omitempty" jsonschema:"take over an abandoned claim (attributed in the op)"`
}

type blockItemIn struct {
	ID        string `json:"id" jsonschema:"the item's id"`
	BlockedBy string `json:"blocked-by,omitempty" jsonschema:"the thing being waited on"`
}

type linkItemsIn struct {
	ID   string            `json:"id" jsonschema:"the item the edge starts from"`
	Type contract.LinkType `json:"type" jsonschema:"link type: duplicates or relates-to"`
	To   string            `json:"to" jsonschema:"the item the edge points at"`
}

type noteItemIn struct {
	ID   string `json:"id" jsonschema:"the item's id"`
	Text string `json:"text" jsonschema:"the trail entry to append"`
}

type tombstoneItemIn struct {
	ID     string `json:"id" jsonschema:"the item's id"`
	Reason string `json:"reason" jsonschema:"why the filing was a mistake"`
}

type registerProjectIn struct {
	Slug        string `json:"slug" jsonschema:"chosen subject-token-safe slug"`
	Name        string `json:"name" jsonschema:"display name"`
	Description string `json:"description,omitempty" jsonschema:"what the project is"`
}

type emptyIn struct{}

func addItemTools(s *sdk.Server, c *client.Client, actor string) {
	sdk.AddTool(s, &sdk.Tool{Name: "create_item", Description: "Open an item; the actor becomes the reporter. A task must name located-in."},
		func(ctx context.Context, _ *sdk.CallToolRequest, in createItemIn) (*sdk.CallToolResult, contract.Item, error) {
			item, err := c.CreateItem(ctx, client.CreateItemRequest{
				Actor: actor, Type: in.Type, Report: in.Report, Priority: in.Priority,
				LocatedIn: in.LocatedIn, DiscoveredWhile: in.DiscoveredWhile,
			})
			return nil, item, err
		})

	sdk.AddTool(s, &sdk.Tool{Name: "get_item", Description: "Read one item's current snapshot.", Annotations: readOnly},
		func(ctx context.Context, _ *sdk.CallToolRequest, in itemRefIn) (*sdk.CallToolResult, contract.Item, error) {
			item, err := c.GetItem(ctx, in.ID)
			return nil, item, err
		})

	sdk.AddTool(s, &sdk.Tool{Name: "edit_item", Description: "Change non-lifecycle properties; absent fields stay untouched."},
		func(ctx context.Context, _ *sdk.CallToolRequest, in editItemIn) (*sdk.CallToolResult, contract.Item, error) {
			item, err := c.EditItem(ctx, client.EditItemRequest{
				Actor: actor, ID: in.ID, Priority: in.Priority,
				LocatedIn: in.LocatedIn, DiscoveredWhile: in.DiscoveredWhile, Lands: in.Lands,
			})
			return nil, item, err
		})

	sdk.AddTool(s, &sdk.Tool{Name: "transition_item", Description: "Move an item's status; closing transitions carry fixed-by and amended-design, and the service stamps the close date."},
		func(ctx context.Context, _ *sdk.CallToolRequest, in transitionItemIn) (*sdk.CallToolResult, contract.Item, error) {
			item, err := c.TransitionItem(ctx, client.TransitionItemRequest{
				Actor: actor, ID: in.ID, To: in.To,
				LocatedIn: in.LocatedIn, FixedBy: in.FixedBy, AmendedDesign: in.AmendedDesign,
			})
			return nil, item, err
		})

	sdk.AddTool(s, &sdk.Tool{Name: "claim_item", Description: "Record intent to work an item — not started work."},
		func(ctx context.Context, _ *sdk.CallToolRequest, in claimItemIn) (*sdk.CallToolResult, contract.Item, error) {
			item, err := c.ClaimItem(ctx, client.ClaimItemRequest{Actor: actor, ID: in.ID, Steal: in.Steal})
			return nil, item, err
		})

	sdk.AddTool(s, &sdk.Tool{Name: "release_item", Description: "Hand a claim back."},
		func(ctx context.Context, _ *sdk.CallToolRequest, in itemRefIn) (*sdk.CallToolResult, contract.Item, error) {
			item, err := c.ReleaseItem(ctx, client.ReleaseItemRequest{Actor: actor, ID: in.ID})
			return nil, item, err
		})

	sdk.AddTool(s, &sdk.Tool{Name: "block_item", Description: "Block an item on the named thing; the interrupted status is remembered."},
		func(ctx context.Context, _ *sdk.CallToolRequest, in blockItemIn) (*sdk.CallToolResult, contract.Item, error) {
			item, err := c.BlockItem(ctx, client.BlockItemRequest{Actor: actor, ID: in.ID, BlockedBy: in.BlockedBy})
			return nil, item, err
		})

	sdk.AddTool(s, &sdk.Tool{Name: "unblock_item", Description: "Lift a block, restoring the interrupted status."},
		func(ctx context.Context, _ *sdk.CallToolRequest, in itemRefIn) (*sdk.CallToolResult, contract.Item, error) {
			item, err := c.UnblockItem(ctx, client.UnblockItemRequest{Actor: actor, ID: in.ID})
			return nil, item, err
		})

	sdk.AddTool(s, &sdk.Tool{Name: "link_items", Description: "Assert a typed edge between two items."},
		func(ctx context.Context, _ *sdk.CallToolRequest, in linkItemsIn) (*sdk.CallToolResult, contract.Item, error) {
			item, err := c.LinkItem(ctx, client.LinkItemRequest{Actor: actor, ID: in.ID, Type: in.Type, To: in.To})
			return nil, item, err
		})

	sdk.AddTool(s, &sdk.Tool{Name: "unlink_items", Description: "Retract a previously asserted edge."},
		func(ctx context.Context, _ *sdk.CallToolRequest, in linkItemsIn) (*sdk.CallToolResult, contract.Item, error) {
			item, err := c.UnlinkItem(ctx, client.LinkItemRequest{Actor: actor, ID: in.ID, Type: in.Type, To: in.To})
			return nil, item, err
		})

	sdk.AddTool(s, &sdk.Tool{Name: "note_item", Description: "Append a trail entry: follow-ups, diagnosis, closing reasoning."},
		func(ctx context.Context, _ *sdk.CallToolRequest, in noteItemIn) (*sdk.CallToolResult, contract.Item, error) {
			item, err := c.NoteItem(ctx, client.NoteItemRequest{Actor: actor, ID: in.ID, Text: in.Text})
			return nil, item, err
		})

	sdk.AddTool(s, &sdk.Tool{Name: "tombstone_item", Description: "Void a filing mistake; the item accepts no further ops."},
		func(ctx context.Context, _ *sdk.CallToolRequest, in tombstoneItemIn) (*sdk.CallToolResult, contract.Item, error) {
			item, err := c.TombstoneItem(ctx, client.TombstoneItemRequest{Actor: actor, ID: in.ID, Reason: in.Reason})
			return nil, item, err
		})

	sdk.AddTool(s, &sdk.Tool{Name: "register_project", Description: "Add a project to the located-in vocabulary."},
		func(ctx context.Context, _ *sdk.CallToolRequest, in registerProjectIn) (*sdk.CallToolResult, contract.Project, error) {
			p, err := c.RegisterProject(ctx, client.RegisterProjectRequest{
				Actor: actor, Slug: in.Slug, Name: in.Name, Description: in.Description,
			})
			return nil, p, err
		})

	sdk.AddTool(s, &sdk.Tool{Name: "list_projects", Description: "Read the whole located-in vocabulary.", Annotations: readOnly},
		func(ctx context.Context, _ *sdk.CallToolRequest, _ emptyIn) (*sdk.CallToolResult, []contract.Project, error) {
			ps, err := c.ListProjects(ctx)
			return nil, ps, err
		})
}

// Package client is the Go surface of HITS: every caller — the CLI, the MCP
// server, the audit, tests — goes through it, and there is no side door.
// It holds the request and reply types and the Client interface; the two
// substrates implement it (hits-hq decision 0023): client/chronicle, the
// store on chronicle (02-DESIGN/the-client.md), and client/fleet, the wire
// contract of the 0.7 fleet frozen at its release. The context a caller
// dials chooses between them; the sentences and the tools above are the
// same on both.
package client

import (
	"context"

	"github.com/impire-io/hits/contract"
)

// Client speaks one HITS. Every method is one call on the substrate; a
// refusal comes back as *APIError with the invariant's name, "not-found",
// or the substrate's own code.
type Client interface {
	// Author is the actor every write of this client carries: on chronicle
	// the principal the connection is, vouched for; on the fleet the actor
	// the client was given.
	Author() string
	// Ping names what answers: the store's announcement on chronicle, the
	// node on the fleet.
	Ping(ctx context.Context) (About, error)

	GetItem(ctx context.Context, id string) (contract.Item, error)
	ListItems(ctx context.Context) ([]contract.Item, error)
	ListAllItems(ctx context.Context) ([]contract.Item, error)
	CreateItem(ctx context.Context, r CreateItemRequest) (contract.Item, error)
	EditItem(ctx context.Context, r EditItemRequest) (contract.Item, error)
	TransitionItem(ctx context.Context, r TransitionItemRequest) (contract.Item, error)
	ClaimItem(ctx context.Context, r ClaimItemRequest) (contract.Item, error)
	ReleaseItem(ctx context.Context, r ReleaseItemRequest) (contract.Item, error)
	BlockItem(ctx context.Context, r BlockItemRequest) (contract.Item, error)
	UnblockItem(ctx context.Context, r UnblockItemRequest) (contract.Item, error)
	LinkItem(ctx context.Context, r LinkItemRequest) (contract.Item, error)
	UnlinkItem(ctx context.Context, r LinkItemRequest) (contract.Item, error)
	NoteItem(ctx context.Context, r NoteItemRequest) (contract.Item, error)
	TombstoneItem(ctx context.Context, r TombstoneItemRequest) (contract.Item, error)

	RegisterProject(ctx context.Context, r RegisterProjectRequest) (contract.Project, error)
	AssignProject(ctx context.Context, r AssignProjectRequest) (contract.Project, error)
	RetireProject(ctx context.Context, r RetireProjectRequest) (contract.Project, error)
	ListProjects(ctx context.Context) ([]contract.Project, error)
	RegisterInitiative(ctx context.Context, r RegisterInitiativeRequest) (contract.Initiative, error)
	RetireInitiative(ctx context.Context, r RetireInitiativeRequest) (contract.Initiative, error)
	ListInitiatives(ctx context.Context) ([]contract.Initiative, error)
	RegisterRelease(ctx context.Context, r RegisterReleaseRequest) (contract.Release, error)
	ShipRelease(ctx context.Context, r ShipReleaseRequest) (contract.Release, error)
	RetireRelease(ctx context.Context, r RetireReleaseRequest) (contract.Release, error)
	ListReleases(ctx context.Context, r ListReleasesRequest) ([]contract.Release, error)

	SearchItems(ctx context.Context, r SearchRequest) (SearchReply, error)
	SemanticSearch(ctx context.Context, r SemanticRequest) (SemanticReply, error)
	GraphNeighbors(ctx context.Context, r NeighborsRequest) (NeighborsReply, error)
	GraphWalk(ctx context.Context, r WalkRequest) (WalkReply, error)
}

// About identifies what a client reached: the solution's name and version
// as the store announces it, or the 0.7 node's.
type About struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// APIError is a refusal. Code is machine-legible: an invariant name from
// the contract package ("terminal-status", "already-claimed", …),
// "not-found", "refused" for a rule chronicle enforced, or the substrate's
// own code.
type APIError struct {
	Code    string
	Message string
}

func (e *APIError) Error() string { return e.Code + ": " + e.Message }

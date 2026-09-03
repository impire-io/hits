package client

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go/micro"

	"github.com/impire-io/hits/contract"
)

// The wire subjects of the item store endpoints. The service implements
// them; every caller reaches them through this package.
const (
	CreateSubject          = "hits.api.create"
	GetSubject             = "hits.api.get"
	EditSubject            = "hits.api.edit"
	TransitionSubject      = "hits.api.transition"
	ClaimSubject           = "hits.api.claim"
	ReleaseSubject         = "hits.api.release"
	BlockSubject           = "hits.api.block"
	UnblockSubject         = "hits.api.unblock"
	LinkSubject            = "hits.api.link"
	UnlinkSubject          = "hits.api.unlink"
	NoteSubject            = "hits.api.note"
	TombstoneSubject       = "hits.api.tombstone"
	RegisterProjectSubject = "hits.api.project.register"
	ListProjectsSubject    = "hits.api.project.list"
)

// APIError is a service-side rejection. Code is machine-legible: an
// invariant name from the contract package ("terminal-status",
// "already-claimed", …), or "invalid-request", "not-found", "internal".
type APIError struct {
	Code    string
	Message string
}

func (e *APIError) Error() string { return e.Code + ": " + e.Message }

// CreateItemRequest opens an item. The actor becomes the reporter; a task
// must name located-in.
type CreateItemRequest struct {
	Actor           string            `json:"actor"`
	Type            contract.Type     `json:"type"`
	Report          string            `json:"report"`
	Priority        contract.Priority `json:"priority,omitempty"`
	LocatedIn       []string          `json:"located-in,omitempty"`
	DiscoveredWhile string            `json:"discovered-while,omitempty"`
}

// GetItemRequest reads one item's snapshot.
type GetItemRequest struct {
	ID string `json:"id"`
}

// EditItemRequest changes properties outside the lifecycle; nil fields stay
// untouched.
type EditItemRequest struct {
	Actor           string             `json:"actor"`
	ID              string             `json:"id"`
	Priority        *contract.Priority `json:"priority,omitempty"`
	LocatedIn       *[]string          `json:"located-in,omitempty"`
	DiscoveredWhile *string            `json:"discovered-while,omitempty"`
	Lands           *[]contract.Land   `json:"lands,omitempty"`
}

// TransitionItemRequest moves an item's status. Closing transitions may
// carry the closing refs; the service stamps the close date.
type TransitionItemRequest struct {
	Actor         string            `json:"actor"`
	ID            string            `json:"id"`
	To            contract.Status   `json:"to"`
	LocatedIn     []string          `json:"located-in,omitempty"`
	FixedBy       []contract.FixRef `json:"fixed-by,omitempty"`
	AmendedDesign []string          `json:"amended-design,omitempty"`
}

// ClaimItemRequest records intent to work an item. Steal takes over an
// abandoned claim, attributed in the op.
type ClaimItemRequest struct {
	Actor string `json:"actor"`
	ID    string `json:"id"`
	Steal bool   `json:"steal,omitempty"`
}

// ReleaseItemRequest hands a claim back.
type ReleaseItemRequest struct {
	Actor string `json:"actor"`
	ID    string `json:"id"`
}

// BlockItemRequest blocks an item on the named thing; the service records
// the interrupted status so unblocking restores it.
type BlockItemRequest struct {
	Actor     string `json:"actor"`
	ID        string `json:"id"`
	BlockedBy string `json:"blocked-by,omitempty"`
}

// UnblockItemRequest lifts a block, restoring the interrupted status.
type UnblockItemRequest struct {
	Actor string `json:"actor"`
	ID    string `json:"id"`
}

// LinkItemRequest asserts or retracts a typed edge between two items.
type LinkItemRequest struct {
	Actor string            `json:"actor"`
	ID    string            `json:"id"`
	Type  contract.LinkType `json:"type"`
	To    string            `json:"to"`
}

// NoteItemRequest appends a trail entry.
type NoteItemRequest struct {
	Actor string `json:"actor"`
	ID    string `json:"id"`
	Text  string `json:"text"`
}

// TombstoneItemRequest voids a filing mistake.
type TombstoneItemRequest struct {
	Actor  string `json:"actor"`
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// RegisterProjectRequest adds one entry to the located-in vocabulary.
type RegisterProjectRequest struct {
	Actor       string `json:"actor"`
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// CreateItem opens an item and returns its first snapshot.
func (c *Client) CreateItem(ctx context.Context, r CreateItemRequest) (contract.Item, error) {
	return request[contract.Item](ctx, c, CreateSubject, r)
}

// GetItem reads one item's current snapshot.
func (c *Client) GetItem(ctx context.Context, id string) (contract.Item, error) {
	return request[contract.Item](ctx, c, GetSubject, GetItemRequest{ID: id})
}

// EditItem changes non-lifecycle properties.
func (c *Client) EditItem(ctx context.Context, r EditItemRequest) (contract.Item, error) {
	return request[contract.Item](ctx, c, EditSubject, r)
}

// TransitionItem moves an item's status.
func (c *Client) TransitionItem(ctx context.Context, r TransitionItemRequest) (contract.Item, error) {
	return request[contract.Item](ctx, c, TransitionSubject, r)
}

// ClaimItem records intent to work an item.
func (c *Client) ClaimItem(ctx context.Context, r ClaimItemRequest) (contract.Item, error) {
	return request[contract.Item](ctx, c, ClaimSubject, r)
}

// ReleaseItem hands a claim back.
func (c *Client) ReleaseItem(ctx context.Context, r ReleaseItemRequest) (contract.Item, error) {
	return request[contract.Item](ctx, c, ReleaseSubject, r)
}

// BlockItem blocks an item, remembering the status it interrupted.
func (c *Client) BlockItem(ctx context.Context, r BlockItemRequest) (contract.Item, error) {
	return request[contract.Item](ctx, c, BlockSubject, r)
}

// UnblockItem lifts a block and restores the interrupted status.
func (c *Client) UnblockItem(ctx context.Context, r UnblockItemRequest) (contract.Item, error) {
	return request[contract.Item](ctx, c, UnblockSubject, r)
}

// LinkItem asserts a typed edge to another item.
func (c *Client) LinkItem(ctx context.Context, r LinkItemRequest) (contract.Item, error) {
	return request[contract.Item](ctx, c, LinkSubject, r)
}

// UnlinkItem retracts a previously asserted edge.
func (c *Client) UnlinkItem(ctx context.Context, r LinkItemRequest) (contract.Item, error) {
	return request[contract.Item](ctx, c, UnlinkSubject, r)
}

// NoteItem appends a trail entry.
func (c *Client) NoteItem(ctx context.Context, r NoteItemRequest) (contract.Item, error) {
	return request[contract.Item](ctx, c, NoteSubject, r)
}

// TombstoneItem voids a filing mistake.
func (c *Client) TombstoneItem(ctx context.Context, r TombstoneItemRequest) (contract.Item, error) {
	return request[contract.Item](ctx, c, TombstoneSubject, r)
}

// RegisterProject adds a project to the located-in vocabulary.
func (c *Client) RegisterProject(ctx context.Context, r RegisterProjectRequest) (contract.Project, error) {
	return request[contract.Project](ctx, c, RegisterProjectSubject, r)
}

// ListProjects reads the whole located-in vocabulary.
func (c *Client) ListProjects(ctx context.Context) ([]contract.Project, error) {
	return request[[]contract.Project](ctx, c, ListProjectsSubject, struct{}{})
}

// request round-trips one endpoint call, surfacing service rejections as
// *APIError.
func request[T any](ctx context.Context, c *Client, subject string, req any) (T, error) {
	var zero T
	data, err := json.Marshal(req)
	if err != nil {
		return zero, fmt.Errorf("encode request: %w", err)
	}
	msg, err := c.nc.RequestWithContext(ctx, subject, data)
	if err != nil {
		return zero, fmt.Errorf("request %s: %w", subject, err)
	}
	if code := msg.Header.Get(micro.ErrorCodeHeader); code != "" {
		return zero, &APIError{Code: code, Message: msg.Header.Get(micro.ErrorHeader)}
	}
	var out T
	if err := json.Unmarshal(msg.Data, &out); err != nil {
		return zero, fmt.Errorf("decode %s reply: %w", subject, err)
	}
	return out, nil
}

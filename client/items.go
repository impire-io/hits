package client

import (
	"github.com/impire-io/hits/contract"
)

// CreateItemRequest opens an item. The author becomes the reporter; a task
// must name located-in; the initiative names the counter the ID mints
// from (decision 0016).
type CreateItemRequest struct {
	Type            contract.Type     `json:"type"`
	Report          string            `json:"report"`
	Priority        contract.Priority `json:"priority,omitempty"`
	Initiative      string            `json:"initiative"`
	Target          string            `json:"target,omitempty"`
	LocatedIn       []string          `json:"located-in,omitempty"`
	DiscoveredWhile string            `json:"discovered-while,omitempty"`
}

// GetItemRequest reads one item.
type GetItemRequest struct {
	ID string `json:"id"`
}

// EditItemRequest changes non-lifecycle properties; a nil field is left
// alone, a pointer to the zero value clears.
type EditItemRequest struct {
	ID              string             `json:"id"`
	Priority        *contract.Priority `json:"priority,omitempty"`
	Initiative      *string            `json:"initiative,omitempty"`
	Target          *string            `json:"target,omitempty"`
	LocatedIn       *[]string          `json:"located-in,omitempty"`
	DiscoveredWhile *string            `json:"discovered-while,omitempty"`
	Lands           *[]contract.Land   `json:"lands,omitempty"`
}

// TransitionItemRequest moves the status; a closing transition carries the
// refs.
type TransitionItemRequest struct {
	ID            string            `json:"id"`
	To            contract.Status   `json:"to"`
	LocatedIn     []string          `json:"located-in,omitempty"`
	FixedBy       []contract.FixRef `json:"fixed-by,omitempty"`
	AmendedDesign []string          `json:"amended-design,omitempty"`
}

// ClaimItemRequest records intent to work an item; Steal takes over an
// abandoned claim, attributed in the op.
type ClaimItemRequest struct {
	ID    string `json:"id"`
	Steal bool   `json:"steal,omitempty"`
}

// ReleaseItemRequest hands a claim back.
type ReleaseItemRequest struct {
	ID string `json:"id"`
}

// BlockItemRequest blocks an item on something, remembering its status.
type BlockItemRequest struct {
	ID        string `json:"id"`
	BlockedBy string `json:"blocked-by,omitempty"`
}

// UnblockItemRequest restores the status a block interrupted.
type UnblockItemRequest struct {
	ID string `json:"id"`
}

// LinkItemRequest asserts or retracts a typed edge to another item.
type LinkItemRequest struct {
	ID   string            `json:"id"`
	Type contract.LinkType `json:"type"`
	To   string            `json:"to"`
}

// NoteItemRequest appends a trail entry.
type NoteItemRequest struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// TombstoneItemRequest voids a filing mistake.
type TombstoneItemRequest struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// --- reads -------------------------------------------------------------------

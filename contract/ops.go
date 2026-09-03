package contract

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nuid"
)

// OpType names what happened. Ops are semantic — never "state is now X".
type OpType string

// The op catalog. All but OpRegistered apply to items; OpRegistered applies
// to projects.
const (
	OpCreated      OpType = "created"
	OpNoted        OpType = "noted"
	OpEdited       OpType = "edited"
	OpTransitioned OpType = "transitioned"
	OpClaimed      OpType = "claimed"
	OpReleased     OpType = "released"
	OpBlocked      OpType = "blocked"
	OpUnblocked    OpType = "unblocked"
	OpLinked       OpType = "linked"
	OpUnlinked     OpType = "unlinked"
	OpTombstoned   OpType = "tombstoned"
	OpRegistered   OpType = "registered"
)

// EnvelopeVersion is the current op envelope schema version.
const EnvelopeVersion = 1

// Op is the envelope of one ops-log entry — the platform's wire contract.
// ID doubles as the Nats-Msg-Id for publish deduplication; Entity is the id
// the subject names (item id or project slug).
type Op struct {
	ID      string          `json:"id"`
	Op      OpType          `json:"op"`
	Entity  string          `json:"entity"`
	Actor   string          `json:"actor"`
	At      time.Time       `json:"at"`
	V       int             `json:"v"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// NewOp mints an envelope for one op: fresh unique ID, UTC timestamp, current
// schema version, payload marshalled in place.
func NewOp(op OpType, entity, actor string, payload any) (Op, error) {
	var raw json.RawMessage
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return Op{}, fmt.Errorf("marshal %s payload: %w", op, err)
		}
		raw = b
	}
	return Op{
		ID:      nuid.Next(),
		Op:      op,
		Entity:  entity,
		Actor:   actor,
		At:      time.Now().UTC(),
		V:       EnvelopeVersion,
		Payload: raw,
	}, nil
}

// CreatedPayload opens an item. The reporter is the op's actor; an empty
// priority defaults to Normal.
type CreatedPayload struct {
	Type            Type     `json:"type"`
	Report          string   `json:"report"`
	Priority        Priority `json:"priority,omitempty"`
	LocatedIn       []string `json:"located-in,omitempty"`
	DiscoveredWhile string   `json:"discovered-while,omitempty"`
}

// NotedPayload appends a trail entry; the author is the op's actor.
type NotedPayload struct {
	Text string `json:"text"`
}

// EditedPayload changes properties outside the lifecycle. Nil fields are
// left untouched.
type EditedPayload struct {
	Priority        *Priority `json:"priority,omitempty"`
	LocatedIn       *[]string `json:"located-in,omitempty"`
	DiscoveredWhile *string   `json:"discovered-while,omitempty"`
	Lands           *[]Land   `json:"lands,omitempty"`
}

// TransitionedPayload moves the status. Closing transitions carry the close
// date and the closing refs; a move to Located may carry the location.
type TransitionedPayload struct {
	To            Status   `json:"to"`
	LocatedIn     []string `json:"located-in,omitempty"`
	Closed        string   `json:"closed,omitempty"`
	FixedBy       []FixRef `json:"fixed-by,omitempty"`
	AmendedDesign []string `json:"amended-design,omitempty"`
}

// ClaimedPayload records claim intent; the claimant is the op's actor. On a
// steal, StolenFrom keeps the displaced claimant — attribution survives.
type ClaimedPayload struct {
	Steal      bool   `json:"steal,omitempty"`
	StolenFrom string `json:"stolen-from,omitempty"`
}

// BlockedPayload blocks an item, remembering the status it interrupted so
// unblocking can restore exactly it. BlockedBy is an item ref or prose;
// empty is legal but weak.
type BlockedPayload struct {
	BlockedBy   string `json:"blocked-by,omitempty"`
	Interrupted Status `json:"interrupted"`
}

// LinkedPayload asserts (or, on OpUnlinked, retracts) a typed edge.
type LinkedPayload struct {
	Type LinkType `json:"type"`
	To   string   `json:"to"`
}

// TombstonedPayload voids a filing mistake. Never a lifecycle operation.
type TombstonedPayload struct {
	Reason string `json:"reason"`
}

// RegisteredPayload registers a project; the slug is the op's entity.
type RegisteredPayload struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

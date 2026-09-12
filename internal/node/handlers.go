package node

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/nats-io/nats.go/micro"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/contract"
)

// opTimeout bounds one command's work against the stream and KV.
const opTimeout = 15 * time.Second

// handlers wires the endpoint surface to the store. Every rejection carries
// a machine-legible code: the contract's invariant name, or one of
// invalid-request / not-found / internal.
type handlers struct {
	st *store
}

func opCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), opTimeout)
}

func (h *handlers) respond(req micro.Request, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		_ = req.Error("internal", "encode reply: "+err.Error(), nil)
		return
	}
	_ = req.Respond(data)
}

func (h *handlers) fail(req micro.Request, err error) {
	var ie *contract.InvariantError
	if errors.As(err, &ie) {
		_ = req.Error(ie.Name, ie.Message, nil)
		return
	}
	_ = req.Error("internal", err.Error(), nil)
}

// decodeInto parses the request payload, replying invalid-request on
// malformed input. The bool reports whether the handler should continue.
func decodeInto(req micro.Request, into any) bool {
	if err := json.Unmarshal(req.Data(), into); err != nil {
		_ = req.Error("invalid-request", "malformed request: "+err.Error(), nil)
		return false
	}
	return true
}

func (h *handlers) create(req micro.Request) {
	var r client.CreateItemRequest
	if !decodeInto(req, &r) {
		return
	}
	ctx, cancel := opCtx()
	defer cancel()

	op, err := contract.NewOp(contract.OpCreated, "", r.Actor, contract.CreatedPayload{
		Type:            r.Type,
		Report:          r.Report,
		Priority:        r.Priority,
		Initiative:      r.Initiative,
		LocatedIn:       r.LocatedIn,
		DiscoveredWhile: r.DiscoveredWhile,
	})
	if err != nil {
		h.fail(req, err)
		return
	}
	// Validate before minting so a rejected create burns no number —
	// the initiative check included, since the mint needs it.
	if err := contract.CheckOp(nil, op); err != nil {
		h.fail(req, err)
		return
	}
	if err := h.st.checkInitiativeLive(ctx, r.Initiative); err != nil {
		h.fail(req, err)
		return
	}
	if err := h.st.checkRegistered(ctx, r.LocatedIn, r.Initiative); err != nil {
		h.fail(req, err)
		return
	}
	id, err := h.st.mintID(ctx, r.Initiative)
	if err != nil {
		h.fail(req, err)
		return
	}
	op.Entity = id
	it, err := h.st.publishAndFold(ctx, nil, 0, op)
	if err != nil {
		h.fail(req, err)
		return
	}
	h.respond(req, it)
}

func (h *handlers) get(req micro.Request) {
	var r client.GetItemRequest
	if !decodeInto(req, &r) {
		return
	}
	ctx, cancel := opCtx()
	defer cancel()

	it, _, err := h.st.loadItem(ctx, r.ID)
	if err != nil {
		h.fail(req, err)
		return
	}
	if it == nil {
		_ = req.Error("not-found", "item "+r.ID+" does not exist", nil)
		return
	}
	h.respond(req, it)
}

func (h *handlers) edit(req micro.Request) {
	var r client.EditItemRequest
	if !decodeInto(req, &r) {
		return
	}
	ctx, cancel := opCtx()
	defer cancel()

	if r.Initiative != nil {
		if err := h.st.checkInitiativeLive(ctx, *r.Initiative); err != nil {
			h.fail(req, err)
			return
		}
	}
	if r.LocatedIn != nil {
		// The located-in projects must belong to the item's initiative:
		// the one being assigned in this same edit, or the snapshot's.
		initiative := ""
		if r.Initiative != nil {
			initiative = *r.Initiative
		} else if it, _, err := h.st.loadItem(ctx, r.ID); err != nil {
			h.fail(req, err)
			return
		} else if it != nil {
			initiative = it.Initiative
		}
		if err := h.st.checkRegistered(ctx, *r.LocatedIn, initiative); err != nil {
			h.fail(req, err)
			return
		}
	}
	h.exec(ctx, req, r.ID, func(*contract.Item) (contract.Op, error) {
		return contract.NewOp(contract.OpEdited, r.ID, r.Actor, contract.EditedPayload{
			Priority:        r.Priority,
			Initiative:      r.Initiative,
			LocatedIn:       r.LocatedIn,
			DiscoveredWhile: r.DiscoveredWhile,
			Lands:           r.Lands,
		})
	})
}

func (h *handlers) transition(req micro.Request) {
	var r client.TransitionItemRequest
	if !decodeInto(req, &r) {
		return
	}
	ctx, cancel := opCtx()
	defer cancel()

	if len(r.LocatedIn) > 0 {
		initiative := ""
		if it, _, err := h.st.loadItem(ctx, r.ID); err != nil {
			h.fail(req, err)
			return
		} else if it != nil {
			initiative = it.Initiative
		}
		if err := h.st.checkRegistered(ctx, r.LocatedIn, initiative); err != nil {
			h.fail(req, err)
			return
		}
	}
	h.exec(ctx, req, r.ID, func(*contract.Item) (contract.Op, error) {
		p := contract.TransitionedPayload{
			To:            r.To,
			LocatedIn:     r.LocatedIn,
			FixedBy:       r.FixedBy,
			AmendedDesign: r.AmendedDesign,
		}
		if r.To.Terminal() {
			p.Closed = time.Now().UTC().Format(time.DateOnly)
		}
		return contract.NewOp(contract.OpTransitioned, r.ID, r.Actor, p)
	})
}

func (h *handlers) claim(req micro.Request) {
	var r client.ClaimItemRequest
	if !decodeInto(req, &r) {
		return
	}
	ctx, cancel := opCtx()
	defer cancel()

	h.exec(ctx, req, r.ID, func(current *contract.Item) (contract.Op, error) {
		p := contract.ClaimedPayload{Steal: r.Steal}
		if r.Steal && current != nil && current.Claim != nil {
			p.StolenFrom = current.Claim.By
		}
		return contract.NewOp(contract.OpClaimed, r.ID, r.Actor, p)
	})
}

func (h *handlers) release(req micro.Request) {
	var r client.ReleaseItemRequest
	if !decodeInto(req, &r) {
		return
	}
	ctx, cancel := opCtx()
	defer cancel()

	h.exec(ctx, req, r.ID, func(*contract.Item) (contract.Op, error) {
		return contract.NewOp(contract.OpReleased, r.ID, r.Actor, nil)
	})
}

func (h *handlers) block(req micro.Request) {
	var r client.BlockItemRequest
	if !decodeInto(req, &r) {
		return
	}
	ctx, cancel := opCtx()
	defer cancel()

	h.exec(ctx, req, r.ID, func(current *contract.Item) (contract.Op, error) {
		p := contract.BlockedPayload{BlockedBy: r.BlockedBy}
		if current != nil {
			p.Interrupted = current.Status
		}
		return contract.NewOp(contract.OpBlocked, r.ID, r.Actor, p)
	})
}

func (h *handlers) unblock(req micro.Request) {
	var r client.UnblockItemRequest
	if !decodeInto(req, &r) {
		return
	}
	ctx, cancel := opCtx()
	defer cancel()

	h.exec(ctx, req, r.ID, func(*contract.Item) (contract.Op, error) {
		return contract.NewOp(contract.OpUnblocked, r.ID, r.Actor, nil)
	})
}

func (h *handlers) link(req micro.Request)   { h.linkOp(req, contract.OpLinked) }
func (h *handlers) unlink(req micro.Request) { h.linkOp(req, contract.OpUnlinked) }

func (h *handlers) linkOp(req micro.Request, op contract.OpType) {
	var r client.LinkItemRequest
	if !decodeInto(req, &r) {
		return
	}
	ctx, cancel := opCtx()
	defer cancel()

	h.exec(ctx, req, r.ID, func(*contract.Item) (contract.Op, error) {
		return contract.NewOp(op, r.ID, r.Actor, contract.LinkedPayload{Type: r.Type, To: r.To})
	})
}

func (h *handlers) note(req micro.Request) {
	var r client.NoteItemRequest
	if !decodeInto(req, &r) {
		return
	}
	ctx, cancel := opCtx()
	defer cancel()

	h.exec(ctx, req, r.ID, func(*contract.Item) (contract.Op, error) {
		return contract.NewOp(contract.OpNoted, r.ID, r.Actor, contract.NotedPayload{Text: r.Text})
	})
}

func (h *handlers) tombstone(req micro.Request) {
	var r client.TombstoneItemRequest
	if !decodeInto(req, &r) {
		return
	}
	ctx, cancel := opCtx()
	defer cancel()

	h.exec(ctx, req, r.ID, func(*contract.Item) (contract.Op, error) {
		return contract.NewOp(contract.OpTombstoned, r.ID, r.Actor, contract.TombstonedPayload{Reason: r.Reason})
	})
}

func (h *handlers) registerProject(req micro.Request) {
	var r client.RegisterProjectRequest
	if !decodeInto(req, &r) {
		return
	}
	ctx, cancel := opCtx()
	defer cancel()

	op, err := contract.NewOp(contract.OpRegistered, r.Slug, r.Actor, contract.RegisteredPayload{
		Name:        r.Name,
		Description: r.Description,
		Initiative:  r.Initiative,
	})
	if err != nil {
		h.fail(req, err)
		return
	}
	// The payload check requires an initiative; the registry check makes
	// it a real one.
	if r.Initiative != "" {
		if err := h.st.checkInitiativeLive(ctx, r.Initiative); err != nil {
			h.fail(req, err)
			return
		}
	}
	p, err := h.st.registerProject(ctx, op)
	if err != nil {
		h.fail(req, err)
		return
	}
	h.respond(req, p)
}

func (h *handlers) assignProject(req micro.Request) {
	var r client.AssignProjectRequest
	if !decodeInto(req, &r) {
		return
	}
	ctx, cancel := opCtx()
	defer cancel()

	op, err := contract.NewOp(contract.OpAssigned, r.Slug, r.Actor, contract.AssignedPayload{
		Initiative: r.Initiative,
	})
	if err != nil {
		h.fail(req, err)
		return
	}
	if r.Initiative != "" {
		if err := h.st.checkInitiativeLive(ctx, r.Initiative); err != nil {
			h.fail(req, err)
			return
		}
	}
	p, err := h.st.assignProject(ctx, op)
	if err != nil {
		h.fail(req, err)
		return
	}
	h.respond(req, p)
}

func (h *handlers) registerInitiative(req micro.Request) {
	var r client.RegisterInitiativeRequest
	if !decodeInto(req, &r) {
		return
	}
	ctx, cancel := opCtx()
	defer cancel()

	op, err := contract.NewOp(contract.OpRegistered, r.Slug, r.Actor, contract.RegisteredPayload{
		Name:        r.Name,
		Description: r.Description,
	})
	if err != nil {
		h.fail(req, err)
		return
	}
	i, err := h.st.registerInitiative(ctx, op)
	if err != nil {
		h.fail(req, err)
		return
	}
	h.respond(req, i)
}

func (h *handlers) retireInitiative(req micro.Request) {
	var r client.RetireInitiativeRequest
	if !decodeInto(req, &r) {
		return
	}
	ctx, cancel := opCtx()
	defer cancel()

	op, err := contract.NewOp(contract.OpRetired, r.Slug, r.Actor, contract.RetiredPayload{
		Reason: r.Reason,
	})
	if err != nil {
		h.fail(req, err)
		return
	}
	i, err := h.st.retireInitiative(ctx, op)
	if err != nil {
		h.fail(req, err)
		return
	}
	h.respond(req, i)
}

func (h *handlers) listInitiatives(req micro.Request) {
	ctx, cancel := opCtx()
	defer cancel()

	is, err := h.st.listInitiatives(ctx)
	if err != nil {
		h.fail(req, err)
		return
	}
	h.respond(req, is)
}

func (h *handlers) retireProject(req micro.Request) {
	var r client.RetireProjectRequest
	if !decodeInto(req, &r) {
		return
	}
	ctx, cancel := opCtx()
	defer cancel()

	op, err := contract.NewOp(contract.OpRetired, r.Slug, r.Actor, contract.RetiredPayload{
		Reason: r.Reason,
	})
	if err != nil {
		h.fail(req, err)
		return
	}
	p, err := h.st.retireProject(ctx, op)
	if err != nil {
		h.fail(req, err)
		return
	}
	h.respond(req, p)
}

func (h *handlers) listProjects(req micro.Request) {
	ctx, cancel := opCtx()
	defer cancel()

	ps, err := h.st.listProjects(ctx)
	if err != nil {
		h.fail(req, err)
		return
	}
	h.respond(req, ps)
}

// exec runs one item command through the store's write path and replies
// with the new snapshot.
func (h *handlers) exec(ctx context.Context, req micro.Request, id string, build func(*contract.Item) (contract.Op, error)) {
	it, err := h.st.execItem(ctx, id, build)
	if err != nil {
		h.fail(req, err)
		return
	}
	h.respond(req, it)
}

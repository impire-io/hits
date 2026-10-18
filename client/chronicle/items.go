package chronicle

import (
	"context"
	"fmt"
	"sort"
	"time"

	chron "github.com/impire-io/chronicle/client"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/contract"
)

// The item requests. The actor is never a field: every write's author is
// the principal the client connected as (hits-hq decision 0021).

// loadItem reads an item's state and the sequence of its last operation;
// nil when the item has no state. Notes are not read here.
func (c *Client) loadItem(ctx context.Context, id string) (*contract.Item, head, error) {
	h, err := c.head(ctx, contract.ItemPath(id))
	if err != nil || !h.exists {
		return nil, h, err
	}
	it, err := contract.ItemFromState(id, h.state)
	if err != nil {
		return nil, h, err
	}
	it.Seq = h.stateSeq
	return it, h, nil
}

// notes reads an item's trail from its history: every note with the
// author and time chronicle recorded on the operation.
func (c *Client) notes(ctx context.Context, id string) ([]contract.Note, error) {
	var notes []contract.Note
	for op, err := range c.cc.History(ctx, c.store, contract.ItemPath(id)) {
		if err != nil {
			return nil, err
		}
		if op.Type != "note" {
			continue
		}
		var p contract.NotedPayload
		if err := jsonDecode(op.Payload, &p); err != nil {
			continue
		}
		notes = append(notes, contract.Note{Author: op.Author, At: op.Ts, Text: p.Text})
	}
	return notes, nil
}

// GetItem reads one item: its state and its trail. A tombstoned item is
// not found, as every view drops it.
func (c *Client) GetItem(ctx context.Context, id string) (contract.Item, error) {
	it, _, err := c.loadItem(ctx, id)
	if err != nil {
		return contract.Item{}, refusal(err)
	}
	if it == nil || it.Tombstoned {
		return contract.Item{}, &client.APIError{Code: "not-found", Message: fmt.Sprintf("item %s does not exist", id)}
	}
	notes, err := c.notes(ctx, id)
	if err != nil {
		return contract.Item{}, refusal(err)
	}
	it.Notes = notes
	return *it, nil
}

// ListItems lists every item of the store from its state, tombstoned ones
// dropped, without their trails — the status view's read.
func (c *Client) ListItems(ctx context.Context) ([]contract.Item, error) {
	return c.listItems(ctx, false)
}

// ListAllItems is ListItems with the tombstoned items kept, for the one
// reader that must see them: the audit, which holds the whole numbering
// and every merged branch against the record.
func (c *Client) ListAllItems(ctx context.Context) ([]contract.Item, error) {
	return c.listItems(ctx, true)
}

func (c *Client) listItems(ctx context.Context, tombstoned bool) ([]contract.Item, error) {
	var items []contract.Item
	for info, err := range c.cc.ListInstances(ctx, c.store, chron.ByType(contract.TypeItem)) {
		if err != nil {
			return nil, refusal(err)
		}
		_, id := contract.ParsePath(info.Path)
		it, err := contract.ItemFromState(id, info.State)
		if err != nil {
			return nil, err
		}
		if it.Tombstoned && !tombstoned {
			continue
		}
		it.Seq = info.Seq
		items = append(items, *it)
	}
	sort.Slice(items, func(a, b int) bool { return items[a].ID < items[b].ID })
	return items, nil
}

// --- writes ------------------------------------------------------------------

// exec is the write loop of the-client.md for one item: read, check, fold,
// send the difference under the guard; on a guard miss, read again.
func (c *Client) exec(ctx context.Context, id string, build func(current *contract.Item) (contract.Op, error)) (contract.Item, error) {
	for attempt := 0; ; attempt++ {
		current, h, err := c.loadItem(ctx, id)
		if err != nil {
			return contract.Item{}, refusal(err)
		}
		op, err := build(current)
		if err != nil {
			return contract.Item{}, refusal(err)
		}
		if err := contract.CheckOp(current, op); err != nil {
			return contract.Item{}, refusal(err)
		}
		next, err := contract.Apply(current, op, h.last+1)
		if err != nil {
			return contract.Item{}, refusal(err)
		}
		opType, err := contract.ItemOperation(op)
		if err != nil {
			return contract.Item{}, err
		}
		var data any
		switch op.Op {
		case contract.OpNoted:
			var p contract.NotedPayload
			if err := jsonDecode(op.Payload, &p); err != nil {
				return contract.Item{}, err
			}
			data = map[string]any{"text": p.Text}
		default:
			data = contract.Diff(contract.ItemState(current), contract.ItemState(next))
		}
		err = c.send(ctx, write{path: contract.ItemPath(id), opType: opType, data: data, opID: op.ID, expect: h.last})
		if moved(err) && attempt < maxRetries {
			continue
		}
		if err != nil {
			return contract.Item{}, refusal(err)
		}
		return c.GetItem(ctx, id)
	}
}

// CreateItem files an item: the checks the fleet made before minting — the
// payload, the initiative, the projects, the target — then the mint on
// the initiative's counter and the create with the create-only guard.
func (c *Client) CreateItem(ctx context.Context, r client.CreateItemRequest) (contract.Item, error) {
	op, err := contract.NewOp(contract.OpCreated, "", c.Author(), contract.CreatedPayload{
		Type: r.Type, Report: r.Report, Priority: r.Priority, Initiative: r.Initiative,
		Target: r.Target, LocatedIn: r.LocatedIn, DiscoveredWhile: r.DiscoveredWhile,
	})
	if err != nil {
		return contract.Item{}, err
	}
	if err := contract.CheckOp(nil, op); err != nil {
		return contract.Item{}, refusal(err)
	}
	if err := c.checkInitiativeLive(ctx, r.Initiative); err != nil {
		return contract.Item{}, refusal(err)
	}
	if err := c.checkRegistered(ctx, r.LocatedIn, r.Initiative); err != nil {
		return contract.Item{}, refusal(err)
	}
	if r.Target != "" {
		if err := c.checkTargetLive(ctx, r.Initiative, r.Target); err != nil {
			return contract.Item{}, refusal(err)
		}
	}
	for attempt := 0; ; attempt++ {
		id, err := c.mint(ctx, r.Initiative)
		if err != nil {
			return contract.Item{}, refusal(err)
		}
		op.Entity = id
		it, err := contract.Apply(nil, op, 1)
		if err != nil {
			return contract.Item{}, err
		}
		err = c.send(ctx, write{path: contract.ItemPath(id), opType: "create", data: contract.ItemState(it), opID: op.ID, create: true})
		if isExists(err) && attempt < maxRetries {
			continue // the number was taken under us: mint again
		}
		if err != nil {
			return contract.Item{}, refusal(err)
		}
		return c.GetItem(ctx, id)
	}
}

// mint takes the next number of an initiative's counter: a guarded write
// of next+1 on the initiative, read again on a miss (the-store.md § the
// counter). A crash between the mint and the create leaves a gap, never a
// reuse.
func (c *Client) mint(ctx context.Context, initiative string) (string, error) {
	path := contract.InitiativePath(initiative)
	for attempt := 0; ; attempt++ {
		h, err := c.head(ctx, path)
		if err != nil {
			return "", err
		}
		if !h.exists {
			return "", inv("unregistered-initiative", "%q is not a registered initiative", initiative)
		}
		_, next, err := contract.InitiativeFromState(initiative, h.state)
		if err != nil {
			return "", err
		}
		mintOp, err := contract.NewOp("mint", initiative, c.Author(), nil)
		if err != nil {
			return "", err
		}
		err = c.send(ctx, write{path: path, opType: "mint", data: map[string]any{"next": next + 1}, opID: mintOp.ID, expect: h.last})
		if moved(err) && attempt < maxRetries {
			continue
		}
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s-%d", initiative, next), nil
	}
}

// EditItem changes non-lifecycle properties, with the registry checks the
// fleet made: a live initiative on assignment, registered projects and a
// live release of the item's initiative.
func (c *Client) EditItem(ctx context.Context, r client.EditItemRequest) (contract.Item, error) {
	if r.Initiative != nil {
		if err := c.checkInitiativeLive(ctx, *r.Initiative); err != nil {
			return contract.Item{}, refusal(err)
		}
	}
	if r.LocatedIn != nil || (r.Target != nil && *r.Target != "") {
		initiative := ""
		if r.Initiative != nil {
			initiative = *r.Initiative
		} else if it, _, err := c.loadItem(ctx, r.ID); err != nil {
			return contract.Item{}, refusal(err)
		} else if it != nil {
			initiative = it.Initiative
		}
		if r.LocatedIn != nil {
			if err := c.checkRegistered(ctx, *r.LocatedIn, initiative); err != nil {
				return contract.Item{}, refusal(err)
			}
		}
		if r.Target != nil && *r.Target != "" {
			if err := c.checkTargetLive(ctx, initiative, *r.Target); err != nil {
				return contract.Item{}, refusal(err)
			}
		}
	}
	return c.exec(ctx, r.ID, func(*contract.Item) (contract.Op, error) {
		return contract.NewOp(contract.OpEdited, r.ID, c.Author(), contract.EditedPayload{
			Priority: r.Priority, Initiative: r.Initiative, Target: r.Target,
			LocatedIn: r.LocatedIn, DiscoveredWhile: r.DiscoveredWhile, Lands: r.Lands,
		})
	})
}

// TransitionItem moves the status; a closing transition stamps the date.
func (c *Client) TransitionItem(ctx context.Context, r client.TransitionItemRequest) (contract.Item, error) {
	if len(r.LocatedIn) > 0 {
		initiative := ""
		if it, _, err := c.loadItem(ctx, r.ID); err != nil {
			return contract.Item{}, refusal(err)
		} else if it != nil {
			initiative = it.Initiative
		}
		if err := c.checkRegistered(ctx, r.LocatedIn, initiative); err != nil {
			return contract.Item{}, refusal(err)
		}
	}
	return c.exec(ctx, r.ID, func(*contract.Item) (contract.Op, error) {
		p := contract.TransitionedPayload{To: r.To, LocatedIn: r.LocatedIn, FixedBy: r.FixedBy, AmendedDesign: r.AmendedDesign}
		if r.To.Terminal() {
			p.Closed = time.Now().UTC().Format(time.DateOnly)
		}
		return contract.NewOp(contract.OpTransitioned, r.ID, c.Author(), p)
	})
}

// ClaimItem records intent to work the item; a steal names whom it was
// taken from.
func (c *Client) ClaimItem(ctx context.Context, r client.ClaimItemRequest) (contract.Item, error) {
	return c.exec(ctx, r.ID, func(current *contract.Item) (contract.Op, error) {
		p := contract.ClaimedPayload{Steal: r.Steal}
		if r.Steal && current != nil && current.Claim != nil {
			p.StolenFrom = current.Claim.By
		}
		return contract.NewOp(contract.OpClaimed, r.ID, c.Author(), p)
	})
}

// ReleaseItem hands the claim back.
func (c *Client) ReleaseItem(ctx context.Context, r client.ReleaseItemRequest) (contract.Item, error) {
	return c.exec(ctx, r.ID, func(*contract.Item) (contract.Op, error) {
		return contract.NewOp(contract.OpReleased, r.ID, c.Author(), nil)
	})
}

// BlockItem blocks the item, remembering the status it interrupts.
func (c *Client) BlockItem(ctx context.Context, r client.BlockItemRequest) (contract.Item, error) {
	return c.exec(ctx, r.ID, func(current *contract.Item) (contract.Op, error) {
		p := contract.BlockedPayload{BlockedBy: r.BlockedBy}
		if current != nil {
			p.Interrupted = current.Status
		}
		return contract.NewOp(contract.OpBlocked, r.ID, c.Author(), p)
	})
}

// UnblockItem restores the interrupted status.
func (c *Client) UnblockItem(ctx context.Context, r client.UnblockItemRequest) (contract.Item, error) {
	return c.exec(ctx, r.ID, func(*contract.Item) (contract.Op, error) {
		return contract.NewOp(contract.OpUnblocked, r.ID, c.Author(), nil)
	})
}

// LinkItem asserts a typed edge.
func (c *Client) LinkItem(ctx context.Context, r client.LinkItemRequest) (contract.Item, error) {
	return c.linkOp(ctx, r, contract.OpLinked)
}

// UnlinkItem retracts a typed edge.
func (c *Client) UnlinkItem(ctx context.Context, r client.LinkItemRequest) (contract.Item, error) {
	return c.linkOp(ctx, r, contract.OpUnlinked)
}

func (c *Client) linkOp(ctx context.Context, r client.LinkItemRequest, opType contract.OpType) (contract.Item, error) {
	return c.exec(ctx, r.ID, func(*contract.Item) (contract.Op, error) {
		return contract.NewOp(opType, r.ID, c.Author(), contract.LinkedPayload{Type: r.Type, To: r.To})
	})
}

// NoteItem appends a trail entry: an operation with effect none, whose
// author and time are the record's.
func (c *Client) NoteItem(ctx context.Context, r client.NoteItemRequest) (contract.Item, error) {
	return c.exec(ctx, r.ID, func(*contract.Item) (contract.Op, error) {
		return contract.NewOp(contract.OpNoted, r.ID, c.Author(), contract.NotedPayload{Text: r.Text})
	})
}

// TombstoneItem voids a filing mistake; the item leaves every view, and
// the reply is the voided item itself, once.
func (c *Client) TombstoneItem(ctx context.Context, r client.TombstoneItemRequest) (contract.Item, error) {
	it, err := c.exec(ctx, r.ID, func(*contract.Item) (contract.Op, error) {
		return contract.NewOp(contract.OpTombstoned, r.ID, c.Author(), contract.TombstonedPayload{Reason: r.Reason})
	})
	var apiErr *client.APIError
	if err != nil && errorsAs(err, &apiErr) && apiErr.Code == "not-found" {
		return c.tombstoned(ctx, r.ID)
	}
	return it, err
}

// tombstoned reads a tombstoned item back for the one caller that may:
// the tombstone itself, so it can echo what it voided.
func (c *Client) tombstoned(ctx context.Context, id string) (contract.Item, error) {
	it, _, err := c.loadItem(ctx, id)
	if err != nil {
		return contract.Item{}, refusal(err)
	}
	if it == nil {
		return contract.Item{}, &client.APIError{Code: "not-found", Message: fmt.Sprintf("item %s does not exist", id)}
	}
	return *it, nil
}

// --- the checks a precondition cannot make -----------------------------------

// checkInitiativeLive refuses an unregistered or retired initiative.
func (c *Client) checkInitiativeLive(ctx context.Context, slug string) error {
	i, _, err := c.loadInitiative(ctx, slug)
	if err != nil {
		return err
	}
	if i == nil {
		return inv("unregistered-initiative", "%q is not a registered initiative", slug)
	}
	if i.Retired {
		return inv("retired-initiative", "initiative %q is retired", slug)
	}
	return nil
}

// checkRegistered refuses located-in values that are not registered,
// unretired projects of the item's own initiative.
func (c *Client) checkRegistered(ctx context.Context, locatedIn []string, initiative string) error {
	for _, slug := range locatedIn {
		p, _, err := c.loadProject(ctx, slug)
		if err != nil {
			return err
		}
		if p == nil {
			return inv("unregistered-project", "located-in names %q, which is not a registered project", slug)
		}
		if p.Retired {
			return inv("retired-project", "located-in names %q, which is retired", slug)
		}
		if initiative != "" && p.Initiative != "" && p.Initiative != initiative {
			return inv("initiative-mismatch", "located-in names %q of initiative %s; the item is in %s — an item cannot span initiatives", slug, p.Initiative, initiative)
		}
	}
	return nil
}

// checkTargetLive refuses a target that is not a registered, unshipped,
// unretired release of the item's initiative.
func (c *Client) checkTargetLive(ctx context.Context, initiative, slug string) error {
	if !contract.ValidReleaseSlug(slug) {
		return inv("invalid-release", "%q is not a well-formed release slug", slug)
	}
	if initiative == "" {
		return inv("initiative-required", "an item targets releases of its own initiative; assign the initiative first")
	}
	r, _, err := c.loadRelease(ctx, initiative, slug)
	if err != nil {
		return err
	}
	if r == nil {
		return inv("unknown-release", "target names %q, which is not a registered release of initiative %s", slug, initiative)
	}
	if r.Shipped {
		return inv("release-shipped", "release %s of initiative %s is shipped", slug, initiative)
	}
	if r.Retired {
		return inv("release-retired", "release %s of initiative %s is retired", slug, initiative)
	}
	return nil
}

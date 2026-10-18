package fleet

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/contract"
)

// listFan is the window of concurrent gets a listing keeps in flight:
// the fleet has no listing endpoint, so a listing walks the IDs each
// initiative minted, as the 0.7 audit did.
const listFan = 8

// GetItem reads one item. The 0.7 node answers a tombstoned item as it
// is; the surface says a tombstoned item is not found (the-client.md §
// reads), so the client says so on both substrates.
func (c *Client) GetItem(ctx context.Context, id string) (contract.Item, error) {
	it, err := c.get(ctx, id)
	if err != nil {
		return contract.Item{}, err
	}
	if it.Tombstoned {
		return contract.Item{}, &client.APIError{Code: "not-found", Message: "item " + id + " is tombstoned"}
	}
	return it, nil
}

// get is the node's answer as it is, tombstoned items included — the
// listing walks over them.
func (c *Client) get(ctx context.Context, id string) (contract.Item, error) {
	return request[contract.Item](ctx, c, GetSubject, client.GetItemRequest{ID: id})
}

// ListItems is every item that is not tombstoned.
func (c *Client) ListItems(ctx context.Context) ([]contract.Item, error) {
	all, err := c.ListAllItems(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]contract.Item, 0, len(all))
	for _, it := range all {
		if !it.Tombstoned {
			items = append(items, it)
		}
	}
	return items, nil
}

// ListAllItems walks the IDs: the legacy bare numbers, then each live
// initiative's, until the first number nobody minted.
func (c *Client) ListAllItems(ctx context.Context) ([]contract.Item, error) {
	initiatives, err := c.ListInitiatives(ctx)
	if err != nil {
		return nil, err
	}
	prefixes := []string{""}
	for _, in := range initiatives {
		prefixes = append(prefixes, in.Slug)
	}
	items := []contract.Item{}
	for _, prefix := range prefixes {
		walked, err := c.walk(ctx, prefix)
		if err != nil {
			return nil, err
		}
		items = append(items, walked...)
	}
	return items, nil
}

func (c *Client) walk(ctx context.Context, prefix string) ([]contract.Item, error) {
	id := func(n int) string {
		if prefix == "" {
			return strconv.Itoa(n)
		}
		return prefix + "-" + strconv.Itoa(n)
	}
	var items []contract.Item
	for base := 1; ; base += listFan {
		batch := make([]*contract.Item, listFan)
		errs := make([]error, listFan)
		var wg sync.WaitGroup
		for i := range listFan {
			wg.Add(1)
			go func() {
				defer wg.Done()
				it, err := c.get(ctx, id(base+i))
				var apiErr *client.APIError
				switch {
				case err == nil:
					batch[i] = &it
				case errors.As(err, &apiErr) && apiErr.Code == "not-found":
				default:
					errs[i] = fmt.Errorf("get %s: %w", id(base+i), err)
				}
			}()
		}
		wg.Wait()
		for i, it := range batch {
			if it == nil {
				if errs[i] != nil {
					return nil, errs[i]
				}
				return items, nil
			}
			items = append(items, *it)
		}
	}
}

// CreateItem files an item through the node, as the client's actor.
func (c *Client) CreateItem(ctx context.Context, r client.CreateItemRequest) (contract.Item, error) {
	return write[contract.Item](ctx, c, CreateSubject, r)
}

// EditItem changes non-lifecycle properties through the node.
func (c *Client) EditItem(ctx context.Context, r client.EditItemRequest) (contract.Item, error) {
	return write[contract.Item](ctx, c, EditSubject, r)
}

// TransitionItem moves the status through the node.
func (c *Client) TransitionItem(ctx context.Context, r client.TransitionItemRequest) (contract.Item, error) {
	return write[contract.Item](ctx, c, TransitionSubject, r)
}

// ClaimItem records intent to work, or steals, through the node.
func (c *Client) ClaimItem(ctx context.Context, r client.ClaimItemRequest) (contract.Item, error) {
	return write[contract.Item](ctx, c, ClaimSubject, r)
}

// ReleaseItem hands a claim back through the node.
func (c *Client) ReleaseItem(ctx context.Context, r client.ReleaseItemRequest) (contract.Item, error) {
	return write[contract.Item](ctx, c, ReleaseSubject, r)
}

// BlockItem blocks an item through the node.
func (c *Client) BlockItem(ctx context.Context, r client.BlockItemRequest) (contract.Item, error) {
	return write[contract.Item](ctx, c, BlockSubject, r)
}

// UnblockItem restores the status a block interrupted.
func (c *Client) UnblockItem(ctx context.Context, r client.UnblockItemRequest) (contract.Item, error) {
	return write[contract.Item](ctx, c, UnblockSubject, r)
}

// LinkItem asserts a typed edge through the node.
func (c *Client) LinkItem(ctx context.Context, r client.LinkItemRequest) (contract.Item, error) {
	return write[contract.Item](ctx, c, LinkSubject, r)
}

// UnlinkItem retracts a typed edge through the node.
func (c *Client) UnlinkItem(ctx context.Context, r client.LinkItemRequest) (contract.Item, error) {
	return write[contract.Item](ctx, c, UnlinkSubject, r)
}

// NoteItem appends a trail entry through the node.
func (c *Client) NoteItem(ctx context.Context, r client.NoteItemRequest) (contract.Item, error) {
	return write[contract.Item](ctx, c, NoteSubject, r)
}

// TombstoneItem voids a filing mistake through the node.
func (c *Client) TombstoneItem(ctx context.Context, r client.TombstoneItemRequest) (contract.Item, error) {
	return write[contract.Item](ctx, c, TombstoneSubject, r)
}

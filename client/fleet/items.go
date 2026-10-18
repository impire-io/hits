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

func (c *Client) GetItem(ctx context.Context, id string) (contract.Item, error) {
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
				it, err := c.GetItem(ctx, id(base+i))
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

func (c *Client) CreateItem(ctx context.Context, r client.CreateItemRequest) (contract.Item, error) {
	return write[contract.Item](ctx, c, CreateSubject, r)
}

func (c *Client) EditItem(ctx context.Context, r client.EditItemRequest) (contract.Item, error) {
	return write[contract.Item](ctx, c, EditSubject, r)
}

func (c *Client) TransitionItem(ctx context.Context, r client.TransitionItemRequest) (contract.Item, error) {
	return write[contract.Item](ctx, c, TransitionSubject, r)
}

func (c *Client) ClaimItem(ctx context.Context, r client.ClaimItemRequest) (contract.Item, error) {
	return write[contract.Item](ctx, c, ClaimSubject, r)
}

func (c *Client) ReleaseItem(ctx context.Context, r client.ReleaseItemRequest) (contract.Item, error) {
	return write[contract.Item](ctx, c, ReleaseSubject, r)
}

func (c *Client) BlockItem(ctx context.Context, r client.BlockItemRequest) (contract.Item, error) {
	return write[contract.Item](ctx, c, BlockSubject, r)
}

func (c *Client) UnblockItem(ctx context.Context, r client.UnblockItemRequest) (contract.Item, error) {
	return write[contract.Item](ctx, c, UnblockSubject, r)
}

func (c *Client) LinkItem(ctx context.Context, r client.LinkItemRequest) (contract.Item, error) {
	return write[contract.Item](ctx, c, LinkSubject, r)
}

func (c *Client) UnlinkItem(ctx context.Context, r client.LinkItemRequest) (contract.Item, error) {
	return write[contract.Item](ctx, c, UnlinkSubject, r)
}

func (c *Client) NoteItem(ctx context.Context, r client.NoteItemRequest) (contract.Item, error) {
	return write[contract.Item](ctx, c, NoteSubject, r)
}

func (c *Client) TombstoneItem(ctx context.Context, r client.TombstoneItemRequest) (contract.Item, error) {
	return write[contract.Item](ctx, c, TombstoneSubject, r)
}

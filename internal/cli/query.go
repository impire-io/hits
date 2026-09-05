package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/contract"
)

func runSearch(inv *invocation) error {
	fs := inv.flagSet("search", "search [<query>] [flags]")
	typ := fs.String("type", "", "filter by item type")
	status := fs.String("status", "", "filter by status")
	limit := fs.Int("limit", 0, "page size (service default 10, capped at 100)")
	offset := fs.Int("offset", 0, "page start")
	var columns multiFlag
	fs.Var(&columns, "columns", "table columns, comma-separated (repeatable); default: every populated field")

	var query string
	args := inv.args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		query, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := noTrailing(fs); err != nil {
		return err
	}
	cols, err := parseColumns(columns)
	if err != nil {
		return err
	}

	c, closeConn, err := inv.dial()
	if err != nil {
		return err
	}
	defer closeConn()
	reply, err := c.SearchItems(inv.ctx, client.SearchRequest{
		Query:  query,
		Type:   contract.Type(*typ),
		Status: contract.Status(*status),
		Limit:  *limit,
		Offset: *offset,
	})
	if err != nil {
		return err
	}
	rows, err := fetchRows(inv.ctx, c, reply.Hits)
	if err != nil {
		return err
	}
	return inv.printSearchTable(rows, reply.Total, cols)
}

// fetchRows resolves each hit to its item snapshot — the index is never
// authority; state comes from the hits service. At most eight gets are in
// flight at once, and hit order is preserved. A hit whose item is gone (a
// tombstone race) keeps its id and score with no snapshot; any other failure
// fails the whole command rather than presenting a silently partial table.
func fetchRows(ctx context.Context, c *client.Client, hits []client.SearchHit) ([]searchRow, error) {
	rows := make([]searchRow, len(hits))
	errs := make([]error, len(hits))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, h := range hits {
		rows[i] = searchRow{ID: h.ID, Score: h.Score}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			it, err := c.GetItem(ctx, h.ID)
			var apiErr *client.APIError
			switch {
			case err == nil:
				rows[i].Item = &it
			case errors.As(err, &apiErr) && apiErr.Code == "not-found":
				// gone between the search and the get: id and score stand
			default:
				errs[i] = fmt.Errorf("get %s: %w", h.ID, err)
			}
		}()
	}
	wg.Wait()
	return rows, errors.Join(errs...)
}

func runSemantic(inv *invocation) error {
	fs := inv.flagSet("semantic", "semantic <text> [--limit <n>]")
	limit := fs.Int("limit", 0, "result count (service default 10, capped at 100)")
	text, rest, err := leadingID(fs, inv.args, "<text>")
	if err != nil {
		return err
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := noTrailing(fs); err != nil {
		return err
	}

	c, closeConn, err := inv.dial()
	if err != nil {
		return err
	}
	defer closeConn()
	reply, err := c.SemanticSearch(inv.ctx, client.SemanticRequest{Text: text, Limit: *limit})
	if err != nil {
		return err
	}
	return inv.printSemantic(reply)
}

func runGraph(inv *invocation) error {
	if len(inv.args) == 0 {
		return errors.New(`graph: want "neighbors" or "walk"`)
	}
	sub, rest := inv.args[0], inv.args[1:]
	switch sub {
	case "neighbors":
		return runGraphNeighbors(inv, rest)
	case "walk":
		return runGraphWalk(inv, rest)
	default:
		return fmt.Errorf(`graph: unknown subcommand %q, want "neighbors" or "walk"`, sub)
	}
}

func runGraphNeighbors(inv *invocation, args []string) error {
	fs := inv.flagSet("graph neighbors", "graph neighbors <id> [flags]")
	kind := fs.String("kind", "item", "node kind: item, project, or actor")
	direction := fs.String("direction", "", "edge direction: out, in, or both (the default)")
	var types multiFlag
	fs.Var(&types, "type", "narrow to the named edge types (repeatable)")
	id, rest, err := leadingID(fs, args, "<id>")
	if err != nil {
		return err
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := noTrailing(fs); err != nil {
		return err
	}

	c, closeConn, err := inv.dial()
	if err != nil {
		return err
	}
	defer closeConn()
	reply, err := c.GraphNeighbors(inv.ctx, client.NeighborsRequest{
		Kind:      client.NodeKind(*kind),
		ID:        id,
		Direction: *direction,
		Types:     types,
	})
	if err != nil {
		return err
	}
	return inv.printNeighbors(reply)
}

func runGraphWalk(inv *invocation, args []string) error {
	fs := inv.flagSet("graph walk", "graph walk <id> [flags]")
	kind := fs.String("kind", "item", "node kind: item, project, or actor")
	depth := fs.Int("depth", 0, "expansion depth (service default 2, capped)")
	direction := fs.String("direction", "", "edge direction: out, in, or both (the default)")
	var types multiFlag
	fs.Var(&types, "type", "narrow to the named edge types (repeatable)")
	id, rest, err := leadingID(fs, args, "<id>")
	if err != nil {
		return err
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := noTrailing(fs); err != nil {
		return err
	}

	c, closeConn, err := inv.dial()
	if err != nil {
		return err
	}
	defer closeConn()
	reply, err := c.GraphWalk(inv.ctx, client.WalkRequest{
		Kind:      client.NodeKind(*kind),
		ID:        id,
		Depth:     *depth,
		Direction: *direction,
		Types:     types,
	})
	if err != nil {
		return err
	}
	return inv.printWalk(reply)
}

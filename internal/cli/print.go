package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/contract"
)

// emit is the --json path: the service reply re-encoded, indented.
func emit(out io.Writer, v any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// field prints one labeled line, skipping empty values.
func field(w io.Writer, name, value string) {
	if value != "" {
		fmt.Fprintf(w, "  %s: %s\n", name, value)
	}
}

func (inv *invocation) printItem(it contract.Item) error {
	if inv.json {
		return emit(inv.out, it)
	}
	w := inv.out
	fmt.Fprintf(w, "%s  %s  %s  %s\n", it.ID, it.Type, it.Status, it.Priority)
	field(w, "report", it.Report)
	field(w, "reporter", it.Reporter)
	field(w, "created", it.Created.Format(time.RFC3339))
	if it.Claim != nil {
		c := fmt.Sprintf("%s  (%s)", it.Claim.By, it.Claim.At.Format(time.RFC3339))
		if it.Claim.StolenFrom != "" {
			c += "  stolen-from: " + it.Claim.StolenFrom
		}
		field(w, "claimed-by", c)
	}
	field(w, "blocked-by", it.BlockedBy)
	field(w, "interrupted", string(it.Interrupted))
	field(w, "located-in", strings.Join(it.LocatedIn, ", "))
	field(w, "discovered-while", it.DiscoveredWhile)
	for _, l := range it.Lands {
		field(w, "lands", landString(l))
	}
	for _, f := range it.FixedBy {
		field(w, "fixed-by", fixRefString(f))
	}
	field(w, "amended-design", strings.Join(it.AmendedDesign, ", "))
	field(w, "closed", it.Closed)
	for _, l := range it.Links {
		field(w, "link", fmt.Sprintf("%s %s", l.Type, l.To))
	}
	if it.Tombstoned {
		field(w, "tombstoned", it.TombstoneReason)
	}
	if len(it.Notes) > 0 {
		fmt.Fprintln(w, "  notes:")
		for _, n := range it.Notes {
			fmt.Fprintf(w, "    %s  %s\n      %s\n", n.Author, n.At.Format(time.RFC3339), n.Text)
		}
	}
	return nil
}

func fixRefString(f contract.FixRef) string {
	var s string
	switch {
	case f.PR != "":
		s = "pr:" + f.PR
	case f.Commit != "":
		s = "commit:" + f.Commit
	case f.Action != "":
		s = "action:" + f.Action
	}
	if f.Note != "" {
		s += " — " + f.Note
	}
	return s
}

func landString(l contract.Land) string {
	s := l.Repo + ":" + l.PR
	if len(l.After) > 0 {
		s += " after " + strings.Join(l.After, ", ")
	}
	if l.Closes {
		s += " closes"
	}
	return s
}

func (inv *invocation) printProject(p contract.Project) error {
	if inv.json {
		return emit(inv.out, p)
	}
	fmt.Fprintf(inv.out, "%s  %s\n", p.Slug, p.Name)
	field(inv.out, "description", p.Description)
	return nil
}

func (inv *invocation) printProjects(ps []contract.Project) error {
	if inv.json {
		return emit(inv.out, ps)
	}
	tw := tabwriter.NewWriter(inv.out, 0, 0, 2, ' ', 0)
	for _, p := range ps {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", p.Slug, p.Name, p.Description)
	}
	return tw.Flush()
}

// searchRow pairs one search hit with its item snapshot. A nil item is a hit
// whose snapshot vanished between the search and the get (a tombstone race):
// the row keeps its id and score, its item cells stay blank.
type searchRow struct {
	ID    string         `json:"id"`
	Score float64        `json:"score"`
	Item  *contract.Item `json:"item,omitempty"`
}

// enrichedReply is the --json shape of a search: each hit resolved to its
// snapshot. A shape of this view, not of the wire — the service reply stays
// {id, score}; the index is never authority.
type enrichedReply struct {
	Hits  []searchRow `json:"hits"`
	Total uint64      `json:"total"`
}

// col is one table column: the header name and how a row renders its cell.
type col struct {
	name    string
	extract func(searchRow) string
}

// itemCol lifts an item-field accessor over rows whose snapshot may be nil.
func itemCol(f func(*contract.Item) string) func(searchRow) string {
	return func(r searchRow) string {
		if r.Item == nil {
			return ""
		}
		return f(r.Item)
	}
}

// columnRegistry is the full column vocabulary in presentation order: id and
// score first, then the item fields as printItem orders them. The names are
// item-model.md's.
func columnRegistry() []col {
	date := func(t time.Time) string { return t.Format("2006-01-02") }
	return []col{
		{"id", func(r searchRow) string { return r.ID }},
		{"score", func(r searchRow) string { return fmt.Sprintf("%.3f", r.Score) }},
		{"type", itemCol(func(it *contract.Item) string { return string(it.Type) })},
		{"status", itemCol(func(it *contract.Item) string { return string(it.Status) })},
		{"priority", itemCol(func(it *contract.Item) string { return string(it.Priority) })},
		{"report", itemCol(func(it *contract.Item) string { return it.Report })},
		{"reporter", itemCol(func(it *contract.Item) string { return it.Reporter })},
		{"created", itemCol(func(it *contract.Item) string { return date(it.Created) })},
		{"claimed-by", itemCol(func(it *contract.Item) string {
			if it.Claim == nil {
				return ""
			}
			return it.Claim.By
		})},
		{"claimed", itemCol(func(it *contract.Item) string {
			if it.Claim == nil {
				return ""
			}
			return date(it.Claim.At)
		})},
		{"blocked-by", itemCol(func(it *contract.Item) string { return it.BlockedBy })},
		{"interrupted", itemCol(func(it *contract.Item) string { return string(it.Interrupted) })},
		{"located-in", itemCol(func(it *contract.Item) string { return strings.Join(it.LocatedIn, ", ") })},
		{"discovered-while", itemCol(func(it *contract.Item) string { return it.DiscoveredWhile })},
		{"lands", itemCol(func(it *contract.Item) string {
			parts := make([]string, len(it.Lands))
			for i, l := range it.Lands {
				parts[i] = landString(l)
			}
			return strings.Join(parts, "; ")
		})},
		{"fixed-by", itemCol(func(it *contract.Item) string {
			parts := make([]string, len(it.FixedBy))
			for i, f := range it.FixedBy {
				parts[i] = fixRefString(f)
			}
			return strings.Join(parts, "; ")
		})},
		{"amended-design", itemCol(func(it *contract.Item) string { return strings.Join(it.AmendedDesign, ", ") })},
		{"closed", itemCol(func(it *contract.Item) string { return it.Closed })},
		{"notes", itemCol(func(it *contract.Item) string {
			if len(it.Notes) == 0 {
				return ""
			}
			return strconv.Itoa(len(it.Notes))
		})},
		{"links", itemCol(func(it *contract.Item) string {
			parts := make([]string, len(it.Links))
			for i, l := range it.Links {
				parts[i] = fmt.Sprintf("%s %s", l.Type, l.To)
			}
			return strings.Join(parts, "; ")
		})},
	}
}

// parseColumns resolves --columns values (each possibly comma-separated)
// against the registry, keeping the caller's order. It runs before anything
// dials, so a typo costs no connection.
func parseColumns(names []string) ([]col, error) {
	if len(names) == 0 {
		return nil, nil
	}
	byName := map[string]col{}
	all := make([]string, 0, len(columnRegistry()))
	for _, c := range columnRegistry() {
		byName[c.name] = c
		all = append(all, c.name)
	}
	var cols []col
	for _, chunk := range names {
		for _, name := range strings.Split(chunk, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			c, ok := byName[name]
			if !ok {
				return nil, fmt.Errorf("search: unknown column %q (columns: %s)", name, strings.Join(all, ", "))
			}
			cols = append(cols, c)
		}
	}
	if len(cols) == 0 {
		return nil, errors.New("search: --columns names no columns")
	}
	return cols, nil
}

// populatedColumns keeps id, score, and every column with a value in at
// least one row — an all-blank column says nothing worth a header.
func populatedColumns(rows []searchRow) []col {
	var kept []col
	for _, c := range columnRegistry() {
		if c.name == "id" || c.name == "score" {
			kept = append(kept, c)
			continue
		}
		for _, r := range rows {
			if c.extract(r) != "" {
				kept = append(kept, c)
				break
			}
		}
	}
	return kept
}

// cell flattens one value for the table: whitespace runs collapse, long
// values truncate, and an empty value reads as a dash — the full record is
// one `hits get` away.
func cell(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return "-"
	}
	if r := []rune(s); len(r) > 60 {
		return string(r[:59]) + "…"
	}
	return s
}

func (inv *invocation) printSearchTable(rows []searchRow, total uint64, cols []col) error {
	if inv.json {
		return emit(inv.out, enrichedReply{Hits: rows, Total: total})
	}
	if len(rows) > 0 {
		if cols == nil {
			cols = populatedColumns(rows)
		}
		tw := tabwriter.NewWriter(inv.out, 0, 0, 2, ' ', 0)
		headers := make([]string, len(cols))
		for i, c := range cols {
			headers[i] = c.name
		}
		fmt.Fprintln(tw, strings.Join(headers, "\t"))
		for _, r := range rows {
			cells := make([]string, len(cols))
			for i, c := range cols {
				cells[i] = cell(c.extract(r))
			}
			fmt.Fprintln(tw, strings.Join(cells, "\t"))
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}
	fmt.Fprintf(inv.out, "total: %d\n", total)
	return nil
}

func (inv *invocation) printSemantic(r client.SemanticReply) error {
	if inv.json {
		return emit(inv.out, r)
	}
	for _, h := range r.Hits {
		fmt.Fprintf(inv.out, "%.3f  %s\n", h.Score, h.ID)
	}
	return nil
}

func nodeString(n client.NodeRef) string {
	s := string(n.Kind) + ":" + n.ID
	if n.Name != "" {
		s += " (" + n.Name + ")"
	}
	return s
}

func edgeString(e client.GraphEdge) string {
	return fmt.Sprintf("%s -[%s]-> %s", nodeString(e.From), e.Type, nodeString(e.To))
}

func (inv *invocation) printNeighbors(r client.NeighborsReply) error {
	if inv.json {
		return emit(inv.out, r)
	}
	for _, e := range r.Edges {
		fmt.Fprintln(inv.out, edgeString(e))
	}
	return nil
}

func (inv *invocation) printWalk(r client.WalkReply) error {
	if inv.json {
		return emit(inv.out, r)
	}
	fmt.Fprintln(inv.out, "nodes:")
	for _, n := range r.Nodes {
		fmt.Fprintf(inv.out, "  %s\n", nodeString(n))
	}
	fmt.Fprintln(inv.out, "edges:")
	for _, e := range r.Edges {
		fmt.Fprintf(inv.out, "  %s\n", edgeString(e))
	}
	return nil
}

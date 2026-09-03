package cli

import (
	"encoding/json"
	"fmt"
	"io"
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

func (inv *invocation) printSearch(r client.SearchReply) error {
	if inv.json {
		return emit(inv.out, r)
	}
	for _, h := range r.Hits {
		fmt.Fprintf(inv.out, "%.3f  %s\n", h.Score, h.ID)
	}
	fmt.Fprintf(inv.out, "total: %d\n", r.Total)
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

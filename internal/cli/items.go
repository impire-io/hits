package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/contract"
)

// multiFlag collects a repeatable string flag.
type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }

func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

func runCreate(inv *invocation) error {
	fs := inv.flagSet("create", "create --type <bug|task|improvement> [flags] <report>")
	typ := fs.String("type", "", "item type: bug, task, or improvement")
	priority := fs.String("priority", "", "triage signal: high, normal, or low")
	var projects multiFlag
	fs.Var(&projects, "project", "located-in project slug (repeatable)")
	discovered := fs.String("discovered-while", "", "the context the item was noticed in")
	if err := fs.Parse(inv.args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return errors.New("create: exactly one <report> argument")
	}
	actor, err := inv.actorOrErr()
	if err != nil {
		return err
	}

	c, closeConn, err := inv.dial()
	if err != nil {
		return err
	}
	defer closeConn()
	item, err := c.CreateItem(inv.ctx, client.CreateItemRequest{
		Actor:           actor,
		Type:            contract.Type(*typ),
		Report:          fs.Arg(0),
		Priority:        contract.Priority(*priority),
		LocatedIn:       projects,
		DiscoveredWhile: *discovered,
	})
	if err != nil {
		return err
	}
	return inv.printItem(item)
}

func runGet(inv *invocation) error {
	fs := inv.flagSet("get", "get <id>")
	id, rest, err := leadingID(fs, inv.args, "<id>")
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
	item, err := c.GetItem(inv.ctx, id)
	if err != nil {
		return err
	}
	return inv.printItem(item)
}

func runEdit(inv *invocation) error {
	fs := inv.flagSet("edit", "edit <id> [flags]")
	priority := fs.String("priority", "", "triage signal: high, normal, or low")
	var projects multiFlag
	fs.Var(&projects, "project", "located-in project slug (repeatable, replaces the list)")
	discovered := fs.String("discovered-while", "", "the context the item was noticed in (\"\" clears)")
	lands := fs.String("lands", "", `landing order as a JSON array ("[]" clears)`)
	id, rest, err := leadingID(fs, inv.args, "<id>")
	if err != nil {
		return err
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := noTrailing(fs); err != nil {
		return err
	}
	actor, err := inv.actorOrErr()
	if err != nil {
		return err
	}

	req := client.EditItemRequest{Actor: actor, ID: id}
	var parseErr error
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "priority":
			p := contract.Priority(*priority)
			req.Priority = &p
		case "project":
			locs := []string(projects)
			req.LocatedIn = &locs
		case "discovered-while":
			req.DiscoveredWhile = discovered
		case "lands":
			var l []contract.Land
			if err := json.Unmarshal([]byte(*lands), &l); err != nil {
				parseErr = fmt.Errorf("bad --lands: %w", err)
				return
			}
			req.Lands = &l
		}
	})
	if parseErr != nil {
		return parseErr
	}
	if req.Priority == nil && req.LocatedIn == nil && req.DiscoveredWhile == nil && req.Lands == nil {
		return errors.New("edit: nothing to change")
	}

	c, closeConn, err := inv.dial()
	if err != nil {
		return err
	}
	defer closeConn()
	item, err := c.EditItem(inv.ctx, req)
	if err != nil {
		return err
	}
	return inv.printItem(item)
}

// parseFixRef reads one --fixed-by value: pr:<ref>, commit:<ref>, or
// action:<what>, with everything after the first space as the evidence note.
func parseFixRef(s string) (contract.FixRef, error) {
	head, note, _ := strings.Cut(s, " ")
	kind, ref, ok := strings.Cut(head, ":")
	if !ok || ref == "" {
		return contract.FixRef{}, fmt.Errorf("bad --fixed-by %q: want pr:<ref>, commit:<ref>, or action:<what>", s)
	}
	fr := contract.FixRef{Note: note}
	switch kind {
	case "pr":
		fr.PR = ref
	case "commit":
		fr.Commit = ref
	case "action":
		fr.Action = ref
	default:
		return contract.FixRef{}, fmt.Errorf("bad --fixed-by kind %q: want pr, commit, or action", kind)
	}
	return fr, nil
}

func runTransition(inv *invocation) error {
	return transitionCmd(inv, "transition", "transition <id> --to <status> [flags]", "")
}

func runResolve(inv *invocation) error {
	return transitionCmd(inv, "resolve", "resolve <id> [flags]", contract.Resolved)
}

func runWontfix(inv *invocation) error {
	return transitionCmd(inv, "wontfix", "wontfix <id> [flags]", contract.Wontfix)
}

// transitionCmd is the one body behind transition and the closing sugar
// verbs: a non-empty preset fixes the target and drops the --to flag.
func transitionCmd(inv *invocation, name, usage string, preset contract.Status) error {
	fs := inv.flagSet(name, usage)
	var to *string
	if preset == "" {
		to = fs.String("to", "", "target status")
	}
	var projects multiFlag
	fs.Var(&projects, "project", "located-in project slug (repeatable)")
	var fixedBy multiFlag
	fs.Var(&fixedBy, "fixed-by", "closing ref: pr:<ref>, commit:<ref>, or action:<what>, note after a space (repeatable)")
	var amended multiFlag
	fs.Var(&amended, "amended-design", "design doc amended by the close (repeatable)")
	id, rest, err := leadingID(fs, inv.args, "<id>")
	if err != nil {
		return err
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := noTrailing(fs); err != nil {
		return err
	}
	target := preset
	if preset == "" {
		if *to == "" {
			fs.Usage()
			return errors.New("transition: --to is required")
		}
		target = contract.Status(*to)
	}
	refs := make([]contract.FixRef, 0, len(fixedBy))
	for _, s := range fixedBy {
		r, err := parseFixRef(s)
		if err != nil {
			return err
		}
		refs = append(refs, r)
	}
	actor, err := inv.actorOrErr()
	if err != nil {
		return err
	}

	c, closeConn, err := inv.dial()
	if err != nil {
		return err
	}
	defer closeConn()
	item, err := c.TransitionItem(inv.ctx, client.TransitionItemRequest{
		Actor:         actor,
		ID:            id,
		To:            target,
		LocatedIn:     projects,
		FixedBy:       refs,
		AmendedDesign: amended,
	})
	if err != nil {
		return err
	}
	return inv.printItem(item)
}

func runClaim(inv *invocation) error {
	fs := inv.flagSet("claim", "claim <id> [--steal]")
	steal := fs.Bool("steal", false, "take over an abandoned claim (attributed in the op)")
	id, rest, err := leadingID(fs, inv.args, "<id>")
	if err != nil {
		return err
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := noTrailing(fs); err != nil {
		return err
	}
	actor, err := inv.actorOrErr()
	if err != nil {
		return err
	}

	c, closeConn, err := inv.dial()
	if err != nil {
		return err
	}
	defer closeConn()
	item, err := c.ClaimItem(inv.ctx, client.ClaimItemRequest{Actor: actor, ID: id, Steal: *steal})
	if err != nil {
		return err
	}
	return inv.printItem(item)
}

func runRelease(inv *invocation) error {
	fs := inv.flagSet("release", "release <id>")
	id, rest, err := leadingID(fs, inv.args, "<id>")
	if err != nil {
		return err
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := noTrailing(fs); err != nil {
		return err
	}
	actor, err := inv.actorOrErr()
	if err != nil {
		return err
	}

	c, closeConn, err := inv.dial()
	if err != nil {
		return err
	}
	defer closeConn()
	item, err := c.ReleaseItem(inv.ctx, client.ReleaseItemRequest{Actor: actor, ID: id})
	if err != nil {
		return err
	}
	return inv.printItem(item)
}

func runBlock(inv *invocation) error {
	fs := inv.flagSet("block", "block <id> [--by <what>]")
	by := fs.String("by", "", "the thing being waited on")
	id, rest, err := leadingID(fs, inv.args, "<id>")
	if err != nil {
		return err
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := noTrailing(fs); err != nil {
		return err
	}
	actor, err := inv.actorOrErr()
	if err != nil {
		return err
	}

	c, closeConn, err := inv.dial()
	if err != nil {
		return err
	}
	defer closeConn()
	item, err := c.BlockItem(inv.ctx, client.BlockItemRequest{Actor: actor, ID: id, BlockedBy: *by})
	if err != nil {
		return err
	}
	return inv.printItem(item)
}

func runUnblock(inv *invocation) error {
	fs := inv.flagSet("unblock", "unblock <id>")
	id, rest, err := leadingID(fs, inv.args, "<id>")
	if err != nil {
		return err
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := noTrailing(fs); err != nil {
		return err
	}
	actor, err := inv.actorOrErr()
	if err != nil {
		return err
	}

	c, closeConn, err := inv.dial()
	if err != nil {
		return err
	}
	defer closeConn()
	item, err := c.UnblockItem(inv.ctx, client.UnblockItemRequest{Actor: actor, ID: id})
	if err != nil {
		return err
	}
	return inv.printItem(item)
}

func runLink(inv *invocation, retract bool) error {
	name := "link"
	if retract {
		name = "unlink"
	}
	fs := inv.flagSet(name, name+" <id> --type <duplicates|relates-to> <to-id>")
	typ := fs.String("type", "", "link type: duplicates or relates-to")
	id, rest, err := leadingID(fs, inv.args, "<id>")
	if err != nil {
		return err
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("%s: exactly one <to-id> argument", name)
	}
	if *typ == "" {
		fs.Usage()
		return fmt.Errorf("%s: --type is required", name)
	}
	actor, err := inv.actorOrErr()
	if err != nil {
		return err
	}

	c, closeConn, err := inv.dial()
	if err != nil {
		return err
	}
	defer closeConn()
	req := client.LinkItemRequest{Actor: actor, ID: id, Type: contract.LinkType(*typ), To: fs.Arg(0)}
	var item contract.Item
	if retract {
		item, err = c.UnlinkItem(inv.ctx, req)
	} else {
		item, err = c.LinkItem(inv.ctx, req)
	}
	if err != nil {
		return err
	}
	return inv.printItem(item)
}

func runNote(inv *invocation) error {
	fs := inv.flagSet("note", "note <id> <text>")
	id, rest, err := leadingID(fs, inv.args, "<id>")
	if err != nil {
		return err
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return errors.New("note: exactly one <text> argument")
	}
	actor, err := inv.actorOrErr()
	if err != nil {
		return err
	}

	c, closeConn, err := inv.dial()
	if err != nil {
		return err
	}
	defer closeConn()
	item, err := c.NoteItem(inv.ctx, client.NoteItemRequest{Actor: actor, ID: id, Text: fs.Arg(0)})
	if err != nil {
		return err
	}
	return inv.printItem(item)
}

func runTombstone(inv *invocation) error {
	fs := inv.flagSet("tombstone", "tombstone <id> <reason>")
	id, rest, err := leadingID(fs, inv.args, "<id>")
	if err != nil {
		return err
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return errors.New("tombstone: exactly one <reason> argument")
	}
	actor, err := inv.actorOrErr()
	if err != nil {
		return err
	}

	c, closeConn, err := inv.dial()
	if err != nil {
		return err
	}
	defer closeConn()
	item, err := c.TombstoneItem(inv.ctx, client.TombstoneItemRequest{Actor: actor, ID: id, Reason: fs.Arg(0)})
	if err != nil {
		return err
	}
	return inv.printItem(item)
}

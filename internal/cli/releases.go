package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/contract"
)

// runReleaseCmd disambiguates the release verb: the vocabulary
// subcommands (decision 0017) by keyword, anything else the claim
// hand-back — unambiguous because an item ID can never be one of these
// words.
func runReleaseCmd(inv *invocation) error {
	if len(inv.args) > 0 {
		switch inv.args[0] {
		case "register":
			return runReleaseRegister(inv, inv.args[1:])
		case "ship":
			return runReleaseShip(inv, inv.args[1:])
		case "retire":
			return runReleaseRetire(inv, inv.args[1:])
		case "list":
			return runReleaseList(inv, inv.args[1:])
		}
	}
	return runRelease(inv)
}

// releaseInitiative resolves the initiative a release verb works in —
// the flag, then $HITS_INITIATIVE, then the selected default, exactly
// as create resolves its mint.
func releaseInitiative(flagValue string) (string, error) {
	init, err := initiativeOrErr(flagValue)
	if err != nil {
		return "", fmt.Errorf("release: %w", err)
	}
	return init, nil
}

func runReleaseRegister(inv *invocation, args []string) error {
	fs := inv.flagSet("release register", "release register <slug> <name> [--description <d>] [--initiative <i>]")
	description := fs.String("description", "", "what the release is")
	initiative := fs.String("initiative", "", "the release's initiative (default: $HITS_INITIATIVE, else the selected initiative)")
	lead, rest, err := leading(args, "<slug>", "<name>")
	if err != nil {
		if errors.Is(err, errFlagsFirst) {
			if perr := fs.Parse(args); perr != nil {
				return perr
			}
			err = errors.New("missing <slug> <name> arguments")
		}
		fs.Usage()
		return fmt.Errorf("release register: %w", err)
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := noTrailing(fs); err != nil {
		return err
	}
	init, err := releaseInitiative(*initiative)
	if err != nil {
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
	r, err := c.RegisterRelease(inv.ctx, client.RegisterReleaseRequest{
		Actor: actor, Initiative: init, Slug: lead[0], Name: lead[1], Description: *description,
	})
	if err != nil {
		return err
	}
	return inv.printRelease(r)
}

// parseShipRef reads one --ref value: tag:<ref>, commit:<ref>, or
// artifact:<ref>, with everything after the first space as the note.
func parseShipRef(s string) (contract.ShipRef, error) {
	head, note, _ := strings.Cut(s, " ")
	kind, ref, ok := strings.Cut(head, ":")
	if !ok || ref == "" {
		return contract.ShipRef{}, fmt.Errorf("bad --ref %q: want tag:<ref>, commit:<ref>, or artifact:<ref>", s)
	}
	sr := contract.ShipRef{Note: note}
	switch kind {
	case "tag":
		sr.Tag = ref
	case "commit":
		sr.Commit = ref
	case "artifact":
		sr.Artifact = ref
	default:
		return contract.ShipRef{}, fmt.Errorf("bad --ref kind %q: want tag, commit, or artifact", kind)
	}
	return sr, nil
}

func runReleaseShip(inv *invocation, args []string) error {
	fs := inv.flagSet("release ship", "release ship <slug> --ref <kind:ref> [--ref ...] [--note <n>] [--initiative <i>]")
	var refs multiFlag
	fs.Var(&refs, "ref", "ship evidence: tag:<ref>, commit:<ref>, or artifact:<ref>, note after a space (repeatable)")
	note := fs.String("note", "", "optional note on the ship")
	initiative := fs.String("initiative", "", "the release's initiative (default: $HITS_INITIATIVE, else the selected initiative)")
	slug, rest, err := leadingID(fs, args, "<slug>")
	if err != nil {
		return err
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := noTrailing(fs); err != nil {
		return err
	}
	if len(refs) == 0 {
		fs.Usage()
		return errors.New("release ship: at least one --ref is required")
	}
	shipRefs := make([]contract.ShipRef, 0, len(refs))
	for _, s := range refs {
		r, err := parseShipRef(s)
		if err != nil {
			return err
		}
		shipRefs = append(shipRefs, r)
	}
	init, err := releaseInitiative(*initiative)
	if err != nil {
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
	r, err := c.ShipRelease(inv.ctx, client.ShipReleaseRequest{
		Actor: actor, Initiative: init, Slug: slug, Refs: shipRefs, Note: *note,
	})
	if err != nil {
		return err
	}
	return inv.printRelease(r)
}

func runReleaseRetire(inv *invocation, args []string) error {
	fs := inv.flagSet("release retire", "release retire <slug> --reason <r> [--initiative <i>]")
	reason := fs.String("reason", "", "why the slug leaves the vocabulary")
	initiative := fs.String("initiative", "", "the release's initiative (default: $HITS_INITIATIVE, else the selected initiative)")
	slug, rest, err := leadingID(fs, args, "<slug>")
	if err != nil {
		return err
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := noTrailing(fs); err != nil {
		return err
	}
	if *reason == "" {
		fs.Usage()
		return errors.New("release retire: --reason is required")
	}
	init, err := releaseInitiative(*initiative)
	if err != nil {
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
	r, err := c.RetireRelease(inv.ctx, client.RetireReleaseRequest{
		Actor: actor, Initiative: init, Slug: slug, Reason: *reason,
	})
	if err != nil {
		return err
	}
	return inv.printRelease(r)
}

func runReleaseList(inv *invocation, args []string) error {
	fs := inv.flagSet("release list", "release list [--initiative <i>]")
	initiative := fs.String("initiative", "", "the initiative to list (default: $HITS_INITIATIVE, else the selected initiative)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := noTrailing(fs); err != nil {
		return err
	}
	init, err := releaseInitiative(*initiative)
	if err != nil {
		return err
	}

	c, closeConn, err := inv.dial()
	if err != nil {
		return err
	}
	defer closeConn()
	rs, err := c.ListReleases(inv.ctx, client.ListReleasesRequest{Initiative: init})
	if err != nil {
		return err
	}
	return inv.printReleases(rs)
}

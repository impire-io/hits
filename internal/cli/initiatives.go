package cli

import (
	"errors"
	"fmt"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/internal/connect"
)

func runInitiative(inv *invocation) error {
	if len(inv.args) == 0 {
		return errors.New(`initiative: want "register", "retire", "list" or "select"`)
	}
	sub, rest := inv.args[0], inv.args[1:]
	switch sub {
	case "register":
		return runInitiativeRegister(inv, rest)
	case "retire":
		return runInitiativeRetire(inv, rest)
	case "list":
		return runInitiativeList(inv, rest)
	case "select":
		return runInitiativeSelect(inv, rest)
	default:
		return fmt.Errorf(`initiative: unknown subcommand %q, want "register", "retire", "list" or "select"`, sub)
	}
}

func runInitiativeRegister(inv *invocation, args []string) error {
	fs := inv.flagSet("initiative register", "initiative register <slug> <name> [--description <d>]")
	description := fs.String("description", "", "what the initiative is")
	lead, rest, err := leading(args, "<slug>", "<name>")
	if err != nil {
		if errors.Is(err, errFlagsFirst) {
			if perr := fs.Parse(args); perr != nil {
				return perr
			}
			err = errors.New("missing <slug> <name> arguments")
		}
		fs.Usage()
		return fmt.Errorf("initiative register: %w", err)
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
	i, err := c.RegisterInitiative(inv.ctx, client.RegisterInitiativeRequest{
		Actor: actor, Slug: lead[0], Name: lead[1], Description: *description,
	})
	if err != nil {
		return err
	}
	return inv.printInitiative(i)
}

func runInitiativeRetire(inv *invocation, args []string) error {
	fs := inv.flagSet("initiative retire", "initiative retire <slug> --reason <r>")
	reason := fs.String("reason", "", "why the slug leaves the vocabulary")
	lead, rest, err := leading(args, "<slug>")
	if err != nil {
		if errors.Is(err, errFlagsFirst) {
			if perr := fs.Parse(args); perr != nil {
				return perr
			}
			err = errors.New("missing <slug> argument")
		}
		fs.Usage()
		return fmt.Errorf("initiative retire: %w", err)
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := noTrailing(fs); err != nil {
		return err
	}
	if *reason == "" {
		fs.Usage()
		return errors.New("initiative retire: --reason is required")
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
	i, err := c.RetireInitiative(inv.ctx, client.RetireInitiativeRequest{
		Actor: actor, Slug: lead[0], Reason: *reason,
	})
	if err != nil {
		return err
	}
	return inv.printInitiative(i)
}

func runInitiativeList(inv *invocation, args []string) error {
	fs := inv.flagSet("initiative list", "initiative list")
	if err := fs.Parse(args); err != nil {
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
	is, err := c.ListInitiatives(inv.ctx)
	if err != nil {
		return err
	}
	return inv.printInitiatives(is)
}

// runInitiativeSelect verifies the slug against the live registry, then
// records it as the client config's selected initiative — a filing
// default for create, never a read scope (decision 0016).
func runInitiativeSelect(inv *invocation, args []string) error {
	fs := inv.flagSet("initiative select", "initiative select <slug>")
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

	c, closeConn, err := inv.dial()
	if err != nil {
		return err
	}
	defer closeConn()
	is, err := c.ListInitiatives(inv.ctx)
	if err != nil {
		return err
	}
	found := false
	for _, i := range is {
		if i.Slug == slug {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("initiative select: %q is not a registered initiative", slug)
	}
	if err := connect.SaveDefaultInitiative(slug); err != nil {
		return err
	}
	fmt.Fprintf(inv.out, "selected initiative: %s\n", slug)
	return nil
}

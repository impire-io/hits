package cli

import (
	"errors"
	"fmt"

	"github.com/impire-io/hits/client"
)

func runProject(inv *invocation) error {
	if len(inv.args) == 0 {
		return errors.New(`project: want "register", "assign", "retire" or "list"`)
	}
	sub, rest := inv.args[0], inv.args[1:]
	switch sub {
	case "register":
		return runProjectRegister(inv, rest)
	case "assign":
		return runProjectAssign(inv, rest)
	case "retire":
		return runProjectRetire(inv, rest)
	case "list":
		return runProjectList(inv, rest)
	default:
		return fmt.Errorf(`project: unknown subcommand %q, want "register", "assign", "retire" or "list"`, sub)
	}
}

func runProjectRegister(inv *invocation, args []string) error {
	fs := inv.flagSet("project register", "project register <slug> <name> --initiative <i> [--description <d>]")
	description := fs.String("description", "", "what the project is")
	initiative := fs.String("initiative", "", "the initiative the project belongs to")
	lead, rest, err := leading(args, "<slug>", "<name>")
	if err != nil {
		if errors.Is(err, errFlagsFirst) {
			if perr := fs.Parse(args); perr != nil {
				return perr
			}
			err = errors.New("missing <slug> <name> arguments")
		}
		fs.Usage()
		return fmt.Errorf("project register: %w", err)
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := noTrailing(fs); err != nil {
		return err
	}
	if *initiative == "" {
		fs.Usage()
		return errors.New("project register: --initiative is required")
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
	p, err := c.RegisterProject(inv.ctx, client.RegisterProjectRequest{
		Actor: actor, Slug: lead[0], Name: lead[1], Description: *description, Initiative: *initiative,
	})
	if err != nil {
		return err
	}
	return inv.printProject(p)
}

func runProjectAssign(inv *invocation, args []string) error {
	fs := inv.flagSet("project assign", "project assign <slug> --initiative <i>")
	initiative := fs.String("initiative", "", "the initiative the project moves to")
	lead, rest, err := leading(args, "<slug>")
	if err != nil {
		if errors.Is(err, errFlagsFirst) {
			if perr := fs.Parse(args); perr != nil {
				return perr
			}
			err = errors.New("missing <slug> argument")
		}
		fs.Usage()
		return fmt.Errorf("project assign: %w", err)
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := noTrailing(fs); err != nil {
		return err
	}
	if *initiative == "" {
		fs.Usage()
		return errors.New("project assign: --initiative is required")
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
	p, err := c.AssignProject(inv.ctx, client.AssignProjectRequest{
		Actor: actor, Slug: lead[0], Initiative: *initiative,
	})
	if err != nil {
		return err
	}
	return inv.printProject(p)
}

func runProjectRetire(inv *invocation, args []string) error {
	fs := inv.flagSet("project retire", "project retire <slug> --reason <r>")
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
		return fmt.Errorf("project retire: %w", err)
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := noTrailing(fs); err != nil {
		return err
	}
	if *reason == "" {
		fs.Usage()
		return errors.New("project retire: --reason is required")
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
	p, err := c.RetireProject(inv.ctx, client.RetireProjectRequest{
		Actor: actor, Slug: lead[0], Reason: *reason,
	})
	if err != nil {
		return err
	}
	return inv.printProject(p)
}

func runProjectList(inv *invocation, args []string) error {
	fs := inv.flagSet("project list", "project list")
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
	ps, err := c.ListProjects(inv.ctx)
	if err != nil {
		return err
	}
	return inv.printProjects(ps)
}

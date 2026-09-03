package cli

import (
	"errors"
	"fmt"

	"github.com/impire-io/hits/client"
)

func runProject(inv *invocation) error {
	if len(inv.args) == 0 {
		return errors.New(`project: want "register" or "list"`)
	}
	sub, rest := inv.args[0], inv.args[1:]
	switch sub {
	case "register":
		return runProjectRegister(inv, rest)
	case "list":
		return runProjectList(inv, rest)
	default:
		return fmt.Errorf(`project: unknown subcommand %q, want "register" or "list"`, sub)
	}
}

func runProjectRegister(inv *invocation, args []string) error {
	fs := inv.flagSet("project register", "project register <slug> <name> [--description <d>]")
	description := fs.String("description", "", "what the project is")
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
		Actor: actor, Slug: lead[0], Name: lead[1], Description: *description,
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

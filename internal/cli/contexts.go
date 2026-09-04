package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/impire-io/hits/internal/connect"
)

// runContext is the context verb family (hits-hq 02-DESIGN/idp-auth.md §
// the verbs): thin over internal/connect's management API, the paved
// path to hits' own context directory — and the only one that writes it.
func runContext(inv *invocation) error {
	if len(inv.args) == 0 {
		return errors.New(`context: want "ls", "add", "import", "edit", "rm" or "select"`)
	}
	sub, rest := inv.args[0], inv.args[1:]
	switch sub {
	case "ls", "list":
		return runContextLs(inv, rest)
	case "add":
		return runContextAdd(inv, rest)
	case "import":
		return runContextImport(inv, rest)
	case "edit":
		return runContextEdit(inv, rest)
	case "rm":
		return runContextRm(inv, rest)
	case "select":
		return runContextSelect(inv, rest)
	default:
		return fmt.Errorf(`context: unknown subcommand %q, want "ls", "add", "import", "edit", "rm" or "select"`, sub)
	}
}

func runContextLs(inv *invocation, args []string) error {
	fs := inv.flagSet("context ls", "context ls")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := noTrailing(fs); err != nil {
		return err
	}
	infos, err := connect.ListContexts()
	if err != nil {
		return err
	}
	if inv.json {
		return emit(inv.out, infos)
	}
	if len(infos) == 0 {
		fmt.Fprintln(inv.out, "no contexts: create one with 'hits context add <name>' or 'hits context import <nats-context>'")
		return nil
	}
	for _, ci := range infos {
		marker := " "
		if ci.Default {
			marker = "*"
		}
		fmt.Fprintf(inv.out, "%s %s\n", marker, ci.Name)
	}
	return nil
}

func runContextAdd(inv *invocation, args []string) error {
	fs := inv.flagSet("context add", "context add <name> [--url <url>] [--issuer <url> --client-id <id>]")
	url := fs.String("url", "", "NATS server url for the nats block")
	issuer := fs.String("issuer", "", "IDP issuer url (with --client-id, adds the oauth block)")
	clientID := fs.String("client-id", "", "OAuth client id (with --issuer, adds the oauth block)")
	name, rest, err := leadingID(fs, args, "<name>")
	if err != nil {
		return err
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := noTrailing(fs); err != nil {
		return err
	}
	if (*issuer == "") != (*clientID == "") {
		return errors.New("context add: --issuer and --client-id go together")
	}
	var oc *connect.OAuthConfig
	if *issuer != "" {
		oc = &connect.OAuthConfig{Issuer: *issuer, ClientID: *clientID}
	}
	path, err := connect.AddContext(name, *url, oc)
	if err != nil {
		return err
	}
	fmt.Fprintf(inv.out, "created context %q (%s)\n", name, path)
	return openEditor(inv, path, false)
}

func runContextImport(inv *invocation, args []string) error {
	fs := inv.flagSet("context import", "context import <nats-context> [<name>]")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 || fs.NArg() > 2 {
		fs.Usage()
		return errors.New("context import: want <nats-context> [<name>]")
	}
	natsName, newName := fs.Arg(0), fs.Arg(1)
	path, err := connect.ImportContext(natsName, newName)
	if err != nil {
		return err
	}
	if newName == "" {
		newName = natsName
	}
	fmt.Fprintf(inv.out, "imported nats context %q as hits context %q (%s)\n", natsName, newName, path)
	return nil
}

func runContextEdit(inv *invocation, args []string) error {
	fs := inv.flagSet("context edit", "context edit <name>")
	name, rest, err := leadingID(fs, args, "<name>")
	if err != nil {
		return err
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := noTrailing(fs); err != nil {
		return err
	}
	path, err := connect.ContextPath(name)
	if err != nil {
		return err
	}
	return openEditor(inv, path, true)
}

func runContextRm(inv *invocation, args []string) error {
	fs := inv.flagSet("context rm", "context rm <name>")
	name, rest, err := leadingID(fs, args, "<name>")
	if err != nil {
		return err
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := noTrailing(fs); err != nil {
		return err
	}
	if err := connect.RemoveContext(name); err != nil {
		return err
	}
	fmt.Fprintf(inv.out, "removed context %q\n", name)
	return nil
}

func runContextSelect(inv *invocation, args []string) error {
	fs := inv.flagSet("context select", "context select <name>")
	name, rest, err := leadingID(fs, args, "<name>")
	if err != nil {
		return err
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := noTrailing(fs); err != nil {
		return err
	}
	if err := connect.SelectContext(name); err != nil {
		return err
	}
	fmt.Fprintf(inv.out, "default context is now %q\n", name)
	return nil
}

// openEditor spawns $EDITOR on path, attached to the terminal. Unset,
// it is an error only where editing is the verb's whole job; add just
// leaves the scaffold in place.
func openEditor(inv *invocation, path string, required bool) error {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		if required {
			return errors.New("$EDITOR is not set")
		}
		return nil
	}
	parts := strings.Fields(editor)
	cmd := exec.Command(parts[0], append(parts[1:], path)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, inv.out, inv.errOut
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor %q: %w", editor, err)
	}
	return nil
}

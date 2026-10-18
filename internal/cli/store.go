package cli

import (
	"errors"
	"fmt"

	"github.com/impire-io/hits/client/chronicle"
	"github.com/impire-io/hits/internal/version"
)

// runStore is the store noun: init declares a HITS in the context's
// account (hits-hq 02-DESIGN/the-cutover.md § hits store init), manifest
// prints the same declaration as the install manifest.
func runStore(inv *invocation) error {
	if len(inv.args) == 0 {
		return errors.New("store: a verb is required: init | manifest")
	}
	switch verb, rest := inv.args[0], inv.args[1:]; verb {
	case "init":
		return runStoreInit(inv, rest)
	case "manifest":
		return runStoreManifest(inv, rest)
	default:
		return fmt.Errorf("store: unknown verb %q (want init | manifest)", verb)
	}
}

func runStoreInit(inv *invocation, args []string) error {
	fs := inv.flagSet("store init", "store init [--version <v>]")
	ver := fs.String("version", version.Version, "the version the announcement carries")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := noTrailing(fs); err != nil {
		return err
	}
	c, closeConn, err := inv.dialChronicle()
	if err != nil {
		return err
	}
	defer closeConn()
	if err := chronicle.Init(inv.ctx, c.Chronicle(), c.Store(), *ver); err != nil {
		return err
	}
	if inv.json {
		return emit(inv.out, map[string]any{"store": c.Store(), "types": len(chronicle.TypeDeclarations()), "indexes": len(chronicle.IndexDeclarations()), "announced": chronicle.AnnouncementPath})
	}
	fmt.Fprintf(inv.out, "store %s declared: %d types, %d indexes, announced as %s\n", c.Store(), len(chronicle.TypeDeclarations()), len(chronicle.IndexDeclarations()), chronicle.AnnouncementPath)
	return nil
}

func runStoreManifest(inv *invocation, args []string) error {
	fs := inv.flagSet("store manifest", "store manifest [--version <v>]")
	ver := fs.String("version", version.Version, "the version the manifest carries")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := noTrailing(fs); err != nil {
		return err
	}
	b, err := chronicle.Manifest(*ver)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(inv.out, string(b))
	return err
}

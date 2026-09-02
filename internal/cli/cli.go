// Package cli implements the hits terminal client. The logic lives here (not
// in cmd/) so it is testable: Run takes an explicit context, output writers,
// and an injectable Connector, letting tests drive every command against an
// in-process server.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/nats-io/nats.go"
	"github.com/synadia-io/orbit.go/natscontext"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/internal/version"
)

const usageText = `hits — a HITS client

Usage:
  hits [--context <name>] <command>

Commands:
  ping     ask the running service to identify itself
  version  print the client version
`

// Connector opens the NATS connection a command talks through. Production use
// resolves a NATS context; tests inject a connection to an embedded server.
type Connector func(contextName string) (*nats.Conn, error)

// ContextConnector resolves the named NATS context ("" means the selected one).
func ContextConnector(contextName string) (*nats.Conn, error) {
	nc, _, err := natscontext.Connect(contextName, nats.Name("hits"))
	return nc, err
}

// Run executes one CLI invocation. args excludes the program name.
func Run(ctx context.Context, args []string, out, errOut io.Writer, connect Connector) error {
	fs := flag.NewFlagSet("hits", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.Usage = func() { fmt.Fprint(errOut, usageText) }
	ctxName := fs.String("context", "", "NATS context to connect with (default: the selected context)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	switch cmd := fs.Arg(0); cmd {
	case "ping":
		return runPing(ctx, *ctxName, out, connect)
	case "version":
		fmt.Fprintln(out, version.Version)
		return nil
	case "":
		fs.Usage()
		return errors.New("no command given")
	default:
		fs.Usage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func runPing(ctx context.Context, contextName string, out io.Writer, connect Connector) error {
	nc, err := connect(contextName)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer nc.Close()

	about, err := client.New(nc).Ping(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s %s\n", about.Name, about.Version)
	return nil
}

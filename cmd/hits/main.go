// Command hits is the HITS terminal client — and, through the up
// subcommand, the whole service fleet in one process
// (hits-hq/02-DESIGN/hits-up.md).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/impire-io/hits/internal/cli"
	"github.com/impire-io/hits/internal/fleet"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// up dispatches before the client CLI's parser, so internal/cli stays
	// a pure client of the fleet.
	args := os.Args[1:]
	var err error
	if len(args) > 0 && args[0] == "up" {
		err = fleet.RunUp(ctx, args[1:], os.Stdout, os.Stderr, fleet.ContextConnector)
	} else {
		err = cli.Run(ctx, args, os.Stdout, os.Stderr, cli.ContextConnector)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "hits:", err)
		os.Exit(1)
	}
}

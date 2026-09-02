// Command hits is the HITS terminal client.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/impire-io/hits/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr, cli.ContextConnector); err != nil {
		fmt.Fprintln(os.Stderr, "hits:", err)
		os.Exit(1)
	}
}

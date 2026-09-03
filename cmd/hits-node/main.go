// Command hits-node runs the hits micro service against the NATS server a
// NATS context points at.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/synadia-io/orbit.go/natscontext"

	"github.com/impire-io/hits/internal/node"
	"github.com/impire-io/hits/internal/version"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "hits-node:", err)
		os.Exit(1)
	}
}

func run() error {
	ctxName := flag.String("context", "", "NATS context to connect with (default: the selected context)")
	flag.Parse()

	nc, _, err := natscontext.Connect(*ctxName, nats.Name("hits-node"))
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer nc.Close()

	startCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	svc, err := node.Start(startCtx, nc)
	if err != nil {
		return err
	}
	defer func() { _ = svc.Stop() }()

	fmt.Printf("hits-node %s serving on %s (Ctrl-C to stop)\n", version.Version, nc.ConnectedUrl())

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	return nil
}

// Command hits-index-graph runs the hits-graph micro service — the graph
// projection of the ops-log — against the NATS server a NATS context
// points at.
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

	"github.com/impire-io/hits/internal/index/graph"
	"github.com/impire-io/hits/internal/version"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "hits-index-graph:", err)
		os.Exit(1)
	}
}

func run() error {
	ctxName := flag.String("context", "", "NATS context to connect with (default: the selected context)")
	flag.Parse()

	nc, _, err := natscontext.Connect(*ctxName, nats.Name("hits-index-graph"))
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer nc.Close()

	startCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	svc, err := graph.Start(startCtx, nc)
	if err != nil {
		return err
	}
	defer svc.Stop()

	fmt.Printf("hits-index-graph %s serving on %s (Ctrl-C to stop)\n", version.Version, nc.ConnectedUrl())

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	return nil
}

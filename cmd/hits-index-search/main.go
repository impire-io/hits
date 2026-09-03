// Command hits-index-search runs the hits-search micro service — the
// full-text index projection of the ops-log — against the NATS server a
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

	"github.com/impire-io/hits/internal/index/search"
	"github.com/impire-io/hits/internal/version"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "hits-index-search:", err)
		os.Exit(1)
	}
}

func run() error {
	ctxName := flag.String("context", "", "NATS context to connect with (default: the selected context)")
	flag.Parse()

	nc, _, err := natscontext.Connect(*ctxName, nats.Name("hits-index-search"))
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer nc.Close()

	startCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	svc, err := search.Start(startCtx, nc)
	if err != nil {
		return err
	}
	defer svc.Stop()

	fmt.Printf("hits-index-search %s serving on %s (Ctrl-C to stop)\n", version.Version, nc.ConnectedUrl())

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	return nil
}

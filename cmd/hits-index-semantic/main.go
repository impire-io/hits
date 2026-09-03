// Command hits-index-semantic runs the hits-semantic micro service — the
// embedding projection of the ops-log — against the NATS server a NATS
// context points at. The embedding provider is OpenAI-API-compatible:
// endpoint and model come from flags, the API key from the environment
// (HITS_EMBEDDING_API_KEY) — a secret does not belong in argv.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/synadia-io/orbit.go/natscontext"

	"github.com/impire-io/hits/internal/index/semantic"
	"github.com/impire-io/hits/internal/version"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "hits-index-semantic:", err)
		os.Exit(1)
	}
}

func run() error {
	ctxName := flag.String("context", "", "NATS context to connect with (default: the selected context)")
	embedURL := flag.String("embedding-url", "", "base URL of the OpenAI-compatible embedding API (POST <url>/embeddings)")
	embedModel := flag.String("embedding-model", "", "embedding model to request")
	flag.Parse()

	if *embedURL == "" || *embedModel == "" {
		return errors.New("--embedding-url and --embedding-model are required")
	}
	cfg := semantic.Config{
		BaseURL: *embedURL,
		APIKey:  os.Getenv("HITS_EMBEDDING_API_KEY"),
		Model:   *embedModel,
	}

	nc, _, err := natscontext.Connect(*ctxName, nats.Name("hits-index-semantic"))
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer nc.Close()

	startCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	svc, err := semantic.Start(startCtx, nc, cfg)
	if err != nil {
		return err
	}
	defer svc.Stop()

	fmt.Printf("hits-index-semantic %s serving on %s (Ctrl-C to stop)\n", version.Version, nc.ConnectedUrl())

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	return nil
}

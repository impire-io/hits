// Package fleet composes the four HITS services into one process — the
// `hits up` subcommand (hits-hq/02-DESIGN/hits-up.md). It calls the same
// Start entrypoints the standalone cmd/ mains call, all on one shared
// connection named hits-up (decision 0006): micro services multiplex on
// it, and the whole platform costs a single seat in the account's
// connection allowance.
package fleet

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/nats-io/nats.go"

	"github.com/impire-io/hits/internal/connect"
	"github.com/impire-io/hits/internal/index/graph"
	"github.com/impire-io/hits/internal/index/search"
	"github.com/impire-io/hits/internal/index/semantic"
	"github.com/impire-io/hits/internal/node"
	"github.com/impire-io/hits/internal/version"
)

// Connector opens the one NATS connection the whole fleet talks through;
// it is called once, with the composition's nats.Name ("hits-up").
// Production resolves a NATS context; tests inject a connection to an
// embedded server.
type Connector func(natsName string) (*nats.Conn, error)

// DialConnector yields the production Connector: the fleet's connection
// resolves the named context, or the plain connection settings when given
// — both zero means the configured default (hits-hq 02-DESIGN/hits-up.md
// § plain connection settings).
func DialConnector(contextName string, d connect.Direct) Connector {
	return func(natsName string) (*nats.Conn, error) {
		return connect.Dial(contextName, d, natsName)
	}
}

// Config selects what the fleet runs. A zero Semantic means the semantic
// index is not started — the fleet functions fully without it
// (hits-hq/02-DESIGN/services.md § embeddings). MaxBytes is the ops
// stream's byte budget, 0 meaning the decided default (decision 0005).
type Config struct {
	Semantic semantic.Config
	MaxBytes int64
}

// Fleet is one running composition.
type Fleet struct {
	Running []string // the services started, in start order
	URL     string   // the server the fleet's connection landed on
	stops   []func()
}

// Start brings the fleet up on one shared connection, fail-fast: any
// service that cannot start stops the ones already running, closes the
// connection, and returns the error. hits-node goes first — it ensures
// the ops-log stream the indexers refuse to start without.
func Start(ctx context.Context, connect Connector, cfg Config) (*Fleet, error) {
	f := &Fleet{}
	ok := false
	defer func() {
		if !ok {
			f.Stop()
		}
	}()

	nc, err := connect("hits-up")
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	f.stops = append(f.stops, nc.Close)
	f.URL = nc.ConnectedUrl()

	startOne := func(name string, start func() (stop func(), err error)) error {
		stop, err := start()
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		f.stops = append(f.stops, stop)
		f.Running = append(f.Running, name)
		return nil
	}

	if err := startOne("hits-node", func() (func(), error) {
		svc, err := node.Start(ctx, nc, node.Config{MaxBytes: cfg.MaxBytes})
		if err != nil {
			return nil, err
		}
		return func() { _ = svc.Stop() }, nil
	}); err != nil {
		return nil, err
	}
	if err := startOne("hits-index-graph", func() (func(), error) {
		svc, err := graph.Start(ctx, nc)
		if err != nil {
			return nil, err
		}
		return svc.Stop, nil
	}); err != nil {
		return nil, err
	}
	if err := startOne("hits-index-search", func() (func(), error) {
		svc, err := search.Start(ctx, nc)
		if err != nil {
			return nil, err
		}
		return svc.Stop, nil
	}); err != nil {
		return nil, err
	}
	if cfg.Semantic != (semantic.Config{}) {
		if err := startOne("hits-index-semantic", func() (func(), error) {
			svc, err := semantic.Start(ctx, nc, cfg.Semantic)
			if err != nil {
				return nil, err
			}
			return svc.Stop, nil
		}); err != nil {
			return nil, err
		}
	}

	ok = true
	return f, nil
}

// Stop tears the composition down in reverse start order — every service
// off the wire before the shared connection closes. Safe to call more
// than once.
func (f *Fleet) Stop() {
	for i := len(f.stops) - 1; i >= 0; i-- {
		f.stops[i]()
	}
	f.stops = nil
}

const upUsage = `hits up — run the HITS service fleet in this process

Usage:
  hits up [--context <name> | --server <urls> [connection flags]]
          [--max-bytes <size>]
          [--embedding-url <url> --embedding-model <m>]

The fleet serves one NATS system: the named hits context's, or the one
the plain connection flags describe — --server (comma-separated URLs),
--creds, --user/--password, --nkey, --tlscert/--tlskey, --tlsca. Each
connection flag falls back to the nats CLI's environment variable
($NATS_URL, $NATS_CREDS, $NATS_USER, $NATS_PASSWORD, $NATS_NKEY,
$NATS_CERT, $NATS_KEY, $NATS_CA); a context and connection flags do not
combine. It runs hits-node plus the graph, search, and semantic indexes.

Provisioning declares byte budgets — the ops stream defaults to 1G and
refuses new writes when full; change it with --max-bytes (e.g. 2G). The
semantic index starts only when --embedding-url and --embedding-model
are both given (API key from $HITS_EMBEDDING_API_KEY). Runs in the
foreground until interrupted.
`

// RunUp executes the up subcommand: parse the flags, start the fleet
// through the Connector the factory yields for --context or the plain
// connection flags, and hold it until ctx ends. args excludes the "up"
// word itself.
func RunUp(ctx context.Context, args []string, out, errOut io.Writer, connectorFor func(contextName string, d connect.Direct) Connector) error {
	fs := flag.NewFlagSet("hits up", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.Usage = func() { fmt.Fprint(errOut, upUsage) }
	ctxName := fs.String("context", "", "hits context to connect with (default: the configured default)")
	server := fs.String("server", "", "NATS server URLs, comma-separated ($NATS_URL)")
	creds := fs.String("creds", "", "NATS credentials file ($NATS_CREDS)")
	user := fs.String("user", "", "NATS username ($NATS_USER)")
	password := fs.String("password", "", "NATS password ($NATS_PASSWORD)")
	nkey := fs.String("nkey", "", "NATS nkey seed file ($NATS_NKEY)")
	tlscert := fs.String("tlscert", "", "TLS client certificate file ($NATS_CERT)")
	tlskey := fs.String("tlskey", "", "TLS client key file ($NATS_KEY)")
	tlsca := fs.String("tlsca", "", "TLS CA bundle file ($NATS_CA)")
	maxBytes := fs.String("max-bytes", "", "ops stream byte budget, e.g. 2G (default 1G)")
	embedURL := fs.String("embedding-url", "", "base URL of the OpenAI-compatible embedding API (POST <url>/embeddings)")
	embedModel := fs.String("embedding-model", "", "embedding model to request")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	direct := connect.Direct{
		Servers: *server, Creds: *creds, User: *user, Password: *password,
		Nkey: *nkey, TLSCert: *tlscert, TLSKey: *tlskey, TLSCA: *tlsca,
	}
	if *ctxName != "" && direct != (connect.Direct{}) {
		return errors.New("a context carries its own connection: pass --context or the plain connection flags, not both")
	}
	if (*embedURL == "") != (*embedModel == "") {
		return errors.New("--embedding-url and --embedding-model go together; pass both or neither")
	}

	cfg := Config{}
	if *maxBytes != "" {
		n, err := node.ParseSize(*maxBytes)
		if err != nil {
			return fmt.Errorf("--max-bytes: %w", err)
		}
		cfg.MaxBytes = n
	}
	if *embedURL != "" {
		cfg.Semantic = semantic.Config{
			BaseURL: *embedURL,
			APIKey:  os.Getenv("HITS_EMBEDDING_API_KEY"),
			Model:   *embedModel,
		}
	}

	f, err := Start(ctx, connectorFor(*ctxName, direct), cfg)
	if err != nil {
		return err
	}
	defer f.Stop()

	fmt.Fprintf(out, "hits up %s — %s serving on %s (Ctrl-C to stop)\n",
		version.Version, strings.Join(f.Running, ", "), f.URL)
	if *embedURL == "" {
		fmt.Fprintln(out, "semantic search is off — pass --embedding-url and --embedding-model to enable it")
	}

	<-ctx.Done()
	fmt.Fprintln(out, "hits up: stopping")
	return nil
}

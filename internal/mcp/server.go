// Package mcp implements hits-mcp, the agent action surface: an MCP stdio
// server exposing the client API to agents as tools, one tool per endpoint
// (hits-hq 02-DESIGN/mcp-server.md, decision 0003). It is a client of the
// fleet, never a peer — stateless, no NATS endpoints, no projections. The
// logic lives here (not in cmd/) so it is testable: Run takes an injectable
// Connector, and NewServer is transport-free so tests drive the same server
// over an in-memory session.
package mcp

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nats-io/nats.go"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/contract"
	"github.com/impire-io/hits/internal/connect"
	"github.com/impire-io/hits/internal/version"
)

// Connector opens the NATS connection the server talks through. Production
// use resolves a NATS context; tests inject a connection to an embedded
// server.
type Connector func(contextName string) (*nats.Conn, error)

// ContextConnector resolves the named context — hits' own or the nats
// CLI's ("" means the configured default, else the selected one).
func ContextConnector(contextName string) (*nats.Conn, error) {
	return connect.Connect(contextName, "hits-mcp")
}

// defaultActor is indirected so Run's connect parameter keeps its name.
var defaultActor = connect.DefaultActor

// Run executes the server: parse flags, resolve and validate the actor,
// connect and ping the fleet — all fail-fast — then serve MCP over stdio
// until ctx ends. args excludes the program name.
func Run(ctx context.Context, args []string, errOut io.Writer, connect Connector) error {
	fs := flag.NewFlagSet("hits-mcp", flag.ContinueOnError)
	fs.SetOutput(errOut)
	ctxName := fs.String("context", "", "NATS context to connect with (default: the selected context)")
	actorFlag := fs.String("actor", "", "acting handle stamped on every write (default: $HITS_ACTOR)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}

	actor := *actorFlag
	if actor == "" {
		actor = os.Getenv("HITS_ACTOR")
	}
	if actor == "" {
		actor = defaultActor()
	}
	if actor == "" {
		return errors.New("no actor: pass --actor, set HITS_ACTOR, or set a default in the client config")
	}
	if !contract.ValidActor(actor) {
		return fmt.Errorf("actor %q is not a well-formed handle", actor)
	}

	nc, err := connect(*ctxName)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer nc.Close()
	c := client.New(nc)
	if _, err := c.Ping(ctx); err != nil {
		return fmt.Errorf("ping hits service: %w", err)
	}

	return NewServer(c, actor).Run(ctx, &sdk.StdioTransport{})
}

// NewServer builds the MCP server on an established client: the eighteen
// tools of the design's table, one per client endpoint. Adding a client
// endpoint means adding its tool here in the same change.
func NewServer(c *client.Client, actor string) *sdk.Server {
	s := sdk.NewServer(&sdk.Implementation{Name: "hits", Version: version.Version}, nil)
	addItemTools(s, c, actor)
	addQueryTools(s, c)
	return s
}

// readOnly marks a query tool for hosts: it changes nothing anywhere.
var readOnly = &sdk.ToolAnnotations{ReadOnlyHint: true}

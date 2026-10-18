// Package mcp implements hits-mcp, the agent action surface: an MCP stdio
// server exposing the client API to agents as tools, one tool per client
// method (hits-hq 02-DESIGN/mcp-server.md, decision 0003, amended by
// 0021). It is a client of the store, never a service — stateless, no NATS
// endpoints, no projections. The logic lives here (not in cmd/) so it is
// testable: Run takes an injectable Connector.
package mcp

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/contract"
	"github.com/impire-io/hits/internal/config"
	"github.com/impire-io/hits/internal/dial"
	"github.com/impire-io/hits/internal/version"
)

// Connector opens the client the server talks through, with the function
// that closes its connection. Production use dials a chronicle context;
// tests inject a client on an in-process chronicle.
type Connector func(contextName, actor string) (client.Client, func(), error)

// ContextConnector dials the named context ("" means the selection) as
// the dial seam chooses: the store on chronicle, or a 0.7 fleet, with
// the --actor flag's value.
func ContextConnector(contextName, actor string) (client.Client, func(), error) {
	return dial.Context(contextName, actor)
}

// defaultInitiative is indirected so Run's connect parameter keeps its
// name.
var defaultInitiative = config.DefaultInitiative

// Run executes the server: parse flags, connect — fail-fast — then serve
// MCP over stdio until ctx ends. args excludes the program name. The actor
// is the context's principal: one process is one context, and the
// context is somebody (hits-hq decision 0021).
func Run(ctx context.Context, args []string, errOut io.Writer, connect Connector) error {
	fs := flag.NewFlagSet("hits-mcp", flag.ContinueOnError)
	fs.SetOutput(errOut)
	ctxName := fs.String("context", "", "context to connect with: chronicle's, or a hits context of a 0.7 fleet (default: the selection)")
	actor := fs.String("actor", "", "acting handle for writes on a hits context (default: $HITS_ACTOR, else the hits config's default); refused on a chronicle context")
	initiativeFlag := fs.String("initiative", "", "filing default for create_item when the call names none (default: $HITS_INITIATIVE, else the selected initiative)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	// The default initiative is optional — a create_item naming its own
	// initiative never needs it — but a given one must be well-formed. It
	// is a filing default only, never a read scope (decision 0016).
	initiative := *initiativeFlag
	if initiative == "" {
		initiative = os.Getenv("HITS_INITIATIVE")
	}
	if initiative == "" {
		initiative = defaultInitiative()
	}
	if initiative != "" && !contract.ValidInitiativeSlug(initiative) {
		return fmt.Errorf("initiative %q is not a well-formed initiative slug", initiative)
	}
	c, closeConn, err := connect(*ctxName, *actor)
	if err != nil {
		return err
	}
	defer closeConn()
	return NewServer(c, initiative).Run(ctx, &sdk.StdioTransport{})
}

// NewServer builds the MCP server on an established client: the tools of
// the design's table, one per client endpoint. Adding a client endpoint
// means adding its tool here in the same change. initiative is the filing
// default create_item falls back to; "" means every create must name its
// own.
func NewServer(c client.Client, initiative string) *sdk.Server {
	s := sdk.NewServer(&sdk.Implementation{Name: "hits", Version: version.Version}, nil)
	addItemTools(s, c, initiative)
	addQueryTools(s, c)
	return s
}

// readOnly marks a query tool for hosts: it changes nothing anywhere.
var readOnly = &sdk.ToolAnnotations{ReadOnlyHint: true}

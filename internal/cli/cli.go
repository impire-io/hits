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
	"os"
	"strings"

	"github.com/nats-io/nats.go"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/internal/connect"
	"github.com/impire-io/hits/internal/version"
)

const usageText = `hits — a HITS client

Usage:
  hits [--context <name>] [--actor <handle>] [--json] <command> [flags] [args]

Item commands (the actor comes from --actor or $HITS_ACTOR):
  create      open an item:           create --type <t> [flags] <report>
  get         one item's snapshot:    get <id>
  edit        non-lifecycle changes:  edit <id> [flags]
  transition  move the status:        transition <id> --to <status> [flags]
  resolve     close as fixed:         resolve <id> [--fixed-by <ref>] [flags]
  wontfix     close without a fix:    wontfix <id> [flags]
  claim       record intent to work:  claim <id> [--steal]
  release     hand a claim back:      release <id>
  block       block on something:     block <id> [--by <what>]
  unblock     restore the status:     unblock <id>
  link        assert a typed edge:    link <id> --type <t> <to-id>
  unlink      retract an edge:        unlink <id> --type <t> <to-id>
  note        append a trail entry:   note <id> <text>
  tombstone   void a filing mistake:  tombstone <id> <reason>

Vocabulary and queries:
  project     the located-in vocabulary: register <slug> <name> | list
  search      full-text over reports and notes: search [<query>] [flags]
  semantic    nearest items to a text: semantic <text> [--limit <n>]
  graph       edges at a node: neighbors <id> | walk <id>  [flags]

Run the platform:
  up          run the service fleet in this process (flags follow the
              subcommand — see 'hits up -h')

Contexts (connection configuration, $XDG_CONFIG_HOME/hits/context):
  context     manage hits contexts: ls | add | import | edit | rm | select

Authentication (hits contexts with an oauth block):
  auth        IDP device-flow login: login | status | logout  [--context <name>]

Service:
  ping        ask the running service to identify itself
  version     print the client version

Global flags go before the command, command flags after the leading <id>.
Run 'hits <command> -h' for a command's flags.
`

// Connector opens the NATS connection a command talks through. Production use
// resolves a NATS context; tests inject a connection to an embedded server.
type Connector func(contextName string) (*nats.Conn, error)

// ContextConnector resolves the named context — hits' own or the nats
// CLI's ("" means the configured default, else the selected one).
func ContextConnector(contextName string) (*nats.Conn, error) {
	return connect.Connect(contextName, "hits")
}

// invocation carries one parsed CLI call: the global flags, the arguments
// after the command word, and the writers and connector to run it with.
type invocation struct {
	ctx         context.Context
	out         io.Writer
	errOut      io.Writer
	connect     Connector
	contextName string
	actor       string
	json        bool
	args        []string
}

// Run executes one CLI invocation. args excludes the program name.
func Run(ctx context.Context, args []string, out, errOut io.Writer, connect Connector) error {
	fs := flag.NewFlagSet("hits", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.Usage = func() { fmt.Fprint(errOut, usageText) }
	ctxName := fs.String("context", "", "hits context to connect with (default: the config's default context)")
	actor := fs.String("actor", "", "acting handle for write commands (default: $HITS_ACTOR)")
	jsonOut := fs.Bool("json", false, "print replies as indented JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	inv := &invocation{
		ctx: ctx, out: out, errOut: errOut, connect: connect,
		contextName: *ctxName, actor: *actor, json: *jsonOut,
	}
	if fs.NArg() > 0 {
		inv.args = fs.Args()[1:]
	}

	switch cmd := fs.Arg(0); cmd {
	case "create":
		return runCreate(inv)
	case "get":
		return runGet(inv)
	case "edit":
		return runEdit(inv)
	case "transition":
		return runTransition(inv)
	case "resolve":
		return runResolve(inv)
	case "wontfix":
		return runWontfix(inv)
	case "claim":
		return runClaim(inv)
	case "release":
		return runRelease(inv)
	case "block":
		return runBlock(inv)
	case "unblock":
		return runUnblock(inv)
	case "link":
		return runLink(inv, false)
	case "unlink":
		return runLink(inv, true)
	case "note":
		return runNote(inv)
	case "tombstone":
		return runTombstone(inv)
	case "project":
		return runProject(inv)
	case "search":
		return runSearch(inv)
	case "semantic":
		return runSemantic(inv)
	case "graph":
		return runGraph(inv)
	case "context":
		return runContext(inv)
	case "auth":
		return runAuth(inv)
	case "ping":
		return runPing(inv)
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

// flagSet builds a command's flag set with a one-line usage header.
func (inv *invocation) flagSet(name, usage string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(inv.errOut)
	fs.Usage = func() {
		fmt.Fprintf(inv.errOut, "Usage: hits %s\n", usage)
		fs.PrintDefaults()
	}
	return fs
}

// actorOrErr resolves the acting handle a write command signs with. It fails
// before anything touches the wire.
func (inv *invocation) actorOrErr() (string, error) {
	if inv.actor != "" {
		return inv.actor, nil
	}
	if a := os.Getenv("HITS_ACTOR"); a != "" {
		return a, nil
	}
	if a := connect.DefaultActor(); a != "" {
		return a, nil
	}
	return "", errors.New("no actor: pass --actor, set HITS_ACTOR, or set a default in the client config")
}

// dial connects and wraps the connection in the client; the returned func
// closes it.
func (inv *invocation) dial() (*client.Client, func(), error) {
	nc, err := inv.connect(inv.contextName)
	if err != nil {
		return nil, nil, fmt.Errorf("connect: %w", err)
	}
	return client.New(nc), nc.Close, nil
}

// errFlagsFirst signals that the first argument is a flag where a positional
// was expected — the caller hands the whole argument list to its flag set so
// -h and unknown-flag errors read normally.
var errFlagsFirst = errors.New("flags before positionals")

// leading peels named positionals off the front of args, before flag
// parsing, so commands read naturally: hits transition <id> --to <status>.
func leading(args []string, names ...string) ([]string, []string, error) {
	for i, name := range names {
		if i < len(args) && strings.HasPrefix(args[i], "-") {
			return nil, nil, errFlagsFirst
		}
		if i >= len(args) {
			return nil, nil, fmt.Errorf("missing %s argument", name)
		}
	}
	return args[:len(names)], args[len(names):], nil
}

// leadingID is the common one-positional case; on a leading flag it lets fs
// report it (help included) before failing on the missing positional.
func leadingID(fs *flag.FlagSet, args []string, name string) (string, []string, error) {
	lead, rest, err := leading(args, name)
	if err != nil {
		if errors.Is(err, errFlagsFirst) {
			if perr := fs.Parse(args); perr != nil {
				return "", nil, perr
			}
			err = fmt.Errorf("missing %s argument", name)
		}
		fs.Usage()
		return "", nil, fmt.Errorf("%s: %w", fs.Name(), err)
	}
	return lead[0], rest, nil
}

// noTrailing rejects unparsed arguments left after the flags.
func noTrailing(fs *flag.FlagSet) error {
	if fs.NArg() != 0 {
		fs.Usage()
		return fmt.Errorf("%s: unexpected argument %q", fs.Name(), fs.Arg(0))
	}
	return nil
}

func runPing(inv *invocation) error {
	c, closeConn, err := inv.dial()
	if err != nil {
		return err
	}
	defer closeConn()

	about, err := c.Ping(inv.ctx)
	if err != nil {
		return err
	}
	if inv.json {
		return emit(inv.out, about)
	}
	fmt.Fprintf(inv.out, "%s %s\n", about.Name, about.Version)
	return nil
}

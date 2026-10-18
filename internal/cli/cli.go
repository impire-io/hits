// Package cli implements the hits terminal client. The logic lives here (not
// in cmd/) so it is testable: Run takes an explicit context, output writers,
// and an injectable Connector, letting tests drive every command against an
// in-process chronicle.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/client/chronicle"
	"github.com/impire-io/hits/internal/dial"
	"github.com/impire-io/hits/internal/version"
)

const usageText = `hits — a HITS client, on chronicle

Usage:
  hits [--context <chronicle context>] [--json] <command> [flags] [args]

Item commands (the author of every write is the context's principal):
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
  initiative  the group-of-projects vocabulary: register <slug> <name> |
              retire <slug> --reason <r> | list | select <slug>
  project     the located-in vocabulary: register <slug> <name> --initiative <i> |
              assign <slug> --initiative <i> | retire <slug> --reason <r> | list
  release     the target vocabulary: register <slug> <name> |
              ship <slug> --ref <kind:ref> | retire <slug> --reason <r> | list
              (per initiative; 'release <id>' still hands a claim back)
  search      full-text over reports and notes: search [<query>] [flags]
  semantic    nearest items to a text: semantic <text> [--limit <n>]
  graph       edges at a node: neighbors <id> | walk <id>  [flags]

Audit (git as evidence, the tracker as the record):
  audit       hold fixed-by refs and merged work against local clones'
              main, and the writes since the cutover against chronicle's
              word: audit [--repo <slug>=<path> ...] [--since-seq <n>]

The store (a HITS is one store in a chronicle account):
  store       declare a HITS in the context's account: store init [--version <v>] |
              print the install manifest: store manifest
  migrate     move a running install's hits-ops stream into the store:
              migrate --from <hits context>  (one release's verb)

Service:
  version     print the client version

The connection is a chronicle context: 'chronicle login' for the hosted
account, 'chronicle context add' for a credential on your own NATS;
--context names one, else the selection (CHRONICLE_CONTEXT). Global flags
go before the command, command flags after the leading <id>.
Run 'hits <command> -h' for a command's flags.
`

// defaultFan is the default --fan wherever a command keeps a window of
// concurrent gets in flight.
const defaultFan = 8

// Connector opens the client a command talks through, with the function
// that closes its connection. Production use dials a chronicle context;
// tests inject a client on an in-process chronicle.
type Connector func(contextName string) (client.Client, func(), error)

// ContextConnector dials the named chronicle context ("" means the
// selected one).
func ContextConnector(contextName string) (client.Client, func(), error) {
	return dial.Context(contextName)
}

// invocation carries one parsed CLI call: the global flags, the arguments
// after the command word, and the writers and connector to run it with.
type invocation struct {
	ctx         context.Context
	out         io.Writer
	errOut      io.Writer
	connect     Connector
	contextName string
	json        bool
	args        []string
}

// Run executes one CLI invocation. args excludes the program name.
func Run(ctx context.Context, args []string, out, errOut io.Writer, connect Connector) error {
	fs := flag.NewFlagSet("hits", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.Usage = func() { fmt.Fprint(errOut, usageText) }
	ctxName := fs.String("context", "", "chronicle context to connect with (default: the selected one)")
	jsonOut := fs.Bool("json", false, "print replies as indented JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	inv := &invocation{
		ctx: ctx, out: out, errOut: errOut, connect: connect,
		contextName: *ctxName, json: *jsonOut,
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
		return runReleaseCmd(inv)
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
	case "initiative":
		return runInitiative(inv)
	case "search":
		return runSearch(inv)
	case "semantic":
		return runSemantic(inv)
	case "graph":
		return runGraph(inv)
	case "audit":
		return runAudit(inv)
	case "store":
		return runStore(inv)
	case "migrate":
		return runMigrate(inv)
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

// dial connects through the context; the returned func closes the
// connection.
func (inv *invocation) dial() (client.Client, func(), error) {
	c, closeConn, err := inv.connect(inv.contextName)
	if err != nil {
		return nil, nil, err
	}
	return c, closeConn, nil
}

// errNotChronicle is a verb that speaks to chronicle alone — store init,
// migrate, the audit's cutover read — dialled on a 0.7 fleet.
var errNotChronicle = errors.New("this verb speaks to a chronicle context; the context dialled is a 0.7 fleet")

// dialChronicle is dial for the verbs that are chronicle's alone.
func (inv *invocation) dialChronicle() (*chronicle.Client, func(), error) {
	c, closeConn, err := inv.dial()
	if err != nil {
		return nil, nil, err
	}
	cc, ok := c.(*chronicle.Client)
	if !ok {
		closeConn()
		return nil, nil, errNotChronicle
	}
	return cc, closeConn, nil
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

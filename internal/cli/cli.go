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

const usageText = `hits — a HITS client: on chronicle, or on a 0.7 fleet

Usage:
  hits [--context <name>] [--actor <handle>] [--json] <command> [flags] [args]

Item commands:
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
  migrate     move a 0.7 fleet's hits-ops stream into the store:
              migrate --from <hits context>

Contexts of a 0.7 fleet ($XDG_CONFIG_HOME/hits/context):
  context     manage hits contexts: ls | add | import | edit | rm | select
  auth        IDP device-flow login: login | status | logout  [--context <name>]

Service:
  ping        name what answers: the store's announcement, or the 0.7 node
  version     print the client version

The context chooses the substrate. A chronicle context — 'chronicle login'
for the hosted account, 'chronicle context add' for a credential on your
own NATS — reaches the store on chronicle; its author is the context's
principal, and --actor is refused. A hits context ('hits context add')
reaches a fleet frozen at the 0.7.0 release; a write there needs --actor,
$HITS_ACTOR, or the hits config's default. --context names one, resolved
against chronicle's contexts first, then hits's; without it, chronicle's
selection, else hits's default. Global flags go before the command,
command flags after the leading <id>. Run 'hits <command> -h' for a
command's flags.
`

// defaultFan is the default --fan wherever a command keeps a window of
// concurrent gets in flight.
const defaultFan = 8

// Connector opens the client a command talks through, with the function
// that closes its connection. Production use dials a context — chronicle's
// or a hits context — with the --actor flag's value; tests inject a client
// on an in-process substrate.
type Connector func(contextName, actor string) (client.Client, func(), error)

// ContextConnector dials the named context ("" means the selection) as
// the dial seam chooses: the store on chronicle, or a 0.7 fleet.
func ContextConnector(contextName, actor string) (client.Client, func(), error) {
	return dial.Context(contextName, actor)
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
	ctxName := fs.String("context", "", "context to connect with: chronicle's, or a hits context of a 0.7 fleet (default: the selection)")
	actor := fs.String("actor", "", "acting handle for writes on a hits context (default: $HITS_ACTOR, else the hits config's default); refused on a chronicle context")
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

// dial connects through the context; the returned func closes the
// connection.
func (inv *invocation) dial() (client.Client, func(), error) {
	c, closeConn, err := inv.connect(inv.contextName, inv.actor)
	if err != nil {
		return nil, nil, err
	}
	return c, closeConn, nil
}

// runPing names what answers on the context: the store's announcement on
// chronicle, the node on a 0.7 fleet.
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

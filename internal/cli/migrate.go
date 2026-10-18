package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	ccontract "github.com/impire-io/chronicle/contract"

	"github.com/impire-io/hits/client/chronicle"
	"github.com/impire-io/hits/contract"
	"github.com/impire-io/hits/internal/connect"
)

// runMigrate is the cutover's one verb (hits-hq 02-DESIGN/the-cutover.md
// § the cutover, decision 0022): read a running install's hits-ops stream
// from sequence 1 through a hits context, write every op into the
// chronicle store as an operation on its mapped path — its id as the
// message id, its actor as the author, its time as Op-Ts — then verify
// that chronicle's derived state equals the install's own fold for every
// entity, that every note's author and time are on the record, and that
// no operation is marked. It is one release's verb, removed with 0.9.0.
func runMigrate(inv *invocation) error {
	fs := inv.flagSet("migrate", "migrate --from <hits context> [--stream <name>] [--limit <n>]")
	from := fs.String("from", "", "the hits context of the install to read from (read-only)")
	stream := fs.String("stream", contract.OpsStream, "the ops stream to read")
	limit := fs.Int("limit", 0, "replay at most this many ops (0: all)")
	if err := fs.Parse(inv.args); err != nil {
		return err
	}
	if err := noTrailing(fs); err != nil {
		return err
	}
	if *from == "" {
		fs.Usage()
		return errors.New("migrate: --from <hits context> is required")
	}
	c, closeConn, err := inv.dialChronicle()
	if err != nil {
		return err
	}
	defer closeConn()
	hc, err := connect.Connect(*from, "hits-migrate")
	if err != nil {
		return fmt.Errorf("hits context %q: %w", *from, err)
	}
	defer hc.Close()
	m := &migration{ctx: inv.ctx, out: inv.out, c: c, hc: hc, stream: *stream, limit: *limit,
		items: map[string]*contract.Item{}, projects: map[string]*contract.Project{}, initiatives: map[string]*contract.Initiative{}, releases: map[string]*contract.Release{},
		counts: map[string]int{}}
	return m.run()
}

type migration struct {
	ctx    context.Context
	out    interface{ Write([]byte) (int, error) }
	c      *chronicle.Client
	hc     *nats.Conn
	js     jetstream.JetStream
	stream string
	limit  int

	items       map[string]*contract.Item
	projects    map[string]*contract.Project
	initiatives map[string]*contract.Initiative
	releases    map[string]*contract.Release // initiative + "/" + slug

	counts  map[string]int
	lastSeq uint64
}

func (m *migration) printf(format string, args ...any) { fmt.Fprintf(m.out, format, args...) }

func (m *migration) run() error {
	hjs, err := jetstream.New(m.hc)
	if err != nil {
		return err
	}
	st, err := hjs.Stream(m.ctx, m.stream)
	if err != nil {
		return fmt.Errorf("stream %s: %w", m.stream, err)
	}
	info, err := st.Info(m.ctx)
	if err != nil {
		return err
	}
	m.printf("%s: %d ops, sequences %d..%d\n", m.stream, info.State.Msgs, info.State.FirstSeq, info.State.LastSeq)
	if m.js, err = jetstream.New(m.c.Chronicle().Conn()); err != nil {
		return err
	}
	cons, err := st.OrderedConsumer(m.ctx, jetstream.OrderedConsumerConfig{DeliverPolicy: jetstream.DeliverAllPolicy})
	if err != nil {
		return err
	}
	started := time.Now()
	n := 0
	for info.State.Msgs > 0 {
		msg, err := cons.Next(jetstream.FetchMaxWait(15 * time.Second))
		if err != nil {
			return fmt.Errorf("after %d ops: %w", n, err)
		}
		meta, err := msg.Metadata()
		if err != nil {
			return err
		}
		if err := m.one(msg.Subject(), msg.Data(), meta.Sequence.Stream); err != nil {
			return err
		}
		n++
		if n%100 == 0 {
			m.printf("  %d ops migrated (%s)\n", n, time.Since(started).Round(time.Millisecond))
		}
		if meta.Sequence.Stream >= info.State.LastSeq || (m.limit > 0 && n >= m.limit) {
			break
		}
	}
	nexts, err := m.mint()
	if err != nil {
		return err
	}
	m.printf("migrated %d ops in %s: %d items, %d projects, %d initiatives, %d releases; the store's last sequence is %d\n", n, time.Since(started).Round(time.Millisecond), len(m.items), len(m.projects), len(m.initiatives), len(m.releases), m.lastSeq)
	kinds := make([]string, 0, len(m.counts))
	for k := range m.counts {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		m.printf("  %-11s %d\n", k, m.counts[k])
	}
	return m.verify(nexts)
}

// publish writes one operation directly on the store's subject — the
// bare client's route, which an importer is — with the record's own
// headers from the install's envelope.
func (m *migration) publish(path, opType string, data map[string]any, op contract.Op) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	tail, err := ccontract.PathTail(path)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	rec := ccontract.Op{ID: op.ID, Type: opType, Author: op.Actor, Ts: op.At, Payload: payload}
	msg := nats.NewMsg(ccontract.OpsSubject(m.c.Store(), tail))
	msg.Header = rec.Header()
	if opType == "create" {
		msg.Header.Set(ccontract.HdrExpectedLastSubjSeq, "0")
	}
	msg.Data = payload
	ack, err := m.js.PublishMsg(m.ctx, msg)
	if err != nil {
		return fmt.Errorf("publish %s %s (%s): %w", path, opType, op.ID, err)
	}
	m.lastSeq = ack.Sequence
	m.counts[opType]++
	return nil
}

func (m *migration) one(subject string, raw []byte, seq uint64) error {
	tokens := strings.Split(subject, ".")
	if len(tokens) < 4 || tokens[0] != "hits" || tokens[1] != "ops" {
		return fmt.Errorf("subject %q is not an ops subject", subject)
	}
	var op contract.Op
	if err := json.Unmarshal(raw, &op); err != nil {
		return fmt.Errorf("%s @%d: decode envelope: %w", subject, seq, err)
	}
	switch tokens[2] {
	case "item":
		id := tokens[3]
		prev := m.items[id]
		next, err := contract.Apply(prev, op, seq)
		if err != nil {
			return fmt.Errorf("item %s @%d (%s): the install's fold refuses: %w", id, seq, op.Op, err)
		}
		m.items[id] = next
		opType, err := contract.ItemOperation(op)
		if err != nil {
			return fmt.Errorf("item %s @%d: %w", id, seq, err)
		}
		var data map[string]any
		switch op.Op {
		case contract.OpCreated:
			data = contract.ItemState(next)
		case contract.OpNoted:
			var p contract.NotedPayload
			if err := json.Unmarshal(op.Payload, &p); err != nil {
				return err
			}
			data = map[string]any{"text": p.Text}
		default:
			data = contract.Diff(contract.ItemState(prev), contract.ItemState(next))
		}
		return m.publish(contract.ItemPath(id), opType, data, op)
	case "project":
		slug := tokens[3]
		prev := m.projects[slug]
		next, err := contract.ApplyProject(prev, op, seq)
		if err != nil {
			return fmt.Errorf("project %s @%d (%s): %w", slug, seq, op.Op, err)
		}
		m.projects[slug] = next
		return m.vocab(contract.ProjectPath(slug), op, stateOrNil(prev, contract.ProjectState), contract.ProjectState(next))
	case "initiative":
		slug := tokens[3]
		prev := m.initiatives[slug]
		next, err := contract.ApplyInitiative(prev, op, seq)
		if err != nil {
			return fmt.Errorf("initiative %s @%d (%s): %w", slug, seq, op.Op, err)
		}
		m.initiatives[slug] = next
		return m.vocab(contract.InitiativePath(slug), op, stateOrNil(prev, contract.InitiativeState), contract.InitiativeState(next))
	case "release":
		initiative := tokens[3]
		slug := strings.Join(tokens[4:], ".")
		key := initiative + "/" + slug
		prev := m.releases[key]
		next, err := contract.ApplyRelease(prev, op, seq)
		if err != nil {
			return fmt.Errorf("release %s @%d (%s): %w", key, seq, op.Op, err)
		}
		if next.Initiative == "" {
			next.Initiative = initiative
		}
		m.releases[key] = next
		return m.vocab(contract.ReleasePath(initiative, slug), op, stateOrNil(prev, contract.ReleaseState), contract.ReleaseState(next))
	}
	return fmt.Errorf("subject %q: unknown kind %q", subject, tokens[2])
}

func stateOrNil[T any](prev *T, state func(*T) map[string]any) map[string]any {
	if prev == nil {
		return nil
	}
	return state(prev)
}

func (m *migration) vocab(path string, op contract.Op, prev, next map[string]any) error {
	opType, err := contract.VocabOperation(op)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if op.Op == contract.OpRegistered {
		if strings.HasPrefix(path, contract.TypeInitiative+"/") && !strings.Contains(path, "/"+contract.ReleasesChild+"/") {
			next["next"] = 1 // the counter is born with the initiative; mint raises it last
		}
		return m.publish(path, opType, next, op)
	}
	return m.publish(path, opType, contract.Diff(prev, next), op)
}

// mint writes each initiative's counter last: the next number after the
// highest the corpus holds under its prefix — what the install would mint
// next. Legacy bare ids stay outside every counter.
func (m *migration) mint() (map[string]int, error) {
	next := map[string]int{}
	for id := range m.items {
		prefix, ok := contract.ParseItemID(id)
		if !ok || prefix == "" {
			continue
		}
		n, err := strconv.Atoi(id[len(prefix)+1:])
		if err != nil {
			continue
		}
		if n >= next[prefix] {
			next[prefix] = n + 1
		}
	}
	for slug := range m.initiatives {
		if next[slug] == 0 {
			next[slug] = 1
		}
		op := contract.Op{ID: "mint-" + slug, Op: "mint", Entity: slug, Actor: m.c.Author(), At: time.Now().UTC(), V: contract.EnvelopeVersion}
		if err := m.publish(contract.InitiativePath(slug), "mint", map[string]any{"next": next[slug]}, op); err != nil {
			return nil, err
		}
	}
	return next, nil
}

// --- the verification: the cutover's acceptance test ------------------------

type mismatch struct {
	path, field string
	want, got   any
}

type mark struct {
	path, decision, detail string
	seq                    uint64
}

func (m *migration) verifyState(path string, want map[string]any, out *[]mismatch) error {
	sv, err := m.c.Chronicle().State(m.ctx, m.c.Store(), path)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	var got map[string]any
	if err := json.Unmarshal(sv.State, &got); err != nil {
		return fmt.Errorf("%s: state is not an object: %w", path, err)
	}
	for k, v := range want {
		if !reflect.DeepEqual(got[k], v) {
			*out = append(*out, mismatch{path, k, v, got[k]})
		}
	}
	for k, v := range got {
		if _, ok := want[k]; !ok {
			*out = append(*out, mismatch{path, k, nil, v})
		}
	}
	return nil
}

func (m *migration) trail(path string, it *contract.Item, marks *[]mark, out *[]mismatch) error {
	cc := m.c.Chronicle()
	res, err := cc.Resolve(m.ctx, m.c.Store(), path)
	if err != nil {
		return fmt.Errorf("%s: resolve: %w", path, err)
	}
	var state json.RawMessage
	notes := 0
	for op, err := range cc.History(m.ctx, m.c.Store(), path) {
		if err != nil {
			return fmt.Errorf("%s: history: %w", path, err)
		}
		if op.Type == "note" {
			if notes < len(it.Notes) {
				n := it.Notes[notes]
				if op.Author != n.Author {
					*out = append(*out, mismatch{path, fmt.Sprintf("note %d author", notes+1), n.Author, op.Author})
				}
				if !op.Ts.Equal(n.At) {
					*out = append(*out, mismatch{path, fmt.Sprintf("note %d time", notes+1), n.At.UTC().Format(time.RFC3339Nano), op.Ts.UTC().Format(time.RFC3339Nano)})
				}
			}
			notes++
		}
		step := ccontract.FoldStep(res, state, op)
		if step.Moved {
			state = step.State
		}
		switch step.Decision {
		case ccontract.Merge, ccontract.None, ccontract.Reset:
		default:
			*marks = append(*marks, mark{path, step.Decision.String(), step.Detail, op.Seq})
		}
	}
	if notes != len(it.Notes) {
		*out = append(*out, mismatch{path, "notes", len(it.Notes), notes})
	}
	return nil
}

func (m *migration) verify(nexts map[string]int) error {
	time.Sleep(2 * time.Second) // let the fold catch up with the last write
	var mismatches []mismatch
	var marks []mark
	for id, it := range m.items {
		path := contract.ItemPath(id)
		if err := m.verifyState(path, contract.ItemState(it), &mismatches); err != nil {
			return err
		}
		if err := m.trail(path, it, &marks, &mismatches); err != nil {
			return err
		}
	}
	for slug, p := range m.projects {
		if err := m.verifyState(contract.ProjectPath(slug), contract.ProjectState(p), &mismatches); err != nil {
			return err
		}
	}
	for slug, i := range m.initiatives {
		want := contract.InitiativeState(i)
		want["next"] = float64(nexts[slug])
		if err := m.verifyState(contract.InitiativePath(slug), want, &mismatches); err != nil {
			return err
		}
	}
	for _, r := range m.releases {
		if err := m.verifyState(contract.ReleasePath(r.Initiative, r.Slug), contract.ReleaseState(r), &mismatches); err != nil {
			return err
		}
	}
	sort.Slice(mismatches, func(i, j int) bool {
		return mismatches[i].path+mismatches[i].field < mismatches[j].path+mismatches[j].field
	})
	sort.Slice(marks, func(i, j int) bool { return marks[i].seq < marks[j].seq })
	m.printf("verified %d items, %d projects, %d initiatives, %d releases: %d mismatches, %d marks\n", len(m.items), len(m.projects), len(m.initiatives), len(m.releases), len(mismatches), len(marks))
	for _, x := range mismatches {
		w, _ := json.Marshal(x.want)
		g, _ := json.Marshal(x.got)
		m.printf("  mismatch %s %s: want %s, got %s\n", x.path, x.field, w, g)
	}
	for _, x := range marks {
		m.printf("  mark %s @%d %s: %s\n", x.path, x.seq, x.decision, x.detail)
	}
	if len(mismatches) > 0 || len(marks) > 0 {
		return errors.New("the migration does not reproduce the install; the store must be discarded before a second attempt")
	}
	m.printf("the migration reproduces the install: every state, every note's author and time, no operation marked; audit with --since-seq %d from here on\n", m.lastSeq)
	return nil
}

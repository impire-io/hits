// Package chronicle is the client on chronicle (hits-hq 02-DESIGN/the-client.md,
// decision 0021): the store's declarations, the write loop and the reads
// of a HITS that lives in a chronicle account. A write reads the
// instance's state, checks the command against the model's invariants,
// folds the next state with the contract's own fold, and sends the
// difference through chronicle's node under the expected-sequence guard;
// a read is chronicle's read of state, history, listings and indexes.
package chronicle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"sync"
	"time"

	chron "github.com/impire-io/chronicle/client"
	ccontract "github.com/impire-io/chronicle/contract"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/contract"
)

// Client speaks one HITS: a store in the account a chronicle client is
// connected to. The chronicle client's lifecycle stays with the caller.
type Client struct {
	cc    *chron.Client
	store string

	namesMu sync.Mutex
	names   map[string]string
}

// New wraps a connected chronicle client for the named store ("" is the
// default, hits).
func New(cc *chron.Client, store string) *Client {
	if store == "" {
		store = contract.StoreName
	}
	return &Client{cc: cc, store: store, names: map[string]string{}}
}

// Author is the actor every write of this client carries: the principal
// the chronicle client connected as, vouched for by chronicle.
func (c *Client) Author() string { return c.cc.Author() }

// Store is the store this client speaks to.
func (c *Client) Store() string { return c.store }

// Chronicle is the underlying chronicle client, for the one caller that
// needs the wire itself: the migration.
func (c *Client) Chronicle() *chron.Client { return c.cc }

// refusal puts every error a write or read can meet in the client's
// words: an invariant keeps its name, a missing instance is not-found, a
// rule chronicle refused is refused, a service code is itself.
func refusal(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		return err
	}
	var invErr *contract.InvariantError
	if errors.As(err, &invErr) {
		return &client.APIError{Code: invErr.Name, Message: invErr.Message}
	}
	switch {
	case errors.Is(err, chron.ErrNoState):
		return &client.APIError{Code: "not-found", Message: err.Error()}
	case errors.Is(err, chron.ErrPreconditionFailed), errors.Is(err, chron.ErrStateOffSchema), errors.Is(err, chron.ErrSchemaViolation), errors.Is(err, chron.ErrUndefinedOperation):
		return &client.APIError{Code: "refused", Message: err.Error()}
	}
	var serr *chron.ServiceError
	if errors.As(err, &serr) {
		return &client.APIError{Code: serr.Code, Message: serr.Desc}
	}
	return err
}

func inv(name, format string, args ...any) error {
	return &contract.InvariantError{Name: name, Message: fmt.Sprintf(format, args...)}
}

// maxRetries bounds the write loop's retries after a guard miss: the
// instance moved under the check, and the retry is the same judgement
// against the state that moved it.
const maxRetries = 8

// head is what a write reads first: the instance's exact current state
// and the sequence of its last operation. Chronicle's state bucket is
// derived and may trail the history by the fold's few milliseconds, so
// the read is the exactness recipe (chronicle-hq design 03 § the state
// buckets): the bucket's value, then every operation after it folded
// here under the current declarations — which also carries the reader
// past operations with effect none and marked ones, whose sequence the
// guard needs.
type head struct {
	exists   bool
	state    json.RawMessage
	stateSeq uint64
	last     uint64
}

func (c *Client) head(ctx context.Context, path string) (head, error) {
	res, err := c.cc.Resolve(ctx, c.store, path)
	if err != nil {
		return head{}, err
	}
	var h head
	var ops iter.Seq2[ccontract.Op, error]
	sv, err := c.cc.State(ctx, c.store, path)
	switch {
	case err == nil:
		h = head{exists: true, state: sv.State, stateSeq: sv.Seq, last: sv.Seq}
		ops = c.cc.FoldTail(ctx, c.store, path, sv.Seq)
	case errors.Is(err, chron.ErrNoState):
		ops = c.cc.History(ctx, c.store, path)
	default:
		return head{}, err
	}
	for op, err := range ops {
		if err != nil {
			if errors.Is(err, chron.ErrNoState) {
				break
			}
			return head{}, err
		}
		out := ccontract.FoldStep(res, h.state, op)
		if out.Moved {
			h.state, h.stateSeq, h.exists = out.State, op.Seq, true
		}
		h.last = op.Seq
	}
	return h, nil
}

// settle waits for the state bucket to carry a write the client just
// made, so the listings that read the bucket see it — read-your-writes
// for one client, bounded, never required for correctness: every exact
// read folds the tail itself.
func (c *Client) settle(ctx context.Context, path string, seq uint64) {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		sv, err := c.cc.State(ctx, c.store, path)
		if err == nil && sv.Seq >= seq {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// write is one operation on one path: a create with the create-only
// guard, or an apply with the expected sequence.
type write struct {
	path   string
	opType string
	data   any
	opID   string
	create bool
	expect uint64
}

// send writes through chronicle's node — a verified author — with the
// caller's op id as the message id, so a retry cannot land twice. A
// create whose only precondition fails — the state is not empty — is the
// instance existing, said as the guard says it. After a merge the client
// settles on the bucket, so its own listings see the write.
func (c *Client) send(ctx context.Context, w write) error {
	payload, err := json.Marshal(w.data)
	if err != nil {
		return err
	}
	opts := []chron.ApplyOpt{chron.Verified()}
	if w.opID != "" {
		opts = append(opts, chron.WithOpID(w.opID))
	}
	var ack chron.Ack
	if w.create {
		ack, err = c.cc.Create(ctx, c.store, w.path, "create", payload, opts...)
		if errors.Is(err, chron.ErrPreconditionFailed) {
			return fmt.Errorf("%w: %s", chron.ErrInstanceExists, w.path)
		}
	} else {
		opts = append(opts, chron.WithExpectedSeq(w.expect))
		ack, err = c.cc.Apply(ctx, c.store, w.path, w.opType, payload, opts...)
	}
	if err != nil {
		return err
	}
	if w.opType != "note" {
		c.settle(ctx, w.path, ack.Seq)
	}
	return nil
}

// moved says the instance moved under the check: a guard miss, or a
// precondition the state no longer satisfies — chronicle's pre-flight
// reads the state fresh and refuses before the guard is tested — and the
// loop reads again, so the refusal, if it stands, is the model's own.
func moved(err error) bool {
	return errors.Is(err, chron.ErrInstanceMoved) || errors.Is(err, chron.ErrPreconditionFailed) || errors.Is(err, chron.ErrStateOffSchema)
}

// stateOf reads one instance's state by path, or nil when it has none.
func (c *Client) stateOf(ctx context.Context, path string) (json.RawMessage, error) {
	sv, err := c.cc.State(ctx, c.store, path)
	if errors.Is(err, chron.ErrNoState) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return sv.State, nil
}

// nameOf is the registered name of a project, an initiative or a release,
// for the graph's nodes — read once per client and kept.
func (c *Client) nameOf(ctx context.Context, path string) string {
	c.namesMu.Lock()
	name, ok := c.names[path]
	c.namesMu.Unlock()
	if ok {
		return name
	}
	state, err := c.stateOf(ctx, path)
	if err != nil || state == nil {
		return ""
	}
	var s struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(state, &s)
	c.namesMu.Lock()
	c.names[path] = s.Name
	c.namesMu.Unlock()
	return s.Name
}

// Ping reads the store's announcement: the solution's name and version
// as hits store init declared them.
func (c *Client) Ping(ctx context.Context) (client.About, error) {
	sv, err := c.cc.State(ctx, c.store, AnnouncementPath)
	if err != nil {
		return client.About{}, refusal(err)
	}
	var about client.About
	if err := json.Unmarshal(sv.State, &about); err != nil {
		return client.About{}, fmt.Errorf("decode announcement: %w", err)
	}
	return about, nil
}

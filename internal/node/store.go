package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/impire-io/hits/contract"
)

// The ops-log and projection resource names live in contract, declared
// once for every service (hits-hq/02-DESIGN/hits-up.md § boundaries).
const (
	counterKey       = "item-counter"
	itemHistory      = 10 // KV revisions kept per item — "the last few states"
	maxWriteAttempts = 8
)

// store owns the ops-log stream and its projections. The stream is the
// source of truth; hits-items and hits-projects are folds of it, disposable
// and rebuilt by replay.
type store struct {
	js       jetstream.JetStream
	stream   jetstream.Stream
	items    jetstream.KeyValue
	projects jetstream.KeyValue
	meta     jetstream.KeyValue
}

func openStore(ctx context.Context, nc *nats.Conn, cfg Config) (*store, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("jetstream: %w", err)
	}
	// Every resource declares a byte budget, and the ops stream refuses new
	// writes at its cap (DiscardNew) — full is an operational signal to
	// raise the budget, never silent trimming of the source of record
	// (hits-hq decision 0005).
	stream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:        contract.OpsStream,
		Description: "HITS ops-log — the source of record; items are kept forever",
		Subjects:    []string{contract.OpsSubjects},
		Storage:     jetstream.FileStorage,
		Retention:   jetstream.LimitsPolicy,
		MaxBytes:    cfg.opsMaxBytes(),
		Discard:     jetstream.DiscardNew,
	})
	if err != nil {
		return nil, fmt.Errorf("ensure stream %s: %w", contract.OpsStream, err)
	}
	s := &store{js: js, stream: stream}
	for _, b := range []struct {
		into    *jetstream.KeyValue
		cfg     jetstream.KeyValueConfig
		purpose string
	}{
		{&s.items, jetstream.KeyValueConfig{Bucket: contract.ItemsBucket, History: itemHistory,
			MaxBytes:    cfg.itemsMaxBytes(),
			Description: "item snapshots — a projection of the ops-log"}, "items"},
		{&s.projects, jetstream.KeyValueConfig{Bucket: contract.ProjectsBucket,
			MaxBytes:    smallBucketMaxBytes,
			Description: "the located-in vocabulary — a projection of the ops-log"}, "projects"},
		{&s.meta, jetstream.KeyValueConfig{Bucket: contract.MetaBucket,
			MaxBytes:    smallBucketMaxBytes,
			Description: "operational state that is not derived from the log (the item id counter)"}, "meta"},
	} {
		kv, err := js.CreateOrUpdateKeyValue(ctx, b.cfg)
		if err != nil {
			return nil, fmt.Errorf("ensure %s bucket: %w", b.purpose, err)
		}
		*b.into = kv
	}
	return s, nil
}

// mintID allocates the next dense item ID via a CAS-update loop on the
// counter key — the allocate-issue.sh trick without the git.
func (s *store) mintID(ctx context.Context) (string, error) {
	for {
		entry, err := s.meta.Get(ctx, counterKey)
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			if _, cerr := s.meta.Create(ctx, counterKey, []byte("1")); cerr != nil {
				if isCASConflict(cerr) {
					continue // raced the first mint; re-read
				}
				return "", fmt.Errorf("init item counter: %w", cerr)
			}
			return "1", nil
		}
		if err != nil {
			return "", fmt.Errorf("read item counter: %w", err)
		}
		n, err := strconv.ParseUint(string(entry.Value()), 10, 64)
		if err != nil {
			return "", fmt.Errorf("corrupt item counter %q: %w", entry.Value(), err)
		}
		next := strconv.FormatUint(n+1, 10)
		if _, err := s.meta.Update(ctx, counterKey, []byte(next), entry.Revision()); err != nil {
			if isCASConflict(err) {
				continue // someone else minted; take the next one
			}
			return "", fmt.Errorf("advance item counter: %w", err)
		}
		return next, nil
	}
}

// isCASConflict matches every shape a JetStream compare-and-swap failure
// takes: wrong last sequence on publish (10071, or 10164 on replicated
// streams) and the KV layer's ErrKeyExists mapping of the same.
func isCASConflict(err error) bool {
	if errors.Is(err, jetstream.ErrKeyExists) {
		return true
	}
	var apiErr *jetstream.APIError
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode == jetstream.JSErrCodeStreamWrongLastSequence ||
			apiErr.ErrorCode == 10164 // JSErrCodeStreamWrongLastSequenceConstant
	}
	return false
}

func (s *store) loadItem(ctx context.Context, id string) (*contract.Item, uint64, error) {
	return loadSnapshot[contract.Item](ctx, s.items, id)
}

func (s *store) loadProject(ctx context.Context, slug string) (*contract.Project, uint64, error) {
	return loadSnapshot[contract.Project](ctx, s.projects, slug)
}

func loadSnapshot[T any](ctx context.Context, kv jetstream.KeyValue, key string) (*T, uint64, error) {
	entry, err := kv.Get(ctx, key)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("read snapshot %s: %w", key, err)
	}
	var v T
	if err := json.Unmarshal(entry.Value(), &v); err != nil {
		return nil, 0, fmt.Errorf("decode snapshot %s: %w", key, err)
	}
	return &v, entry.Revision(), nil
}

// saveItem writes a snapshot at the expected KV revision. On a revision
// conflict another writer folded first: the newer snapshot (by ops-log
// sequence) wins, never the later writer.
func (s *store) saveItem(ctx context.Context, it *contract.Item, rev uint64) (*contract.Item, error) {
	data, err := json.Marshal(it)
	if err != nil {
		return nil, fmt.Errorf("encode snapshot %s: %w", it.ID, err)
	}
	for {
		if rev == 0 {
			_, err = s.items.Create(ctx, it.ID, data)
		} else {
			_, err = s.items.Update(ctx, it.ID, data, rev)
		}
		if err == nil {
			return it, nil
		}
		if !isCASConflict(err) {
			return nil, fmt.Errorf("write snapshot %s: %w", it.ID, err)
		}
		current, curRev, lerr := s.loadItem(ctx, it.ID)
		if lerr != nil {
			return nil, lerr
		}
		if current != nil && current.Seq >= it.Seq {
			return current, nil
		}
		rev = curRev
	}
}

func (s *store) saveProject(ctx context.Context, p *contract.Project, rev uint64) (*contract.Project, error) {
	data, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("encode project %s: %w", p.Slug, err)
	}
	for {
		if rev == 0 {
			_, err = s.projects.Create(ctx, p.Slug, data)
		} else {
			_, err = s.projects.Update(ctx, p.Slug, data, rev)
		}
		if err == nil {
			return p, nil
		}
		if !isCASConflict(err) {
			return nil, fmt.Errorf("write project %s: %w", p.Slug, err)
		}
		current, curRev, lerr := s.loadProject(ctx, p.Slug)
		if lerr != nil {
			return nil, lerr
		}
		if current != nil && current.Seq >= p.Seq {
			return current, nil
		}
		rev = curRev
	}
}

// execItem runs one command through the write path: load the snapshot, let
// build shape the op from current state, check the invariants, publish with
// per-subject CAS, fold, save. A CAS conflict means the snapshot was stale —
// catch up from the stream and retry against fresh state.
func (s *store) execItem(ctx context.Context, id string, build func(current *contract.Item) (contract.Op, error)) (*contract.Item, error) {
	for attempt := 0; attempt < maxWriteAttempts; attempt++ {
		current, rev, err := s.loadItem(ctx, id)
		if err != nil {
			return nil, err
		}
		op, err := build(current)
		if err != nil {
			return nil, err
		}
		if err := contract.CheckOp(current, op); err != nil {
			return nil, err
		}
		next, err := s.publishAndFold(ctx, current, rev, op)
		if isCASConflict(err) {
			if cerr := s.catchUp(ctx, id); cerr != nil {
				return nil, cerr
			}
			continue
		}
		return next, err
	}
	return nil, fmt.Errorf("item %s: gave up after %d contended writes", id, maxWriteAttempts)
}

// publishAndFold appends the op (expecting the snapshot's sequence to be the
// subject's last) and folds it into the KV projection.
func (s *store) publishAndFold(ctx context.Context, current *contract.Item, rev uint64, op contract.Op) (*contract.Item, error) {
	data, err := json.Marshal(op)
	if err != nil {
		return nil, fmt.Errorf("encode op: %w", err)
	}
	var lastSeq uint64
	if current != nil {
		lastSeq = current.Seq
	}
	ack, err := s.js.Publish(ctx, contract.ItemOpsPrefix+op.Entity, data,
		jetstream.WithMsgID(op.ID),
		jetstream.WithExpectLastSequencePerSubject(lastSeq))
	if err != nil {
		return nil, err
	}
	next, err := contract.Apply(current, op, ack.Sequence)
	if err != nil {
		return nil, fmt.Errorf("fold %s op on item %s: %w", op.Op, op.Entity, err)
	}
	return s.saveItem(ctx, next, rev)
}

// catchUp folds every op the snapshot has not seen yet — the recovery path
// after a crash between publish and fold, or after losing a write race to
// another instance.
func (s *store) catchUp(ctx context.Context, id string) error {
	current, rev, err := s.loadItem(ctx, id)
	if err != nil {
		return err
	}
	subject := contract.ItemOpsPrefix + id
	last, err := s.stream.GetLastMsgForSubject(ctx, subject)
	if errors.Is(err, jetstream.ErrMsgNotFound) {
		return nil // no ops at all; nothing to fold
	}
	if err != nil {
		return fmt.Errorf("read last op for %s: %w", id, err)
	}
	if current != nil && last.Sequence <= current.Seq {
		return nil
	}
	var start uint64 = 1
	if current != nil {
		start = current.Seq + 1
	}
	err = s.foldRange(ctx, []string{subject}, start, last.Sequence, func(op contract.Op, seq uint64) error {
		next, aerr := contract.Apply(current, op, seq)
		if aerr != nil {
			return aerr
		}
		current = next
		return nil
	})
	if err != nil {
		return fmt.Errorf("catch up item %s: %w", id, err)
	}
	if current == nil {
		return nil // nothing folded; the range was already compacted away
	}
	_, err = s.saveItem(ctx, current, rev)
	return err
}

// replay folds the whole ops-log into the projections, skipping everything
// already folded (snapshots carry the sequence of their last op). Deleting
// the buckets and calling this reproduces them — the FR-31 guarantee.
func (s *store) replay(ctx context.Context) error {
	info, err := s.stream.Info(ctx)
	if err != nil {
		return fmt.Errorf("stream info: %w", err)
	}
	if info.State.LastSeq == 0 {
		return nil
	}
	return s.foldRange(ctx, []string{contract.OpsSubjects}, 0, info.State.LastSeq, func(op contract.Op, seq uint64) error {
		return s.foldOne(ctx, op, seq)
	})
}

func (s *store) foldOne(ctx context.Context, op contract.Op, seq uint64) error {
	switch op.Op {
	case contract.OpRegistered:
		current, rev, err := s.loadProject(ctx, op.Entity)
		if err != nil {
			return err
		}
		if current != nil && seq <= current.Seq {
			return nil
		}
		next, err := contract.ApplyProject(current, op, seq)
		if err != nil {
			return err
		}
		_, err = s.saveProject(ctx, next, rev)
		return err
	default:
		current, rev, err := s.loadItem(ctx, op.Entity)
		if err != nil {
			return err
		}
		if current != nil && seq <= current.Seq {
			return nil
		}
		next, err := contract.Apply(current, op, seq)
		if err != nil {
			return err
		}
		_, err = s.saveItem(ctx, next, rev)
		return err
	}
}

// foldRange reads ops in stream order via an ordered consumer — the only
// delivery shape that guarantees per-subject order — from startSeq (0 means
// the beginning) through at least lastSeq, handing each to fn.
func (s *store) foldRange(ctx context.Context, subjects []string, startSeq, lastSeq uint64, fn func(op contract.Op, seq uint64) error) error {
	cfg := jetstream.OrderedConsumerConfig{FilterSubjects: subjects}
	if startSeq > 0 {
		cfg.DeliverPolicy = jetstream.DeliverByStartSequencePolicy
		cfg.OptStartSeq = startSeq
	}
	cons, err := s.stream.OrderedConsumer(ctx, cfg)
	if err != nil {
		return fmt.Errorf("ordered consumer: %w", err)
	}
	for {
		batch, err := cons.FetchNoWait(256)
		if err != nil {
			return fmt.Errorf("fetch ops: %w", err)
		}
		got := 0
		for msg := range batch.Messages() {
			got++
			md, err := msg.Metadata()
			if err != nil {
				return fmt.Errorf("op metadata: %w", err)
			}
			var op contract.Op
			if err := json.Unmarshal(msg.Data(), &op); err != nil {
				return fmt.Errorf("decode op at seq %d: %w", md.Sequence.Stream, err)
			}
			if err := fn(op, md.Sequence.Stream); err != nil {
				return err
			}
			if md.Sequence.Stream >= lastSeq {
				return nil
			}
		}
		if err := batch.Error(); err != nil {
			return fmt.Errorf("fetch ops: %w", err)
		}
		if got == 0 {
			return nil // drained: nothing pending below lastSeq
		}
	}
}

// registerProject validates and appends a project registration; slug
// uniqueness is the expected-sequence-zero publish.
func (s *store) registerProject(ctx context.Context, op contract.Op) (*contract.Project, error) {
	current, _, err := s.loadProject(ctx, op.Entity)
	if err != nil {
		return nil, err
	}
	if err := contract.CheckProjectOp(current, op); err != nil {
		return nil, err
	}
	data, err := json.Marshal(op)
	if err != nil {
		return nil, fmt.Errorf("encode op: %w", err)
	}
	ack, err := s.js.Publish(ctx, contract.ProjectOpsPrefix+op.Entity, data,
		jetstream.WithMsgID(op.ID),
		jetstream.WithExpectLastSequencePerSubject(0))
	if err != nil {
		if isCASConflict(err) {
			return nil, &contract.InvariantError{Name: "already-registered",
				Message: fmt.Sprintf("project %s is already registered", op.Entity)}
		}
		return nil, err
	}
	next, err := contract.ApplyProject(nil, op, ack.Sequence)
	if err != nil {
		return nil, err
	}
	return s.saveProject(ctx, next, 0)
}

// listProjects reads the registry projection.
func (s *store) listProjects(ctx context.Context) ([]contract.Project, error) {
	lister, err := s.projects.ListKeys(ctx)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	out := []contract.Project{}
	for key := range lister.Keys() {
		p, _, err := s.loadProject(ctx, key)
		if err != nil {
			return nil, err
		}
		if p != nil {
			out = append(out, *p)
		}
	}
	return out, nil
}

// checkRegistered rejects located-in values that name unregistered projects
// — the registry check the contract package cannot do without I/O.
func (s *store) checkRegistered(ctx context.Context, locatedIn []string) error {
	for _, slug := range locatedIn {
		p, _, err := s.loadProject(ctx, slug)
		if err != nil {
			return err
		}
		if p == nil {
			return &contract.InvariantError{Name: "unregistered-project",
				Message: fmt.Sprintf("located-in names %q, which is not a registered project", slug)}
		}
	}
	return nil
}

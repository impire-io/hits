package semantic

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nats.go/micro"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/contract"
	"github.com/impire-io/hits/internal/version"
)

const (
	serviceName = "hits-semantic"
	serviceDesc = "HITS semantic index — an embedding projection of the ops-log"
)

// Service is one running hits-semantic instance.
type Service struct {
	micro      micro.Service
	cons       jetstream.ConsumeContext
	foldCancel context.CancelFunc
}

// Stop takes the service off the wire and stops the consumer.
func (s *Service) Stop() {
	if s.micro != nil {
		_ = s.micro.Stop()
	}
	s.cons.Stop()
	s.foldCancel()
}

// Start builds the embedding collection by replaying the ops-log and
// registers the hits-semantic micro service only once caught up with the
// backlog measured at start. An item whose embedding call fails is skipped
// with a log line — degraded, not down (spec 004 FR-04).
func Start(ctx context.Context, nc *nats.Conn, cfg Config) (*Service, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("jetstream: %w", err)
	}
	stream, err := js.Stream(ctx, contract.OpsStream)
	if err != nil {
		return nil, fmt.Errorf("ops-log stream %q not found (is hits-node running?): %w", contract.OpsStream, err)
	}
	sinfo, err := stream.Info(ctx, jetstream.WithSubjectFilter(contract.ItemOpsSubjects))
	if err != nil {
		return nil, fmt.Errorf("stream info: %w", err)
	}
	var pending uint64
	for _, n := range sinfo.State.Subjects {
		pending += n
	}

	idx, err := newChromemIndex(cfg)
	if err != nil {
		return nil, err
	}
	cons, err := stream.OrderedConsumer(ctx, jetstream.OrderedConsumerConfig{
		FilterSubjects: []string{contract.ItemOpsSubjects},
	})
	if err != nil {
		return nil, fmt.Errorf("ordered consumer: %w", err)
	}

	ready := make(chan struct{})
	if pending == 0 {
		close(ready)
	}
	// The consumer outlives Start, so folds — embedding calls included —
	// run under a service-lifetime context that Stop cancels, never under
	// the Start context the caller is free to cancel once Start returns.
	foldCtx, foldCancel := context.WithCancel(context.Background())
	items := map[string]*contract.Item{}
	cc, err := cons.Consume(func(msg jetstream.Msg) {
		md, err := msg.Metadata()
		if err != nil {
			log.Printf("hits-semantic: op metadata: %v", err)
			return
		}
		var op contract.Op
		if err := json.Unmarshal(msg.Data(), &op); err != nil {
			log.Printf("hits-semantic: decode op at seq %d: %v", md.Sequence.Stream, err)
			return
		}
		fold(foldCtx, idx, items, op, md.Sequence.Stream)
		if pending > 0 {
			pending--
			if pending == 0 {
				close(ready)
			}
		}
	})
	if err != nil {
		foldCancel()
		return nil, fmt.Errorf("consume ops: %w", err)
	}

	select {
	case <-ready:
	case <-ctx.Done():
		cc.Stop()
		foldCancel()
		return nil, fmt.Errorf("catching up with the ops-log: %w", ctx.Err())
	}

	svc, err := micro.AddService(nc, micro.Config{
		Name:        serviceName,
		Version:     version.Version,
		Description: serviceDesc,
	})
	if err != nil {
		cc.Stop()
		foldCancel()
		return nil, fmt.Errorf("register service: %w", err)
	}
	if err := svc.AddEndpoint("query", micro.HandlerFunc(queryHandler(idx)),
		micro.WithEndpointSubject(client.SemanticSubject)); err != nil {
		_ = svc.Stop()
		cc.Stop()
		foldCancel()
		return nil, fmt.Errorf("add query endpoint: %w", err)
	}
	// Flush so the endpoint subscriptions have reached the server: once
	// Start returns, a request from any connection must find a responder.
	if err := nc.FlushTimeout(5 * time.Second); err != nil {
		_ = svc.Stop()
		cc.Stop()
		foldCancel()
		return nil, fmt.Errorf("flush endpoint subscriptions: %w", err)
	}
	return &Service{micro: svc, cons: cc, foldCancel: foldCancel}, nil
}

// fold applies one op; only text-changing ops re-embed, and a tombstone
// removes the document.
func fold(ctx context.Context, idx indexer, items map[string]*contract.Item, op contract.Op, seq uint64) {
	next, err := contract.Apply(items[op.Entity], op, seq)
	if err != nil {
		log.Printf("hits-semantic: fold %s op on item %s: %v", op.Op, op.Entity, err)
		return
	}
	items[op.Entity] = next
	switch {
	case next.Tombstoned:
		if err := idx.remove(ctx, next.ID); err != nil {
			log.Printf("hits-semantic: %v", err)
		}
	case op.Op == contract.OpCreated || op.Op == contract.OpNoted:
		if err := idx.upsert(ctx, next.ID, textOf(next)); err != nil {
			// Degraded, not down: the item is simply not findable
			// semantically until a later re-embed succeeds.
			log.Printf("hits-semantic: %v", err)
		}
	}
}

// textOf is the embedded document: the report plus the trail.
func textOf(it *contract.Item) string {
	parts := make([]string, 0, 1+len(it.Notes))
	parts = append(parts, it.Report)
	for _, n := range it.Notes {
		parts = append(parts, n.Text)
	}
	return strings.Join(parts, "\n")
}

func queryHandler(idx indexer) func(micro.Request) {
	return func(req micro.Request) {
		var r client.SemanticRequest
		if err := json.Unmarshal(req.Data(), &r); err != nil {
			_ = req.Error("invalid-request", "malformed request: "+err.Error(), nil)
			return
		}
		if r.Text == "" {
			_ = req.Error("invalid-request", "semantic search needs text", nil)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		hits, err := idx.query(ctx, r.Text, r.Limit)
		if err != nil {
			_ = req.Error("internal", err.Error(), nil)
			return
		}
		respond(req, client.SemanticReply{Hits: hits})
	}
}

func respond(req micro.Request, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		_ = req.Error("internal", "encode reply: "+err.Error(), nil)
		return
	}
	_ = req.Respond(data)
}

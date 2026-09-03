package search

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nats.go/micro"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/contract"
	"github.com/impire-io/hits/internal/version"
)

const (
	serviceName       = "hits-search"
	serviceDesc       = "HITS full-text index — a projection of the ops-log"
	queryEndpointName = "query"
)

// Service is one running hits-search instance: the ordered consumer folding
// the ops-log and the micro service answering queries.
type Service struct {
	micro micro.Service
	cons  jetstream.ConsumeContext
	idx   indexer
}

// Stop takes the service off the wire and stops the consumer.
func (s *Service) Stop() {
	if s.micro != nil {
		_ = s.micro.Stop()
	}
	s.cons.Stop()
	_ = s.idx.close()
}

// Start builds the index by replaying the ops-log from sequence 1 and only
// then registers the hits-search micro service — until caught up with the
// stream head observed here, the service is not on the wire. The consumer
// keeps running as the live tail; ordered-consumer gap recovery plus the
// fold's sequence idempotence keep the index consistent.
func Start(ctx context.Context, nc *nats.Conn) (*Service, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("jetstream: %w", err)
	}
	stream, err := js.Stream(ctx, contract.OpsStream)
	if err != nil {
		return nil, fmt.Errorf("ops-log stream %q not found (is hits-node running?): %w", contract.OpsStream, err)
	}

	idx, err := newBleveIndex()
	if err != nil {
		return nil, err
	}

	// Measure the backlog of item ops before consuming: the consumer is
	// filtered, so the stream head alone cannot say when we are caught up —
	// the head may be a project op the consumer never sees.
	sinfo, err := stream.Info(ctx, jetstream.WithSubjectFilter(contract.ItemOpsSubjects))
	if err != nil {
		_ = idx.close()
		return nil, fmt.Errorf("stream info: %w", err)
	}
	var pending uint64
	for _, n := range sinfo.State.Subjects {
		pending += n
	}

	cons, err := stream.OrderedConsumer(ctx, jetstream.OrderedConsumerConfig{
		FilterSubjects: []string{contract.ItemOpsSubjects},
	})
	if err != nil {
		_ = idx.close()
		return nil, fmt.Errorf("ordered consumer: %w", err)
	}

	// pending counts down the backlog measured above; the fold below is the
	// only goroutine touching it, and ready closes exactly once.
	ready := make(chan struct{})
	if pending == 0 {
		close(ready)
	}

	items := map[string]*contract.Item{}
	cc, err := cons.Consume(func(msg jetstream.Msg) {
		md, err := msg.Metadata()
		if err != nil {
			log.Printf("hits-search: op metadata: %v", err)
			return
		}
		var op contract.Op
		if err := json.Unmarshal(msg.Data(), &op); err != nil {
			log.Printf("hits-search: decode op at seq %d: %v", md.Sequence.Stream, err)
			return
		}
		next, err := contract.Apply(items[op.Entity], op, md.Sequence.Stream)
		if err != nil {
			log.Printf("hits-search: fold %s op on item %s: %v", op.Op, op.Entity, err)
			return
		}
		items[op.Entity] = next
		if next.Tombstoned {
			if err := idx.remove(next.ID); err != nil {
				log.Printf("hits-search: remove item %s: %v", next.ID, err)
			}
		} else if err := idx.upsert(next); err != nil {
			log.Printf("hits-search: index item %s: %v", next.ID, err)
		}
		if pending > 0 {
			pending--
			if pending == 0 {
				close(ready)
			}
		}
	})
	if err != nil {
		_ = idx.close()
		return nil, fmt.Errorf("consume ops: %w", err)
	}

	select {
	case <-ready:
	case <-ctx.Done():
		cc.Stop()
		_ = idx.close()
		return nil, fmt.Errorf("catching up with the ops-log: %w", ctx.Err())
	}

	svc, err := micro.AddService(nc, micro.Config{
		Name:        serviceName,
		Version:     version.Version,
		Description: serviceDesc,
	})
	if err != nil {
		cc.Stop()
		_ = idx.close()
		return nil, fmt.Errorf("register service: %w", err)
	}
	if err := svc.AddEndpoint(queryEndpointName, micro.HandlerFunc(queryHandler(idx)),
		micro.WithEndpointSubject(client.SearchSubject)); err != nil {
		_ = svc.Stop()
		cc.Stop()
		_ = idx.close()
		return nil, fmt.Errorf("add query endpoint: %w", err)
	}
	// Flush so the endpoint subscriptions have reached the server: once
	// Start returns, a request from any connection must find a responder.
	if err := nc.FlushTimeout(5 * time.Second); err != nil {
		_ = svc.Stop()
		cc.Stop()
		_ = idx.close()
		return nil, fmt.Errorf("flush endpoint subscriptions: %w", err)
	}
	return &Service{micro: svc, cons: cc, idx: idx}, nil
}

func queryHandler(idx indexer) func(micro.Request) {
	return func(req micro.Request) {
		var r client.SearchRequest
		if err := json.Unmarshal(req.Data(), &r); err != nil {
			_ = req.Error("invalid-request", "malformed request: "+err.Error(), nil)
			return
		}
		reply, err := idx.query(r)
		if err != nil {
			_ = req.Error("internal", err.Error(), nil)
			return
		}
		data, err := json.Marshal(reply)
		if err != nil {
			_ = req.Error("internal", "encode reply: "+err.Error(), nil)
			return
		}
		_ = req.Respond(data)
	}
}

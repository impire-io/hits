package graph

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
	streamName  = "hits-ops"
	serviceName = "hits-graph"
	serviceDesc = "HITS graph index — a projection of the ops-log"
	maxDepth    = 5
)

// Service is one running hits-graph instance.
type Service struct {
	micro micro.Service
	cons  jetstream.ConsumeContext
	st    store
}

// Stop takes the service off the wire and stops the consumer.
func (s *Service) Stop() {
	if s.micro != nil {
		_ = s.micro.Stop()
	}
	s.cons.Stop()
	s.st.close()
}

// Start builds the graph by replaying the whole ops-log — the consumer is
// unfiltered because project registrations carry the project node names —
// and registers the hits-graph micro service only once caught up with the
// backlog measured at start.
func Start(ctx context.Context, nc *nats.Conn) (*Service, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("jetstream: %w", err)
	}
	stream, err := js.Stream(ctx, streamName)
	if err != nil {
		return nil, fmt.Errorf("ops-log stream %q not found (is hits-node running?): %w", streamName, err)
	}
	sinfo, err := stream.Info(ctx)
	if err != nil {
		return nil, fmt.Errorf("stream info: %w", err)
	}
	pending := sinfo.State.Msgs

	st := newMemStore()
	cons, err := stream.OrderedConsumer(ctx, jetstream.OrderedConsumerConfig{})
	if err != nil {
		return nil, fmt.Errorf("ordered consumer: %w", err)
	}

	ready := make(chan struct{})
	if pending == 0 {
		close(ready)
	}
	items := map[string]*contract.Item{}
	cc, err := cons.Consume(func(msg jetstream.Msg) {
		md, err := msg.Metadata()
		if err != nil {
			log.Printf("hits-graph: op metadata: %v", err)
			return
		}
		var op contract.Op
		if err := json.Unmarshal(msg.Data(), &op); err != nil {
			log.Printf("hits-graph: decode op at seq %d: %v", md.Sequence.Stream, err)
			return
		}
		fold(st, items, op, md.Sequence.Stream)
		if pending > 0 {
			pending--
			if pending == 0 {
				close(ready)
			}
		}
	})
	if err != nil {
		return nil, fmt.Errorf("consume ops: %w", err)
	}

	select {
	case <-ready:
	case <-ctx.Done():
		cc.Stop()
		return nil, fmt.Errorf("catching up with the ops-log: %w", ctx.Err())
	}

	svc, err := micro.AddService(nc, micro.Config{
		Name:        serviceName,
		Version:     version.Version,
		Description: serviceDesc,
	})
	if err != nil {
		cc.Stop()
		return nil, fmt.Errorf("register service: %w", err)
	}
	for name, handler := range map[string]struct {
		subject string
		fn      func(micro.Request)
	}{
		"neighbors": {client.GraphNeighborsSubject, neighborsHandler(st)},
		"walk":      {client.GraphWalkSubject, walkHandler(st)},
	} {
		if err := svc.AddEndpoint(name, micro.HandlerFunc(handler.fn),
			micro.WithEndpointSubject(handler.subject)); err != nil {
			_ = svc.Stop()
			cc.Stop()
			return nil, fmt.Errorf("add %s endpoint: %w", name, err)
		}
	}
	// Flush so the endpoint subscriptions have reached the server: once
	// Start returns, a request from any connection must find a responder.
	if err := nc.FlushTimeout(5 * time.Second); err != nil {
		_ = svc.Stop()
		cc.Stop()
		return nil, fmt.Errorf("flush endpoint subscriptions: %w", err)
	}
	return &Service{micro: svc, cons: cc, st: st}, nil
}

// fold applies one op and set-replaces the touched node's edges from its
// fresh snapshot — no incremental edge bookkeeping to drift.
func fold(st store, items map[string]*contract.Item, op contract.Op, seq uint64) {
	if op.Op == contract.OpRegistered {
		var p contract.RegisteredPayload
		if err := json.Unmarshal(op.Payload, &p); err != nil {
			log.Printf("hits-graph: decode registration for %s: %v", op.Entity, err)
			return
		}
		st.setName(nodeKey{kind: client.NodeProject, id: op.Entity}, p.Name)
		return
	}
	next, err := contract.Apply(items[op.Entity], op, seq)
	if err != nil {
		log.Printf("hits-graph: fold %s op on item %s: %v", op.Op, op.Entity, err)
		return
	}
	items[op.Entity] = next
	key := nodeKey{kind: client.NodeItem, id: next.ID}
	if next.Tombstoned {
		st.removeNode(key)
		return
	}
	st.setEdges(key, deriveEdges(next))
}

// deriveEdges is the graph contract: the asserted links plus the derived
// edges of 02-DESIGN/item-model.md § links.
func deriveEdges(it *contract.Item) []edge {
	edges := make([]edge, 0, len(it.Links)+len(it.LocatedIn)+3)
	for _, l := range it.Links {
		edges = append(edges, edge{typ: string(l.Type), to: nodeKey{kind: client.NodeItem, id: l.To}})
	}
	for _, slug := range it.LocatedIn {
		edges = append(edges, edge{typ: client.EdgeLocatedIn, to: nodeKey{kind: client.NodeProject, id: slug}})
	}
	edges = append(edges, edge{typ: client.EdgeReportedBy, to: nodeKey{kind: client.NodeActor, id: it.Reporter}})
	if it.Claim != nil {
		edges = append(edges, edge{typ: client.EdgeClaimedBy, to: nodeKey{kind: client.NodeActor, id: it.Claim.By}})
	}
	if it.Status == contract.Blocked && isItemID(it.BlockedBy) {
		edges = append(edges, edge{typ: client.EdgeBlockedBy, to: nodeKey{kind: client.NodeItem, id: it.BlockedBy}})
	}
	return edges
}

// isItemID reports whether s is exactly an item ID — all digits. Prose
// blockers derive no edge.
func isItemID(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func neighborsHandler(st store) func(micro.Request) {
	return func(req micro.Request) {
		var r client.NeighborsRequest
		if err := json.Unmarshal(req.Data(), &r); err != nil {
			_ = req.Error("invalid-request", "malformed request: "+err.Error(), nil)
			return
		}
		respond(req, client.NeighborsReply{
			Edges: st.neighbors(nodeKey{kind: r.Kind, id: r.ID}, r.Direction, r.Types),
		})
	}
}

func walkHandler(st store) func(micro.Request) {
	return func(req micro.Request) {
		var r client.WalkRequest
		if err := json.Unmarshal(req.Data(), &r); err != nil {
			_ = req.Error("invalid-request", "malformed request: "+err.Error(), nil)
			return
		}
		depth := r.Depth
		if depth <= 0 {
			depth = 2
		}
		if depth > maxDepth {
			depth = maxDepth
		}

		type edgeKey struct {
			from nodeKey
			typ  string
			to   nodeKey
		}
		start := nodeKey{kind: r.Kind, id: r.ID}
		seen := map[nodeKey]bool{start: true}
		seenEdge := map[edgeKey]bool{}
		reply := client.WalkReply{Nodes: []client.NodeRef{}, Edges: []client.GraphEdge{}}
		frontier := []nodeKey{start}
		for range depth {
			var next []nodeKey
			for _, k := range frontier {
				for _, e := range st.neighbors(k, r.Direction, r.Types) {
					ek := edgeKey{
						from: nodeKey{kind: e.From.Kind, id: e.From.ID},
						typ:  e.Type,
						to:   nodeKey{kind: e.To.Kind, id: e.To.ID},
					}
					if seenEdge[ek] {
						continue
					}
					seenEdge[ek] = true
					reply.Edges = append(reply.Edges, e)
					for _, ref := range [2]client.NodeRef{e.From, e.To} {
						nk := nodeKey{kind: ref.Kind, id: ref.ID}
						if !seen[nk] {
							seen[nk] = true
							reply.Nodes = append(reply.Nodes, ref)
							next = append(next, nk)
						}
					}
				}
			}
			frontier = next
		}
		respond(req, reply)
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

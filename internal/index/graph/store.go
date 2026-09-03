// Package graph runs the hits-graph micro service: link exploration over an
// adjacency projection of the ops-log that is wider than the asserted links
// — project and actor nodes are materialized, with edges derived from
// properties and ops. Every result resolves to node refs; item state comes
// from the hits service, never from here.
package graph

import (
	"sync"

	"github.com/impire-io/hits/client"
)

type nodeKey struct {
	kind client.NodeKind
	id   string
}

type edge struct {
	typ string
	to  nodeKey
}

type inEdge struct {
	typ  string
	from nodeKey
}

// store is the adjacency boundary: in-memory by default, external later if
// scale ever demands it. The whole edge set of a node is replaced at once —
// the graph is a function of the item snapshots, so replay and live tail
// share one code path and cannot diverge.
type store interface {
	setEdges(from nodeKey, edges []edge)
	setName(k nodeKey, name string)
	removeNode(k nodeKey)
	neighbors(k nodeKey, dir string, types []string) []client.GraphEdge
	close()
}

// memStore is the in-memory adjacency: an out-map, a reverse in-map, the
// project display names, and the dead set. A tombstoned item is marked dead
// rather than scrubbed from other nodes' edge sets — those sets are
// recomputed from snapshots that still assert the edges, so filtering at
// query time is the only shape that stays consistent under refolds.
type memStore struct {
	mu    sync.RWMutex
	out   map[nodeKey]map[edge]struct{}
	in    map[nodeKey]map[inEdge]struct{}
	names map[nodeKey]string
	dead  map[nodeKey]struct{}
}

func newMemStore() *memStore {
	return &memStore{
		out:   map[nodeKey]map[edge]struct{}{},
		in:    map[nodeKey]map[inEdge]struct{}{},
		names: map[nodeKey]string{},
		dead:  map[nodeKey]struct{}{},
	}
}

func (s *memStore) setEdges(from nodeKey, edges []edge) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for e := range s.out[from] {
		delete(s.in[e.to], inEdge{typ: e.typ, from: from})
	}
	set := make(map[edge]struct{}, len(edges))
	for _, e := range edges {
		set[e] = struct{}{}
		if s.in[e.to] == nil {
			s.in[e.to] = map[inEdge]struct{}{}
		}
		s.in[e.to][inEdge{typ: e.typ, from: from}] = struct{}{}
	}
	s.out[from] = set
}

func (s *memStore) setName(k nodeKey, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.names[k] = name
}

func (s *memStore) removeNode(k nodeKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dead[k] = struct{}{}
}

func (s *memStore) neighbors(k nodeKey, dir string, types []string) []client.GraphEdge {
	s.mu.RLock()
	defer s.mu.RUnlock()

	edges := []client.GraphEdge{}
	if _, gone := s.dead[k]; gone {
		return edges
	}
	wanted := map[string]bool{}
	for _, t := range types {
		wanted[t] = true
	}
	match := func(typ string) bool { return len(wanted) == 0 || wanted[typ] }

	if dir == "" || dir == "out" || dir == "both" {
		for e := range s.out[k] {
			if _, gone := s.dead[e.to]; gone || !match(e.typ) {
				continue
			}
			edges = append(edges, client.GraphEdge{From: s.ref(k), Type: e.typ, To: s.ref(e.to)})
		}
	}
	if dir == "" || dir == "in" || dir == "both" {
		for e := range s.in[k] {
			if _, gone := s.dead[e.from]; gone || !match(e.typ) {
				continue
			}
			edges = append(edges, client.GraphEdge{From: s.ref(e.from), Type: e.typ, To: s.ref(k)})
		}
	}
	return edges
}

// ref builds the wire ref for a node; callers hold at least the read lock.
func (s *memStore) ref(k nodeKey) client.NodeRef {
	return client.NodeRef{Kind: k.kind, ID: k.id, Name: s.names[k]}
}

func (s *memStore) close() {}

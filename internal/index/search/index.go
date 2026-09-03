// Package search runs the hits-search micro service: a full-text projection
// of the ops-log over item reports and notes. It consumes the log through
// its own ordered consumer, folds item state with the shared contract fold,
// and answers hits.search.query. Every hit resolves to an item ID; state
// comes from the hits service — this index is never authority.
package search

import (
	"fmt"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/contract"
)

// indexer is the store boundary: an embedded index by default, an external
// one later if scale ever demands it, without touching consumer or endpoint.
type indexer interface {
	upsert(it *contract.Item) error
	remove(id string) error
	query(r client.SearchRequest) (client.SearchReply, error)
	close() error
}

// bleveIndex is the embedded, in-memory, pure-Go implementation, rebuilt
// from the ops-log on every boot.
type bleveIndex struct {
	idx bleve.Index
}

func newBleveIndex() (*bleveIndex, error) {
	text := bleve.NewTextFieldMapping()
	kw := bleve.NewKeywordFieldMapping()

	doc := bleve.NewDocumentMapping()
	doc.AddFieldMappingsAt("report", text)
	doc.AddFieldMappingsAt("notes", text)
	for _, f := range []string{"type", "status", "priority", "located-in"} {
		doc.AddFieldMappingsAt(f, kw)
	}
	mapping := bleve.NewIndexMapping()
	mapping.DefaultMapping = doc

	idx, err := bleve.NewMemOnly(mapping)
	if err != nil {
		return nil, fmt.Errorf("open bleve index: %w", err)
	}
	return &bleveIndex{idx: idx}, nil
}

// docFor flattens a snapshot into the indexed document. A map keeps the
// field names explicit — they are part of the query contract below.
func docFor(it *contract.Item) map[string]any {
	notes := make([]string, len(it.Notes))
	for i, n := range it.Notes {
		notes[i] = n.Text
	}
	return map[string]any{
		"report":     it.Report,
		"notes":      notes,
		"type":       string(it.Type),
		"status":     string(it.Status),
		"priority":   string(it.Priority),
		"located-in": it.LocatedIn,
	}
}

func (b *bleveIndex) upsert(it *contract.Item) error {
	return b.idx.Index(it.ID, docFor(it))
}

func (b *bleveIndex) remove(id string) error {
	return b.idx.Delete(id)
}

func (b *bleveIndex) query(r client.SearchRequest) (client.SearchReply, error) {
	var must []query.Query
	if r.Query != "" {
		report := bleve.NewMatchQuery(r.Query)
		report.SetField("report")
		notes := bleve.NewMatchQuery(r.Query)
		notes.SetField("notes")
		must = append(must, bleve.NewDisjunctionQuery(report, notes))
	}
	if r.Type != "" {
		tq := bleve.NewTermQuery(string(r.Type))
		tq.SetField("type")
		must = append(must, tq)
	}
	if r.Status != "" {
		sq := bleve.NewTermQuery(string(r.Status))
		sq.SetField("status")
		must = append(must, sq)
	}

	var q query.Query
	switch len(must) {
	case 0:
		q = bleve.NewMatchAllQuery()
	case 1:
		q = must[0]
	default:
		q = bleve.NewConjunctionQuery(must...)
	}

	limit := r.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}
	res, err := b.idx.Search(bleve.NewSearchRequestOptions(q, limit, r.Offset, false))
	if err != nil {
		return client.SearchReply{}, fmt.Errorf("search: %w", err)
	}
	reply := client.SearchReply{Hits: []client.SearchHit{}, Total: res.Total}
	for _, hit := range res.Hits {
		reply.Hits = append(reply.Hits, client.SearchHit{ID: hit.ID, Score: hit.Score})
	}
	return reply, nil
}

func (b *bleveIndex) close() error {
	return b.idx.Close()
}

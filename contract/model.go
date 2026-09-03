// Package contract declares the shared contract of the HITS platform: the op
// envelope and catalog written to the ops-log, the item and project models
// folded from it, and the invariants the write path enforces. Every service
// imports this package; it imports no service.
//
// The design authority is hits-hq/02-DESIGN (item-model.md, ops-log.md,
// services.md) at commit 826c108.
package contract

import "time"

// Type classifies an item.
type Type string

// The item types.
const (
	Bug         Type = "bug"         // a defect, usually with unknown owner
	Task        Type = "task"        // a known follow-up; opens located, skips diagnosis
	Improvement Type = "improvement" // a deliberate betterment, not a defect
)

// Status is an item's lifecycle position.
type Status string

// The item statuses. Resolved and Wontfix are terminal.
const (
	Open       Status = "open"
	Diagnosing Status = "diagnosing"
	Located    Status = "located"
	Blocked    Status = "blocked"
	Resolved   Status = "resolved"
	Wontfix    Status = "wontfix"
)

// Terminal reports whether s accepts no further transitions.
func (s Status) Terminal() bool { return s == Resolved || s == Wontfix }

// Priority is the triage signal on an item. It carries no scheduling meaning.
type Priority string

// The priorities; an empty priority defaults to Normal at creation.
const (
	High   Priority = "high"
	Normal Priority = "normal"
	Low    Priority = "low"
)

// LinkType names an asserted item↔item relation. Relations to projects and
// actors are properties, not links; the graph index derives edges for those.
type LinkType string

// The assertable link types. "blocks" is deliberately absent: a block on
// another item is derived from the blocked op for the duration of the block.
const (
	Duplicates LinkType = "duplicates"
	RelatesTo  LinkType = "relates-to"
)

// Note is one appended trail entry: the report's follow-ups, a bug's
// diagnosis trail, closing reasoning. Notes are append-only.
type Note struct {
	Author string    `json:"author"`
	At     time.Time `json:"at"`
	Text   string    `json:"text"`
}

// Link is an asserted, typed, directed edge to another item.
type Link struct {
	Type LinkType `json:"type"`
	To   string   `json:"to"`
}

// Claim records intent to work an item — not started work. The claimant is
// the claiming op's actor.
type Claim struct {
	By         string    `json:"by"`
	At         time.Time `json:"at"`
	StolenFrom string    `json:"stolen-from,omitempty"`
}

// FixRef is one verifiable reference recorded on close: a PR, a commit, or
// an operational action (e.g. deploy) whose note carries the evidence.
type FixRef struct {
	PR     string `json:"pr,omitempty"`
	Commit string `json:"commit,omitempty"`
	Action string `json:"action,omitempty"`
	Note   string `json:"note,omitempty"`
}

// Land is one entry of a cross-repo landing order.
type Land struct {
	Repo   string   `json:"repo"`
	PR     string   `json:"pr"`
	After  []string `json:"after"`
	Closes bool     `json:"closes,omitempty"`
}

// Item is the current-state snapshot of one item: the fold of its ops. Seq is
// the ops-log stream sequence of the last op folded in, which is what makes
// re-folding idempotent.
type Item struct {
	ID              string    `json:"id"`
	Type            Type      `json:"type"`
	Status          Status    `json:"status"`
	Priority        Priority  `json:"priority"`
	Report          string    `json:"report"`
	Reporter        string    `json:"reporter"`
	Created         time.Time `json:"created"`
	Claim           *Claim    `json:"claim,omitempty"`
	BlockedBy       string    `json:"blocked-by,omitempty"`
	Interrupted     Status    `json:"interrupted,omitempty"`
	LocatedIn       []string  `json:"located-in,omitempty"`
	DiscoveredWhile string    `json:"discovered-while,omitempty"`
	Lands           []Land    `json:"lands,omitempty"`
	FixedBy         []FixRef  `json:"fixed-by,omitempty"`
	AmendedDesign   []string  `json:"amended-design,omitempty"`
	Closed          string    `json:"closed,omitempty"`
	Notes           []Note    `json:"notes,omitempty"`
	Links           []Link    `json:"links,omitempty"`
	Tombstoned      bool      `json:"tombstoned,omitempty"`
	TombstoneReason string    `json:"tombstone-reason,omitempty"`
	Seq             uint64    `json:"seq"`
}

// clone returns a copy of the item safe to mutate: slices are copied, the
// claim is copied by value.
func (it *Item) clone() *Item {
	c := *it
	c.LocatedIn = append([]string(nil), it.LocatedIn...)
	c.Lands = append([]Land(nil), it.Lands...)
	c.FixedBy = append([]FixRef(nil), it.FixedBy...)
	c.AmendedDesign = append([]string(nil), it.AmendedDesign...)
	c.Notes = append([]Note(nil), it.Notes...)
	c.Links = append([]Link(nil), it.Links...)
	if it.Claim != nil {
		cl := *it.Claim
		c.Claim = &cl
	}
	return &c
}

// Project is one entry of the located-in vocabulary: registered, thin, no
// lifecycle. In the hits install a project is a repo.
type Project struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Seq         uint64 `json:"seq"`
}

package contract

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
)

// The store (hits-hq 02-DESIGN/the-store.md): where each entity lives on
// chronicle, the state it keeps there, and the difference a write sends.
// Pure: every function here is a mapping between the model and the shape
// chronicle holds, and no I/O happens in this package.

// StoreName is the one store a HITS keeps in its account.
const StoreName = "hits"

// The four types, and the child name a release lives under.
const (
	TypeItem       = "item"
	TypeProject    = "project"
	TypeInitiative = "initiative"
	TypeRelease    = "release"
	ReleasesChild  = "releases"
)

// ItemPath is an item's path: item/<id>.
func ItemPath(id string) string { return TypeItem + "/" + id }

// ProjectPath is a project's path: project/<slug>.
func ProjectPath(slug string) string { return TypeProject + "/" + slug }

// InitiativePath is an initiative's path: initiative/<slug>.
func InitiativePath(slug string) string { return TypeInitiative + "/" + slug }

// MemberPath is how a person or an agent is referenced: member/<handle>.
// A member is not an instance; the graph treats the path as an opaque
// target, which is enough to walk from.
func MemberPath(handle string) string { return "member/" + handle }

// ReleaseID is a release slug as an instance id: every dot an underscore,
// reversibly, since neither slug grammar admits an underscore.
func ReleaseID(slug string) string { return strings.ReplaceAll(slug, ".", "_") }

// ReleaseSlug is the inverse of ReleaseID.
func ReleaseSlug(id string) string { return strings.ReplaceAll(id, "_", ".") }

// ReleasePath is a release's path: a child of its initiative.
func ReleasePath(initiative, slug string) string {
	return InitiativePath(initiative) + "/" + ReleasesChild + "/" + ReleaseID(slug)
}

// ParsePath reads a path back into its kind and the id the model uses:
// an item's id, a project's or initiative's slug, a release's
// "<initiative>/<slug>", a member's handle. Anything else is ("", path).
func ParsePath(path string) (kind, id string) {
	segs := strings.Split(path, "/")
	switch {
	case len(segs) == 2 && segs[0] == TypeItem:
		return TypeItem, segs[1]
	case len(segs) == 2 && segs[0] == TypeProject:
		return TypeProject, segs[1]
	case len(segs) == 2 && segs[0] == TypeInitiative:
		return TypeInitiative, segs[1]
	case len(segs) == 2 && segs[0] == "member":
		return "member", segs[1]
	case len(segs) == 4 && segs[0] == TypeInitiative && segs[2] == ReleasesChild:
		return TypeRelease, segs[1] + "/" + ReleaseSlug(segs[3])
	}
	return "", path
}

var itemRefRe = regexp.MustCompile(`^([a-z][a-z0-9-]*-)?[0-9]+$`)

// RefPath is how a blocked-by value is kept: an item's ref as its path,
// prose as it is.
func RefPath(ref string) string {
	if itemRefRe.MatchString(ref) {
		return ItemPath(ref)
	}
	return ref
}

// PathRef is the inverse of RefPath.
func PathRef(s string) string {
	if kind, id := ParsePath(s); kind == TypeItem {
		return id
	}
	return s
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseStamp(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func stripPrefix(s, prefix string) string { return strings.TrimPrefix(s, prefix) }

// Normalize round-trips a value through JSON so that two states compare
// as chronicle hands them back: maps, slices and float64.
func Normalize(v any) map[string]any {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

// Diff is what a write sends as a merge: every field that changed as its
// new value, every field that was cleared as null (which a merge deletes).
func Diff(before, after map[string]any) map[string]any {
	d := map[string]any{}
	for k, v := range after {
		if !reflect.DeepEqual(before[k], v) {
			d[k] = v
		}
	}
	for k := range before {
		if _, ok := after[k]; !ok {
			d[k] = nil
		}
	}
	return d
}

// --- items -------------------------------------------------------------------

// ItemState is an item's state on chronicle: the snapshot's fields with
// every reference as a path, the claim flattened, the links as a map, and
// the notes left out — they are the history. Priority defaults to normal.
func ItemState(it *Item) map[string]any {
	priority := it.Priority
	if priority == "" {
		priority = Normal
	}
	m := map[string]any{
		"type": it.Type, "status": it.Status, "priority": priority,
		"report": it.Report, "reporter": MemberPath(it.Reporter), "created": stamp(it.Created),
	}
	if it.Initiative != "" {
		m["initiative"] = InitiativePath(it.Initiative)
	}
	if it.Target != "" {
		m["target"] = ReleasePath(it.Initiative, it.Target)
	}
	if it.Claim != nil {
		m["claimed-by"] = MemberPath(it.Claim.By)
		m["claimed"] = stamp(it.Claim.At)
		if it.Claim.StolenFrom != "" {
			m["stolen-from"] = MemberPath(it.Claim.StolenFrom)
		}
	}
	if it.BlockedBy != "" {
		m["blocked-by"] = RefPath(it.BlockedBy)
	}
	if it.Interrupted != "" {
		m["interrupted"] = it.Interrupted
	}
	if len(it.LocatedIn) > 0 {
		paths := make([]string, 0, len(it.LocatedIn))
		for _, p := range it.LocatedIn {
			paths = append(paths, ProjectPath(p))
		}
		m["located-in"] = paths
	}
	if it.DiscoveredWhile != "" {
		m["discovered-while"] = it.DiscoveredWhile
	}
	if len(it.Lands) > 0 {
		m["lands"] = it.Lands
	}
	if len(it.FixedBy) > 0 {
		m["fixed-by"] = it.FixedBy
	}
	if len(it.AmendedDesign) > 0 {
		m["amended-design"] = it.AmendedDesign
	}
	if it.Closed != "" {
		m["closed"] = it.Closed
	}
	if len(it.Links) > 0 {
		links := map[string][]string{}
		for _, l := range it.Links {
			links[string(l.Type)] = append(links[string(l.Type)], ItemPath(l.To))
		}
		m["links"] = links
	}
	if it.Tombstoned {
		m["tombstoned"] = true
		if it.TombstoneReason != "" {
			m["tombstone-reason"] = it.TombstoneReason
		}
	}
	return Normalize(m)
}

// itemState is the shape ItemState writes, read back.
type itemState struct {
	Type            Type                `json:"type"`
	Status          Status              `json:"status"`
	Priority        Priority            `json:"priority"`
	Initiative      string              `json:"initiative"`
	Target          string              `json:"target"`
	Report          string              `json:"report"`
	Reporter        string              `json:"reporter"`
	Created         string              `json:"created"`
	ClaimedBy       string              `json:"claimed-by"`
	Claimed         string              `json:"claimed"`
	StolenFrom      string              `json:"stolen-from"`
	BlockedBy       string              `json:"blocked-by"`
	Interrupted     Status              `json:"interrupted"`
	LocatedIn       []string            `json:"located-in"`
	DiscoveredWhile string              `json:"discovered-while"`
	Lands           []Land              `json:"lands"`
	FixedBy         []FixRef            `json:"fixed-by"`
	AmendedDesign   []string            `json:"amended-design"`
	Closed          string              `json:"closed"`
	Links           map[string][]string `json:"links"`
	Tombstoned      bool                `json:"tombstoned"`
	TombstoneReason string              `json:"tombstone-reason"`
}

// ItemFromState reads an item's chronicle state back into the model. The
// notes are not in the state: the caller reads them from the history.
func ItemFromState(id string, state []byte) (*Item, error) {
	var s itemState
	if err := json.Unmarshal(state, &s); err != nil {
		return nil, fmt.Errorf("item %s: decode state: %w", id, err)
	}
	it := &Item{
		ID: id, Type: s.Type, Status: s.Status, Priority: s.Priority,
		Initiative:      stripPrefix(s.Initiative, TypeInitiative+"/"),
		Report:          s.Report,
		Reporter:        stripPrefix(s.Reporter, "member/"),
		Created:         parseStamp(s.Created),
		BlockedBy:       PathRef(s.BlockedBy),
		Interrupted:     s.Interrupted,
		DiscoveredWhile: s.DiscoveredWhile,
		Lands:           s.Lands,
		FixedBy:         s.FixedBy,
		AmendedDesign:   s.AmendedDesign,
		Closed:          s.Closed,
		Tombstoned:      s.Tombstoned,
		TombstoneReason: s.TombstoneReason,
	}
	if s.Target != "" {
		if kind, rel := ParsePath(s.Target); kind == TypeRelease {
			if _, slug, ok := strings.Cut(rel, "/"); ok {
				it.Target = slug
			}
		}
	}
	if s.ClaimedBy != "" {
		it.Claim = &Claim{By: stripPrefix(s.ClaimedBy, "member/"), At: parseStamp(s.Claimed), StolenFrom: stripPrefix(s.StolenFrom, "member/")}
	}
	for _, p := range s.LocatedIn {
		it.LocatedIn = append(it.LocatedIn, stripPrefix(p, TypeProject+"/"))
	}
	types := make([]string, 0, len(s.Links))
	for t := range s.Links {
		types = append(types, t)
	}
	sort.Strings(types)
	for _, t := range types {
		for _, to := range s.Links[t] {
			it.Links = append(it.Links, Link{Type: LinkType(t), To: PathRef(to)})
		}
	}
	return it, nil
}

// ItemOperation names the operation on the item type an op of the catalog
// becomes: its namesake, except a claim with a steal, which is `steal`.
func ItemOperation(op Op) (string, error) {
	switch op.Op {
	case OpCreated:
		return "create", nil
	case OpNoted:
		return "note", nil
	case OpEdited:
		// The legacy backfill — the initiative and nothing else — is the
		// store's assign, which a closed item still admits (the-store.md).
		var p EditedPayload
		if err := decode(op, &p); err != nil {
			return "", err
		}
		if initiativeOnlyEdit(p) {
			return "assign", nil
		}
		return "edit", nil
	case OpTransitioned:
		return "transition", nil
	case OpClaimed:
		var p ClaimedPayload
		if err := decode(op, &p); err != nil {
			return "", err
		}
		if p.Steal {
			return "steal", nil
		}
		return "claim", nil
	case OpReleased:
		return "release", nil
	case OpBlocked:
		return "block", nil
	case OpUnblocked:
		return "unblock", nil
	case OpLinked:
		return "link", nil
	case OpUnlinked:
		return "unlink", nil
	case OpTombstoned:
		return "tombstone", nil
	}
	return "", fmt.Errorf("op %q is not an item op", op.Op)
}

// VocabOperation names the operation a vocabulary op becomes.
func VocabOperation(op Op) (string, error) {
	switch op.Op {
	case OpRegistered:
		return "create", nil
	case OpAssigned:
		return "assign", nil
	case OpRetired:
		return "retire", nil
	case OpShipped:
		return "ship", nil
	}
	return "", fmt.Errorf("op %q is not a vocabulary op", op.Op)
}

// --- the vocabularies --------------------------------------------------------

// ProjectState is a project's state on chronicle.
func ProjectState(p *Project) map[string]any {
	m := map[string]any{"slug": p.Slug, "name": p.Name}
	if p.Description != "" {
		m["description"] = p.Description
	}
	if p.Initiative != "" {
		m["initiative"] = InitiativePath(p.Initiative)
	}
	if p.Retired {
		m["retired"] = true
		if p.RetireReason != "" {
			m["retire-reason"] = p.RetireReason
		}
	}
	return Normalize(m)
}

// ProjectFromState reads a project's state back.
func ProjectFromState(slug string, state []byte) (*Project, error) {
	var s struct {
		Slug, Name, Description, Initiative, RetireReason string
		Retired                                           bool
	}
	var raw map[string]any
	if err := json.Unmarshal(state, &raw); err != nil {
		return nil, fmt.Errorf("project %s: decode state: %w", slug, err)
	}
	s.Slug, _ = raw["slug"].(string)
	s.Name, _ = raw["name"].(string)
	s.Description, _ = raw["description"].(string)
	s.Initiative, _ = raw["initiative"].(string)
	s.Retired, _ = raw["retired"].(bool)
	s.RetireReason, _ = raw["retire-reason"].(string)
	if s.Slug == "" {
		s.Slug = slug
	}
	return &Project{Slug: s.Slug, Name: s.Name, Description: s.Description, Initiative: stripPrefix(s.Initiative, TypeInitiative+"/"), Retired: s.Retired, RetireReason: s.RetireReason}, nil
}

// InitiativeState is an initiative's state on chronicle, without its
// counter: `next` is written by the mint and the registration, read with
// InitiativeFromState.
func InitiativeState(i *Initiative) map[string]any {
	m := map[string]any{"slug": i.Slug, "name": i.Name}
	if i.Description != "" {
		m["description"] = i.Description
	}
	if i.Retired {
		m["retired"] = true
		if i.RetireReason != "" {
			m["retire-reason"] = i.RetireReason
		}
	}
	return Normalize(m)
}

// InitiativeFromState reads an initiative's state back, with its counter:
// the number the next item takes, 1 when the state carries none.
func InitiativeFromState(slug string, state []byte) (*Initiative, int, error) {
	var raw map[string]any
	if err := json.Unmarshal(state, &raw); err != nil {
		return nil, 0, fmt.Errorf("initiative %s: decode state: %w", slug, err)
	}
	i := &Initiative{Slug: slug}
	if v, ok := raw["slug"].(string); ok && v != "" {
		i.Slug = v
	}
	i.Name, _ = raw["name"].(string)
	i.Description, _ = raw["description"].(string)
	i.Retired, _ = raw["retired"].(bool)
	i.RetireReason, _ = raw["retire-reason"].(string)
	next := 1
	if v, ok := raw["next"].(float64); ok && v >= 1 {
		next = int(v)
	}
	return i, next, nil
}

// ReleaseState is a release's state on chronicle; its initiative is its
// parent's path, not a field.
func ReleaseState(r *Release) map[string]any {
	m := map[string]any{"slug": r.Slug, "name": r.Name}
	if r.Description != "" {
		m["description"] = r.Description
	}
	if r.Shipped {
		m["shipped"] = true
		if len(r.ShipRefs) > 0 {
			m["ship-refs"] = r.ShipRefs
		}
		if r.ShipNote != "" {
			m["ship-note"] = r.ShipNote
		}
	}
	if r.Retired {
		m["retired"] = true
		if r.RetireReason != "" {
			m["retire-reason"] = r.RetireReason
		}
	}
	return Normalize(m)
}

// ReleaseFromState reads a release's state back under its initiative.
func ReleaseFromState(initiative string, state []byte) (*Release, error) {
	var s struct {
		Slug         string    `json:"slug"`
		Name         string    `json:"name"`
		Description  string    `json:"description"`
		Shipped      bool      `json:"shipped"`
		ShipRefs     []ShipRef `json:"ship-refs"`
		ShipNote     string    `json:"ship-note"`
		Retired      bool      `json:"retired"`
		RetireReason string    `json:"retire-reason"`
	}
	if err := json.Unmarshal(state, &s); err != nil {
		return nil, fmt.Errorf("release of %s: decode state: %w", initiative, err)
	}
	return &Release{Initiative: initiative, Slug: s.Slug, Name: s.Name, Description: s.Description, Shipped: s.Shipped, ShipRefs: s.ShipRefs, ShipNote: s.ShipNote, Retired: s.Retired, RetireReason: s.RetireReason}, nil
}

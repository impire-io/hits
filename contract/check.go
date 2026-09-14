package contract

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// InvariantError is a rejected command. Name is the machine-legible
// invariant name and travels to the caller verbatim; Message is for humans.
type InvariantError struct {
	Name    string
	Message string
}

func (e *InvariantError) Error() string { return e.Name + ": " + e.Message }

func inv(name, format string, args ...any) *InvariantError {
	return &InvariantError{Name: name, Message: fmt.Sprintf(format, args...)}
}

var (
	actorRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
	slugRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
)

// Byte budgets on free-text payload fields (decision 0014): bodies — report
// and note text — carry prose; every other free-text field is a label. Over
// budget is refused whole, never trimmed, so the log holds exactly what was
// written or nothing. The numbers are starting points, raised by decision.
const (
	MaxBodyBytes  = 8 * 1024
	MaxLabelBytes = 1024
)

func overBudget(field, s string, budget int) error {
	if len(s) > budget {
		return inv("over-budget", "%s is %d bytes; the budget is %d", field, len(s), budget)
	}
	return nil
}

// ValidActor reports whether s is a well-formed actor handle.
func ValidActor(s string) bool { return actorRe.MatchString(s) }

// ValidSlug reports whether s is a well-formed project slug (also the shape
// of an item subject token).
func ValidSlug(s string) bool { return slugRe.MatchString(s) }

// ValidInitiativeSlug reports whether s may name an initiative: a
// well-formed slug whose trailing hyphen-segment is not all digits — the
// rule that keeps <initiative>-<n> item IDs unambiguous (decision 0016).
// An all-digit slug is one trailing digit segment, hence refused too.
func ValidInitiativeSlug(s string) bool {
	if !ValidSlug(s) {
		return false
	}
	tail := s
	if i := strings.LastIndexByte(s, '-'); i >= 0 {
		tail = s[i+1:]
	}
	// An empty tail is a trailing hyphen; an all-digit tail is the parse
	// ambiguity itself. Both are refused.
	return tail != "" && !allDigits(tail)
}

// ParseItemID splits an item ID into its initiative prefix and reports
// whether the ID is well-formed: bare digits are a legacy ID (empty
// initiative), and <initiative>-<n> carries its initiative. The item
// number is the trailing all-digit segment (decision 0016).
func ParseItemID(id string) (initiative string, ok bool) {
	if id == "" {
		return "", false
	}
	if allDigits(id) {
		return "", true
	}
	i := strings.LastIndexByte(id, '-')
	if i <= 0 || !allDigits(id[i+1:]) || id[i+1:] == "" {
		return "", false
	}
	if !ValidInitiativeSlug(id[:i]) {
		return "", false
	}
	return id[:i], true
}

func allDigits(s string) bool {
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

// ValidReleaseSlug reports whether s may name a release: dot-separated
// segments, each a well-formed slug — dots are wanted, releases are
// usually versions ("0.5") — within one label's length. Uniqueness is per
// initiative, and nothing prefixes item IDs with it, so neither the
// global-uniqueness nor the trailing-digit machinery of the other
// vocabularies applies (decision 0017).
func ValidReleaseSlug(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, seg := range strings.Split(s, ".") {
		if !ValidSlug(seg) {
			return false
		}
	}
	return true
}

// ParseReleaseEntity splits a release op's entity — <initiative>.<slug> —
// and reports whether it is well-formed. Initiative slugs are dot-free,
// so the initiative is everything up to the first dot and the release
// slug is the rest, dots literal (decision 0017).
func ParseReleaseEntity(entity string) (initiative, slug string, ok bool) {
	i := strings.IndexByte(entity, '.')
	if i <= 0 || i == len(entity)-1 {
		return "", "", false
	}
	initiative, slug = entity[:i], entity[i+1:]
	if !ValidInitiativeSlug(initiative) || !ValidReleaseSlug(slug) {
		return "", "", false
	}
	return initiative, slug, true
}

func validType(t Type) bool { return t == Bug || t == Task || t == Improvement }

func validPriority(p Priority) bool { return p == High || p == Normal || p == Low }

func validLinkType(t LinkType) bool { return t == Duplicates || t == RelatesTo }

// transitions is the lifecycle: which statuses a transitioned op may target
// from where. Blocked is entered by OpBlocked, never by transition; terminal
// statuses appear as sources of nothing.
var transitions = map[Status][]Status{
	Open:       {Diagnosing, Located, Resolved, Wontfix},
	Diagnosing: {Located, Resolved, Wontfix},
	Located:    {Resolved, Wontfix},
}

func allowedTarget(from, to Status) bool {
	for _, t := range transitions[from] {
		if t == to {
			return true
		}
	}
	return false
}

// CheckOp validates one item op against the current snapshot (nil for a
// creation). It returns nil when the op may append, or an *InvariantError
// naming the violated invariant. It checks everything checkable without I/O;
// the write path adds the registry check on located-in values.
func CheckOp(current *Item, op Op) error {
	if !ValidActor(op.Actor) {
		return inv("invalid-actor", "actor %q is not a well-formed handle", op.Actor)
	}

	if op.Op == OpCreated {
		if current != nil {
			return inv("item-exists", "item %s already exists", current.ID)
		}
		return checkCreated(op)
	}
	if current == nil {
		return inv("not-found", "item %s does not exist", op.Entity)
	}
	if current.Tombstoned {
		return inv("tombstoned", "item %s is tombstoned and accepts no further ops", current.ID)
	}

	switch op.Op {
	case OpNoted:
		var p NotedPayload
		if err := decode(op, &p); err != nil {
			return err
		}
		if p.Text == "" {
			return inv("empty-note", "a note needs text")
		}
		return overBudget("note text", p.Text, MaxBodyBytes)
	case OpEdited:
		return checkEdited(current, op)
	case OpTransitioned:
		return checkTransitioned(current, op)
	case OpClaimed:
		return checkClaimed(current, op)
	case OpReleased:
		if current.Claim == nil {
			return inv("not-claimed", "item %s has no claim to release", current.ID)
		}
		return nil
	case OpBlocked:
		return checkBlocked(current, op)
	case OpUnblocked:
		if current.Status != Blocked {
			return inv("not-blocked", "item %s is not blocked", current.ID)
		}
		return nil
	case OpLinked, OpUnlinked:
		return checkLink(current, op)
	case OpTombstoned:
		var p TombstonedPayload
		if err := decode(op, &p); err != nil {
			return err
		}
		if p.Reason == "" {
			return inv("empty-reason", "a tombstone needs its reason")
		}
		return overBudget("tombstone reason", p.Reason, MaxLabelBytes)
	default:
		return inv("invalid-op", "unknown item op %q", op.Op)
	}
}

func checkCreated(op Op) error {
	var p CreatedPayload
	if err := decode(op, &p); err != nil {
		return err
	}
	if !validType(p.Type) {
		return inv("invalid-type", "type %q is not bug, task or improvement", p.Type)
	}
	if p.Report == "" {
		return inv("empty-report", "an item opens with the symptom in plain terms")
	}
	if p.Priority != "" && !validPriority(p.Priority) {
		return inv("invalid-priority", "priority %q is not high, normal or low", p.Priority)
	}
	if p.Initiative == "" {
		return inv("initiative-required", "an item opens into an initiative — the mint needs it")
	}
	if !ValidInitiativeSlug(p.Initiative) {
		return inv("invalid-initiative", "%q is not a well-formed initiative slug", p.Initiative)
	}
	if p.Target != "" && !ValidReleaseSlug(p.Target) {
		return inv("invalid-release", "%q is not a well-formed release slug", p.Target)
	}
	if p.Type == Task && len(p.LocatedIn) == 0 {
		return inv("task-requires-location", "a task cannot be created without located-in")
	}
	for _, loc := range p.LocatedIn {
		if !ValidSlug(loc) {
			return inv("invalid-slug", "located-in entry %q is not a well-formed project slug", loc)
		}
	}
	if err := overBudget("report", p.Report, MaxBodyBytes); err != nil {
		return err
	}
	return overBudget("discovered-while", p.DiscoveredWhile, MaxLabelBytes)
}

func checkEdited(current *Item, op Op) error {
	var p EditedPayload
	if err := decode(op, &p); err != nil {
		return err
	}
	// A closed item is history, and history still gets its memory kept:
	// notes, links — and the one-time legacy initiative assignment
	// (decision 0016's backfill), which reopens nothing. Every other edit
	// stays refused on terminal items.
	if current.Status.Terminal() && !initiativeOnlyEdit(p) {
		return inv("terminal-status", "item %s is %s; closed items accept notes, links, and the legacy initiative assignment only", current.ID, current.Status)
	}
	if p.Priority != nil && !validPriority(*p.Priority) {
		return inv("invalid-priority", "priority %q is not high, normal or low", *p.Priority)
	}
	if p.Initiative != nil {
		if prefix, _ := ParseItemID(current.ID); prefix != "" {
			return inv("initiative-immutable", "item %s carries its initiative in its ID; only legacy bare-ID items are assignable", current.ID)
		}
		if !ValidInitiativeSlug(*p.Initiative) {
			return inv("invalid-initiative", "%q is not a well-formed initiative slug", *p.Initiative)
		}
	}
	if p.Target != nil && *p.Target != "" && !ValidReleaseSlug(*p.Target) {
		return inv("invalid-release", "%q is not a well-formed release slug", *p.Target)
	}
	if p.LocatedIn != nil {
		for _, loc := range *p.LocatedIn {
			if !ValidSlug(loc) {
				return inv("invalid-slug", "located-in entry %q is not a well-formed project slug", loc)
			}
		}
	}
	if p.DiscoveredWhile != nil {
		if err := overBudget("discovered-while", *p.DiscoveredWhile, MaxLabelBytes); err != nil {
			return err
		}
	}
	return checkEditedLands(p)
}

// initiativeOnlyEdit reports whether the edit carries the initiative and
// nothing else — the one edit a terminal item accepts. Target is not in
// the exception: terminal-is-terminal is what freezes an item's target
// at close (decision 0017).
func initiativeOnlyEdit(p EditedPayload) bool {
	return p.Initiative != nil && p.Priority == nil && p.Target == nil &&
		p.LocatedIn == nil && p.DiscoveredWhile == nil && p.Lands == nil
}

func checkEditedLands(p EditedPayload) error {
	if p.Lands != nil {
		for _, l := range *p.Lands {
			if err := overBudget("lands repo", l.Repo, MaxLabelBytes); err != nil {
				return err
			}
			if err := overBudget("lands pr", l.PR, MaxLabelBytes); err != nil {
				return err
			}
			for _, a := range l.After {
				if err := overBudget("lands after entry", a, MaxLabelBytes); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func checkTransitioned(current *Item, op Op) error {
	if current.Status.Terminal() {
		return inv("terminal-status", "item %s is %s; a defect found later is a new item", current.ID, current.Status)
	}
	if current.Status == Blocked {
		return inv("blocked", "item %s is blocked on %q; unblock it first", current.ID, current.BlockedBy)
	}
	var p TransitionedPayload
	if err := decode(op, &p); err != nil {
		return err
	}
	if !allowedTarget(current.Status, p.To) {
		return inv("illegal-transition", "%s → %s is not a legal move", current.Status, p.To)
	}
	if current.Type == Task && (p.To == Diagnosing || p.To == Located) {
		return inv("illegal-transition", "a task skips the localization stages")
	}
	if p.To == Located && len(p.LocatedIn) == 0 && len(current.LocatedIn) == 0 {
		return inv("located-requires-location", "status located requires located-in")
	}
	if p.To.Terminal() && p.Closed == "" {
		return inv("close-requires-date", "a closing transition carries the close date")
	}
	if !p.To.Terminal() && (len(p.FixedBy) > 0 || len(p.AmendedDesign) > 0) {
		return inv("refs-only-on-close", "fixed-by and amended-design are carried by the closing transition")
	}
	for _, loc := range p.LocatedIn {
		if !ValidSlug(loc) {
			return inv("invalid-slug", "located-in entry %q is not a well-formed project slug", loc)
		}
	}
	for _, ref := range p.FixedBy {
		for _, f := range []struct{ field, s string }{
			{"fixed-by pr", ref.PR}, {"fixed-by commit", ref.Commit},
			{"fixed-by action", ref.Action}, {"fixed-by note", ref.Note},
		} {
			if err := overBudget(f.field, f.s, MaxLabelBytes); err != nil {
				return err
			}
		}
	}
	for _, d := range p.AmendedDesign {
		if err := overBudget("amended-design entry", d, MaxLabelBytes); err != nil {
			return err
		}
	}
	return nil
}

func checkClaimed(current *Item, op Op) error {
	if current.Status.Terminal() {
		return inv("terminal-status", "item %s is %s; there is nothing left to claim", current.ID, current.Status)
	}
	var p ClaimedPayload
	if err := decode(op, &p); err != nil {
		return err
	}
	if current.Claim != nil && !p.Steal {
		return inv("already-claimed", "item %s is claimed by %s", current.ID, current.Claim.By)
	}
	return nil
}

func checkBlocked(current *Item, op Op) error {
	if current.Status.Terminal() {
		return inv("terminal-status", "item %s is %s", current.ID, current.Status)
	}
	if current.Status == Blocked {
		return inv("already-blocked", "item %s is already blocked on %q", current.ID, current.BlockedBy)
	}
	var p BlockedPayload
	if err := decode(op, &p); err != nil {
		return err
	}
	if p.Interrupted != current.Status {
		return inv("interrupted-mismatch", "blocked op records %q as interrupted, item is %q", p.Interrupted, current.Status)
	}
	return overBudget("blocked-by", p.BlockedBy, MaxLabelBytes)
}

func checkLink(current *Item, op Op) error {
	var p LinkedPayload
	if err := decode(op, &p); err != nil {
		return err
	}
	if !validLinkType(p.Type) {
		return inv("invalid-link-type", "link type %q is not assertable", p.Type)
	}
	if p.To == "" || p.To == current.ID {
		return inv("self-link", "a link needs a distinct target item")
	}
	has := false
	for _, l := range current.Links {
		if l.Type == p.Type && l.To == p.To {
			has = true
			break
		}
	}
	if op.Op == OpLinked && has {
		return inv("link-exists", "item %s already links %s %s", current.ID, p.Type, p.To)
	}
	if op.Op == OpUnlinked && !has {
		return inv("link-not-found", "item %s has no %s link to %s", current.ID, p.Type, p.To)
	}
	return nil
}

// CheckProjectOp validates a project op against the current registry entry
// (nil when the slug is unregistered).
func CheckProjectOp(current *Project, op Op) error {
	if !ValidActor(op.Actor) {
		return inv("invalid-actor", "actor %q is not a well-formed handle", op.Actor)
	}
	if !ValidSlug(op.Entity) {
		return inv("invalid-slug", "%q is not a well-formed project slug", op.Entity)
	}
	switch op.Op {
	case OpRegistered:
		if current != nil {
			if current.Retired {
				return inv("slug-retired", "project %s is retired; a retired slug is never reused", current.Slug)
			}
			return inv("already-registered", "project %s is already registered", current.Slug)
		}
		var p RegisteredPayload
		if err := decode(op, &p); err != nil {
			return err
		}
		if p.Name == "" {
			return inv("empty-name", "a project registers with a display name")
		}
		if p.Initiative == "" {
			return inv("initiative-required", "a project registers into an initiative (decision 0016)")
		}
		if !ValidInitiativeSlug(p.Initiative) {
			return inv("invalid-initiative", "%q is not a well-formed initiative slug", p.Initiative)
		}
		if err := overBudget("project name", p.Name, MaxLabelBytes); err != nil {
			return err
		}
		return overBudget("project description", p.Description, MaxLabelBytes)
	case OpAssigned:
		if current == nil {
			return inv("unregistered-project", "project %s is not registered", op.Entity)
		}
		if current.Retired {
			return inv("retired-project", "project %s is retired", current.Slug)
		}
		var p AssignedPayload
		if err := decode(op, &p); err != nil {
			return err
		}
		if p.Initiative == "" {
			return inv("initiative-required", "an assignment names the initiative")
		}
		if !ValidInitiativeSlug(p.Initiative) {
			return inv("invalid-initiative", "%q is not a well-formed initiative slug", p.Initiative)
		}
		return nil
	case OpRetired:
		if current == nil {
			return inv("unregistered-project", "project %s is not registered", op.Entity)
		}
		if current.Retired {
			return inv("already-retired", "project %s is already retired", current.Slug)
		}
		var p RetiredPayload
		if err := decode(op, &p); err != nil {
			return err
		}
		if p.Reason == "" {
			return inv("empty-reason", "a retirement needs its reason")
		}
		return overBudget("retire reason", p.Reason, MaxLabelBytes)
	default:
		return inv("invalid-op", "unknown project op %q", op.Op)
	}
}

// CheckInitiativeOp validates an initiative op against the current
// registry entry (nil when the slug is unregistered). The lifecycle is
// exactly the project's (decision 0015, extended by 0016); the slug rule
// is stricter — no trailing all-digit segment, so prefixed item IDs stay
// unambiguous.
func CheckInitiativeOp(current *Initiative, op Op) error {
	if !ValidActor(op.Actor) {
		return inv("invalid-actor", "actor %q is not a well-formed handle", op.Actor)
	}
	if !ValidInitiativeSlug(op.Entity) {
		return inv("invalid-initiative", "%q is not a well-formed initiative slug", op.Entity)
	}
	switch op.Op {
	case OpRegistered:
		if current != nil {
			if current.Retired {
				return inv("slug-retired", "initiative %s is retired; a retired slug is never reused", current.Slug)
			}
			return inv("already-registered", "initiative %s is already registered", current.Slug)
		}
		var p RegisteredPayload
		if err := decode(op, &p); err != nil {
			return err
		}
		if p.Name == "" {
			return inv("empty-name", "an initiative registers with a display name")
		}
		if err := overBudget("initiative name", p.Name, MaxLabelBytes); err != nil {
			return err
		}
		return overBudget("initiative description", p.Description, MaxLabelBytes)
	case OpRetired:
		if current == nil {
			return inv("unregistered-initiative", "initiative %s is not registered", op.Entity)
		}
		if current.Retired {
			return inv("already-retired", "initiative %s is already retired", current.Slug)
		}
		var p RetiredPayload
		if err := decode(op, &p); err != nil {
			return err
		}
		if p.Reason == "" {
			return inv("empty-reason", "a retirement needs its reason")
		}
		return overBudget("retire reason", p.Reason, MaxLabelBytes)
	default:
		return inv("invalid-op", "unknown initiative op %q", op.Op)
	}
}

// CheckReleaseOp validates a release op against the current registry
// entry (nil when the slug is unregistered). The entity is
// <initiative>.<slug>; lifecycle register → shipped | retired, both
// terminal (decision 0017). The straggler gate on shipping needs the
// item snapshots and lives on the write path, like the located-in
// registry check.
func CheckReleaseOp(current *Release, op Op) error {
	if !ValidActor(op.Actor) {
		return inv("invalid-actor", "actor %q is not a well-formed handle", op.Actor)
	}
	if _, _, ok := ParseReleaseEntity(op.Entity); !ok {
		return inv("invalid-release", "%q is not <initiative>.<slug> with well-formed slugs", op.Entity)
	}
	switch op.Op {
	case OpRegistered:
		if current != nil {
			if current.Retired {
				return inv("slug-retired", "release %s is retired; a retired slug is never reused", current.Slug)
			}
			if current.Shipped {
				return inv("slug-shipped", "release %s is shipped; a shipped slug is never reused", current.Slug)
			}
			return inv("already-registered", "release %s is already registered", current.Slug)
		}
		var p RegisteredPayload
		if err := decode(op, &p); err != nil {
			return err
		}
		if p.Name == "" {
			return inv("empty-name", "a release registers with a display name")
		}
		if err := overBudget("release name", p.Name, MaxLabelBytes); err != nil {
			return err
		}
		return overBudget("release description", p.Description, MaxLabelBytes)
	case OpShipped:
		if current == nil {
			return inv("unregistered-release", "release %s is not registered", op.Entity)
		}
		if current.Retired {
			return inv("release-retired", "release %s is retired", current.Slug)
		}
		if current.Shipped {
			return inv("already-shipped", "release %s is already shipped", current.Slug)
		}
		var p ShippedPayload
		if err := decode(op, &p); err != nil {
			return err
		}
		if len(p.Refs) == 0 {
			return inv("empty-refs", "a ship carries verifiable refs — tag, commit, or artifact")
		}
		for _, ref := range p.Refs {
			if ref.Tag == "" && ref.Commit == "" && ref.Artifact == "" {
				return inv("empty-ref", "a ship ref names a tag, commit, or artifact")
			}
			for _, f := range []struct{ field, s string }{
				{"ship tag", ref.Tag}, {"ship commit", ref.Commit},
				{"ship artifact", ref.Artifact}, {"ship ref note", ref.Note},
			} {
				if err := overBudget(f.field, f.s, MaxLabelBytes); err != nil {
					return err
				}
			}
		}
		return overBudget("ship note", p.Note, MaxLabelBytes)
	case OpRetired:
		if current == nil {
			return inv("unregistered-release", "release %s is not registered", op.Entity)
		}
		if current.Shipped {
			return inv("release-shipped", "release %s is shipped; shipped is terminal", current.Slug)
		}
		if current.Retired {
			return inv("already-retired", "release %s is already retired", current.Slug)
		}
		var p RetiredPayload
		if err := decode(op, &p); err != nil {
			return err
		}
		if p.Reason == "" {
			return inv("empty-reason", "a retirement needs its reason")
		}
		return overBudget("retire reason", p.Reason, MaxLabelBytes)
	default:
		return inv("invalid-op", "unknown release op %q", op.Op)
	}
}

func decode(op Op, into any) error {
	if err := json.Unmarshal(op.Payload, into); err != nil {
		return inv("invalid-payload", "malformed %s payload: %v", op.Op, err)
	}
	return nil
}

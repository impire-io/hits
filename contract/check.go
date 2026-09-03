package contract

import (
	"encoding/json"
	"fmt"
	"regexp"
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

// ValidActor reports whether s is a well-formed actor handle.
func ValidActor(s string) bool { return actorRe.MatchString(s) }

// ValidSlug reports whether s is a well-formed project slug (also the shape
// of an item subject token).
func ValidSlug(s string) bool { return slugRe.MatchString(s) }

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
		return nil
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
		return nil
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
	if p.Type == Task && len(p.LocatedIn) == 0 {
		return inv("task-requires-location", "a task cannot be created without located-in")
	}
	for _, loc := range p.LocatedIn {
		if !ValidSlug(loc) {
			return inv("invalid-slug", "located-in entry %q is not a well-formed project slug", loc)
		}
	}
	return nil
}

func checkEdited(current *Item, op Op) error {
	if current.Status.Terminal() {
		return inv("terminal-status", "item %s is %s; closed items accept notes and links only", current.ID, current.Status)
	}
	var p EditedPayload
	if err := decode(op, &p); err != nil {
		return err
	}
	if p.Priority != nil && !validPriority(*p.Priority) {
		return inv("invalid-priority", "priority %q is not high, normal or low", *p.Priority)
	}
	if p.LocatedIn != nil {
		for _, loc := range *p.LocatedIn {
			if !ValidSlug(loc) {
				return inv("invalid-slug", "located-in entry %q is not a well-formed project slug", loc)
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
	return nil
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
	if op.Op != OpRegistered {
		return inv("invalid-op", "unknown project op %q", op.Op)
	}
	if !ValidSlug(op.Entity) {
		return inv("invalid-slug", "%q is not a well-formed project slug", op.Entity)
	}
	if current != nil {
		return inv("already-registered", "project %s is already registered", current.Slug)
	}
	var p RegisteredPayload
	if err := decode(op, &p); err != nil {
		return err
	}
	if p.Name == "" {
		return inv("empty-name", "a project registers with a display name")
	}
	return nil
}

func decode(op Op, into any) error {
	if err := json.Unmarshal(op.Payload, into); err != nil {
		return inv("invalid-payload", "malformed %s payload: %v", op.Op, err)
	}
	return nil
}

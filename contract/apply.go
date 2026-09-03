package contract

import "fmt"

// Apply folds one op into an item snapshot and returns the new snapshot with
// Seq set to the op's stream sequence. It is a pure fold: current is never
// mutated, and an op at or below the snapshot's Seq is skipped unchanged —
// that idempotence is what makes replay safe. Apply assumes the op passed
// CheckOp when it was appended; a malformed op in the log is an error here,
// because the log is the source of truth and disagreeing with it is not an
// option.
func Apply(current *Item, op Op, seq uint64) (*Item, error) {
	if current != nil && seq <= current.Seq {
		return current, nil
	}

	if op.Op == OpCreated {
		var p CreatedPayload
		if err := decode(op, &p); err != nil {
			return nil, err
		}
		prio := p.Priority
		if prio == "" {
			prio = Normal
		}
		return &Item{
			ID:              op.Entity,
			Type:            p.Type,
			Status:          Open,
			Priority:        prio,
			Report:          p.Report,
			Reporter:        op.Actor,
			Created:         op.At,
			LocatedIn:       p.LocatedIn,
			DiscoveredWhile: p.DiscoveredWhile,
			Seq:             seq,
		}, nil
	}
	if current == nil {
		return nil, fmt.Errorf("apply %s to item %s: no snapshot before creation", op.Op, op.Entity)
	}

	it := current.clone()
	it.Seq = seq

	switch op.Op {
	case OpNoted:
		var p NotedPayload
		if err := decode(op, &p); err != nil {
			return nil, err
		}
		it.Notes = append(it.Notes, Note{Author: op.Actor, At: op.At, Text: p.Text})
	case OpEdited:
		var p EditedPayload
		if err := decode(op, &p); err != nil {
			return nil, err
		}
		if p.Priority != nil {
			it.Priority = *p.Priority
		}
		if p.LocatedIn != nil {
			it.LocatedIn = append([]string(nil), *p.LocatedIn...)
		}
		if p.DiscoveredWhile != nil {
			it.DiscoveredWhile = *p.DiscoveredWhile
		}
		if p.Lands != nil {
			it.Lands = append([]Land(nil), *p.Lands...)
		}
	case OpTransitioned:
		var p TransitionedPayload
		if err := decode(op, &p); err != nil {
			return nil, err
		}
		it.Status = p.To
		if len(p.LocatedIn) > 0 {
			it.LocatedIn = p.LocatedIn
		}
		if p.To.Terminal() {
			it.Closed = p.Closed
			it.FixedBy = p.FixedBy
			it.AmendedDesign = p.AmendedDesign
		}
	case OpClaimed:
		var p ClaimedPayload
		if err := decode(op, &p); err != nil {
			return nil, err
		}
		it.Claim = &Claim{By: op.Actor, At: op.At, StolenFrom: p.StolenFrom}
	case OpReleased:
		it.Claim = nil
	case OpBlocked:
		var p BlockedPayload
		if err := decode(op, &p); err != nil {
			return nil, err
		}
		it.Interrupted = p.Interrupted
		it.BlockedBy = p.BlockedBy
		it.Status = Blocked
	case OpUnblocked:
		it.Status = it.Interrupted
		it.Interrupted = ""
		it.BlockedBy = ""
	case OpLinked:
		var p LinkedPayload
		if err := decode(op, &p); err != nil {
			return nil, err
		}
		it.Links = append(it.Links, Link{Type: p.Type, To: p.To})
	case OpUnlinked:
		var p LinkedPayload
		if err := decode(op, &p); err != nil {
			return nil, err
		}
		links := it.Links[:0]
		for _, l := range it.Links {
			if !(l.Type == p.Type && l.To == p.To) {
				links = append(links, l)
			}
		}
		it.Links = links
	case OpTombstoned:
		var p TombstonedPayload
		if err := decode(op, &p); err != nil {
			return nil, err
		}
		it.Tombstoned = true
		it.TombstoneReason = p.Reason
	default:
		return nil, fmt.Errorf("apply: unknown item op %q", op.Op)
	}
	return it, nil
}

// ApplyProject folds one project op into a registry entry, with the same
// idempotence rule as Apply.
func ApplyProject(current *Project, op Op, seq uint64) (*Project, error) {
	if current != nil && seq <= current.Seq {
		return current, nil
	}
	if op.Op != OpRegistered {
		return nil, fmt.Errorf("apply: unknown project op %q", op.Op)
	}
	var p RegisteredPayload
	if err := decode(op, &p); err != nil {
		return nil, err
	}
	return &Project{Slug: op.Entity, Name: p.Name, Description: p.Description, Seq: seq}, nil
}

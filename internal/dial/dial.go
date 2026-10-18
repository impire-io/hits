// Package dial is the one seam every hits binary connects through, and
// the place the substrate is chosen (hits-hq decision 0023): a chronicle
// context — the hosted account signed into with `chronicle login`, or a
// credential on any NATS saved with `chronicle context add` — dials the
// store on chronicle; a hits context — the document `hits context add`
// saves — dials a 0.7 fleet frozen at its release. A name resolves
// against chronicle's contexts first, then hits's; a name both hold is
// refused; with no name, chronicle's selection, else hits's default.
package dial

import (
	"errors"
	"fmt"
	"os"

	ccli "github.com/impire-io/chronicle/cli"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/client/chronicle"
	"github.com/impire-io/hits/client/fleet"
	"github.com/impire-io/hits/contract"
	"github.com/impire-io/hits/internal/connect"
)

// ErrNoContext is a dial with no context selected and none named.
var ErrNoContext = errors.New("no context: run 'chronicle login' for the hosted account or 'chronicle context add' for a credential on your own NATS, then select it or pass --context; a 0.7 fleet is reached through a hits context ('hits context add')")

// ErrActorOnChronicle is an actor named for a chronicle context, whose
// author is its principal.
var ErrActorOnChronicle = errors.New("--actor and HITS_ACTOR name an actor, and a chronicle context's author is its principal: unset them, or dial a hits context")

// ErrNoActor is a write-capable dial of a hits context with no actor.
var ErrNoActor = errors.New("no actor for the 0.7 fleet: pass --actor, set HITS_ACTOR, or set a default in the hits client config")

// Context dials the named context ("" is the selection) and returns the
// HITS client on it with the function that closes the connection. actor
// is the --actor flag's value: refused with a chronicle context, where
// the author is the principal; on a hits context it is the actor, else
// HITS_ACTOR, else the hits config's default, and one of them must name
// somebody.
func Context(name, actor string) (client.Client, func(), error) {
	hitsName, hits, err := connect.Resolve(name)
	if err != nil {
		return nil, nil, err
	}
	// chronicle's loader refuses a name it does not hold; a hits context
	// of that name is then what the name means.
	cx, chron, err := ccli.LoadContext(name)
	if err != nil {
		if hits {
			chron = false
		} else {
			return nil, nil, fmt.Errorf("%w; a 0.7 fleet is reached through a hits context ('hits context add')", err)
		}
	}
	switch {
	case chron && hits && name != "":
		return nil, nil, fmt.Errorf("context %q is both a chronicle context and a hits context: rename one", name)
	case chron:
		if actor != "" || os.Getenv("HITS_ACTOR") != "" {
			return nil, nil, ErrActorOnChronicle
		}
		cc, err := cx.Dial()
		if err != nil {
			return nil, nil, fmt.Errorf("connect: %w", err)
		}
		return chronicle.New(cc, storeOf(cx.Store)), cc.Close, nil
	case hits:
		actor, err := actorFor(actor)
		if err != nil {
			return nil, nil, err
		}
		nc, err := connect.Connect(hitsName, "hits")
		if err != nil {
			return nil, nil, fmt.Errorf("connect: %w", err)
		}
		return fleet.New(nc, actor), nc.Close, nil
	}
	return nil, nil, ErrNoContext
}

// actorFor resolves the fleet's actor: the flag, HITS_ACTOR, the hits
// config's default, in that order (the 0.7 convention).
func actorFor(flag string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	if a := os.Getenv("HITS_ACTOR"); a != "" {
		return a, nil
	}
	if a := connect.DefaultActor(); a != "" {
		return a, nil
	}
	return "", ErrNoActor
}

// storeOf is the store a context's selection names — a context saved by
// `chronicle login` selects none, and a HITS lives in the one store named
// hits.
func storeOf(selected string) string {
	if selected == "" {
		return contract.StoreName
	}
	return selected
}

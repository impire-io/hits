// Package dial is the one seam every hits binary connects through: a
// chronicle context — the hosted account signed into with `chronicle
// login`, or a credential on any NATS saved with `chronicle context add` —
// loaded and dialled as chronicle's own CLI dials it (hits-hq
// 02-DESIGN/the-client.md § the connection). The store is the context's
// selected store, or hits.
package dial

import (
	"errors"
	"fmt"

	ccli "github.com/impire-io/chronicle/cli"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/contract"
)

// ErrNoContext is a dial with no context selected and none named.
var ErrNoContext = errors.New("no chronicle context: run 'chronicle login' for the hosted account, or 'chronicle context add' for a credential on your own NATS, then select it or pass --context")

// Context dials the named chronicle context ("" is the selection) and
// returns the HITS client on it with the function that closes the
// connection.
func Context(name string) (*client.Client, func(), error) {
	cx, ok, err := ccli.LoadContext(name)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, ErrNoContext
	}
	cc, err := cx.Dial()
	if err != nil {
		return nil, nil, fmt.Errorf("connect: %w", err)
	}
	return client.New(cc, storeOf(cx.Store)), cc.Close, nil
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

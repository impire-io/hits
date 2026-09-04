// Package connect is the one seam every hits binary connects through
// (hits-hq 02-DESIGN/idp-auth.md, decisions 0008-0010). It resolves a
// context name — hits' own context directory first, then the nats CLI's —
// and opens the NATS connection via natscontext either way: a hits context
// by its file path, a nats context by name. The one delta a hits context
// can carry is an oauth block, which layers a token handler that feeds the
// current access token into every connect and reconnect.
//
// HITS owns the exchange; the deployment's auth callout owns validation.
package connect

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/nats-io/nats.go"
	"github.com/synadia-io/orbit.go/natscontext"
)

// Connect resolves the named context ("" means the configured default, else
// the nats CLI's selected one) and opens the connection under natsName.
func Connect(contextName, natsName string) (*nats.Conn, error) {
	name, err := effectiveName(contextName)
	if err != nil {
		return nil, err
	}

	ref, err := lookup(name)
	if err != nil {
		return nil, err
	}

	opts := []nats.Option{nats.Name(natsName)}
	if ref.hits == nil {
		// A nats CLI context (or no context at all): natscontext's own
		// path, exactly as before this package existed.
		nc, _, err := natscontext.Connect(name, opts...)
		return nc, err
	}

	if oc := ref.hits.OAuth; oc != nil {
		if _, err := cacheFor(name).read(); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil, fmt.Errorf("no token for context %q: run 'hits auth login --context %s'", name, name)
			}
			return nil, fmt.Errorf("token cache for context %q: %w", name, err)
		}
		opts = append(opts, nats.TokenHandler(tokenHandler(name, oc)))
	}
	nc, _, err := natscontext.Connect(ref.hits.path, opts...)
	return nc, err
}

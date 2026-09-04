// Package connect is the one seam every hits binary connects through
// (hits-hq 02-DESIGN/idp-auth.md, decisions 0008-0011). It resolves a
// context name against hits' own context directory — the only source of
// connection configuration — and opens the connection by feeding the
// context's nats subtree to natscontext. The one delta a context's oauth
// block adds is a token handler that feeds the current access token into
// every connect and reconnect.
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

// Connect resolves the named context ("" means the configured default)
// and opens the connection under natsName. No context configured at all
// is the default NATS URL, reached without touching the nats CLI's state
// (decision 0011).
func Connect(contextName, natsName string) (*nats.Conn, error) {
	name, err := effectiveName(contextName)
	if err != nil {
		return nil, err
	}

	opts := []nats.Option{nats.Name(natsName)}
	if name == "" {
		return nats.Connect("", opts...)
	}

	hc, err := loadHitsContext(name)
	if err != nil {
		return nil, err
	}

	if oc := hc.OAuth; oc != nil {
		if _, err := cacheFor(name).read(); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil, fmt.Errorf("no token for context %q: run 'hits auth login --context %s'", name, name)
			}
			return nil, fmt.Errorf("token cache for context %q: %w", name, err)
		}
		opts = append(opts, nats.TokenHandler(tokenHandler(name, oc)))
	}

	path, cleanup, err := hc.natsSubtreeFile()
	if err != nil {
		return nil, err
	}
	defer cleanup()
	nc, _, err := natscontext.Connect(path, opts...)
	return nc, err
}

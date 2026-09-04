package connect

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/nats-io/nats.go"
	"github.com/synadia-io/orbit.go/natscontext"
)

// Direct is a connection described in plain settings rather than a saved
// context (hits-hq 02-DESIGN/hits-up.md § plain connection settings).
// Fields mirror the nats CLI's flags; empty means unset, filled from the
// nats CLI's environment variables when a server URL is in play.
type Direct struct {
	Servers  string // comma-separated server URLs ($NATS_URL)
	Creds    string // credentials file ($NATS_CREDS)
	User     string // username ($NATS_USER)
	Password string // password ($NATS_PASSWORD)
	Nkey     string // nkey seed file ($NATS_NKEY)
	TLSCert  string // TLS client certificate file ($NATS_CERT)
	TLSKey   string // TLS client key file ($NATS_KEY)
	TLSCA    string // TLS CA bundle file ($NATS_CA)
}

// Dial is the seam's front door for a binary that takes both a context
// name and plain connection settings. Precedence is flag → environment →
// configuration, per setting: an explicit context outranks $NATS_URL, a
// $NATS_URL outranks the configured default context, and the auth
// variables activate only when a server URL is in play — so an exported
// $NATS_CREDS never bleeds into a context connection.
func Dial(contextName string, d Direct, natsName string) (*nats.Conn, error) {
	if contextName != "" {
		if d != (Direct{}) {
			return nil, errors.New("a context carries its own connection: pass --context or the plain connection flags, not both")
		}
		return Connect(contextName, natsName)
	}
	d = d.fromEnv()
	if d.Servers == "" {
		if d != (Direct{}) {
			return nil, errors.New("plain connection settings need a server: pass --server or set $NATS_URL")
		}
		return Connect("", natsName)
	}
	if err := d.check(); err != nil {
		return nil, err
	}
	return d.dial(natsName)
}

// fromEnv fills unset fields from the nats CLI's environment variables.
// Without a server URL nothing fills: the environment's auth settings
// describe a direct connection, not a context's.
func (d Direct) fromEnv() Direct {
	fill := func(p *string, key string) {
		if *p == "" {
			*p = os.Getenv(key)
		}
	}
	fill(&d.Servers, "NATS_URL")
	if d.Servers == "" {
		return d
	}
	fill(&d.Creds, "NATS_CREDS")
	fill(&d.User, "NATS_USER")
	fill(&d.Password, "NATS_PASSWORD")
	fill(&d.Nkey, "NATS_NKEY")
	fill(&d.TLSCert, "NATS_CERT")
	fill(&d.TLSKey, "NATS_KEY")
	fill(&d.TLSCA, "NATS_CA")
	return d
}

// check rejects the half-pairs natscontext would silently ignore.
func (d Direct) check() error {
	if d.Password != "" && d.User == "" {
		return errors.New("--password goes with --user; a password alone authenticates nothing")
	}
	if (d.TLSCert == "") != (d.TLSKey == "") {
		return errors.New("--tlscert and --tlskey go together; pass both or neither")
	}
	return nil
}

// settings maps the plain fields onto the exact natscontext Settings
// schema a context's nats block carries.
func (d Direct) settings() natscontext.Settings {
	return natscontext.Settings{
		URL:      d.Servers,
		Creds:    d.Creds,
		User:     d.User,
		Password: d.Password,
		NKey:     d.Nkey,
		Cert:     d.TLSCert,
		Key:      d.TLSKey,
		CA:       d.TLSCA,
	}
}

// dial assembles the settings into the nats subtree of an ephemeral
// context — never written to the context directory — and connects through
// the same natscontext loader a saved context takes, so plain settings
// and context files cannot drift apart in meaning.
func (d Direct) dial(natsName string) (*nats.Conn, error) {
	b, err := json.Marshal(d.settings())
	if err != nil {
		return nil, fmt.Errorf("connection settings: %w", err)
	}
	path, cleanup, err := subtreeFile(b)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	nc, _, err := natscontext.Connect(path, nats.Name(natsName))
	return nc, err
}

// Package chronicletest boots chronicle's quick start in-process for the
// tests: an embedded NATS server, one account, one member (admin), the
// node and its indexes — the real wire, never a mock — with a HITS
// declared in it and one initiative registered to file into.
package chronicletest

import (
	"context"
	"testing"
	"time"

	ccli "github.com/impire-io/chronicle/cli"
	chron "github.com/impire-io/chronicle/client"
	"github.com/impire-io/chronicle/devdir"
	"github.com/impire-io/chronicle/index/semantic"
	"github.com/impire-io/chronicle/up"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/client/chronicle"
	"github.com/impire-io/hits/contract"
)

// Initiative is the initiative every test may file into.
const Initiative = "hits"

// Store is the store the tests declare.
const Store = contract.StoreName

// Start boots a quick start, connects its member, declares the store and
// registers the initiative. The connection closes with the test.
func Start(t *testing.T) *chron.Client {
	t.Helper()
	return StartWith(t, nil)
}

// Client is Start with the HITS client on it.
func Client(t *testing.T) *chronicle.Client {
	t.Helper()
	return chronicle.New(Start(t), Store)
}

// StartWith is Start with the quick start's embedding provider set, so
// the meaning index serves.
func StartWith(t *testing.T, embedding *semantic.ProviderConfig) *chron.Client {
	t.Helper()
	t.Setenv("CHRONICLE_CONFIG_HOME", t.TempDir())
	t.Setenv("HITS_INITIATIVE", Initiative)
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	l, err := up.Up(ctx, up.Config{Dir: dir, Port: -1, Embedding: embedding})
	if err != nil {
		t.Fatalf("chronicle up: %v", err)
	}
	t.Cleanup(l.Stop)
	// The context chronicle's own `up` would save: the dial seam's tests
	// load it as the selection.
	if err := ccli.SaveLocalContext(l.URL, dir); err != nil {
		t.Fatalf("save the local context: %v", err)
	}
	if err := ccli.SelectContext(ccli.LocalContextName); err != nil {
		t.Fatalf("select the local context: %v", err)
	}
	cc, err := chron.ConnectNkeyFile(l.URL, devdir.UserNkeyPath(dir), "admin")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(cc.Close)
	if err := chronicle.Init(ctx, cc, Store, "test"); err != nil {
		t.Fatalf("store init: %v", err)
	}
	c := chronicle.New(cc, Store)
	if _, err := c.RegisterInitiative(ctx, client.RegisterInitiativeRequest{Slug: Initiative, Name: "The HITS platform"}); err != nil {
		t.Fatalf("register initiative %s: %v", Initiative, err)
	}
	return cc
}

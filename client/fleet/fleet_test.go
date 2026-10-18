package fleet_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/client/clienttest"
	"github.com/impire-io/hits/client/fleet"
	"github.com/impire-io/hits/internal/fleettest"
	"github.com/impire-io/hits/internal/natstest"
)

// TestSurface: the whole surface against the v0.7.0 fleet built from its
// tag — the compatibility target, as itself.
func TestSurface(t *testing.T) {
	clienttest.Run(t, func(t *testing.T) client.Client {
		return fleettest.Start(t).Client("admin")
	})
}

// TestPingNamesTheNode: ping is the node's own name and version.
func TestPingNamesTheNode(t *testing.T) {
	c := fleettest.Start(t).Client("admin")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	about, err := c.Ping(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if about.Name == "" || about.Version == "" {
		t.Fatalf("about: %+v", about)
	}
}

// TestUnreachable: a NATS with no fleet on it refuses every sentence
// with the one code that says so.
func TestUnreachable(t *testing.T) {
	nc, err := nats.Connect(natstest.Start(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	c := fleet.New(nc, "admin")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = c.GetItem(ctx, "hits-1")
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != fleet.CodeUnreachable {
		t.Fatalf("want %s, got %v", fleet.CodeUnreachable, err)
	}
}

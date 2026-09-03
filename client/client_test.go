package client_test

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/internal/natstest"
	"github.com/impire-io/hits/internal/node"
	"github.com/impire-io/hits/internal/version"
)

// TestPingRoundTrip proves the wire contract end to end: the node registered
// as a micro service on a real NATS server, the client requesting on the
// contract subject, and the payload coming back intact.
func TestPingRoundTrip(t *testing.T) {
	url := natstest.StartJetStream(t)

	svcConn, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect service side: %v", err)
	}
	t.Cleanup(svcConn.Close)

	svc, err := node.Start(context.Background(), svcConn)
	if err != nil {
		t.Fatalf("start node: %v", err)
	}
	t.Cleanup(func() { _ = svc.Stop() })

	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect client side: %v", err)
	}
	t.Cleanup(nc.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	about, err := client.New(nc).Ping(ctx)
	if err != nil {
		t.Fatalf("ping: %v", err)
	}
	if about.Name != "hits" {
		t.Errorf("service name = %q, want %q", about.Name, "hits")
	}
	if about.Version != version.Version {
		t.Errorf("service version = %q, want %q", about.Version, version.Version)
	}
}

// Package natstest is a test-only helper that runs an in-process NATS server,
// so wire-contract tests need no external server.
//
// It is under internal/ because it is not part of the module's public surface.
package natstest

import (
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
)

// Start runs an in-process NATS server on a random localhost port and returns
// its client URL. The server is shut down when the test ends.
func Start(t *testing.T) (url string) {
	t.Helper()
	return start(t, &server.Options{
		Host: "127.0.0.1",
		Port: -1, // pick a random free port
	})
}

// StartJetStream runs an in-process NATS server with JetStream enabled,
// storing state in a per-test temp dir. The server is shut down when the
// test ends.
func StartJetStream(t *testing.T) (url string) {
	t.Helper()
	return start(t, &server.Options{
		Host:      "127.0.0.1",
		Port:      -1, // pick a random free port
		JetStream: true,
		StoreDir:  t.TempDir(),
	})
}

// StartJetStreamMaxBytesRequired runs an in-process JetStream server whose
// account requires every stream config to declare max bytes — the shape
// Synadia Cloud enforces (hits-hq issue 003). The server is shut down when
// the test ends.
func StartJetStreamMaxBytesRequired(t *testing.T) (url string) {
	t.Helper()
	srv := startServer(t, &server.Options{
		Host:      "127.0.0.1",
		Port:      -1, // pick a random free port
		JetStream: true,
		StoreDir:  t.TempDir(),
	})
	limits := map[string]server.JetStreamAccountLimits{
		"": {
			MaxMemory: -1, MaxStore: -1, MaxStreams: -1, MaxConsumers: -1,
			MaxAckPending: -1, MemoryMaxStreamBytes: -1, StoreMaxStreamBytes: -1,
			MaxBytesRequired: true,
		},
	}
	if err := srv.GlobalAccount().UpdateJetStreamLimits(limits); err != nil {
		t.Fatalf("require max bytes on the account: %v", err)
	}
	return srv.ClientURL()
}

func start(t *testing.T, opts *server.Options) (url string) {
	t.Helper()
	return startServer(t, opts).ClientURL()
}

func startServer(t *testing.T, opts *server.Options) *server.Server {
	t.Helper()

	srv, err := server.NewServer(opts)
	if err != nil {
		t.Fatalf("new nats server: %v", err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("nats server not ready in time")
	}
	t.Cleanup(srv.Shutdown)
	return srv
}

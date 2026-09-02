package cli_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/nats-io/nats.go"

	"github.com/impire-io/hits/internal/cli"
	"github.com/impire-io/hits/internal/natstest"
	"github.com/impire-io/hits/internal/node"
	"github.com/impire-io/hits/internal/version"
)

// startNode runs an embedded NATS server with the hits service on it and
// returns a Connector that dials it, so commands run against the real wire.
func startNode(t *testing.T) cli.Connector {
	t.Helper()

	url := natstest.Start(t)
	svcConn, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect service side: %v", err)
	}
	t.Cleanup(svcConn.Close)

	svc, err := node.Start(svcConn)
	if err != nil {
		t.Fatalf("start node: %v", err)
	}
	t.Cleanup(func() { _ = svc.Stop() })

	return func(string) (*nats.Conn, error) { return nats.Connect(url) }
}

func TestPingCommand(t *testing.T) {
	connect := startNode(t)

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), []string{"ping"}, &out, &errOut, connect); err != nil {
		t.Fatalf("run ping: %v", err)
	}
	want := "hits " + version.Version + "\n"
	if out.String() != want {
		t.Errorf("ping output = %q, want %q", out.String(), want)
	}
}

func TestVersionCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), []string{"version"}, &out, &errOut, nil); err != nil {
		t.Fatalf("run version: %v", err)
	}
	if want := version.Version + "\n"; out.String() != want {
		t.Errorf("version output = %q, want %q", out.String(), want)
	}
}

func TestUnknownCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	err := cli.Run(context.Background(), []string{"bogus"}, &out, &errOut, nil)
	if err == nil {
		t.Fatal("unknown command: want error, got nil")
	}
	if !strings.Contains(errOut.String(), "Usage:") {
		t.Errorf("unknown command should print usage, stderr = %q", errOut.String())
	}
}

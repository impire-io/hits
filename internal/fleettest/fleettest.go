// Package fleettest runs the 0.7 fleet for the tests: hits-node and the
// three index services of the v0.7.0 release, built from their tag, on
// an embedded NATS, reached through a hits context in a temporary config
// home. It is the compatibility target of client/fleet (hits-hq decision
// 0023): the frozen release itself, never a stand-in. A fleet that cannot
// be built fails the test; nothing skips.
package fleettest

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/client/fleet"
	"github.com/impire-io/hits/internal/connect"
	"github.com/impire-io/hits/internal/embedtest"
	"github.com/impire-io/hits/internal/natstest"
)

// Version is the release the fleet is frozen at.
const Version = "v0.7.0"

// Context is the hits context the fleet and the tests connect through.
const Context = "fleet"

// Initiative is the initiative every test may file into, registered at
// start as chronicletest registers it.
const Initiative = "hits"

var binaries = []string{"hits-node", "hits-index-search", "hits-index-graph", "hits-index-semantic"}

// Fleet is a running v0.7.0 fleet.
type Fleet struct {
	URL  string
	Conn *nats.Conn
}

// Start builds the fleet if the cache lacks it, boots NATS with
// JetStream, saves the hits context and starts the four services; they
// stop with the test.
func Start(t *testing.T) *Fleet {
	t.Helper()
	bin := ensure(t)
	url := natstest.StartJetStream(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if _, err := connect.AddContext(Context, url, nil); err != nil {
		t.Fatalf("hits context: %v", err)
	}
	provider := embedtest.Server(t)
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(nc.Close)
	// The node creates the ops stream; an index service started before it
	// exists exits at boot.
	run(t, filepath.Join(bin, "hits-node"))
	await(t, nc, fleet.PingSubject, nil)
	run(t, filepath.Join(bin, "hits-index-search"))
	run(t, filepath.Join(bin, "hits-index-graph"))
	run(t, filepath.Join(bin, "hits-index-semantic"), "--embedding-url", provider.URL, "--embedding-model", "fake-model")
	await(t, nc, fleet.SearchSubject, []byte(`{}`))
	await(t, nc, fleet.GraphNeighborsSubject, []byte(`{"kind":"item","id":"none"}`))
	await(t, nc, fleet.SemanticSubject, []byte(`{"text":"ready"}`))
	f := &Fleet{URL: url, Conn: nc}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := f.Client("admin").RegisterInitiative(ctx, client.RegisterInitiativeRequest{Slug: Initiative, Name: "The HITS platform"}); err != nil {
		t.Fatalf("register initiative %s: %v", Initiative, err)
	}
	return f
}

// Client is the fleet client on the running fleet, as actor.
func (f *Fleet) Client(actor string) *fleet.Client {
	return fleet.New(f.Conn, actor)
}

// run starts one service on the context; its output is logged when the
// test fails.
func run(t *testing.T, path string, args ...string) {
	t.Helper()
	cmd := exec.Command(path, append([]string{"--context", Context}, args...)...)
	cmd.Env = os.Environ()
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", filepath.Base(path), err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
		if t.Failed() {
			t.Logf("%s:\n%s", filepath.Base(path), out.String())
		}
	})
}

// await retries a request until a service answers it — a refusal counts
// as an answer — or a minute passes.
func await(t *testing.T, nc *nats.Conn, subject string, data []byte) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, err := nc.RequestWithContext(ctx, subject, data)
		cancel()
		if err == nil {
			return
		}
		if !errors.Is(err, nats.ErrNoResponders) && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("%s: %v", subject, err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: no service answered within a minute", subject)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// ensure returns the directory holding the fleet's binaries, building
// any that is missing from the release tag with go install. HITS_FLEET_BIN
// names the directory; the default is the user cache.
func ensure(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("HITS_FLEET_BIN")
	if dir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			t.Fatalf("user cache dir: %v", err)
		}
		dir = filepath.Join(base, "hits", "fleet", Version)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("fleet cache: %v", err)
	}
	unlock := lock(t, filepath.Join(dir, ".lock"))
	defer unlock()
	var missing []string
	for _, name := range binaries {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			missing = append(missing, "github.com/impire-io/hits/cmd/"+name+"@"+Version)
		}
	}
	if len(missing) == 0 {
		return dir
	}
	cmd := exec.Command("go", append([]string{"install"}, missing...)...)
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(), "GOBIN="+dir, "GOFLAGS=-mod=mod", "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the %s fleet from its tag: %v\n%s", Version, err, out)
	}
	return dir
}

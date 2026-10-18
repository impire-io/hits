package dial

import (
	"errors"
	"strings"
	"testing"

	"github.com/impire-io/hits/client/chronicle"
	"github.com/impire-io/hits/client/fleet"
	"github.com/impire-io/hits/internal/chronicletest"
	"github.com/impire-io/hits/internal/connect"
	"github.com/impire-io/hits/internal/natstest"
)

func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("CHRONICLE_CONFIG_HOME", t.TempDir())
	t.Setenv("CHRONICLE_CONTEXT", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HITS_ACTOR", "")
}

// TestNothingConfigured: no context of either family is the refusal
// that names both ways to get one.
func TestNothingConfigured(t *testing.T) {
	isolate(t)
	if _, _, err := Context("", ""); !errors.Is(err, ErrNoContext) {
		t.Fatalf("want ErrNoContext, got %v", err)
	}
	if _, _, err := Context("nowhere", ""); err == nil || !strings.Contains(err.Error(), "hits context add") {
		t.Fatalf("an unknown name names both ways: got %v", err)
	}
}

// TestHitsContextIsTheFleetWithAnActor: a hits context dials the fleet
// client, as the flag's actor, else HITS_ACTOR's, and refuses without one.
func TestHitsContextIsTheFleetWithAnActor(t *testing.T) {
	isolate(t)
	url := natstest.Start(t)
	if _, err := connect.AddContext("dev", url, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Context("dev", ""); !errors.Is(err, ErrNoActor) {
		t.Fatalf("no actor: want ErrNoActor, got %v", err)
	}
	c, closeConn, err := Context("dev", "someone")
	if err != nil {
		t.Fatal(err)
	}
	defer closeConn()
	if f, ok := c.(*fleet.Client); !ok || f.Author() != "someone" {
		t.Fatalf("want the fleet client as someone, got %T %v", c, c.Author())
	}
	t.Setenv("HITS_ACTOR", "env-actor")
	c2, closeConn2, err := Context("dev", "")
	if err != nil {
		t.Fatal(err)
	}
	defer closeConn2()
	if c2.Author() != "env-actor" {
		t.Fatalf("HITS_ACTOR: got %q", c2.Author())
	}
	// With no name, hits's default context is the fallback.
	if err := connect.SelectContext("dev"); err != nil {
		t.Fatal(err)
	}
	c3, closeConn3, err := Context("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer closeConn3()
	if _, ok := c3.(*fleet.Client); !ok {
		t.Fatalf("the hits default: got %T", c3)
	}
}

// TestChronicleContextIsTheStoreWithoutAnActor: chronicle's selection
// dials the store, and an actor named for it is refused.
func TestChronicleContextIsTheStoreWithoutAnActor(t *testing.T) {
	isolate(t)
	cc := chronicletest.Start(t)
	cc.Close()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c, closeConn, err := Context("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer closeConn()
	if _, ok := c.(*chronicle.Client); !ok {
		t.Fatalf("want the chronicle client, got %T", c)
	}
	if _, _, err := Context("", "someone"); !errors.Is(err, ErrActorOnChronicle) {
		t.Fatalf("--actor: want ErrActorOnChronicle, got %v", err)
	}
	t.Setenv("HITS_ACTOR", "someone")
	if _, _, err := Context("", ""); !errors.Is(err, ErrActorOnChronicle) {
		t.Fatalf("HITS_ACTOR: want ErrActorOnChronicle, got %v", err)
	}
	t.Setenv("HITS_ACTOR", "")
	// A name both families hold is refused.
	if _, err := connect.AddContext("local", natstest.Start(t), nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Context("local", ""); err == nil || !strings.Contains(err.Error(), "both a chronicle context and a hits context") {
		t.Fatalf("ambiguous name: got %v", err)
	}
}

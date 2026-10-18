package chronicle_test

import (
	"context"
	"testing"
	"time"

	"github.com/impire-io/chronicle/index/semantic"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/client/chronicle"
	"github.com/impire-io/hits/client/clienttest"
	"github.com/impire-io/hits/internal/chronicletest"
	"github.com/impire-io/hits/internal/embedtest"
)

// TestSurface: the whole surface on the store on chronicle, with the
// quick start's meaning index fed by the fake provider.
func TestSurface(t *testing.T) {
	clienttest.Run(t, func(t *testing.T) client.Client {
		provider := embedtest.Server(t)
		cc := chronicletest.StartWith(t, &semantic.ProviderConfig{BaseURL: provider.URL, APIKey: "test-key", Model: "fake-model"})
		return chronicle.New(cc, chronicletest.Store)
	})
}

// TestPingReadsTheAnnouncement: ping is the store's announcement, as
// hits store init declared it.
func TestPingReadsTheAnnouncement(t *testing.T) {
	c := chronicletest.Client(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	about, err := c.Ping(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if about.Name != "hits" || about.Version != "test" {
		t.Fatalf("announcement: %+v", about)
	}
}

package dial

import "testing"

// TestStoreOf: the context's selected store when it has one, hits when it
// has none (the-client.md § the connection).
func TestStoreOf(t *testing.T) {
	if got := storeOf(""); got != "hits" {
		t.Errorf("no selection: got %q, want hits", got)
	}
	if got := storeOf("tracker"); got != "tracker" {
		t.Errorf("selection: got %q, want tracker", got)
	}
}

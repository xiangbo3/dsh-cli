package core

import (
	"encoding/json"
	"testing"

	"dsh-cli/internal/protocol"
)

// The context bar's denominator follows the selected model: the window
// cache is seeded by request/context events and merged with the settings
// catalog (the pressure projection's window lags a model switch).
func TestModelWindowCache(t *testing.T) {
	s := NewStore("http://x")
	s.Event("s1", &protocol.SessionEvent{Type: "request/context", Seq: 1,
		Data: json.RawMessage(`{"provider":"p","model":"m","contextWindow":200000}`)})
	if got := s.ModelWindow("p", "m"); got != 200000 {
		t.Fatalf("event-seeded window = %d, want 200000", got)
	}
	s.SetModelWindows(map[string]int{ModelKey("p", "m"): 256000, ModelKey("p", "m2"): 1024})
	if got := s.ModelWindow("p", "m"); got != 256000 {
		t.Fatalf("catalog window = %d, want 256000", got)
	}
	if got := s.ModelWindow("p", "m3"); got != 0 {
		t.Fatalf("unknown model window = %d, want 0", got)
	}
	// Zero windows never land; a later event re-seeds on top.
	s.SetModelWindows(map[string]int{ModelKey("p", "m2"): 0})
	if got := s.ModelWindow("p", "m2"); got != 1024 {
		t.Fatalf("zero window overwrote the cache (%d)", got)
	}
}

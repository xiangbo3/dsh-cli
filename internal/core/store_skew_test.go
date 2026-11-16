// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package core

import (
	"encoding/json"
	"testing"
	"time"

	"dsh-cli/internal/protocol"
)

// skewFeed folds n live-shaped events whose host stamps run d ms ahead of
// local arrival (a fast host clock: local = host - d).
func skewFeed(s *Store, n int, d int64, seq0 int64) int64 {
	for i := 0; i < n; i++ {
		s.Event("s1", &protocol.SessionEvent{
			Type: "session/title", Seq: seq0 + int64(i),
			Time: time.Now().UnixMilli() + d,
			Data: json.RawMessage(`{"title":"t"}`),
		})
	}
	return seq0 + int64(n)
}

func wantSkew(t *testing.T, got, want int64) {
	t.Helper()
	if got < want-600 || got > want+600 {
		t.Fatalf("ClockOffsetMs = %d, want ≈ %d", got, want)
	}
}

func TestHostSkewLiveEstimate(t *testing.T) {
	s := NewStore("http://x")
	skewFeed(s, 5, 5000, 1)
	wantSkew(t, s.Get("s1").ClockOffsetMs, -5000)
}

// A reconnect replays old pages first: their arrival-creation gaps are
// the event age plus the skew, far above the live ones, so the minimum
// estimate must home in on the live value once the tail flows.
func TestHostSkewReplayDoesNotLockEstimate(t *testing.T) {
	s := NewStore("http://x")
	skewFeed(s, 3, -600_000, 1) // replay: stamps ten minutes old
	wantSkew(t, s.Get("s1").ClockOffsetMs, 600_000)
	skewFeed(s, 3, 5000, 10) // live tail, host five seconds fast
	wantSkew(t, s.Get("s1").ClockOffsetMs, -5000)
}

func TestHostSkewImplausibleStampIgnored(t *testing.T) {
	s := NewStore("http://x")
	s.Event("s1", &protocol.SessionEvent{Type: "session/title", Seq: 1,
		Time: 12345, Data: json.RawMessage(`{"title":"t"}`)})
	if got := s.Get("s1").ClockOffsetMs; got != 0 {
		t.Fatalf("ClockOffsetMs = %d, want 0 (never sampled)", got)
	}
}

// TurnStart reaches the UI on the local basis, so a turn that just
// started reads as just started no matter how the host clock is set.
func TestHostSkewTurnStartBasis(t *testing.T) {
	s := NewStore("http://x")
	seq := skewFeed(s, 3, 2000, 1) // calibrate: host clock two seconds fast
	start := time.Now().UnixMilli() + 2000
	s.Event("s1", &protocol.SessionEvent{Type: "turn/start", Seq: seq,
		Time: start, Data: json.RawMessage(`{"turn":1}`)})
	snap := s.Get("s1")
	if e := time.Since(snap.TurnStart); e < -1*time.Second || e > 3*time.Second {
		t.Fatalf("turn elapsed = %v, want ≈ 0 on the local basis", e)
	}
}

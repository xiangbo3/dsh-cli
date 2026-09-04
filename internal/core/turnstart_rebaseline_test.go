// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package core

import (
	"encoding/json"
	"testing"
	"time"

	"dsh-cli/internal/protocol"
	"dsh-cli/internal/textutil"
)

// TestSnapshotTurnStartZeroMapping pins the unit trap: a zero TurnStartAt
// is "no turn/start in view", which must surface as the true zero time
// (time.UnixMilli(0) is 1970-01-01 and fails the IsZero guards).
func TestSnapshotTurnStartZeroMapping(t *testing.T) {
	s := NewStore("http://x")
	s.Sess("s1")
	if snap := s.Get("s1"); !snap.TurnStart.IsZero() {
		t.Fatalf("fresh session TurnStart = %v, want zero", snap.TurnStart)
	}
}

// TestTurnStartLostOnMidTurnRebaseline is the field bug: a long turn emits
// more than one history page (120 events), and a reconnect / downlink-stall
// re-baseline rebuilds the fold from the last 120-event tail — which no
// longer contains the current turn's turn/start. The elapsed readout must
// degrade to "0s" instead of counting from 1970 (≈20000 days), and the
// turn-end duration must be 0 instead of the epoch span.
func TestTurnStartLostOnMidTurnRebaseline(t *testing.T) {
	s := NewStore("http://x")
	now := time.Now().UnixMilli()

	chunk := func(seq int64) *protocol.SessionEvent {
		raw, _ := json.Marshal(map[string]any{"turn": 1, "step": 1, "chunk": map[string]any{"type": "text", "text": "x"}})
		return &protocol.SessionEvent{Type: "assistant/chunk", Seq: seq, Time: now + seq, Data: raw}
	}

	s.Sess("s1")
	s.SetSessions([]protocol.SessionSummary{{SessionId: "s1", Running: true, Cwd: "/w"}})
	s.Event("s1", &protocol.SessionEvent{Type: "turn/start", Seq: 1, Time: now, Data: mustJSON(`{"turn":1}`)})
	var all []*protocol.SessionEvent
	for seq := int64(2); seq <= 131; seq++ {
		e := chunk(seq)
		all = append(all, e)
		s.Event("s1", e)
	}
	if snap := s.Get("s1"); snap.TurnStart.IsZero() {
		t.Fatal("turn start known before the re-baseline")
	}

	// Reconnect: the host answers session.history with the LAST 120 events
	// only (seq 12..131); the turn/start at seq 1 is outside the window.
	tail := &protocol.HistoryResponse{HasMore: true}
	for _, e := range all[10:] {
		tail.Events = append(tail.Events, protocol.HistoryEntry{Event: *e})
	}
	s.LoadTail("s1", tail)

	snap := s.Get("s1")
	if !snap.TurnStart.IsZero() {
		t.Fatalf("TurnStart = %v, want zero (start event outside the tail page)", snap.TurnStart)
	}
	// The UI readout, exactly as the top bar renders it (guarded): without
	// the guard, time.Since on the zero time overflows the int64 Duration
	// range (~740000 days of span) into a garbage ~292-year readout.
	elapsed := "0s"
	if !snap.TurnStart.IsZero() {
		elapsed = textutil.HumanDuration(time.Since(snap.TurnStart))
	}
	if elapsed != "0s" {
		t.Fatalf("elapsed readout = %q, want 0s", elapsed)
	}

	// turn/end with the start lost: duration 0, not the epoch span.
	s.Event("s1", &protocol.SessionEvent{Type: "turn/end", Seq: 132, Time: now + 200,
		Data: mustJSON(`{"turn":1,"reason":{"kind":"completed"}}`)})
	select {
	case info := <-s.EndOfTurn():
		if info.Ms != 0 {
			t.Fatalf("turn-end Ms = %d, want 0 (start lost)", info.Ms)
		}
	case <-time.After(time.Second):
		t.Fatal("no turn-end delivered")
	}
}

// TestTurnStartStaleAfterRebaseline is the sibling case: the tail page
// DOES contain a turn/start — but the previous turn's (with its turn/end),
// not the current one. The first turn-bound event of the current turn
// (different turn number) must invalidate the foreign start.
func TestTurnStartStaleAfterRebaseline(t *testing.T) {
	tr := NewTranscript()
	// Previous turn (turn 9), completed, inside the tail page.
	tr.Apply(ev(t, "turn/start", 1, map[string]any{"turn": 9}))
	tr.Apply(ev(t, "turn/end", 2, map[string]any{"turn": 9, "reason": map[string]any{"kind": "completed"}}))
	if tr.TurnStartAt == 0 {
		t.Fatal("previous turn's start recorded")
	}
	// Current turn's (turn 10) first event arrives after the re-baseline.
	tr.Apply(ev(t, "assistant/chunk", 3, map[string]any{
		"turn": 10, "step": 1, "chunk": map[string]any{"type": "text", "text": "x"},
	}))
	if tr.TurnStartAt != 0 {
		t.Fatalf("TurnStartAt = %d, want 0 (stale start of turn 9)", tr.TurnStartAt)
	}
	// A legit start for the current turn (a later turn in the log) wins.
	tr.Apply(ev(t, "turn/start", 4, map[string]any{"turn": 10}))
	if tr.TurnStartAt == 0 {
		t.Fatal("legit turn/start must restore the start")
	}
	// Same-numbered events never invalidate (the normal live path).
	tr.Apply(ev(t, "assistant/chunk", 5, map[string]any{
		"turn": 10, "step": 1, "chunk": map[string]any{"type": "text", "text": "y"},
	}))
	if tr.TurnStartAt == 0 {
		t.Fatal("same-turn chunk must not invalidate its own start")
	}
}

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
// longer contains the current turn's turn/start. The old fold knew that
// turn's start, so the re-baseline must carry it over: the elapsed readout
// keeps counting (no reset to "0s"), and a later turn-end still measures
// the real duration. (The zero-mapping guard above still stands: a start
// the old fold never knew stays zero, never 1970.)
func TestTurnStartLostOnMidTurnRebaseline(t *testing.T) {
	s := NewStore("http://x")
	now := time.Now().UnixMilli()
	start := now - 3_600_000 // the turn has been running an hour

	chunk := func(seq int64) *protocol.SessionEvent {
		raw, _ := json.Marshal(map[string]any{"turn": 1, "step": 1, "chunk": map[string]any{"type": "text", "text": "x"}})
		return &protocol.SessionEvent{Type: "assistant/chunk", Seq: seq, Time: start + seq, Data: raw}
	}

	s.Sess("s1")
	s.SetSessions([]protocol.SessionSummary{{SessionId: "s1", Running: true, Cwd: "/w"}})
	s.Event("s1", &protocol.SessionEvent{Type: "turn/start", Seq: 1, Time: start, Data: mustJSON(`{"turn":1}`)})
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
	if snap.TurnStart.IsZero() {
		t.Fatal("TurnStart lost across the re-baseline (elapsed would reset to 0s)")
	}
	if got := snap.TurnStart.UnixMilli(); got != start {
		t.Fatalf("TurnStart = %d, want the carried-over %d", got, start)
	}
	// The UI readout, exactly as the top bar renders it (guarded): without
	// the guard, time.Since on the zero time overflows the int64 Duration
	// range (~740000 days of span) into a garbage ~292-year readout.
	elapsed := "0s"
	if !snap.TurnStart.IsZero() {
		elapsed = textutil.HumanDuration(time.Since(snap.TurnStart))
	}
	if elapsed == "0s" {
		t.Fatalf("elapsed readout reset to 0s across the re-baseline")
	}
	if elapsed != "1h" {
		t.Fatalf("elapsed readout = %q, want 1h (the hour-long turn kept counting)", elapsed)
	}

	// turn/end after the re-baseline: the carried-over start still
	// measures the turn's real duration.
	s.Event("s1", &protocol.SessionEvent{Type: "turn/end", Seq: 132, Time: now + 200,
		Data: mustJSON(`{"turn":1,"reason":{"kind":"completed"}}`)})
	select {
	case info := <-s.EndOfTurn():
		if want := int64(3_600_000 + 200); info.Ms != want {
			t.Fatalf("turn-end Ms = %d, want %d (start carried over)", info.Ms, want)
		}
	case <-time.After(time.Second):
		t.Fatal("no turn-end delivered")
	}
}

// TestTurnStartCarryOverBoundary pins the carry-over's two guards: a tail
// page that spans the previous turn's end must restore the CURRENT turn's
// start (not the previous turn's), and a tail that already carries a newer
// turn's start must not be overwritten by the old fold's.
func TestTurnStartCarryOverBoundary(t *testing.T) {
	now := time.Now().UnixMilli()
	ev := func(typ string, seq int64, v map[string]any) protocol.HistoryEntry {
		raw, _ := json.Marshal(v)
		return protocol.HistoryEntry{Event: protocol.SessionEvent{Type: typ, Seq: seq, Time: now + seq, Data: raw}}
	}

	// Old fold: mid turn 10, start known.
	s := NewStore("http://x")
	s.Sess("s1")
	s.SetSessions([]protocol.SessionSummary{{SessionId: "s1", Running: true, Cwd: "/w"}})
	s.Event("s1", &protocol.SessionEvent{Type: "turn/start", Seq: 50, Time: now + 50,
		Data: mustJSON(`{"turn":10}`)})
	s.Event("s1", &protocol.SessionEvent{Type: "assistant/chunk", Seq: 51, Time: now + 51,
		Data: mustJSON(`{"turn":10,"step":1,"chunk":{"type":"text","text":"x"}}`)})

	// Tail spans turn 9's start+end and turn 10's chunks: the rebuild
	// invalidates turn 9's start (attrTurn), the carry-over restores the
	// turn-10 start the old fold knew.
	s.LoadTail("s1", &protocol.HistoryResponse{Events: []protocol.HistoryEntry{
		ev("turn/start", 30, map[string]any{"turn": 9}),
		ev("turn/end", 31, map[string]any{"turn": 9, "reason": map[string]any{"kind": "completed"}}),
		ev("assistant/chunk", 51, map[string]any{"turn": 10, "step": 1, "chunk": map[string]any{"type": "text", "text": "x"}}),
	}})
	if got := s.Get("s1").TurnStart.UnixMilli(); got != now+50 {
		t.Fatalf("TurnStart = %d, want the turn-10 start %d", got, now+50)
	}

	// A newer turn's start inside the tail wins: no overwrite.
	s2 := NewStore("http://x")
	s2.Sess("s1")
	s2.SetSessions([]protocol.SessionSummary{{SessionId: "s1", Running: true, Cwd: "/w"}})
	s2.Event("s1", &protocol.SessionEvent{Type: "turn/start", Seq: 50, Time: now + 50,
		Data: mustJSON(`{"turn":10}`)})
	s2.LoadTail("s1", &protocol.HistoryResponse{Events: []protocol.HistoryEntry{
		ev("turn/end", 60, map[string]any{"turn": 10, "reason": map[string]any{"kind": "completed"}}),
		ev("turn/start", 61, map[string]any{"turn": 11}),
	}})
	if got := s2.Get("s1").TurnStart.UnixMilli(); got != now+61 {
		t.Fatalf("TurnStart = %d, want the fresh turn-11 start %d", got, now+61)
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

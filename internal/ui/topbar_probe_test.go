// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"dsh-cli/internal/app"
	"dsh-cli/internal/protocol"
)

// topbarProbeModel builds a model with a long transcript (200 turns of
// user/assistant messages) so the pane scrolls.
func topbarProbeModel(t *testing.T, w, h int) *Model {
	t.Helper()
	m := dockProbeModel(t)
	m.W, m.H = w, h
	var evs []protocol.HistoryEntry
	for i := 0; i < 200; i++ {
		evs = append(evs,
			protocol.HistoryEntry{Event: protocol.SessionEvent{Type: "user/message", Seq: int64(i*2 + 1), Data: json.RawMessage(
				fmt.Sprintf(`{"role":"user","content":[{"type":"text","text":"message %d"}]}`, i))}},
			protocol.HistoryEntry{Event: protocol.SessionEvent{Type: "assistant/message", Seq: int64(i*2 + 2), Data: json.RawMessage(
				fmt.Sprintf(`{"turn":%d,"step":1,"message":{"role":"assistant","content":[{"type":"text","text":"answer %d with some length to wrap a bit"}]}`, i, i))}},
		)
	}
	m.st.LoadTail("s1", &protocol.HistoryResponse{Events: evs})
	return m
}

// TestTopbarPinnedWhileScrolling renders the frame through a full wheel
// scroll walk and checks the top bar survives on row 0 with the frame
// never outrunning the window height.
func TestTopbarPinnedWhileScrolling(t *testing.T) {
	m := topbarProbeModel(t, 100, 24)
	m.dockVisible = true // two-column main area, the widest contract
	for step := 0; step < 200; step++ {
		m.scrollBy(-3)
		frame := m.View()
		lines := strings.Split(frame, "\n")
		if len(lines) > m.H {
			t.Fatalf("step %d: frame has %d lines, window is %d", step, len(lines), m.H)
		}
		strip := strings.TrimRight(stripANSI(lines[0]), " ")
		if !strings.Contains(strip, "dsh-cli") {
			t.Fatalf("step %d: row 0 is not the top bar: %q (frame %d lines)", step, strip, len(lines))
		}
	}
	// Dock closed as well.
	m.dockVisible = false
	for step := 0; step < 20; step++ {
		m.scrollBy(-3)
		frame := m.View()
		lines := strings.Split(frame, "\n")
		if len(lines) > m.H {
			t.Fatalf("step %d (no dock): frame has %d lines, window is %d", step, len(lines), m.H)
		}
	}
}

// TestTranscriptRejectsRawEscapes pins the terminal hijack guard: text
// that arrives from the host with raw terminal escapes (a user pasting
// colored terminal output into the composer) must never reach the frame —
// the renderer writes frames to the same tty, so an ESC[7m / ESC[H in the
// transcript would be executed by the terminal mid-frame and clobber the
// pinned top bar.
func TestTranscriptRejectsRawEscapes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a := app.New("http://127.0.0.1:3999")
	a.Start(ctx)
	m := NewModel(a)
	m.splashOff = true
	m.W, m.H = 100, 24
	m.st.SetSessions([]protocol.SessionSummary{{SessionId: "s1", Cwd: "/tmp/x"}})
	m.st.SetActive("s1")
	jq := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	m.st.LoadTail("s1", &protocol.HistoryResponse{Events: []protocol.HistoryEntry{
		{Event: protocol.SessionEvent{Type: "user/message", Seq: 1, Data: json.RawMessage(
			`{"role":"user","content":[{"type":"text","text":` + jq("hello \x1b[7mworld \x1b[H\x1b[0m") + `}]}`)}},
		{Event: protocol.SessionEvent{Type: "assistant/message", Seq: 2, Data: json.RawMessage(
			`{"turn":1,"step":1,"message":{"role":"assistant","content":[{"type":"text","text":` + jq("reply \x1b[38;5;196mred") + `}]}}`)}},
	}})
	frame := m.View()
	for _, seq := range []string{"\x1b[7m", "\x1b[H", "\x1b[38;5;196m"} {
		if strings.Contains(frame, seq) {
			t.Fatalf("frame carries raw escape %q: host text reached the renderer unstripped", seq)
		}
	}
	// The visible text survives the strip — only the escapes go.
	joined := strings.Join(m.trans.lines, " ")
	if !strings.Contains(joined, "hello world") || !strings.Contains(joined, "reply red") {
		t.Fatalf("sanitization ate the text itself: %q", stripANSI(joined))
	}
	// The title lane is host-fed too: a titled session must not inject the
	// top bar's own row.
	m.st.Event("s1", &protocol.SessionEvent{Type: "session/title", Data: json.RawMessage(
		jq(`{"title":"\x1b[2Jevil"}`))})
	frame = m.View()
	if strings.Contains(frame, "\x1b[2J") {
		t.Fatal("frame carries raw escape from the session title")
	}
}

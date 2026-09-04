// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"dsh-cli/internal/app"
	"dsh-cli/internal/protocol"

	tea "github.com/charmbracelet/bubbletea"
)

// forkLog is a fixture session log: two completed turns plus an in-flight
// third. The turn-end items carry the seqs the fork anchor may return
// (turn 1 ends at seq 3, turn 2 ends at seq 7).
func forkLog() []protocol.SessionEvent {
	msg := func(seq int, role, text string) protocol.SessionEvent {
		b, _ := json.Marshal(protocol.Message{Role: role,
			Content: []protocol.ContentBlock{{Type: "text", Text: text}}})
		return protocol.SessionEvent{Type: role + "/message", Seq: int64(seq), Time: int64(seq) * 1000, Data: b}
	}
	end := func(seq, turn int) protocol.SessionEvent {
		b, _ := json.Marshal(protocol.TurnEndEventData{Turn: turn, Reason: protocol.TurnEndReason{Kind: "completed"}})
		return protocol.SessionEvent{Type: "turn/end", Seq: int64(seq), Time: int64(seq) * 1000, Data: b}
	}
	return []protocol.SessionEvent{
		{Type: "turn/start", Seq: 0, Time: 1000, Data: json.RawMessage(`{"turn":1}`)},
		msg(1, "user", "first question"),
		msg(2, "assistant", "first answer"),
		end(3, 1),
		{Type: "turn/start", Seq: 4, Time: 5000, Data: json.RawMessage(`{"turn":2}`)},
		msg(5, "user", "second question"),
		msg(6, "assistant", "second answer"),
		end(7, 2),
		{Type: "turn/start", Seq: 8, Time: 9000, Data: json.RawMessage(`{"turn":3}`)},
		msg(9, "user", "third question"),
	}
}

// forkModel builds a model whose active session holds the fork log. The
// transcript cache is filled with controlled per-item rows (real render
// heights vary with the terminal profile; the anchor math only needs
// deterministic ones), sized like a rendered frame.
func forkModel(t *testing.T) *Model {
	t.Helper()
	a := app.New("http://127.0.0.1:3999") // dead port: fixture only
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a.Start(ctx)
	m := NewModel(a)
	m.splashOff = true
	m.W, m.H = 120, 40
	m.st.SetActive("s1")
	for _, ev := range forkLog() {
		if ok, _ := m.st.Event("s1", &ev); !ok {
			t.Fatalf("event %s/%d not applied", ev.Type, ev.Seq)
		}
	}
	snap := m.st.Get("s1")
	if snap == nil || len(snap.Items) != 7 {
		t.Fatalf("fixture items = %d, want 7", len(snap.Items))
	}
	// 7 items: 2+1+2+1+3+3+1 = 13 rows.
	m.trans.sessID = "s1"
	m.trans.itemsRender = [][]string{
		{"a0", "a1"}, {"a2"}, {"a3", "a4"}, {"a5"},
		{"a6", "a7", "a8"}, {"a9", "b0", "b1"}, {"b2"},
	}
	m.trans.lines = nil
	for _, blk := range m.trans.itemsRender {
		m.trans.lines = append(m.trans.lines, blk...)
	}
	m.transH = 4 // a narrow pane so the 13 rows scroll
	return m
}

// TestForkAnchorSeq pins the fork cut point at the view position: the tail
// pinned uses the host's last-completed-turn fallback (atSeq 0); scrolled
// up, the cut is the last completed-turn boundary rendered fully above the
// view's top row; with no boundary above (top of the log, mid first turn)
// it falls back to 0, and a stale render cache does the same.
func TestForkAnchorSeq(t *testing.T) {
	m := forkModel(t)
	// Item first rows: 0 2 3 5 6 9 12 (heights 2 1 2 1 3 3 1).
	top := func(item int) int {
		row := 0
		for i := 0; i < item; i++ {
			row += len(m.trans.itemsRender[i])
		}
		return row
	}
	if max := m.transMaxOff(); max != 9 {
		t.Fatalf("transMaxOff = %d, want 9 (13 rows, pane 4)", max)
	}
	m.follow = true
	if got := m.forkAnchorSeq("s1"); got != 0 {
		t.Fatalf("tail-pinned anchor = %d, want 0 (host fallback)", got)
	}
	m.follow = false
	m.scroll = top(3) // second turn's question at the view's top row
	if got := m.forkAnchorSeq("s1"); got != 3 {
		t.Fatalf("anchor at turn-2 question = %d, want 3 (turn 1 end)", got)
	}
	m.scroll = top(5) // second turn's answer: its end row is still in view
	if got := m.forkAnchorSeq("s1"); got != 3 {
		t.Fatalf("anchor at turn-2 answer = %d, want 3", got)
	}
	m.scroll = 0 // very top: no completed turn lies above
	if got := m.forkAnchorSeq("s1"); got != 0 {
		t.Fatalf("top-of-log anchor = %d, want 0", got)
	}
	m.scroll = top(1) // inside the first turn: still no boundary above
	if got := m.forkAnchorSeq("s1"); got != 0 {
		t.Fatalf("mid-turn-1 anchor = %d, want 0", got)
	}
	m.scroll = top(3)
	m.trans.itemsRender = m.trans.itemsRender[:5] // the item set moved after the render
	if got := m.forkAnchorSeq("s1"); got != 0 {
		t.Fatalf("stale-cache anchor = %d, want 0", got)
	}
}

// forkTestServer serves session.fork and records the anchor it receives.
type forkTestServer struct {
	srv   *httptest.Server
	sid   []string
	atSeq []int64
}

func newForkTestServer(t *testing.T) *forkTestServer {
	t.Helper()
	fs := &forkTestServer{}
	fs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Payload json.RawMessage `json:"payload"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/session.fork":
			var p struct {
				SessionId string `json:"sessionId"`
				AtSeq     int64  `json:"atSeq"`
			}
			_ = json.Unmarshal(req.Payload, &p)
			fs.sid = append(fs.sid, p.SessionId)
			fs.atSeq = append(fs.atSeq, p.AtSeq)
			fmt.Fprint(w, `{"type":"server-response","result":{"ok":true,"value":{"sessionId":"s-fork"}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	return fs
}

// TestForkKeyBindsAtView pins the f key: with the input empty it forks the
// active session (tail pinned: atSeq 0) and switches focus to the child via
// the usual create path; an f typed into a non-empty input only edits the
// prompt.
func TestForkKeyBindsAtView(t *testing.T) {
	fs := newForkTestServer(t)
	a := app.New(fs.srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a.Start(ctx)
	m := NewModel(a)
	m.splashOff = true
	m.W, m.H = 120, 40
	m.st.SetActive("s1")
	m.View() // render once: pane measured, cache in sync with the store

	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	if cmd == nil {
		t.Fatal("f with empty input must return the fork cmd")
	}
	msg := cmd()
	cm, ok := msg.(createMsg)
	if !ok || cm.err != nil || cm.id != "s-fork" {
		t.Fatalf("fork cmd result = %T (want the s-fork create message)", msg)
	}
	if len(fs.sid) != 1 || fs.sid[0] != "s1" {
		t.Fatalf("server session = %v, want [s1]", fs.sid)
	}
	if len(fs.atSeq) != 1 || fs.atSeq[0] != 0 {
		t.Fatalf("server atSeq = %v, want [0] (tail pinned)", fs.atSeq)
	}
	m.Update(cm)
	if m.st.Active() != "s-fork" {
		t.Fatalf("active = %q, want the forked child", m.st.Active())
	}

	// A typed f lands in the input, not on the fork: once the prompt
	// holds text the chord gives way to the editor.
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	if got := m.inp.value(); got != "xf" {
		t.Fatalf("input = %q, want the typed text", got)
	}
	if len(fs.atSeq) != 1 {
		t.Fatalf("typed f must not fork again (atSeq = %v)", fs.atSeq)
	}
}

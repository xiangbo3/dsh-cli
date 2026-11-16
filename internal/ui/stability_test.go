// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dsh-cli/internal/app"
	"dsh-cli/internal/protocol"

	tea "github.com/charmbracelet/bubbletea"
)

func stabModel(t *testing.T) *Model {
	t.Helper()
	t.Setenv("NO_COLOR", "")
	m := NewModel(app.New("http://127.0.0.1:3999"))
	m.splashOff = true
	return m
}

// TestWindowSizeMsgFloorsTinyTerminal: chrome derives widths like w-2
// from the reported frame; a degenerate pty report must hit the floor
// and the frame must still render (strings.Repeat panics on negative).
func TestWindowSizeMsgFloorsTinyTerminal(t *testing.T) {
	m := stabModel(t)
	m.Update(tea.WindowSizeMsg{Width: 3, Height: 1})
	if m.W != minTermW || m.H != minTermH {
		t.Fatalf("3x1 frame: W=%d H=%d, want the floor %dx%d", m.W, m.H, minTermW, minTermH)
	}
	if frame := m.View(); frame == "" {
		t.Fatal("a floored frame rendered empty")
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if m.W != 100 || m.H != 30 {
		t.Fatalf("a healthy report must pass through unclamped: %dx%d", m.W, m.H)
	}
}

// TestResizeDropsSelection: the selection anchors rows of the old wrap;
// after a re-wrap they would copy the wrong text, so a resize clears it.
func TestResizeDropsSelection(t *testing.T) {
	m := stabModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.sel.anchor(2, 0)
	m.sel.dragTo(4, 3)
	if !m.sel.active {
		t.Fatal("test precondition: the drag must leave a live selection")
	}
	m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	if m.sel.active {
		t.Fatal("a resize must drop the stale selection")
	}
}

// TestMsgDbgTruncatesAtCap: the DSH_MSG_DBG trace stops at the cap and
// never reopens, even though the env var keeps reading set.
func TestMsgDbgTruncatesAtCap(t *testing.T) {
	p := filepath.Join(t.TempDir(), "trace.log")
	t.Setenv("DSH_MSG_DBG", p)
	old := msgDbgCap
	msgDbgCap = 128
	defer func() { msgDbgCap = old }()
	m := stabModel(t)
	for i := 0; i < 40; i++ {
		m.Update(tickMsg{t: time.Now()})
	}
	if m.msgDbgN != -1 || m.msgDbg != nil {
		t.Fatalf("trace must shut at the cap: n=%d file=%v", m.msgDbgN, m.msgDbg != nil)
	}
	b, err := os.ReadFile(p)
	if err != nil || !strings.Contains(string(b), "truncated") {
		t.Fatalf("no truncation marker in the trace: %v", err)
	}
	m.Update(tickMsg{t: time.Now()}) // must stay shut
	if m.msgDbg != nil {
		t.Fatal("the trace reopened after the cap")
	}
}

// TestLastGenRatePrunedWithRoster: cached rates of sessions the host
// no longer lists must not outlive them.
func TestLastGenRatePrunedWithRoster(t *testing.T) {
	m := stabModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.st.SetSessions([]protocol.SessionSummary{{SessionId: "live"}})
	m.lastGenRate["live"] = 11
	m.lastGenRate["gone"] = 22
	m.genRate()
	if _, ok := m.lastGenRate["gone"]; ok {
		t.Fatal("rate of a vanished session survived the roster prune")
	}
	if _, ok := m.lastGenRate["live"]; !ok {
		t.Fatal("the roster prune swept a live session")
	}
}

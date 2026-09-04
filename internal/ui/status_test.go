// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"dsh-cli/internal/app"
	"dsh-cli/internal/protocol"
	"dsh-cli/internal/usage"

	tea "github.com/charmbracelet/bubbletea"
)

// newStatusTestModel builds a TUI model (120x40) with an app that has
// the given usage recorder attached (nil: none attached).
func newStatusTestModel(t *testing.T, rec *usage.Recorder) *Model {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// A closed port keeps the boot probe from ever resolving a host
	// (the tests set their own), so the frames are deterministic.
	a := app.New("http://127.0.0.1:1")
	if rec != nil {
		a.AttachUsage(rec)
	}
	a.Start(ctx)
	m := NewModel(a)
	m.W, m.H = 120, 40
	return m
}

func seedUsageModel(t *testing.T) (*Model, *usage.Recorder) {
	t.Helper()
	rec := usage.New(t.TempDir() + "/usage.json")
	m := newStatusTestModel(t, rec)
	now := time.Now().UnixMilli()
	m.st.SetSessions([]protocol.SessionSummary{
		{SessionId: "s1", Cwd: "/tmp/proj"},
		{SessionId: "s2", Cwd: "/tmp/proj"},
	})
	data, err := json.Marshal(protocol.TitleEventData{Title: "Demo Project"})
	if err != nil {
		t.Fatal(err)
	}
	m.st.Event("s1", &protocol.SessionEvent{Type: "session/title", Seq: 1, Time: now, Data: data})
	m.st.WorkspaceUpsert(&protocol.WorkspaceView{WorkspaceId: "ws1", Path: "/tmp/proj", Title: "proj"})
	rec.Record("s1", "ws1", now, 1, &protocol.TokenUsage{InputTokens: 1200000, OutputTokens: 400000})
	rec.Record("s1", "ws1", now, 2, &protocol.TokenUsage{InputTokens: 100, OutputTokens: 50})
	rec.Record("s2", "ws1", now, 1, &protocol.TokenUsage{InputTokens: 700, OutputTokens: 300})
	m.st.SetHost(&protocol.HostDescription{Version: "9.9", Cwd: "/tmp"})
	return m, rec
}

func TestStatusCommandOpensPopup(t *testing.T) {
	m, _ := seedUsageModel(t)
	cmd, handled := m.localSlash("status", "")
	if !handled || cmd != nil {
		t.Fatalf("localSlash(status) = %v %v, want handled with no cmd", handled, cmd)
	}
	sm, ok := m.topModal().(*statusModal)
	if !ok {
		t.Fatalf("top modal = %T, want *statusModal", m.topModal())
	}
	if sm.sec != 0 {
		t.Fatalf("initial section = %d, want 0 (overview)", sm.sec)
	}
}

func TestStatusOverviewRows(t *testing.T) {
	m, _ := seedUsageModel(t)
	m.localSlash("status", "")
	sm := m.topModal().(*statusModal)
	lines := sm.view(m, 66, 16)
	joined := strings.Join(lines, "\n")

	// The tab line leads; the active section is overview.
	if !strings.Contains(lines[0], "overview") || !strings.Contains(lines[0], "workspaces") || !strings.Contains(lines[0], "sessions") {
		t.Errorf("tab line = %q", lines[0])
	}
	// The connection line (the old toast, now in the window).
	if !strings.Contains(joined, "dsh") || !strings.Contains(joined, "9.9") || !strings.Contains(joined, "2 sessions") {
		t.Errorf("missing the connection line: %q", joined)
	}
	// The four fixed windows with the compact in/out values:
	// s1 (1200000+100 in / 400000+50 out) + s2 (700/300) →
	// in 1.2M / out 400.4K on every window (all records are today).
	for _, label := range []string{"total", "this month", "this week", "today"} {
		found := false
		for _, ln := range lines {
			if strings.Contains(ln, label) && strings.Contains(ln, "1.2M in / 400.4K out") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("metric row %q missing or wrong: %q", label, joined)
		}
	}
}

func TestStatusOverviewWithoutRecorder(t *testing.T) {
	m := newStatusTestModel(t, nil)
	m.localSlash("status", "")
	sm := m.topModal().(*statusModal)
	joined := strings.Join(sm.view(m, 66, 16), "\n")
	if !strings.Contains(joined, "not recording") {
		t.Errorf("missing the not-recording face: %q", joined)
	}
}

func TestStatusSectionKeys(t *testing.T) {
	m, _ := seedUsageModel(t)
	m.localSlash("status", "")
	sm := m.topModal().(*statusModal)

	// → walks forward, ← walks back (both wrap), 1-3 jump, tab cycles.
	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if sm.sec != 1 {
		t.Fatalf("sec after → = %d, want 1", sm.sec)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if sm.sec != 2 {
		t.Fatalf("sec after →→ = %d, want 2", sm.sec)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if sm.sec != 1 {
		t.Fatalf("sec after →→← = %d, want 1 (wrap)", sm.sec)
	}
	// ← wraps from the first section to the last; section walks reset
	// the row cursor.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if sm.sec != 2 {
		t.Fatalf("sec after ← on section 1 = %d, want 2 (wrap)", sm.sec)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	if sm.sec != 0 {
		t.Fatalf("sec after 1 = %d, want 0", sm.sec)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if sm.sec != 1 {
		t.Fatalf("sec after tab = %d, want 1", sm.sec)
	}
	// Reset to sessions and walk the rows (a render first: the cursor
	// clamp follows the last rendered row count).
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	if sm.sec != 2 {
		t.Fatalf("sec after 3 = %d, want 2", sm.sec)
	}
	sm.view(m, 66, 16)
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if sm.cur != 1 {
		t.Fatalf("cur after down = %d, want 1", sm.cur)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if sm.cur != 0 {
		t.Fatalf("cur after up = %d, want 0", sm.cur)
	}
	// Walking a section with no rows keeps cur clamped at zero.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if sm.cur != 0 {
		t.Fatalf("cur on the overview = %d, want 0 (no rows)", sm.cur)
	}
}

func TestStatusSectionsRender(t *testing.T) {
	m, _ := seedUsageModel(t)
	m.localSlash("status", "")
	sm := m.topModal().(*statusModal)

	// Workspaces: the registry enriches the recorded ws1 key with its
	// title (both sessions recorded under ws1 → one row).
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	wsLines := strings.Join(sm.view(m, 66, 16), "\n")
	if !strings.Contains(wsLines, "proj") {
		t.Errorf("workspace section missing the registry title: %q", wsLines)
	}
	if !strings.Contains(wsLines, "1.2M in / 400.4K out") {
		t.Errorf("workspace section missing the totals: %q", wsLines)
	}

	// Sessions: roster titles (s1), the compact per-session totals, the
	// most-recent-first order (s1 last-used after its two records).
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	seLines := strings.Join(sm.view(m, 66, 16), "\n")
	if !strings.Contains(seLines, "Demo Project") {
		t.Errorf("session section missing the roster title: %q", seLines)
	}
	if !strings.Contains(seLines, "1.2M in / 400.1K out") {
		t.Errorf("session section missing the per-session totals: %q", seLines)
	}
	if !strings.Contains(seLines, "700 in / 300 out") {
		t.Errorf("session section missing s2's totals: %q", seLines)
	}
	// The current section row carries the cursor band; the walked row
	// must appear in the window (hgt budget respected).
	if got := len(sm.view(m, 66, 16)); got > 16 {
		t.Errorf("windowed view = %d lines, want <= 16", got)
	}
}

func TestStatusSectionNoneFaces(t *testing.T) {
	m := newStatusTestModel(t, usage.New(t.TempDir()+"/usage.json"))
	m.localSlash("status", "")
	sm := m.topModal().(*statusModal)

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	if j := strings.Join(sm.view(m, 66, 16), "\n"); !strings.Contains(j, "no workspace usage") {
		t.Errorf("workspace empty face missing: %q", j)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	if j := strings.Join(sm.view(m, 66, 16), "\n"); !strings.Contains(j, "no session usage") {
		t.Errorf("session empty face missing: %q", j)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	if j := strings.Join(sm.view(m, 66, 16), "\n"); !strings.Contains(j, "no usage recorded") {
		t.Errorf("overview empty face missing: %q", j)
	}
}

func TestStatusCloseChords(t *testing.T) {
	for _, chord := range []tea.KeyMsg{
		{Type: tea.KeyEsc},
		{Type: tea.KeyEnter},
		{Type: tea.KeyRunes, Runes: []rune{'q'}},
	} {
		m, _ := seedUsageModel(t)
		m.localSlash("status", "")
		if m.topModal() == nil {
			t.Fatal("popup did not open")
		}
		m.Update(chord)
		if m.topModal() != nil {
			t.Errorf("chord %v did not close the popup", chord)
		}
	}
	// Walking keys must NOT close it.
	m, _ := seedUsageModel(t)
	m.localSlash("status", "")
	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.topModal() == nil {
		t.Fatal("walking keys closed the popup")
	}
}

func TestStatusTitleAndHint(t *testing.T) {
	m, _ := seedUsageModel(t)
	sm := newStatusModal(m.loc)
	if sm.title() != "status & usage" {
		t.Errorf("title = %q", sm.title())
	}
	if !strings.Contains(sm.hint(), "esc close") {
		t.Errorf("hint = %q", sm.hint())
	}
}

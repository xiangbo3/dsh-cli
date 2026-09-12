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
	"dsh-cli/internal/config"
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

	// → walks forward, ← walks back (both wrap), 1-4 jump, tab cycles.
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
	// ← wraps from the first section to the last (billing); section
	// walks reset the row cursor.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if sm.sec != 3 {
		t.Fatalf("sec after ← on section 1 = %d, want 3 (wrap)", sm.sec)
	}
	// In the billing section digits go to the price field, not the
	// section jump; tab leaves it (wrapping to overview).
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	if sm.sec != 3 {
		t.Fatalf("digit in billing jumped sections: sec = %d", sm.sec)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if sm.sec != 0 {
		t.Fatalf("sec after tab on billing = %d, want 0 (wrap)", sm.sec)
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
	sm := newStatusModal(m)
	if sm.title() != "status & usage" {
		t.Errorf("title = %q", sm.title())
	}
	if !strings.Contains(sm.hint(), "esc close") {
		t.Errorf("hint = %q", sm.hint())
	}
}

// TestStatusBarGenRate pins the status bar's t/s readout (the recent
// server-side generation average, after the tokens line): the output
// tokens recorded in the recent window over that window's active span,
// tracked across the turn boundary, kept (the last rate) once the window
// empties, and off before anything was generated.
func TestStatusBarGenRate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// A closed port keeps the boot probe from resolving a host, so the
	// frames are deterministic (the test sets its own state).
	a := app.New("http://127.0.0.1:1")
	a.Start(ctx)
	m := NewModel(a)
	m.W, m.H = 120, 40
	m.st.SetSessions([]protocol.SessionSummary{{SessionId: "s1"}})
	m.st.SetActive("s1")
	m.st.SetConnected(true)

	// Before anything is generated the line carries the tokens readout
	// only.
	if bar := stripANSI(m.statusBar(m.W)); strings.Contains(bar, "t/s") {
		t.Fatalf("rate readout before any generation: %q", bar)
	}

	t0 := time.Now().Add(-30 * time.Second)
	e := func(seq int64, offset time.Duration, typ, data string) {
		m.st.Event("s1", &protocol.SessionEvent{
			Type: typ, Seq: seq, Time: t0.Add(offset).UnixMilli(), Data: []byte(data),
		})
	}
	msg := func(seq int64, at time.Duration, in, out int) {
		e(seq, at, "assistant/message", fmt.Sprintf(`{"turn":1,"step":%d,"message":{"role":"assistant","content":[{"type":"text","text":"x"}]},"usage":{"inputTokens":%d,"outputTokens":%d}}`, seq, in, out))
	}

	e(1, 0, "turn/start", `{"turn":1}`)
	msg(2, 5*time.Second, 1000, 500)
	msg(3, 15*time.Second, 1000, 500)

	// Live: 1000 output tokens since the first sample — the rate is the
	// average over that span (1000/15s at the 20s clock → 67t/s; the
	// tighter 15s clock reads 100t/s).
	for _, tc := range []struct {
		at   time.Duration
		want string
	}{
		{20 * time.Second, " · 67t/s"},
		{15 * time.Second, " · 100t/s"},
	} {
		m.now = t0.Add(tc.at)
		bar := stripANSI(m.statusBar(m.W))
		if !strings.Contains(bar, "tokens:") {
			t.Fatalf("tokens readout missing: %q", bar)
		}
		if !strings.Contains(bar, tc.want) {
			t.Fatalf("at %s: want %q in status bar: %q", tc.at, tc.want, bar)
		}
	}

	// The readout survives the turn end (the window, not the turn, is
	// the unit): at the 25s clock the 20s span reads 50t/s.
	e(4, 20*time.Second, "turn/end", `{"turn":1,"reason":{"kind":"completed"}}`)
	m.now = t0.Add(25 * time.Second)
	if bar := stripANSI(m.statusBar(m.W)); !strings.Contains(bar, " · 50t/s") {
		t.Fatalf("rate lost after the turn end: %q", bar)
	}

	// An input-only message (a cache-heavy turn) adds no generation: the
	// rate just ages (1000 over the 30s span → 33t/s).
	e(5, 30*time.Second, "assistant/message", `{"turn":1,"step":4,"message":{"role":"assistant","content":[{"type":"text","text":"x"}]},"usage":{"inputTokens":1000,"cacheReadTokens":700,"outputTokens":0}}`)
	m.now = t0.Add(35 * time.Second)
	if bar := stripANSI(m.statusBar(m.W)); !strings.Contains(bar, " · 33t/s") {
		t.Fatalf("input-only message changed the generation rate: %q", bar)
	}

	// Past the 60s window the samples decay out; the readout stays up on
	// the last rate (the 33t/s of the 35s clock), never going off once
	// the session has generated.
	m.now = t0.Add(90 * time.Second)
	if bar := stripANSI(m.statusBar(m.W)); !strings.Contains(bar, " · 33t/s") {
		t.Fatalf("last rate lost after the window drained: %q", bar)
	}
}

// TestStatusBarTokensLive pins the bottom-line token readout's live
// behavior: while a step streams, the meter carries the streamed output
// estimate (the input is not metered until the step's usage lands). The
// usage chunk — ahead of the canonical message — swaps the estimate for
// the metered value, and the canonical adds nothing (no double count).
func TestStatusBarTokensLive(t *testing.T) {
	m := newStatusTestModel(t, nil)
	m.st.SetSessions([]protocol.SessionSummary{{SessionId: "s1"}})
	m.st.SetActive("s1")

	chunk := func(seq int64, c map[string]any) {
		b, _ := json.Marshal(map[string]any{"turn": 1, "step": 1, "chunk": c})
		m.st.Event("s1", &protocol.SessionEvent{Type: "assistant/chunk", Seq: seq, Time: seq * 1000, Data: b})
	}
	m.st.Event("s1", &protocol.SessionEvent{Type: "turn/start", Seq: 1, Time: 1000, Data: []byte(`{"turn":1}`)})

	// Streaming: the meter carries the streamed-output estimate; the
	// input is not metered yet.
	chunk(2, map[string]any{"type": "block-start", "index": 0, "blockType": "text"})
	chunk(3, map[string]any{"type": "text-delta", "index": 0, "text": "Hello, this is a streaming sentence."})
	in, out := m.tokenTotals()
	if in != 0 || out <= 0 {
		t.Fatalf("while streaming = in %d out %d, want in 0 and a live out estimate", in, out)
	}
	first := out
	chunk(4, map[string]any{"type": "text-delta", "index": 0, "text": " And it keeps growing as the model generates."})
	if _, out2 := m.tokenTotals(); out2 <= first {
		t.Fatalf("estimate did not move with the stream: %d -> %d", first, out2)
	}

	// The usage chunk (ahead of the canonical message) meters the step:
	// the estimate is replaced by the exact value.
	chunk(5, map[string]any{"type": "usage", "usage": map[string]any{"inputTokens": 1000, "outputTokens": 60}})
	if in, out := m.tokenTotals(); in != 1000 || out != 60 {
		t.Fatalf("after usage chunk = in %d out %d, want in 1000 out 60", in, out)
	}

	// The canonical message adds nothing (no double count), and the bar
	// carries the metered value.
	b, _ := json.Marshal(map[string]any{
		"turn": 1, "step": 1,
		"message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "full text"}}},
		"usage":   map[string]any{"inputTokens": 1000, "outputTokens": 60},
	})
	m.st.Event("s1", &protocol.SessionEvent{Type: "assistant/message", Seq: 6, Time: 6000, Data: b})
	if in, out := m.tokenTotals(); in != 1000 || out != 60 {
		t.Fatalf("after canonical = in %d out %d (double count)", in, out)
	}
	bar := stripANSI(m.statusBar(m.W))
	want := "tokens: " + m.th.Glyph.TokenIn + compactInt(1000) + " " + m.th.Glyph.TokenOut + "60"
	if !strings.Contains(bar, want) {
		t.Fatalf("status bar = %q, want %q", bar, want)
	}
}

// TestStatusBilling pins the billing section: the input / output price
// fields apply as they are typed (no confirm key; a blank field clears),
// ↑↓ walks the fields, the values persist to config, and the cost
// follows the token values on every window.
func TestStatusBilling(t *testing.T) {
	t.Setenv("DSH_CLI_HOME", t.TempDir())
	m, _ := seedUsageModel(t)
	m.localSlash("status", "")
	sm := m.topModal().(*statusModal)

	// Jump to the billing section and type the input price: it applies
	// at once (no enter needed) and lands in the config.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})
	if sm.sec != 3 {
		t.Fatalf("jump 4: sec = %d, want 3 (billing)", sm.sec)
	}
	for _, r := range "83.3" {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if m.tokenInPrice != 83.3 {
		t.Fatalf("input price = %v, want 83.3 (applies as typed)", m.tokenInPrice)
	}
	if got := config.Load().TokenInPrice; got != 83.3 {
		t.Fatalf("config input price = %v, want 83.3 (not persisted)", got)
	}

	// ↓ switches to the output field; its price applies at once too.
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if sm.cur != 1 {
		t.Fatalf("down: cur = %d, want 1 (output field)", sm.cur)
	}
	for _, r := range "2.5" {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if m.tokenOutPrice != 2.5 {
		t.Fatalf("output price = %v, want 2.5 (applies as typed)", m.tokenOutPrice)
	}

	// An in-between invalid state does not apply (the last valid value
	// stays); a backspace back to a valid number applies at once.
	m.Update(tea.KeyMsg{Type: tea.KeyBackspace}) // "2.5" -> "2."
	if m.tokenOutPrice != 2.5 {
		t.Fatalf("invalid intermediate state applied: %v", m.tokenOutPrice)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyBackspace}) // "2." -> "2"
	if m.tokenOutPrice != 2 {
		t.Fatalf("backspace to a valid number must apply: %v", m.tokenOutPrice)
	}

	// The cost now follows the token values on every window: the seeded
	// 1.2M in / 400.4K out at 83.3 / 2 per 1M reads 101 (whole at 100+).
	// → walks out of the billing section (there the digits belong to the
	// price fields; the arrows move the sections), wrapping to the overview.
	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if sm.sec != 0 {
		t.Fatalf("walk →: sec = %d, want 0 (billing wraps to overview)", sm.sec)
	}
	joined := strings.Join(sm.view(m, 120, 40), "\n")
	if !strings.Contains(joined, "(101)") && !strings.Contains(joined, "（101元）") {
		t.Fatalf("cost missing from the token values: %q", joined)
	}

	// A blank field clears its price (applies at once, persists).
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})
	m.Update(tea.KeyMsg{Type: tea.KeyUp}) // back to the input field
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	if m.tokenInPrice != 0 {
		t.Fatalf("cleared input price = %v, want 0", m.tokenInPrice)
	}
	if got := config.Load().TokenInPrice; got != 0 {
		t.Fatalf("config input price = %v, want 0 after clearing", got)
	}
	// The cost now reflects the output price only: 400350*2/1M → 0.80.
	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	joined = strings.Join(sm.view(m, 120, 40), "\n")
	if !strings.Contains(joined, "(0.80)") && !strings.Contains(joined, "（0.80元）") {
		t.Fatalf("output-only cost missing: %q", joined)
	}

	// Enter closes like everywhere else (there is no confirm step), and
	// reopening pre-fills the fields from the stored prices.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.topModal() != nil {
		t.Fatal("enter should close the window")
	}
	m.localSlash("status", "")
	sm = m.topModal().(*statusModal)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})
	if got := sm.priceOutEd.string(); got != "2" {
		t.Fatalf("reopened output field = %q, want %q", got, "2")
	}
}

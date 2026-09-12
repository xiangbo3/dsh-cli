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

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// dockProbeModel builds a 100x30 model whose session content carries every
// line shape that broke the old grid: a short styled line (the assistant
// header), a wrapped body line, and wide overruns (a fenced code block and
// an un-wrapped command result, both longer than the transcript pane).
func dockProbeModel(t *testing.T) *Model {
	t.Helper()
	lipgloss.SetColorProfile(termenv.TrueColor)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a := app.New(newFakeHost(t).URL)
	a.Start(ctx)
	t.Setenv("DSH_CLI_HOME", t.TempDir()) // no inherited startup language
	m := NewModel(a)
	m.W, m.H = 100, 30
	m.st.SetSessions([]protocol.SessionSummary{{SessionId: "s1", Cwd: "/tmp/x"}})
	m.st.SetActive("s1")
	long := strings.TrimRight(strings.Repeat("abcdefghijklmnop ", 8), " ") // 143 cells
	jq := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	events := []protocol.HistoryEntry{
		{Event: protocol.SessionEvent{Type: "user/message", Seq: 1, Data: json.RawMessage(
			`{"role":"user","content":[{"type":"text","text":"hi"}]}`)}},
		{Event: protocol.SessionEvent{Type: "assistant/message", Seq: 2, Data: json.RawMessage(
			`{"turn":1,"step":1,"message":{"role":"assistant","content":[{"type":"text","text":` + jq(long) + `}]}}`)}},
		{Event: protocol.SessionEvent{Type: "assistant/message", Seq: 3, Data: json.RawMessage(
			`{"turn":1,"step":2,"message":{"role":"assistant","content":[{"type":"text","text":` + jq("```\n"+strings.Repeat("x", 90)+"\n```") + `}]}}`)}},
		{Event: protocol.SessionEvent{Type: "command/done", Seq: 4, Data: json.RawMessage(
			`{"kind":"success","text":` + jq(long) + `}`)}},
	}
	m.st.LoadTail("s1", &protocol.HistoryResponse{Events: events})
	return m
}

// TestDockFrameGridExact is the dock drift regression: with the ctrl+b dock
// open, the composed frame is a grid of exact columns — every line spans
// exactly W cells, the gutter column is blank on every main row, and the
// dock region of each main row equals the standalone dock render cell for
// cell. Before the fix the transcript's ragged lines (a styled header 13
// columns short of its pane, a job detail line 60 cells over) pushed the
// dock's column one-for-one on those rows: the panel's left edge drifted
// with the transcript content instead of sitting at a fixed column.
func TestDockFrameGridExact(t *testing.T) {
	m := dockProbeModel(t)
	m.dockVisible = true
	m.dockTab = dockJobs // the probe's wide lines live on the jobs page
	now := time.Now().UnixMilli()
	m.st.MuxJobs("s1", []protocol.JobView{{
		Id: "j1", Label: "probe job", Status: "running",
		Detail: strings.Repeat("d", 90), StartedAt: now - 5000,
	}})
	m.st.MuxProjection("s1", "todos", 1, json.RawMessage(
		`[{"content":"a very long todo item that definitely overruns the dock pane", "status":"pending"}]`))

	_, transW, dockW := m.paneWidths(m.W)
	frame := m.View()
	lines := strings.Split(frame, "\n")

	// 1. Every frame line spans exactly W display cells: nothing for the
	//    renderer to clip or stretch, so no column can drift.
	for i, ln := range lines {
		if pw := plainWidth(ln); pw != m.W {
			t.Fatalf("line %d spans %d cells, want %d: %q", i, pw, m.W, strings.TrimRight(stripANSI(ln), " "))
		}
	}

	// The main area starts below the top bar and its hairline rule (the
	// probe has no toasts); the rule row is all box-drawing.
	top := 2
	if !strings.HasPrefix(stripANSI(lines[top-1]), "─") {
		t.Fatalf("expected the rule at line %d, got %q", top-1, stripANSI(lines[top-1]))
	}
	mainH := m.transH

	tv := strings.Split(m.transcriptView(transW, mainH), "\n")
	dock := strings.Split(m.dockView(dockW, mainH), "\n")

	// 2. Per main row: the transcript region and the dock region of the
	//    composed frame must equal the standalone pane renders — the
	//    composition is a pure grid paste, and the gutter column is blank.
	for i := 0; i < mainH; i++ {
		ln := lines[top+i]
		if got := rowText(ln, transW, transW); got != "" {
			// rowText trims: a blank gutter column reads back as the empty
			// string, a content cell as itself.
			t.Fatalf("row %d: gutter column %d is %q, want blank", i, transW, got)
		}
		if got, want := rowText(ln, 0, transW-1), strings.TrimRight(stripANSI(tv[i]), " "); got != want {
			t.Fatalf("row %d: transcript region drifted\n got %q\nwant %q", i, got, want)
		}
		if got, want := rowText(ln, transW+1, m.W-1), strings.TrimRight(stripANSI(dock[i]), " "); got != want {
			t.Fatalf("row %d: dock region drifted\n got %q\nwant %q", i, got, want)
		}
	}

	// 3. The wide content that used to overrun the panes is still in the
	//    frame — clipped at the pane edge (assertions 1-2 prove the grid
	//    stayed exact), not dropped: a 20-cell run of the code block and a
	//    detail line filling the dock pane must both appear.
	wideCode, wideDetail := false, false
	for i := 0; i < mainH; i++ {
		if strings.Contains(rowText(lines[top+i], 0, transW-1), strings.Repeat("x", 20)) {
			wideCode = true
		}
		if n := strings.Count(rowText(lines[top+i], transW+1, m.W-1), "d"); n >= dockW-5 {
			wideDetail = true
		}
	}
	if !wideCode {
		t.Fatal("the wide code line never reached the frame")
	}
	if !wideDetail {
		t.Fatal("the job detail line never reached the dock pane")
	}
}

// TestDockTabOrder pins the dock's left-to-right tab order: todos,
// jobs, subs, queue, goal. The default page is todos (tab 0), 1-5 jump
// by visual position, and ←→ walk the same order.
func TestDockTabOrder(t *testing.T) {
	m := dockProbeModel(t)
	m.W = 140 // five tabs plus the status pill need room to avoid clipping
	m.dockVisible = true

	tabRow := func() string {
		for _, ln := range strings.Split(stripANSI(m.View()), "\n") {
			if strings.Contains(ln, "TODOS") {
				return ln
			}
		}
		t.Fatal("dock tab row not found")
		return ""
	}
	row := tabRow()
	for _, pair := range [][2]string{{"TODOS", "JOBS"}, {"JOBS", "SUBS"}, {"SUBS", "QUEUE"}, {"QUEUE", "GOAL"}} {
		if i, j := strings.Index(row, pair[0]), strings.Index(row, pair[1]); i < 0 || j < 0 || i > j {
			t.Fatalf("tab order must keep %s before %s: %q", pair[0], pair[1], row)
		}
	}

	// 2 jumps to the second tab's visual position: jobs.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	if m.dockTab != dockJobs {
		t.Fatalf("dockTab = %d, want the jobs tab", m.dockTab)
	}
	// ← walks back to todos, → forward to jobs again.
	m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if m.dockTab != dockTodos {
		t.Fatalf("dockTab = %d, want todos after ←", m.dockTab)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if m.dockTab != dockJobs {
		t.Fatalf("dockTab = %d, want jobs after →", m.dockTab)
	}

	// → must reach the end of the row (the bound is the last tab, not an
	// earlier one — capping partway is how the row gets cut off) and stop
	// there; ← walks back to the first tab and stops there too.
	for _, want := range []int{dockSubs, dockQueue, dockGoal, dockGoal} {
		m.Update(tea.KeyMsg{Type: tea.KeyRight})
		if m.dockTab != want {
			t.Fatalf("dockTab = %d, want %d", m.dockTab, want)
		}
	}
	for _, want := range []int{dockQueue, dockSubs, dockJobs, dockTodos, dockTodos} {
		m.Update(tea.KeyMsg{Type: tea.KeyLeft})
		if m.dockTab != want {
			t.Fatalf("dockTab = %d, want %d", m.dockTab, want)
		}
	}
}

// TestFitWidth pins the pane-clip/pad contract (the string world's
// ultraviolet rule): the result always spans exactly w display cells, wide
// runes straddling the edge become a styled blank in their in-bound column,
// a live tail state is closed before the bare-ground pad, and plain lines
// keep the old padCell bytes.
func TestFitWidth(t *testing.T) {
	red := "\x1b[31m"
	reset := "\x1b[0m"

	cases := []struct {
		name string
		s    string
		w    int
		want string
	}{
		{"plain short keeps old bytes", "hi", 5, "hi   "},
		{"plain exact", "12345", 5, "12345"},
		{"plain long clips", "1234567", 5, "12345"},
		{"styled short ending closed stays byte-identical", red + "hi" + reset, 5, red + "hi" + reset + "   "},
		{"styled short live tail closes before pad", red + "hi", 5, "\x1b[0;31m" + "hi" + reset + "   "},
		{"wide rune straddling edge becomes blank", "abc中", 4, "abc "},
		{"wide rune fitting exactly stays", "abc中", 5, "abc中"},
		{"wide rune fully out becomes blank", "中", 1, " "},
		{"empty to width", "", 3, "   "},
	}
	for _, c := range cases {
		if got := fitWidth(c.s, c.w); got != c.want {
			t.Fatalf("%s: fitWidth(%q, %d) = %q, want %q", c.name, c.s, c.w, got, c.want)
		} else if pw := plainWidth(got); pw != c.w {
			t.Fatalf("%s: result spans %d, want %d", c.name, pw, c.w)
		}
	}

	// A styled segment crossing the clip edge keeps its style on both sides
	// and the tail ends on a clean state.
	in := red + "abcde" + "\x1b[32m" + "fg"
	got := fitWidth(in, 4)
	if plainWidth(got) != 4 {
		t.Fatalf("cross-edge clip spans %d, want 4: %q", plainWidth(got), got)
	}
	cells := scanStyled(got)
	if len(cells) != 4 {
		t.Fatalf("cross-edge clip has %d cells, want 4: %q", len(cells), got)
	}
	for _, c := range cells {
		if !c.state.equals(sgrState{fg: []int{31}}) {
			t.Fatalf("cross-edge clip cell %q lost the red state: %q", string(c.r), got)
		}
	}
}

// TestTruncDisplayStyled pins the ANSI-aware ellipsis: a styled line that
// fits its budget passes through whole (the old walk counted the escape
// bytes as cells and cut it early), an over-long line cuts on a cell
// boundary with the ellipsis riding the last cell's style and a closed tail,
// and a wide rune the cut would split is dropped whole.
func TestTruncDisplayStyled(t *testing.T) {
	if got := truncDisplay("1234567890", 5); got != "1234…" {
		t.Fatalf("plain: %q", got)
	}

	// The dock's job line: 27 visible cells, more than a dozen of them
	// inside escape bytes. It must pass a 27-cell budget uncut.
	job := "  " + "\x1b[38;5;255m" + "◐" + "\x1b[0m" + " running    probe job " + "\x1b[38;5;245m" + "0s" + "\x1b[0m"
	if pw := plainWidth(job); pw != 27 {
		t.Fatalf("probe job line spans %d, want 27", pw)
	}
	if got := truncDisplay(job, 27); got != job {
		t.Fatalf("job line cut early: %q", stripANSI(got))
	}

	// A styled line that truly overruns: the cut lands on a cell boundary,
	// the ellipsis takes the final column in the surviving style, and the
	// tail state is closed (a self-contained line).
	in := "\x1b[31m" + "red " + strings.Repeat("a", 30) + "\x1b[0m"
	got := truncDisplay(in, 10)
	if pw := plainWidth(got); pw != 10 {
		t.Fatalf("styled over cut spans %d, want 10: %q", pw, got)
	}
	if !strings.HasSuffix(stripANSI(got), "…") {
		t.Fatalf("no ellipsis at the tail: %q", stripANSI(got))
	}
	if !strings.HasSuffix(got, "\x1b[0m") {
		t.Fatalf("tail state not closed: %q", got)
	}

	// A wide rune the cut would split is dropped whole: the ellipsis takes
	// its column and the result may come up one cell short.
	got = truncDisplay("aaa中zz", 5)
	if plainWidth(got) > 5 {
		t.Fatalf("wide-rune cut overran: %q (width %d)", got, plainWidth(got))
	}
	if strings.Contains(stripANSI(got), "中") {
		t.Fatalf("split wide rune survived the cut: %q", stripANSI(got))
	}
}

// TestDockJobDurZeroStart pins the unit trap: a job whose StartedAt the
// host left at 0 renders "0s" rather than the span since 1970 (tens of
// thousands of days).
func TestDockJobDurZeroStart(t *testing.T) {
	m := dockProbeModel(t)
	m.dockVisible = true
	m.dockTab = dockJobs
	m.transH = 12 // View() is skipped: set the pane height directly (fork_test pattern)
	m.st.MuxJobs("s1", []protocol.JobView{
		{Id: "j1", Label: "nostrt", Status: "running", StartedAt: 0},
		{Id: "j2", Label: "finstr", Status: "completed", StartedAt: 0, FinishedAt: time.Now().UnixMilli()},
		// short labels: the narrow dock pane would clip a long label's duration
	})
	_, _, dockW := m.paneWidths(m.W)
	out := stripANSI(m.dockView(dockW, m.transH))
	// Each job line's trailing token (after the label) is the duration:
	// it must read exactly 0s — no day/hour unit of a 1970-span.
	for _, ln := range strings.Split(out, "\n") {
		for _, label := range []string{"nostrt", "finstr"} {
			i := strings.Index(ln, label)
			if i < 0 {
				continue
			}
			if tail := strings.TrimSpace(ln[i+len(label):]); tail != "0s" {
				t.Fatalf("job line %q must show 0s, got %q", ln, tail)
			}
		}
	}
}

// TestSubTranscriptEscClose pins the child-window path end to end: the
// subs tab renders the catalog, enter opens the window, the history page
// fills it, and esc closes it (the close switch must know this modal
// type — a read-only page that swallows its keys without closing would
// swallow every key after it and hang the UI).
func TestSubTranscriptEscClose(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	fh := newFakeHost(t)
	fh.subagents = []protocol.SubagentListEntry{{
		Kind: "child", Id: "c1", Mode: "continuable", Activity: "running",
		Label: "Fast child: count README lines",
	}}
	fh.histories["c1"] = []protocol.HistoryEntry{{
		Event: protocol.SessionEvent{Type: "user/message", Seq: 1, Data: json.RawMessage(
			`{"role":"user","content":[{"type":"text","text":"probe"}]}`)},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a := app.New(fh.URL)
	a.Start(ctx)
	t.Setenv("DSH_CLI_HOME", t.TempDir())
	m := NewModel(a)
	m.W, m.H = 120, 30
	m.st.SetSessions([]protocol.SessionSummary{{SessionId: "s1", Cwd: "/tmp/x"}})
	m.st.SetActive("s1")

	m.dockVisible = true
	m.dockTab = dockSubs
	cmd := m.subDockRefresh()
	if cmd == nil {
		t.Fatal("no catalog fetch scheduled for an empty cache")
	}
	upd := func(msg tea.Msg) {
		nv, c := m.Update(msg)
		m = nv.(*Model)
		cmd = c
	}
	upd(cmd())
	if entries, fresh := subagentEntries("s1"); !fresh || len(entries) != 1 {
		t.Fatalf("catalog = %d entries fresh=%v, want the seeded child", len(entries), fresh)
	}
	out := stripANSI(m.View())
	if !strings.Contains(out, "Fast child: count README") {
		t.Fatalf("subs tab does not render the child row: %q", out)
	}
	if !strings.Contains(out, "● Fast child") {
		t.Fatalf("running child must carry the activity dot: %q", out)
	}

	// Enter opens the window and pulls the child's history page.
	upd(tea.KeyMsg{Type: tea.KeyEnter})
	if m.topModal() == nil {
		t.Fatal("enter did not open the child window")
	}
	if cmd == nil {
		t.Fatal("history fetch not scheduled on open")
	}
	upd(cmd())
	sm, ok := m.topModal().(*subagentModal)
	if !ok || sm.loading {
		t.Fatalf("window not filled with the history page")
	}
	if got := stripANSI(m.View()); !strings.Contains(got, "probe") {
		t.Fatalf("window does not show the child's user message: %q", got)
	}

	// Esc closes it (the regression: the close switch lacked this type).
	upd(tea.KeyMsg{Type: tea.KeyEsc})
	if m.topModal() != nil {
		t.Fatalf("esc left the child window open: %T", m.topModal())
	}
}

// TestSubModalScroll pins the child window's scroll: the page is longer
// than the popup body, so the window opens on the first lines and walks
// with the arrows, the page keys, home/end, and the mouse wheel — which
// must not scroll the transcript behind the open window.
func TestSubModalScroll(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	fh := newFakeHost(t)
	// 30 user messages plus their turn ends: 60 page lines, more than
	// double the 16-row popup body the 30-row model renders.
	var events []protocol.HistoryEntry
	for i := 0; i < 30; i++ {
		events = append(events,
			protocol.HistoryEntry{Event: protocol.SessionEvent{Type: "user/message", Seq: int64(2*i + 1), Data: json.RawMessage(
				fmt.Sprintf(`{"role":"user","content":[{"type":"text","text":"msg-%02d"}]}`, i))}},
			protocol.HistoryEntry{Event: protocol.SessionEvent{Type: "turn/end", Seq: int64(2*i + 2), Data: json.RawMessage(`{}`)}},
		)
	}
	fh.subagents = []protocol.SubagentListEntry{{
		Kind: "child", Id: "c1", Mode: "continuable", Activity: "running",
		Label: "probe child",
	}}
	fh.histories["c1"] = events
	// The host roster carries the session: the async boot baseline's
	// session.list must not clobber the store's session row.
	fh.sessions = []protocol.SessionSummary{{SessionId: "s1", Cwd: "/tmp/x"}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a := app.New(fh.URL)
	a.Start(ctx)
	t.Setenv("DSH_CLI_HOME", t.TempDir())
	m := NewModel(a)
	m.W, m.H = 120, 30
	m.st.SetSessions([]protocol.SessionSummary{{SessionId: "s1", Cwd: "/tmp/x"}})
	m.st.SetActive("s1")
	// Transcript content behind the window: the wheel must not move it
	// while the modal is open.
	var main []protocol.HistoryEntry
	for i := 0; i < 30; i++ {
		main = append(main, protocol.HistoryEntry{Event: protocol.SessionEvent{Type: "assistant/message", Seq: int64(i + 1), Data: json.RawMessage(
			fmt.Sprintf(`{"turn":1,"step":%d,"message":{"role":"assistant","content":[{"type":"text","text":"main-%02d"}]}}`, i+1, i))}})
	}
	m.st.LoadTail("s1", &protocol.HistoryResponse{Events: main})

	m.dockVisible = true
	m.dockTab = dockSubs
	var cmd tea.Cmd
	upd := func(msg tea.Msg) {
		nv, c := m.Update(msg)
		m = nv.(*Model)
		cmd = c
	}
	if c := m.subDockRefresh(); c != nil {
		upd(c()) // the child catalog lands before the window can open
	}
	upd(tea.KeyMsg{Type: tea.KeyEnter}) // opens the window, schedules the page fetch
	if cmd == nil {
		t.Fatal("history fetch not scheduled on open")
	}
	upd(cmd())
	sm, ok := m.topModal().(*subagentModal)
	if !ok || sm.loading || len(sm.lines) != 60 {
		t.Fatalf("window state = %T loading=%v lines=%d, want the 60-line page", m.topModal(), sm.loading, len(sm.lines))
	}

	// shown returns the msg lines visible in the window, in order.
	shown := func() []string {
		out := stripANSI(m.View())
		var s []string
		for i := 0; i < 30; i++ {
			if mark := fmt.Sprintf("msg-%02d", i); strings.Contains(out, mark) {
				s = append(s, mark)
			}
		}
		return s
	}
	first := func(s []string) string {
		if len(s) == 0 {
			t.Fatal("window shows no lines")
		}
		return s[0]
	}

	// 1) The window opens on the first lines: the page head is visible,
	//    the page tail is not (16 rows cannot hold 60 lines).
	if s := shown(); first(s) != "msg-00" || strings.Contains(strings.Join(s, " "), "msg-29") {
		t.Fatalf("initial window = %v, want the page head only", s)
	}

	// 2) Down arrows walk one line at a time.
	upd(tea.KeyMsg{Type: tea.KeyDown})
	upd(tea.KeyMsg{Type: tea.KeyDown})
	if s := shown(); first(s) != "msg-01" {
		t.Fatalf("after two downs first = %s (window = %v), want msg-01", first(s), s)
	}

	// 3) Page down moves a full window (vis-1 lines); page up walks back.
	upd(tea.KeyMsg{Type: tea.KeyPgDown})
	if s := shown(); first(s) != "msg-09" {
		t.Fatalf("after pgdown first = %s (window = %v), want msg-09", first(s), s)
	}
	upd(tea.KeyMsg{Type: tea.KeyPgUp})
	if s := shown(); first(s) != "msg-01" {
		t.Fatalf("after pgup first = %s, want msg-01 (the pre-pgdown seat)", first(s))
	}

	// 4) End jumps to the page tail, home back to the head.
	upd(tea.KeyMsg{Type: tea.KeyEnd})
	if s := shown(); !strings.Contains(strings.Join(s, " "), "msg-29") || first(s) != "msg-22" {
		t.Fatalf("after end window = %v, want the page tail (msg-29 visible)", s)
	}
	upd(tea.KeyMsg{Type: tea.KeyHome})
	if s := shown(); first(s) != "msg-00" {
		t.Fatalf("after home first = %s, want msg-00", first(s))
	}

	// 5) The wheel scrolls the window, not the transcript behind it.
	scBefore := m.scroll
	upd(tea.MouseMsg{Button: tea.MouseButtonWheelDown, X: 10, Y: 10})
	if m.scroll != scBefore {
		t.Fatalf("wheel with the window open moved the transcript: %d -> %d", scBefore, m.scroll)
	}
	if s := shown(); first(s) != "msg-02" {
		t.Fatalf("after wheel down first = %s (window = %v), want msg-02", first(s), s)
	}
	for i := 0; i < 3; i++ {
		upd(tea.MouseMsg{Button: tea.MouseButtonWheelUp, X: 10, Y: 10})
	}
	if s := shown(); first(s) != "msg-00" {
		t.Fatalf("wheel up past the head: first = %s, want msg-00 (clamped)", first(s))
	}

	// 6) Closed, the wheel belongs to the transcript again.
	upd(tea.KeyMsg{Type: tea.KeyEsc})
	if m.topModal() != nil {
		t.Fatal("esc left the window open")
	}
	upd(tea.MouseMsg{Button: tea.MouseButtonWheelDown, X: 10, Y: 10})
	if m.scroll == scBefore {
		t.Fatalf("wheel with no modal left the transcript at %d", m.scroll)
	}
}

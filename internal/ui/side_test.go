package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	"dsh-cli/internal/app"
	"dsh-cli/internal/protocol"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// checkCardWindow pins the card-window contract on a rendered window:
// every line spans the full width and every cell carries a background —
// the card surface on the frame, the title rail, the rows, the footer
// and the padding, the accent band on the selected row, the field's own
// surface on the live search line — so nothing of the underlying screen
// shows through the window (the old floating look left every blank cell
// bare).
func checkCardWindow(t *testing.T, label string, lines []string, w, h int) {
	t.Helper()
	if len(lines) != h {
		t.Fatalf("%s: window emitted %d lines, want %d", label, len(lines), h)
	}
	for i, ln := range lines {
		if pw := plainWidth(ln); pw != w {
			t.Fatalf("%s: line %d visible width %d, want %d: %q", label, i, pw, w, stripANSI(ln))
		}
		for _, c := range scanStyled(ln) {
			if len(c.state.bg) == 0 {
				t.Fatalf("%s: line %d col %d: transparent cell (want the card surface)", label, i, c.col)
			}
		}
	}
}

// TestSessionListCardSurface checks that the open session window is a
// solid card slab: the frame, title, rows, hint and the empty tail all
// carry the card surface — no cell of the window is transparent — and
// the lines span the full column width.
func TestSessionListCardSurface(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Setenv("NO_COLOR", "")
	t.Setenv("COLORFGBG", "15;0")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a := app.New("http://127.0.0.1:3080")
	a.Start(ctx)
	m := NewModel(a)
	if m.th.CCardBG == "" {
		t.Skip("theme has no card surface in this profile")
	}
	m.st.SetSessions([]protocol.SessionSummary{
		{SessionId: "s1", Running: true, Cwd: "/tmp/dir"},
		{SessionId: "s2", Blank: true},
	})
	m.st.SetActive("s1")

	checkCardWindow(t, "list", strings.Split(m.sideView(30, 12), "\n"), 30, 12)
}

// TestSideSearchFilter checks the session list's search line: an empty
// query renders the whole roster with the placeholder, a live query
// narrows the rendered rows to the matches (title / id / cwd) and shows
// the match count.
func TestSideSearchFilter(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Setenv("NO_COLOR", "")
	t.Setenv("COLORFGBG", "15;0")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a := app.New("http://127.0.0.1:3080")
	a.Start(ctx)
	m := NewModel(a)
	m.st.SetSessions([]protocol.SessionSummary{
		{SessionId: "s1", Cwd: "/tmp/alpha"},
		{SessionId: "s2", Cwd: "/tmp/beta"},
		{SessionId: "s3", Cwd: "/tmp/review"},
	})
	m.st.SetTitle("s1", "alpha build")
	m.st.SetTitle("s2", "beta deploy")
	m.st.SetTitle("s3", "alpha review")
	m.W, m.H = 120, 40

	v := m.sideView(30, 14)
	plain := stripANSI(v)
	for _, want := range []string{"alpha build", "beta deploy", "alpha review", "search"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("unfiltered list missing %q:\n%s", want, plain)
		}
	}

	m.sideSearch.val = []rune("alpha")
	v = m.sideView(30, 14)
	plain = stripANSI(v)
	if !strings.Contains(plain, "alpha build") || !strings.Contains(plain, "alpha review") {
		t.Fatalf("query alpha: matches missing:\n%s", plain)
	}
	if strings.Contains(plain, "beta deploy") {
		t.Fatalf("query alpha: non-match still rendered:\n%s", plain)
	}
	if !strings.Contains(plain, "Sessions 2") {
		t.Fatalf("query alpha: match count missing:\n%s", plain)
	}
	// A query that hits nothing renders no session rows at all.
	m.sideSearch.val = []rune("zzz")
	plain = stripANSI(m.sideView(30, 14))
	if strings.Contains(plain, "build") || strings.Contains(plain, "deploy") || strings.Contains(plain, "review") {
		t.Fatalf("query zzz: rows leaked through:\n%s", plain)
	}
}

// TestSideArrowSelection pins the session window cursor: with the window
// open, up/down move the cursor along the roster (the search filter narrows
// the walk) without switching the active session, and with the window closed
// the arrows keep the old transcript-scroll duty.
func TestSideArrowSelection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a := app.New("http://127.0.0.1:3999")
	a.Start(ctx)
	m := NewModel(a)
	m.splashOff = true
	m.st.SetSessions([]protocol.SessionSummary{
		{SessionId: "s1"}, {SessionId: "s2"}, {SessionId: "s3"},
	})
	m.st.SetActive("s1")
	m.sideVisible = true
	m.sideCursorID = m.activeID()

	// The cursor starts on the active session.
	if m.sideCursorID != "s1" {
		t.Fatalf("cursor init: %q, want s1", m.sideCursorID)
	}
	// Down moves the cursor; the active session does not change (a move,
	// not a pick).
	m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.sideCursorID != "s2" {
		t.Fatalf("down with open window: cursor %q, want s2", m.sideCursorID)
	}
	if m.activeID() != "s1" {
		t.Fatalf("down must not switch the active session: %q", m.activeID())
	}
	// Up walks back; a further up clamps at the top.
	m.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	m.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	if m.sideCursorID != "s1" {
		t.Fatalf("up x2: cursor %q, want s1 (clamped)", m.sideCursorID)
	}
	// The filter narrows the cursor's range: only s3 matches "s3".
	m.sideSearch.val = []rune("s3")
	m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.sideCursorID != "s3" {
		t.Fatalf("down with filter s3: cursor %q, want s3", m.sideCursorID)
	}
	// Closed window: the arrows go back to the transcript scroll.
	m.sideSearch.val = nil
	m.sideVisible = false
	m.scroll = 0
	m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.activeID() != "s1" {
		t.Fatalf("down with closed window switched sessions: %q", m.activeID())
	}
}

// TestSessionWindowSpaceSelect pins the session window's two-stage
// selection: the arrows move the cursor, space commits the cursor row as
// the active session (and the window stays open), and enter confirms
// (open the cursor row and close).
func TestSessionWindowSpaceSelect(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a := app.New("http://127.0.0.1:3999")
	a.Start(ctx)
	m := NewModel(a)
	m.splashOff = true
	m.st.SetSessions([]protocol.SessionSummary{
		{SessionId: "s1"}, {SessionId: "s2"}, {SessionId: "s3"},
	})
	m.st.SetActive("s1")
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	if !m.sideVisible {
		t.Fatal("ctrl+s must open the session window")
	}
	if m.sideCursorID != "s1" {
		t.Fatalf("cursor must start on the active session: %q", m.sideCursorID)
	}
	// Space on the active row is a no-op (the cursor already is the pick):
	// nothing changes and the window stays open.
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	if m.activeID() != "s1" || m.sideCursorID != "s1" {
		t.Fatalf("space on the active row must be a no-op: active %q cursor %q",
			m.activeID(), m.sideCursorID)
	}
	if !m.sideVisible {
		t.Fatal("space must keep the window open")
	}
	// Down, down: the cursor walks to s3; the active session stays s1.
	m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.sideCursorID != "s3" {
		t.Fatalf("down x2: cursor %q, want s3", m.sideCursorID)
	}
	if m.activeID() != "s1" {
		t.Fatalf("down must not switch the active session: %q", m.activeID())
	}
	// Space selects s3: it becomes the active session, the cursor keeps its
	// row, and the window stays open.
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	if m.activeID() != "s3" {
		t.Fatalf("space: active %q, want s3", m.activeID())
	}
	if m.sideCursorID != "s3" {
		t.Fatalf("space: cursor %q, want s3 (kept)", m.sideCursorID)
	}
	if !m.sideVisible {
		t.Fatal("space must keep the window open")
	}
	// Up moves the cursor to s2; the active session stays s3.
	m.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	if m.sideCursorID != "s2" {
		t.Fatalf("up: cursor %q, want s2", m.sideCursorID)
	}
	if m.activeID() != "s3" {
		t.Fatalf("up must not switch the active session: %q", m.activeID())
	}
	// Enter confirms the cursor row (s2) and closes the window.
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.sideVisible {
		t.Fatal("enter must close the session window")
	}
	if m.activeID() != "s2" {
		t.Fatalf("enter: active %q, want s2", m.activeID())
	}
}

// TestSessionWindowKeys wires the session window end to end: ctrl+s
// opens the centered popup (and, pressed again inside, closes it),
// which then owns the keyboard (typing
// lands in its search line, global chords don't leak through), down
// walks the filtered results, enter picks and closes, esc clears the
// query then closes, and closing hands the keys back to the main input
// bar.
func TestSessionWindowKeys(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a := app.New("http://127.0.0.1:3999")
	a.Start(ctx)
	m := NewModel(a)
	m.splashOff = true
	m.st.SetSessions([]protocol.SessionSummary{
		{SessionId: "s1", Cwd: "/tmp/alpha"},
		{SessionId: "s2", Cwd: "/tmp/beta"},
	})
	m.st.SetActive("s1")

	// Ctrl+s opens the session window; while it is open its search line
	// owns the keys (typing lands there, not in the main input).
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	if !m.sideVisible {
		t.Fatal("ctrl+s must open the session window")
	}
	typeInWindow := func(s string) {
		for _, r := range s {
			m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
	}
	typeInWindow("b")
	if got := m.sideSearch.string(); got != "b" {
		t.Fatalf("typed into search: %q, want %q", got, "b")
	}
	if got := m.inp.value(); got != "" {
		t.Fatalf("typed into the main input instead: %q", got)
	}
	// A global chord must not leak through the open window: ctrl+b is a
	// cursor edit for the search line (lineEdit), not the dock toggle;
	// the dock stays closed and no help modal opens.
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlB})
	if m.dockVisible {
		t.Fatal("ctrl+b leaked to the dock toggle")
	}
	// (ctrl+b moved the caret left in the search line; return it before
	// the next character so the assertion stays position-independent.)
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlF})
	if len(m.mods) != 0 {
		t.Fatalf("a chord leaked into a modal: %d modals", len(m.mods))
	}
	for _, r := range "a" {
		m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if got := m.sideSearch.string(); got != "ba" {
		t.Fatalf("text keys must keep editing the search line: %q", got)
	}
	if got := m.inp.value(); got != "" {
		t.Fatalf("window keys landed in the main input: %q", got)
	}
	m.sideSearch.reset()

	// A global chord must not act while the window is open: ctrl+z (the
	// model picker's opener) is absorbed by the window, which stays open.
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlZ})
	if !m.sideVisible {
		t.Fatal("ctrl+z must not act while the window is open")
	}
	typeInWindow("b")
	// Down moves the cursor over the filtered result; the active session is
	// not among the matches, so it stays s1 until the pick is confirmed.
	m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.sideCursorID != "s2" {
		t.Fatalf("down in the window: cursor %q, want s2", m.sideCursorID)
	}
	if m.activeID() != "s1" {
		t.Fatalf("down must not switch the active session: %q", m.activeID())
	}
	// ← and → move the cursor the same way inside the window (a single
	// match clamps in place).
	m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	if m.sideCursorID != "s2" {
		t.Fatalf("right in the window: cursor %q (filtered list must stay in range)", m.sideCursorID)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	if m.sideCursorID != "s2" {
		t.Fatalf("left in the window: cursor %q (single match must clamp)", m.sideCursorID)
	}
	// Enter confirms the cursor row (s2) and closes; the keys hand back to
	// the main input bar.
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.sideVisible {
		t.Fatal("enter must close the session window")
	}
	if m.activeID() != "s2" {
		t.Fatalf("enter: active %q, want s2", m.activeID())
	}
	if got := m.sideSearch.string(); got != "b" {
		t.Fatalf("closing must keep the query for the next open: %q", got)
	}
	for _, r := range "hi" {
		m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if got := m.inp.value(); got != "hi" {
		t.Fatalf("keys after the window closed must reach the main input: %q", got)
	}
	m.inp.clear()

	// Ctrl+s reopens (the query persists); esc clears the query, a
	// second esc closes the window.
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	if !m.sideVisible {
		t.Fatal("ctrl+s must reopen the window")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if got := m.sideSearch.string(); got != "" {
		t.Fatalf("esc must clear the query: %q", got)
	}
	if !m.sideVisible {
		t.Fatal("first esc keeps the window open (second one closes)")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.sideVisible {
		t.Fatal("second esc must close the window")
	}

	// Ctrl+s opens the window; pressing it again inside closes (toggle),
	// and the next press reopens.
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	if !m.sideVisible {
		t.Fatal("ctrl+s must open the window")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	if m.sideVisible {
		t.Fatal("ctrl+s inside the window must close it")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	if !m.sideVisible {
		t.Fatal("ctrl+s must reopen the window")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	if m.sideVisible {
		t.Fatal("second ctrl+s must close the window")
	}
	if got := m.sideSearch.string(); got != "" {
		t.Fatalf("cycling must keep an empty query: %q", got)
	}
	if got := m.inp.value(); got != "" {
		t.Fatalf("main input must be untouched by the window: %q", got)
	}
}

// TestSessionWindowCardSurface pins the opaque-card rule on the whole
// window: the hairline border, the gradient title rail, the rows, the
// footer and the padding below them all carry the card surface — no
// cell of the window is transparent. The intentional surfaces stay
// raised: the selected row's accent band and, once a query is typed, the
// live search field's card band (with its inverted caret cell).
func TestSessionWindowCardSurface(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Setenv("NO_COLOR", "")
	t.Setenv("COLORFGBG", "15;0")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a := app.New("http://127.0.0.1:3080")
	a.Start(ctx)
	m := NewModel(a)
	if m.th.CCardBG == "" {
		t.Skip("theme has no card surface in this profile")
	}
	m.st.SetSessions([]protocol.SessionSummary{
		{SessionId: "s1", Cwd: "/tmp/alpha"},
		{SessionId: "s2", Cwd: "/tmp/beta"},
	})
	m.st.SetActive("s1")

	const w, h = 30, 12
	check := func(query, label string) {
		m.sideSearch.val = []rune(query)
		m.sideSearch.cur = len(query)
		checkCardWindow(t, label, strings.Split(m.sideView(w, h), "\n"), w, h)
	}
	check("", "empty")
	check("al", "query+caret")
}

// TestSessionWindowWorkspaceScope pins the session window's scope: it
// lists only the active session's workspace (the group header plus its
// rows); another workspace's sessions stay out — the arrow walk included
// — and picking across keeps re-scoping the window.
func TestSessionWindowWorkspaceScope(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Setenv("NO_COLOR", "")
	t.Setenv("COLORFGBG", "15;0")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a := app.New("http://127.0.0.1:3999")
	a.Start(ctx)
	m := NewModel(a)
	m.st.SetWorkspaces([]protocol.WorkspaceView{
		{WorkspaceId: "w1", Path: "/tmp/a", Title: "alpha", SessionIds: []string{"s1", "s2"}},
		{WorkspaceId: "w2", Path: "/tmp/b", Title: "beta", SessionIds: []string{"s3"}},
	}, nil)
	m.st.SetSessions([]protocol.SessionSummary{
		{SessionId: "s1", Cwd: "/tmp/a"},
		{SessionId: "s2", Cwd: "/tmp/a"},
		{SessionId: "s3", Cwd: "/tmp/b"},
	})
	m.st.SetTitle("s1", "alpha one")
	m.st.SetTitle("s2", "alpha two")
	m.st.SetTitle("s3", "beta one")
	m.W, m.H = 120, 40

	// Active on s2: the window shows the alpha workspace only.
	m.st.SetActive("s2")
	plain := stripANSI(m.sideView(30, 14))
	if !strings.Contains(plain, "alpha one") || !strings.Contains(plain, "alpha two") {
		t.Fatalf("scoped window missing the current workspace's sessions:\n%s", plain)
	}
	if !strings.Contains(plain, "/tmp/a") {
		t.Fatalf("scoped window missing the workspace header:\n%s", plain)
	}
	if strings.Contains(plain, "beta one") {
		t.Fatalf("scoped window leaked another workspace's session:\n%s", plain)
	}
	if !strings.Contains(plain, "Sessions 2") {
		t.Fatalf("scoped window count wrong:\n%s", plain)
	}

	// The arrow walk stays in scope: from s2 the left arrow lands on s1,
	// back right lands on s2 (w2's s3 is out of the window). The keys go
	// through the global chord (input empty, dock closed), not the window.
	m.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	if m.st.Active() != "s1" {
		t.Fatalf("scoped walk: active = %q, want s1", m.st.Active())
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	if m.st.Active() != "s2" {
		t.Fatalf("scoped walk: active = %q, want s2", m.st.Active())
	}

	// Moving into the other workspace re-scopes the window.
	m.st.SetActive("s3")
	plain = stripANSI(m.sideView(30, 14))
	if strings.Contains(plain, "alpha one") || !strings.Contains(plain, "beta one") {
		t.Fatalf("window did not re-scope to the new workspace:\n%s", plain)
	}
}

// TestSessionWindowScopeFallback pins the scoping fallback: an active
// session no workspace accounts (here: no workspace registered) keeps
// the full flat roster in the window.
func TestSessionWindowScopeFallback(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Setenv("NO_COLOR", "")
	t.Setenv("COLORFGBG", "15;0")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a := app.New("http://127.0.0.1:3999")
	a.Start(ctx)
	m := NewModel(a)
	m.st.SetSessions([]protocol.SessionSummary{
		{SessionId: "s1", Cwd: "/tmp/a"},
		{SessionId: "s2", Cwd: "/tmp/b"},
	})
	m.st.SetTitle("s1", "loose one")
	m.st.SetTitle("s2", "loose two")
	m.W, m.H = 120, 40

	m.st.SetActive("s1")
	plain := stripANSI(m.sideView(30, 14))
	if !strings.Contains(plain, "loose one") || !strings.Contains(plain, "loose two") {
		t.Fatalf("fallback window should keep the full roster:\n%s", plain)
	}
	if !strings.Contains(plain, "Sessions 2") {
		t.Fatalf("fallback window count wrong:\n%s", plain)
	}
}

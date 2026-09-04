// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

// TestInputCurInvariantStress replays realistic multi-line editing
// sessions (shift+enter newlines, mouse press/drag, caret walks,
// pastes, slash menu, history) plus a key fuzz, asserting the caret
// invariant 0 <= cur <= len(val) after every event. The panic behind
// "slice bounds out of range [3385:3053]" in delBack is a caret that
// outran the buffer; this test hunts the event that lets it.
package ui

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"dsh-cli/internal/app"
	"dsh-cli/internal/protocol"

	tea "github.com/charmbracelet/bubbletea"
)

var traceBuf []string

func trace(what string) {
	traceBuf = append(traceBuf, what)
	if len(traceBuf) > 40 {
		traceBuf = traceBuf[len(traceBuf)-40:]
	}
}

func checkInp(t *testing.T, m *Model, what string) {
	t.Helper()
	trace(what)
	if m.inp.cur < 0 || m.inp.cur > len(m.inp.val) {
		t.Fatalf("%s: caret out of range: cur=%d len=%d val=%q trace=%v",
			what, m.inp.cur, len(m.inp.val), string(m.inp.val), traceBuf)
	}
	if a := m.inp.sel.anchor; m.inp.sel.on && (a < 0 || a > len(m.inp.val)) {
		t.Fatalf("%s: pick anchor out of range: anchor=%d len=%d val=%q trace=%v",
			what, a, len(m.inp.val), string(m.inp.val), traceBuf)
	}
}

func typeStr(t *testing.T, m *Model, s string) {
	for _, r := range s {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		checkInp(t, m, "type "+string(r))
	}
}

func shiftEnter(t *testing.T, m *Model) {
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	checkInp(t, m, "shift+enter")
}

func TestInputCurInvariantStress(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a := app.New("http://127.0.0.1:3999")
	a.Start(ctx)
	m := NewModel(a)
	m.W, m.H = 120, 40

	// The bar is framed: text rows start one row below the frame origin.
	barY := func(row int) int { m.View(); return m.inpY + 1 + row }

	// 1) The user's session: multi-line build-up with shift+enter.
	typeStr(t, m, "hello world X")
	shiftEnter(t, m)
	typeStr(t, m, "second line here")
	shiftEnter(t, m)
	typeStr(t, m, "third line")
	// 2) Backspaces across the newlines.
	for i := 0; i < 12; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		checkInp(t, m, "backspace")
	}
	// 3) Caret walks on every axis.
	for _, k := range []tea.KeyMsg{
		{Type: tea.KeyLeft}, {Type: tea.KeyLeft}, {Type: tea.KeyRight},
		{Type: tea.KeyHome}, {Type: tea.KeyEnd},
		{Type: tea.KeyCtrlA}, {Type: tea.KeyCtrlE},
		{Type: tea.KeyUp}, {Type: tea.KeyDown},
		{Type: tea.KeyShiftLeft}, {Type: tea.KeyShiftRight},
		{Type: tea.KeyShiftHome}, {Type: tea.KeyShiftEnd},
		{Type: tea.KeyShiftUp}, {Type: tea.KeyShiftDown},
	} {
		m.Update(k)
		checkInp(t, m, k.String())
	}
	// 4) Mouse press + drag inside the bar (rows 0..1).
	m.View()
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 10, Y: barY(0)})
	checkInp(t, m, "mouse press row0")
	m.Update(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: 60, Y: barY(1)})
	checkInp(t, m, "mouse drag row1")
	m.Update(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: 60, Y: barY(1)})
	checkInp(t, m, "mouse release")
	// 5) Delete to line end, then more edits.
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	checkInp(t, m, "ctrl+u")
	typeStr(t, m, "refill")
	shiftEnter(t, m)
	typeStr(t, m, "tail")
	// 6) Paste with newlines and a trailing backslash line.
	m.Update(pasteTextMsg{text: "p1\\\np2\n"})
	checkInp(t, m, "paste")
	// 7) Slash menu open/complete/backspace.
	m.inp.clear()
	typeStr(t, m, "/")
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	checkInp(t, m, "slash tab")
	m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	checkInp(t, m, "slash backspace")
	// 8) Prompt history.
	m.inp.clear()
	typeStr(t, m, "hist probe")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	checkInp(t, m, "send")
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	checkInp(t, m, "hist up")
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	checkInp(t, m, "hist down")
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	checkInp(t, m, "hist down2")
	// 9) Fuzz: 2000 random keys over the same surface.
	m.inp.clear()
	rng := rand.New(rand.NewSource(42))
	keys := func() tea.KeyMsg {
		switch rng.Intn(14) {
		case 0:
			return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'\\'}}
		case 1:
			return tea.KeyMsg{Type: tea.KeyEnter}
		case 2:
			return tea.KeyMsg{Type: tea.KeyBackspace}
		case 3:
			return tea.KeyMsg{Type: tea.KeyLeft}
		case 4:
			return tea.KeyMsg{Type: tea.KeyRight}
		case 5:
			return tea.KeyMsg{Type: tea.KeyUp}
		case 6:
			return tea.KeyMsg{Type: tea.KeyDown}
		case 7:
			return tea.KeyMsg{Type: tea.KeyShiftUp}
		case 8:
			return tea.KeyMsg{Type: tea.KeyShiftDown}
		case 9:
			return tea.KeyMsg{Type: tea.KeyCtrlA}
		case 10:
			return tea.KeyMsg{Type: tea.KeyCtrlE}
		case 11:
			return tea.KeyMsg{Type: tea.KeyCtrlU}
		case 12:
			return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{rune("abcdefghXYZ ."[rng.Intn(13)])}}
		default:
			return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'\\'}}
		}
	}
	// Hermetic port (like the sibling bar tests): no live server, so no
	// session ever activates and the empty-input chords toast instead of
	// opening pickers mid-fuzz.
	for i := 0; i < 2000; i++ {
		km := keys()
		trace(km.String())
		m.Update(km)
		checkInp(t, m, "fuzz")
	}
}

// TestHistBrowsePickDesync is the regression for the multi-line backspace
// panic ("slice bounds out of range [3385:3053]"): a pick armed at the
// tail of a long buffer, then Up replaces the buffer with a shorter
// history entry. histUp/histDown used to keep the pick live, leaving the
// anchor past the end of the new buffer; the next backspace sliced
// through it. The draft restore (Down at the newest) is covered too.
func TestHistBrowsePickDesync(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a := app.New(newFakeHost(t).URL)
	a.Start(ctx)
	m := NewModel(a)
	m.W, m.H = 120, 40
	st := a.Store()
	st.SetSessions([]protocol.SessionSummary{{SessionId: "s1", Cwd: "/tmp"}})
	st.SetActive("s1")

	// Seed the prompt history with a short entry, then build a longer
	// buffer and arm a pick at its tail.
	typeStr(t, m, "short")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	typeStr(t, m, "abcdefgh")
	m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m.Update(tea.KeyMsg{Type: tea.KeyShiftRight})
	checkInp(t, m, "pick armed")
	if !m.inp.selActive() {
		t.Fatal("pick should be active before the browse")
	}
	// Up at the top edge: single line, so the arrow goes to history.
	// The buffer is replaced by "short"; the stale pick must die with it.
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	checkInp(t, m, "hist up")
	// Down at the newest steps back out of the browse and restores the
	// saved draft (caret included).
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	checkInp(t, m, "hist down draft")
	if got := string(m.inp.val); got != "abcdefgh" {
		t.Fatalf("draft restore: got %q want %q", got, "abcdefgh")
	}
	if m.inp.cur != len(m.inp.val) {
		t.Fatalf("draft restore caret: cur=%d len=%d", m.inp.cur, len(m.inp.val))
	}
	// Re-enter the browse, then the backspace that used to panic: with a
	// stale anchor the insert/delete would slice past the buffer's end.
	// (The edit detaches the browse, per the history contract.)
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	checkInp(t, m, "hist up 2")
	m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	checkInp(t, m, "backspace after browse")
	if got := string(m.inp.val); got != "shor" {
		t.Fatalf("backspace after browse: got %q want %q", got, "shor")
	}
}

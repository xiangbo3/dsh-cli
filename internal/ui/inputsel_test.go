// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"encoding/base64"
	"strings"
	"testing"

	"dsh-cli/internal/app"

	"github.com/charmbracelet/bubbletea"
	"github.com/muesli/termenv"
)

// inputPickModel builds a headless model at 100x30 with the given bar
// text; the input bar's origin is measured by the first View call.
func inputPickModel(t *testing.T, text string) *Model {
	t.Helper()
	m := NewModel(app.New("http://127.0.0.1:3999"))
	m.splashOff = true
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.inp.val = []rune(text)
	m.inp.cur = 0
	m.View()
	if m.inpY < 0 {
		t.Fatalf("input bar origin not published after View (inpY=%d)", m.inpY)
	}
	return m
}

// TestInputLineSelKeys pins the main input bar's keyboard pick:
// shift+moves extend, plain moves clear, edits replace, esc/copy follow.
func TestInputLineSelKeys(t *testing.T) {
	in := newInputLine()
	in.val = []rune("hello world\nsecond line")
	// Shift+right three times picks the first three runes.
	for i := 0; i < 3; i++ {
		ok, _ := in.handleKey(keyModel(t), tea.KeyMsg{Type: tea.KeyShiftRight})
		if !ok {
			t.Fatal("shift+right must be consumed")
		}
	}
	if !in.selActive() || in.selText() != "hel" || in.cur != 3 {
		t.Fatalf("shift+right pick: active=%v text=%q cur=%d", in.selActive(), in.selText(), in.cur)
	}
	// A plain move clears the pick and keeps the caret.
	in.handleKey(keyModel(t), tea.KeyMsg{Type: tea.KeyRight})
	if in.selActive() {
		t.Fatalf("plain move must clear the pick")
	}
	if in.cur != 4 {
		t.Fatalf("caret %d, want 4", in.cur)
	}
	// Shift+home extends back to the line start (a fresh pick re-anchors
	// at the caret: the range is caret..line-start).
	in.handleKey(keyModel(t), tea.KeyMsg{Type: tea.KeyShiftHome})
	if !in.selActive() || in.selText() != "hell" {
		t.Fatalf("shift+home pick: active=%v text=%q", in.selActive(), in.selText())
	}
	// Shift+right from the first line's end steps past the newline into
	// the second line (rune-level, like the plain moves): a fresh pick
	// re-anchors at the caret each time a plain reset happens.
	in.sel.reset()
	in.cur = 10                                                    // 'd' of "world"
	in.handleKey(keyModel(t), tea.KeyMsg{Type: tea.KeyShiftRight}) // 'r'
	in.handleKey(keyModel(t), tea.KeyMsg{Type: tea.KeyShiftRight}) // 'l'
	in.handleKey(keyModel(t), tea.KeyMsg{Type: tea.KeyShiftRight}) // 'd'
	in.handleKey(keyModel(t), tea.KeyMsg{Type: tea.KeyShiftRight}) // '\n'
	in.handleKey(keyModel(t), tea.KeyMsg{Type: tea.KeyShiftRight}) // 's'
	if !in.selActive() || in.selText() != "d\nsec" {
		t.Fatalf("cross-line pick: %q", in.selText())
	}
	// Shift+up extends to the same column on the previous line (the
	// fresh pick re-anchors at the caret before the move).
	in.sel.reset()
	in.cur = 15 // 'o' of "second"
	in.handleKey(keyModel(t), tea.KeyMsg{Type: tea.KeyShiftUp})
	if !in.selActive() || in.cur != 3 || in.selText() != "lo world\nsec" {
		t.Fatalf("shift+up: active=%v text=%q cur=%d", in.selActive(), in.selText(), in.cur)
	}
	// Backspace over an active pick deletes the whole range.
	in.val = []rune("abcdef")
	in.cur = 0
	in.selSetAll()
	in.sel.on = true
	in.cur = 3
	in.delBack()
	if string(in.val) != "def" || in.cur != 0 || in.selActive() {
		t.Fatalf("backspace over pick: val=%q cur=%d", string(in.val), in.cur)
	}
	// A keystroke over a pick replaces it.
	in.val = []rune("12345")
	in.cur = 5
	in.selSetAll()
	in.insertRune('x')
	if string(in.val) != "x" || in.cur != 1 {
		t.Fatalf("insert over select-all: val=%q cur=%d", string(in.val), in.cur)
	}
	// Double-click picks the logical line under the caret.
	in.val = []rune("one\ntwo three\nfour")
	in.cur = 6 // inside the second line
	in.mouseSelectLine()
	if !in.selActive() || in.selText() != "two three" {
		t.Fatalf("double-click line pick: %q", in.selText())
	}
}

// TestLineEditSelAndPaste pins the modal-field pick + paste: the same
// semantics as the main bar on one line, newlines folded to spaces.
func TestLineEditSelAndPaste(t *testing.T) {
	e := lineEdit{val: []rune("hello world"), cur: 0}
	for i := 0; i < 3; i++ {
		if !e.handleKey(tea.KeyMsg{Type: tea.KeyShiftRight}) {
			t.Fatal("shift+right must be consumed by the field")
		}
	}
	if !e.selActive() || e.selText() != "hel" {
		t.Fatalf("shift+right pick: active=%v text=%q", e.selActive(), e.selText())
	}
	// Shift+end extends to the line end.
	e.handleKey(tea.KeyMsg{Type: tea.KeyShiftEnd})
	if !e.selActive() || e.selText() != "hello world" {
		t.Fatalf("shift+end pick: %q", e.selText())
	}
	// Paste replaces the active pick; newlines become spaces.
	if !e.paste("a\nb") {
		t.Fatal("paste must insert")
	}
	if e.string() != "a b" || e.cur != 3 {
		t.Fatalf("paste over pick: val=%q cur=%d", e.string(), e.cur)
	}
	// Paste without a pick inserts at the caret.
	e.cur = 1
	e.paste("XY")
	if e.string() != "aXY b" || e.cur != 3 {
		t.Fatalf("paste at caret: val=%q cur=%d", e.string(), e.cur)
	}
	// An empty paste is dropped.
	if e.paste("") {
		t.Fatal("empty paste must not insert")
	}
	// mousePress / mouseDrag: a drag keeps its range after release.
	e.val = []rune("abcd")
	e.cur = 0
	e.mousePress(1)
	e.mouseDrag(3)
	if !e.selActive() || e.selText() != "bc" {
		t.Fatalf("drag pick: %q", e.selText())
	}
	e.mouseRelease()
	if !e.selActive() || e.selText() != "bc" {
		t.Fatalf("release must keep a moved drag: active=%v text=%q", e.selActive(), e.selText())
	}
	// A click that never moves is a plain caret.
	e.mousePress(2)
	if e.selActive() {
		t.Fatal("plain click must not arm a pick")
	}
	if e.cur != 2 {
		t.Fatalf("caret %d, want 2", e.cur)
	}
	// Select-all in a single-line field picks the whole field.
	e.mousePress(0)
	e.mouseSelectLine()
	if !e.selActive() || e.selText() != "abcd" {
		t.Fatalf("select-all: %q", e.selText())
	}
}

// TestRuneIndexAt pins the click-to-rune boundary rule, incl. wide runes.
func TestRuneIndexAt(t *testing.T) {
	if got := runeIndexAt("hello", 0); got != 0 {
		t.Fatalf("col 0: %d", got)
	}
	if got := runeIndexAt("hello", 3); got != 3 {
		t.Fatalf("col 3: %d", got)
	}
	if got := runeIndexAt("hello", 42); got != 5 {
		t.Fatalf("past the end: %d", got)
	}
	// "你" and "好" occupy two cells each; col 0..1 = the first glyph,
	// col 2..3 the second, col 4 the trailing ascii rune.
	row := "你好a"
	wants := map[int]int{0: 0, 1: 1, 2: 1, 3: 2, 4: 2, 5: 3, 42: 3}
	for colx, want := range wants {
		if got := runeIndexAt(row, colx); got != want {
			t.Fatalf("wide row col %d: got %d want %d", colx, got, want)
		}
	}
}

// TestInputIndexAtWrap pins click mapping through the bar's word wrap:
// a row below the fold resolves into the right offset of the bar.
func TestInputIndexAtWrap(t *testing.T) {
	m := NewModel(app.New("http://127.0.0.1:3999"))
	m.splashOff = true
	m.W, m.H = 40, 10
	m.inp.val = []rune(strings.Repeat("abcdefghij", 4)) // 40 runes, wraps
	ind := inputPromptWidth(m.th)
	usable := m.W - 2 - ind
	if usable >= 40 {
		t.Fatalf("window too wide to force a wrap (usable=%d)", usable)
	}
	lines := wrapInputText(string(m.inp.val), usable)
	if len(lines) < 2 {
		t.Fatalf("expected a wrapped bar, got %d row(s)", len(lines))
	}
	midx := runeIndexAt(lines[1].text, len(lines[1].text)/2)
	want := lines[1].start + midx
	// The bar is framed: the text rows start one cell right and one row
	// down from the frame's origin.
	got, ok := m.inp.indexAt(m, 1+ind+midx, 2)
	if !ok {
		t.Fatal("row 1 click outside the bar")
	}
	if got != want {
		t.Fatalf("row 1 col %d: got rune %d want %d", midx, got, want)
	}
	// A click on the bottom border row is outside the text.
	if _, ok := m.inp.indexAt(m, 5, len(lines)+1); ok {
		t.Fatal("row past the bar must be outside")
	}
}

// TestInputMousePickFlow drives the real mouse path through
// Model.Update: a plain click places the caret, a drag picks a range
// (kept after release), ctrl+c schedules the copy, esc clears, and a
// double-click picks the whole line.
func TestInputMousePickFlow(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	m := inputPickModel(t, "hello world")
	m.th.Profile = termenv.TrueColor
	ind := inputPromptWidth(m.th)

	// The bar is framed: text rows start at inpY+1, one cell in from the
	// left border.
	click := func(col int) {
		m.Update(tea.MouseMsg{X: 1 + ind + col, Y: m.inpY + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
		m.Update(tea.MouseMsg{X: 1 + ind + col, Y: m.inpY + 1, Button: tea.MouseButtonNone, Action: tea.MouseActionRelease})
	}
	drag := func(from, to int) {
		m.Update(tea.MouseMsg{X: 1 + ind + from, Y: m.inpY + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
		m.Update(tea.MouseMsg{X: 1 + ind + to, Y: m.inpY + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
		m.Update(tea.MouseMsg{X: 1 + ind + to, Y: m.inpY + 1, Button: tea.MouseButtonNone, Action: tea.MouseActionRelease})
	}

	// A drag: the pick survives the release (copy is explicit).
	drag(2, 7)
	if !m.inp.selActive() || m.inp.selText() != "llo w" || m.inpDrag {
		t.Fatalf("drag: active=%v text=%q drag=%v", m.inp.selActive(), m.inp.selText(), m.inpDrag)
	}
	// ctrl+c claims the copy (a cmd is scheduled by whichever channel
	// applies); the pick stays, esc clears.
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		t.Fatal("ctrl+c over an input pick must schedule the copy")
	}
	if !m.inp.selActive() {
		t.Fatal("copy must keep the pick highlighted")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.inp.selActive() {
		t.Fatal("esc must clear the input pick")
	}
	// A plain click (a different cell after the drag, so the
	// double-click window does not fire): caret, no pick.
	click(5)
	if m.inp.selActive() || m.inp.cur != 5 {
		t.Fatalf("click: active=%v cur=%d", m.inp.selActive(), m.inp.cur)
	}
	// A double-click picks the whole logical line.
	m.Update(tea.MouseMsg{X: 1 + ind + 5, Y: m.inpY + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m.Update(tea.MouseMsg{X: 1 + ind + 5, Y: m.inpY + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m.Update(tea.MouseMsg{X: 1 + ind + 5, Y: m.inpY + 1, Button: tea.MouseButtonNone, Action: tea.MouseActionRelease})
	if !m.inp.selActive() || m.inp.selText() != "hello world" {
		t.Fatalf("double-click: active=%v text=%q", m.inp.selActive(), m.inp.selText())
	}
}

// TestTerminalPasteIntoFields pins the bracketed-paste routing: a
// Paste-flagged KeyMsg lands in the focused field — the topmost modal's
// editor where one is open, else the main bar — replacing any pick.
func TestTerminalPasteIntoFields(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	m := inputPickModel(t, "abc def")

	// Bracketed paste into the main bar: replaces an active pick.
	m.inp.cur = 0
	m.inp.sel.anchor = 0
	m.inp.sel.on = true
	m.inp.cur = 3
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("XY"), Paste: true})
	if m.inp.value() != "XY def" || m.inp.cur != 2 {
		t.Fatalf("bar paste: val=%q cur=%d", m.inp.value(), m.inp.cur)
	}

	// A modal open: the same paste lands in its field, not the bar.
	m.openModal(&renameModal{edit: lineEdit{val: []rune("old"), cur: 0}})
	m.View()
	mod := m.topModal().(*renameModal)
	mod.edit.selSetAll() // the paste replaces the field's content
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("new title"), Paste: true})
	if mod.edit.string() != "new title" {
		t.Fatalf("modal paste: %q", mod.edit.string())
	}
	if m.inp.value() != "XY def" {
		t.Fatalf("bar must keep its text: %q", m.inp.value())
	}
	m.closeModal()

	// Multi-line bracketed paste wraps onto extra rows in the bar.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a\nb"), Paste: true})
	// The caret sat after "XY" (index 2) when the modal closed: the
	// paste lands there, before the remaining " def".
	if m.inp.value() != "XYa\nb def" || m.inp.cur != 5 {
		t.Fatalf("multi-line paste: %q", m.inp.value())
	}
	rows := m.inp.render(m, m.W)
	if len(rows) < 2 {
		t.Fatalf("wrapped bar must render extra rows: %d", len(rows))
	}
}

// TestModalFieldMousePick drives a press+drag on a modal edit field
// through the real mouse path (box geometry from the last frame), and
// the box's opacity over the transcript pick.
func TestModalFieldMousePick(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	m := inputPickModel(t, "")
	m.openModal(&renameModal{edit: lineEdit{val: []rune("abc def"), cur: 0}})
	m.View()
	px, py, _, _, ok := m.plateGeom()
	if !ok {
		t.Fatal("no box geometry with a modal open")
	}
	fields := m.topModal().(mouseEditor).mouseFields(m)
	if len(fields) != 1 {
		t.Fatalf("fields: %+v", fields)
	}
	// The field text starts one cell inside the box frame border plus
	// the two-cell body margin (col() covers the margin + prefix).
	startX := px + 1 + fields[0].col()
	y := py + 2 + fields[0].row
	m.Update(tea.MouseMsg{X: startX + 2, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m.Update(tea.MouseMsg{X: startX + 5, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	m.Update(tea.MouseMsg{X: startX + 5, Y: y, Button: tea.MouseButtonNone, Action: tea.MouseActionRelease})
	mod := m.topModal().(*renameModal)
	if !mod.edit.selActive() || mod.edit.selText() != "c d" {
		t.Fatalf("field drag: active=%v text=%q", mod.edit.selActive(), mod.edit.selText())
	}
	// ctrl+c copies the field pick (the main bar is empty, so the
	// transcript pick's copy path must not win).
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		t.Fatal("ctrl+c over a modal field pick must schedule the copy")
	}
	// A press on the plate off every field row still eats the press:
	// the transcript pick hidden under the plate is dropped and no field
	// pick is armed.
	mod.edit.sel.reset()
	m.sel.active = true
	row := y + 1 // the footer (hint) row
	m.Update(tea.MouseMsg{X: startX, Y: row, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m.Update(tea.MouseMsg{X: startX, Y: row, Button: tea.MouseButtonNone, Action: tea.MouseActionRelease})
	if m.sel.active {
		t.Fatal("press on the box must drop the transcript pick")
	}
	if mod.edit.selActive() {
		t.Fatal("off-field press must not arm a pick")
	}
}

// TestReadClipboardNoTool pins the explicit paste key's missing-reader
// fallback (no cmd, a diagnostic toast).
func TestReadClipboardNoTool(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("XDG_SESSION_TYPE", "")
	t.Setenv("PATH", "/nonexistent")
	m := inputPickModel(t, "")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	if cmd != nil {
		t.Fatal("no clipboard reader: no cmd expected")
	}
	if len(m.toasts) == 0 {
		t.Fatal("expected a toast for the missing reader")
	}
	last := m.toasts[len(m.toasts)-1].text
	if !strings.Contains(last, "no clipboard reader") {
		t.Fatalf("toast: %q", last)
	}
}

// TestOSC52Payload pins the clipboard fallback encoding (base64 on the
// OSC 52 channel) — the payload the copy path emits when no clipboard
// binary is installed.
func TestOSC52Payload(t *testing.T) {
	payload := osc52Payload("hi\n")
	want := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte("hi\n")) + "\a"
	if payload != want {
		t.Fatalf("payload %q want %q", payload, want)
	}
}

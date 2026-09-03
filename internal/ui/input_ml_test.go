package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbletea"
)

// TestInputShiftEnterNewline pins the manual newline: shift+enter (the
// terminal's bare LF, i.e. the ctrl+j byte) breaks the line instead of
// sending; a trailing backslash before enter no longer has that job —
// it stays in the buffer and enter sends; the next enter sends the
// multi-line text with the newline intact.
func TestInputShiftEnterNewline(t *testing.T) {
	m := keyModel(t)
	in := newInputLine()
	for _, r := range "abc" {
		in.insertRune(r)
	}
	ok, send := in.handleKey(m, tea.KeyMsg{Type: tea.KeyCtrlJ})
	if !ok || send {
		t.Fatalf("shift+enter: want consumed, no send; got ok=%v send=%v", ok, send)
	}
	if string(in.val) != "abc\n" || in.cur != 4 {
		t.Fatalf("shift+enter: val=%q cur=%d, want a trailing newline", string(in.val), in.cur)
	}
	// The second enter sends the multi-line text with the newline intact.
	ok, send = in.handleKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !ok || !send {
		t.Fatalf("enter on the multi-line buffer must send: ok=%v send=%v", ok, send)
	}

	// A trailing backslash before enter now stays in the buffer (the
	// old backslash-Enter rule is gone) and enter sends.
	in2 := newInputLine()
	for _, r := range "abc\\" {
		in2.insertRune(r)
	}
	ok, send = in2.handleKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !ok || !send {
		t.Fatalf("enter after a trailing backslash must send now: ok=%v send=%v", ok, send)
	}
	if string(in2.val) != "abc\\" {
		t.Fatalf("backslash must stay in the buffer: %q", string(in2.val))
	}
}

// TestInputMultiLineCaretVert pins up/down caret navigation inside a
// multi-line buffer: the arrows walk the physical rows keeping the
// column (clamped), and only at the top / bottom edge does the key fall
// through (to the prompt history, tested separately).
func TestInputMultiLineCaretVert(t *testing.T) {
	m := keyModel(t)
	in := newInputLine()
	// Rows (usable width 98 — no word wrap involved):
	//   r0 "hello world"            [0,11)
	//   r1 "second line is shorter" [12,34)
	//   r2 "third"                  [35,40)
	in.val = []rune("hello world\nsecond line is shorter\nthird")
	press := func(typ tea.KeyType) (ok bool) {
		ok, _ = in.handleKey(m, tea.KeyMsg{Type: typ})
		return ok
	}
	in.cur = 40 // end of the bottom row
	if !press(tea.KeyUp) || in.cur != 17 {
		t.Fatalf("up: cur=%d, want row 1 col 5 (cur 17)", in.cur)
	}
	if !press(tea.KeyUp) || in.cur != 5 {
		t.Fatalf("up: cur=%d, want row 0 col 5 (cur 5)", in.cur)
	}
	// Top edge, empty history: up is not consumed (it falls through).
	if press(tea.KeyUp) {
		t.Fatal("top edge without history: up must fall through")
	}
	if !press(tea.KeyDown) || in.cur != 17 {
		t.Fatalf("down: cur=%d, want row 1 col 5 (cur 17)", in.cur)
	}
	// Column clamp: row 2 is shorter than the column the caret carries.
	if !press(tea.KeyDown) || in.cur != 40 {
		t.Fatalf("down: cur=%d, want row 2 clamped to its end (cur 40)", in.cur)
	}
	// Bottom edge: down is not consumed.
	if press(tea.KeyDown) {
		t.Fatal("bottom edge: down must fall through")
	}
}

// TestInputCaretVertWrappedRows pins that the arrows walk WORD-WRAPPED
// physical rows too (a long single logical line wraps, and the caret
// follows the visible layout, not the logical line).
func TestInputCaretVertWrappedRows(t *testing.T) {
	m := keyModel(t)
	in := newInputLine()
	// 110 runes, one logical line: at usable 96 (the framed bar's inner
	// width at 100 columns) it wraps to r0 [0,96) and r1 [96,110).
	in.val = []rune(strings.Repeat("x", 110))
	in.cur = 110 // end of the bottom wrapped row (col 14)
	ok, _ := in.handleKey(m, tea.KeyMsg{Type: tea.KeyUp})
	if !ok {
		t.Fatal("up on a wrapped row must be consumed")
	}
	// The column is kept on the top wrapped row (col 14 < 96: no clamp).
	if in.cur != 14 {
		t.Fatalf("caret %d, want 14 (top wrapped row, same column)", in.cur)
	}
	ok, _ = in.handleKey(m, tea.KeyMsg{Type: tea.KeyDown})
	if !ok || in.cur != 110 {
		t.Fatalf("down: ok=%v cur=%d, want 110 (back to the wrapped tail)", ok, in.cur)
	}
}

// TestInputMultiLineHistoryAtEdges pins the arrow contract between the
// multi-line buffer and the prompt history: inside the buffer the arrows
// walk rows; at the top edge up opens the browse (newest first); while
// browsing the arrows walk whole entries; down out of the browse
// restores the saved draft and caret.
func TestInputMultiLineHistoryAtEdges(t *testing.T) {
	m := keyModel(t)
	in := newInputLine()
	in.hist = []string{"alpha", "beta"}
	// Rows: r0 "one" [0,3), r1 "two" [4,7).
	in.val = []rune("one\ntwo")
	in.cur = 5 // row 1, col 1
	// Down at the bottom edge (no browse) is a no-op.
	if ok, _ := in.handleKey(m, tea.KeyMsg{Type: tea.KeyDown}); ok {
		t.Fatal("down at the bottom edge must fall through")
	}
	// Up first walks the rows …
	ok, _ := in.handleKey(m, tea.KeyMsg{Type: tea.KeyUp})
	if !ok || in.histPos != -1 || in.cur != 1 {
		t.Fatalf("up inside the buffer: ok=%v histPos=%d cur=%d, want row 0 col 1 (cur 1)", ok, in.histPos, in.cur)
	}
	// … and the top edge opens the history browse (newest first).
	ok, _ = in.handleKey(m, tea.KeyMsg{Type: tea.KeyUp})
	if !ok || in.histPos != 1 || string(in.val) != "beta" {
		t.Fatalf("top edge up must open the browse: ok=%v pos=%d val=%q", ok, in.histPos, string(in.val))
	}
	// While browsing, the arrows walk whole entries (not rows).
	ok, _ = in.handleKey(m, tea.KeyMsg{Type: tea.KeyUp})
	if !ok || in.histPos != 0 || string(in.val) != "alpha" {
		t.Fatalf("up in the browse: pos=%d val=%q, want the older entry", in.histPos, string(in.val))
	}
	ok, _ = in.handleKey(m, tea.KeyMsg{Type: tea.KeyDown})
	if !ok || in.histPos != 1 || string(in.val) != "beta" {
		t.Fatalf("down in the browse: pos=%d val=%q, want the newest entry", in.histPos, string(in.val))
	}
	// … and down out of the browse restores the saved draft (the buffer
	// and caret as they were when the browse started: cur 1, after the
	// row walk above).
	ok, _ = in.handleKey(m, tea.KeyMsg{Type: tea.KeyDown})
	if !ok || in.histPos != -1 || string(in.val) != "one\ntwo" || in.cur != 1 {
		t.Fatalf("down out of the browse: pos=%d val=%q cur=%d, want the draft restored", in.histPos, string(in.val), in.cur)
	}
}

// TestInputMultiLineRenderRows pins the multi-line render: one physical
// row per text line, the continuation rows carrying the prompt-width
// indent, and the text split across the right rows.
func TestInputMultiLineRenderRows(t *testing.T) {
	m := keyModel(t)
	m.inp.val = []rune("first line\nsecond line")
	m.inp.cur = 14 // row 1, inside "second line"
	lines := m.inp.render(m, 100)
	if len(lines) != 2 {
		t.Fatalf("multi-line render emitted %d rows, want 2", len(lines))
	}
	p0, p1 := stripANSI(lines[0]), stripANSI(lines[1])
	if !strings.Contains(p0, "first line") {
		t.Fatalf("row 1 missing its text: %q", p0)
	}
	// The continuation row is indented by the prompt width and carries
	// the second line's text.
	if !strings.HasPrefix(p1, "  ") || !strings.Contains(p1, "second line") {
		t.Fatalf("continuation row not indented / missing text: %q", p1)
	}
	if strings.Contains(p0, "second") {
		t.Fatalf("row 1 leaked the second line: %q", p0)
	}
}

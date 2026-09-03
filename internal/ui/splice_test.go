package ui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"dsh-cli/internal/app"
	"dsh-cli/internal/protocol"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestSpliceRow pins the popup-splice contract: the popup's opaque cells
// take [ox, ox+ww); the underlying row's content and styling keep
// showing through both margins (a styled segment crossing a splice edge
// stays styled on the margin side), and a wide underlying rune under a
// splice edge keeps its column as a styled space so the popup edge stays
// column-exact.
func TestSpliceRow(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	red := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000"))
	green := lipgloss.NewStyle().Foreground(lipgloss.Color("#00ff00"))
	card := lipgloss.NewStyle().Background(lipgloss.Color("#222222"))

	below := red.Render("abcdefghij") + green.Render("klmnopqrst")
	row := card.Render("▓▓▓▓▓▓")

	out := spliceRow(below, row, 5, 6)
	if pw := plainWidth(out); pw != 20 {
		t.Fatalf("visible width %d, want 20: %q", pw, stripANSI(out))
	}
	vis := stripANSI(out)
	want := "abcde" + strings.Repeat("▓", 6) + "lmnopqrst"
	if vis != want {
		t.Fatalf("spliced row = %q, want %q", vis, want)
	}
	cells := scanStyled(out)
	if len(cells) != 20 {
		t.Fatalf("%d cells, want 20", len(cells))
	}
	if !cells[0].state.equals(extractSGR(red.Render("x"))) {
		t.Fatalf("left margin lost its red style: %+v", cells[0].state)
	}
	if !cells[5].state.equals(extractSGR(card.Render("x"))) {
		t.Fatalf("window band not the popup surface: %+v", cells[5].state)
	}
	if !cells[11].state.equals(extractSGR(green.Render("x"))) {
		t.Fatalf("right margin lost its green style (segment crossing the edge): %+v", cells[11].state)
	}

	// A wide underlying rune under the left splice edge keeps its column
	// as the styled space; the popup stays column-exact (the margin tail
	// starts after the window, at the first cell below owns there).
	wide := red.Render("ab世cd") + green.Render("ef")
	outW := spliceRow(wide, row, 3, 4)
	visW := stripANSI(outW)
	if pw := plainWidth(outW); pw != 8 {
		t.Fatalf("wide-row visible width %d, want 8: %q", pw, visW)
	}
	if want := "ab " + strings.Repeat("▓", 4) + "f"; visW != want {
		t.Fatalf("wide-row splice = %q, want %q", visW, want)
	}
	cellsW := scanStyled(outW)
	if !cellsW[2].state.equals(extractSGR(red.Render("x"))) {
		t.Fatalf("stripped wide-rune column lost its style: %+v", cellsW[2].state)
	}

	// Flush edges: zero left margin, popup covering the row's first half;
	// the window's own glyphs take its band, the margin keeps its own.
	edge := spliceRow(below, card.Render("▓▓▓▓▓▓▓▓▓▓"), 0, 10)
	if vis := stripANSI(edge); vis != strings.Repeat("▓", 10)+"klmnopqrst" {
		t.Fatalf("flush splice = %q", vis)
	}
	ec := scanStyled(edge)
	if !ec[0].state.equals(extractSGR(card.Render("x"))) || !ec[9].state.equals(extractSGR(card.Render("x"))) {
		t.Fatalf("flush window band lost its surface: %+v %+v", ec[0].state, ec[9].state)
	}
	if !ec[10].state.equals(extractSGR(green.Render("x"))) {
		t.Fatalf("flush right margin lost its style: %+v", ec[10].state)
	}
}

// spliceMarginModel is the margin-splice fixture: a 100x30 model with a
// card-surface theme and several transcript lines behind the popup.
func spliceMarginModel(t *testing.T) *Model {
	t.Helper()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Setenv("NO_COLOR", "")
	t.Setenv("COLORFGBG", "15;0")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	a := app.New("http://127.0.0.1:3999")
	a.Start(ctx)
	m := NewModel(a)
	m.splashOff = true
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if m.th.CCardBG == "" {
		t.Skip("theme has no card surface in this profile")
	}
	m.st.SetActive("s1")
	for i := 1; i <= 6; i++ {
		text := strings.Repeat("zebra-"+string(rune('a'+i-1))+" ", 8)
		b, err := json.Marshal(protocol.Message{Role: "user",
			Content: []protocol.ContentBlock{{Type: "text", Text: text}}})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		m.st.Event("s1", &protocol.SessionEvent{Type: "user/message", Seq: int64(i), Time: 1000, Data: b})
	}
	return m
}

// cellAt returns the display cell of cells occupying column x (a wide
// rune's tail column belongs to the rune itself); ok is false past the
// line's end.
func cellAt(cells []styleCell, x int) (styleCell, bool) {
	for i := range cells {
		c := cells[i]
		if c.col <= x && x < c.col+c.w {
			return c, true
		}
		if c.col > x {
			return styleCell{}, false
		}
	}
	return styleCell{}, false
}

// checkSplicedFrame verifies every popup row of the open frame: each
// margin column keeps the underlying frame row's cell (glyph and style)
// where the row has one, and plain bare-ground cells where it doesn't —
// the popup's opaque cells take exactly [ox, ox+ww), so no background
// slab covers the transcript on either side.
func checkSplicedFrame(t *testing.T, m *Model, base, open []string, ox, ww, oy, wh int) {
	t.Helper()
	checkMargin := func(label string, y, x int, oc, bc []styleCell) {
		t.Helper()
		if u, ok := cellAt(bc, x); ok {
			got, gOK := cellAt(oc, x)
			if !gOK || got.r != u.r || !got.state.equals(u.state) {
				t.Fatalf("row %d: %s margin col %d lost the underlying cell %q",
					y, label, x, u.r)
			}
			return
		}
		// Bare ground behind this column: the open row carries a plain
		// bare space there — the frame's rows are filled to the full
		// screen width (fillRowWidth), so a short underlying row never
		// ends before it and leaves the renderer to EraseLineRight the
		// tail with the window's card background.
		if g, gOK := cellAt(oc, x); !gOK || g.r != ' ' || !g.state.empty() {
			t.Fatalf("row %d: %s margin col %d painted over bare ground: %q",
				y, label, x, g.r)
		}
	}
	for i := 0; i < wh && oy+i < len(open); i++ {
		y := oy + i
		oc, bc := scanStyled(open[y]), scanStyled(base[y])
		for x := 0; x < ox; x++ {
			checkMargin("left", y, x, oc, bc)
		}
		for x := ox + ww; x < m.W; x++ {
			checkMargin("right", y, x, oc, bc)
		}
	}
}

// TestSessionWindowMarginSplice pins the session window's margin
// contract: the centered window's rows are full screen width, the
// transcript keeps showing in the margins on either side — the card
// slab covers exactly the window's columns and nothing more.
func TestSessionWindowMarginSplice(t *testing.T) {
	m := spliceMarginModel(t)
	base := strings.Split(m.View(), "\n")

	m.sideVisible = true
	ww, wh := m.sessionWindowSize()
	ox, oy := (m.W-ww)/2, (m.H-wh)/2
	open := strings.Split(m.View(), "\n")
	checkSplicedFrame(t, m, base, open, ox, ww, oy, wh)
}

// TestModalMarginSplice pins the same contract for the modal plate.
func TestModalMarginSplice(t *testing.T) {
	m := spliceMarginModel(t)
	base := strings.Split(m.View(), "\n")

	m.openModal(&renameModal{edit: lineEdit{val: []rune("session one"), cur: 0}})
	box, ox, oy, bw, bh := m.modalBox(m.topModal())
	_ = box
	open := strings.Split(m.View(), "\n")
	checkSplicedFrame(t, m, base, open, ox, bw, oy, bh)
}

// TestFrameRowsFullWidth pins the EraseLineRight contract: no rendered
// row is shorter than the terminal, so the renderer's row-tail erase
// never fires with a styled background active. The case that used to
// leak: on a screen where the centered window's rows overlap a short
// underlying row (a toast), the spliced row ended on the window's card
// border and ran short of the right edge — the renderer then smeared the
// card surface into the right margin, covering the underlying text there
// (crush's screen-buffer renderer never leaves a row short, so its
// dialogs don't show the slab).
func TestFrameRowsFullWidth(t *testing.T) {
	m := spliceMarginModel(t) // 100x30 fixture
	// Shrink to a screen where the window's rows overlap the toast row
	// (the window covers rows oy..oy+wh, the toast sits at the top of
	// the frame) and add the toast.
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 16})
	m.toasts = append(m.toasts, toast{level: "ok", text: "saved", until: time.Now().Add(time.Minute)})
	// Park the input caret off the (empty) line: when the window opens,
	// the main bar's caret stands down (its keyboard goes to the window's
	// search line), so the underlying row would otherwise legitimately
	// differ between the base and the open frame.
	m.inp.cur = 1
	base := strings.Split(m.View(), "\n")
	for y, ln := range base {
		if pw := plainWidth(ln); pw < m.W {
			t.Fatalf("base row %d is %d wide, want >= %d (the renderer would ELR its tail)", y, pw, m.W)
		}
	}

	m.sideVisible = true
	ww, wh := m.sessionWindowSize()
	ox, oy := (m.W-ww)/2, (m.H-wh)/2
	open := strings.Split(m.View(), "\n")
	for y, ln := range open {
		if pw := plainWidth(ln); pw < m.W {
			t.Fatalf("open row %d is %d wide, want >= %d (ELR would paint the card tail)", y, pw, m.W)
		}
	}
	// The window's own rows are exactly full width: the card slab ends at
	// the border, and checkSplicedFrame pins that the margin right of it
	// keeps the underlying content or bare ground — never a card smear.
	for i := 0; i < wh && oy+i < len(open); i++ {
		y := oy + i
		total := 0
		for _, c := range scanStyled(open[y]) {
			total += c.w
		}
		if total != m.W {
			t.Fatalf("window row %d is %d wide, want %d", y, total, m.W)
		}
	}
	checkSplicedFrame(t, m, base, open, ox, ww, oy, wh)
}

// TestModalOpaqueInterior pins the modal box's opaque contract: every
// cell the box paints carries a background (the card surface, or the
// row's own band), so the spliced screen row takes the box's cell as
// painted — nothing of the underlying screen shows through the box
// (the old floating look passed the transcript through the box's hole
// cells).
func TestModalOpaqueInterior(t *testing.T) {
	m := spliceMarginModel(t)
	_ = strings.Split(m.View(), "\n")

	m.openModal(&renameModal{edit: lineEdit{val: []rune("session one"), cur: 0}})
	box, ox, oy, bw, _ := m.modalBox(m.topModal())
	open := strings.Split(m.View(), "\n")

	for i, boxLn := range strings.Split(box, "\n") {
		y := oy + i
		if y >= len(open) {
			continue
		}
		oc, rc := scanStyled(open[y]), scanStyled(boxLn)
		for x := ox; x < ox+bw; x++ {
			pc, pOK := cellAt(rc, x-ox)
			if !pOK {
				t.Fatalf("row %d: box row has no cell at col %d", y, x)
			}
			if len(pc.state.bg) == 0 {
				t.Fatalf("row %d col %d: transparent box cell %q (want the card surface)", y, x, pc.r)
			}
			got, gOK := cellAt(oc, x)
			if !gOK || got.r != pc.r || !got.state.equals(pc.state) {
				t.Fatalf("row %d col %d: box cell not painted as-is (got %q, want %q)", y, x, got.r, pc.r)
			}
		}
	}
}

// TestFillRowWidth pins the bare-ground padding: a short line that ends
// styled (a card surface) gets a reset before the pad so the pad cells
// carry no background at all, and a line that already reaches the width
// passes through byte-identical.
func TestFillRowWidth(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	card := lipgloss.NewStyle().Background(lipgloss.Color("#222222"))
	out := fillRowWidth(card.Render("abc"), 10)
	if pw := plainWidth(out); pw != 10 {
		t.Fatalf("width %d, want 10: %q", pw, stripANSI(out))
	}
	cells := scanStyled(out)
	for i, c := range cells {
		if i < 3 {
			if len(c.state.bg) == 0 {
				t.Fatalf("col %d: lost the card background", i)
			}
		} else if !c.state.empty() || c.r != ' ' {
			t.Fatalf("col %d: pad cell not bare: %+v", i, c.state)
		}
	}
	full := card.Render(strings.Repeat("x", 10))
	if got := fillRowWidth(full, 10); got != full {
		t.Fatalf("full-width line was modified")
	}
	fg := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000"))
	cellsFg := scanStyled(fillRowWidth(fg.Render("ab"), 6))
	for i := 2; i < 6; i++ {
		if !cellsFg[i].state.empty() {
			t.Fatalf("col %d: fg line's pad carries state: %+v", i, cellsFg[i].state)
		}
	}
}

// TestSpliceRowHoleWide pins spliceRow's wide-rune edge cases under a
// hole: a CJK rune under the dialog keeps showing (its tail column
// inside the hole becomes the styled space), and a rune straddling the
// window's right edge keeps its tail column as the styled space.
func TestSpliceRowHoleWide(t *testing.T) {
	below := "\x1b[2m甲乙丙丁戊己庚辛壬癸\x1b[0m" // 10 CJK runes, 20 columns, dim

	// Window [2,6): border, hole, glyph, hole. 乙 occupies [2,4) — its
	// col 2 is covered by the border, its tail col 3 falls in the first
	// hole and must come back as the dim styled space. 丙 occupies
	// [4,6) — its tail col 5 falls in the second hole the same way.
	out := spliceRow(below, "│ X ", 2, 4)
	cells := scanStyled(out)
	// col 2 is the window's left edge: the border covers 乙's head, 乙's
	// tail col 3 comes back as the dim styled space.
	want := []struct {
		x   int
		r   rune
		dim bool
	}{
		{0, '甲', true}, {2, '│', false}, {3, ' ', true},
		{4, 'X', false}, {5, ' ', true}, {6, '丁', true},
	}
	for _, wt := range want {
		c, ok := cellAt(cells, wt.x)
		if !ok {
			t.Fatalf("col %d: no cell", wt.x)
		}
		if c.r != wt.r {
			t.Fatalf("col %d: got %q, want %q", wt.x, c.r, wt.r)
		}
		if c.state.dim != wt.dim {
			t.Fatalf("col %d: dim=%v, want %v", wt.x, c.state.dim, wt.dim)
		}
	}
	// Visible width survives the splice: 20 columns.
	total := 0
	for _, c := range cells {
		total += c.w
	}
	if total != 20 {
		t.Fatalf("visible width %d, want 20", total)
	}

	// Window [2,5): 乙 [2,4) — its tail col 3 falls in the hole, a dim
	// styled space. 丙 [4,6) — its head col 4 is covered by X, its tail
	// col 5 is the window's right edge: the right margin keeps it as the
	// dim styled space, and the row continues from 丁.
	out = spliceRow(below, "│ X", 2, 3)
	cells = scanStyled(out)
	for _, wt := range []struct {
		x   int
		r   rune
		dim bool
	}{
		{0, '甲', true}, {2, '│', false}, {3, ' ', true},
		{4, 'X', false}, {5, ' ', true}, {6, '丁', true},
	} {
		c, ok := cellAt(cells, wt.x)
		if !ok {
			t.Fatalf("col %d: no cell", wt.x)
		}
		if c.r != wt.r {
			t.Fatalf("col %d: got %q, want %q", wt.x, c.r, wt.r)
		}
		if c.state.dim != wt.dim {
			t.Fatalf("col %d: dim=%v, want %v", wt.x, c.state.dim, wt.dim)
		}
	}
}

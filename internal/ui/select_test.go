package ui

import (
	"strings"
	"testing"

	"dsh-cli/internal/app"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func termenvTrueColor() termenv.Profile { return termenv.TrueColor }

// TestExpandTabsGrid pins the tab fix: raw tabs in styled rows collapse to
// a 4-column grid regardless of the column they land on (the code-block
// margin already shifts content), ANSI escapes are skipped and wide runes
// count double.
func TestExpandTabsGrid(t *testing.T) {
	line := "\x1b[38;5;166mfunc\x1b[0m\x1b[38;5;254m\t\x1b[0mif ok {\n"
	out := expandTabs("\x1b[38;5;254m\x1b[0m" + line)
	if strings.ContainsRune(out, '\t') {
		t.Fatalf("tab survived: %q", out)
	}
	// "func" (4) + tab at col 4 -> 4 spaces (next stop) + "if ok {"
	want := "func    if ok {\n"
	if plain := stripANSI(out); plain != want {
		t.Fatalf("grid wrong: got %q want %q", plain, want)
	}
	// Off-grid tab: col 5 -> 3 spaces to the stop.
	if got, want := stripANSI(expandTabs("12345\tx")), "12345   x"; got != want {
		t.Fatalf("off-grid tab: %q", got)
	}
	// No tab: byte-identity.
	if got := expandTabs("no tabs here"); got != "no tabs here" {
		t.Fatalf("identity broken: %q", got)
	}
}

// TestGoBlockIndentNoTabs pins the transcript rendering of a Go fenced
// block: no raw tab survives into the rendered rows, every row stays
// inside the pane width, and the indentation is a multiple of 4 spaces.
func TestGoBlockIndentNoTabs(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	m := NewModel(app.New("http://127.0.0.1:3999"))
	doc := "```go\nfunc main() {\n\tif x > 0 {\n\t\tfmt.Println(x)\n\t}\n}\n```\n"
	for i, ln := range m.mdRender(doc, 60, "") {
		if strings.ContainsRune(stripANSI(ln), '\t') {
			t.Fatalf("row %d still carries a raw tab: %q", i, stripANSI(ln))
		}
		if w := plainWidth(ln); w > 60 {
			t.Fatalf("row %d overflows (%d cells): %q", i, w, stripANSI(ln))
		}
	}
	plain := stripANSI(strings.Join(m.mdRender(doc, 60, ""), "\n"))
	idx := strings.Index(plain, "fmt.Println")
	if idx < 0 {
		t.Fatalf("call lost: %q", plain)
	}
	lineStart := strings.LastIndex(plain[:idx], "\n") + 1
	indent := strings.TrimLeft(plain[lineStart:idx], " ")
	if len(indent)%4 != 0 {
		t.Fatalf("indent %d cells is off the 4-grid", len(indent))
	}
}

// TestSelStyle pins the pick band: a background-only style (rows keep
// their own colors under it), width-true when rendered over a styled row.
func TestSelStyle(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	lipgloss.SetColorProfile(termenvTrueColor())
	m := NewModel(app.New("http://127.0.0.1:3999"))
	line := "\x1b[1;38;5;166mfunc\x1b[0m \x1b[38;5;254mmain\x1b[0m()"
	out := m.th.Sel().Render(line)
	if plainWidth(out) != plainWidth(line) {
		t.Fatalf("band changed the row width: %d vs %d", plainWidth(out), plainWidth(line))
	}
	if stripANSI(out) != "func main()" {
		t.Fatalf("text drifted: %q", stripANSI(out))
	}
	if !strings.Contains(out, "m") || !strings.Contains(out, "func") {
		t.Fatalf("content lost under the band: %q", out)
	}
	// A band escape must appear (48;5 idx in the NoColor palette).
	if !strings.Contains(out, "48;") {
		t.Fatalf("band background missing: %q", out)
	}
}

// TestRowText pins the copy extraction: styled rows give back plain text
// clipped to the inclusive column range, pad stripped; a wide rune is
// picked when its first column is covered and not when only its second
// is (the ultraviolet cell rule).
func TestRowText(t *testing.T) {
	styled := "\x1b[1;38;5;166mfunc\x1b[0m main()                    "
	if got := rowText(styled, 0, 10); got != "func main()" {
		t.Fatalf("full pick: %q", got)
	}
	if got := rowText(styled, 0, 3); got != "func" {
		t.Fatalf("leading slice: %q", got)
	}
	if got := rowText(styled, 5, 10); got != "main()" {
		t.Fatalf("trailing slice: %q", got)
	}
	if got := rowText(styled, 4, 4); got != "" {
		t.Fatalf("spaces-only pick trims to empty: %q", got)
	}
	if got := rowText("", 0, 5); got != "" {
		t.Fatalf("empty row: %q", got)
	}
	wide := "\x1b[38;5;166m你好\x1b[0m世界"
	if got := rowText(wide, 0, 7); got != "你好世界" {
		t.Fatalf("wide full: %q", got)
	}
	if got := rowText(wide, 2, 5); got != "好世" {
		t.Fatalf("wide mid (leading column rule): %q", got)
	}
	if got := rowText(wide, 1, 3); got != "好" {
		t.Fatalf("wide second-column start: %q", got)
	}
}

// TestTextSelBounds pins the crush getHighlightRange port: backward drags
// invert to read order, plain clicks are zero-size, and the range clamps
// to the content.
func TestTextSelBounds(t *testing.T) {
	s := textSel{anchorLine: 5, anchorCol: 7, dragLine: 2, dragCol: 3, moved: true}
	fl, fc, ll, lc := s.bounds(10)
	if [4]int{fl, fc, ll, lc} != [4]int{2, 3, 5, 7} {
		t.Fatalf("backward drag normalization: %d %d %d %d", fl, fc, ll, lc)
	}
	s = textSel{anchorLine: 2, anchorCol: 3, dragLine: 2, dragCol: 7, moved: true}
	fl, fc, ll, lc = s.bounds(10)
	if [4]int{fl, fc, ll, lc} != [4]int{2, 3, 2, 7} {
		t.Fatalf("reverse drag on one line: %d %d %d %d", fl, fc, ll, lc)
	}
	s = textSel{anchorLine: -3, anchorCol: -1, dragLine: 99, dragCol: 40, moved: true}
	fl, fc, ll, lc = s.bounds(10)
	if [4]int{fl, fc, ll, lc} != [4]int{0, 0, 9, 40} {
		t.Fatalf("clamping: %d %d %d %d", fl, fc, ll, lc)
	}
	s.anchor(4, 9) // a plain click: zero-size
	fl, fc, ll, lc = s.bounds(10)
	if fl != -1 || ll != -1 {
		t.Fatalf("plain click must be zero-size: %d %d %d %d", fl, fc, ll, lc)
	}
}

// TestSGRState pins the SGR state machine: terminal semantics (0 resets
// mid-sequence, 38/48 sub-lists, toggles), and marshal round-trips the
// same parameter set a terminal would hold.
func TestSGRState(t *testing.T) {
	var st sgrState
	st.apply([]int{1, 38, 5, 166})
	if !st.bold || !eqParams(st.fg, []int{38, 5, 166}) {
		t.Fatalf("bold + 256 fg: %+v", st)
	}
	if em := st.marshal(); em != "\x1b[0;1;38;5;166m" {
		t.Fatalf("marshal: %q", em)
	}
	st.apply([]int{0, 2, 38, 2, 255, 0, 128})
	if !st.dim || !eqParams(st.fg, []int{38, 2, 255, 0, 128}) || st.bold {
		t.Fatalf("reset mid-sequence + truecolor: %+v", st)
	}
	st.apply([]int{3, 4, 7})
	st.apply([]int{27, 24})
	if !st.italic || st.inverse || st.underline {
		t.Fatalf("toggles: %+v", st)
	}
	st.apply([]int{39})
	if len(st.fg) != 0 {
		t.Fatalf("default fg: %+v", st)
	}
	st.apply([]int{0})
	if !st.empty() {
		t.Fatalf("full reset: %+v", st)
	}
	// The band overlay: background replaced, own foreground kept; the
	// no-palette band is reverse video.
	band := sgrState{bg: []int{48, 2, 100, 100, 100}}
	n := sgrState{bold: true, fg: []int{31}}.withBand(band)
	if !n.bold || !eqParams(n.fg, []int{31}) || !eqParams(n.bg, []int{48, 2, 100, 100, 100}) {
		t.Fatalf("band merge: %+v", n)
	}
	n = sgrState{fg: []int{31}}.withBand(sgrState{inverse: true})
	if !n.inverse || !eqParams(n.fg, []int{31}) {
		t.Fatalf("reverse band: %+v", n)
	}
}

// TestApplyBandPartial pins the partial-row band: the covered cells carry
// the band, the cell colors survive under it, the row width is unchanged,
// and a full-row pick takes the fast theme pass.
func TestApplyBandPartial(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	lipgloss.SetColorProfile(termenvTrueColor())
	m := NewModel(app.New("http://127.0.0.1:3999"))
	line := "\x1b[1;38;5;166mfunc\x1b[0m \x1b[38;5;254mmain\x1b[0m()"
	out := m.applyBand(line, 5, 10) // over "main"
	if plainWidth(out) != plainWidth(line) {
		t.Fatalf("width drifted: %d vs %d", plainWidth(out), plainWidth(line))
	}
	if stripANSI(out) != "func main()" {
		t.Fatalf("text drifted: %q", stripANSI(out))
	}
	if !strings.Contains(out, "38;5;166") {
		t.Fatalf("func color lost: %q", out)
	}
	if !strings.Contains(out, "48;") {
		t.Fatalf("band background missing: %q", out)
	}
	if stripANSI(m.applyBand("", 0, 9)) != "" {
		t.Fatal("empty row must stay empty")
	}
	if out2 := m.applyBand(line, 0, plainWidth(line)-1); out2 != m.th.Sel().Render(line) {
		t.Fatal("full-row pick must use the theme band")
	}
}

// TestSelectionTextJoin pins the crush joinRows port: near-full wrapped
// rows join with a space, blank rows are paragraph breaks, block starters
// keep their own line, and a mid-line pick clips the picked rows.
func TestSelectionTextJoin(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	m := NewModel(app.New("http://127.0.0.1:3999"))
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	w := m.transcriptWidth()
	full := strings.Repeat("a", w)
	m.trans.lines = []string{
		full,
		strings.Repeat("b", w/2),
		"",
		"- bullet row",
	}
	m.sel.anchor(0, 0)
	m.sel.dragTo(3, plainWidth(m.trans.lines[3])-1)
	want := full + " " + strings.Repeat("b", w/2) + "\n\n- bullet row"
	if got := m.selectionText(); got != want {
		t.Fatalf("join: %q\nwant %q", got, want)
	}
	// A mid-line pick clips the picked row at the pick end.
	m.sel.selectWord(1, 3, m.trans.lines[1])
	if got := m.selectionText(); got != strings.Repeat("b", w/2) {
		t.Fatalf("word pick of the b-row: %q", got)
	}
	// Zero-size: a plain click copies nothing.
	m.sel.anchor(1, 2)
	if got := m.selectionText(); got != "" {
		t.Fatalf("plain click must copy nothing: %q", got)
	}
}

// TestWordBounds pins the crush findWordBoundaries port: UAX#29 segments,
// display-column widths (wide runes double), and a zero range on
// whitespace.
func TestWordBounds(t *testing.T) {
	line := "the quick brown fox"
	if ws, we := wordBounds(line, 1); ws != 0 || we != 2 {
		t.Fatalf("the: %d %d", ws, we)
	}
	if ws, we := wordBounds(line, 5); ws != 4 || we != 8 {
		t.Fatalf("quick: %d %d", ws, we)
	}
	if ws, we := wordBounds(line, 9); ws != 9 || we != 9 {
		t.Fatalf("whitespace: %d %d", ws, we)
	}
	if ws, we := wordBounds(line, 20); ws != 20 || we != 20 {
		t.Fatalf("past the end is a zero range (crush falls back to a plain anchor): %d %d", ws, we)
	}
	if ws, we := wordBounds("你好 world", 2); ws != 2 || we != 3 {
		t.Fatalf("CJK word: %d %d", ws, we)
	}
	if ws, we := wordBounds("", 0); ws != 0 || we != 0 {
		t.Fatalf("empty: %d %d", ws, we)
	}
}

// TestMousePickFlow drives the crush-style gesture on a rendered model:
// a press anchors a cell (zero-size, no band, no copy), a drag extends to
// a cell endpoint (the release settles the pick; the copy is explicit),
// a double-click selects the word, a triple-click the whole line, esc
// clears, ctrl+c copies, a press off the pane drops the pick, and the
// wheel still scrolls.
func TestMousePickFlow(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	m := NewModel(app.New("http://127.0.0.1:3999"))
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.trans.lines = []string{
		"\x1b[1;38;5;166malpha\x1b[0m beta gamma",
		"delta epsilon",
		"zeta eta",
		"theta iota",
	}
	m.transX, m.transY = 0, 2
	m.follow = false
	m.scroll = 0

	pressAt := func(x, y int) {
		m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	}
	moveTo := func(x, y int) {
		m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	}
	release := func() tea.Cmd {
		_, cmd := m.Update(tea.MouseMsg{X: 1, Y: 1, Button: tea.MouseButtonNone, Action: tea.MouseActionRelease})
		return cmd
	}

	// Click: a zero-size anchor — no band, and the release schedules no copy.
	pressAt(3, 3) // pane row 1 -> absolute line 1, col 3
	if m.sel.active || m.sel.anchorLine != 1 || m.sel.anchorCol != 3 {
		t.Fatalf("click anchor: %+v", m.sel)
	}
	if cmd := release(); cmd != nil {
		t.Fatalf("plain click must not schedule a copy: %v", cmd)
	}

	// Drag: anchor at (line 0, col 0) via a press on the marker row,
	// extend to (line 2, col 3).
	pressAt(0, 2) // pane row 0 -> absolute line 0
	moveTo(3, 3)
	moveTo(3, 4) // pane row 2 -> absolute line 2
	if cmd := release(); cmd != nil {
		t.Fatalf("release must not schedule anything (the copy is explicit): %v", cmd)
	}
	if m.sel.anchorLine != 0 || m.sel.anchorCol != 0 || m.sel.dragLine != 2 || m.sel.dragCol != 3 {
		t.Fatalf("drag endpoints: %+v", m.sel)
	}
	if !m.sel.active || m.sel.down {
		t.Fatalf("finished drag must stay picked: %+v", m.sel)
	}
	fl, fc, ll, lc := m.sel.bounds(4)
	if [4]int{fl, fc, ll, lc} != [4]int{0, 0, 2, 3} {
		t.Fatalf("drag bounds: %d %d %d %d", fl, fc, ll, lc)
	}

	// Press on line 4? pane has only 4 rows; drag again backward.
	pressAt(7, 4) // pane row 2 -> absolute line 2
	moveTo(7, 3)
	release()
	fl, fc, ll, lc = m.sel.bounds(4)
	if [4]int{fl, fc, ll, lc} != [4]int{1, 7, 2, 7} {
		t.Fatalf("backward drag: %d %d %d %d", fl, fc, ll, lc)
	}

	// Double-click selects the word under the cursor ("beta" on line 0).
	m.sel.reset()
	m.lastPressT = time.Time{}
	pressAt(7, 2) // col 7 sits on "beta" ("alpha beta gamma")
	pressAt(7, 2)
	fl, _, ll, _ = m.sel.bounds(4)
	if fl != 0 || ll != 0 {
		t.Fatalf("word pick rows: %d %d", fl, ll)
	}
	if got := m.selectionText(); got != "beta" {
		t.Fatalf("word pick text: %q", got)
	}

	// Triple-click selects the whole line.
	m.lastPressT = time.Time{}
	pressAt(5, 2)
	pressAt(5, 2)
	pressAt(5, 2)
	fl, fc, ll, lc = m.sel.bounds(4)
	if fl != 0 || ll != 0 || fc != 0 || lc != plainWidth(m.trans.lines[0])-1 {
		t.Fatalf("line pick: %d %d %d %d", fl, fc, ll, lc)
	}
	if got := m.selectionText(); got != "alpha beta gamma" {
		t.Fatalf("line pick text: %q", got)
	}

	// ctrl+c repeats the copy while the pick is live; esc clears it.
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		t.Fatalf("ctrl+c must schedule the copy")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.sel.active {
		t.Fatalf("esc must clear the pick")
	}

	// Off-pane press drops the pick; so does a press under a modal.
	pressAt(3, 3)
	m.transX = -1
	pressAt(3, 3)
	if m.sel.active {
		t.Fatal("off-pane press must drop the pick")
	}
	m.transX = 0
	m.openModal(&helpModal{})
	// The help box (70 wide, 20 tall on a 100x30 screen) covers
	// rows 5..24 from the frame's left edge: press inside it.
	pressAt(3, 10)
	if m.sel.active {
		t.Fatal("press under a modal box must drop the pick")
	}
}

// TestMouseWheelStillScrolls pins the pre-existing wheel behavior through
// the new action-switched handler.
func TestMouseWheelStillScrolls(t *testing.T) {
	m := NewModel(app.New("http://127.0.0.1:3999"))
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.trans.lines = make([]string, 50)
	m.follow = true
	m.Update(tea.MouseMsg{X: 5, Y: 5, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if m.scroll != m.transMaxOff()-wheelStep {
		t.Fatalf("wheel up miss: scroll %d max %d", m.scroll, m.transMaxOff())
	}
}

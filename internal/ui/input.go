// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"strings"

	"dsh-cli/internal/i18n"

	"github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
)

// slashCmd is one candidate in the slash menu.
type slashCmd struct {
	Name  string
	Desc  string
	Local bool
}

// inputLine is a borderless multi-line text input with a caret and a
// slash-command completion menu.
type inputLine struct {
	val      []rune
	cur      int     // rune index
	sel      editSel // text pick (mouse drag / shift+move; see editSel)
	menu     []slashCmd
	menuOpen bool
	menuCur  int

	// @ completion (running children + cwd paths), parallel to the slash menu.
	atMenu []atMenuItem
	atOpen bool
	atCur  int
	atPos  int // rune index of the '@' that opened the menu (-1 when none)

	// Prompt history (up/down keys): every submitted text, newest last;
	// histPos indexes it while browsing (-1 while not) and histDraft /
	// histDraftCur hold the pre-browsing buffer so down can restore it.
	hist         []string
	histPos      int
	histDraft    string
	histDraftCur int

	forceQueue bool // Alt+Enter: a queued send over the implicit steer
}

// histCap bounds the prompt history (oldest entries fall off).
const histCap = 100

// newInputLine starts out of the history browse (histPos -1): the zero
// value would look like "at the oldest entry".
func newInputLine() *inputLine { return &inputLine{histPos: -1} }

// consumeForceQueue clears and reports a pending force-queue (Alt+Enter).
func (in *inputLine) consumeForceQueue() bool {
	f := in.forceQueue
	in.forceQueue = false
	return f
}

// refreshMenu recomputes the slash menu from the current text.
func (in *inputLine) refreshMenu(cmds []slashCmd) {
	if len(in.val) == 0 || in.val[0] != '/' {
		in.menuOpen = false
		return
	}
	// The menu stays open while typing the command token (no space yet).
	firstSpace := false
	for _, r := range in.val {
		if r == ' ' {
			firstSpace = true
			break
		}
	}
	if firstSpace {
		in.menuOpen = false
		return
	}
	token := string(in.val[1:])
	var matches []slashCmd
	for _, c := range cmds {
		if strings.HasPrefix(c.Name, token) {
			matches = append(matches, c)
		}
	}
	if len(matches) == 0 && token != "" {
		// An unknown token is still a legal host command: keep the raw
		// text and hide the menu.
		in.menuOpen = false
		return
	}
	in.menu = matches
	in.menuOpen = len(matches) > 0
	if in.menuOpen {
		if in.menuCur >= len(matches) {
			in.menuCur = len(matches) - 1
		}
	}
}

// closeMenu drops the slash menu state.
func (in *inputLine) closeMenu() {
	in.menuOpen = false
	in.menuCur = 0
}

// atMenuStart returns the first visible @ menu row for a window of n.
func (in *inputLine) atMenuStart(n int) int {
	vis := n
	if vis > len(in.atMenu) {
		vis = len(in.atMenu)
	}
	start := in.atCur - vis/2
	if start < 0 {
		start = 0
	}
	if start > len(in.atMenu)-vis {
		start = len(in.atMenu) - vis
	}
	if start < 0 {
		start = 0
	}
	return start
}

// atToken returns the position of the '@' that starts the current word
// and the word typed after it (-1, "" when the caret is not inside one).
func (in *inputLine) atToken() (int, string) {
	text := in.value()
	caret := in.cur
	for i := caret - 1; i >= 0; i-- {
		if text[i] == '@' {
			if i == 0 || text[i-1] == ' ' || text[i-1] == '\n' || text[i-1] == '\t' {
				return i, text[i+1 : caret]
			}
			return -1, "" // an '@' mid-word is not a trigger
		}
		if text[i] == ' ' || text[i] == '\n' || text[i] == '\t' {
			return -1, "" // whitespace before any '@'
		}
	}
	return -1, ""
}

// closeAt drops the @ completion state.
func (in *inputLine) closeAt() {
	in.atOpen = false
	in.atMenu = nil
	in.atCur = 0
	in.atPos = -1
}

// completeAt inserts the selected @ item at the token position (a
// trailing space unless the text already ends in a separator).
func (in *inputLine) completeAt() {
	if !in.atOpen || len(in.atMenu) == 0 || in.atPos < 0 {
		return
	}
	in.histDetach()
	it := in.atMenu[in.atCur]
	text := in.value()
	newText := text[:in.atPos] + it.Text
	if !strings.HasSuffix(newText, " ") && !strings.HasSuffix(newText, "/") {
		newText += " "
	}
	in.sel.reset()
	in.val = []rune(newText)
	in.cur = len(in.val)
	in.closeAt()
}

// value returns the full text.
func (in *inputLine) value() string { return string(in.val) }

// menuStart returns the first visible menu row for a window of n.
func (in *inputLine) menuStart(n int) int {
	vis := n
	if vis > len(in.menu) {
		vis = len(in.menu)
	}
	start := in.menuCur - vis/2
	if start < 0 {
		start = 0
	}
	if start > len(in.menu)-vis {
		start = len(in.menu) - vis
	}
	if start < 0 {
		start = 0
	}
	return start
}

// empty reports whether the input holds only whitespace.
func (in *inputLine) empty() bool { return strings.TrimSpace(in.value()) == "" }

// clear empties the line and closes the menu.
func (in *inputLine) clear() {
	in.val = nil
	in.cur = 0
	in.sel.reset()
	in.menuOpen = false
	in.menuCur = 0
	in.closeAt()
	in.histPos = -1
}

// record appends one submitted text to the prompt history, newest last,
// dropping a duplicate of the most recent entry and capping at histCap.
func (in *inputLine) record(text string) {
	if n := len(in.hist); n > 0 && in.hist[n-1] == text {
		return
	}
	in.hist = append(in.hist, text)
	if len(in.hist) > histCap {
		in.hist = in.hist[len(in.hist)-histCap:]
	}
}

// histUp steps to an older prompt. Starting a browse saves the current
// buffer (and caret) so histDown can restore it; at the oldest entry it
// holds. It reports false when the history is empty, so the caller keeps
// up/down for other uses.
func (in *inputLine) histUp() bool {
	if len(in.hist) == 0 {
		return false
	}
	if in.histPos < 0 {
		in.histDraft = string(in.val)
		in.histDraftCur = in.cur
		in.histPos = len(in.hist) - 1
	} else if in.histPos > 0 {
		in.histPos--
	}
	in.closeMenu()
	in.sel.reset()
	in.val = []rune(in.hist[in.histPos])
	in.cur = len(in.val)
	return true
}

// histDown steps to a newer prompt, or out of the browse when at the
// newest one (restoring the saved buffer). It reports false when not
// browsing.
func (in *inputLine) histDown() bool {
	if in.histPos < 0 {
		return false
	}
	if in.histPos < len(in.hist)-1 {
		in.histPos++
		in.sel.reset()
		in.val = []rune(in.hist[in.histPos])
		in.cur = len(in.val)
		return true
	}
	in.closeMenu()
	in.sel.reset()
	in.val = []rune(in.histDraft)
	in.cur = in.histDraftCur
	in.histPos = -1
	return true
}

// histDetach abandons a running browse: any content edit means the user
// diverged from the recalled entries (the edited buffer is kept as-is).
func (in *inputLine) histDetach() { in.histPos = -1 }

// editSel is one editor's text pick, shared by inputLine and lineEdit
// (the crush / textinput pattern: an anchor holds one end, the caret
// walks the other; the range [anchor, caret) is normalized to [min, max)
// and is "active" only when it holds at least one rune). A plain caret
// move (no shift) resets the pick — the next shift-move re-anchors at
// the caret — and any edit over an active pick replaces it.
type editSel struct {
	anchor int
	on     bool
}

func (s *editSel) reset() {
	s.anchor = 0
	s.on = false
}

// span is the pick in read order: [lo, hi), active when non-empty.
func (s *editSel) span(caret int) (lo, hi int, active bool) {
	if !s.on {
		return caret, caret, false
	}
	lo, hi = s.anchor, caret
	if lo > hi {
		lo, hi = hi, lo
	}
	return lo, hi, hi > lo
}

// text is the picked range as plain text ("" when the pick is empty).
func (s *editSel) text(val []rune, caret int) string {
	lo, hi, ok := s.span(caret)
	if !ok {
		return ""
	}
	return string(val[lo:hi])
}

// insertRune inserts one rune at the caret.
func (in *inputLine) insertRune(r rune) {
	in.insertRunes([]rune{r})
}

// insertRunes inserts runes at the caret, replacing an active pick
// (editor convention: a keystroke over a selection replaces it).
func (in *inputLine) insertRunes(rs []rune) {
	in.histDetach()
	if lo, hi, active := in.sel.span(in.cur); active {
		in.val = append(in.val[:lo], append(rs, in.val[hi:]...)...)
		in.cur = lo + len(rs)
		in.sel.reset()
		return
	}
	in.val = append(in.val[:in.cur], append(rs, in.val[in.cur:]...)...)
	in.cur += len(rs)
}

// clampCaret repairs a caret or pick anchor that outran the buffer
// (a stale index faults the next slice — the multi-line backspace behind
// "slice bounds out of range [3385:3053]"). The insert/delete choke
// points call it before any val slice.
func (in *inputLine) clampCaret() {
	if in.cur > len(in.val) {
		in.cur = len(in.val)
	}
	if in.sel.anchor > len(in.val) {
		in.sel.anchor = len(in.val)
	}
}

// delBack removes the rune before the caret.
func (in *inputLine) delBack() {
	in.histDetach()
	in.clampCaret()
	if lo, hi, active := in.sel.span(in.cur); active {
		in.val = append(in.val[:lo], in.val[hi:]...)
		in.cur = lo
		in.sel.reset()
		return
	}
	if in.cur > 0 {
		in.val = append(in.val[:in.cur-1], in.val[in.cur:]...)
		in.cur--
	}
}

// delFwd removes the rune at the caret.
func (in *inputLine) delFwd() {
	in.histDetach()
	in.clampCaret()
	if lo, hi, active := in.sel.span(in.cur); active {
		in.val = append(in.val[:lo], in.val[hi:]...)
		in.cur = lo
		in.sel.reset()
		return
	}
	if in.cur < len(in.val)-1 {
		in.val = append(in.val[:in.cur], in.val[in.cur+1:]...)
	} else if in.cur == len(in.val)-1 {
		in.val = in.val[:in.cur]
	}
}

// moveLeft / moveRight shift the caret by words.
func (in *inputLine) moveLeft() {
	in.sel.reset()
	if in.cur <= 0 {
		return
	}
	in.cur--
}

func (in *inputLine) moveRight() {
	in.sel.reset()
	if in.cur >= len(in.val) {
		return
	}
	in.cur++
}

func (in *inputLine) moveHome() {
	in.sel.reset()
	in.cur = 0
}
func (in *inputLine) moveEnd() {
	in.sel.reset()
	in.cur = len(in.val)
}

// lineBounds returns the [start, end) rune range of the logical line
// (the '\n'-delimited block) containing index i.
func (in *inputLine) lineBounds(i int) (start, end int) {
	for j := 0; j < i && j < len(in.val); j++ {
		if in.val[j] == '\n' {
			start = j + 1
		}
	}
	end = len(in.val)
	for j := i; j < len(in.val); j++ {
		if in.val[j] == '\n' {
			end = j
			break
		}
	}
	return start, end
}

func (in *inputLine) moveLineStart() {
	in.sel.reset()
	start, _ := in.lineBounds(in.cur)
	in.cur = start
}

func (in *inputLine) moveLineEnd() {
	in.sel.reset()
	_, end := in.lineBounds(in.cur)
	in.cur = end
}

// delToLineEnd removes the runes from the cursor to the end of its line,
// leaving the newline intact.
func (in *inputLine) delToLineEnd() {
	in.histDetach()
	in.sel.reset()
	_, end := in.lineBounds(in.cur)
	if end > in.cur {
		in.val = append(in.val[:in.cur], in.val[end:]...)
	}
}

// ---- text pick (selection) ---------------------------------------------------------

// selActive reports whether the caret + anchor hold a non-empty pick.
func (in *inputLine) selActive() bool {
	_, _, active := in.sel.span(in.cur)
	return active
}

// selText is the picked text ("" when the pick is empty).
func (in *inputLine) selText() string { return in.sel.text(in.val, in.cur) }

// selMove moves the caret to pos while extending the pick: the first
// shift-move after a plain move re-anchors at the caret (editor
// convention — a fresh selection starts where the caret is), later
// shift-moves hold the anchor. No-op when pos == cur.
func (in *inputLine) selMove(pos int) {
	if pos == in.cur {
		return
	}
	if !in.sel.on {
		in.sel.anchor = in.cur
	}
	in.sel.on = true
	in.cur = pos
}

func (in *inputLine) selLeft() {
	if in.cur > 0 {
		in.selMove(in.cur - 1)
	}
}
func (in *inputLine) selRight() {
	if in.cur < len(in.val) {
		in.selMove(in.cur + 1)
	}
}
func (in *inputLine) selHome() { start, _ := in.lineBounds(in.cur); in.selMove(start) }
func (in *inputLine) selEnd()  { _, end := in.lineBounds(in.cur); in.selMove(end) }

// selVert extends the pick to the same column on the adjacent logical
// line (clamped to that line's length); on the first line up holds the
// line start, on the last line down holds the line end.
func (in *inputLine) selVert(dir int) {
	if len(in.val) == 0 {
		return
	}
	start, end := in.lineBounds(in.cur)
	col := in.cur - start
	if dir < 0 {
		if start == 0 {
			in.selMove(0)
			return
		}
		prev := 0
		for j := start - 2; j >= 0; j-- {
			if in.val[j] == '\n' {
				prev = j + 1
				break
			}
		}
		pos := prev + col
		if pos > start-1 {
			pos = start - 1
		}
		in.selMove(pos)
		return
	}
	if end == len(in.val) {
		in.selMove(end)
		return
	}
	nextStart := end + 1
	nextEnd := len(in.val)
	for j := nextStart; j < len(in.val); j++ {
		if in.val[j] == '\n' {
			nextEnd = j
			break
		}
	}
	pos := nextStart + col
	if pos > nextEnd {
		pos = nextEnd
	}
	in.selMove(pos)
}

// selUp / selDown extend the pick one logical line up / down.
func (in *inputLine) selUp()   { in.selVert(-1) }
func (in *inputLine) selDown() { in.selVert(+1) }

// caretVert moves the caret one physical row up / down, keeping the
// column (clamped to a shorter row). It walks the same wrapped rows
// render() draws — explicit newlines and word wrap alike — so the caret
// follows the visible layout; it reports false when the bar is a single
// row or the caret is on the topmost / bottommost row, so the caller can
// fall through to the prompt history. A plain move resets the pick, like
// the horizontal moves.
func (in *inputLine) caretVert(m *Model, dir int) bool {
	ind := inputPromptWidth(m.th)
	// The bar is framed: the wrap width is the inner area (render
	// receives m.W-2; this must match it row for row).
	usable := m.W - 2 - ind
	if usable < 8 {
		usable = 8
	}
	rows := wrapInputText(in.value(), usable)
	if len(rows) <= 1 {
		return false
	}
	// Locate the caret's physical row with render's rule: the row whose
	// [start, start+len) contains the caret (a caret parked on a newline
	// belongs to the row that ends at it). The rows are contiguous with
	// a one-rune gap (the '\n') between them, so at most one matches.
	row := -1
	col := 0
	for i, ln := range rows {
		if in.cur >= ln.start && in.cur <= ln.start+len([]rune(ln.text)) {
			row = i
			col = in.cur - ln.start
			break
		}
	}
	if row < 0 {
		return false
	}
	next := row + dir
	if next < 0 || next >= len(rows) {
		return false
	}
	if cap := len([]rune(rows[next].text)); col > cap {
		col = cap
	}
	in.sel.reset()
	in.cur = rows[next].start + col
	return true
}

// selSetAll picks everything (the caret is parked at the end when the
// pick would come out empty).
func (in *inputLine) selSetAll() {
	if len(in.val) == 0 {
		in.sel.reset()
		return
	}
	if in.cur == 0 {
		in.cur = len(in.val)
	}
	in.sel.anchor = 0
	in.sel.on = true
}

// mousePress places the caret at idx (a plain click: no pick).
func (in *inputLine) mousePress(idx int) {
	if idx > len(in.val) {
		idx = len(in.val)
	}
	in.sel.anchor = idx
	in.sel.on = false
	in.cur = idx
}

// mouseDrag arms the pick and moves the caret to idx; the finished pick
// stays highlighted until cleared (esc / a plain move / a new click).
func (in *inputLine) mouseDrag(idx int) {
	if idx > len(in.val) {
		idx = len(in.val)
	}
	in.sel.on = true
	in.cur = idx
}

// mouseRelease settles a drag: a click that never moved is a plain
// caret placement, a drag keeps its range.
func (in *inputLine) mouseRelease() {
	if in.sel.anchor == in.cur {
		in.sel.on = false
	}
}

// mouseSelectLine picks the whole logical line under the caret (the
// double-click gesture).
func (in *inputLine) mouseSelectLine() {
	start, end := in.lineBounds(in.cur)
	if start == end {
		in.sel.reset()
		return
	}
	in.sel.anchor = start
	in.sel.on = true
	in.cur = end
}

// copySel starts the clipboard copy of the active pick (nil when the
// pick is empty).
func (in *inputLine) copySel(m *Model) tea.Cmd {
	if !in.selActive() {
		return nil
	}
	return m.copyText(in.selText())
}

// paste inserts text at the caret, replacing an active pick. Newlines
// stay: the bar wraps them onto extra rows (the terminal's bracketed
// paste and the clipboard read both flow through here).
func (in *inputLine) paste(text string) {
	if text == "" {
		return
	}
	in.histDetach()
	in.clampCaret()
	text = strings.ReplaceAll(text, "\r\n", "\n")
	in.insertRunes([]rune(text))
	in.sel.reset()
}

// indexAt maps an input-area cell (x from the bar's left edge, y from
// the frame's top row) to a rune index of val. The bar is framed (one
// border cell per side, one border row top and bottom): the top/bottom
// rows take no hit, the side cells land on the nearest text column.
// It reproduces the renderer's word wrap, so a click lands on the glyph
// under the cursor; a click at or past a line's end maps to that line's
// end rune index.
func (in *inputLine) indexAt(m *Model, x, y int) (int, bool) {
	text := in.value()
	ind := inputPromptWidth(m.th)
	usable := m.W - 2 - ind
	if usable < 8 {
		usable = 8
	}
	lines := wrapInputText(text, usable)
	// Framed rows: 0 = top border, 1..len(lines) = the text rows,
	// len(lines)+1 = the bottom border.
	if y < 1 || y > len(lines) {
		return 0, false
	}
	ln := lines[y-1]
	x -= 1
	if x < 0 {
		x = 0
	}
	if x >= m.W-2 {
		x = m.W - 3
	}
	if x < ind {
		x = ind
	}
	return ln.start + runeIndexAt(ln.text, x-ind), true
}

// complete inserts the selected menu item as the leading token.
func (in *inputLine) complete() {
	if !in.menuOpen || len(in.menu) == 0 {
		return
	}
	in.histDetach()
	c := in.menu[in.menuCur]
	in.sel.reset()
	in.val = []rune("/" + c.Name + " ")
	in.cur = len(in.val)
	in.menuOpen = false
}

// handleKey processes one key into the input. It returns:
//
//	ok   — the key was consumed by the input
//	send — enter was pressed with non-empty text
func (in *inputLine) handleKey(m *Model, km tea.KeyMsg) (ok, send bool) {
	in.clampCaret()
	switch {
	case in.menuOpen && km.Type == tea.KeyUp:
		if in.menuCur > 0 {
			in.menuCur--
		}
		return true, false
	case in.menuOpen && km.Type == tea.KeyDown:
		if in.menuCur < len(in.menu)-1 {
			in.menuCur++
		}
		return true, false
	case in.menuOpen && (km.Type == tea.KeyTab || (km.Type == tea.KeyEnter && !km.Alt)):
		c := in.menu[in.menuCur]
		if c.Local {
			// Local commands execute on submit; just complete.
		}
		in.complete()
		return true, c.Local && (strings.TrimSpace(in.value()) == "/"+c.Name)
	case in.atOpen && km.Type == tea.KeyUp:
		if in.atCur > 0 {
			in.atCur--
		}
		return true, false
	case in.atOpen && km.Type == tea.KeyDown:
		if in.atCur < len(in.atMenu)-1 {
			in.atCur++
		}
		return true, false
	case in.atOpen && (km.Type == tea.KeyTab || (km.Type == tea.KeyEnter && !km.Alt)):
		in.completeAt()
		return true, false
	case in.atOpen && km.Type == tea.KeyEsc:
		in.closeAt()
		return true, false
	case km.Type == tea.KeyUp:
		// Prompt history (the slash menu keeps the arrows while open).
		// A multi-line buffer walks its physical rows first: while the
		// caret is off the top edge the arrow moves it within the text,
		// and the history is reached only from the edge. An active
		// browse keeps owning the arrows (whole entries, per the
		// history contract).
		if in.histPos < 0 && in.caretVert(m, -1) {
			return true, false
		}
		if in.histUp() {
			return true, false
		}
		return false, false
	case km.Type == tea.KeyDown:
		// Multi-line first, then history (down at the bottom edge of a
		// non-browsing buffer is a no-op, like the old single-line rule).
		if in.histPos < 0 && in.caretVert(m, +1) {
			return true, false
		}
		if in.histDown() {
			return true, false
		}
		return false, false
	case km.Type == tea.KeyTab:
		// Plain tab (menu closed): literal tab character.
		in.insertRune('\t')
		return true, false
	case km.Type == tea.KeyCtrlJ:
		// Shift+Enter: terminals send a bare LF (0x0A — the ctrl+j byte,
		// indistinguishable from a physical ctrl+j) for it, and the kitty
		// protocol's ESC[13;2u is routed here from Update. A manual newline,
		// never a send.
		in.insertRune('\n')
		return true, false
	case km.Type == tea.KeyEnter:
		if km.Alt {
			// Alt+Enter forces a queued send even mid-run (a plain Enter
			// would steer); submit consumes the flag.
			in.forceQueue = true
		}
		if !in.empty() {
			return true, true
		}
		return true, false
	case km.Type == tea.KeyBackspace || km.String() == "\b":
		in.delBack()
		return true, false
	case km.Type == tea.KeyCtrlA:
		in.moveLineStart()
		return true, false
	case km.Type == tea.KeyCtrlE:
		in.moveLineEnd()
		return true, false
	case km.Type == tea.KeyCtrlB:
		in.moveLeft()
		return true, false
	case km.Type == tea.KeyCtrlF:
		in.moveRight()
		return true, false
	case km.Type == tea.KeyCtrlD:
		in.delFwd()
		return true, false
	case km.Type == tea.KeyCtrlU || km.Type == tea.KeyCtrlK:
		// ctrl+k mirrors ctrl+u (both kill to the line end).
		in.delToLineEnd()
		return true, false
	case km.Type == tea.KeySpace:
		// bubbletea delivers the space bar as a dedicated key type.
		in.insertRune(' ')
		return true, false
	case km.Type == tea.KeyDelete:
		in.delFwd()
		return true, false
	case km.Type == tea.KeyShiftLeft:
		// Extend the pick (crush's textarea pattern): shift+move arms
		// the anchor and walks the caret.
		in.selLeft()
		return true, false
	case km.Type == tea.KeyShiftRight:
		in.selRight()
		return true, false
	case km.Type == tea.KeyShiftHome:
		in.selHome()
		return true, false
	case km.Type == tea.KeyShiftEnd:
		in.selEnd()
		return true, false
	case km.Type == tea.KeyShiftUp:
		in.selUp()
		return true, false
	case km.Type == tea.KeyShiftDown:
		in.selDown()
		return true, false
	case km.Type == tea.KeyLeft:
		in.moveLeft()
		return true, false
	case km.Type == tea.KeyRight:
		in.moveRight()
		return true, false
	case km.Type == tea.KeyHome:
		in.moveHome()
		return true, false
	case km.Type == tea.KeyEnd:
		in.moveEnd()
		return true, false
	case km.Type == tea.KeyRunes:
		for _, r := range km.Runes {
			in.insertRune(r)
		}
		return true, false
	}
	return false, false
}

// render draws the input bar's inner rows: the accent prompt, the typed
// text and the caret (the char under the cursor in the bar's accent with
// an underline — or an underlined space at the line end; no background
// anywhere). The caller frames the rows (layout's frameInput); width is
// the inner width. Every segment carries the bar background — empty on
// the built-in palettes, where the whole bar (text, caret and padding
// alike) rides the terminal's own background — so no internal ANSI reset
// can leave a strip of a foreign background behind the typed text.
func (in *inputLine) render(m *Model, width int) []string {
	th := m.th
	text := in.value()

	prompt := th.Glyph.Caret + " "
	ind := runewidth.StringWidth(prompt)
	usable := width - ind
	if usable < 8 {
		usable = 8
	}

	lines := wrapInputText(text, usable)

	plain := th.Bar()
	barSel := th.BarSel()
	accent := th.BarAccent()
	cursor := th.BarCursor()
	surface := th.BarBG()
	caretGiven := false
	var out []string
	lo, hi, active := in.sel.span(in.cur)
	for i, ln := range lines {
		line := ""
		if i == 0 {
			line += accent.Render(prompt)
		} else {
			line += surface.Render(strings.Repeat(" ", ind))
		}
		r := []rune(ln.text)
		cpos := -1
		// The open session window owns the keyboard: its search line
		// carries the live caret, so the main bar's caret stands down.
		if !caretGiven && !m.sideVisible &&
			in.cur >= ln.start && in.cur <= ln.start+len(ln.text) {
			cpos = in.cur - ln.start
			caretGiven = true
		}
		// The pick segment intersecting this physical row (render() clips
		// it to the row the same way the renderer wraps the text).
		slo, sha := 0, 0
		if active {
			if lo < ln.start {
				lo = ln.start
			}
			if hi > ln.start+len(r) {
				hi = ln.start + len(r)
			}
			if lo < hi {
				slo, sha = lo-ln.start, hi-ln.start
			}
		}
		line += segmentRunes(r, cpos, slo, sha, plain, barSel, cursor)
		cw := ind + runewidth.StringWidth(ln.text)
		if cpos >= 0 && cpos >= len([]rune(ln.text)) {
			cw++ // the end-of-line caret cell is an added space
		}
		if cw < width {
			line += surface.Render(strings.Repeat(" ", width-cw))
		}
		out = append(out, line)
	}
	return out
}

// wline is one physical row of the input bar's word wrap: the row text
// and the rune index in the bar where it begins. render() draws from it
// and indexAt() maps clicks back through it, so the two stay in lockstep
// by construction.
type wline struct {
	text  string
	start int // rune index in the bar text
}

// wrapInputText word-wraps the bar text exactly the way render() draws
// it: explicit newlines become line breaks, and a row that overflows the
// usable width starts a new physical row.
func wrapInputText(text string, usable int) []wline {
	var lines []wline
	var cur []rune
	start := 0
	ww := 0
	for i, r := range text {
		if r == '\n' {
			lines = append(lines, wline{string(cur), start})
			cur = nil
			start = i + 1
			ww = 0
			continue
		}
		rw := runewidth.RuneWidth(r)
		if ww+rw > usable && len(cur) > 0 {
			lines = append(lines, wline{string(cur), start})
			cur = nil
			start = i
			ww = 0
		}
		cur = append(cur, r)
		ww += rw
	}
	lines = append(lines, wline{string(cur), start})
	return lines
}

// inputPromptWidth is the display width of the bar's prompt prefix
// (the accent caret + the gap); the prefix occupies the same cells on
// every physical row, the prompt glyph only on the first.
func inputPromptWidth(th *Theme) int {
	return runewidth.StringWidth(th.Glyph.Caret + " ")
}

// runeIndexAt maps a display column within one physical row to the rune
// boundary the cursor should take: a column at or before a rune's start
// lands before it, a column inside a wide rune lands after it (clicking
// the second cell of a CJK glyph takes the whole glyph with the pointer).
func runeIndexAt(row string, colx int) int {
	r := []rune(row)
	col := 0
	for i, ch := range r {
		w := runewidth.RuneWidth(ch)
		if w < 0 {
			w = 0
		}
		if colx <= col {
			return i
		}
		if colx > col+w-1 {
			col += w
			continue
		}
		return i + 1
	}
	return len(r)
}

// segmentRunes renders one row's runes on the editor's surface: plain
// ink except the underlined pick over [lo, hi) and the accent underlined
// caret at caret (caret == -1 draws no caret: the caret is painted once,
// on the row that holds it). Single-style runs are grouped, so the SGR
// churn stays at a few transitions per row.
func segmentRunes(r []rune, caret, lo, hi int, plain, selStyle, cursor Style) string {
	var b strings.Builder
	mode := ""
	run := ""
	flush := func() {
		if run == "" {
			return
		}
		switch mode {
		case "sel":
			b.WriteString(selStyle.Render(run))
		case "cursor":
			b.WriteString(cursor.Render(run))
		default:
			b.WriteString(plain.Render(run))
		}
		run = ""
	}
	for i, ch := range r {
		md := "plain"
		if i >= lo && i < hi {
			md = "sel"
		}
		if i == caret {
			md = "cursor"
		}
		if md != mode {
			flush()
			mode = md
		}
		run += string(ch)
	}
	flush()
	if caret == len(r) {
		// The caret at a row end is an underlined cell of its own.
		b.WriteString(cursor.Render(" "))
	}
	return b.String()
}

// lineEdit is one line of text edited with the main input box's semantics:
// a rune caret plus the ctrl+a/e/b/f/d/u bindings (left/right/home/end
// included). Modal text fields embed it, so they edit exactly like the
// main input bar.
type lineEdit struct {
	val []rune
	cur int
	sel editSel // text pick (same semantics as the main input bar)
}

func (e *lineEdit) reset()        { e.val, e.cur = nil, 0 }
func (e lineEdit) string() string { return string(e.val) }

func (e *lineEdit) insert(ch rune) {
	e.insertRunes([]rune{ch})
}

// insertRunes inserts runes at the caret, replacing an active pick
// (editor convention: a keystroke over a selection replaces it).
func (e *lineEdit) insertRunes(rs []rune) {
	if lo, hi, active := e.sel.span(e.cur); active {
		e.val = append(e.val[:lo], append(rs, e.val[hi:]...)...)
		e.cur = lo + len(rs)
		e.sel.reset()
		return
	}
	e.val = append(e.val[:e.cur], append(rs, e.val[e.cur:]...)...)
	e.cur += len(rs)
}

// paste inserts text at the caret, replacing an active pick. Newlines
// become spaces: the field is one line. It reports whether anything was
// inserted (an empty paste is dropped, no error of its own).
func (e *lineEdit) paste(text string) bool {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if text == "" {
		return false
	}
	var out []rune
	for _, r := range text {
		if r == '\n' || r == '\r' {
			out = append(out, ' ')
			continue
		}
		out = append(out, r)
	}
	e.insertRunes(out)
	return true
}

func (e *lineEdit) delBack() {
	if lo, hi, active := e.sel.span(e.cur); active {
		e.val = append(e.val[:lo], e.val[hi:]...)
		e.cur = lo
		e.sel.reset()
		return
	}
	if e.cur > 0 {
		e.val = append(e.val[:e.cur-1], e.val[e.cur:]...)
		e.cur--
	}
}

func (e *lineEdit) delFwd() {
	if lo, hi, active := e.sel.span(e.cur); active {
		e.val = append(e.val[:lo], e.val[hi:]...)
		e.cur = lo
		e.sel.reset()
		return
	}
	if e.cur < len(e.val)-1 {
		e.val = append(e.val[:e.cur], e.val[e.cur+1:]...)
	} else if e.cur == len(e.val)-1 {
		e.val = e.val[:e.cur]
	}
}

func (e *lineEdit) moveLeft() {
	e.sel.reset()
	if e.cur > 0 {
		e.cur--
	}
}

func (e *lineEdit) moveRight() {
	e.sel.reset()
	if e.cur < len(e.val) {
		e.cur++
	}
}

func (e *lineEdit) home() {
	e.sel.reset()
	e.cur = 0
}
func (e *lineEdit) end() {
	e.sel.reset()
	e.cur = len(e.val)
}

// killToEnd removes the runes from the cursor to the end of the text.
func (e *lineEdit) killToEnd() {
	e.sel.reset()
	if len(e.val) > e.cur {
		e.val = e.val[:e.cur]
	}
}

// ---- text pick (selection) ---------------------------------------------------------

// selActive reports whether the caret + anchor hold a non-empty pick.
func (e *lineEdit) selActive() bool {
	_, _, active := e.sel.span(e.cur)
	return active
}

// selText is the picked text ("" when the pick is empty).
func (e *lineEdit) selText() string { return e.sel.text(e.val, e.cur) }

func (e *lineEdit) selMove(pos int) {
	if pos == e.cur {
		return
	}
	if !e.sel.on {
		e.sel.anchor = e.cur
	}
	e.sel.on = true
	e.cur = pos
}

func (e *lineEdit) selLeft() {
	if e.cur > 0 {
		e.selMove(e.cur - 1)
	}
}
func (e *lineEdit) selRight() {
	if e.cur < len(e.val) {
		e.selMove(e.cur + 1)
	}
}
func (e *lineEdit) selHome() { e.selMove(0) }
func (e *lineEdit) selEnd()  { e.selMove(len(e.val)) }

// selSetAll picks everything (the caret is parked at the end when the
// pick would come out empty).
func (e *lineEdit) selSetAll() {
	if len(e.val) == 0 {
		e.sel.reset()
		return
	}
	if e.cur == 0 {
		e.cur = len(e.val)
	}
	e.sel.anchor = 0
	e.sel.on = true
}

// mousePress places the caret at idx (a plain click: no pick).
func (e *lineEdit) mousePress(idx int) {
	e.sel.anchor = idx
	e.sel.on = false
	e.cur = idx
}

// mouseDrag arms the pick and moves the caret to idx.
func (e *lineEdit) mouseDrag(idx int) {
	e.sel.on = true
	e.cur = idx
}

// mouseRelease settles a drag (a moved-less click is a plain caret).
func (e *lineEdit) mouseRelease() {
	if e.sel.anchor == e.cur {
		e.sel.on = false
	}
}

// mouseSelectLine picks the whole field (the double-click gesture; the
// field is one line, so a line pick is a select-all).
func (e *lineEdit) mouseSelectLine() { e.selSetAll() }

// copySel starts the clipboard copy of the active pick (nil when the
// pick is empty).
func (e *lineEdit) copySel(m *Model) tea.Cmd {
	if !e.selActive() {
		return nil
	}
	return m.copyText(e.selText())
}

// handleKey applies the main input box's editor bindings to the field.
// It reports whether the key was consumed.
func (e *lineEdit) handleKey(km tea.KeyMsg) bool {
	switch {
	case km.Type == tea.KeyCtrlA:
		e.home()
		return true
	case km.Type == tea.KeyCtrlE:
		e.end()
		return true
	case km.Type == tea.KeyCtrlB || km.Type == tea.KeyLeft:
		e.moveLeft()
		return true
	case km.Type == tea.KeyCtrlF || km.Type == tea.KeyRight:
		e.moveRight()
		return true
	case km.Type == tea.KeyShiftLeft:
		e.selLeft()
		return true
	case km.Type == tea.KeyShiftRight:
		e.selRight()
		return true
	case km.Type == tea.KeyShiftHome:
		e.selHome()
		return true
	case km.Type == tea.KeyShiftEnd:
		e.selEnd()
		return true
	case km.Type == tea.KeyCtrlD || km.Type == tea.KeyDelete:
		e.delFwd()
		return true
	case km.Type == tea.KeyCtrlU || km.Type == tea.KeyCtrlK:
		// ctrl+k mirrors ctrl+u (both kill to the end of the field).
		e.killToEnd()
		return true
	case km.Type == tea.KeyHome:
		e.home()
		return true
	case km.Type == tea.KeyEnd:
		e.end()
		return true
	case km.Type == tea.KeyBackspace || km.String() == "\b":
		e.delBack()
		return true
	case km.Type == tea.KeySpace:
		e.insert(' ')
		return true
	case km.Type == tea.KeyRunes:
		for _, r := range km.Runes {
			e.insert(r)
		}
		return true
	}
	return false
}

// cardEditLine renders a card-surface text field (the session list's
// search line): unfocused, plain card ink with no caret; focused, the
// main input box's caret — inverted cell on the card — plus the pick
// band, so the side search edits with the identical look and feel.
func cardEditLine(th *Theme, focused bool, e *lineEdit) string {
	if !focused {
		return th.CardStyle(th.Plain()).Render(string(e.val))
	}
	lo, hi, active := e.sel.span(e.cur)
	if !active {
		lo, hi = 0, 0
	}
	return segmentRunes(e.val, e.cur, lo, hi, th.CardStyle(th.Plain()), th.CardSel(), th.CardCursor())
}

// localCommandSpecs is the TUI's own command set in key form: the menu
// descriptions are catalog lines, the names stay literal (typing
// /quit must work on every face).
type localCommandSpec struct {
	name    string
	local   bool
	descKey string
}

var localCommandSpecs = []localCommandSpec{
	{"help", true, "cmd.help.desc"},
	{"status", true, "cmd.status.desc"},
	{"new", true, "cmd.new.desc"},
	{"title", true, "cmd.title.desc"},
	{"model", true, "cmd.model.desc"},
	{"mode", true, "cmd.mode.desc"},
	{"search", true, "cmd.search.desc"},
	{"cancel", true, "cmd.cancel.desc"},
	{"detail", true, "cmd.detail.desc"},
	{"permission", true, "cmd.permission.desc"},
	{"workspace", true, "cmd.workspace.desc"},
	{"language", true, "cmd.language.desc"},
	{"quit", true, "cmd.quit.desc"},
	{"plan", false, "cmd.plan.desc"},
	{"compact", false, "cmd.compact.desc"},
	{"goal", false, "cmd.goal.desc"},
}

// localCommandsFor builds the set with one locale's descriptions.
func localCommandsFor(l *i18n.Locale) []slashCmd {
	out := make([]slashCmd, len(localCommandSpecs))
	for i, c := range localCommandSpecs {
		out[i] = slashCmd{Name: c.name, Local: c.local, Desc: l.T(c.descKey)}
	}
	return out
}

// localCommands is the English set: the slash-menu tests pin against it.
var localCommands = localCommandsFor(nil)

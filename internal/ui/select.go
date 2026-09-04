// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/clipperhouse/displaywidth"
	"github.com/clipperhouse/uax29/v2/words"
	"github.com/mattn/go-runewidth"
)

// textSel is the transcript text pick as a cell range (crush's
// pattern, adapted to the flat rendered-line model): a left press anchors
// at (line, col), a drag extends to a (line, col) endpoint, and the
// finished pick stays banded until esc, a new press, or its copy lands.
// Columns are display cells (wide runes count double) and the endpoints
// are inclusive; a press that never moves is a zero-size anchor, so a
// plain click paints no band and copies nothing. Double-click selects the
// word under the cursor, triple-click the whole line (crush's click
// ladder; word bounds come from UAX#29 segmentation).
type textSel struct {
	anchorLine, anchorCol int  // where the left press landed
	dragLine, dragCol     int  // live drag endpoint (tracks the anchor until it moves)
	down                  bool // left button held
	moved                 bool // drag moved past the anchor (or a word/line pick)
	active                bool // the pick has size (drives the band and the copy)
	copied                bool // a copy of this pick is in flight (cleared on the reply)
}

func (s *textSel) reset() {
	*s = textSel{}
}

// bounds returns the pick as (firstLine, firstCol, lastLine, lastCol) in
// read order (-1s when zero-size), clamped to the n-line content. crush's
// getHighlightRange: forward order follows the anchor-to-drag direction,
// reversed when the drag went up or left over the anchor.
func (s *textSel) bounds(n int) (firstLine, firstCol, lastLine, lastCol int) {
	if n <= 0 || !s.moved {
		return -1, 0, -1, 0
	}
	aL, aC, dL, dC := s.anchorLine, s.anchorCol, s.dragLine, s.dragCol
	if aL > dL || (aL == dL && aC > dC) {
		aL, aC, dL, dC = dL, dC, aL, aC
	}
	if aL < 0 {
		aL = 0
	}
	if dL >= n {
		dL = n - 1
	}
	if aC < 0 {
		aC = 0
	}
	if dC < 0 {
		dC = 0
	}
	return aL, aC, dL, dC
}

// anchor drops a zero-size pick at (line, col) — a plain single click.
func (s *textSel) anchor(line, col int) {
	*s = textSel{anchorLine: line, anchorCol: col, dragLine: line, dragCol: col, down: true}
}

// dragTo extends the living pick to (line, col).
func (s *textSel) dragTo(line, col int) {
	if !s.down {
		return
	}
	s.dragLine, s.dragCol = line, col
	s.moved = line != s.anchorLine || col != s.anchorCol
	s.active = s.moved
}

// selectWord picks the word under (line, col) (crush's double-click); a
// click on whitespace falls back to a plain anchor.
func (s *textSel) selectWord(line, col int, lineText string) {
	ws, we := wordBounds(lineText, col)
	if ws == we {
		s.anchor(line, col)
		return
	}
	*s = textSel{anchorLine: line, anchorCol: ws, dragLine: line, dragCol: we,
		down: true, moved: true, active: true}
}

// selectLine picks the whole rendered line (crush's triple-click).
func (s *textSel) selectLine(line, width int) {
	last := width - 1
	if last < 0 {
		last = 0
	}
	*s = textSel{anchorLine: line, anchorCol: 0, dragLine: line, dragCol: last,
		down: true, moved: true, active: width > 0}
}

// ---- SGR state -------------------------------------------------------------

// sgrState is a terminal cell's style state: the attribute bits a
// terminal tracks plus the foreground/background as the raw SGR parameter
// lists that set them (plain [31], 8-bit [38;5;n], truecolor
// [38;2;r;g;b]). The row scan and the band speak only this state, so a
// styled row re-emits each cell under the style it carries — the same
// cell-level surgery crush does with ultraviolet's ScreenBuffer, without
// the dependency.
type sgrState struct {
	bold, dim, italic, underline     bool
	blink, inverse, strike, overline bool
	fg, bg                           []int
}

func (s *sgrState) empty() bool {
	return !s.bold && !s.dim && !s.italic && !s.underline &&
		!s.blink && !s.inverse && !s.strike && !s.overline &&
		len(s.fg) == 0 && len(s.bg) == 0
}

func (s sgrState) equals(o sgrState) bool {
	return s.bold == o.bold && s.dim == o.dim && s.italic == o.italic &&
		s.underline == o.underline && s.blink == o.blink && s.inverse == o.inverse &&
		s.strike == o.strike && s.overline == o.overline &&
		eqParams(s.fg, o.fg) && eqParams(s.bg, o.bg)
}

func eqParams(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// apply folds one SGR parameter list into the state with terminal SGR
// semantics: 0 resets everything mid-sequence, attributes toggle, colors
// stick, and 38/48 consume their 5-index or 2-truecolor sub-list.
func (s *sgrState) apply(p []int) {
	for i := 0; i < len(p); {
		v := p[i]
		switch v {
		case 0:
			*s = sgrState{}
		case 1:
			s.bold = true
		case 2:
			s.dim = true
		case 3:
			s.italic = true
		case 4:
			s.underline = true
		case 5:
			s.blink = true
		case 7:
			s.inverse = true
		case 9:
			s.strike = true
		case 21:
			s.dim = false
		case 22:
			s.bold, s.dim = false, false
		case 23:
			s.italic = false
		case 24:
			s.underline = false
		case 25:
			s.blink = false
		case 27:
			s.inverse = false
		case 29:
			s.strike = false
		case 53:
			s.overline = true
		case 54:
			s.overline = false
		case 39:
			s.fg = nil
		case 49:
			s.bg = nil
		case 99:
			s.fg = nil
		case 109:
			s.bg = nil
		case 38, 48:
			// 38;5;n or 38;2;r;g;b (48 is the background twin).
			target := &s.fg
			if v == 48 {
				target = &s.bg
			}
			switch {
			case i+2 < len(p) && p[i+1] == 5:
				*target = []int{v, 5, p[i+2]}
				i += 2
			case i+4 < len(p) && p[i+1] == 2:
				*target = []int{v, 2, p[i+2], p[i+3], p[i+4]}
				i += 4
			}
		default:
			switch {
			case v >= 30 && v <= 37:
				s.fg = []int{v}
			case v >= 40 && v <= 47:
				s.bg = []int{v}
			case v >= 90 && v <= 97:
				s.fg = []int{v}
			case v >= 100 && v <= 107:
				s.bg = []int{v}
			}
		}
		i++
	}
}

// marshal renders the state as one SGR escape ("" for the default state;
// callers emit a reset instead).
func (s *sgrState) marshal() string {
	if s.empty() {
		return ""
	}
	p := []int{0}
	if s.bold {
		p = append(p, 1)
	}
	if s.dim {
		p = append(p, 2)
	}
	if s.italic {
		p = append(p, 3)
	}
	if s.underline {
		p = append(p, 4)
	}
	if s.blink {
		p = append(p, 5)
	}
	if s.inverse {
		p = append(p, 7)
	}
	if s.strike {
		p = append(p, 9)
	}
	if s.overline {
		p = append(p, 53)
	}
	p = append(p, s.fg...)
	p = append(p, s.bg...)
	var b strings.Builder
	b.WriteByte('\x1b')
	b.WriteByte('[')
	for i, v := range p {
		if i > 0 {
			b.WriteByte(';')
		}
		b.WriteString(strconv.Itoa(v))
	}
	b.WriteByte('m')
	return b.String()
}

func (s *sgrState) marshalOrReset() string {
	if em := s.marshal(); em != "" {
		return em
	}
	return "\x1b[0m"
}

// withBand overlays the pick band on the cell's own style (crush's
// highlighter merges the selection style into each covered cell): the
// band background replaces the cell's, or the cell flips to reverse video
// where the theme has no palette to lean on.
func (s sgrState) withBand(b sgrState) sgrState {
	if len(b.bg) > 0 {
		s.bg = b.bg
	}
	if b.inverse {
		s.inverse = true
	}
	return s
}

// withCard bakes the card surface into a cell that carries no background
// of its own (the popup's hole cells): cells with their own surface —
// the pick band, the edit field, the caret — keep it.
func (s sgrState) withCard(card sgrState) sgrState {
	if len(s.bg) == 0 && len(card.bg) > 0 {
		s.bg = card.bg
	}
	return s
}

// ---- cell scan --------------------------------------------------------------

// styleCell is one display cell of a styled line: the glyph (a zero-width
// combiner repeats the previous cell's column), its display width, the
// 0-based column it occupies and the SGR state that applies to it.
type styleCell struct {
	r     rune
	w     int
	col   int
	state sgrState
}

// scanStyled splits a styled line into display cells while walking the SGR
// state: an ESC[...m run updates it, every other escape run is skipped,
// wide runes count double (crush decodes the same content through
// ultraviolet; a small state machine keeps the dependency graph short).
func scanStyled(line string) []styleCell {
	var cells []styleCell
	var st sgrState
	col, i := 0, 0
	for i < len(line) {
		c0 := line[i]
		if c0 == 0x1b {
			switch {
			case i+1 < len(line) && line[i+1] == '[':
				j := i + 2
				for j < len(line) && line[j] >= 0x20 && line[j] <= 0x3f {
					j++
				}
				if j >= len(line) {
					i = len(line)
				} else {
					if line[j] == 'm' {
						st.apply(parseSGRParams(line[i+2 : j]))
					}
					i = j + 1
				}
			case i+2 < len(line) && (line[i+2] == ']' || line[i+2] == 'P'):
				// OSC (hyperlinks, title) and DCS runs end at BEL or (ESC \).
				rest := line[i+3:]
				if k := strings.IndexByte(rest, 0x07); k >= 0 {
					i += 4 + k
				} else if k := strings.Index(rest, "\x1b\\"); k >= 0 {
					i += 4 + k
				} else {
					i = len(line)
				}
			default:
				i += 2
			}
			continue
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		cw := runewidth.RuneWidth(r)
		if cw < 0 {
			cw = 0
		}
		cells = append(cells, styleCell{r: r, w: cw, col: col, state: st})
		i += size
		col += cw
	}
	return cells
}

// parseSGRParams parses the parameter bytes of an ESC[m run (blank slots
// are SGR-0, a bare ESC[m the empty list that means reset).
func parseSGRParams(s string) []int {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ";")
	p := make([]int, len(parts))
	for i, part := range parts {
		if part == "" {
			continue
		}
		v, _ := strconv.Atoi(part)
		p[i] = v
	}
	return p
}

// extractSGR pulls the first ESC[...m parameter list out of a styled probe
// (a one-cell render of the band style).
func extractSGR(s string) sgrState {
	var st sgrState
	for i := 0; i+1 < len(s); i++ {
		if s[i] != 0x1b {
			continue
		}
		if s[i+1] != '[' {
			continue
		}
		j := i + 2
		for j < len(s) && s[j] >= 0x20 && s[j] <= 0x3f {
			j++
		}
		if j < len(s) && s[j] == 'm' {
			st.apply(parseSGRParams(s[i+2 : j]))
			return st
		}
	}
	return st
}

// ---- words ------------------------------------------------------------------

// visibleText is the display text of a styled line (CSI escapes stripped):
// word boundaries and pick math work in display columns, not raw bytes
// (crush ansi.Strips the rendered item row before findWordBoundaries).
func visibleText(line string) string {
	return ansiSeqRe.ReplaceAllString(line, "")
}

// wordBounds returns the inclusive display-column range of the word under
// col (crush's findWordBoundaries): the line is segmented on UAX#29 word
// boundaries and token widths count in display cells (wide runes double).
// A click on whitespace returns a zero range.
func wordBounds(line string, col int) (int, int) {
	if line == "" || col < 0 {
		return 0, 0
	}
	lineCol, lastCol := 0, 0
	iter := words.FromString(line)
	for iter.Next() {
		token := iter.Value()
		gStart := lineCol
		gEnd := lineCol + displaywidth.String(token)
		lineCol = gEnd
		if col < gStart {
			return lastCol, lastCol
		}
		lastCol = gEnd
		if col >= gStart && col < gEnd {
			if strings.TrimSpace(token) == "" {
				return col, col
			}
			return gStart, gEnd - 1
		}
	}
	return col, col
}

// ---- pick text ---------------------------------------------------------------

// rowText is the plain text of a displayed line clipped to the inclusive
// column range: a glyph is picked when its first column is covered (the
// same rule the band applies, ultraviolet-cell semantics), trailing pad
// stripped (renderers pad rows to the pane width; nobody wants that
// pasted).
func rowText(line string, cs, ce int) string {
	if cs < 0 {
		cs = 0
	}
	var b strings.Builder
	for _, c := range scanStyled(line) {
		if c.r != 0 && c.col >= cs && c.col <= ce {
			b.WriteRune(c.r)
		}
	}
	return strings.TrimRight(b.String(), " \t")
}

// joinRows stitches clipped screen rows back into text, deciding per row
// boundary whether it was a real newline or a renderer word wrap (crush's
// list.joinRows ported): the renderer fills wrapped rows to most of the
// width, so a near-full row continues on the next one with a space; a
// blank row is a paragraph break; a row that starts a new markdown block
// (bullet, heading, ordered item) always begins its own line.
func joinRows(rows []string, width int) string {
	var sb strings.Builder
	for i, row := range rows {
		text := strings.TrimRight(row, " ")
		sb.WriteString(text)
		if i == len(rows)-1 {
			break
		}
		next := rows[i+1]
		switch {
		case strings.TrimSpace(next) == "":
			sb.WriteString("\n")
		case isWordWrap(text, next, width):
			sb.WriteString(" ")
		default:
			sb.WriteString("\n")
		}
	}
	return strings.TrimSpace(sb.String())
}

func isWordWrap(text, next string, width int) bool {
	if startsBlock(next) {
		return false
	}
	return width > 0 && plainWidth(text) >= width*3/5
}

func startsBlock(row string) bool {
	if row != strings.TrimLeft(row, " ") {
		return false
	}
	switch {
	case strings.HasPrefix(row, "- "), strings.HasPrefix(row, "* "),
		strings.HasPrefix(row, "+ "), strings.HasPrefix(row, "• "),
		strings.HasPrefix(row, "#"):
		return true
	}
	if i := strings.IndexAny(row, ".)"); i > 0 && i < 4 {
		for j := range i {
			if row[j] < '0' || row[j] > '9' {
				return false
			}
		}
		return true
	}
	return false
}

// ---- clipboard ---------------------------------------------------------------

type clipTool struct {
	path string
	args []string
}

// findClipTool resolves the platform clipboard tool: wl-copy on Wayland,
// pbcopy on macOS, clip.exe on Windows, xclip elsewhere (X11). None found
// returns the zero value.
func findClipTool() clipTool {
	var candidates []clipTool
	switch {
	case runtime.GOOS == "darwin":
		candidates = []clipTool{{path: "pbcopy"}}
	case runtime.GOOS == "windows":
		candidates = []clipTool{{path: "clip.exe"}}
	case os.Getenv("WAYLAND_DISPLAY") != "" || os.Getenv("XDG_SESSION_TYPE") == "wayland":
		candidates = []clipTool{{path: "wl-copy"}, {path: "xclip", args: []string{"-selection", "clipboard"}}}
	default:
		candidates = []clipTool{{path: "xclip", args: []string{"-selection", "clipboard"}}, {path: "wl-copy"}}
	}
	for _, t := range candidates {
		if p, err := exec.LookPath(t.path); err == nil {
			return clipTool{path: p, args: t.args}
		}
	}
	return clipTool{}
}

// findClipReadTool resolves the platform clipboard *read* tool (the
// counterpart of findClipTool): pbpaste on macOS, Get-Clipboard via
// PowerShell on Windows, wl-paste on Wayland, xclip -o elsewhere.
func findClipReadTool() clipTool {
	var candidates []clipTool
	switch {
	case runtime.GOOS == "darwin":
		candidates = []clipTool{{path: "pbpaste"}}
	case runtime.GOOS == "windows":
		candidates = []clipTool{{path: "powershell", args: []string{"-NoProfile", "-Command", "Get-Clipboard"}}}
	case os.Getenv("WAYLAND_DISPLAY") != "" || os.Getenv("XDG_SESSION_TYPE") == "wayland":
		candidates = []clipTool{{path: "wl-paste"}, {path: "xclip", args: []string{"-o", "-selection", "clipboard"}}}
	default:
		candidates = []clipTool{{path: "xclip", args: []string{"-o", "-selection", "clipboard"}}, {path: "wl-paste"}}
	}
	for _, t := range candidates {
		if p, err := exec.LookPath(t.path); err == nil {
			return clipTool{path: p, args: t.args}
		}
	}
	return clipTool{}
}

// osc52Payload encodes text for the terminal's OSC 52 clipboard channel.
func osc52Payload(text string) string {
	return fmt.Sprintf("\x1b]52;c;%s\a", base64.StdEncoding.EncodeToString([]byte(text)))
}

// oscCmd writes its payload once the renderer is parked (tea.Exec).
type oscCmd struct{ payload string }

func (c oscCmd) Run() error          { _, err := os.Stdout.Write([]byte(c.payload)); return err }
func (c oscCmd) SetStdin(io.Reader)  {}
func (c oscCmd) SetStdout(io.Writer) {}
func (c oscCmd) SetStderr(io.Writer) {}

// baseName is the file name of a path (clipboard binary label for toasts).
func baseName(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

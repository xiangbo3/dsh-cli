// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unsafe"

	"dsh-cli/internal/core"

	"dsh-cli/internal/textutil"

	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/muesli/reflow/wordwrap"
)

// transCache holds the rendered transcript lines of one session. Per item
// it caches the pointer (identity across reloads), the transcript Gen
// (unique per item, immune to GC address reuse) and the Ver mutation
// counter, so a dirty pulse re-renders only the rows that actually
// changed instead of re-hashing the whole transcript.
type transCache struct {
	sessID      string
	width       int
	verbose     bool
	label       string // model label baked into assistant headers; a change invalidates all
	itemsRender [][]string
	ptrs        []uintptr
	gens        []int
	vers        []int
	// Per-slot streaming throttle state: lastRender when the rows last
	// rendered, lastLen the rendered content size, pending a skipped
	// redraw that an item-set shape change (finalization) must flush.
	lastRender []time.Time
	lastLen    []int
	pending    []bool
	lines      []string
}

// Streaming throttle bounds: a whole-item re-render happens only once at
// least throttleInterval has passed or the content grew by at least
// throttleGrowth bytes — small deltas in between keep the previous rows.
// The finalization flush in apply guarantees the settled text is always
// painted.
const (
	throttleInterval = 100 * time.Millisecond
	throttleGrowth   = 32
)

// itemAddr is the render-identity handle of one item.
func itemAddr(it *core.Item) uintptr { return uintptr(unsafe.Pointer(it)) }

func newTransCache() *transCache { return &transCache{} }

// apply resynchronizes the cache with the current items. It returns true
// when the line set changed.
func (c *transCache) apply(m *Model, items []*core.Item) bool {
	changed := false
	now := time.Now()
	if c.sessID != m.activeID() || c.width != m.transcriptWidth() || c.verbose != m.verbose ||
		c.label != m.ctxLabel() {
		c.sessID = m.activeID()
		c.width = m.transcriptWidth()
		c.verbose = m.verbose
		c.label = m.ctxLabel()
		c.itemsRender = nil
		c.ptrs = nil
		c.gens = nil
		c.vers = nil
		c.lastRender = nil
		c.lastLen = nil
		c.pending = nil
		c.lines = nil
		changed = true
	}
	structural := len(items) != len(c.gens)
	if n := len(items); n < len(c.itemsRender) {
		c.itemsRender = c.itemsRender[:n]
		c.ptrs = c.ptrs[:n]
		c.gens = c.gens[:n]
		c.vers = c.vers[:n]
		c.lastRender = c.lastRender[:n]
		c.lastLen = c.lastLen[:n]
		c.pending = c.pending[:n]
		changed = true
	}
	for i := len(c.gens); i < len(items); i++ {
		c.itemsRender = append(c.itemsRender, renderItem(m, items[i], c.width, c.label))
		c.ptrs = append(c.ptrs, itemAddr(items[i]))
		c.gens = append(c.gens, items[i].Gen)
		c.vers = append(c.vers, items[i].Ver)
		c.lastRender = append(c.lastRender, now)
		c.lastLen = append(c.lastLen, itemSize(items[i]))
		c.pending = append(c.pending, false)
		changed = true
	}
	for i := 0; i < len(items) && i < len(c.gens); i++ {
		it := items[i]
		if c.gens[i] == it.Gen && c.vers[i] == it.Ver {
			if c.pending[i] && structural {
				// The queued redraw outlived a shape change (a turn ended,
				// a canonical message landed): the content behind it is
				// final and must render now.
				c.itemsRender[i] = renderItem(m, it, c.width, c.label)
				c.lastRender[i] = now
				c.lastLen[i] = itemSize(it)
				c.pending[i] = false
				changed = true
			}
			continue
		}
		if c.gens[i] == it.Gen && !structural && !c.pending[i] {
			growth := itemSize(it) - c.lastLen[i]
			if growth >= 0 && now.Sub(c.lastRender[i]) < throttleInterval && growth < throttleGrowth {
				// Streaming delta too small and too soon: keep the previous
				// rows. Remember the skip — a pending redraw is force-flushed
				// when the item set changes shape, so the settled text is
				// never left in the queue.
				c.vers[i] = it.Ver
				c.ptrs[i] = itemAddr(it)
				c.pending[i] = true
				continue
			}
		}
		c.itemsRender[i] = renderItem(m, it, c.width, c.label)
		c.ptrs[i] = itemAddr(it)
		c.gens[i] = it.Gen
		c.vers[i] = it.Ver
		c.lastRender[i] = now
		c.lastLen[i] = itemSize(it)
		c.pending[i] = false
		changed = true
	}
	if changed {
		c.lines = nil
		for _, blk := range c.itemsRender {
			c.lines = append(c.lines, blk...)
		}
	}
	return changed
}

// itemSize is the render-relevant content size of an item in bytes: user
// text plus block text and tool args/results. Its growth between renders
// drives the streaming throttle.
func itemSize(it *core.Item) int {
	n := len(it.Text)
	for _, b := range it.Blocks {
		n += len(b.Text)
		if b.Tool != nil {
			n += len(b.Tool.Args) + len(b.Tool.ResultText)
		}
	}
	return n
}

func (c *transCache) total() int { return len(c.lines) }

// mdLRUCap bounds the (width, doc-color) pipelines kept: a resize wobble
// (width flips between two or three values, with the body and the
// user-voice pipelines interleaved) must not rebuild the goldmark/glamour
// pipeline on every frame.
const mdLRUCap = 8

// mdKey identifies one pipeline: the wrap width plus the base text color
// variant ("" = body color, the user-voice color for user lines).
func mdKey(width int, doc string) string {
	var b [12]byte
	i := len(b)
	w := width
	for w > 0 {
		i--
		b[i] = byte('0' + w%10)
		w /= 10
	}
	if i == len(b) {
		i = len(b) - 1
	}
	return string(b[i:]) + "|" + doc
}

// mdFor returns the markdown pipeline for (width, doc) (see
// newMdRenderer). It is an LRU over pipeline keys: a hit moves the entry
// to the tail, a miss builds a pipeline and evicts the stalest key past
// the cap.
func (m *Model) mdFor(width int, doc string) *mdRenderer {
	key := mdKey(width, doc)
	if r, ok := m.mds[key]; ok {
		m.mdTouch(key)
		return r
	}
	r := newMdRenderer(width, m.th, doc)
	if m.mds == nil {
		m.mds = map[string]*mdRenderer{}
	}
	m.mds[key] = r
	m.mdOrder = append(m.mdOrder, key)
	for len(m.mdOrder) > mdLRUCap {
		evict := m.mdOrder[0]
		m.mdOrder = m.mdOrder[1:]
		delete(m.mds, evict)
	}
	return r
}

// mdTouch marks a key most-recently-used (moves it to the tail).
func (m *Model) mdTouch(key string) {
	for i, k := range m.mdOrder {
		if k == key {
			m.mdOrder = append(m.mdOrder[:i], m.mdOrder[i+1:]...)
			m.mdOrder = append(m.mdOrder, key)
			return
		}
	}
}

// braunMarkStyle derives the markdown style from the built-in dark/light
// config and recolors it: warm neutrals, the orange accent for links and
// level-1 headings, hairline rules, and a muted warm syntax palette in
// code blocks. doc ("" = theme body color) is the pipeline's base text
// color: user lines render in the user-voice color, everything else in
// the body color. Code is told apart by its own foreground (CCode), not a
// background surface — both inline code and blocks carry no background.
func braunMarkStyle(t *Theme, doc string) ansi.StyleConfig {
	var st ansi.StyleConfig
	if t.LightBgn {
		st = styles.LightStyleConfig
	} else {
		st = styles.DarkStyleConfig
	}
	sp := func(s string) *string { return &s }
	body := t.CFG
	if doc != "" {
		body = doc
	}
	c, dim, faint, accent := sp(body), sp(t.CDim), sp(t.CFaint), sp(t.CAccent)
	cCode, cErr, cOK := sp(t.CCode), sp(t.CErr), sp(t.COK)
	carn, cInfo, cBorder := sp(t.CWarn), sp(t.CInfo), sp(t.CBorder)
	ib := func(b bool) *bool { return &b }

	st.Document.Color = c
	st.Code.Color = cCode
	st.CodeBlock.Color = cCode
	// The built-in dark/light configs give code spans their own
	// background: clear it so code is told apart by color alone.
	st.Code.BackgroundColor = nil
	st.CodeBlock.BackgroundColor = nil
	st.Heading.Color = c
	st.H1.Color = accent
	st.H1.BackgroundColor = nil
	st.Link.Color = accent
	st.Image.Color = accent
	st.BlockQuote.Color = dim
	st.Emph.Italic = ib(true)
	st.Strong.Bold = ib(true)
	st.HorizontalRule.Color = cBorder
	if ch := st.CodeBlock.Chroma; ch != nil {
		if t.NoColor {
			// The NoColor palette speaks in 256-level indices ("251")
			// and the word "default" — none of which the chroma entry
			// parser accepts (hex colours plus italic/bold keywords).
			// Blank every entry so code blocks fall back to the
			// terminal's own colours instead of panicking the style
			// builder the first time a fenced block renders.
			for _, e := range []*ansi.StylePrimitive{
				&ch.Text, &ch.Error, &ch.Comment, &ch.CommentPreproc,
				&ch.Keyword, &ch.KeywordReserved, &ch.KeywordNamespace,
				&ch.KeywordType, &ch.Operator, &ch.Punctuation, &ch.Name,
				&ch.NameBuiltin, &ch.NameTag, &ch.NameAttribute,
				&ch.NameClass, &ch.NameConstant, &ch.NameDecorator,
				&ch.NameException, &ch.NameFunction, &ch.NameOther,
				&ch.Literal, &ch.LiteralNumber, &ch.LiteralDate,
				&ch.LiteralString, &ch.LiteralStringEscape,
				&ch.GenericDeleted, &ch.GenericEmph, &ch.GenericInserted,
				&ch.GenericStrong, &ch.GenericSubheading, &ch.Background,
			} {
				e.Color = nil
				e.BackgroundColor = nil
				e.Italic = nil
				e.Bold = nil
				e.Underline = nil
			}
			return st
		}
		ch.Text.Color = c
		ch.Error.Color = cErr
		ch.Comment.Color = faint
		ch.Comment.Italic = ib(true)
		ch.CommentPreproc.Color = faint
		ch.Keyword.Color = accent
		ch.KeywordReserved.Color = accent
		ch.KeywordNamespace.Color = accent
		ch.KeywordType.Color = cInfo
		ch.Operator.Color = dim
		ch.Punctuation.Color = dim
		ch.Name.Color = c
		ch.NameBuiltin.Color = cInfo
		ch.NameTag.Color = cInfo
		ch.NameAttribute.Color = cInfo
		ch.NameClass.Color = c
		ch.NameClass.Bold = ib(true)
		ch.NameFunction.Color = c
		ch.LiteralNumber.Color = cInfo
		ch.LiteralString.Color = carn
		ch.GenericDeleted.Color = cErr
		ch.GenericInserted.Color = cOK
		ch.GenericEmph.Italic = ib(true)
		ch.GenericStrong.Bold = ib(true)
		ch.GenericSubheading.Color = dim
	}
	return st
}

// mdRenderer is a (width, doc-color)-scoped markdown pipeline (see
// newMdRenderer), built lazily and LRU-cached on the model.
type mdRenderer struct {
	width  int
	doc    string // base text color variant ("" = body)
	render func(txt string) (string, error)
}

// mdRender renders markdown text at the given width. doc is the pipeline
// base text color ("" = theme body color; the user-voice color for user
// lines).
func (m *Model) mdRender(text string, width int, doc string) []string {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return nil
	}
	r := m.mdFor(width, doc)
	if r == nil || r.render == nil {
		return wrapLines(text, width)
	}
	out, err := r.render(text)
	if err != nil {
		return wrapLines(text, width)
	}
	// Both the leading document newline and trailing newlines are
	// rendering artifacts, not content.
	lines := strings.Split(strings.Trim(out, "\n"), "\n")
	for i, ln := range lines {
		// Expand tabs into the 4-grid and drop the padded right edge:
		// glamour pads every code line to its block width counting a tab
		// as one cell, so after expansion the line can overrun the pane
		// (the pad is plain space — no surface — trimming loses nothing).
		// The pad arrives as per-space style spans (" \x1b[0m" × n), so a
		// byte-level TrimRight misses it: the trim must be style-aware.
		lines[i] = trimRightStyled(expandTabs(ln))
	}
	return lines
}

// trimRightStyled returns s without its trailing styled pad: glamour ends
// padded rows with one styled space per cell ("\x1b[...m \x1b[0m" × n),
// so the row's last byte is an escape, not a space. It walks the escapes
// forward and cuts just after the last visible non-space rune.
func trimRightStyled(s string) string {
	if !strings.Contains(s, " ") {
		return s
	}
	cut := 0
	i := 0
	for i < len(s) {
		c := s[i]
		if c == 0x1b {
			if i+1 < len(s) && s[i+1] == '[' {
				j := i + 2
				for j < len(s) && s[j] >= 0x20 && s[j] <= 0x3f {
					j++
				}
				if j < len(s) {
					i = j + 1
					continue
				}
				break
			}
			i += 2
			continue
		}
		if c == ' ' || c == '\t' {
			i++
			continue
		}
		r, sz := utf8.DecodeRuneInString(s[i:])
		i += sz
		if runewidth.RuneWidth(r) > 0 {
			cut = i
		}
	}
	return s[:cut]
}

// expandTabs maps raw TABs to spaces on a 4-column grid — the Go
// codebase's canonical indent — before the line reaches the terminal.
// Terminals honor 8-column tab stops, so a raw tab on an odd column
// (the code-block margin already shifted the content) lands where the
// render never accounted for and the indentation mangles. The column
// walk skips ANSI escapes and counts wide runes.
func expandTabs(s string) string {
	if !strings.ContainsRune(s, '\t') {
		return s
	}
	const grid = "    "
	var b strings.Builder
	b.Grow(len(s) + 4)
	col := 0
	for i := 0; i < len(s); {
		switch c := s[i]; {
		case c == 0x1b:
			// One CSI (the only class the pipeline emits): skip whole,
			// no width.
			if i+1 < len(s) && s[i+1] == '[' {
				j := i + 2
				for j < len(s) && s[j] >= 0x20 && s[j] <= 0x3f {
					j++
				}
				if j < len(s) {
					b.WriteString(s[i : j+1])
					i = j + 1
					continue
				}
			}
			b.WriteByte(c)
			i++
		case c == '\t':
			b.WriteString(grid[:4-col%4])
			col += 4 - col%4
			i++
		default:
			r, size := utf8.DecodeRuneInString(s[i:])
			b.WriteRune(r)
			if w := runewidth.RuneWidth(r); w > 0 {
				col += w
			}
			i += size
		}
	}
	return b.String()
}

// renderItem turns one transcript item into styled lines. label is the
// model label baked into assistant headers, resolved once per apply pass
// (one cheap store read instead of one snapshot per item).
func renderItem(m *Model, it *core.Item, width int, label string) []string {
	th := m.th
	switch it.Kind {
	case core.KindUser:
		return renderUser(m, it, width)
	case core.KindAssistant:
		return renderAssistant(m, it, width, label)
	case core.KindTurnEnd:
		return renderTurnEnd(m, it)
	case core.KindCommand:
		return renderCommand(m, it)
	case core.KindNote:
		return plainLines(m, th.System(), strings.ReplaceAll(it.Note, "\n", "  "), width)
	default:
		line := "· " + it.Note
		if !it.Ignorable && it.Raw != "" {
			line += " " + it.Raw
		}
		return plainLines(m, th.Faint(), strings.ReplaceAll(line, "\n", " "), width)
	}
}

func renderUser(m *Model, it *core.Item, width int) []string {
	th := m.th
	const indent = 4
	// The markdown is wrapped at the remaining width so that prefix plus
	// line never overflows, in a pipeline whose base text color is the
	// user-voice color: the prompt is told apart from the model's voice
	// by character color, not by the former input-bar background surface.
	// The prompt glyph keeps its state color; inline code and links inside
	// the prompt wear their own colors on top of the voice color; the
	// full-width padding is plain space (invisible — no surface to paint).
	lines := m.mdRender(it.Text, width-indent, th.CUser)
	label := th.Glyph.Asterisk
	glyphStyle := th.Accent()
	if it.EchoRpcId != "" {
		label = th.Glyph.Thought
		glyphStyle = th.System()
	}
	glyph := glyphStyle.Render(label) + "   "
	var out []string
	for i, ln := range lines {
		var line string
		if i == 0 {
			line = glyph
		} else {
			line = "   "
		}
		line += ln
		// Plain (ANSI-stripped) width: the styled segments carry escape
		// bytes that a raw StringWidth would count as display cells.
		if w := plainWidth(line); w < width {
			line += strings.Repeat(" ", width-w)
		}
		out = append(out, line)
	}
	return out
}

// estimateStreamed rough-sizes an in-flight step's generated output from
// its streamed content (text, reasoning, tool-call args) with the same
// ballpark as estimateTokens: the status bar carries it live until the
// step's usage lands (which then replaces it).
func estimateStreamed(it *core.Item) int {
	var s strings.Builder
	for _, b := range it.Blocks {
		switch b.Kind {
		case "text", "reasoning":
			s.WriteString(b.Text)
		case "tool":
			if b.Tool != nil {
				s.WriteString(b.Tool.ArgsFull)
			}
		}
	}
	return estimateTokens(s.String())
}

// estimateTokens rough-sizes a block for the fold line: CJK runs about a
// token per char, the rest about four chars per token (DeepSeek ballpark —
// a display hint, never a meter).
func estimateTokens(s string) int {
	cjk, other := 0, 0
	for _, r := range s {
		switch {
		case r >= 0x2E80 && r <= 0x9FFF, r >= 0x3000 && r <= 0x30FF, r >= 0xFF00 && r <= 0xFFEF:
			cjk++
		default:
			other++
		}
	}
	return cjk + other/4
}

func renderAssistant(m *Model, it *core.Item, width int, label string) []string {
	th := m.th
	name := m.loc.T("transcript.agent")
	if label != "" {
		name = label
	}
	// The block label steps back (dim); the accent is reserved for the
	// user's voice and interactive surfaces.
	header := th.Subtle().Render(padCell(name, 7))
	usedHeader := false
	var out []string
	for _, b := range it.Blocks {
		switch b.Kind {
		case "text":
			if !usedHeader {
				out = append(out, header)
				usedHeader = true
			}
			out = append(out, indentLines(m.mdRender(b.Text, width-2, ""), "  ")...)
		case "reasoning":
			if !m.verbose {
				out = append(out, th.Faint().Render(m.loc.T("transcript.think.fold", th.Glyph.Thought,
					compactInt(int64(len(b.Text))), compactInt(int64(estimateTokens(b.Text))))))
			} else {
				out = append(out, th.Faint().Render(m.loc.T("transcript.thinking", th.Glyph.Thought)))
				out = append(out, indentLines(wrapLines(b.Text, width-6), "    ")...)
			}
		case "tool":
			if !usedHeader {
				out = append(out, header)
				usedHeader = true
			}
			out = append(out, renderToolCard(m, b.Tool, width)...)
		}
	}
	if !usedHeader {
		out = append(out, header+"…")
	}
	return out
}

func renderToolCard(m *Model, tb *core.ToolBlock, width int) []string {
	th := m.th
	if tb == nil {
		return []string{th.Faint().Render("  " + th.Glyph.Tool + " …")}
	}
	name := tb.Name
	if name == "" {
		name = m.loc.T("transcript.tool")
	}
	var nameStyle func() Style
	if tb.IsError {
		nameStyle = th.Err
	} else {
		nameStyle = th.Plain
	}
	var stateStyle func() Style
	state := m.loc.T("transcript.tool.wait")
	switch {
	case tb.Done && tb.IsError:
		state, stateStyle = m.loc.T("transcript.tool.fail"), th.Err
	case tb.Done:
		state, stateStyle = "✓", th.Ok
	default:
		stateStyle = th.Faint
	}
	if tb.Done && tb.CallTime > 0 && tb.DoneTime > tb.CallTime {
		state += " " + textutil.HumanDuration(time.Duration(tb.DoneTime-tb.CallTime)*time.Millisecond)
	}
	// Flat spec line: "  ▸ name args …" with the state LED right-aligned
	// like a product status readout.
	baseW := 2 + runewidth.StringWidth(th.Glyph.Tool) + 1 + runewidth.StringWidth(name)
	args, argsW := "", 0
	if tb.Args != "" {
		budget := width - 2 - runewidth.StringWidth(state) - 3
		if avail := budget - baseW; avail >= 4 {
			a := tb.Args
			if runewidth.StringWidth(a) > avail {
				a = runewidth.Truncate(a, avail, "…")
			}
			args, argsW = a, runewidth.StringWidth(a)
		}
	}
	line := th.System().Render("  "+th.Glyph.Tool+" ") + nameStyle().Render(name)
	if argsW > 0 {
		line += th.Subtle().Render(" " + args)
	}
	pad := width - 2 - runewidth.StringWidth(state) - baseW - argsW
	if pad < 1 {
		pad = 1
	}
	line += strings.Repeat(" ", pad) + stateStyle().Render(state)
	out := []string{line}
	if pv := diffPreview(m, tb, width); len(pv) > 0 {
		out = append(out, pv...)
	}
	if (m.verbose || tb == m.expandedTool) && tb.ResultText != "" {
		res := strings.TrimRight(tb.ResultText, "\n")
		if len(res) > 4000 {
			res = textutil.Truncate(res, 3997, "…") + m.loc.T("transcript.truncated")
		}
		res = strings.ReplaceAll(res, "\n", " ")
		out = append(out, indentLines(plainLines(m, th.Faint(), res, width-5), "     ")...)
	}
	return out
}

// diffPreview renders a compact before/after preview for the
// edit/write family (the args carry file_path plus old/new strings or
// full content): a path header and a few unified-style lines. It shows
// on the card itself — a peek at what the tool is about to change.
func diffPreview(m *Model, tb *core.ToolBlock, width int) []string {
	th := m.th
	var d struct {
		FilePath  string `json:"file_path"`
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
		Content   string `json:"content"`
	}
	if err := json.Unmarshal([]byte(tb.Args), &d); err != nil || d.FilePath == "" {
		return nil
	}
	var oldL, newL []string
	if d.OldString != "" || d.NewString != "" {
		oldL = strings.Split(d.OldString, "\n")
		newL = strings.Split(d.NewString, "\n")
	} else {
		newL = strings.Split(d.Content, "\n")
	}
	const capLines = 4
	var out []string
	out = append(out, th.Faint().Render("     "+textutil.Truncate(d.FilePath, width-9, "…")))
	nOld := len(oldL)
	for i := 0; i < nOld && i < capLines; i++ {
		out = append(out, "     "+th.Err().Render("- ")+th.Faint().Render(truncDisplay(oldL[i], width-11)))
	}
	if nOld > capLines {
		out = append(out, th.Faint().Render(fmt.Sprintf("     … +%d more", nOld-capLines)))
	}
	nNew := len(newL)
	for i := 0; i < nNew && i < capLines; i++ {
		out = append(out, "     "+th.Ok().Render("+ ")+th.Subtle().Render(truncDisplay(newL[i], width-11)))
	}
	if nNew > capLines {
		out = append(out, th.Faint().Render(fmt.Sprintf("     … +%d more", nNew-capLines)))
	}
	return out
}

func renderTurnEnd(m *Model, it *core.Item) []string {
	th := m.th
	var tok string
	if it.TurnTok.In > 0 || it.TurnTok.Out > 0 {
		tok = " · " + compactInt(int64(it.TurnTok.In)) + " in / " + compactInt(int64(it.TurnTok.Out)) + " out"
	}
	dur := textutil.HumanDuration(time.Duration(it.TurnMs) * time.Millisecond)
	switch it.TurnEnd.Kind {
	case "completed":
		return []string{th.Ok().Render(m.loc.T("turnend.done", th.Glyph.Check, dur, tok))}
	case "interrupted", "aborted":
		return []string{th.Warn().Render(m.loc.T("turnend.interrupted", th.Glyph.Warn, dur, tok))}
	case "blocked":
		return []string{th.Warn().Render(m.loc.T("turnend.blocked", th.Glyph.Warn, tok))}
	case "max-tokens":
		return []string{
			th.Warn().Render(m.loc.T("turnend.maxtokens", th.Glyph.Warn, tok)),
			th.Faint().Render(m.loc.T("turnend.maxtokens.hint")),
		}
	default:
		// Unknown protocol kind: the reason is the message — the raw kind
		// is an English wire constant that must not leak into localized UI.
		msg := it.TurnEnd.Reason
		if msg == "" && it.TurnEnd.Error != nil {
			msg = it.TurnEnd.Error.Message
		}
		if msg == "" {
			msg = m.loc.T("turnend.unknown")
		}
		return []string{th.Err().Render("  " + th.Glyph.Cross + " " + msg + tok)}
	}
}

func renderCommand(m *Model, it *core.Item) []string {
	th := m.th
	if it.CmdRun != nil && it.CmdDone == nil {
		return []string{th.System().Render(fmt.Sprintf("  %s /%s %s", th.Glyph.Command, it.CmdRun.Name, it.CmdRun.Args))}
	}
	if it.CmdDone == nil {
		return []string{""}
	}
	name := ""
	if it.CmdRun != nil {
		name = it.CmdRun.Name
	}
	var st func(...string) string
	if it.CmdDone.Kind == "success" {
		st = th.Ok().Render
	} else {
		st = th.Err().Render
	}
	glyph := th.Glyph.Check
	if it.CmdDone.Kind != "success" {
		glyph = th.Glyph.Cross
	}
	return []string{th.System().Render("  "+th.Glyph.Command+" ") + st(glyph) +
		th.Subtle().Render("/"+name+" ") + th.Plain().Render(it.CmdDone.Text)}
}

// plainLines wraps a string to width and styles each physical line.
func plainLines(m *Model, st lipgloss.Style, s string, width int) []string {
	s = strings.ReplaceAll(s, "\n", " ")
	var out []string
	for _, ln := range wrapLines(s, width) {
		out = append(out, st.Render(ln))
	}
	return out
}

// padCell pads s with plain spaces up to n display columns. The width is
// the visible one (ANSI-stripped): a byte count would add the escape
// bytes to the width and under-pad a styled line (a styled header used to
// land 13 columns short of its pane width, dragging the neighbor pane's
// column left with it).
func padCell(s string, n int) string {
	if w := plainWidth(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}

func indentLines(lines []string, pad string) []string {
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		if ln == "" {
			out = append(out, ln)
			continue
		}
		out = append(out, pad+ln)
	}
	return out
}

func compactInt(v int64) string {
	switch {
	case v >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(v)/1_000_000)
	case v >= 1_000:
		return fmt.Sprintf("%.1fK", float64(v)/1_000)
	}
	return fmt.Sprintf("%d", v)
}

// wrapPlain wraps plain text to width, returning physical lines.
func wrapPlain(s string, w int) []string {
	if w < 8 {
		w = 8
	}
	return wrapLines(s, w)
}

// wrapLines wraps plain text to width, returning physical lines. Raw
// tabs expand first (see expandTabs): wordwrap would count a tab as one
// cell and break the wrapped column math.
func wrapLines(s string, w int) []string {
	if w < 4 {
		w = 4
	}
	return strings.Split(strings.TrimRight(wordwrap.String(expandTabs(s), w), "\n"), "\n")
}

// reasoningIsEmpty is a helper on the model pick row (defined near rows).
func (r modelRow) reasoningIsEmpty() bool { return r.reasoning == nil || len(r.reasoning.Efforts) == 0 }

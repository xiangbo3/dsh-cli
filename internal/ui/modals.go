// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"encoding/json"
	"sort"
	"strings"

	"dsh-cli/internal/core"
	"dsh-cli/internal/i18n"
	"dsh-cli/internal/protocol"
	"dsh-cli/internal/textutil"

	"github.com/charmbracelet/bubbletea"
)

// subagentModal is a scrollable window over one child's event log
// (subagent.history, one page, chronological): the conversation rarely
// fits the popup body, so the page walks with the arrows, the page keys,
// and the mouse wheel (wheelModal).
type subagentModal struct {
	parent, child, mode, label string
	loading                    bool
	err                        string
	lines                      []string
	top                        int // first visible line (scroll offset)
	vis                        int // body height the last view rendered (page size)
	loc                        *i18n.Locale
}

func (s *subagentModal) title() string { return s.loc.T("dock.subs.title") + s.label }

func (s *subagentModal) hint() string { return s.loc.T("dock.subs.winhint") }

func (s *subagentModal) view(m *Model, w, h int) []string {
	th := m.th
	if s.loading {
		return []string{th.Faint().Render("  " + s.loc.T("dock.subs.loading"))}
	}
	if s.err != "" {
		return []string{th.Warn().Render("  " + s.err)}
	}
	if len(s.lines) == 0 {
		return []string{th.Faint().Render("  " + s.loc.T("dock.subs.empty"))}
	}
	if h < 1 {
		h = 1
	}
	// A page shorter than the window stands as is: the slice ends at the
	// last line, and vis takes the rendered height (the clamp keeps
	// top honest either way).
	end := s.top + h
	if end > len(s.lines) {
		end = len(s.lines)
	}
	s.vis = end - s.top
	if s.vis < 1 {
		s.vis = 1
	}
	if s.top < 0 {
		s.top = 0
	}
	if max := s.maxTop(); s.top > max {
		s.top = max
	}
	return s.lines[s.top:end]
}

// maxTop is the largest first-visible offset the window can hold (0 when
// the page fits; the window is sized by the last rendered view).
func (s *subagentModal) maxTop() int {
	m := len(s.lines) - maxInt(1, s.vis)
	if m < 0 {
		return 0
	}
	return m
}

// step moves the window by d lines (negative = up), clamped to the page.
func (s *subagentModal) step(d int) {
	s.top += d
	if s.top < 0 {
		s.top = 0
	}
	if max := s.maxTop(); s.top > max {
		s.top = max
	}
}

// page is one page-scroll move: a full window minus the overlap line.
func (s *subagentModal) page() int {
	p := s.vis - 1
	if p < 1 {
		return 1
	}
	return p
}

// wheel takes one mouse-wheel notch (±wheelStep lines); the modal owns
// the wheel while open, so it is always consumed.
func (s *subagentModal) wheel(d int) bool {
	s.step(d)
	return true
}

func (s *subagentModal) update(km tea.KeyMsg) (tea.Cmd, bool) {
	switch km.Type {
	case tea.KeyEsc, tea.KeyEnter, tea.KeyCtrlH:
		return nil, true // closing chord (the dispatch closes it)
	case tea.KeyUp:
		s.step(-1)
	case tea.KeyDown:
		s.step(1)
	case tea.KeyPgUp:
		s.step(-s.page())
	case tea.KeyPgDown:
		s.step(s.page())
	case tea.KeyHome:
		s.top = 0
	case tea.KeyEnd:
		s.top = s.maxTop()
	}
	return nil, true
}

// subHistoryLines renders a child's event page: user and assistant
// messages in full, tool calls as one line each, turn ends as a rule.
func subHistoryLines(loc *i18n.Locale, events []protocol.HistoryEntry) []string {
	var lines []string
	for _, he := range events {
		ev := he.Event
		switch ev.Type {
		case "user/message":
			var msgp protocol.Message
			if json.Unmarshal(ev.Data, &msgp) != nil {
				continue
			}
			text := protocol.SessionTextMessage(&msgp)
			for _, ln := range wrapPlain(text, 100) {
				lines = append(lines, "  > "+ln)
			}
		case "assistant/message":
			var d protocol.AssistantMessageEventData
			if json.Unmarshal(ev.Data, &d) != nil {
				continue
			}
			text := protocol.SessionTextMessage(&d.Message)
			for _, ln := range wrapPlain(text, 100) {
				lines = append(lines, "  "+ln)
			}
		case "tool/call":
			var d protocol.ToolCallEventData
			if json.Unmarshal(ev.Data, &d) == nil && d.Name != "" {
				lines = append(lines, "    "+d.Name)
			}
		case "turn/end":
			lines = append(lines, "  ──")
		}
	}
	return lines
}

// modal is the common shape of every overlay.
type modal interface {
	title() string
	// hint is the window's footer key line (the popup chrome renders it
	// under the body; every popup owns the keyboard while open).
	hint() string
	view(m *Model, w, h int) []string
	update(km tea.KeyMsg) (cmd tea.Cmd, handled bool)
}

// editField is one editable line of a modal at its painted position:
// the modal body row it occupies (the box's border + title rows sit
// above the body) and the styled prefix in front of the field text. The
// box frame pads a one-cell border plus a two-cell margin, so the text
// starts at column one plus col() from the box's left edge.
type editField struct {
	e      *lineEdit
	row    int
	prefix string
}

// col is the box-relative x where the field text begins (inside the
// frame border).
func (f editField) col() int { return 2 + plainWidth(f.prefix) }

// mouseEditor is a modal with an editable field: the mouse picks its
// text (mouseFields, per the frame geometry above) and pastes land in
// it (paste reports whether anything was inserted). Modals without a
// field simply do not implement the interface — their plates eat mouse
// presses and pastes fall through to the main input bar.
type mouseEditor interface {
	mouseFields(m *Model) []editField
	paste(text string) bool
}

// wheelModal is a modal that scrolls its own body on the mouse wheel
// (instead of the transcript behind it) while open; wheel takes one
// notch (±wheelStep lines) and reports whether it was consumed.
type wheelModal interface {
	wheel(d int) bool
}

// frame renders a modal as the centered popup box (crush's dialog
// shape): a rounded hairline frame, the title row with the gradient
// wind-streak rail, the body rows under a two-cell margin, the
// key-hint footer, and the closing frame. The box's interior is a solid
// card slab, not crush's transparent dialog: every cell the box paints
// — border, title row, rail, body padding, the blank tail between body
// and footer, the footer — carries the card surface (cardLine merges it
// into every cell the row leaves without a background), so nothing of
// the underlying screen shows through the box; the margins on either
// side keep the frame's own content. Body lines may be styled; styled
// lines keep their own surface (the search field, the cursor band) and
// get the margins from the frame. w/h are the box's outer size; h must
// hold the chrome (at least body + title + hint + both borders).
// cardLine re-emits a popup row over the card surface: every cell the
// row leaves without a background of its own — the frame's holes, the
// padding, the gaps between styled spans, the blank tail — merges the
// card background, so the box interior is a solid slab instead of
// crush's transparent dialog. Cells with their own surface (the pick
// band, the edit field, the caret, the gradient rail's colored glyphs
// keep their foreground) keep it. A palette without a card color
// (CCardBG empty) leaves the row exactly as it came in.
func cardLine(row string, card sgrState) string {
	if len(card.bg) == 0 {
		return row
	}
	var (
		out     strings.Builder
		prev    sgrState
		started bool
	)
	for _, c := range scanStyled(row) {
		st := c.state.withCard(card)
		if !started || !prev.equals(st) {
			out.WriteString(st.marshalOrReset())
			prev = st
			started = true
		}
		out.WriteRune(c.r)
	}
	return out.String()
}

func frame(m *Model, title, hint string, body []string, w, h int) string {
	th := m.th
	card := th.CardState()
	inner := maxInt(1, w-2)
	clip := func(s string, width int) string {
		return truncDisplay(s, width)
	}
	// The outline is a hairline (border color): the frame is chrome, the
	// accent lives in the title bullet and the row bands inside.
	edge := th.Border()
	space := func(n int) string { return strings.Repeat(" ", n) }
	pad := func(s string, width int) string {
		if p := plainWidth(s); p < width {
			s += strings.Repeat(" ", width-p)
		}
		return s
	}
	// A content row under the frame: border cells plus the row padded to
	// the inner width, the whole line over the card surface — the
	// padding cells merge the card, styled spans keep their own surface.
	border := func(content string) string {
		return cardLine(edge.Render("│")+pad(clip(content, inner), inner)+edge.Render("│"), card)
	}
	if h <= 1 {
		return cardLine(edge.Render(strings.Repeat("─", w)), card)
	}
	if h < 4 {
		// Degenerate box (a healthy one holds the chrome): content rows
		// only, hint on the last row.
		var deg []string
		for i := 0; i < h-1; i++ {
			deg = append(deg, border(space(inner)))
		}
		return strings.Join(append(deg, border(hint)), "\n")
	}
	body = truncateLines(body, h-4)
	// Title: the accent bullet marker, the name, then the wind-streak
	// rail (dense-to-sparse, accent -> dim) filling what is left of the
	// row. The rail is raw SGR gradient glyphs (foreground only); the
	// line ends over the card surface.
	titleLine := space(2) + th.Accent().Render(th.Glyph.Bullet+" ") + th.Plain().Render(title)
	if railW := inner - plainWidth(titleLine) - 1; railW > 0 {
		titleLine += space(1) + th.gradRail(th.CAccent, th.CDim, railW)
	}
	var out []string
	out = append(out, cardLine(edge.Render("╭"+strings.Repeat("─", w-2)+"╮"), card))
	out = append(out, border(titleLine))
	for _, ln := range body {
		if hasANSI(ln) {
			// Styled row: the frame covers both margins; the row's own
			// surface carries its cells, the card pads what is left of
			// the row.
			out = append(out, border(space(2)+ln))
		} else {
			// Plain row: default ink over the card, either side.
			out = append(out, border("  "+ln))
		}
	}
	// The blank tail (box taller than the body) lands between the body
	// and the footer, like the session window's list+help layout —
	// card-filled rows, the box stays a solid slab.
	for i := len(body); i < h-4; i++ {
		out = append(out, border(space(inner)))
	}
	out = append(out, border(hint))
	out = append(out, cardLine(edge.Render("╰"+strings.Repeat("─", w-2)+"╯"), card))
	return strings.Join(out, "\n")
}

// hasANSI reports whether s carries escape sequences.
func hasANSI(s string) bool { return strings.ContainsRune(s, '\u001b') }

func truncateLines(lines []string, n int) []string {
	if n < 0 {
		n = 0
	}
	if len(lines) > n {
		return lines[:n]
	}
	return lines
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ---- help -------------------------------------------------------------------

// helpModal is the help window: the same centered popup chrome every
// dialog shares, with a live search line (body row 0) that narrows the
// entries and a cursor row (the accent band) the arrows walk.
type helpModal struct {
	ed  lineEdit
	cur int
	loc *i18n.Locale
}

var _ modal = (*helpModal)(nil)

func (h helpModal) title() string { return h.loc.T("help.title") }

func (h helpModal) hint() string {
	return h.loc.T("help.hint")
}

// helpEntryKeys is the help body in key form: section headers (two-space
// indent) and key entries (four-space indent) in the catalog, "" for a
// blank gap. The layout rule survives any translation: a section header
// starts two-space indent, an entry four-space, and the search filter
// (helpFiltered) relies on exactly that.
var helpEntryKeys = []string{
	"help.sec.input",
	"help.enter.send",
	"help.enter.newline",
	"help.slash.cmd",
	"help.tab",
	"help.history",
	"help.caret.move",
	"help.cursor",
	"help.char.delete",
	"help.line.delete",
	"",
	"help.sec.clipboard",
	"help.drag",
	"help.dblclick.field",
	"help.dblclick.trans",
	"help.sel.extend",
	"help.copy",
	"help.paste",
	"help.esc.pick",
	"",
	"help.sec.navigation",
	"help.nav.scroll",
	"help.nav.page",
	"help.nav.homeend",
	"help.nav.wheel",
	"help.nav.older",
	"help.nav.side",
	"help.nav.side.keys",
	"help.nav.dock",
	"help.nav.prevnext",
	"help.nav.docktab",
	"",
	"help.sec.session",
	"help.session.model",
	"help.session.mode",
	"help.session.perm",
	"help.session.workspace",
	"help.session.fork",
	"help.session.dropq",
	"help.session.verbose",
	"help.session.help",
	"help.session.quit",
	"help.session.clear",
	"",
	"help.sec.running",
	"help.running.esc",
	"help.running.enter",
	"",
	"help.sec.popups",
	"help.popup.note",
	"help.popup.approve",
	"help.popup.question",
	"help.popup.question2",
	"help.popup.reopen",
}

// helpEntriesFor builds the body in one locale.
func helpEntriesFor(l *i18n.Locale) []string {
	out := make([]string, len(helpEntryKeys))
	for i, k := range helpEntryKeys {
		if k != "" {
			out[i] = l.T(k)
		}
	}
	return out
}

// helpEntries is the English body: the filter's entry count comes from
// it in keyboard state where no locale is in reach, and the width tests
// pin against it.
var helpEntries = helpEntriesFor(nil)

func (h *helpModal) entries() []string {
	return helpEntriesFor(h.loc)
}

func (h *helpModal) filtered() []string {
	return helpFiltered(h.ed.string(), h.entries())
}

// helpFiltered narrows the body to the live search: entries containing
// the lowercased query survive; a section header stays while at least
// one of its entries does; an empty query keeps the whole list intact.
func helpFiltered(q string, entries []string) []string {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return entries
	}
	var out []string
	header, shown := "", false
	for _, ln := range entries {
		switch {
		case ln == "":
			continue
		case !strings.HasPrefix(ln, "    "):
			header, shown = ln, false
		default:
			if strings.Contains(strings.ToLower(ln), q) {
				if !shown {
					out = append(out, header)
					shown = true
				}
				out = append(out, ln)
			}
		}
	}
	return out
}

func (h *helpModal) view(m *Model, w, hgt int) []string {
	th := m.th
	pad := func(s string, width int) string {
		if p := plainWidth(s); p < width {
			s += th.Card().Render(strings.Repeat(" ", width-p))
		}
		return s
	}
	// Search line (body row 0): the window chrome adds the margin.
	search := th.CardStyle(th.Accent()).Render("/") +
		th.Card().Render(" ")
	if len(h.ed.val) == 0 {
		search += th.CardStyle(th.Faint()).Render(h.loc.T("side.search"))
	} else {
		search += cardEditLine(th, true, &h.ed)
	}
	lines := []string{pad(search, maxInt(1, w))}

	entries := h.filtered()
	if h.cur >= len(entries) {
		h.cur = maxInt(0, len(entries)-1)
	}
	// hgt <= 0: the full filtered list (tests, size probes); otherwise
	// the cursor window the popup box gives this line.
	if hgt <= 0 {
		lines = append(lines, entries...)
		return lines
	}
	budget := hgt - 1
	if budget < 1 {
		budget = 1
	}
	start := 0
	if h.cur > start+budget-1 {
		start = h.cur - budget + 1
	}
	for i := start; i < len(entries) && i < start+budget; i++ {
		if i == h.cur {
			// Cursor row: the accent band (the chrome adds the margin).
			lines = append(lines, th.SessBand().Render(pad(entries[i], maxInt(1, w-2))))
			continue
		}
		lines = append(lines, entries[i])
	}
	return lines
}

func (h *helpModal) update(km tea.KeyMsg) (tea.Cmd, bool) {
	entries := h.filtered()
	switch {
	case km.Type == tea.KeyUp:
		if h.cur > 0 {
			h.cur--
		}
		return nil, true
	case km.Type == tea.KeyDown:
		if h.cur < len(entries)-1 {
			h.cur++
		}
		return nil, true
	case km.Type == tea.KeyEnter || km.Type == tea.KeyCtrlH ||
		km.Type == tea.KeyRunes && (km.String() == "h" || km.String() == "?"):
		// Close; the global chord re-opens (toggle parity with the old
		// help modal).
		return nil, true
	case km.Type == tea.KeyEsc:
		// Two-step, like the session window: a non-empty search clears
		// first, the second esc closes.
		if h.ed.string() != "" {
			h.ed.reset()
		} else {
			return nil, true
		}
		return nil, true
	}
	if h.ed.handleKey(km) {
		return nil, true
	}
	return nil, false
}

func (h *helpModal) mouseFields(m *Model) []editField {
	return []editField{{e: &h.ed, row: 0,
		prefix: m.th.CardStyle(m.th.Accent()).Render("/ ")}}
}

func (h *helpModal) paste(text string) bool { return h.ed.paste(text) }

// ---- approval -----------------------------------------------------------------

type approvalModal struct {
	pen   *core.ApprovalPend
	loc   *i18n.Locale
	armed bool // bash-family: the first allow arms, the second confirms
}

func (a approvalModal) title() string {
	return a.loc.T("approval.title") + a.pen.ToolName
}

func (a approvalModal) view(m *Model, w, h int) []string {
	th := m.th
	var lines []string
	if a.pen.Reason != "" {
		lines = append(lines, wrapPlain(a.pen.Reason, w-6)...)
	}
	if a.pen.Args != "" {
		lines = append(lines, "")
		if cmd, ok := commandLine(a.pen.ToolName, a.pen.Args); ok {
			// The actionable line on its own, wrapped: the 900-char raw
			// dump is the fallback for payloads without one.
			for _, ln := range wrapPlain(cmd, w-8) {
				lines = append(lines, "  "+th.System().Render(ln))
			}
			return lines
		}
		lines = append(lines, "  "+textutil.Truncate(a.pen.Args, 900, "…"))
	}
	return lines
}

// commandLine pulls the actionable line out of a tool's args: shell tools
// carry a "command" JSON field, file tools a path. The raw args stay the
// fallback for shapes without either.
func commandLine(tool, args string) (string, bool) {
	var d struct {
		Command  string `json:"command"`
		Cmd      string `json:"cmd"`
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal([]byte(args), &d); err != nil {
		return "", false
	}
	t := strings.ToLower(tool)
	switch {
	case d.Command != "" && (t == "bash" || t == "shell" || t == "exec" || strings.Contains(t, "run")):
		return d.Command, true
	case (t == "edit" || t == "write" || t == "read" || strings.Contains(t, "file")) && d.FilePath != "":
		return d.FilePath, true
	case d.Command != "":
		return d.Command, true
	}
	return "", false
}

// update classifies the key; the armed gate and the response ride the
// dispatch case (which owns the modal's mutation).
func (a *approvalModal) update(km tea.KeyMsg) (tea.Cmd, bool) {
	switch {
	case km.Type == tea.KeyEnter || km.Type == tea.KeyCtrlA ||
		km.Type == tea.KeyRunes && km.String() == "a":
		return nil, true // allow
	case km.Type == tea.KeyEsc || km.Type == tea.KeyCtrlR ||
		km.Type == tea.KeyRunes && km.String() == "d":
		return nil, true // reject
	}
	return nil, false // absorbed by the popup keyboard rule
}

// bashLike reports whether the pending tool runs shell commands — the
// approval most often mis-reads, so its allow takes a confirming second
// key.
func (a *approvalModal) bashLike() bool {
	n := strings.ToLower(a.pen.ToolName)
	return n == "bash" || n == "shell" || n == "exec" || strings.Contains(n, "run")
}

// ---- questions ----------------------------------------------------------------

type questionModal struct {
	pen  *core.QuestionPend
	cur  int
	opt  int
	sel  map[int]map[string]bool
	cust map[int]*lineEdit
	err  string
	sub  bool
	loc  *i18n.Locale
}

// newQuestionModal starts on the built-in face; a production open site
// stamps the model's current locale before showing it.
func newQuestionModal(pen *core.QuestionPend) *questionModal {
	return &questionModal{
		pen:  pen,
		sel:  map[int]map[string]bool{},
		cust: map[int]*lineEdit{},
		loc:  i18n.English(),
	}
}

// custEditor returns question i's free-text field, creating it on first
// touch (the map holds pointers: the mouse pick and the field editor
// both need a stable addressable lineEdit).
func (q *questionModal) custEditor(i int) *lineEdit {
	e := q.cust[i]
	if e == nil {
		e = &lineEdit{}
		q.cust[i] = e
	}
	return e
}

func (q *questionModal) title() string {
	return q.loc.T("question.title", q.cur+1, len(q.pen.Questions))
}

func (q *questionModal) hint() string {
	return q.loc.T("question.hint")
}

func (q *questionModal) current() protocol.QuestionItem {
	if q.cur < 0 || q.cur >= len(q.pen.Questions) {
		return protocol.QuestionItem{}
	}
	return q.pen.Questions[q.cur]
}

func (q *questionModal) view(m *Model, w, h int) []string {
	th := m.th
	it := q.current()
	var lines []string
	if it.Header != "" {
		lines = append(lines, th.CardStyle(th.Accent()).Render(it.Header))
	}
	if it.Intent != nil && it.Intent.Kind == "plan-review" {
		lines = append(lines, th.CardStyle(th.Info()).Render(q.loc.T("question.plan")+it.Intent.Approve+"?"))
	}
	for _, ln := range wrapPlain(it.Question, w-6) {
		lines = append(lines, th.CardStyle(th.Plain()).Render(ln))
	}
	if it.Detail != "" {
		lines = append(lines, th.CardStyle(th.Subtle()).Render("  "+it.Detail))
	}
	lines = append(lines, "")
	if len(it.Options) > 0 {
		for i, op := range it.Options {
			marker := th.Card().Render("  ")
			selected := false
			if s, ok := q.sel[q.cur]; ok && s[op.Label] {
				marker = th.CardStyle(th.Ok()).Render("  " + th.Glyph.Check)
				selected = true
			}
			cursor := th.Card().Render("  ")
			if i == q.opt {
				cursor = th.CardStyle(th.Accent()).Render("  " + th.Glyph.Caret)
			}
			t := op.Label
			if selected {
				lines = append(lines, cursor+marker+" "+th.CardStyle(th.Plain()).Render(t)+th.CardStyle(th.Subtle()).Render(q.loc.T("question.selected")))
			} else {
				lines = append(lines, cursor+marker+" "+th.CardStyle(th.Plain()).Render(t))
			}
			if op.Description != "" {
				lines = append(lines, th.CardStyle(th.Faint()).Render("     "+op.Description))
			}
		}
	} else {
		lines = append(lines, th.CardStyle(th.Faint()).Render(q.loc.T("question.no.opts")))
	}
	// The free-text "other" field edits like the main input box (caret
	// plus the ctrl bindings); an inverted space marks the empty field.
	cust := q.custEditor(q.cur)
	lines = append(lines, th.Card().Render("  ")+th.CardStyle(th.Subtle()).Render(th.Glyph.Dot+q.loc.T("question.other"))+cardEditLine(th, true, cust))
	if q.err != "" {
		lines = append(lines, "", th.CardStyle(th.Err()).Render(q.err))
	}
	return lines
}

func (q *questionModal) update(km tea.KeyMsg) (tea.Cmd, bool) {
	it := q.current()
	switch {
	case km.Type == tea.KeyUp:
		if q.opt > 0 {
			q.opt--
		}
	case km.Type == tea.KeyDown:
		if q.opt < len(it.Options)-1 {
			q.opt++
		}
	case km.Type == tea.KeyLeft:
		if q.cur > 0 {
			q.cur--
			q.opt = 0
			q.err = ""
		}
	case km.Type == tea.KeyRight:
		if q.cur < len(q.pen.Questions)-1 {
			q.cur++
			q.opt = 0
			q.err = ""
		}
	case km.Type == tea.KeySpace || km.Type == tea.KeyRunes && km.String() == " ":
		q.toggleOpt()
	case km.Type == tea.KeyEnter:
		if !it.MultiSelect && len(it.Options) > 0 && q.opt < len(it.Options) &&
			strings.TrimSpace(q.custEditor(q.cur).string()) == "" {
			// Single-select: enter confirms the highlighted option (replacing
			// any earlier selection, like space does). Only when the "other"
			// field is empty — a typed free-text answer stands alone and the
			// encoding drops the selection on submit.
			q.sel[q.cur] = map[string]bool{it.Options[q.opt].Label: true}
		}
		if q.cur < len(q.pen.Questions)-1 {
			q.cur++
			q.opt = 0
			q.err = ""
			return nil, true
		}
		// Last question: validate; the app submits when q.sub is set.
		if q.err == "" {
			q.sub = true
		}
		q.validate()
		return nil, true
	case km.Type == tea.KeyEsc:
		// Keep the frame pending; the app re-arms a reopen hint.
		return nil, true
	default:
		// The "other:" field: the main input box's editor semantics.
		// ctrl+a is the line start (option select moved to space), so
		// the field behaves exactly like the main input.
		q.custEditor(q.cur).handleKey(km)
	}
	return nil, false
}

// mouseFields locates the "other:" field in the view (its row depends on
// the question's wrapped height — re-derived against the same wrap width
// view() uses).
func (q *questionModal) mouseFields(m *Model) []editField {
	it := q.current()
	// The same wrap width view() gets from the box chrome, so the row
	// count matches the painted rows.
	w := m.modalInnerWidth()
	row := 0
	if it.Header != "" {
		row++
	}
	if it.Intent != nil && it.Intent.Kind == "plan-review" {
		row++
	}
	row += len(wrapPlain(it.Question, w-6))
	if it.Detail != "" {
		row++
	}
	row++
	if len(it.Options) > 0 {
		for _, op := range it.Options {
			row++
			if op.Description != "" {
				row++
			}
		}
	} else {
		row++ // the "(no options — type below)" line
	}
	return []editField{{e: q.custEditor(q.cur), row: row,
		prefix: m.th.Card().Render("  ") + m.th.CardStyle(m.th.Subtle()).Render(m.th.Glyph.Dot+q.loc.T("question.other"))}}
}

func (q *questionModal) paste(text string) bool {
	return q.custEditor(q.cur).paste(text)
}

func (q *questionModal) toggleOpt() {
	it := q.current()
	if len(it.Options) == 0 || q.opt >= len(it.Options) {
		return
	}
	if q.sel[q.cur] == nil {
		q.sel[q.cur] = map[string]bool{}
	}
	label := it.Options[q.opt].Label
	if it.MultiSelect {
		q.sel[q.cur][label] = !q.sel[q.cur][label]
	} else {
		q.sel[q.cur] = map[string]bool{label: true}
	}
}

func (q *questionModal) validate() {
	for i := range q.pen.Questions {
		if _, ok := q.sel[i]; ok && len(q.sel[i]) > 0 {
			continue
		}
		if strings.TrimSpace(q.custEditor(i).string()) != "" {
			continue
		}
		q.err = q.loc.T("question.need", i+1)
		q.cur = i
		return
	}
}

// answers builds the batch payload in the host's canonical encoding
// (the web client's twin, gated by the proxy's matchesQuestions):
// sessionId from the pending frame; every answer carries a non-null
// selected array (an absent selection is [] , never null); a non-empty
// free-text answer stands alone on single-select questions (it replaces
// the selection) and accompanies the selected labels on multi-select.
func (q *questionModal) answers() *protocol.QuestionAnswer {
	ans := protocol.QuestionAnswer{SessionId: q.pen.SessionId}
	ans.Answer.Answers = make([]protocol.QuestionAnswerItem, len(q.pen.Questions))
	for i, item := range q.pen.Questions {
		a := protocol.QuestionAnswerItem{Id: item.Id, Selected: []string{}}
		if s, ok := q.sel[i]; ok {
			var labels []string
			for l := range s {
				labels = append(labels, l)
			}
			sort.Strings(labels)
			a.Selected = labels
		}
		if c := strings.TrimSpace(q.custEditor(i).string()); c != "" {
			if !item.MultiSelect {
				a.Selected = []string{}
			}
			a.Custom = c
		}
		ans.Answer.Answers[i] = a
	}
	return &ans
}

// ---- model picker -------------------------------------------------------------

type modelRow struct {
	providerID string
	modelID    string
	display    string
	reasoning  *protocol.ModelReasoning
	groupName  string
}

type modelPicker struct {
	loading bool
	err     string
	models  *protocol.SessionModels
	rows    []modelRow
	cur     int
	effort  int // index into row.reasoning.Efforts (-1 = default)
	loc     *i18n.Locale
}

// newModelPicker starts on the built-in face; the open site stamps the
// model's current locale before showing it.
func newModelPicker() *modelPicker {
	return &modelPicker{effort: -1, loc: i18n.English()}
}

func (p *modelPicker) title() string { return p.loc.T("model.title") }

func (p *modelPicker) hint() string {
	return p.loc.T("model.hint")
}

func (p *modelPicker) view(m *Model, w, h int) []string {
	th := m.th
	if p.loading {
		return []string{th.CardStyle(th.Subtle()).Render(p.loc.T("model.loading"))}
	}
	if p.err != "" {
		return []string{th.CardStyle(th.Err()).Render("  " + p.err)}
	}
	if p.models == nil || len(p.rows) == 0 {
		return []string{th.CardStyle(th.Faint()).Render(p.loc.T("model.none"))}
	}
	var lines []string
	if p.models.Current.Model != "" {
		// The header carries the same display name the roster shows below
		// it (and the top bar does), not the raw model id.
		lines = append(lines, th.CardStyle(th.Faint()).Render(
			p.loc.T("model.current", p.models.Current.Provider,
				p.models.DisplayName(p.models.Current.Provider, p.models.Current.Model),
				effortSuffix(p.models.Current.ReasoningEffort))))
	}
	if !p.models.Routable {
		lines = append(lines, th.CardStyle(th.Warn()).Render(p.loc.T("model.not.routable")))
	}
	lines = append(lines, "")
	lastGroup := ""
	for i, r := range rangeRows(p.rows) {
		if r.groupName != lastGroup {
			lines = append(lines, th.CardStyle(th.Faint()).Render("  "+r.groupName))
			lastGroup = r.groupName
		}
		cursor := th.Card().Render("    ")
		if i == p.cur {
			cursor = th.CardStyle(th.Accent()).Render("  " + th.Glyph.Caret + " ")
		}
		mark := th.Card().Render("")
		if r.modelID == p.models.Current.Model && r.providerID == p.models.Current.Provider {
			mark = th.CardStyle(th.Subtle()).Render(p.loc.T("model.current.mark"))
		}
		name := r.display
		if name == "" {
			name = r.modelID
		}
		lines = append(lines, cursor+th.CardStyle(th.Plain()).Render(name)+mark)
	}
	if len(p.models.Failures) > 0 {
		lines = append(lines, "")
		for _, f := range p.models.Failures {
			lines = append(lines, th.CardStyle(th.Faint()).Render(p.loc.T("model.fail.pref")+f.Name+": "+f.Message))
		}
	}
	lines = append(lines, "")
	if !p.rows[p.cur].reasoningIsEmpty() {
		lines = append(lines, th.CardStyle(th.Subtle()).Render(p.loc.T("model.efforts"))+th.CardStyle(th.Plain()).Render(p.effortsLabel()))
		lines = append(lines, th.CardStyle(th.Faint()).Render(p.loc.T("model.effort.hint")))
	} else {
		lines = append(lines, th.CardStyle(th.Faint()).Render(p.loc.T("model.select.hint")))
	}
	return lines
}

func rangeRows(rows []modelRow) []modelRow { return rows }

func (p *modelPicker) effortsLabel() string {
	r := p.rows[p.cur].reasoning
	if r == nil {
		return p.loc.T("model.default")
	}
	if p.effort < 0 {
		d := ""
		if r.DefaultEffort != "" {
			d = " → " + r.DefaultEffort
		}
		return p.loc.T("model.default.head") + d + ")"
	}
	if p.effort < len(r.Efforts) {
		return r.Efforts[p.effort].Name + " [" + r.Efforts[p.effort].Id + "]"
	}
	return p.loc.T("model.default")
}

func (p *modelPicker) update(km tea.KeyMsg) (tea.Cmd, bool) {
	switch {
	case km.Type == tea.KeyUp:
		if p.cur > 0 {
			p.cur--
			p.effort = -1
		}
	case km.Type == tea.KeyDown:
		if p.cur < len(p.rows)-1 {
			p.cur++
			p.effort = -1
		}
	case km.Type == tea.KeyLeft, km.Type == tea.KeyRight:
		// No row yet (still loading): no reasoning to walk.
		if len(p.rows) > 0 {
			r := p.rows[p.cur].reasoning
			if r != nil && len(r.Efforts) > 0 {
				if km.Type == tea.KeyRight {
					p.effort++
					if p.effort > len(r.Efforts)-1 {
						p.effort = -1
					}
				} else {
					if p.effort <= 0 {
						p.effort = len(r.Efforts) - 1
					} else {
						p.effort--
					}
				}
			}
		}
	case km.Type == tea.KeyEnter:
		return nil, true
	case km.Type == tea.KeyEsc:
		return nil, true
	}
	return nil, false
}

// selected returns the provider/model/effort for a confirmed selection;
// all three are empty when the picker holds no row (error face or an
// empty catalog — the caller must not confirm into that).
func (p *modelPicker) selected() (provider, model, effort string) {
	if p.cur >= len(p.rows) {
		return
	}
	r := p.rows[p.cur]
	provider, model = r.providerID, r.modelID
	if r.reasoning != nil && p.effort >= 0 && p.effort < len(r.reasoning.Efforts) {
		effort = r.reasoning.Efforts[p.effort].Id
	}
	return
}

func (p *modelPicker) fill(models *protocol.SessionModels) {
	p.loading = false
	p.models = models
	p.rows = nil
	for _, g := range models.Groups {
		for _, mo := range g.Models {
			p.rows = append(p.rows, modelRow{
				providerID: g.Id, modelID: mo.Id, display: mo.Name,
				reasoning: mo.Reasoning, groupName: g.Name,
			})
		}
	}
	if p.cur >= len(p.rows) {
		p.cur = 0
	}
}

// ---- rename ---------------------------------------------------------------------

type renameModal struct {
	edit lineEdit
	err  string
	loc  *i18n.Locale
}

func (r *renameModal) title() string { return r.loc.T("rename.title") }

func (r *renameModal) hint() string { return r.loc.T("rename.hint") }

func (r *renameModal) view(m *Model, w, h int) []string {
	th := m.th
	var lines []string
	if r.err != "" {
		lines = append(lines, th.CardStyle(th.Err()).Render(r.err))
	}
	lines = append(lines, th.Card().Render("  ")+cardEditLine(th, true, &r.edit))
	return lines
}

func (r *renameModal) update(km tea.KeyMsg) (tea.Cmd, bool) {
	switch {
	case km.Type == tea.KeyEnter:
		return nil, true
	case km.Type == tea.KeyEsc:
		return nil, true
	default:
		// The main input box's editor semantics: caret navigation plus the
		// ctrl+a/e/b/f/d/u bindings.
		r.edit.handleKey(km)
	}
	return nil, false
}

func (r *renameModal) mouseFields(m *Model) []editField {
	row := 0
	if r.err != "" {
		row = 1 // the error line pushes the field down one row
	}
	return []editField{{e: &r.edit, row: row, prefix: m.th.Card().Render("  ")}}
}

func (r *renameModal) paste(text string) bool { return r.edit.paste(text) }

// ---- search ---------------------------------------------------------------------

type searchModal struct {
	ed   lineEdit
	cur  int
	res  []protocol.SessionSearchItem
	load bool
	err  string
	loc  *i18n.Locale
}

func (s *searchModal) title() string { return s.loc.T("search.title") }

func (s *searchModal) hint() string {
	return s.loc.T("search.hint")
}

func (s *searchModal) view(m *Model, w, h int) []string {
	th := m.th
	var lines []string
	// Search line: the dot marker plus the query with its caret (the
	// box chrome adds the body margin).
	lines = append(lines, th.CardStyle(th.Subtle()).Render(th.Glyph.Dot+" ")+cardEditLine(th, true, &s.ed))
	if s.load {
		lines = append(lines, th.CardStyle(th.Faint()).Render(s.loc.T("search.loading")))
	}
	if s.err != "" {
		lines = append(lines, th.CardStyle(th.Err()).Render(s.err))
	}
	for i, r := range s.res {
		var cursor string
		if i == s.cur {
			cursor = th.CardStyle(th.Accent()).Render("  " + th.Glyph.Caret + " ")
		} else {
			cursor = th.Card().Render("    ")
		}
		snip := r.Snippet
		if len(snip) > w-12 && w > 24 {
			snip = snip[:w-13] + "…"
		}
		lines = append(lines, truncDisplay(cursor+th.CardStyle(th.Plain()).Render("#"+shortID(r.SessionId)+"  ")+th.CardStyle(th.Faint()).Render(snip), w))
	}
	return lines
}

func (s *searchModal) update(km tea.KeyMsg) (tea.Cmd, bool) {
	switch {
	case km.Type == tea.KeyEnter:
		return nil, true
	case km.Type == tea.KeyEsc:
		return nil, true
	case km.Type == tea.KeyUp:
		if s.cur > 0 {
			s.cur--
		}
	case km.Type == tea.KeyDown:
		if s.cur < len(s.res)-1 {
			s.cur++
		}
	default:
		// Query text: the main input box's editor semantics (caret plus
		// the ctrl+a/e/b/f/d/u bindings).
		s.ed.handleKey(km)
	}
	return nil, false
}

func (s *searchModal) mouseFields(m *Model) []editField {
	return []editField{{e: &s.ed, row: 0,
		prefix: m.th.CardStyle(m.th.Subtle()).Render(m.th.Glyph.Dot + " ")}}
}

func (s *searchModal) paste(text string) bool { return s.ed.paste(text) }

// shortID renders a compact id for lists.
func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:6] + "…"
}

func effortSuffix(e string) string {
	if e == "" {
		return ""
	}
	return " (" + e + ")"
}

func (a *approvalModal) hint() string {
	if a.armed {
		return a.loc.T("approval.hint.armed")
	}
	return a.loc.T("approval.hint")
}

// ---- language ------------------------------------------------------------------

// langRow is one selectable catalog in the /language picker.
type langRow struct {
	code string
	name string
}

// languageModal is the /language picker: a searchable roster of the
// available catalogs (the session window's search-line shape on top).
// Enter on a row switches the UI face; esc closes — with the query open
// the first esc clears it, the second closes (the session window's
// rule). The filter is client-side: the roster is the small catalog
// file set on disk, no RPC.
type languageModal struct {
	ed   lineEdit
	cur  int
	rows []langRow
	loc  *i18n.Locale
}

// newLanguageModal lists every available catalog, the built-in English
// included. The open site stamps the model's current locale before
// showing it (like the other pickers).
func newLanguageModal() *languageModal {
	l := &languageModal{loc: i18n.English()}
	for _, c := range i18n.Available() {
		l.rows = append(l.rows, langRow{code: c, name: i18n.DisplayName(c)})
	}
	return l
}

// filtered narrows the roster by the query: code or display name,
// case-insensitive substring (a Chinese query matches the native name).
func (l *languageModal) filtered() []langRow {
	q := strings.ToLower(strings.TrimSpace(l.ed.string()))
	if q == "" {
		return l.rows
	}
	var out []langRow
	for _, r := range l.rows {
		if strings.Contains(strings.ToLower(r.code), q) ||
			strings.Contains(strings.ToLower(r.name), q) {
			out = append(out, r)
		}
	}
	return out
}

// clampCur keeps the cursor on the last surviving row after a filter
// change (a keystroke that drops the highlighted row must not leave it
// pointing past the list).
func (l *languageModal) clampCur() {
	if l.cur >= len(l.filtered()) {
		l.cur = maxInt(0, len(l.filtered())-1)
	}
}

func (l *languageModal) title() string { return l.loc.T("lang.title") }

func (l *languageModal) hint() string { return l.loc.T("lang.hint") }

func (l *languageModal) view(m *Model, w, h int) []string {
	th := m.th
	// Search line: the dot marker plus the query with its caret (the
	// box chrome adds the body margin).
	lines := []string{th.CardStyle(th.Subtle()).Render(th.Glyph.Dot+" ") + cardEditLine(th, true, &l.ed)}
	rows := l.filtered()
	l.clampCur()
	if len(rows) == 0 {
		lines = append(lines, th.CardStyle(th.Faint()).Render(l.loc.T("lang.none")))
		return lines
	}
	for i, r := range rows {
		cursor := th.Card().Render("    ")
		if i == l.cur {
			cursor = th.CardStyle(th.Accent()).Render("  " + th.Glyph.Caret + " ")
		}
		row := th.CardStyle(th.Plain()).Render(r.name + " " + r.code)
		if r.code == m.loc.Lang {
			row += th.CardStyle(th.Subtle()).Render(l.loc.T("lang.current"))
		}
		lines = append(lines, truncDisplay(cursor+row, w))
	}
	return lines
}

func (l *languageModal) update(km tea.KeyMsg) (tea.Cmd, bool) {
	switch {
	case km.Type == tea.KeyEnter:
		return nil, true
	case km.Type == tea.KeyEsc:
		if strings.TrimSpace(l.ed.string()) != "" {
			// First esc clears the query (the session window's rule);
			// handled=false keeps the window open and absorbs the key.
			l.ed.reset()
			l.cur = 0
			return nil, false
		}
		return nil, true
	case km.Type == tea.KeyUp:
		if l.cur > 0 {
			l.cur--
		}
	case km.Type == tea.KeyDown:
		if l.cur < len(l.filtered())-1 {
			l.cur++
		}
	default:
		// Query text: the main input box's editor semantics (caret plus
		// the ctrl+a/e/b/f/d/u bindings); the roster re-filters live.
		l.ed.handleKey(km)
		l.clampCur()
	}
	return nil, false
}

func (l *languageModal) mouseFields(m *Model) []editField {
	return []editField{{e: &l.ed, row: 0,
		prefix: m.th.CardStyle(m.th.Subtle()).Render(m.th.Glyph.Dot + " ")}}
}

func (l *languageModal) paste(text string) bool { return l.ed.paste(text) }

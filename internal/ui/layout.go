// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"dsh-cli/internal/config"
	"dsh-cli/internal/core"
	"dsh-cli/internal/modes"
	"dsh-cli/internal/protocol"
	"dsh-cli/internal/textutil"
	"dsh-cli/internal/version"

	"github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
)

var verbKeys = []string{
	"verb.thinking", "verb.working", "verb.reasoning", "verb.cooking", "verb.wiring",
	"verb.weaving", "verb.simmering", "verb.computing", "verb.plotting",
}

// verbs is the running-state word set in the active language.
func (m *Model) verbs() []string {
	out := make([]string, len(verbKeys))
	for i, k := range verbKeys {
		out[i] = m.loc.T(k)
	}
	return out
}

// View renders the whole frame: a flat top bar over a hairline rule, the
// pane area (transcript / dock separated by gutters), a centered session
// window popup when it is open, and a second rule above the input deck
// (queue, slash menu, input, status).
func (m *Model) View() string {
	m.updateTermTitle()
	// The boot splash owns the whole screen until it is dismissed by time
	// or by a keypress.
	if m.splashActive() && m.W > 0 && m.H > 0 {
		m.transX = -1 // splash owns the whole screen: no pane to pick in
		m.inpY = -1   // ... and no input bar to pick in either
		return fillRows(m.splashView(), m.W)
	}
	// A 5-row window cannot hold the full deck (top bar, two hairlines,
	// input line, status strip, plus at least one transcript row), so it
	// falls into the mini layout too.
	if m.W <= 12 || m.H < 6 {
		m.transX = -1 // mini layout keeps only the top bar: no pane
		m.inpY = -1
		return fillRows(m.miniView(), m.W)
	}
	// (transcript cache is refreshed in transcriptView; keep cost down here)

	// Build bottom-up to measure the main area.
	menuLines := m.menuView(m.W)
	if menuLines == nil {
		menuLines = m.atMenuView(m.W)
	}
	inputLines := m.frameInput(m.W, m.inp.render(m, m.W-2))
	queue := m.queueStrip(m.W)
	status := m.statusBar(m.W)
	toasts := m.toastsView(m.W)

	// The frame carries two hairline rules: below the top bar and above the
	// input deck, and it must never exceed the window height: the alt-screen
	// renderer clips overflow from the top, so a too-tall frame eats the
	// persistent top bar. Count every layer in the budget, and in a cramped
	// window drop the transient layers (toasts, queue strip, slash menu) in
	// that order — the transcript keeps at least its last row.
	mainH := m.H - 1 - len(menuLines) - len(inputLines) - len(queue) - len(toasts) - 1 - 2
	if mainH < 1 {
		toasts = toasts[:0]
		mainH = m.H - 1 - len(menuLines) - len(inputLines) - len(queue) - 1 - 2
	}
	if mainH < 1 {
		queue = queue[:0]
		mainH = m.H - 1 - len(menuLines) - len(inputLines) - 1 - 2
	}
	if mainH < 1 {
		menuLines = menuLines[:0]
		mainH = m.H - 1 - len(inputLines) - 1 - 2
	}
	if mainH < 1 {
		mainH = 1 // pathological window: top bar + input deck survive
	}
	// Publish the transcript pane origin for mouse mapping (the
	// transcript keeps column 0: the session list floats as a centered
	// popup, not a column).
	m.transX = 0
	m.transY = 2 + len(toasts)

	// Publish the input bar origin for mouse mapping (the bar spans the
	// full width, drawn after the second rule, the queue and the slash
	// menu; its row count includes the rounded frame: one border row on
	// top and bottom, the wrapped input lines between them).
	m.inpY = 2 + len(toasts) + mainH + 1 + len(queue) + len(menuLines)
	m.inpH = len(inputLines)

	top := m.topBar(m.W)
	main := m.mainView(m.W, mainH)

	rule := m.th.Rule().Render(strings.Repeat("─", m.W))
	var lines []string
	lines = append(lines, top)
	lines = append(lines, rule)
	lines = append(lines, toasts...)
	lines = append(lines, strings.Split(main, "\n")...)
	lines = append(lines, rule)
	lines = append(lines, queue...)
	lines = append(lines, menuLines...)
	lines = append(lines, inputLines...)
	lines = append(lines, status)
	// The session window is the topmost surface: composed after the rest
	// of the frame and pasted at the screen center, whole rows: the
	// window's opaque cells take their columns (its interior is a solid
	// card slab — every cell the window covers carries the card surface
	// or its own band), and the underlying frame row's content keeps
	// showing in the margins on either side.
	if m.sideVisible {
		ww, wh := m.sessionWindowSize()
		win := strings.Split(m.sideView(ww, wh), "\n")
		ox := (m.W - ww) / 2
		oy := (m.H - wh) / 2
		if ox < 0 {
			ox = 0
		}
		if oy < 0 {
			oy = 0
		}
		for i, ln := range win {
			if y := oy + i; y < len(lines) {
				lines[y] = spliceRow(lines[y], ln, ox, ww)
			}
		}
	}
	// The modal popup is the topmost surface of all: the same box every
	// modal shares, pasted at the screen center like the session
	// window: the box's opaque cells take their columns (its interior
	// is a solid card slab), and the underlying row's content keeps
	// showing in the margins on either side.
	if mod := m.topModal(); mod != nil {
		box, ox, oy, bw, _ := m.modalBox(mod)
		for i, ln := range strings.Split(box, "\n") {
			if y := oy + i; y < len(lines) {
				lines[y] = spliceRow(lines[y], ln, ox, bw)
			}
		}
	}
	// Extend every row to the full screen width with bare-ground cells:
	// a short row's tail would be EraseLineRight'd by the renderer with
	// whatever background is active at the row's end, smearing the
	// popup's card surface into the margin (fillRowWidth).
	for i, ln := range lines {
		lines[i] = fillRowWidth(ln, m.W)
	}
	out := strings.Join(lines, "\n")
	if m.bell {
		m.bell = false
		out += "\a"
	}
	return out
}

// frameInput wraps the input bar's rows in the deck's rounded frame
// (the same shape the dialog windows wear): one border row on top and
// bottom, one border cell on each side, so the bar reads as its own
// window against the queue strip, the slash menu and the status line
// around it. The outline is a hairline (border color), not the accent:
// the frame is chrome, and the accent is the bar's single lit element —
// the prompt glyph. The border cells ride the bar background, like every
// other cell of the bar (hand-drawn, as in frame(): lipgloss's box would
// leave its border rows background-free); on the built-in palettes the
// bar background is transparent, so the hairline is the only chrome
// around the text.
func (m *Model) frameInput(w int, rows []string) []string {
	th := m.th
	edge := th.BarStyle(th.Border())
	surface := th.BarBG()
	inner := w - 2
	var out []string
	out = append(out, edge.Render("╭"+strings.Repeat("─", inner)+"╮"))
	for _, ln := range rows {
		if p := plainWidth(ln); p > inner {
			// A full row with the caret parked at its end carries the
			// added caret cell: clip the overflow, not the frame.
			ln = truncDisplay(ln, inner)
		} else if p < inner {
			ln += surface.Render(strings.Repeat(" ", inner-p))
		}
		out = append(out, edge.Render("│")+ln+edge.Render("│"))
	}
	out = append(out, edge.Render("╰"+strings.Repeat("─", inner)+"╯"))
	return out
}

// topBar is the Braun status line: brand mark and session title on the
// left — with the model name + reasoning effort readout riding along (the
// bottom status line no longer carries it) — and live turn state on the
// right; the hairline rule below it is drawn by View.
func (m *Model) topBar(w int) string {
	th := m.th
	title := m.loc.T("no.session")
	// The model name + reasoning effort readout (former bottom-line slot).
	model, _ := m.modelReadout()
	mode := ""
	if id := m.activeID(); id != "" {
		if snap := m.st.Get(id); snap != nil {
			if snap.Title != "" {
				title = snap.Title
			}
			if p := snap.Summary.AgentPreset; p != "" {
				mode = modes.Label(p)
			}
		}
	}
	nameVer := th.Faint().Render("dsh-cli " + version.Version)
	compose := func(t string) string {
		l := th.Accent().Render(th.Glyph.Zap) + " " + th.Plain().Render(t)
		if mode != "" {
			l += " " + th.Subtle().Render("· "+mode)
		}
		if model != "" {
			l += " " + th.Faint().Render("· ") + model
		}
		return l
	}
	left := compose(title)
	// The name/version must stay on screen at any width: in narrow
	// windows the unbounded title takes the truncation budget first,
	// then the styled left side as a whole (mode/model are bounded but
	// can still overflow sub-30-column bars).
	if budget := w - plainWidth(nameVer) - 1; plainWidth(left) > budget {
		chrome := plainWidth(left) - runewidth.StringWidth(title)
		left = compose(truncDisplay(title, budget-chrome))
		if plainWidth(left) > budget {
			left = truncDisplay(left, budget)
		}
	}
	// Right edge: the client name and version lead, then the running-state
	// readout — the strip identifies the program even before a session
	// lands, and the state icon keeps its own color after the plain readout.
	// Compose with a hard width budget (visible width: the left side
	// carries ANSI, which a raw byte count would inflate). The name/version
	// always fits; the state readout absorbs the clipping (shrinks first,
	// then drops out entirely in sub-30-column windows).
	state := m.topStatus()
	stateW := w - plainWidth(left) - plainWidth(nameVer) - 2
	if stateW < 1 {
		state = ""
	} else if runewidth.StringWidth(state) > stateW {
		state = truncDisplay(state, stateW)
	}
	right := nameVer
	if state != "" {
		right += " " + state
	}
	// Right-align on the exact width: pad by the leftover of the actual
	// right block (not the full avail), or the bar runs past the window
	// edge and the renderer clips the block off-screen.
	pad := w - plainWidth(left) - plainWidth(right)
	if pad < 1 {
		pad = 1
	}
	return left + strings.Repeat(" ", pad) + right
}

// miniView keeps the persistent top bar alive when the window is too small
// for the full deck: top bar plus its hairline always, the input line and
// the status strip only where there is room.
func (m *Model) miniView() string {
	w := m.W
	if w < 6 || m.H < 2 {
		return " …dsh-cli… "
	}
	lines := []string{
		m.topBar(w),
		m.th.Rule().Render(strings.Repeat("─", w)),
	}
	if m.H >= 3 {
		lines = append(lines, m.inp.render(m, w)[0])
	}
	if m.H >= 4 {
		lines = append(lines, m.statusBar(w))
	}
	return strings.Join(lines, "\n")
}

func (m *Model) topStatus() string {
	th := m.th
	if !m.st.Connected() {
		return th.Warn().Render(th.Glyph.Reconnect + " " + m.loc.T("top.connecting"))
	}
	id := m.activeID()
	if id != "" {
		if snap := m.st.Get(id); snap != nil {
			// Liveness comes from the host's running flag (snap.Running),
			// re-baselined by the session list and live turn/status events.
			// NOT snap.TurnActive: that is a property of the folded log and
			// stays true for a turn that ended (or was cut off) in history
			// without a turn/end — at boot that would spin the verb forever
			// with a stale elapsed time.
			if snap.Running {
				verbs := m.verbs()
				frame := th.Glyph.Spinner[frameIdx(m.now)]
				verb := verbs[m.verbIdx()]
				elapsed := "0s"
				if !snap.TurnStart.IsZero() {
					elapsed = textutil.HumanDuration(time.Since(snap.TurnStart))
				}
				return th.Accent().Render(frame) + " " + m.sweepText(verb+"…") + " " +
					th.Faint().Render(elapsed)
			}
			if snap.Flash != nil && time.Since(snap.Flash.At) < 6*time.Second {
				return m.turnFlash(snap.Flash)
			}
		}
	}
	// The context occupancy rides on the status strip (ctxBar) instead:
	// the top-right state slot stays lean.
	return th.Faint().Render(m.loc.T("top.idle"))
}

// turnFlash renders the post-turn banner. It must use the prebuilt theme:
// NewTheme() issues terminal capability queries, which must not run per
// frame (they also race the key reader for up to termenv.OSCTimeout).
func (m *Model) turnFlash(f *core.TurnEndInfo) string {
	th := m.th
	dur := textutil.HumanDuration(time.Duration(f.Ms) * time.Millisecond)
	switch f.Kind {
	case "completed":
		return th.Ok().Render(m.loc.T("turn.done", th.Glyph.Check, dur, compactInt(int64(f.Out))))
	case "interrupted", "aborted":
		return th.Warn().Render(m.loc.T("turn.stop", dur))
	case "max-tokens":
		return th.Warn().Render(" " + th.Glyph.Warn + " " + m.loc.T("turn.maxtokens"))
	default:
		return th.Err().Render(" " + th.Glyph.Cross + " " + f.Kind)
	}
}

// ctxUsage reports the active session's context occupancy. The live
// "contextPressure" projection wins when the host publishes it (the
// host's own current footprint against the advertised window); the
// request/context window plus the last assistant step's usage is the
// fallback for hosts without the projection. known is false when no
// window is in reach — the readout stays off instead of guessing.
func (m *Model) ctxUsage() (pct int, known bool) {
	id := m.activeID()
	if id == "" {
		return 0, false
	}
	snap := m.st.Get(id)
	if snap == nil {
		return 0, false
	}
	if cp := snap.CtxPressure; cp != nil && cp.ContextWindow > 0 {
		used := cp.PressureTokens
		if used > cp.ContextWindow {
			used = cp.ContextWindow
		}
		return used * 100 / cp.ContextWindow, true
	}
	if snap.Ctx == nil || snap.Ctx.ContextWindow <= 0 {
		return 0, false
	}
	var last *protocol.TokenUsage
	for i := len(snap.Items) - 1; i >= 0; i-- {
		if snap.Items[i].Kind == core.KindAssistant && snap.Items[i].Usage != nil {
			last = snap.Items[i].Usage
			break
		}
	}
	if last == nil {
		return 0, true
	}
	used := last.InputTokens + last.CacheReadTokens + last.OutputTokens
	return used * 100 / snap.Ctx.ContextWindow, true
}

// ctxBar renders the Ctx icon (∞) leading a 10-cell context-usage progress
// bar with its percentage ("" until the context window is known). The level
// colors the icon and the bar together: dim below 60%, accent below 85%,
// warn above.
func (m *Model) ctxBar() (styled, plain string) {
	pct, known := m.ctxUsage()
	if !known {
		return "", ""
	}
	const cells = 10
	filled := pct * cells / 100
	if filled > cells {
		filled = cells
	}
	bar := strings.Repeat("\u2588", filled) + strings.Repeat("\u2591", cells-filled)
	var pick func() Style
	switch {
	case pct >= 85:
		pick = m.th.Warn
	case pct >= 60:
		pick = m.th.Accent
	default:
		pick = m.th.Faint
	}
	plain = fmt.Sprintf("%s %s %d%%", m.th.Glyph.Ctx, bar, pct)
	return pick().Render(plain), plain
}

func frameIdx(now time.Time) int {
	return int(now.Nanosecond()/1e6/110) % 4
}

// sweepBand is the half-width (in cells) of the sweepText highlight band.
const sweepBand = 2

// sweepText paints a running-state verb with a traveling highlight (扫光):
// every rune shares one hue and only its brightness moves — dim outside
// the band, base level at its edge, bright at the center — so the pass
// reads as light sweeping across the word instead of a color change. The
// band walks one cell per render tick until it is fully past the last
// rune (the pass length scales with the verb, so every verb gets the same
// left-to-right sweep).
func (m *Model) sweepText(text string) string {
	dim, mid, hot := m.th.sweepShades()
	rs := []rune(text)
	n := len(rs)
	span := n + 2*sweepBand
	f := int(m.now.Nanosecond()/1e6/100) % span
	edge := float64(f) - float64(sweepBand)
	var b strings.Builder
	for i, r := range rs {
		d := float64(i) - edge
		if d < 0 {
			d = -d
		}
		switch {
		case d <= 1:
			b.WriteString(hot.Render(string(r)))
		case d <= float64(sweepBand):
			b.WriteString(mid.Render(string(r)))
		default:
			b.WriteString(dim.Render(string(r)))
		}
	}
	return b.String()
}

func (m *Model) verbIdx() int {
	return int(m.now.Nanosecond()/1e6/1700) % len(verbKeys)
}

// toastsView renders the toast shelf (up to 3 lines, newest last): a
// state-colored marker LED and dim text, Braun-flat.
func (m *Model) toastsView(w int) []string {
	if len(m.toasts) == 0 {
		return nil
	}
	th := m.th
	var out []string
	for _, t := range m.toasts {
		var glyphStyle func() Style
		glyph := th.Glyph.Dot
		switch t.level {
		case "ok":
			glyph, glyphStyle = th.Glyph.Check, th.Ok
		case "warn":
			glyph, glyphStyle = th.Glyph.Warn, th.Warn
		case "err":
			glyph, glyphStyle = th.Glyph.Cross, th.Err
		default:
			glyphStyle = th.Faint
		}
		text := t.text
		if runewidth.StringWidth(text) > w-8 {
			text = truncDisplay(text, w-8)
		}
		out = append(out, "  "+glyphStyle().Render(glyph+" "+text))
	}
	return out
}

// paneWidths computes the three-column layout widths, reserving one
// gutter of space between each visible column. The session list no
// longer takes a column (it is the centered session window popup), so
// the budget only ever reserves the dock.
func (m *Model) paneWidths(w int) (side, trans, dock int) {
	budget := w
	if m.dockVisible {
		budget--
	}
	if m.dockVisible {
		dock = budget * 30 / 100
		if dock < 26 {
			dock = 26
		} else if dock > 38 {
			dock = 38
		}
	}
	trans = budget - side - dock
	if trans < 16 {
		trans = 16
	}
	return
}

// mainView lays out transcript | dock with single-space gutters (the
// Braun grid: flat columns, no rails). The session list floats as a
// centered popup on top of the frame (sideView), not as a column.
func (m *Model) mainView(w, h int) string {
	_, transW, dockW := m.paneWidths(w)

	var cols [][]string
	cols = append(cols, strings.Split(m.transcriptView(transW, h), "\n"))
	if m.dockVisible {
		cols = append(cols, strings.Split(m.dockView(dockW, h), "\n"))
	}
	return hjoin(cols)
}

// hjoin concatenates columns line by line with a single-space gutter,
// padded to a common height. Grid contract (the string world's equivalent
// of crush's screen buffer, where each component draws into its own fixed
// rectangle): every input line is EXACTLY its column's display width
// (padLines/fitWidth own that), or a ragged line shifts this column's
// neighbor one-for-one on that row — the dock's left edge would drift
// with the transcript content instead of sitting at a fixed column.
func hjoin(cols [][]string) string {
	height := 0
	for _, c := range cols {
		if len(c) > height {
			height = len(c)
		}
	}
	var out []string
	for i := 0; i < height; i++ {
		var row []string
		for _, c := range cols {
			ln := ""
			if i < len(c) {
				ln = c[i]
			}
			row = append(row, ln)
		}
		out = append(out, strings.Join(row, " "))
	}
	return strings.Join(out, "\n")
}

// sideView renders the session list under a small-caps header.
//
// sideQuery is the live session-list search text ("" = full roster).
func (m *Model) sideQuery() string {
	return strings.ToLower(strings.TrimSpace(m.sideSearch.string()))
}

// sideRowsFiltered narrows the scoped rows (the active session's
// workspace, full roster when unbound — see Store.CurrentSide) to the
// live search: with a query only the matching session rows survive
// (flat, no workspace headers — the cwd stays on each row for
// orientation), without one the scoped list stands as is.
func (m *Model) sideRowsFiltered() []core.SideRow {
	rows := m.st.CurrentSide(m.activeID())
	q := m.sideQuery()
	if q == "" {
		return rows
	}
	var out []core.SideRow
	for _, r := range rows {
		if !r.Header && sideRowMatches(r, q) {
			out = append(out, r)
		}
	}
	return out
}

// sideRowMatches reports whether a session row hits the (lowercased)
// search query: title, id or cwd containing it.
func sideRowMatches(r core.SideRow, q string) bool {
	return strings.Contains(strings.ToLower(r.Row.Title), q) ||
		strings.Contains(strings.ToLower(r.Row.Id), q) ||
		strings.Contains(strings.ToLower(r.Row.Cwd), q)
}

// sideNavIDs is the roster the arrows (←/→) session switch walks: the
// active session's workspace scope (see Store.CurrentSideIDs), narrowed
// to the live search filter when one is set — the arrows follow exactly
// what the list shows.
func (m *Model) sideNavIDs() []string {
	q := m.sideQuery()
	if q == "" {
		return m.st.CurrentSideIDs(m.activeID())
	}
	var out []string
	for _, r := range m.st.CurrentSide(m.activeID()) {
		if !r.Header && sideRowMatches(r, q) {
			out = append(out, r.Row.Id)
		}
	}
	return out
}

// sessionWindowSize is the popup's size (crush's dialog caps: at most
// 70 wide, 20 tall), clamped to the screen with a one-cell margin and a
// floor small enough to hold the frame chrome.
func (m *Model) sessionWindowSize() (w, h int) {
	w = m.W - 2
	if w > 70 {
		w = 70
	}
	if w < 14 {
		w = 14
	}
	h = m.H - 2
	if h > 20 {
		h = 20
	}
	if h < 6 {
		h = 6
	}
	return w, h
}

// modalBoxSize is the modal popup box's size (crush's dialog caps: at
// most 70 wide, 20 tall), clamped to the screen with a one-cell margin
// and a floor that holds the chrome (frame border + title + hint + at
// least one body row). It returns the outer width and the body height
// budget (h-4: frame border rows, title and hint).
func (m *Model) modalBoxSize() (w, budget int) {
	w = m.W - 2
	if w > 70 {
		w = 70
	}
	if w < 14 {
		w = 14
	}
	budget = m.H - 6
	if budget > 16 {
		budget = 16
	}
	if budget < 1 {
		budget = 1
	}
	return w, budget
}

// modalInnerWidth is the width a modal view receives: the box inner
// width (the chrome adds the one-cell border plus the two-cell body
// margin). Views wrap and paginate against it; modals' mouseFields
// derive their rows against the same width.
func (m *Model) modalInnerWidth() int {
	w, _ := m.modalBoxSize()
	return maxInt(1, w-4)
}

// modalBox renders the open modal's popup (the card box from frame:
// rounded hairline border, gradient wind-streak title rail, hint footer)
// and reports where View pastes it: at the screen center — horizontally
// and vertically, whole rows, clamped to the screen. The press-path
// popupBox shares the math, so hit-testing lands where the box actually
// painted.
func (m *Model) modalBox(mod modal) (box string, ox, oy, bw, bh int) {
	w, budget := m.modalBoxSize()
	body := mod.view(m, m.modalInnerWidth(), budget)
	bh = 4 + len(body)
	if bh < 6 {
		bh = 6
	}
	if bh > budget+4 {
		bh = budget + 4
	}
	box = frame(m, mod.title(), mod.hint(), body, w, bh)
	// Whole-row paste (the session window's convention): the box origin
	// parks the popup at the screen center, horizontally and
	// vertically.
	// View prepends ox spaces to every pasted row; the press-path plate
	// geometry (popupBox) shares these numbers, so hit-testing follows
	// the painted position.
	ox = (m.W - w) / 2
	if ox < 0 {
		ox = 0
	}
	oy = (m.H - bh) / 2
	if oy < 0 {
		oy = 0
	}
	return box, ox, oy, w, bh
}

// sideView renders the session window (the centered popup, crush's
// dialog shape): a rounded hairline frame on the card surface holding,
// top to bottom, the "Sessions" title with the ASCII wind-streak rail
// (a dense dash line on the left, shredded into dots and gaps blowing
// off to the right), the
// search line (the window owns the keys, so its caret is always live),
// the visible slice of the filtered roster with the pick on the accent
// band, and the key-hint footer. The interior is a solid card slab —
// every cell the window paints carries the card surface or its own
// band — not crush's transparent dialog. w/h are the window's outer
// size.
func (m *Model) sideView(w, h int) string {
	th := m.th
	inner := maxInt(1, w-2)
	rows := m.sideRowsFiltered()
	grouped := m.sideQuery() == "" && len(m.st.Workspaces()) > 0
	sessCount := 0
	for _, r := range rows {
		if !r.Header {
			sessCount++
		}
	}
	active := m.activeID()
	// The popup cursor (sideCursorID) drives both the scroll position and
	// the accent band: it starts on the active session and follows the
	// live search filter. A stale/empty id — or one the filter dropped —
	// re-seats the cursor on the active session's row.
	cursorID := m.sideCursorID
	if cursorID == "" {
		cursorID = active
	}
	inRoster := false
	for _, r := range rows {
		if !r.Header && r.Row.Id == cursorID {
			inRoster = true
			break
		}
	}
	if !inRoster {
		cursorID = active
	}
	cur := -1
	for i, r := range rows {
		if !r.Header && r.Row.Id == cursorID {
			cur = i
			break
		}
	}
	// The interior is a solid card slab: the row filler (cardLine, in
	// frame's file) merges the card surface into every cell a row leaves
	// without a background, so nothing of the underlying screen shows
	// through the window. The intentional bands — the selected row and
	// the live search field — keep their own surfaces on top.
	space := func(n int) string { return strings.Repeat(" ", n) }
	pad := func(s string, width int) string {
		if p := plainWidth(s); p < width {
			s += strings.Repeat(" ", width-p)
		}
		return s
	}

	var content []string
	// Title: "Sessions" in the accent, the live count dim, then the
	// wind-streak rail (dense → sparse, the gust blown left to right)
	// filling the remainder (crush's dialog title). The count is the
	// filtered match count while a query is set.
	title := space(2) + th.Accent().Render(m.loc.T("side.title")) +
		space(1) + th.Faint().Render(fmt.Sprintf("%d", sessCount))
	if railW := inner - plainWidth(title) - 1; railW > 0 {
		// The rail is raw SGR gradient glyphs (foreground only); the hole
		// cells either side let the screen show through.
		title += space(1) + th.gradRail(th.CAccent, th.CDim, railW)
	}
	content = append(content, truncDisplay(title, inner))

	// Search line: the "/" marker in accent and the live query with its
	// caret (faint "search" placeholder while empty, transparent; the
	// live field keeps its card band).
	searchLine := space(2) + th.Accent().Render("/") + space(1)
	if len(m.sideSearch.val) == 0 {
		searchLine += th.Faint().Render(m.loc.T("side.search"))
	} else {
		searchLine += cardEditLine(th, true, &m.sideSearch)
	}
	content = append(content, truncDisplay(searchLine, inner))

	var rowsList []string
	// Rows: the visible slice of the filtered roster (the pick row keeps
	// itself in view); the footer sits on the last line, so the blank
	// tail — when the roster is shorter than the window — lands between
	// them, exactly like crush's list+help layout.
	fixed := 3 // title + search + footer rows
	if innerH := h - 2; innerH > fixed {
		visible := innerH - fixed
		start := 0
		if cur > start+visible-1 {
			start = cur - visible + 1
		}
		indent := ""
		if grouped {
			indent = space(2)
		}
		for i := start; i < len(rows) && i < start+visible; i++ {
			r := rows[i]
			if r.Header {
				row := space(2) + th.Accent().Render(th.Glyph.Folder+" ") +
					th.Label().Render(truncDisplay(r.Title, inner-8))
				if r.Path != "" {
					if p := truncDisplay(r.Path, inner-4); p != "" {
						row += space(1) + th.Faint().Render(p)
					}
				}
				rowsList = append(rowsList, truncDisplay(pad(row, inner), inner))
				continue
			}
			sr := r.Row
			dot := th.Glyph.Dot
			if sr.Running {
				dot = th.Glyph.Bullet
			} else if sr.Fresh {
				dot = th.Glyph.New
			}
			title := sr.Title
			if title == "" {
				title = m.loc.T("common.untitled")
			}
			dotStyle := th.Faint()
			if sr.Running {
				dotStyle = th.Accent()
			} else if sr.Fresh {
				dotStyle = th.Warn()
			}
			switch {
			case sr.Id == cursorID:
				// The row under the cursor: one solid accent band across the
				// window, card ink on top, the pick marker out front (crush's
				// selected row, uniform by construction — the window's raised
				// surface over the card slab).
				text := th.Glyph.Caret + " " + dot + " " + title
				if mode := modes.Short(sr.Mode); mode != "" {
					text += "  · " + mode
				}
				if sr.Cwd != "" {
					text += "  " + sr.Cwd
				}
				rowsList = append(rowsList, th.SessBand().Render(pad(text, inner)))
			case sr.Id == active:
				// The committed selection (the active session), not under the
				// cursor: a check prefix marks it — the row space would make
				// current and enter would open.
				row := indent + space(2)
				row += th.Accent().Render(th.Glyph.Check + " ")
				row += dotStyle.Render(dot)
				row += space(1) + th.Subtle().Render(title)
				if mode := modes.Short(sr.Mode); mode != "" {
					row += space(1) + th.Faint().Render("· "+mode)
				}
				if sr.Cwd != "" && !grouped {
					// Grouped rows already carry their workspace header.
					row += space(1) + th.Faint().Render(sr.Cwd)
				}
				rowsList = append(rowsList, truncDisplay(pad(row, inner), inner))
			default:
				row := indent + space(2)
				row += dotStyle.Render(dot)
				titleStyle := th.Subtle()
				if until, ok := m.flashEnds[sr.Id]; ok && time.Now().Before(until) {
					// A turn just ended on this session: flash the row once.
					titleStyle = th.Accent()
				}
				row += space(1) + titleStyle.Render(title)
				if mode := modes.Short(sr.Mode); mode != "" {
					row += space(1) + th.Faint().Render("· "+mode)
				}
				if sr.Cwd != "" && !grouped {
					// Grouped rows already carry their workspace header.
					row += space(1) + th.Faint().Render(sr.Cwd)
				}
				rowsList = append(rowsList, truncDisplay(pad(row, inner), inner))
			}
		}
	}

	content = append(content, rowsList...)
	for n := len(content); n < h-2-1; n++ {
		// Blank tail between the last row and the footer: the card slab
		// fills it (cardLine gives these rows their surface).
		content = append(content, strings.Repeat(" ", inner))
	}

	// Footer: the window's own key map (it owns the keyboard while open;
	// esc is two-step: a first press clears the search, the second closes).
	hint := m.loc.T("side.hint")
	if runewidth.StringWidth(hint) > inner {
		hint = runewidth.Truncate(hint, inner, "…")
	}
	content = append(content, th.Faint().Render(pad(hint, inner)))

	// The rounded hairline frame (crush's dialog border, border color:
	// the frame is chrome, the accent lives in the title, the rail and
	// the pick band inside), drawn by hand over the card surface:
	// cardLine merges the card background into every cell the line
	// leaves without one — border cells, title row, gaps, blank tail,
	// footer — so the window's interior is a solid slab and nothing of
	// the screen shows through it.
	edge := th.Border()
	card := th.CardState()
	var out []string
	out = append(out, cardLine(edge.Render("╭"+strings.Repeat("─", w-2)+"╮"), card))
	for _, ln := range content {
		if p := plainWidth(ln); p < inner {
			ln += strings.Repeat(" ", inner-p)
		}
		out = append(out, cardLine(edge.Render("│")+ln+edge.Render("│"), card))
	}
	out = append(out, cardLine(edge.Render("╰"+strings.Repeat("─", w-2)+"╯"), card))
	return strings.Join(out, "\n")
}

// syncTrans keeps the transcript cache in step with the active session —
// the markdown for its items renders at the current wrap width inside
// apply. It runs only when something invalidates the cache (the
// idle-frame short-circuit): transDirty marks model-side invalidation
// (resize, verbose, cache reset), a store rev change marks state movement
// since the last sync, and the wrap width is part of the gate too (the
// dock toggle re-lays the panes without a store mutation or a window
// resize). Otherwise nothing is re-scanned. It reports whether the session
// row still exists (a vanished row keeps the board blank). The render
// pass and the splash pre-load (preloadTrans) share it, so the rev /
// width bookkeeping has exactly one owner.
func (m *Model) syncTrans() bool {
	id := m.activeID()
	if id == "" {
		return false
	}
	rev := m.st.Rev()
	tw := m.transcriptWidth()
	if !m.transDirty && rev == m.lastTransRev && tw == m.lastTransWidth {
		return true
	}
	m.transDirty = false
	m.lastTransRev = rev
	m.lastTransWidth = tw
	snap := m.st.Get(id)
	if snap != nil {
		if m.transFor != id {
			m.followBase = 0 // mirror swapped: line counts no longer comparable
			m.transFor = id
		}
		m.trans.apply(m, snap.Items)
	}
	return snap != nil
}

// preloadTrans is the splash-phase pre-load: while the boot animation
// still owns the screen, the roster baseline and the boot auto-pick land
// and the previous session's tail arrives — fold that into the transcript
// cache HERE so the first visible frame is served from cache instead of
// paying the markdown render cost on screen (the splash smoothness win).
// Skipped until the window is sized (the wrap width is unknown; the first
// render pass pays the cost once).
func (m *Model) preloadTrans() {
	if !m.splashActive() || m.W <= 0 {
		return
	}
	m.syncTrans()
}

// transcriptView renders the visible window of the transcript — blank
// before the first session exists — and any open modal.
func (m *Model) transcriptView(w, h int) string {
	// The scroll machinery (page keys, wheel, follow re-arm) measures
	// against the pane as it really is this frame.
	m.transH = h
	id := m.activeID()
	var visible []string
	if id == "" {
		// No session yet: the board stays blank (the post-splash welcome
		// plate is gone); the modal overlay below still applies.
		visible = make([]string, h)
	} else if !m.syncTrans() {
		// The session row vanished from the store: blank board.
		visible = make([]string, h)
	}
	if visible == nil {
		total := m.trans.total()
		maxOff := total - h
		if maxOff < 0 {
			maxOff = 0
		}
		off := m.transOffset()
		if off == maxOff {
			// Sitting on the last line: re-arm follow so streaming
			// content keeps the tail pinned (chat-app rule).
			m.follow = true
			m.followBase = 0
		}
		end := off + h
		if end > total {
			end = total
		}
		visible = m.trans.lines[off:end]
	}
	lines := padLines(visible, w, h)

	// Pick band (crush's highlight): every visible row the pick covers
	// gets the band painted on the covered cells only — a full row goes
	// through the theme's one-pass band, a partial row is re-emitted
	// cell by cell with the band state merged into the covered cells
	// (the row's own styling survives under the band).
	if m.sel.active && id != "" {
		off := m.transOffset()
		first, fcol, last, lcol := m.sel.bounds(len(m.trans.lines))
		for i := 0; i < h; i++ {
			l := off + i
			if l < first || l > last {
				continue
			}
			cs := 0
			if l == first {
				cs = fcol
			}
			ce := plainWidth(lines[i])
			if l == last && lcol < ce {
				ce = lcol
			}
			lines[i] = m.applyBand(lines[i], cs, ce)
		}
	}

	return strings.Join(lines, "\n")
}

// menuView renders the slash command popup above the input: the selected
// entry in full foreground with the accent caret, the rest dimmed.
func (m *Model) menuView(w int) []string {
	if !m.inp.menuOpen || len(m.inp.menu) == 0 {
		return nil
	}
	th := m.th
	maxRows := 8
	n := len(m.inp.menu)
	if n > maxRows {
		n = maxRows
	}
	var lines []string
	for i := m.inp.menuStart(n); i < m.inp.menuStart(n)+n; i++ {
		c := m.inp.menu[i]
		cursor := "  "
		nameStyle := th.Subtle
		if i == m.inp.menuCur {
			cursor = th.Accent().Render(th.Glyph.Caret + " ")
			nameStyle = th.Plain
		}
		name := "/" + c.Name
		if c.Local {
			name += th.Faint().Render(m.loc.T("menu.local"))
		}
		line := cursor + nameStyle().Render(name) + "  " + th.Faint().Render(c.Desc)
		lines = append(lines, truncDisplay(line, w-2))
	}
	lines = append(lines, th.Faint().Render(m.loc.T("menu.footer", m.inp.menuCur+1, len(m.inp.menu))))
	return lines
}

// atMenuView renders the @ completion popup above the input: running
// children first, then cwd paths, the selected entry accented.
func (m *Model) atMenuView(w int) []string {
	in := m.inp
	if !in.atOpen || len(in.atMenu) == 0 {
		return nil
	}
	th := m.th
	maxRows := 8
	n := len(in.atMenu)
	if n > maxRows {
		n = maxRows
	}
	var lines []string
	for i := in.atMenuStart(n); i < in.atMenuStart(n)+n; i++ {
		c := in.atMenu[i]
		cursor := "  "
		nameStyle := th.Subtle
		if i == in.atCur {
			cursor = th.Accent().Render(th.Glyph.Caret + " ")
			nameStyle = th.Plain
		}
		name := "@" + c.Text
		suffix := c.Info
		if c.Kind == "child" && suffix == "" {
			suffix = m.loc.T("at.child")
		}
		line := cursor + nameStyle().Render(name)
		if suffix != "" {
			line += th.Faint().Render("  " + suffix)
		}
		lines = append(lines, truncDisplay(line, w-2))
	}
	lines = append(lines, th.Faint().Render(m.loc.T("menu.footer", in.atCur+1, len(in.atMenu))))
	return lines
}

// queueStrip renders the pending lines above the input (LED + dim text):
// the queued inbox, and any parked answerable frame (a question batch or
// a tool approval) the user can still open with ctrl+i.
func (m *Model) queueStrip(w int) []string {
	id := m.activeID()
	if id == "" {
		return nil
	}
	snap := m.st.Get(id)
	if snap == nil {
		return nil
	}
	th := m.th
	var lines []string
	if len(snap.Queue) > 0 {
		n := len(snap.Queue)
		first := snap.Queue[0]
		text := protocol.SessionTextMessage(&first.Message)
		text = strings.ReplaceAll(text, "\n", " ")
		if runewidth.StringWidth(text) > w-40 {
			text = truncDisplay(text, w-40)
		}
		lines = append(lines, th.Accent().Render(th.Glyph.Queue+" ")+th.Faint().Render(m.loc.T("queue.pending", n, text)))
	}
	if len(snap.Questions) > 0 || len(snap.Approvals) > 0 {
		key := "queue.approval"
		if len(snap.Questions) > 0 {
			key = "queue.question"
		}
		lines = append(lines, th.Accent().Render(th.Glyph.Queue+" ")+th.Faint().Render(m.loc.T(key)))
	}
	return lines
}

// statusBar is the bottom context-sensitive hint line: the current model
// name and reasoning effort lead it (no line of their own, no break after
// them), the permission chip (the session's current preset) sits left of
// the hints, colored by risk; the live connection and the session's token
// totals close the line.
// updateTermTitle refreshes the xterm title (OSC 0) on state or identity
// changes — multi-pane setups read the title, not the transcript.
func (m *Model) updateTermTitle() {
	mark, name := "✓", "dsh-cli"
	if id := m.activeID(); id != "" {
		if snap := m.st.Get(id); snap != nil {
			if snap.Title != "" {
				name = snap.Title
			}
			if len(snap.Questions) > 0 || len(snap.Approvals) > 0 {
				mark = "?"
			}
		}
	}
	if m.running() {
		mark = "●"
	}
	t := mark + " " + textutil.StripANSI(name)
	if t == m.termTitle {
		return
	}
	m.termTitle = t
	if !probeLive() {
		return
	}
	// OSC 0 (ST = ESC \\) — written straight to the tty, the same lane
	// the theme probe uses; the diffing renderer never sees it.
	_, _ = probeOutput().WriteString("\x1b]0;" + t + "\x1b\\")
}

func (m *Model) statusBar(w int) string {
	th := m.th
	ws, wsPlain := "", ""
	if id := m.activeID(); id != "" {
		if w := m.st.WorkspaceForSession(id); w != nil {
			label := workspacePathLabel(w)
			ws = th.Subtle().Render(th.Glyph.Folder+" "+label) + " · "
			wsPlain = th.Glyph.Folder + " " + label + " · "
		}
	}
	hints := m.loc.T("status.hints.idle")
	if m.running() {
		hints = m.loc.T("status.hints.running")
	}
	if !m.follow && m.followBase > 0 {
		// Scrolled up while the tail grows: count the lines landed since
		// follow disarmed (end / pgdn jump back and re-arm).
		if n := m.trans.total() - m.followBase; n > 0 {
			hints = strings.TrimRight(hints, " ") + "  " + m.loc.T("status.newlines", fmt.Sprintf("%d", n))
		}
	}
	if m.dockVisible {
		hints = m.loc.T("status.hints.dock")
	}
	if m.sideVisible {
		hints = m.loc.T("status.hints.side")
	}
	// A plain-http remote (http:// off-loopback) sends prompts and tool
	// args in the clear: the marker stays on the connection readout for
	// the whole session — the one-shot pre-alt-screen note is long gone
	// by the time the user is in the UI.
	conn := th.Ok().Render(th.Glyph.Connected + " " + m.loc.T("status.live"))
	if !m.st.Connected() {
		conn = th.Warn().Render(th.Glyph.Reconnect + " " + m.loc.T("status.reconnect"))
	} else if config.PlainHTTP(m.st.BaseURL()) {
		conn = th.Warn().Render(th.Glyph.Connected + " " + m.loc.T("status.plainhttp"))
	}
	perm, permPlain := "", ""
	if id := m.activeID(); id != "" {
		if snap := m.st.Get(id); snap != nil && snap.Permission != nil && snap.Permission.CurrentValue != "" {
			permPlain = th.Glyph.Shield + " " + permissionLabel(snap.Permission.CurrentValue) + " "
			perm = th.PermChip(snap.Permission.CurrentValue).Render(
				th.Glyph.Shield+" "+permissionLabel(snap.Permission.CurrentValue)) + " "
		}
	}
	tok, tokPlain := "", ""
	if m.activeID() != "" {
		in, out := m.tokenTotals()
		// The in/out arrows are theme glyphs prepended here ("tokens:
		// ↓1.6M ↑21.1K"), not words in the status.tokens catalog
		// template: a user-edited locale file may localize the label and
		// separator but never the arrows.
		tokPlain = m.loc.T("status.tokens",
			th.Glyph.TokenIn+compactInt(int64(in)),
			th.Glyph.TokenOut+compactInt(int64(out)))
		tok = th.Faint().Render(tokPlain)
	}
	// Two trailing spaces set the context bar (icon, cells, percentage)
	// apart from the hints whenever the bar is on. The localized hint
	// carries no trailing separator of its own (user-edited locale
	// files included), so the pad is owned here, normalized to a
	// constant two.
	ctxBar, ctxPlain := m.ctxBar()
	if ctxPlain != "" {
		hints = strings.TrimRight(hints, " ") + "  "
	}
	avail := w - runewidth.StringWidth(wsPlain+permPlain) - runewidth.StringWidth(hints) -
		runewidth.StringWidth(conn) - runewidth.StringWidth(tokPlain) -
		runewidth.StringWidth(ctxPlain) - 2
	if avail < 1 {
		avail = 1
	}
	return ws + perm + th.Faint().Render(hints) + ctxBar + strings.Repeat(" ", avail) + conn + tok
}

// tokenTotals sums the active session's assistant usage into the same
// in/out aggregation the turn markers use: in counts prompt input plus
// cache reads, out counts generated output.
func (m *Model) tokenTotals() (in, out int) {
	id := m.activeID()
	if id == "" {
		return 0, 0
	}
	snap := m.st.Get(id)
	if snap == nil {
		return 0, 0
	}
	for _, it := range snap.Items {
		if it.Kind == core.KindAssistant && it.Usage != nil {
			in += it.Usage.InputTokens + it.Usage.CacheReadTokens
			out += it.Usage.OutputTokens
		}
	}
	return in, out
}

// modelReadout renders the active session's current model name and
// reasoning effort; the top bar carries it (the bottom line no longer
// does). Both come back "" when no model is known yet, so the chrome stays
// clean instead of carrying a placeholder.
func (m *Model) modelReadout() (styled, plain string) {
	th := m.th
	name := m.ctxLabel()
	effort := ""
	if id := m.activeID(); id != "" {
		if snap := m.st.Get(id); snap != nil && snap.ModelSel != nil {
			effort = snap.ModelSel.ReasoningEffort
		}
	}
	if name == "" {
		return "", ""
	}
	styled = th.Subtle().Render(name)
	plain = name
	if effort != "" {
		styled += th.Faint().Render(" · " + effort)
		plain += " · " + effort
	}
	return styled, plain
}

// ---- dock ----------------------------------------------------------------------

// Tab order = display order on the dock bar; the 1..5 jump keys
// address the tabs in this order.
const (
	dockTodos = iota
	dockJobs
	dockSubs
	dockQueue
	dockGoal
)

// dockView renders the right-hand dock: flat small-caps tabs (the active
// tab is the only lit one) over a hairline rule, Braun-style.
func (m *Model) dockView(w, h int) string {
	th := m.th
	id := m.activeID()
	snap := (*core.Snapshot)(nil)
	if id != "" {
		snap = m.st.Get(id)
	}
	tabNames := []string{
		m.loc.T("dock.tab.todos"), m.loc.T("dock.tab.jobs"),
		m.loc.T("dock.tab.subs"), m.loc.T("dock.tab.queue"),
		m.loc.T("dock.tab.goal"),
	}
	var tabs []string
	for i, name := range tabNames {
		if i == m.dockTab {
			tabs = append(tabs, th.Accent().Render(name))
		} else {
			tabs = append(tabs, th.Faint().Render(name))
		}
	}
	body := []string{
		"  " + strings.Join(tabs, "  "),
		th.Rule().Render(strings.Repeat("─", maxInt(1, w-2))),
		"",
	}
	if snap == nil {
		body = append(body, "  "+m.loc.T("no.session"))
	} else {
		switch m.dockTab {
		case dockJobs:
			if len(snap.Jobs) == 0 {
				body = append(body, "  "+m.loc.T("dock.no.jobs"))
			}
			for _, j := range snap.Jobs {
				glyph, st := "◐", th.Accent
				switch j.Status {
				case "completed":
					glyph, st = "✓", th.Ok
				case "failed":
					glyph, st = "✗", th.Err
				case "killed":
					glyph, st = "⏹", th.Warn
				case "stopping":
					glyph, st = "◍", th.Warn
				}
				dur := "0s"
				if j.StartedAt > 0 {
					if j.FinishedAt > 0 {
						dur = textutil.HumanDuration(time.UnixMilli(j.FinishedAt).Sub(time.UnixMilli(j.StartedAt)))
					} else {
						dur = textutil.HumanDuration(time.Since(time.UnixMilli(j.StartedAt)))
					}
				}
				line := fmt.Sprintf("  %s %-10s %s %s", st().Render(glyph), j.Status, j.Label, th.Faint().Render(dur))
				body = append(body, truncDisplay(line, w-2))
				if j.Detail != "" {
					body = append(body, "     "+th.Faint().Render(j.Detail))
				}
			}
		case dockTodos:
			if len(snap.Todos) == 0 {
				body = append(body, "  "+m.loc.T("dock.no.todos"))
			}
			done := 0
			for _, t := range snap.Todos {
				if t.Status == "completed" {
					done++
				}
			}
			body = append(body, th.Faint().Render(fmt.Sprintf("  %d/%d", done, len(snap.Todos))))
			for _, t := range snap.Todos {
				glyph, st := "□", th.Subtle
				switch t.Status {
				case "in_progress":
					glyph, st = "◐", th.Accent
				case "completed":
					glyph, st = "✓", th.Ok
				}
				body = append(body, truncDisplay("  "+st().Render(glyph+" "+t.Content), w-2))
			}
		case dockGoal:
			if snap.Goal == nil {
				body = append(body, "  "+m.loc.T("dock.no.goal"))
			} else {
				g := snap.Goal
				body = append(body, truncDisplay("  "+th.Plain().Render(g.Goal.Objective), w-2))
				body = append(body, th.Faint().Render(
					m.loc.T("dock.goal.line", g.Goal.Phase, g.RoundsStarted, g.Goal.MaxGoalRounds)))
			}
		case dockQueue:
			if len(snap.Queue) == 0 {
				body = append(body, "  "+m.loc.T("dock.no.queue"))
			}
			for i, q := range snap.Queue {
				text := protocol.SessionTextMessage(&q.Message)
				text = strings.ReplaceAll(text, "\n", " ")
				body = append(body, truncDisplay(fmt.Sprintf("  %d. [%s] %s", i+1, q.Placement, text), w-2))
			}
		case dockSubs:
			// A stale catalog still renders its last rows (the dirty pulse
			// re-fetches while the tab is on screen); loading… is only for
			// the cold first page.
			entries, fresh := subagentEntries(id)
			if len(entries) == 0 && !fresh {
				body = append(body, "  "+th.Faint().Render(m.loc.T("dock.subs.loading")))
				break
			}
			if len(entries) == 0 {
				body = append(body, "  "+m.loc.T("dock.no.subs"))
				break
			}
			for i, e := range entries {
				glyph, st := "○", th.Faint
				label, tag := e.Label, ""
				switch {
				case e.Kind == "diagnostic":
					glyph, st, label = "!", th.Warn, e.Reason
				case e.Activity == "running":
					glyph, st = "●", th.Accent
				}
				if e.Kind == "child" {
					if label == "" {
						label = e.Id
					}
					if e.Mode == "one-shot" {
						tag = m.loc.T("dock.subs.oneshot")
					}
					if e.HasChildren {
						if tag != "" {
							tag += " · "
						}
						tag += m.loc.T("dock.subs.children")
					}
				}
				prefix := "  "
				if i == m.subDockCur {
					prefix = th.Accent().Render(th.Glyph.Caret + " ")
				}
				line := prefix + st().Render(glyph+" ") + th.Plain().Render(label)
				if tag != "" {
					line += th.Faint().Render("  " + tag)
				}
				body = append(body, truncDisplay(line, w-2))
			}
			body = append(body, th.Faint().Render("  "+m.loc.T("dock.subs.hint")))
		}
	}
	lines := padLines(body, w, h)
	return strings.Join(lines, "\n")
}

// ---- small helpers ---------------------------------------------------------------

// fillRowWidth extends ln with bare-ground (default-state) cells up to a
// total display width of w; a line that already reaches w or runs wider
// passes through byte-identical (the renderer truncates wide lines
// itself). Why: the standard renderer erases a short line's tail with
// EraseLineRight, which paints with whatever background is active at the
// line's end — a popup row spliced over a short underlying line (a toast,
// a queue line, an empty transcript row) ends on the window's card
// border, so the erase smears the card surface into the right margin and
// covers the underlying text there. crush's screen-buffer renderer never
// leaves a row short, which is why its dialogs don't show the slab. The
// pad is bare: a reset is emitted first when the line ends styled, so the
// pad cells carry no foreground, background or attribute.
func fillRowWidth(ln string, w int) string {
	pw := plainWidth(ln)
	if pw >= w {
		return ln
	}
	if cells := scanStyled(ln); len(cells) > 0 && !cells[len(cells)-1].state.empty() {
		var bare sgrState
		ln += bare.marshalOrReset()
	}
	return ln + strings.Repeat(" ", w-pw)
}

// fillRows extends every line of a whole frame with bare-ground cells up
// to width w, so no rendered line is ever shorter than the terminal and
// the renderer's EraseLineRight never fires on a styled tail.
func fillRows(frame string, w int) string {
	if w <= 0 {
		return frame
	}
	lines := strings.Split(frame, "\n")
	for i, ln := range lines {
		lines[i] = fillRowWidth(ln, w)
	}
	return strings.Join(lines, "\n")
}

// fitWidth normalizes a styled line to exactly w display columns — the
// grid contract the panes compose against. crush draws every component
// into its fixed rectangle on a shared screen buffer, so a component's
// overflow simply never lands and its rectangle is always fully defined;
// the string columns hjoin stitches need the same property, or a ragged
// line shifts the neighbor pane's cells one-for-one (the dock drift).
// Overflow is clipped at a cell boundary: a wide rune that starts in-bound
// but overruns the edge becomes a styled blank in the one column that
// still fits (ultraviolet's too-wide rule), carrying its combinators with
// the dropped glyph. The shortfall is bare ground: a line still carrying
// an active SGR state at its tail closes it first, so the pad cells
// inherit no color (a trailing reset already in the line makes the close
// a no-op). Every surviving cell re-emits its SGR state at the
// transition, so a styled segment crossing the clip edge keeps its style
// on both sides; a plain line stays byte-identical (the fast path below).
func fitWidth(s string, w int) string {
	pw := plainWidth(s)
	if pw == w {
		return s
	}
	if pw < w {
		if ts := tailState(s); ts.empty() {
			// The line leaves the terminal in the default state (it ends on
			// a reset, or never styled at all): the old padCell's bytes.
			return s + strings.Repeat(" ", w-pw)
		}
		// A live tail state would color the pad: re-emit the line and close
		// the state before the bare-ground fill.
		b, tail := emitCells(scanStyled(s), w)
		if !tail.empty() {
			b += "\x1b[0m"
		}
		return b + strings.Repeat(" ", w-pw)
	}
	// Clip: re-emit only the cells that fit (the walk stops at the edge).
	b, _ := emitCells(scanStyled(s), w)
	return b
}

// tailState is the SGR state a terminal ends in after consuming s: the
// escape sequences apply in order and the visible characters change
// nothing. A line ending on a reset (or one that never styled) leaves
// the default state; a line ending mid-style keeps that style live for
// whatever the renderer writes next.
func tailState(s string) sgrState {
	var st sgrState
	for i := 0; i+1 < len(s); i++ {
		if s[i] != 0x1b || s[i+1] != '[' {
			continue
		}
		j := i + 2
		for j < len(s) && s[j] >= 0x20 && s[j] <= 0x3f {
			j++
		}
		if j < len(s) && s[j] == 'm' {
			st.apply(parseSGRParams(s[i+2 : j]))
			i = j
		}
	}
	return st
}

// emitCells re-emits a styled line cell by cell through the SGR state,
// stopping at display column w: a short line re-emits whole (the pad
// path), an over-long line stops at the edge (the clip path). It returns
// the emitted bytes and the state of the last emitted cell (the empty
// state when nothing was emitted). A wide rune straddling the edge
// becomes a styled blank in its in-bound column (ultraviolet's too-wide
// rule) and the loop stops there; zero-width combinators sharing its
// column ride with the dropped glyph.
func emitCells(cells []styleCell, w int) (string, sgrState) {
	var b strings.Builder
	var prev sgrState
	started := false
	emit := func(st sgrState, r rune) {
		if !started {
			if !st.empty() {
				b.WriteString(st.marshal())
			}
			started = true
		} else if !prev.equals(st) {
			b.WriteString(st.marshalOrReset())
		}
		prev = st
		b.WriteRune(r)
	}
	for i := 0; i < len(cells); i++ {
		c := cells[i]
		if c.col >= w {
			break
		}
		if c.col+c.w > w {
			// In-bound start, overrun: a wide rune keeps its first column
			// as a styled blank; a 1-wide cell cannot reach this branch
			// (col < w implies col+cw <= w).
			if c.w > 1 {
				emit(c.state, ' ')
				for i+1 < len(cells) && cells[i+1].col == c.col {
					i++ // the clipped glyph's combinators
				}
			}
			break
		}
		emit(c.state, c.r)
	}
	return b.String(), prev
}

// padLines returns exactly h rows of exactly w display columns each: the
// pane's rectangle is fully defined on every row, ragged or missing
// included — the screen-buffer contract hjoin composes against. Missing
// rows are bare ground (no state), like the renderer's own erasure.
func padLines(lines []string, w, h int) []string {
	out := make([]string, h)
	for i := range out {
		if i < len(lines) {
			out[i] = fitWidth(lines[i], w)
		} else if w > 0 {
			out[i] = strings.Repeat(" ", w)
		}
	}
	return out
}

var ansiSeqRe = regexp.MustCompile("\x1b\\[[0-9;?]*[A-Za-z]")

// plainWidth returns the visible display width of s; ANSI sequences are
// ignored (runewidth would count every byte of them).
func plainWidth(s string) int {
	return runewidth.StringWidth(ansiSeqRe.ReplaceAllString(s, ""))
}

// truncDisplay truncates a styled string to a display width, closing the
// cut with a "…" that rides the last surviving cell's style. The cut
// lands on a cell boundary (a wide rune that would overrun the final
// column is dropped whole — the pane-clip rule), and every segment
// re-marshals its SGR state at the transition, so the tail ends on a
// clean state the ellipsis inherits. The old plain-text walk counted the
// escape bytes as cells (and could break mid-sequence), so a styled line
// truncated well before its true width — the dock's job line went to
// "  ◐ running  …" inside a budget it actually fit.
func truncDisplay(s string, w int) string {
	if plainWidth(s) <= w {
		return s
	}
	cells := scanStyled(s)
	var b strings.Builder
	var prev sgrState
	started := false
	last := sgrState{}
	for _, c := range cells {
		// Leave the final column for the ellipsis.
		if c.col+c.w > w-1 {
			break
		}
		if !started {
			if !c.state.empty() {
				b.WriteString(c.state.marshal())
			}
			started = true
		} else if !prev.equals(c.state) {
			b.WriteString(c.state.marshalOrReset())
		}
		prev = c.state
		b.WriteRune(c.r)
		last = c.state
	}
	// The ellipsis: after the re-emission the terminal is exactly in the
	// last cell's state (or bare for plain input), so a bare rune rides it.
	// A live tail state is closed before the return: the truncated line is
	// a self-contained unit, and an open state would otherwise bleed into
	// the next frame line the renderer writes right after it.
	b.WriteRune('…')
	if !last.empty() {
		b.WriteString("\x1b[0m")
	}
	return b.String()
}

// spliceRow overlays a popup row (display width ww) on the underlying
// frame row below at display column ox: the popup's opaque cells take
// [ox, ox+ww); below's own content and styling keep showing through
// the left and right margins — and through the popup's hole cells (bare
// spaces with no SGR state, crush's transparent dialog: the frame's
// padding, blank tail and space after the hint), so the transcript
// behind the popup survives around and behind the box. A whole-row
// paste would leave the margins as plain or card-colored erasures (the
// renderer's EraseLineRight paints a short row's tail with whatever
// background is active), covering them.
// Every cell is re-emitted with the SGR state re-marshaled at each style
// transition, so a styled segment crossing a splice boundary keeps its
// full styling on both sides; an underlying wide rune straddling a
// splice edge becomes its styled space so the popup's edge stays
// column-exact.
func spliceRow(below, row string, ox, ww int) string {
	bc := scanStyled(below)
	rc := scanStyled(row)
	var (
		out     strings.Builder
		bi, ri  int
		prev    sgrState
		started bool
	)
	emit := func(c styleCell, r rune) {
		if r == 0 {
			r = c.r
		}
		if !started || !prev.equals(c.state) {
			out.WriteString(c.state.marshalOrReset())
			prev = c.state
			started = true
		}
		out.WriteRune(r)
	}
	// Left margin: below's cells strictly before the window.
	leftEnd := 0
	for bi < len(bc) && bc[bi].col < ox {
		c := bc[bi]
		if c.col+c.w > ox {
			// A wide underlying rune runs under the window's left edge:
			// keep its column, drop the glyph.
			emit(c, ' ')
			leftEnd = ox
		} else {
			emit(c, 0)
			leftEnd = c.col + c.w
		}
		bi++
	}
	// Default-space padding covers whatever of the left margin the
	// underlying row doesn't have (an empty transcript row: the box
	// stays centered on bare ground).
	for leftEnd < ox {
		emit(styleCell{r: ' ', w: 1, state: sgrState{}}, 0)
		leftEnd++
	}
	// The popup's cells over [ox, ox+ww). Opaque cells take their
	// columns; a hole — a bare space carrying no SGR state — lets the
	// underlying cell show through, so the transcript survives behind
	// the dialog's transparent cells (crush's floating dialog). bcWin
	// tracks the underlying cells the window has accounted for.
	bcWin := bi
	for ri < len(rc) && rc[ri].col < ww {
		c := rc[ri]
		if c.r == ' ' && c.state.empty() {
			hc := ox + c.col
			he := hc + c.w
			pass := 0 // hole columns already emitted
			// An underlying wide cell straddling the hole's left edge:
			// keep its in-hole column as the styled space.
			if bcWin < len(bc) && bc[bcWin].col < hc && bc[bcWin].col+bc[bcWin].w > hc {
				emit(bc[bcWin], ' ')
				bcWin++
				pass++
			}
			for bcWin < len(bc) && bc[bcWin].col < hc {
				bcWin++
			}
		holeCell:
			for bcWin < len(bc) && bc[bcWin].col < he {
				uc := bc[bcWin]
				if uc.col+uc.w > he {
					// A wide underlying cell overruns the hole: its
					// in-hole column becomes a styled space. Its tail
					// column is he: under a hole that follows, keep it as
					// the styled space now; under an opaque cell, consume
					// and let the cell cover it; at the window's right
					// edge, leave the cell unconsumed so the right margin
					// below keeps the tail.
					emit(uc, ' ')
					pass++
					if he < ox+ww {
						if ri+1 < len(rc) && rc[ri+1].col == he &&
							rc[ri+1].r == ' ' && rc[ri+1].state.empty() {
							emit(uc, ' ')
							pass++
						}
						bcWin++
					}
					break holeCell
				}
				emit(uc, 0)
				pass += uc.w
				bcWin++
			}
			// Bare ground: the underlying row ends inside the hole
			// (an empty transcript row) — the hole stays a bare space.
			for pass < he-hc {
				emit(styleCell{r: ' ', w: 1, state: sgrState{}}, 0)
				pass++
			}
		} else {
			emit(c, 0)
		}
		ri++
	}
	// Right margin: below's cells from the window's right edge on. Cells
	// the holes already passed through are skipped (bcWin).
	j := bi
	for j < len(bc) && bc[j].col < ox+ww {
		j++
	}
	if j > 0 && bc[j-1].col < ox+ww && bc[j-1].col+bc[j-1].w > ox+ww && bcWin <= j-1 {
		// A wide underlying rune ends inside the window: keep its tail
		// column as the styled space so the right margin stays aligned.
		emit(bc[j-1], ' ')
	}
	bi = j
	for bi < len(bc) {
		emit(bc[bi], 0)
		bi++
	}
	return out.String()
}

var _ context.Context
var _ tea.Msg

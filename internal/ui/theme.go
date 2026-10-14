// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

// Package ui is the bubbletea presentation layer of dsh-cli.
//
// The interface follows the Braun (Dieter Rams) design language: warm
// neutrals and "biscuit" surfaces, one orange accent used like a device
// LED, muted state colors, and flat sections separated by hairline rules
// instead of heavy borders — as little design as possible.
package ui

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/mattn/go-isatty"

	"github.com/charmbracelet/lipgloss"
	"github.com/lucasb-eyer/go-colorful"
	"github.com/muesli/termenv"
)

// Theme carries every color and derived style of the interface.
type Theme struct {
	Profile  termenv.Profile
	NoColor  bool
	LightBgn bool
	// SysTheme is true when the palette was derived from the terminal's
	// own theme (OSC 11/10 or COLORFGBG) instead of the built-in Braun
	// colors.
	SysTheme bool
	sysBG    colorful.Color
	sysFG    colorful.Color

	CFG     string
	CDim    string
	CFaint  string
	CAccent string
	COK     string
	CWarn   string
	CErr    string
	CInfo   string
	CBorder string
	// Voice colors carried by character color, not by a background
	// surface (Braun Design rule: black/white/gray + the brand orange,
	// nothing tinted in between): the user line (formerly the input-bar
	// background) wears the Braun orange, code (formerly the card
	// background) a Braun neutral gray.
	CUser string
	CCode string

	// CBarBG is the input-bar surface. The built-in palettes leave it
	// empty: the bar (frame, prompt, text and padding alike) is
	// transparent and rides the terminal's own background.
	// CCardBG is the popup/session-window surface: a raised panel, one
	// step off the page (above it on a dark theme, below it on a light
	// one) so the box stands out from the conversation behind it, and
	// still opaque — nothing of the screen shows through. CCardEdge is
	// the popup frame's hairline, a step further off the page than the
	// panel it outlines.
	CBarBG    string
	CModalBG  string
	CCardBG   string
	CCardEdge string
	// CCodeBG is the code-region surface: a slab sunk into the page under
	// run_code cards — a shade darker than the window background — so a
	// stretch of executed code is visible at a glance without the weight
	// of a popup panel. Empty (NoColor) leaves the region on the page.
	CCodeBG string
	CSysFG  string

	// Named-style cache (see styleSet): styleGen bumps on every palette
	// rebuild; the key folds in the render profile so a post-construction
	// profile override (tests) rebuilds too.
	styleGen  uint64
	cachedKey uint64
	styles    styleSet
	stylesOk  bool

	// glyphs
	Glyph struct {
		Bullet    string
		Asterisk  string
		Caret     string
		Cursor    string
		Dot       string
		Check     string
		Cross     string
		Warn      string
		Spinner   [4]string
		Tool      string
		Command   string
		Thought   string
		New       string
		Queue     string
		Connected string
		Reconnect string
		Elapsed   string
		Zap       string
		Fork      string
		Shield    string
		Folder    string
		// TokenIn/TokenOut are the status bar's in/out arrows (↓/↑).
		// They are appended to the compact numbers by the caller rather
		// than baked into the status.tokens catalog template, so a
		// user-edited locale file can localize the label without ever
		// changing the arrows.
		TokenIn  string
		TokenOut string
		// Ctx is the status bar's context-occupancy icon (∞): it leads the
		// usage bar and rides the bar's level color, one unit with it.
		Ctx string
	}
}

// NewTheme builds the palette from the terminal's capabilities.
func NewTheme() *Theme {
	profile := termenv.ColorProfile()
	noColor := os.Getenv("NO_COLOR") != ""
	t := &Theme{Profile: profile, NoColor: noColor}
	t.setPalette()
	t.Glyphs()
	return t
}

// themeColorNil reports whether a color reported by the terminal is
// missing: either a nil Color or termenv's NoColor{} sentinel (the value
// an unanswered OSC status report leaves behind).
func themeColorNil(c termenv.Color) bool {
	return c == nil || c == termenv.NoColor{}
}

// The OSC status-report codes the live probe queries: 11 is the default
// background, 10 the default foreground. A report the key reader delivers
// for them starts with oscReply(code) ("NN;" after the Alt-"]" intro).
const (
	oscBGCode = "11"
	oscFGCode = "10"
)

// oscReply is the report prefix for a code: "]11;" / "]10;".
func oscReply(code string) string { return "]" + code + ";" }

// parseOSCReport parses a fully reassembled report ("]11;payload" or
// "]10;payload"): the report code plus the reported color. A false result
// marks the payload truncated (a report split across reader reads) or
// foreign, so the collector keeps assembling.
func parseOSCReport(report string) (code string, c termenv.Color, ok bool) {
	for _, cd := range []string{oscBGCode, oscFGCode} {
		p := oscReply(cd)
		if strings.HasPrefix(report, p) {
			cc, ok := parseOSCColor(report[len(p):])
			return cd, cc, ok
		}
	}
	return "", nil, false
}

// parseOSCColor parses the payload after the report prefix in a status
// report: "rgb:RRRR/GGGG/BBBB" (a terminal may add a fourth alpha
// component), or a decimal 0-255 palette index. A false result marks the
// payload truncated or foreign.
func parseOSCColor(payload string) (termenv.Color, bool) {
	spec := payload
	if i := strings.IndexByte(payload, ':'); i >= 0 {
		spec, payload = payload[:i], payload[i+1:]
	}
	switch spec {
	case "rgb", "rgba":
		parts := strings.Split(payload, "/")
		if len(parts) != 3 && len(parts) != 4 {
			return nil, false
		}
		var hex strings.Builder
		hex.WriteString("#")
		for _, p := range parts[:3] {
			if len(p) != 2 && len(p) != 4 {
				return nil, false
			}
			if _, err := strconv.ParseUint(p[:2], 16, 8); err != nil {
				return nil, false
			}
			hex.WriteString(p[:2])
		}
		return termenv.RGBColor(hex.String()), true
	case "":
		n, err := strconv.Atoi(payload)
		if err != nil || n < 0 || n > 255 {
			return nil, false
		}
		return termenv.ANSIColor(n), true
	default:
		return nil, false
	}
}

// muxTermEnviron is the termenv.Environment the live color probe reads:
// the multiplexer prefix (probeTerm) comes off TERM so the OSC 10/11
// query is actually sent — see liveReport; everything else passes through
// untouched.
type muxTermEnviron struct{ term string }

func (muxTermEnviron) Environ() []string { return os.Environ() }

func (e muxTermEnviron) Getenv(key string) string {
	if key == "TERM" {
		return e.term
	}
	return os.Getenv(key)
}

// probeLive reports whether the OSC 10/11 query is worth writing: a real
// terminal on stdout (termenv's own isTTY discipline), or the query bytes
// would just leak into a pipe — the probe then waits out its deadline
// like an unanswered one.
func probeLive() bool {
	if len(os.Getenv("CI")) > 0 {
		return false
	}
	f, ok := probeOutput().Writer().(*os.File)
	if !ok {
		return false
	}
	return isatty.IsTerminal(f.Fd())
}

// probeTerm returns the TERM value the OSC 10/11 status report should be
// gated on. termenv bails out of the query when TERM carries a "tmux-"
// or "screen-" prefix, but recent multiplexers forward a pane's query
// and answer it themselves — tmux (≥3.4) from the color its attached
// terminal last reported or that was written into the pane, which is
// what desktop theme switchers push into every pane tty (osc 10/11 +
// osc 4 palette) on each theme change. Bailing out on the
// prefix would leave the probe reading a static COLORFGBG snapshot (or
// termenv's hard default) forever.
func probeTerm() string {
	term := os.Getenv("TERM")
	if os.Getenv("TMUX") != "" {
		term = strings.TrimPrefix(term, "tmux-")
	}
	if os.Getenv("STTY") != "" {
		term = strings.TrimPrefix(term, "screen-")
	}
	if term == "tmux" || term == "screen" {
		term = ""
	}
	return term
}

var (
	probeOutOnce sync.Once
	probeOut     *termenv.Output
)

// probeOutput is the shared termenv output for the OSC 10/11 probe: a
// fresh (cache-less) output on os.Stdout gated on probeTerm, so every
// call re-arms a real query — which is what the live theme probe needs.
func probeOutput() *termenv.Output {
	probeOutOnce.Do(func() {
		probeOut = termenv.NewOutput(os.Stdout,
			termenv.WithEnvironment(muxTermEnviron{term: probeTerm()}))
	})
	return probeOut
}

// liveReport returns the terminal's current default background and
// foreground. The answer chain is the terminal's own OSC 10/11 status
// report (relayed by the multiplexer when one is in front), COLORFGBG,
// and finally termenv's hard default — which ansiFallback detects.
func liveReport() (bg, fg termenv.Color) {
	o := probeOutput()
	return o.BackgroundColor(), o.ForegroundColor()
}

// ansiFallback reports that c is termenv's hard-coded stand-in for an
// OSC 10/11 query that got no answer and no usable COLORFGBG either
// (default black / default white): c is a termenv.ANSIColor while the
// COLORFGBG environment is empty — the "color" came from termenv's
// fallback chain, not from the terminal.
func ansiFallback(c termenv.Color) bool {
	_, ok := c.(termenv.ANSIColor)
	return ok && strings.TrimSpace(os.Getenv("COLORFGBG")) == ""
}

// sysCapable reports whether the color profile can carry the blended system
// palette (256 colors or better). termenv orders its profiles
// strongest-first (TrueColor=0 ... Ascii=3), so "at least 256 colors" is a
// <= comparison — an intuitive >= would silently drop TrueColor terminals.
func (t *Theme) sysCapable() bool {
	return !t.NoColor && t.Profile <= termenv.ANSI256
}

// setPalette fills the interface palette. When the terminal reports its
// own theme — the OSC 11 background / OSC 10 foreground status reports,
// with the COLORFGBG environment as the fallback — and the color profile
// can carry the result, every neutral (text ramp, surfaces, border, code)
// is derived from those system colors so the UI blends into whatever theme
// the user runs. The built-in Braun palettes — warm graphite on dark
// surfaces, biscuit paper with a burnt-orange accent on light ones, plain
// intensity levels under NO_COLOR — stay the fallback for profiles that
// cannot express the derived colors or terminals that answer nothing.
func (t *Theme) setPalette() {
	if t.sysCapable() {
		// themeColorNil covers nil and termenv's NoColor{} sentinel (an
		// unanswered OSC 11/10 query: piped stdout, go test); ansiFallback
		// catches the same non-report on a live terminal, where termenv
		// returns its hard default instead. Both fall back to the
		// built-in palettes.
		if bg, fg := liveReport(); !themeColorNil(bg) && !ansiFallback(bg) {
			t.setSystemPalette(bg, fg)
			return
		}
	}
	if t.NoColor {
		t.CFG, t.CDim, t.CFaint = "default", "251", "245"
		t.CAccent, t.COK, t.CWarn, t.CErr, t.CInfo = "default", "default", "180", "204", "default"
		t.CUser, t.CCode = "default", "253"
		t.CBorder, t.CBarBG, t.CModalBG, t.CCardBG, t.CSysFG =
			"240", "", "", "default", "246"
		t.CCardEdge = "240"
		t.CCodeBG = ""
		return
	}
	if t.LightBgn {
		// Biscuit / eggshell: warm paper surfaces, burnt-orange accent.
		t.CFG, t.CDim, t.CFaint = "#2D2A25", "#6F6A5F", "#948E80"
		t.CAccent, t.COK, t.CWarn, t.CErr, t.CInfo = "#C24E14", "#5E7F49", "#8A681F", "#A8492F", "#4D7375"
		t.CUser, t.CCode = "#C24E14", "#595345"
		t.CBorder, t.CModalBG, t.CSysFG =
			"#D8D1C2", "#FAF6EC", "#7C766A"
		// The popup is a deeper biscuit panel with a burnt edge: a step
		// off the paper, so it reads as a window over the conversation.
		t.CCardBG, t.CCardEdge = "#F0E9D9", "#C6BCA6"
		// The code slab sinks into the paper: a shade deeper than the page,
		// enough to fence a run without framing it.
		t.CCodeBG = "#F2ECDE"
		return
	}
	// Warm graphite with the Braun orange accent.
	t.CFG, t.CDim, t.CFaint = "#EDE9E0", "#A8A294", "#67625A"
	t.CAccent, t.COK, t.CWarn, t.CErr, t.CInfo = "#E85A22", "#8FA671", "#CEA44B", "#C4573B", "#7E9D97"
	t.CUser, t.CCode = "#FF5900", "#8F8D86"
	t.CBorder, t.CModalBG, t.CSysFG =
		"#3A362F", "#211F1B", "#918B7E"
	// The popup is a raised graphite panel with a lit edge: a step above
	// the page, so it reads as a window over the conversation.
	t.CCardBG, t.CCardEdge = "#2E2A23", "#514A3E"
	// The code slab sinks below the page: a deep graphite, a shade under a
	// dark window background — a fence for a run of code, quieter than a
	// popup so the page stays the page.
	t.CCodeBG = "#100E0C"
}

// applySystem re-derives the palette from a fresh background/foreground
// report, for the case where the user switches the terminal theme while
// the TUI is already up. It returns true when the palette actually moved
// (the caller then drops the render caches so the next frame repaints
// with the live colors).
func (t *Theme) applySystem(bg, fg termenv.Color) bool {
	if !t.sysCapable() || themeColorNil(bg) || ansiFallback(bg) {
		// ...or the probe's answer was lost (the key reader snatched it,
		// the terminal stayed silent) and termenv returned its hard
		// default: keep the current palette instead of snapping to black.
		return false
	}
	nb := termenv.ConvertToRGB(bg)
	if t.SysTheme && nb.Hex() == t.sysBG.Hex() {
		// Background unchanged: re-apply only when a real foreground was
		// reported and it genuinely moved.
		if themeColorNil(fg) || termenv.ConvertToRGB(fg).Hex() == t.sysFG.Hex() {
			return false
		}
	}
	t.setSystemPalette(bg, fg)
	t.styleGen++ // move the style-cache key
	return true
}

// setSystemPalette derives the palette from the terminal's own theme. The
// system foreground is the text color (blend weight 1), the background the
// page (weight 0); each role's weight is that role's built-in Braun color
// projected onto the same ramp (dark palette on black->white, light one on
// white->black), so the fixed palettes' hierarchy is preserved under any
// theme. Interpolation runs in HSL on the shortest hue arc, so a theme's
// tint survives into dim text and the raised surfaces instead of collapsing
// to gray. The Braun orange keeps its hue as the accent, and the state
// colors keep theirs at lightnesses pinned for the detected surface.
func (t *Theme) setSystemPalette(bg, fg termenv.Color) {
	b := termenv.ConvertToRGB(bg)
	_, _, bl := b.Hsl()
	if themeColorNil(fg) { // no default foreground to query
		if bl < 0.5 {
			fg = termenv.ANSIColor(15)
		} else {
			fg = termenv.ANSIColor(0)
		}
	}
	dark := bl < 0.5
	t.SysTheme = true
	t.LightBgn = !dark
	t.sysBG, t.sysFG = b, termenv.ConvertToRGB(fg)

	// ramp blends the system background (0) and foreground (1) in HSL,
	// hue taking the shortest arc between the two theme colors.
	ramp := func(w float64) string {
		bh, bs, byl := t.sysBG.Hsl()
		fh, fs, fyl := t.sysFG.Hsl()
		dh := fh - bh
		if dh > 180 {
			dh -= 360
		} else if dh < -180 {
			dh += 360
		}
		h := bh + dh*w
		if h < 0 {
			h += 360
		}
		return colorful.Hsl(h, bs+(fs-bs)*w, byl+(fyl-byl)*w).Hex()
	}

	// panel is a popup surface: the page's own hue and saturation with its
	// lightness stepped w of the way toward the text, floored so a flat
	// theme still gets a panel that reads and capped so a high-contrast
	// one gets no garish slab. The hue stays the page's on purpose: a
	// surface the size of a dialog that interpolates toward the text color
	// (as the ramp above does) tints the whole box.
	panel := func(w, limit float64) string {
		h, s, l := t.sysBG.Hsl()
		_, _, fl := t.sysFG.Hsl()
		d := (fl - l) * w
		if d > 0 {
			d = math.Max(d, 0.08)
		} else {
			d = math.Min(d, -0.08)
		}
		d = math.Max(-limit, math.Min(limit, d))
		return colorful.Hsl(h, s, math.Max(0, math.Min(1, l+d))).Hex()
	}

	// sink is a sunken surface: the page's own hue and saturation with its
	// lightness stepped w of the way down toward black. Floored so a dark
	// page still gets a step that reads, capped so a light one does not
	// open into a hole.
	sink := func(w, limit float64) string {
		h, s, l := t.sysBG.Hsl()
		if l < 0.03 {
			// A page already at black cannot sink: raise a hair instead,
			// or the code region loses its fence altogether.
			return colorful.Hsl(h, s, l+0.04).Hex()
		}
		d := math.Max(-l*w, -limit)
		if d > -0.05 {
			d = -math.Min(l, 0.05)
		}
		return colorful.Hsl(h, s, math.Max(0, l+d)).Hex()
	}

	// tinted keeps the hue of a built-in reference color and pins its
	// saturation and lightness for the detected surface.
	tinted := func(ref string, s, l float64) string {
		c, err := colorful.Hex(ref)
		if err != nil {
			return ref
		}
		h, _, _ := c.Hsl()
		return colorful.Hsl(h, s, l).Hex()
	}

	// Weight table, both built-in palettes projected onto the one
	// background(0)->foreground(1) ramp: dark and light agree within a
	// step or two, so a single table serves either surface.
	t.CFG = ramp(1) // the terminal's own text color, verbatim
	t.CDim, t.CFaint = ramp(0.61), ramp(0.42)
	t.CCode = ramp(0.62)
	t.CSysFG = ramp(0.54)
	t.CBorder, t.CModalBG =
		ramp(0.21), ramp(0.08)
	// The popups are raised panels: the page's own hue and saturation with
	// its lightness stepped toward the text (up on a dark theme, down on a
	// light one), the frame's hairline a step further out — the box must
	// read as a window laid over the conversation, not as a hole in it.
	t.CCardBG, t.CCardEdge = panel(0.18, 0.14), panel(0.40, 0.28)
	// The code slab sinks instead of rising: the page stepped down toward
	// black, so a code region reads as a shallow recess in the page rather
	// than a box laid over it.
	t.CCodeBG = sink(0.45, 0.07)
	// The input bar stays transparent on the system surface too: no
	// painted plate, the text rides the terminal's own background.
	t.CBarBG = ""

	// The Braun orange keeps its hue; state colors keep theirs.
	if dark {
		t.CAccent = tinted("#E85A22", 0.65, 0.55)
		t.CUser = tinted("#FF5900", 0.70, 0.54)
		t.COK = tinted("#8FA671", 0.30, 0.55)
		t.CWarn = tinted("#CEA44B", 0.55, 0.50)
		t.CErr = tinted("#C4573B", 0.52, 0.50)
		t.CInfo = tinted("#7E9D97", 0.14, 0.50)
		return
	}
	t.CAccent = tinted("#C24E14", 0.60, 0.42)
	t.CUser = t.CAccent
	t.COK = tinted("#5E7F49", 0.30, 0.33)
	t.CWarn = tinted("#8A681F", 0.55, 0.28)
	t.CErr = tinted("#A8492F", 0.45, 0.34)
	t.CInfo = tinted("#4D7375", 0.32, 0.30)
}
func (t *Theme) Glyphs() {
	if t.Profile == termenv.ANSI {
		t.Glyph.Bullet, t.Glyph.Asterisk, t.Glyph.Caret = "o", "*", ">"
		t.Glyph.Cursor, t.Glyph.Dot = "|", "-"
		t.Glyph.Check, t.Glyph.Cross, t.Glyph.Warn = "+", "x", "!"
		t.Glyph.Spinner = [4]string{".", "o", "O", "O"}
		t.Glyph.Tool, t.Glyph.Command, t.Glyph.Thought = ">", "$", "*"
		t.Glyph.New, t.Glyph.Queue, t.Glyph.Connected, t.Glyph.Reconnect = "n", "q", "o", "r"
		t.Glyph.Elapsed, t.Glyph.Zap, t.Glyph.Fork = "t", "z", "f"
		t.Glyph.Shield = "s"
		t.Glyph.Folder = "w"
		t.Glyph.TokenIn, t.Glyph.TokenOut = "v", "^"
		t.Glyph.Ctx = "oo"
		return
	}
	t.Glyph.Bullet = "●"
	t.Glyph.Asterisk = "⚡"
	t.Glyph.Caret = "❯"
	t.Glyph.Cursor = "▊"
	t.Glyph.Dot = "·"
	t.Glyph.Check = "✓"
	t.Glyph.Cross = "✗"
	t.Glyph.Warn = "⚠"
	t.Glyph.Spinner = [4]string{"◐", "◓", "◑", "◒"}
	t.Glyph.Tool = "▸"
	t.Glyph.Command = "⌘"
	t.Glyph.Thought = "◌"
	t.Glyph.New = "◉"
	t.Glyph.Queue = "⏸"
	t.Glyph.Connected = "●"
	t.Glyph.Reconnect = "↻"
	t.Glyph.Elapsed = "⏱"
	t.Glyph.Zap = "⚡"
	t.Glyph.Fork = "⑂"
	t.Glyph.Shield = "⛨"
	t.Glyph.Folder = "⌂"
	t.Glyph.TokenIn, t.Glyph.TokenOut = "↓", "↑"
	t.Glyph.Ctx = "∞"
}

// Style shortcuts.
type Style = lipgloss.Style

// styleSet is the pre-built named styles: a lipgloss Style is a
// value-type builder, and the hot path (dozens of .Render calls per
// frame) must not pay a NewStyle allocation per call.
type styleSet struct {
	plain, subtle, faint, accent, ok, warn, err, info    Style
	user, code, sel, border, rule, label, system         Style
	bar, barAccent, barCursor, barSel, barBG             Style
	barBGSeq                                             string
	plate, platePlain, plateSubtle, plateFaint           Style
	plateAccent, plateOk, plateWarn, plateErr, plateInfo Style
	plateSystem, plateRule, plateCursor, plateSel        Style
	card, cardEdge, cardCursor, cardSel, sessBand        Style
	cardState                                            sgrState
	codeBG                                               Style
	codeState                                            sgrState
	sweepDim, sweepMid, sweepHot                         Style
}

// S wraps a style builder with theme colors pre-applied.
func (t *Theme) S() Style { return lipgloss.NewStyle() }

func (t *Theme) fg(c string) Style { return lipgloss.NewStyle().Foreground(t.color(c)) }

// styleKey moves when the palette moves (a styleGen bump) or the render
// profile changes (tests override it post-construction).
func (t *Theme) styleKey() uint64 {
	k := t.styleGen * 1315423911
	k ^= uint64(t.Profile) * 2654435761
	if t.NoColor {
		k ^= 0x9e3779b97f4a7c15
	}
	return k
}

// styled returns the pre-built style set, rebuilding it only when the
// palette or the render profile moved.
func (t *Theme) styled() *styleSet {
	if !t.stylesOk || t.cachedKey != t.styleKey() {
		t.styles = t.buildStyles()
		t.cachedKey = t.styleKey()
		t.stylesOk = true
	}
	return &t.styles
}

// buildStyles pre-builds every named style (see styleSet).
func (t *Theme) buildStyles() styleSet {
	var s styleSet
	s.plain = t.fg(t.CFG)
	s.subtle = t.fg(t.CDim)
	s.faint = t.fg(t.CFaint)
	s.accent = t.fg(t.CAccent)
	s.ok = t.fg(t.COK)
	s.warn = t.fg(t.CWarn)
	s.err = t.fg(t.CErr)
	s.info = t.fg(t.CInfo)
	s.user = t.fg(t.CUser)
	s.code = t.fg(t.CCode)
	s.border = t.fg(t.CBorder)
	s.rule = t.fg(t.CBorder)
	edge := t.CCardEdge
	if edge == "" {
		edge = t.CBorder
	}
	s.cardEdge = t.fg(edge)
	s.label = t.fg(t.CDim)
	s.system = t.fg(t.CSysFG)
	if t.NoColor || t.Profile == termenv.ANSI {
		s.sel = lipgloss.NewStyle().Reverse(true)
	} else {
		s.sel = lipgloss.NewStyle().Background(t.color(t.CDim))
	}
	s.bar = t.barBase(t.CFG)
	s.barAccent = t.barBase(t.CAccent)
	s.barCursor = s.barAccent.Underline(true)
	s.barSel = s.bar.Underline(true)
	if t.CBarBG == "" {
		s.barBG = lipgloss.NewStyle()
	} else {
		s.barBG = lipgloss.NewStyle().Background(lipgloss.Color(t.CBarBG))
		raw := s.barBG.Render("x")
		if i := strings.IndexByte(raw, 'x'); i > 0 {
			s.barBGSeq = raw[:i]
		}
	}
	if t.CModalBG == "" {
		s.plate = lipgloss.NewStyle()
	} else {
		s.plate = lipgloss.NewStyle().Background(lipgloss.Color(t.CModalBG))
	}
	s.platePlain = t.pfg(t.CFG)
	s.plateSubtle = t.pfg(t.CDim)
	s.plateFaint = t.pfg(t.CFaint)
	s.plateAccent = t.pfg(t.CAccent)
	s.plateOk = t.pfg(t.COK)
	s.plateWarn = t.pfg(t.CWarn)
	s.plateErr = t.pfg(t.CErr)
	s.plateInfo = t.pfg(t.CInfo)
	s.plateSystem = t.pfg(t.CSysFG)
	s.plateRule = t.pfg(t.CBorder)
	if st := lipgloss.NewStyle().Background(t.color(t.CFG)); t.CModalBG != "" {
		st = st.Foreground(lipgloss.Color(t.CModalBG))
		s.plateCursor = st
	}
	if t.NoColor || t.Profile == termenv.ANSI {
		s.plateSel = lipgloss.NewStyle().Reverse(true)
	} else {
		s.plateSel = lipgloss.NewStyle().Foreground(t.color(t.CFG)).Background(t.color(t.CDim))
	}
	if t.CCardBG == "" {
		s.card = lipgloss.NewStyle()
	} else {
		s.card = lipgloss.NewStyle().Background(lipgloss.Color(t.CCardBG))
	}
	if t.CCardBG == "default" {
		s.cardState = sgrState{bg: []int{49}}
	} else {
		s.cardState = extractSGR(s.card.Render("x"))
	}
	if t.CCodeBG == "" {
		s.codeBG = lipgloss.NewStyle()
	} else {
		s.codeBG = lipgloss.NewStyle().Background(lipgloss.Color(t.CCodeBG))
	}
	if t.CCodeBG == "default" {
		s.codeState = sgrState{bg: []int{49}}
	} else {
		s.codeState = extractSGR(s.codeBG.Render("x"))
	}
	if st := lipgloss.NewStyle().Background(t.color(t.CFG)); t.CCardBG != "" {
		st = st.Foreground(lipgloss.Color(t.CCardBG))
		s.cardCursor = st
	}
	if t.NoColor || t.Profile == termenv.ANSI {
		s.cardSel = lipgloss.NewStyle().Reverse(true)
	} else {
		s.cardSel = lipgloss.NewStyle().Foreground(t.color(t.CFG)).Background(t.color(t.CDim))
	}
	if t.CCardBG == "" || t.CAccent == "default" {
		s.sessBand = lipgloss.NewStyle().Reverse(true)
	} else {
		s.sessBand = lipgloss.NewStyle().Foreground(lipgloss.Color(t.CCardBG)).Background(lipgloss.Color(t.CAccent))
	}
	s.sweepMid = t.fg(t.CAccent)
	s.sweepDim = t.fgScaled(t.CAccent, 0.45)
	s.sweepHot = t.fgScaled(t.CAccent, 1.5)
	return s
}

// barBase is the input-bar text style with the bar surface baked in.
func (t *Theme) barBase(c string) Style {
	st := lipgloss.NewStyle().Foreground(t.color(c))
	if t.CBarBG != "" {
		st = st.Background(lipgloss.Color(t.CBarBG))
	}
	return st
}

func (t *Theme) color(c string) lipgloss.Color {
	if t.NoColor && c == t.CAccent {
		return lipgloss.Color("255")
	}
	return lipgloss.Color(c)
}

// Plain returns a foreground style.
func (t *Theme) Plain() Style  { return t.styled().plain }
func (t *Theme) Subtle() Style { return t.styled().subtle }
func (t *Theme) Faint() Style  { return t.styled().faint }
func (t *Theme) Accent() Style { return t.styled().accent }
func (t *Theme) Ok() Style     { return t.styled().ok }
func (t *Theme) Warn() Style   { return t.styled().warn }
func (t *Theme) Err() Style    { return t.styled().err }
func (t *Theme) Info() Style   { return t.styled().info }

// User is the user-voice foreground: user prompt text rides its own color
// instead of the former input-bar background surface.
func (t *Theme) User() Style { return t.styled().user }

// Code is the code foreground (inline + blocks): code text is told apart
// by color instead of the former card background.
func (t *Theme) Code() Style { return t.styled().code }

// Sel is the text-pick surface (the transcript selection band): a
// background only — the picked lines keep their own character colors, so
// code stays legible on the band. lipgloss composes it over the styled
// row (its cellbuf parser keeps every cell), so the highlight costs one
// library pass per line and never splits a rune at an edge. Under
// NO_COLOR / 8-bit terminals there is no palette to lean on, so the band
// falls back to reverse video.
func (t *Theme) Sel() Style    { return t.styled().sel }
func (t *Theme) Border() Style { return t.styled().border }
func (t *Theme) System() Style { return t.styled().system }

// Rule is the hairline separator style.
func (t *Theme) Rule() Style { return t.styled().rule }

// Label is the small-caps section header style.
func (t *Theme) Label() Style { return t.styled().label }

// sweepShades returns the brightness tiers of the 扫光 highlight in the
// order dim, base, hot — three lightness levels of the same accent hue, so
// the sweep reads as brightness moving across the verb, never a color
// change. Pre-built with the style set (rebuilt on a profile override,
// which is how tests flip the tier).
func (t *Theme) sweepShades() (dim, mid, hot Style) {
	s := t.styled()
	return s.sweepDim, s.sweepMid, s.sweepHot
}

// fgScaled builds a foreground style for the palette color c with its
// brightness scaled by factor (<1 dimmer, >1 brighter, clamped to the
// gamut); the hue is preserved.
func (t *Theme) fgScaled(c string, factor float64) Style {
	col := t.Profile.Color(string(t.color(c)))
	if col == nil {
		return t.fg(c)
	}
	rgb := termenv.ConvertToRGB(col)
	clip := func(v float64) float64 {
		if v < 0 {
			return 0
		}
		if v > 1 {
			return 1
		}
		return v
	}
	hex := fmt.Sprintf("#%02x%02x%02x",
		uint8(clip(rgb.R*factor)*255),
		uint8(clip(rgb.G*factor)*255),
		uint8(clip(rgb.B*factor)*255))
	return lipgloss.NewStyle().Foreground(lipgloss.Color(hex))
}

// PermChip grades one permission preset by risk: read-only stays quiet,
// workspace-write is the safe default (ok green), full access warns.
func (t *Theme) PermChip(value string) Style {
	switch value {
	case "read-only":
		return t.styled().subtle
	case "workspace-write":
		return t.styled().ok
	case "danger-full-access":
		return t.styled().warn
	default:
		return t.styled().plain
	}
}

// ---- input bar surface ----------------------------------------------------------
//
// Each bar helper carries the bar background (CBarBG) into the style it
// returns. The built-in palettes leave CBarBG empty — the bar is
// transparent and rides the terminal's own background — but whenever a
// surface is set, an internally composed segment (for example the accent
// prompt) still emits its own trailing SGR reset on the terminal, which
// would otherwise drop back to the terminal's background behind the typed
// text; baking the background per segment keeps the whole bar surface
// uniform.

// Bar is the plain text style of the input line.
func (t *Theme) Bar() Style { return t.styled().bar }

// BarAccent is the accent (prompt/caret) style of the input line.
func (t *Theme) BarAccent() Style { return t.styled().barAccent }

// BarCursor is the caret cell of the input line: the char under the
// cursor — or a plain space at the line end — drawn in the bar's accent
// and underlined, with no background of its own. The cell rides the bar
// surface: the painted CBarBG when one is set, the terminal's own
// background on the transparent bar — so the caret is a colored,
// underlined char rather than an inverted block.
func (t *Theme) BarCursor() Style {
	return t.styled().barCursor
}

// BarSel is the selection mark of the input line: picked chars keep the
// bar's plain ink and gain an underline — no painted band behind them,
// no reverse video — so a pick reads as an underlined run on the bar
// surface on every profile.
func (t *Theme) BarSel() Style {
	return t.styled().barSel
}

// BarBG is the bare input surface, used for padding.
func (t *Theme) BarBG() Style { return t.styled().barBG }

// BarBGSeq returns the bare SGR escape (ESC[...m) that sets the bar
// background — the exact code lipgloss emits for the input bar surface,
// or "" when the theme has no bar surface. It forces the surface into
// lines composed of pre-styled segments (markdown output) whose internal
// SGR resets would otherwise expose the terminal's own background.
func (t *Theme) BarBGSeq() string {
	// Pre-built from one rendered cell: byte-for-byte the code the
	// input box draws with, regardless of the active color profile.
	return t.styled().barBGSeq
}

// BarSurface re-emits an ANSI-styled line with the bar background forced
// before every visible run. The original SGR sequences are kept (span
// colors survive); the forced surface comes after any reset, so no cell
// of the line can fall back to the terminal's default background.
func (t *Theme) BarSurface(ln string) string {
	seq := t.BarBGSeq()
	if seq == "" {
		return ln
	}
	var sb strings.Builder
	i, n := 0, len(ln)
	for i < n {
		if ln[i] == '\x1b' && strings.HasPrefix(ln[i:], "\x1b[") {
			if end := strings.IndexByte(ln[i+2:], 'm'); end >= 0 {
				sb.WriteString(ln[i : i+end+3])
				i += end + 3
				continue
			}
		}
		j := i
		for j < n && !(ln[j] == '\x1b' && strings.HasPrefix(ln[j:], "\x1b[")) {
			j++
		}
		if j == i {
			j = i + 1 // malformed escape: consume it as a visible byte
		}
		sb.WriteString(seq)
		sb.WriteString(ln[i:j])
		i = j
	}
	return sb.String()
}

// BarStyle returns the foreground style fg with the input-bar surface
// baked in, for transcript segments that share the bar's background (the
// user's own lines).
func (t *Theme) BarStyle(fg Style) Style {
	if t.CBarBG == "" {
		return fg
	}
	return fg.Background(lipgloss.Color(t.CBarBG))
}

// ---- modal plate surface --------------------------------------------------------
//
// Modals float on a flat raised plate (CModalBG). The same reset problem
// as the input bar applies, so every Plate* style bakes the plate
// background into the returned style.

func (t *Theme) pfg(c string) Style {
	st := lipgloss.NewStyle().Foreground(t.color(c))
	if t.CModalBG != "" {
		st = st.Background(lipgloss.Color(t.CModalBG))
	}
	return st
}

// Plate is the bare modal surface, used for padding.
func (t *Theme) Plate() Style { return t.styled().plate }

func (t *Theme) PlatePlain() Style  { return t.styled().platePlain }
func (t *Theme) PlateSubtle() Style { return t.styled().plateSubtle }
func (t *Theme) PlateFaint() Style  { return t.styled().plateFaint }
func (t *Theme) PlateAccent() Style { return t.styled().plateAccent }
func (t *Theme) PlateOk() Style     { return t.styled().plateOk }
func (t *Theme) PlateWarn() Style   { return t.styled().plateWarn }
func (t *Theme) PlateErr() Style    { return t.styled().plateErr }
func (t *Theme) PlateInfo() Style   { return t.styled().plateInfo }
func (t *Theme) PlateSystem() Style { return t.styled().plateSystem }
func (t *Theme) PlateRule() Style   { return t.styled().plateRule }

// PlateCursor is the reverse-video caret cell of modal text fields: the
// char under the cursor (or a plain space at the line end) drawn inverted
// on the plate surface - the same caret style the main input box uses on
// its bar, so modal inputs edit with the identical look and feel.
func (t *Theme) PlateCursor() Style {
	return t.styled().plateCursor
}

// PlateSel is the selection band of the modal text fields: the plate
// ink on the selection background (the transcript band color).
func (t *Theme) PlateSel() Style {
	return t.styled().plateSel
}

// ---- card surface (the popups) ------------------------------------------------
//
// The popups float on the card surface (CCardBG): a raised panel a step
// off the page, so an open box is visible against the conversation
// behind it. Styled segments bake the surface into their own style where
// a concrete color exists; where the palette has no card color the
// popup's row filler (cardLine) still paints every cell the row leaves
// without a background, so no cell of the open panel stays a bare hole
// the underlying screen shows through.

// Card is the bare card surface, used for padding.
func (t *Theme) Card() Style { return t.styled().card }

// CardState is the card surface as the SGR state a cell carries (the
// empty state when the palette has no card color): the popup's row
// filler merges it into every cell the row leaves without a background,
// so the box interior is opaque. "default" is SGR 49 — lipgloss renders
// it as no code at all, so the state is built by hand.
func (t *Theme) CardState() sgrState {
	return t.styled().cardState
}

// CodeState is the code-region surface (the run_code slab) as the SGR
// state a cell carries: cardLine merges it into every cell a card line
// leaves without a background of its own. The empty state (NoColor, or
// a palette without a slab color) leaves the lines on the page.
func (t *Theme) CodeState() sgrState {
	return t.styled().codeState
}

// CardEdge is the popup frame's hairline: a step further off the page
// than the panel it outlines, so the outline still reads on the card.
func (t *Theme) CardEdge() Style { return t.styled().cardEdge }

// CardStyle returns the foreground style fg with the card surface baked in.
func (t *Theme) CardStyle(fg Style) Style {
	if t.CCardBG == "" {
		return fg
	}
	return fg.Background(lipgloss.Color(t.CCardBG))
}

// CardCursor is the reverse-video caret cell of the card surface's edit
// line (the session list's search): the char under the cursor — or a
// plain space at the line end — drawn inverted on the card surface, the
// same caret style the main input box and the modal fields use.
func (t *Theme) CardCursor() Style {
	return t.styled().cardCursor
}

// CardSel is the selection band of the card surface's edit line: the
// card ink on the selection background — the plate fields' band, on the
// card.
func (t *Theme) CardSel() Style {
	return t.styled().cardSel
}

// SessBand is the pick band of the session window (crush's selected
// session row): the brand accent as a solid surface with the card ink on
// top — a full-width inverted row on the window.
func (t *Theme) SessBand() Style { return t.styled().sessBand }

// gradRail is the session window's title rail: an ASCII-art wind streak
// beside the dialog title — '+' / '-' / '.' / '`' cells that flow from a
// dense, near-solid dash line on the left (with '+' knots) through
// shredding dashes, ticks and gaps, to isolated dots and empty cells
// dissolving on the right — like a gust blown from left to right. Each
// cell is also color-interpolated fromHex→toHex, so the streak fades in
// hue as it fades in density. A pair that is not a six-digit hex (ANSI /
// no-color palettes) degrades to a flat dim rail with the same streak.
func (t *Theme) gradRail(fromHex, toHex string, n int) string {
	if n <= 0 {
		return ""
	}
	from, okFrom := parseHexRGB(fromHex)
	to, okTo := parseHexRGB(toHex)
	if !okFrom || !okTo {
		var s strings.Builder
		for i := 0; i < n; i++ {
			s.WriteRune(windGlyph(i, n))
		}
		return t.Label().Render(s.String())
	}
	var b strings.Builder
	last := float64(maxInt(1, n-1))
	for i := 0; i < n; i++ {
		f := float64(i) / last
		r := int(float64(from.r) + (float64(to.r)-float64(from.r))*f)
		g := int(float64(from.g) + (float64(to.g)-float64(from.g))*f)
		bl := int(float64(from.b) + (float64(to.b)-float64(from.b))*f)
		b.WriteString(fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, bl))
		b.WriteRune(windGlyph(i, n))
	}
	b.WriteString("\x1b[0m")
	return b.String()
}

// windGlyph is the one cell the wind streak carries at position i (0 left
// → n-1 right). Deterministic, no randomness: the density falls linearly
// left→right while a sin term undulates it like a gust, an i%5 phase keeps
// the knot rhythm of the dash line, and the bands step the streak from
// solid to dissolved:
//
//	d ≥ .62  solid core   — dash line, '+' knot every 5th cell (the
//	                        trailing dash frays to a '.' as the core thins)
//	.38+     shredding    — dashes breaking into '.' / '`' ticks, knots
//	                        persist
//	.18+     wisps        — isolated ticks in the growing gaps
//	else     dissolving   — a receding dotted tail, one dot in four cells
func windGlyph(i, n int) rune {
	if n == 1 {
		return '+'
	}
	t := float64(i) / float64(n-1)
	d := (1 - t) * (0.94 + 0.06*math.Sin(float64(i)*1.7))
	ph := i % 5
	switch {
	case d >= 0.62:
		if ph == 0 {
			return '+' // knot
		}
		if ph == 4 && d < 0.78 {
			return '.' // the core's edge frays before the next knot
		}
		return '-' // solid run
	case d >= 0.38:
		switch ph {
		case 0:
			return '+'
		case 2:
			return '.'
		case 4:
			return '`'
		default:
			return '-'
		}
	case d >= 0.18:
		switch ph {
		case 0:
			return '.'
		case 3:
			return '`'
		default:
			return ' '
		}
	default:
		if i%4 == 0 {
			return '.'
		}
		return ' '
	}
}

type rgbTriple struct{ r, g, b uint8 }

// parseHexRGB decodes a #rrggbb hex color; false on any other shape
// (ANSI palette names, "default", …) so the caller can fall back.
func parseHexRGB(s string) (rgbTriple, bool) {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return rgbTriple{}, false
	}
	var out rgbTriple
	rest := s
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseUint(rest[:2], 16, 8)
		if err != nil {
			return rgbTriple{}, false
		}
		rest = rest[2:]
		switch i {
		case 0:
			out.r = uint8(v)
		case 1:
			out.g = uint8(v)
		default:
			out.b = uint8(v)
		}
	}
	return out, true
}

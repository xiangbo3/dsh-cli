// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"dsh-cli/internal/app"

	"github.com/charmbracelet/bubbletea"
	"github.com/lucasb-eyer/go-colorful"
	"github.com/muesli/termenv"
)

// sysTheme drives the system-derived palette with fixed theme colors,
// skipping the TTY query that NewTheme performs on a live terminal.
func sysTheme(t *testing.T, bg, fg string) *Theme {
	t.Helper()
	th := &Theme{Profile: termenv.TrueColor}
	th.setPalette() // go test's stdout is not a TTY: the fixed fallback wins
	if th.SysTheme {
		t.Fatal("expected the fixed palette fallback under go test")
	}
	th.setSystemPalette(termenv.RGBColor(bg), termenv.RGBColor(fg))
	return th
}

func parseHex(t *testing.T, s string) colorful.Color {
	t.Helper()
	c, err := colorful.Hex(s)
	if err != nil {
		t.Fatalf("palette color %q is not a hex color: %v", s, err)
	}
	return c
}

// hueDist is the shortest angular distance between two hue degrees.
func hueDist(a, b float64) float64 {
	d := math.Mod(a-b+360, 360)
	if d > 180 {
		d = 360 - d
	}
	return d
}

func TestSystemPaletteDark(t *testing.T) {
	// A tinted (blue-gray) dark theme, e.g. GitHub-dark.
	th := sysTheme(t, "#0d1117", "#c9d1d9")
	if !th.SysTheme || th.LightBgn {
		t.Fatalf("SysTheme=%v LightBgn=%v, want dark system theme", th.SysTheme, th.LightBgn)
	}
	// The input bar is transparent: it rides the terminal's background
	// instead of a painted surface.
	if th.CBarBG != "" {
		t.Fatalf("bar surface %s should be transparent", th.CBarBG)
	}
	themeHue, _, _ := parseHex(t, "#0d1117").Hsl()
	// The raised surfaces stay close to the background...
	card := parseHex(t, th.CCardBG)
	ch, cs, _ := card.Hsl()
	_, _, l := card.Hsl()
	if l < 0.02 || l > 0.25 {
		t.Fatalf("card surface lightness %.3f not between bg and text", l)
	}
	// ...and keep the theme's hue instead of collapsing to gray.
	if cs < 0.02 {
		t.Fatalf("card surface %s unsaturated, theme tint lost", th.CCardBG)
	}
	if hueDist(ch, themeHue) > 25 {
		t.Fatalf("card surface hue %.1f drifts %+.1f from theme hue %.1f", ch, ch-themeHue, themeHue)
	}
	// The hierarchy on the ramp must stay ordered.
	lum := func(c string) float64 { _, _, l := parseHex(t, c).Hsl(); return l }
	if !(lum(th.CModalBG) < lum(th.CCardBG) && lum(th.CCardBG) < lum(th.CBorder) &&
		lum(th.CBorder) < lum(th.CFaint) && lum(th.CFaint) < lum(th.CDim) && lum(th.CDim) < lum(th.CFG)) {
		t.Fatalf("ramp order broken: modal=%.3f card=%.3f border=%.3f faint=%.3f dim=%.3f cfg=%.3f",
			lum(th.CModalBG), lum(th.CCardBG), lum(th.CBorder), lum(th.CFaint), lum(th.CDim), lum(th.CFG))
	}
	// Body text inherits the terminal's own foreground color.
	got := parseHex(t, th.CFG)
	want := parseHex(t, "#c9d1d9")
	if d := func(f, t float64) float64 {
		x := f - t
		if x < 0 {
			return -x
		}
		return x
	}; d(got.R, want.R) > 0.01 || d(got.G, want.G) > 0.01 || d(got.B, want.B) > 0.01 {
		t.Fatalf("CFG %s should be the terminal fg verbatim", th.CFG)
	}
	// The accent keeps its Braun-orange hue.
	ah, _, _ := parseHex(t, th.CAccent).Hsl()
	if ah < 5 || ah > 30 {
		t.Fatalf("accent %s lost its orange hue %.1f", th.CAccent, ah)
	}
}

func TestSystemPaletteLight(t *testing.T) {
	// A tinted light theme (cool paper).
	th := sysTheme(t, "#eef2f8", "#1f2a3a")
	if !th.SysTheme || !th.LightBgn {
		t.Fatalf("SysTheme=%v LightBgn=%v, want light system theme", th.SysTheme, th.LightBgn)
	}
	// The input bar is transparent on the light surface too.
	if th.CBarBG != "" {
		t.Fatalf("bar surface %s should be transparent", th.CBarBG)
	}
	bgHue, _, bgLum := parseHex(t, "#eef2f8").Hsl()
	cardHue, cardSat, cardLum := parseHex(t, th.CCardBG).Hsl()
	// Surfaces are raised plates: darker than the page, tinted like it.
	if cardLum > bgLum-0.01 {
		t.Fatalf("card surface lightness %.3f not below the light background %.3f", cardLum, bgLum)
	}
	if cardSat < 0.02 {
		t.Fatalf("card surface %s unsaturated, theme tint lost", th.CCardBG)
	}
	if hueDist(cardHue, bgHue) > 25 {
		t.Fatalf("card surface hue %.1f drifts %.1f from theme hue %.1f", cardHue, hueDist(cardHue, bgHue), bgHue)
	}
	// Light theme: body text is the terminal fg, dim is between it and bg.
	lum := func(c string) float64 { _, _, l := parseHex(t, c).Hsl(); return l }
	if !(lum(th.CDim) > lum(th.CFG) && lum(th.CDim) < lum("#eef2f8")) {
		t.Fatalf("dim %s outside (cfg %s, bg) bounds", th.CDim, th.CFG)
	}
}

// TestNewThemeFixedFallbackWithoutTTY pins that the live path does not
// query (and cannot hang on) a non-TTY stdout: go test pipes it, so
// NewTheme must land on the built-in palette.
func TestNewThemeFixedFallbackWithoutTTY(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	if th := NewTheme(); th.SysTheme || th.NoColor {
		t.Fatalf("NewTheme under go test: SysTheme=%v NoColor=%v, want fixed fallback", th.SysTheme, th.NoColor)
	}
}

// TestThemeApplySystem pins the live re-apply path: identical reports are
// no-ops, a changed terminal theme re-derives the palette (flipping the
// surface class), and a missing report (the NoColor sentinel) leaves it
// alone.
func TestThemeApplySystem(t *testing.T) {
	th := sysTheme(t, "#0d1117", "#c9d1d9")
	if th.applySystem(termenv.RGBColor("#0d1117"), termenv.RGBColor("#c9d1d9")) {
		t.Fatal("identical report re-applied the palette")
	}
	// The user switches the terminal over to a light theme.
	if !th.applySystem(termenv.RGBColor("#eef2f8"), termenv.RGBColor("#1f2a3a")) {
		t.Fatal("changed terminal theme was not applied")
	}
	if !th.LightBgn {
		t.Fatal("LightBgn not flipped with the light theme")
	}
	// The bar surface stays transparent across the re-apply.
	if th.CBarBG != "" {
		t.Fatalf("bar surface %s should stay transparent after re-apply", th.CBarBG)
	}
	// A missing report (the NoColor sentinel of an unanswered query) must
	// not clobber the current palette.
	cfgBefore := th.CFG
	if th.applySystem(termenv.NoColor{}, nil) {
		t.Fatal("NoColor report should be ignored")
	}
	if th.CFG != cfgBefore {
		t.Fatalf("NoColor report changed CFG %s -> %s", cfgBefore, th.CFG)
	}
}

// TestProbeTerm pins the TERM gate override of the live color probe:
// inside tmux (TMUX set) or screen (STTY set) the multiplexer prefix
// drops off so termenv sends the OSC 10/11 query and the multiplexer's
// own answer (its tracked outer color) is used; everywhere else TERM
// passes through untouched.
func TestProbeTerm(t *testing.T) {
	cases := []struct {
		name, term, tmux, stty, want string
	}{
		{"tmux strips prefix", "tmux-256color", "/tmp/tmux-1000/default,9,1", "", "256color"},
		{"tmux bare", "tmux", "/tmp/tmux-1000/default,9,1", "", ""},
		{"screen strips prefix", "screen-256color", "", "512/513/4", "256color"},
		{"plain terminal untouched", "xterm-256color", "", "", "xterm-256color"},
		{"no multiplexer env", "tmux-256color", "", "", "tmux-256color"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("TERM", c.term)
			t.Setenv("TMUX", c.tmux)
			t.Setenv("STTY", c.stty)
			if got := probeTerm(); got != c.want {
				t.Fatalf("probeTerm() = %q, want %q", got, c.want)
			}
		})
	}
}

// TestApplySystemIgnoresHardFallback pins the anti-flicker guard: when
// the probe's OSC answer is lost and no COLORFGBG exists, termenv returns
// its hard default (ANSI 0/7) — not a report, so the palette stays put.
// With a real COLORFGBG (some desktop environments push one into tmux on
// every theme switch) the same ANSI colors are a meaningful fallback and apply.
func TestApplySystemIgnoresHardFallback(t *testing.T) {
	th := sysTheme(t, "#0d1117", "#c9d1d9")
	t.Setenv("COLORFGBG", "")
	cfgBefore := th.CFG
	if th.applySystem(termenv.ANSIColor(0), termenv.ANSIColor(7)) {
		t.Fatal("termenv's hard fallback applied as a terminal report")
	}
	if th.CFG != cfgBefore {
		t.Fatalf("hard fallback moved CFG %s -> %s", cfgBefore, th.CFG)
	}
	// A real COLORFGBG makes the ANSI fallback a genuine report.
	t.Setenv("COLORFGBG", "0;15")
	if !th.applySystem(termenv.ANSIColor(15), termenv.ANSIColor(0)) {
		t.Fatal("COLORFGBG-sourced report not applied")
	}
	if th.CFG == cfgBefore {
		t.Fatal("COLORFGBG report did not move the palette")
	}
}

// TestThemeProbeFollowsThemeSwitch drives the model side of the live re-
// query: a changed report re-applies the palette and drops the render
// caches; an identical report churns nothing.
func TestThemeProbeFollowsThemeSwitch(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	a := app.New("http://127.0.0.1:3999")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a.Start(ctx)
	m := NewModel(a)
	// go test's stdout is not a TTY: termenv reports Ascii, pin the
	// profile the way a live truecolor terminal would present it.
	m.th.Profile = termenv.TrueColor

	m.mdFor(80, "")
	if len(m.mds) == 0 {
		t.Fatal("expected a cached markdown pipeline")
	}
	// The probe is armed by the tick handler; the report then rides the
	// key reader as the alt intro + payload + terminator stream (see
	// feedOSCReport).
	m.beginThemeProbe()
	feedOSCReport(t, m, "11", "rgb:0d0d/1111/1717")
	if !m.themeProbing {
		t.Fatal("probe closed after the background report alone")
	}
	feedOSCReport(t, m, "10", "rgb:c9c9/d1d1/d9d9")
	if m.themeProbing {
		t.Fatal("probe not closed after the full report")
	}
	if !m.th.SysTheme {
		t.Fatal("theme report did not apply the system palette")
	}
	if m.th.LightBgn {
		t.Fatal("dark theme applied as a light surface")
	}
	if len(m.mds) != 0 {
		t.Fatal("markdown pipeline cache survived the theme change")
	}
	if !m.transDirty {
		t.Fatal("transcript cache not marked dirty after the theme change")
	}
	// An identical report must be a no-op: no re-apply, no cache churn.
	m.transDirty = false
	m.mdFor(80, "")
	m.beginThemeProbe()
	feedOSCReport(t, m, "11", "rgb:0d0d/1111/1717")
	feedOSCReport(t, m, "10", "rgb:c9c9/d1d1/d9d9")
	if m.transDirty {
		t.Fatal("identical theme report churned the render caches")
	}
	if len(m.mds) == 0 {
		t.Fatal("identical theme report evicted the markdown pipeline cache")
	}
}

// TestThemeProbeTickSchedulesRequery pins the probe pacing: on the
// themeProbeEvery-th tick the query worker is scheduled (and only then);
// its report rides the key reader, so an answerless probe closes at its
// deadline with the current palette intact.
func TestThemeProbeTickSchedulesRequery(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	a := app.New("http://127.0.0.1:3999")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a.Start(ctx)
	m := NewModel(a)
	m.th.Profile = termenv.TrueColor // go test's stdout is not a TTY

	// Ticks below the threshold must not start a probe.
	for i := 0; i < themeProbeEvery-1; i++ {
		m.Update(tickMsg{t: time.Now()})
	}
	if m.themeProbing {
		t.Fatal("probe started before themeProbeEvery ticks")
	}

	_, cmd := m.Update(tickMsg{t: time.Now()})
	if !m.themeProbing {
		t.Fatal("probe not armed on the threshold tick")
	}
	if m.themeProbe != 0 {
		t.Fatalf("probe counter not reset, got %d", m.themeProbe)
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("tick returned %T, want tea.BatchMsg", batch)
	}
	// Dispatch the batch the way the program would: run each worker and
	// feed its message back through Update. The query worker only writes
	// the OSC report (its answer rides the key reader), so the probe
	// stays open — no terminal answers it in this non-TTY test.
	for _, c := range batch {
		if msg := c(); msg != nil {
			m.Update(msg)
		}
	}
	if !m.themeProbing {
		t.Fatal("probe closed before its answer (or deadline)")
	}
	// Past the deadline the answerless probe closes with nothing.
	m.Update(tickMsg{t: time.Now().Add(oscProbeTimeout + time.Second)})
	if m.themeProbing {
		t.Fatal("probe still open past its deadline")
	}
	if m.th.SysTheme {
		t.Fatal("answerless probe should not switch the palette")
	}
}

// TestThemeProbeRidesKeyReader pins the live probe's tty discipline: the
// query is a bare OSC write — the program keeps the terminal (no
// ReleaseTerminal/RestoreTerminal blip) — and the answer lands as key
// events through handleKey, applying the palette with no mouse re-arm (
// the mouse never left during the exchange).
func TestThemeProbeRidesKeyReader(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	a := app.New("http://127.0.0.1:3999")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a.Start(ctx)
	m := NewModel(a)
	m.th.Profile = termenv.TrueColor // go test's stdout is not a TTY
	prog := tea.NewProgram(m,
		tea.WithoutRenderer(), // headless
		tea.WithContext(ctx),
		tea.WithInput(strings.NewReader("")))
	m.SetProg(prog)

	m.beginThemeProbe() // as armed by the tick handler
	if msg := m.sendThemeQuery()(); msg != nil {
		t.Fatalf("query worker returned %T, want a bare report over the key reader", msg)
	}
	if !m.themeProbing {
		t.Fatal("probe not open after the query write")
	}
	// The report rides the key reader as the alt intro + payload +
	// terminator stream. The background alone keeps the probe open.
	probeKeys := func(code, payload string) tea.Cmd {
		var last tea.Cmd
		var mo tea.Model
		mo, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Alt: true, Runes: []rune("]")})
		m = mo.(*Model)
		mo, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(code + ";" + payload)})
		m = mo.(*Model)
		mo, last = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Alt: true, Runes: []rune("\\")})
		m = mo.(*Model)
		return last
	}
	if cmd := probeKeys("11", "rgb:0d0d/1111/1717"); cmd != nil {
		t.Fatalf("bg-only report returned cmd %v, want none (no terminal handoff)", cmd)
	}
	if !m.themeProbing {
		t.Fatal("probe must stay open until the foreground report lands too")
	}
	if cmd := probeKeys("10", "rgb:c9c9/d1d1/d9d9"); cmd != nil {
		t.Fatalf("full report returned cmd %v, want none (mouse never left)", cmd)
	}
	if m.themeProbing {
		t.Fatal("probe not closed after the full report")
	}
	if !m.th.SysTheme {
		t.Fatal("report did not apply the system palette")
	}
	if m.th.CFG == "" {
		t.Fatal("system palette left body text unset")
	}
}

// feedOSCReport delivers a status report the way the key reader actually
// does: the Alt-"]" intro, the plain payload run, and the ST terminator
// (the Alt-\\ tail).
func feedOSCReport(t *testing.T, m *Model, code, payload string) {
	t.Helper()
	var mo tea.Model
	mo, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Alt: true, Runes: []rune("]")})
	m = mo.(*Model)
	mo, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(code + ";" + payload)})
	m = mo.(*Model)
	mo, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Alt: true, Runes: []rune("\\")})
	_ = mo
}

// TestThemeProbeCollector pins the report reassembly: the answer's event
// stream is captured end to end (ST and BEL terminators alike), a
// truncated payload keeps assembling, and an answerless probe never eats
// real typing.
func TestThemeProbeCollector(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	a := app.New("http://127.0.0.1:3999")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a.Start(ctx)
	m := NewModel(a)
	m.th.Profile = termenv.TrueColor // go test's stdout is not a TTY

	m.beginThemeProbe()

	// No report yet: real typing must pass through, untouched.
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("abc")})
	if got := m.inp.value(); got != "abc" {
		t.Fatalf("an answerless probe ate typing, input now %q", got)
	}
	if m.oscBuf != "" {
		t.Fatalf("collector assembled %q without a report intro", m.oscBuf)
	}

	// A background report with a BEL terminator (Ctrl-g tail).
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Alt: true, Runes: []rune("]")})
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("11;rgb:0d0d/1111/1717")})
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlG})
	if themeColorNil(m.oscBG) {
		t.Fatal("BEL-terminated background report not captured")
	}
	if !m.themeProbing {
		t.Fatal("probe closed after the background report alone")
	}

	// A foreground report whose ST terminator crossed a read boundary: the
	// terminal's ESC arrived alone, so the reader emits it as a plain
	// Escape key; the backslash lands in the next read.
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Alt: true, Runes: []rune("]")})
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("10;rgb:c9c9/d1d1/d9d9")})
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if themeColorNil(m.oscFG) {
		t.Fatal("foreground report not captured before its split terminator")
	}
	if got := m.inp.value(); got != "abc" {
		t.Fatalf("the stray Escape leaked into the input: %q", got)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("\\")})
	if m.themeProbing {
		t.Fatal("probe not closed after the split terminator")
	}

	// A truncated payload keeps assembling until it completes (fresh
	// probe).
	m.beginThemeProbe()
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Alt: true, Runes: []rune("]")})
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("10;rgb:c9c9")})
	if themeColorNil(m.oscFG) == false || m.themeProbing != true {
		t.Fatal("truncated foreground payload must keep the probe open")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/d1d1/d9d9")})
	if !m.themeProbing || themeColorNil(m.oscFG) {
		t.Fatal("complete report but probe state wrong (want open, fg set)")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Alt: true, Runes: []rune("\\")})
	// A full background report with its terminator completes the stream:
	// the probe closes only once the terminators are consumed.
	feedOSCReport(t, m, "11", "rgb:0d0d/1111/1717")
	if m.themeProbing {
		t.Fatal("probe not closed once the report stream completed")
	}
	if !m.th.SysTheme || m.th.CFG == "" {
		t.Fatal("reassembled report did not apply the system palette")
	}
	if got := m.inp.value(); got != "abc" {
		t.Fatalf("report traffic leaked into the input: %q", got)
	}
}

// TestRailWindStreak pins the title rail's wind-streak look: the cell
// alphabet is ASCII only ('+', '-', '.', '`' and the gaps they open),
// the left edge starts on a dense knot/run, the right edge dissolves
// into sparse dots and gaps, and density trends down left→right — the
// gust. The exact shape is pinned at n=8/16, and the non-hex fallback
// stays identical.
func TestRailWindStreak(t *testing.T) {
	th := NewTheme()
	allowed := map[rune]bool{' ': true, '+': true, '-': true, '.': true, '`': true}
	for _, n := range []int{1, 2, 4, 8, 16, 32, 50, 66} {
		got := []rune(stripANSI(th.gradRail("#ff8800", "#222222", n)))
		if len(got) != n {
			t.Fatalf("rail(%d): %d visible cells, want %d", n, len(got), n)
		}
		for i, r := range got {
			if !allowed[r] {
				t.Fatalf("rail(%d): cell %d is %q, want one of + - . ` (gap): %q",
					n, i, r, string(got))
			}
		}
		solid := func(rs []rune) int {
			k := 0
			for _, r := range rs {
				if r == '+' || r == '-' {
					k++
				}
			}
			return k
		}
		nonSpace := func(rs []rune) int {
			k := 0
			for _, r := range rs {
				if r != ' ' {
					k++
				}
			}
			return k
		}
		h := n / 3
		if h < 1 {
			h = 1
		}
		left, right := got[:h], got[n-h:]
		// The gust: the left head carries more solid dash than the tail
		// (and at least as much ink of any kind).
		if solid(left) < solid(right) {
			t.Fatalf("rail(%d): left head has %d solid cells but the tail has %d — the streak must blow left→right: %q",
				n, solid(left), solid(right), string(got))
		}
		if nonSpace(left) < nonSpace(right) {
			t.Fatalf("rail(%d): left head ink %d < tail ink %d: %q", n, nonSpace(left), nonSpace(right), string(got))
		}
		if n > 1 {
			if got[0] != '+' {
				t.Fatalf("rail(%d): first cell %q, want the '+' knot (dense start)", n, got[0])
			}
			if last := got[n-1]; last != ' ' && last != '.' {
				t.Fatalf("rail(%d): last cell %q, want a sparse end (gap or dot)", n, last)
			}
		}
	}
	// The exact streak shapes at the small widths.
	if got := stripANSI(th.gradRail("#ff8800", "#222222", 8)); got != "+---`.  " {
		t.Fatalf("rail(8) = %q, want %q", got, "+---`.  ")
	}
	if got := stripANSI(th.gradRail("#ff8800", "#222222", 16)); got != "+---.+-.-`.     " {
		t.Fatalf("rail(16) = %q, want %q", got, "+---.+-.-`.     ")
	}
	// The non-hex fallback keeps the identical streak (flat style).
	fb := stripANSI(th.gradRail("default", "default", 16))
	if fb != "+---.+-.-`.     " {
		t.Fatalf("fallback rail(16) = %q, want the same wind streak", fb)
	}
}

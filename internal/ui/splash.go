// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/muesli/termenv"
)

// Boot splash: the "DSH-CLI" wordmark — thin double-line box-drawing
// letterforms set wide-tracked — lights up left-to-right over
// splashReveal. A soft band of light sweeps through the columns: each
// stroke burns up from the row's resting shade and cools back down as the
// band passes. The plate (DSH) rests on the theme's top→bottom warm
// ramp, the suffix (-CLI) on the orange accent ramp, the hyphen stays one
// quiet hairline below both. The letter-spaced caps tagline dissolves in
// from the first frame — in sync with the glyphs, which is what ties the
// caption to the plate — while a hairline rail draws out over the full
// splashDuration, and the interface takes over. Any key dismisses early.
const (
	splashDuration = 2 * time.Second
	splashReveal   = 900 * time.Millisecond
	splashHint     = "any key to continue"
	splashTagline  = "terminal client for DeepSeek Harness"

	// splashTracking is the inter-letter air of the wordmark: the extra
	// tracking is what keeps the lockup reading as a brand plate instead
	// of a banner.
	splashTracking = 2

	// splashEdge is the width of the reveal band in wordmark columns: the
	// sweep is a soft light wipe, not a hard cut, so consecutive frames
	// overlap and the head glides instead of stepping.
	splashEdge = 4

	// The comet-head brightness boost of the sweep band, and the tagline
	// dissolve (starts in sync with the sweep at t=0, alpha 0→1).
	splashGlowAmp = 0.6
	splashTagFade = 240 * time.Millisecond
)

type splashGlyph struct {
	name string
	rows [8]string
}

// splashGlyphs is the DSH-CLI letter set on a uniform eight-row face in a
// thin-line type: double-line box drawing (one-cell corners, bars and
// stems) with rounded terminals, so the wordmark reads as one consistent
// light stroke with no oversized or undersized glyphs and no closed
// counters that blur at terminal resolution. D is a flat left wall with a
// domed bowl, S and C carry their hooks as dangling corner terminals, H
// holds a single hairline crossbar on the face centerline (the same row
// as the hyphen), L plants a two-cell base, and I takes serifs. The
// hyphen is a thin single-line rule six cells long — deliberately lighter
// than the letter strokes, so the "─CLI" seam reads as a division rather
// than a weight change. Glyphs are stored trimmed; the compositor
// right-pads each row to the glyph's own width.
var splashGlyphs = []splashGlyph{
	{
		name: "D",
		rows: [8]string{
			"╔══╗",
			"║   ║",
			"║    ║",
			"║    ║",
			"║    ║",
			"║    ║",
			"║   ║",
			"╚══╝",
		},
	},
	{
		name: "S",
		rows: [8]string{
			" ╔══╗",
			" ║",
			" ║",
			" ╚══╗",
			"    ║",
			"    ║",
			"    ║",
			" ╚══╝",
		},
	},
	{
		name: "H",
		rows: [8]string{
			"║   ║",
			"║   ║",
			"║   ║",
			"╢───╟",
			"║   ║",
			"║   ║",
			"║   ║",
			"║   ║",
		},
	},
	{
		name: "-",
		rows: [8]string{"", "", "", "──────", "", "", "", ""},
	},
	{
		name: "C",
		rows: [8]string{
			"╔══╗",
			"║  ║",
			"║  ║",
			"║",
			"║",
			"║  ║",
			"║  ║",
			"╚══╝",
		},
	},
	{
		name: "L",
		rows: [8]string{"║", "║", "║", "║", "║", "║", "║", "╚══"},
	},
	{
		name: "I",
		rows: [8]string{
			"╔═╗",
			" ║ ",
			" ║ ",
			" ║ ",
			" ║ ",
			" ║ ",
			" ║ ",
			"╚═╝",
		},
	},
}

// joinGlyphs right-pads every glyph row to the glyph's own width and joins
// the glyphs with splashTracking spaces, so no letterform needs manual
// alignment.
func joinGlyphs(glyphs ...splashGlyph) []string {
	rows := make([]string, 8)
	for i, g := range glyphs {
		if i > 0 {
			for r := range rows {
				rows[r] += strings.Repeat(" ", splashTracking)
			}
		}
		w := 0
		for _, row := range g.rows {
			if l := len([]rune(row)); l > w {
				w = l
			}
		}
		for r := range rows {
			rs := []rune(g.rows[r])
			rows[r] += string(rs) + strings.Repeat(" ", w-len(rs))
		}
	}
	return rows
}

// composeSplashArt lays the glyph blocks out row by row into the finished
// wordmark. accent is the first accented column (the "─CLI" seam) and sepW
// the hyphen width at that seam; width is the uniform visible width of
// every row.
func composeSplashArt() (rows []string, accent, sepW, width int) {
	cols := 0
	for i, g := range splashGlyphs {
		if i > 0 {
			cols += splashTracking
		}
		if g.name == "-" {
			accent = cols
		}
		w := 0
		for _, row := range g.rows {
			if l := len([]rune(row)); l > w {
				w = l
			}
		}
		if g.name == "-" {
			sepW = w
		}
		cols += w
	}
	width = cols
	rows = joinGlyphs(splashGlyphs...)
	return rows, accent, sepW, width
}

var (
	splashArt    []string
	splashAccent int
	splashSepW   int
	splashWidth  int
)

func init() {
	splashArt, splashAccent, splashSepW, splashWidth = composeSplashArt()
}

// easeOutCubic accelerates the sweep out of the gate and lets it settle
// at the right edge — the wordmark lands with momentum instead of on a
// metronome.
func easeOutCubic(x float64) float64 {
	q := 1 - x
	return 1 - q*q*q
}

// splashTaglineSpaced is the English tagline set in upper case with a
// double inter-word space: a wide, quiet baseline under the wordmark
// (the boot splash's tests pin against it).
func splashTaglineSpaced() string {
	return strings.ToUpper(strings.ReplaceAll(splashTagline, " ", "  "))
}

// splashTagline renders the tagline in the active language, the same
// upper-case double-spaced treatment (a no-op for non-alphabetic text).
func (m *Model) splashTagline() string {
	return strings.ToUpper(strings.ReplaceAll(m.loc.T("splash.tagline"), " ", "  "))
}

// mixRGB linearly blends two 24-bit colors (t 0→1).
func mixRGB(a, b rgbTriple, t float64) rgbTriple {
	return rgbTriple{
		r: uint8(float64(a.r) + (float64(b.r)-float64(a.r))*t),
		g: uint8(float64(a.g) + (float64(b.g)-float64(a.g))*t),
		b: uint8(float64(a.b) + (float64(b.b)-float64(a.b))*t),
	}
}

// splashSGR is a raw 24-bit foreground sequence (the splash predates the
// theme layer's profile conversion: it only runs on true-color profiles,
// see splashView).
func splashSGR(c rgbTriple) string {
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", c.r, c.g, c.b)
}

// scaleSGR is a raw 24-bit foreground sequence for color c multiplied by a
// brightness factor f, clipped at white (f 1 = unchanged).
func scaleSGR(c rgbTriple, f float64) string {
	clip := func(v float64) uint8 {
		if v > 255 {
			return 255
		}
		if v < 0 {
			return 0
		}
		return uint8(v)
	}
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm",
		clip(float64(c.r)*f), clip(float64(c.g)*f), clip(float64(c.b)*f))
}

// splashRowHex renders one wordmark row of the eased sweep at 24-bit:
// every column inside the reveal band lights from its resting shade
// (rf is the row's 0→1 position in the per-zone top→bottom ramp) while
// the band's core burns brighter and cools behind it; the cursor block
// rides the sweep front on the face centerline (mid).
func splashRowHex(rs []rune, sweepX, rf float64, accent, sepW int,
	dshTop, dshBot, cliTop, cliBot, sep rgbTriple, mid, cursor int) string {
	var b strings.Builder
	for c := 0; c < len(rs); c++ {
		if c == cursor {
			b.WriteString(scaleSGR(cliTop, 1.2))
			b.WriteString("▌")
			continue
		}
		ch := rs[c]
		if ch == ' ' {
			b.WriteByte(' ')
			continue
		}
		f := (sweepX - float64(c)) / float64(splashEdge)
		if f <= 0 {
			b.WriteByte(' ')
			continue
		}
		if f > 1 {
			f = 1
		}
		var base rgbTriple
		switch {
		case c < accent:
			base = mixRGB(dshTop, dshBot, rf)
		case c < accent+sepW:
			base = sep
		default:
			base = mixRGB(cliTop, cliBot, rf)
		}
		d := float64(c) - (sweepX - float64(splashEdge)/2)
		b.WriteString(scaleSGR(base, f*(1+splashGlowAmp*math.Exp(-d*d/2))))
		b.WriteRune(ch)
	}
	b.WriteString("\x1b[0m")
	return b.String()
}

// splashActive reports whether the boot animation is still on screen.
func (m *Model) splashActive() bool { return m.splashStart != (time.Time{}) }

// splashView renders the boot animation at absolute time t since the
// splash started: the wordmark lights column-by-column under the sweep
// band (a cursor block rides the front), the tagline fades in after the
// sweep lands, and the rail under it draws out over the full duration.
func (m *Model) splashView() string {
	th := m.th
	now := m.now
	if now.IsZero() || now.Before(m.splashStart) {
		now = m.splashStart
	}
	t := now.Sub(m.splashStart)
	if t < 0 {
		t = 0
	}

	art, artW, accent := splashArt, splashWidth, splashAccent
	sepW := splashSepW
	if m.W < artW+4 {
		// Too narrow for the wordmark: a plain one-line fallback.
		art, artW, accent, sepW = []string{"DSH-CLI"}, 7, 3, 1
	}
	artH := len(art)

	// Eased sweep position in wordmark columns.
	frac := float64(t) / float64(splashReveal)
	if frac > 1 {
		frac = 1
	}
	sweepX := easeOutCubic(frac) * float64(artW)

	// The cursor block rides the sweep front on the face centerline
	// while the sweep is live.
	cursor := -1
	if sweepX >= 1 && sweepX < float64(artW) {
		cursor = int(math.Ceil(sweepX))
		if cursor > artW {
			cursor = artW
		}
	}

	// Resting shade per row and zone: the plate (DSH) settles top→bottom
	// into the theme text ramp, the suffix (-CLI) into the accent ramp,
	// the hyphen stays one quiet line below both. When the palette cannot
	// carry a 24-bit ramp (ANSI / no-color) the flat theme styles take
	// over and the reveal is a hard wipe with the cursor block.
	var dshTop, dshBot, cliTop, cliBot, sepRGB rgbTriple
	hexOK := !th.NoColor && th.Profile == termenv.TrueColor
	if hexOK {
		var ok1, ok2, ok3, ok4, ok5 bool
		dshTop, ok1 = parseHexRGB(th.CFG)
		dshBot, ok2 = parseHexRGB(th.CDim)
		cliTop, ok3 = parseHexRGB(th.CUser)
		cliBot, ok4 = parseHexRGB(th.CAccent)
		sepRGB, ok5 = parseHexRGB(th.CFaint)
		hexOK = ok1 && ok2 && ok3 && ok4 && ok5
	}
	rowShade := func(r int) float64 {
		if artH <= 1 {
			return 0
		}
		return float64(r) / float64(artH-1)
	}

	// Block layout: art, gap, tagline, gap, rail, hint — the extra
	// breathing row under the art keeps the plate floating above the
	// caption block.
	const chrome = 6
	contentH := artH + chrome
	oy := (m.H - contentH) / 2
	if oy < 0 {
		oy = 0
	}
	ox := (m.W - artW) / 2
	if ox < 0 {
		ox = 0
	}
	gap := strings.Repeat(" ", ox)
	mid := artH / 2

	out := make([]string, m.H)
	put := func(dy int, line string) {
		if y := oy + dy; y >= 0 && y < len(out) {
			out[y] = line
		}
	}

	for i, row := range art {
		rs := []rune(row)
		if hexOK {
			put(i, gap+splashRowHex(rs, sweepX, rowShade(i), accent, sepW,
				dshTop, dshBot, cliTop, cliBot, sepRGB, mid, cursor))
			continue
		}
		// Flat fallback: a hard left-to-right wipe, zone styles from the
		// theme, cursor block on the mid row.
		cols := int(sweepX)
		if cols > len(rs) {
			cols = len(rs)
		}
		var sb strings.Builder
		a := accent
		if a > cols {
			a = cols
		}
		if a > 0 {
			sb.WriteString(th.Plain().Render(string(rs[:a])))
		}
		bEnd := accent + sepW
		if bEnd > cols {
			bEnd = cols
		}
		if bEnd > a {
			sb.WriteString(th.Faint().Render(string(rs[a:bEnd])))
		}
		if cols > bEnd {
			sb.WriteString(th.Accent().Render(string(rs[bEnd:cols])))
		}
		if i == mid && cursor >= 0 && cursor < len(rs) && cursor >= cols {
			sb.WriteString(strings.Repeat(" ", cursor-cols))
			sb.WriteString(th.Accent().Render("▌"))
			if trailing := len(rs) - cursor - 1; trailing > 0 {
				sb.WriteString(strings.Repeat(" ", trailing))
			}
		} else if cols < len(rs) {
			sb.WriteString(strings.Repeat(" ", len(rs)-cols))
		}
		put(i, gap+sb.String())
	}

	put(artH+1, "")
	// Tagline: a uniform 0→1 dissolve into the faint line, in sync with
	// the wordmark sweep — the caption starts breathing at the same
	// instant the first glyph lights up. (The flat path cannot fade, so
	// the tagline simply rides the frame from the start.)
	tag := m.splashTagline()
	tagLine := ""
	if hexOK {
		p := float64(t) / float64(splashTagFade)
		if p > 1 {
			p = 1
		}
		if p > 0 {
			tagLine = scaleSGR(sepRGB, p) + tag + "\x1b[0m"
		}
	} else {
		tagLine = th.Faint().Render(tag)
	}
	if tagLine != "" {
		dy := (artW - plainWidth(tag)) / 2
		if dy < 0 {
			dy = 0
		}
		put(artH+2, gap+strings.Repeat(" ", dy)+tagLine)
	}

	put(artH+3, "")
	// Rail: the accent fill draws out over the full duration, the rest
	// stays a quiet hairline.
	frac = float64(t) / float64(splashDuration)
	if frac > 1 {
		frac = 1
	}
	fill := int(frac * float64(artW))
	rail := ""
	if hexOK {
		var b strings.Builder
		last := float64(maxInt(1, artW-1))
		for i := 0; i < fill; i++ {
			b.WriteString(splashSGR(mixRGB(cliTop, cliBot, float64(i)/last)))
			b.WriteString("─")
		}
		if rest := artW - fill; rest > 0 {
			b.WriteString(splashSGR(sepRGB))
			b.WriteString(strings.Repeat("─", rest))
		}
		b.WriteString("\x1b[0m")
		rail = b.String()
	} else {
		rail = th.Accent().Render(strings.Repeat("─", fill)) +
			th.Faint().Render(strings.Repeat("─", artW-fill))
	}
	put(artH+4, gap+rail)
	hint := th.Faint().Render(m.loc.T("splash.hint"))
	dy := (artW - plainWidth(hint)) / 2
	if dy < 0 {
		dy = 0
	}
	put(artH+5, gap+strings.Repeat(" ", dy)+hint)
	return strings.Join(out, "\n")
}

package ui

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"dsh-cli/internal/app"
	"dsh-cli/internal/core"
	"dsh-cli/internal/protocol"
	"dsh-cli/internal/version"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

var nameVer = "dsh-cli " + version.Version

// TestTopBarNameVersionBeforeState pins the right-edge readout order of the
// top bar: the client name and version lead, and the running-state icon /
// text follows them (never before).
func TestTopBarNameVersionBeforeState(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a := app.New("http://127.0.0.1:3080")
	a.Start(ctx)
	m := NewModel(a)
	m.splashOff = true
	m.W, m.H = 120, 30

	plain := stripANSI(m.topBar(m.W))
	i := strings.Index(plain, nameVer)
	if i < 0 {
		t.Fatalf("topBar missing name+version %q: %q", nameVer, plain)
	}
	rest := strings.TrimSpace(plain[i+len(nameVer):])
	if rest == "" {
		t.Fatalf("no running-state readout after the name/version: %q", plain)
	}

	// Width contract: the bar must never run past the window edge, or the
	// renderer clips the whole right block off-screen.
	for _, w := range []int{30, 40, 80, 100, 120} {
		if pw := plainWidth(m.topBar(w)); pw > w {
			t.Fatalf("topBar(%d) = %d cells wide, want <= %d", w, pw, w)
		}
	}
}

// TestTopBarAlwaysFirstLine pins the frame budget: whatever the transient
// layers hold (toast shelf, slash menu), the rendered frame must fit the
// window height exactly — an overflowing frame is clipped from the top by
// the alt-screen renderer, which eats the persistent top bar.
func TestTopBarAlwaysFirstLine(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a := app.New("http://127.0.0.1:3080")
	a.Start(ctx)
	m := NewModel(a)
	m.splashOff = true

	check := func(label string, h int) {
		m.W, m.H = 100, h
		lines := strings.Split(m.View(), "\n")
		if len(lines) != h {
			t.Fatalf("%s: frame %d lines, want %d", label, len(lines), h)
		}
		if !strings.Contains(stripANSI(lines[0]), nameVer) {
			t.Fatalf("%s: top bar not first line: %q", label, stripANSI(lines[0]))
		}
		if !strings.Contains(stripANSI(lines[0]), "(no session)") {
			t.Fatalf("%s: top bar missing session title: %q", label, stripANSI(lines[0]))
		}
	}

	check("no layers", 10)
	m.addToast(core.Notice{Level: "info", Text: "reconnected"})
	check("one toast", 10)
	m.addToast(core.Notice{Level: "warn", Text: "turn finished in 3s"})
	check("two toasts", 10)
	check("two toasts, short window", 8)

	// Full slash menu plus toasts in a short window: the menu is the first
	// transient layer sacrificed, the top bar still owns line one.
	m.mergedCmds = localCommands // no host in the test: the local set suffices
	for _, r := range "/" {
		m.inp.insertRune(r)
	}
	m.inp.refreshMenu(m.mergedCmds)
	if !m.inp.menuOpen {
		t.Fatal("slash menu did not open")
	}
	check("slash menu + toasts, short window", 8)
}

// TestMiniViewKeepsTopBar pins the small-window fallback: below the full
// deck's minimum size the top bar must still render on the first line.
func TestMiniViewKeepsTopBar(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a := app.New("http://127.0.0.1:3080")
	a.Start(ctx)
	m := NewModel(a)
	m.splashOff = true

	for _, size := range [][2]int{{40, 5}, {40, 4}, {10, 3}, {10, 2}} {
		m.W, m.H = size[0], size[1]
		lines := strings.Split(m.View(), "\n")
		if len(lines) > m.H {
			t.Fatalf("W=%d H=%d: frame %d lines, want <= %d", m.W, m.H, len(lines), m.H)
		}
		if !strings.Contains(stripANSI(lines[0]), nameVer) {
			t.Fatalf("W=%d H=%d: top bar not first line: %q", m.W, m.H, stripANSI(lines[0]))
		}
	}
}

// TestTopBarStateSlotOmitsCtx pins the top-right state slot: the compact
// "ctx N%" occupancy no longer rides there (it lives on the status strip
// as the context bar) — idle shows only the idle marker, and the running
// slot shows the rotating verb with its sweep plus the elapsed seconds.
func TestTopBarStateSlotOmitsCtx(t *testing.T) {
	a := app.New("http://127.0.0.1:3999")
	// Start (and its downlink pump) is intentionally skipped: a dead
	// host's probe would race the test and drop the connection
	// mid-render. The store is reachable without it.
	t.Setenv("DSH_CLI_HOME", t.TempDir()) // no inherited startup language
	m := NewModel(a)
	m.splashOff = true
	m.W, m.H = 120, 30
	m.st.SetActive("s1")
	m.st.SetConnected(true) // no live host in the test: bypass the banner branch

	e := func(seq int64, typ string, data string) {
		m.st.Event("s1", &protocol.SessionEvent{Type: typ, Seq: seq, Time: time.Now().UnixMilli(), Data: []byte(data)})
	}
	// No window advertised, no usage yet: the readout stays off.
	e(1, "user/message", `{"role":"user","content":[{"type":"text","text":"hi"}]}`)
	if got := stripANSI(m.topBar(m.W)); strings.Contains(got, "ctx ") {
		t.Fatalf("ctx readout before any data: %q", got)
	}
	// Assistant step with usage — still off until the window is known.
	e(2, "assistant/message", `{"turn":1,"step":1,"message":{"role":"assistant","content":[{"type":"text","text":"hello"}]},"usage":{"inputTokens":230,"outputTokens":10}}`)
	if got := stripANSI(m.topBar(m.W)); strings.Contains(got, "ctx ") {
		t.Fatalf("ctx readout without an advertised window: %q", got)
	}
	// The known window (1000 cells) would have woken the old readout
	// (230 in + 10 out = 24%); the state slot must stay clean.
	e(3, "request/context", `{"provider":"acme","model":"acme-x","contextWindow":1000}`)
	if got := stripANSI(m.topBar(m.W)); strings.Contains(got, "ctx ") {
		t.Fatalf("ctx readout must have left the top bar: %q", got)
	}
	// While the turn runs: verb + elapsed, still no occupancy readout.
	e(4, "turn/start", `{}`)
	bar := stripANSI(m.topBar(m.W))
	if strings.Contains(bar, "ctx ") {
		t.Fatalf("running state slot must not show the ctx readout: %q", bar)
	}
	if !regexp.MustCompile(`\d+s`).MatchString(bar) {
		t.Fatalf("running readout missing the elapsed seconds: %q", bar)
	}
}

// TestTopBarVerbSweep pins the 扫光 animation on the running-state verb:
// the plain text and width never change, the brightness band enters from
// the left (nothing bright on the first frames), reaches the head of the
// verb after two ticks, and keeps advancing one cell per tick for the
// whole pass.
func TestTopBarVerbSweep(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	t.Setenv("NO_COLOR", "") // the dev shell exports NO_COLOR: it would flatten the tiers
	a := app.New("http://127.0.0.1:3999")
	a.Start(ctx)
	m := NewModel(a)
	m.splashOff = true
	m.th.Profile = termenv.TrueColor            // make the tiers distinguishable
	lipgloss.SetColorProfile(termenv.TrueColor) // go test's stdout is not a TTY: Ascii would flatten the tiers

	verb := "thinking…"
	const span = 9 + 2*2  // len(verb) + 2*sweepBand
	t0 := time.Unix(0, 0) // nanosecond zero: tick phase 0
	at := func(tick int) string {
		m.now = t0.Add(time.Duration(tick) * 100 * time.Millisecond)
		return m.sweepText(verb)
	}
	_, _, hot := m.th.sweepShades() // brightness tiers of the accent hue
	if strings.Contains(at(0), hot.Render("t")) {
		t.Fatal("the sweep must enter from the left: nothing bright at tick 0")
	}
	if got := at(2); !strings.Contains(got, hot.Render(verb[:1])) {
		t.Fatalf("tick 2 must put the bright band on the first rune: %q", got)
	}
	first := at(0)
	moved := 0
	for tick := 1; tick < span; tick++ {
		cur := at(tick)
		if plainWidth(cur) != plainWidth(first) {
			t.Fatalf("tick %d: sweep changed the visible width", tick)
		}
		if cur != first {
			moved++
		}
	}
	if moved < span-2 {
		t.Fatalf("sweep advanced on %d/%d ticks, want a continuous pass", moved, span-1)
	}
}

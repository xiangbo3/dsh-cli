// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"dsh-cli/internal/app"
	"dsh-cli/internal/protocol"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// splashArtRows037 pins the three signature rows of the composed wordmark:
// the top line (D's dome, S's top hook, the C/L/I serifs), the centerline
// (S's middle bar, the H crossbar, the six-cell hyphen at the accent seam)
// and the base line (D's floor, the S hooks, the L bar, the I serif).
var splashArtRows037 = [3]string{
	"╔══╗     ╔══╗  ║   ║          ╔══╗  ║    ╔═╗",
	"║    ║   ╚══╗  ╢───╟  ──────  ║     ║     ║ ",
	"╚══╝     ╚══╝  ║   ║          ╚══╝  ╚══  ╚═╝",
}

// TestSplashArtShape pins the composed wordmark: eight uniform rows of the
// same visible width, the accent at the "─CLI" seam, the six-cell hyphen on
// the face centerline, and the three signature letterform rows.
func TestSplashArtShape(t *testing.T) {
	if len(splashArt) != 8 {
		t.Fatalf("wordmark rows = %d (want 8)", len(splashArt))
	}
	var width int
	for i, row := range splashArt {
		n := len([]rune(row))
		if i == 0 {
			width = n
			continue
		}
		if n != width {
			t.Fatalf("row %d visible width %d != row 0 width %d", i, n, width)
		}
	}
	if width != splashWidth {
		t.Fatalf("splashWidth = %d (want %d)", splashWidth, width)
	}
	if splashAccent <= 0 || splashAccent >= width {
		t.Fatalf("accent column %d outside wordmark of width %d", splashAccent, width)
	}
	if splashSepW != 6 {
		t.Fatalf("hyphen width = %d (want 6)", splashSepW)
	}
	// The six-cell hyphen starts exactly at the accent column, on the
	// face's centerline row.
	center := string([]rune(splashArt[3])[splashAccent : splashAccent+splashSepW])
	if center != "──────" {
		t.Fatalf("suffix hyphen misplaced at accent %d: %q", splashAccent, center)
	}
	// The signature rows, verbatim (top, centerline, base).
	for i, want := range []int{0, 3, 7} {
		if splashArt[want] != splashArtRows037[i] {
			t.Fatalf("wordmark row %d = %q (want %q)", want, splashArt[want], splashArtRows037[i])
		}
	}
}

// TestSplashRevealAndEnd drives the animation timeline: nothing at t=0, a
// partial sweep mid-way with the cursor and the tagline already up (the
// caption dissolve starts in sync with the glyphs, so by the half-sweep
// mark it is on screen), the full wordmark after the sweep — and
// auto-dismiss when the duration elapses.
func TestSplashRevealAndEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a := app.New(newFakeHost(t).URL)
	a.Start(ctx)
	m := NewModel(a)
	m.W, m.H = 100, 30

	start := time.Now()
	m.splashStart = start

	// t=0: no wordmark ink yet, but the hint and rail are already up.
	m.now = start
	view := m.splashView()
	if !strings.Contains(stripANSI(view), splashHint) {
		t.Fatalf("hint missing from first frame")
	}
	if strings.Contains(stripANSI(view), "╔") || strings.Contains(stripANSI(view), "║") ||
		strings.Contains(stripANSI(view), "╚") || strings.Contains(stripANSI(view), "▌") {
		t.Fatalf("wordmark visible before the sweep starts:\n%s", stripANSI(view))
	}

	// Mid-sweep: partial wordmark with the cursor block — and the tagline
	// is already on screen: its dissolve started in sync with the
	// glyphs at t=0, so by the half-sweep mark it is done fading (hex
	// path) or simply riding the frame (flat path).
	m.now = start.Add(splashReveal / 2)
	plain := stripANSI(m.splashView())
	if !strings.Contains(plain, "║") || !strings.Contains(plain, "═") {
		t.Fatal("no wordmark ink mid-sweep")
	}
	if !strings.Contains(plain, "▌") {
		t.Fatal("cursor block missing mid-sweep")
	}
	if !strings.Contains(plain, splashTaglineSpaced()) {
		t.Fatal("tagline should start in sync with the sweep and be on screen mid-sweep")
	}

	// Just after the sweep: full wordmark, no cursor, tagline still up.
	m.now = start.Add(splashReveal + 50*time.Millisecond)
	plain = stripANSI(m.splashView())
	for i, want := range []int{0, 3, 7} {
		if !strings.Contains(plain, splashArtRows037[i]) {
			t.Fatalf("wordmark incomplete after the sweep (row %d):\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "▌") {
		t.Fatal("cursor should be gone after the sweep")
	}
	if !strings.Contains(plain, splashTaglineSpaced()) {
		t.Fatal("tagline missing after the sweep")
	}

	// A tick at the end of the duration auto-dismisses the splash.
	m.Update(tickMsg{t: start.Add(splashDuration)})
	if m.splashActive() {
		t.Fatalf("splash still active at %s", splashDuration)
	}
}

// TestSplashSkipAndNarrowFallback checks that any keypress dismisses the
// animation early and that a narrow terminal gets the one-line wordmark.
func TestSplashSkipAndNarrowFallback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a := app.New(newFakeHost(t).URL)
	a.Start(ctx)

	m := NewModel(a)
	m.W, m.H = 100, 30
	m.splashStart = time.Now()

	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.splashActive() {
		t.Fatal("keypress should dismiss the splash")
	}

	m.splashStart = time.Now()
	m.W = 30
	m.now = m.splashStart.Add(splashReveal + time.Millisecond)
	if !strings.Contains(stripANSI(m.splashView()), "DSH-CLI") {
		t.Fatal("narrow fallback missing the DSH-CLI wordmark")
	}
}

// TestSplashPreloadTranscript pins the splash-phase pre-load: while the
// boot animation covers the screen the roster baseline lands, boot picks
// the previous session, its tail page arrives — and the transcript cache
// must already be rendered, so the first frame after the splash is served
// from the warm cache instead of paying the markdown cost on screen.
func TestSplashPreloadTranscript(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Setenv("NO_COLOR", "")
	t.Setenv("COLORFGBG", "15;0")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a := app.New("http://127.0.0.1:3999")
	a.Start(ctx)
	m := NewModel(a)
	m.splashStart = time.Now()
	m.W, m.H = 120, 40

	st := m.st
	st.SetHost(&protocol.HostDescription{Version: "test", Cwd: "/tmp"})
	st.SetSessions([]protocol.SessionSummary{
		{SessionId: "s1", UpdatedAt: 1, Blank: false, Cwd: "/tmp"},
	})
	if m.st.Active() != "" {
		t.Fatal("setup: no session should be active yet")
	}

	// The roster lands while the splash covers the screen: boot auto-picks
	// s1 (the most recent non-blank) instead of minting a fresh session.
	m.Update(dirtyMsg{})
	if m.st.Active() != "s1" {
		t.Fatalf("boot should pick s1 during the splash, active = %q", m.st.Active())
	}

	// The tail page arrives: fold it into the store the way app.LoadTail
	// does, pulse the model — the dirty path must render the cache while
	// the splash is still on screen.
	msgData, _ := json.Marshal(map[string]any{
		"role":    "user",
		"content": []map[string]any{{"type": "text", "text": "hello from the last run"}},
	})
	st.LoadTail("s1", &protocol.HistoryResponse{Events: []protocol.HistoryEntry{
		{Event: protocol.SessionEvent{Type: "user/message", Seq: 1, Data: msgData}},
	}})
	m.Update(dirtyMsg{})
	if m.trans.total() == 0 {
		t.Fatal("splash pre-load: the transcript cache must be rendered while the splash is live")
	}

	// Splash falls: the first visible frame serves the pre-rendered cache
	// and shows the previous session's content.
	m.splashStart = time.Time{}
	if plain := stripANSI(m.View()); !strings.Contains(plain, "hello from the last run") {
		t.Fatalf("first frame after the splash must show the pre-loaded transcript:\n%s", plain)
	}
}

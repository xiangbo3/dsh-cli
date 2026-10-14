// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"dsh-cli/internal/app"
	"dsh-cli/internal/protocol"

	tea "github.com/charmbracelet/bubbletea"
)

// frameWriter captures what the standard renderer puts on the wire.
type frameWriter struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (w *frameWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf.Write(p)
	return len(p), nil
}

func (w *frameWriter) Flush() error { return nil }
func (w *frameWriter) Fd() uintptr  { return 0 }

func mustMsgJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func (w *frameWriter) snapshot() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// stripRepaintMarker drops View's per-second repaint marker from every row,
// so tests that compare frame content can ignore it.
func stripRepaintMarker(frame string) string {
	lines := strings.Split(frame, "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimSuffix(ln, repaintMarker)
	}
	return strings.Join(lines, "\n")
}

// TestFrameRepaintedEverySecond pins the self-heal: the alt-screen renderer
// rewrites only rows whose bytes changed, so a steady screen is painted once
// and any row the terminal garbles or loses (a wrapped row, a stray control
// byte, a redraw glitch) stays wrong until a resize forces a repaint. The
// whole frame must therefore be re-emitted on a steady beat.
func TestFrameRepaintedEverySecond(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	a := app.New(newFakeHost(t).URL)
	a.Start(ctx)
	m := NewModel(a)
	m.splashOff = true
	out := &frameWriter{}
	prog := tea.NewProgram(m, tea.WithAltScreen(),
		tea.WithInput(strings.NewReader("")), tea.WithOutput(out), tea.WithContext(ctx))
	m.SetProg(prog)
	done := make(chan struct{})
	go func() { prog.Run(); close(done) }()
	const H = 24
	prog.Send(tea.WindowSizeMsg{Width: 80, Height: H})
	time.Sleep(400 * time.Millisecond)

	// A slow trickle, so frames land on the wire without the deck or the
	// transcript rows themselves ever changing: the steady state that hides
	// the bug.
	stop := make(chan struct{})
	go func() {
		for i := 1; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			m.st.Event("s1", &protocol.SessionEvent{Type: "assistant/message", Seq: int64(i), Time: 1000,
				Data: mustMsgJSON(protocol.Message{Role: "assistant",
					Content: []protocol.ContentBlock{{Type: "text", Text: fmt.Sprintf("line %d", i)}}})})
			time.Sleep(300 * time.Millisecond)
		}
	}()
	time.Sleep(3200 * time.Millisecond)
	close(stop)

	stream := out.snapshot()
	// Each alt-screen flush starts at cursor home; inside one, a written row
	// ends in CRLF while a row the diff skips is a bare LF.
	flushes := strings.Split(stream, "\x1b[H")
	full, best := 0, 0
	for _, f := range flushes {
		if w := strings.Count(f, "\r\n"); w > best {
			best = w
		}
		if strings.Count(f, "\r\n") >= H-1 && strings.Contains(f, "\u2570") {
			full++
		}
	}
	if full < 2 {
		t.Fatalf("only %d flushes re-drew the whole frame (best %d of %d rows) over 3s: "+
			"a garbled row never heals", full, best, H)
	}

	prog.Send(tea.KeyMsg{Type: tea.KeyCtrlQ})
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}

// TestRepaintMarkerIsInvisible pins the marker's contract: it changes every
// row's bytes on alternate seconds (so the renderer cannot skip them forever)
// without changing what the screen shows — same row count, same display
// widths, same visible text.
func TestRepaintMarkerIsInvisible(t *testing.T) {
	m := scrollModel(t, 6)
	m.W, m.H = 80, 24
	m.now = time.Unix(1000, 0) // even second: unmarked
	a := m.View()
	m.now = time.Unix(1001, 0) // odd second: marked
	b := m.View()
	ra, rb := strings.Split(a, "\n"), strings.Split(b, "\n")
	if len(ra) != m.H || len(rb) != m.H {
		t.Fatalf("frame height %d/%d, want %d", len(ra), len(rb), m.H)
	}
	marked := 0
	for i := range ra {
		if i == 0 {
			continue // the top bar's clock rides the same second boundary
		}
		if stripCSI(ra[i]) != stripCSI(rb[i]) {
			t.Errorf("row %d: marker changed the visible content", i)
		}
		if plainWidth(ra[i]) != plainWidth(rb[i]) {
			t.Errorf("row %d: marker changed the width", i)
		}
		if ra[i] != rb[i] {
			marked++
		}
	}
	if marked != m.H-1 {
		t.Errorf("marked %d rows across a second boundary, want %d", marked, m.H-1)
	}
	if c := m.View(); c != b {
		t.Error("frames inside one second must not churn")
	}
}

// TestTermTitleDropsControlRunes pins the OSC 0 payload: a session title
// is model-authored text written straight to the tty, and a control rune
// in it would cut the escape sequence short and print the rest over the
// deck at the bottom of the screen.
func TestTermTitleDropsControlRunes(t *testing.T) {
	m := scrollModel(t, 3)
	// Valid JSON with the control runes escaped: the store's own filter
	// spares newlines, so the OSC payload is where they must be dropped.
	m.st.Event("s1", &protocol.SessionEvent{Type: "session/title", Seq: 90, Time: 1000,
		Data: []byte("{\"title\":\"ok\\u0007bad\\u001b[31m title\\nsecond line\"}")})
	m.W, m.H = 80, 24
	m.View()
	if strings.ContainsAny(m.termTitle, "\x00\x07\x1b\n\r") {
		t.Errorf("title reached the OSC with control runes: %q", m.termTitle)
	}
	if !strings.Contains(m.termTitle, "ok") {
		t.Errorf("title lost its printable text: %q", m.termTitle)
	}
}

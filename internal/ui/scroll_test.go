package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"dsh-cli/internal/app"
	"dsh-cli/internal/protocol"

	"github.com/charmbracelet/bubbletea"
)

// scrollModel builds a model whose active session holds n single-line user
// messages (markers "line-001" .. the last), tall enough to scroll. The
// host port is chosen so no live server interferes with the fixture.
func scrollModel(t *testing.T, n int) *Model {
	t.Helper()
	a := app.New("http://127.0.0.1:3999")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a.Start(ctx)
	m := NewModel(a)
	m.splashOff = true
	m.W, m.H = 120, 40
	m.st.SetActive("s1")
	for i := 1; i <= n; i++ {
		text := fmt.Sprintf("line-%03d", i)
		b, err := json.Marshal(protocol.Message{Role: "user",
			Content: []protocol.ContentBlock{{Type: "text", Text: text}}})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		m.st.Event("s1", &protocol.SessionEvent{Type: "user/message", Seq: int64(i), Time: 1000, Data: b})
	}
	return m
}

// topMarker returns the first transcript marker visible in the rendered
// frame ("line-NNN" -> NNN), or -1 when none is visible.
func topMarker(frame string) int {
	for _, ln := range strings.Split(frame, "\n") {
		if i := strings.Index(ln, "line-"); i >= 0 && i+8 <= len(ln) {
			if n, err := strconv.Atoi(ln[i+5 : i+8]); err == nil {
				return n
			}
		}
	}
	return -1
}

// TestTranscriptPagingAndWheel pins the transcript scroll surface: the
// mouse wheel (3 lines per notch), PgUp/PgDn pages, the arrow keys, the
// ctrl+g/ctrl+o jumps, and the follow re-arm when the view lands back on
// the last line.
func TestTranscriptPagingAndWheel(t *testing.T) {
	m := scrollModel(t, 100)
	m.View() // first frame: renders the follow state and measures the pane
	h := m.transH
	if h < 10 {
		t.Fatalf("transcript pane height = %d, want a real viewport", h)
	}
	// The tail assertions below need two pages from the top to run past
	// the bottom (total <= 3 viewports). The pane height is a layout
	// property (the framed input bar costs two rows), so rebuild the
	// fixture at the right height if the default roster overshoots.
	if m.trans.total() > 3*h {
		m = scrollModel(t, 3*h)
		m.View()
		h = m.transH
	}
	total := m.trans.total()
	if total < 2*h {
		t.Fatalf("transcript %d lines shorter than two viewports (%d)", total, h)
	}
	n0 := topMarker(m.View()) // follow mode: the tail is pinned
	if n0 <= 0 {
		t.Fatalf("no marker visible in follow state")
	}

	// One wheel notch up: exactly 3 lines earlier, follow drops.
	m.Update(tea.MouseMsg{X: 5, Y: 5, Button: tea.MouseButtonWheelUp})
	if m.follow {
		t.Fatal("wheel up must leave follow mode")
	}
	if got := topMarker(m.View()); got != n0-3 {
		t.Fatalf("wheel up: top marker = %d, want %d", got, n0-3)
	}

	// Wheeling back down re-arms follow on the last line.
	for i := 0; i < 10; i++ {
		m.Update(tea.MouseMsg{X: 5, Y: 5, Button: tea.MouseButtonWheelDown})
	}
	if !m.follow {
		t.Fatal("wheeling to the bottom must re-arm follow")
	}
	if got := topMarker(m.View()); got != n0 {
		t.Fatalf("wheel to bottom: top marker = %d, want %d", got, n0)
	}

	// PgUp from the follow state steps one page up from the tail (not a
	// jump to the top: the stored offset is anchored to the current view).
	m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.follow {
		t.Fatal("PgUp must leave follow mode")
	}
	if got := topMarker(m.View()); got != n0-h {
		t.Fatalf("PgUp: top marker = %d, want %d (one page up from the tail)", got, n0-h)
	}

	// PgDn lands back on the tail and re-arms follow.
	m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if got := topMarker(m.View()); got != n0 {
		t.Fatalf("PgDn: top marker = %d, want %d", got, n0)
	}
	if !m.follow {
		t.Fatal("PgDn to the bottom must re-arm follow")
	}

	// Arrow keys nudge one line (input is empty).
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := topMarker(m.View()); got != n0-1 {
		t.Fatalf("up arrow: top marker = %d, want %d", got, n0-1)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if !m.follow || topMarker(m.View()) != n0 {
		t.Fatal("down arrow to the bottom must re-arm follow on the tail")
	}

	// home to the top (input is empty), then pages down: two PgDn presses
	// from a viewport of height h cover h + h lines, which for this
	// fixture is past the bottom only after the second page.
	m.Update(tea.KeyMsg{Type: tea.KeyHome})
	if got := topMarker(m.View()); got != 1 {
		t.Fatalf("home: top marker = %d, want 1", got)
	}
	if m.follow {
		t.Fatal("home (top) must not keep follow")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if got := topMarker(m.View()); got != 1+h {
		t.Fatalf("PgDn from top: top marker = %d, want %d", got, 1+h)
	}
	if m.follow {
		t.Fatal("mid-transcript page must not re-arm follow")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if !m.follow || topMarker(m.View()) != n0 {
		t.Fatal("second PgDn must land on the tail and re-arm follow")
	}
}

// TestTranscriptWheelShortTranscript pins the degenerate case: a transcript
// shorter than the viewport cannot scroll — the wheel moves nothing and
// follow stays armed.
func TestTranscriptWheelShortTranscript(t *testing.T) {
	m := scrollModel(t, 5)
	m.View()
	before := topMarker(m.View())
	if before != 1 {
		t.Fatalf("short transcript: top marker = %d, want 1", before)
	}
	for i := 0; i < 5; i++ {
		m.Update(tea.MouseMsg{X: 5, Y: 5, Button: tea.MouseButtonWheelUp})
	}
	if got := m.scroll; got != 0 {
		t.Fatalf("scroll offset = %d, want 0 (nothing to scroll)", got)
	}
	if !m.follow {
		t.Fatal("short transcript: follow must stay armed")
	}
	if got := topMarker(m.View()); got != before {
		t.Fatalf("view moved: top marker = %d, want %d", got, before)
	}
}

// TestTranscriptHomeEnd pins the home/end keys: home jumps to the top
// (follow drops), end to the bottom (follow re-arms on the tail); with
// text in the input the keys steer the caret, not the transcript.
func TestTranscriptHomeEnd(t *testing.T) {
	m := scrollModel(t, 100)
	m.View()
	h := m.transH
	if h < 10 {
		t.Fatalf("transcript pane height = %d, want a real viewport", h)
	}
	// end from the follow state: the tail stays pinned, follow armed
	// Tail window: the first visible line is the (maxOff+1)th transcript
	// line, i.e. marker 100-h+1.
	tailTop := 101 - h
	m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if !m.follow {
		t.Fatal("end must keep follow armed on the tail")
	}
	if got := topMarker(m.View()); got != tailTop {
		t.Fatalf("end: top marker = %d, want %d", got, tailTop)
	}
	// home to the top
	m.Update(tea.KeyMsg{Type: tea.KeyHome})
	if m.follow {
		t.Fatal("home must drop follow mode")
	}
	if got := topMarker(m.View()); got != 1 {
		t.Fatalf("home: top marker = %d, want 1", got)
	}
	// nudge two lines down, then home back to the top
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := topMarker(m.View()); got != 3 {
		t.Fatalf("down down: top marker = %d, want 3 (two lines below the top)", got)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyHome})
	if got := topMarker(m.View()); got != 1 {
		t.Fatalf("home again: top marker = %d, want 1", got)
	}
	// end lands on the tail and re-arms follow
	m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if !m.follow || topMarker(m.View()) != tailTop {
		t.Fatalf("end: follow=%v top marker=%d, want follow and %d", m.follow, topMarker(m.View()), tailTop)
	}
	// with text in the input, home/end steer the caret, not the transcript
	for _, r := range "hi" {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if m.inp.cur != 2 {
		t.Fatalf("end with text: caret = %d, want 2", m.inp.cur)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyHome})
	if m.inp.cur != 0 {
		t.Fatalf("home with text: caret = %d, want 0", m.inp.cur)
	}
	if !m.follow {
		t.Fatal("caret home/end must not touch the follow state")
	}
}

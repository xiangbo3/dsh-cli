package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	"dsh-cli/internal/app"
	"dsh-cli/internal/protocol"

	"github.com/mattn/go-runewidth"
)

// TestStatusBarNarrowKeepsRate pins the strip's width contract: whatever
// the terminal width, the status line must fit the row, and the right-edge
// readouts (connection, tokens, t/s) must survive — the transient left
// segments yield space instead of clipping the rate off-screen.
func TestStatusBarNarrowKeepsRate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a := app.New("http://127.0.0.1:1") // closed port: no boot probe noise
	a.Start(ctx)
	m := NewModel(a)
	m.H = 30
	m.st.SetSessions([]protocol.SessionSummary{{SessionId: "s1"}})
	m.st.SetActive("s1")
	m.st.SetConnected(true)
	t0 := time.Now().Add(-10 * time.Second)
	e := func(seq int64, off time.Duration, typ, data string) {
		m.st.Event("s1", &protocol.SessionEvent{Type: typ, Seq: seq, Time: t0.Add(off).UnixMilli(), Data: []byte(data)})
	}
	e(1, 0, "turn/start", `{"turn":1}`)
	e(2, time.Second, "assistant/chunk", `{"turn":1,"step":1,"chunk":{"type":"text-delta","index":0,"text":"x"}}`)
	e(3, 2*time.Second, "assistant/chunk", `{"turn":1,"step":1,"chunk":{"type":"usage","usage":{"inputTokens":2000,"outputTokens":500}}}`)
	e(4, 2*time.Second, "assistant/message", `{"turn":1,"step":1,"message":{"role":"assistant","content":[{"type":"text","text":"x"}]},"usage":{"inputTokens":2000,"outputTokens":500}}`)
	m.now = time.Now()

	for _, w := range []int{48, 64, 96, 120} {
		bar := stripANSI(m.statusBar(w))
		if !strings.Contains(bar, "t/s") {
			t.Fatalf("w=%d: rate readout clipped: %q", w, bar)
		}
		if !strings.Contains(bar, "tokens:") {
			t.Fatalf("w=%d: tokens readout lost: %q", w, bar)
		}
		if n := runewidth.StringWidth(bar); n > w {
			t.Fatalf("w=%d: status line spills to %d: %q", w, n, bar)
		}
	}
}

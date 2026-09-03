package ui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"dsh-cli/internal/app"
	"dsh-cli/internal/core"
	"dsh-cli/internal/protocol"

	tea "github.com/charmbracelet/bubbletea"
)

// cachesApp builds a model against a dead host port (no live server may
// interfere with the fixtures).
func cachesApp(t *testing.T) *Model {
	t.Helper()
	a := app.New("http://127.0.0.1:3999")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a.Start(ctx)
	m := NewModel(a)
	m.splashOff = true
	m.W, m.H = 120, 40
	return m
}

func userMsgData(t *testing.T, text string) []byte {
	t.Helper()
	b, err := json.Marshal(protocol.Message{Role: "user",
		Content: []protocol.ContentBlock{{Type: "text", Text: text}}})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestTransCacheStreamingThrottle pins the render throttle: small deltas
// within the window keep the previous rows, large growth renders, and a
// shape change (a new item lands) force-flushes a queued redraw.
func TestTransCacheStreamingThrottle(t *testing.T) {
	m := cachesApp(t)
	c := m.trans

	base := &core.Item{Kind: core.KindUser, Gen: 7, Ver: 1, Text: "hello"}
	if !c.apply(m, []*core.Item{base}) {
		t.Fatal("first apply must render")
	}
	first := strings.Join(c.lines, "\n")

	// Small delta (same gen, ver bump, <32B within the window): skip.
	small := &core.Item{Kind: core.KindUser, Gen: 7, Ver: 2, Text: "hello world"}
	if c.apply(m, []*core.Item{small}) {
		t.Fatal("small in-window delta must not re-render")
	}
	if got := strings.Join(c.lines, "\n"); got != first {
		t.Fatalf("throttled delta changed the rows: %q", got)
	}

	// Large growth: render.
	bigText := "hello world " + strings.Repeat("x", 60)
	big := &core.Item{Kind: core.KindUser, Gen: 7, Ver: 3, Text: bigText}
	if !c.apply(m, []*core.Item{big}) {
		t.Fatal("large growth must re-render")
	}
	if got := strings.Join(c.lines, "\n"); !strings.Contains(got, bigText) {
		t.Fatalf("grown text missing: %q", got)
	}

	// Finalization: a small final delta is skipped, then the item set
	// changes shape (a turn-end row lands) and the queued redraw flushes.
	final := &core.Item{Kind: core.KindUser, Gen: 7, Ver: 4, Text: bigText + "!"}
	if c.apply(m, []*core.Item{final}) {
		t.Fatal("small in-window delta must not re-render")
	}
	turnEnd := &core.Item{Kind: core.KindTurnEnd, Gen: 8, Ver: 1,
		TurnEnd: &protocol.TurnEndReason{Kind: "completed"}}
	if !c.apply(m, []*core.Item{final, turnEnd}) {
		t.Fatal("shape change must flush the queued redraw")
	}
	if got := strings.Join(c.lines, "\n"); !strings.Contains(got, bigText+"!") {
		t.Fatalf("finalized text missing after flush: %q", got)
	}

	// Idle apply: everything cached, no work reported.
	if c.apply(m, []*core.Item{final, turnEnd}) {
		t.Fatal("idle apply must be a cache hit")
	}
}

// TestIdleFrameServesCache pins the idle-frame short-circuit: back-to-back
// frames without store movement are stable, and a store mutation (no dirty
// message in between) still reaches the next frame through the rev gate.
func TestIdleFrameServesCache(t *testing.T) {
	m := scrollModel(t, 20)
	f1 := m.View()
	if f2 := m.View(); f2 != f1 {
		t.Fatal("idle frames must be stable (no re-sync churning)")
	}

	m.st.Event("s1", &protocol.SessionEvent{Type: "user/message", Seq: 200, Time: 2000, Data: userMsgData(t, "line-999")})
	f3 := m.View()
	if !strings.Contains(f3, "line-999") {
		t.Fatal("store mutation must reach the next frame through the rev gate")
	}
	if f4 := m.View(); f4 != f3 {
		t.Fatal("idle frames after the mutation must be stable")
	}
}

// TestMdWidthLRU pins the markdown pipeline LRU: repeated (width, doc)
// pairs reuse the pipeline, the stalest key past the cap is evicted
// first, and the doc-color variant is a distinct pipeline.
func TestMdWidthLRU(t *testing.T) {
	m := cachesApp(t)
	r80 := m.mdFor(80, "")
	m.mdFor(100, "")
	// Touching 80 again is an LRU hit: same pipeline, no rebuild.
	if got := m.mdFor(80, ""); got != r80 {
		t.Fatal("width pipeline must be reused on an LRU hit")
	}
	// Seven more widths push the key set past the 8-entry cap: the
	// stalest key (100, touched least recently) is evicted, and the
	// recently touched 80 survives.
	for _, w := range []int{110, 120, 130, 140, 150, 160, 170} {
		m.mdFor(w, "")
	}
	if len(m.mds) != mdLRUCap {
		t.Fatalf("cache holds %d pipelines, want %d", len(m.mds), mdLRUCap)
	}
	if _, ok := m.mds[mdKey(100, "")]; ok {
		t.Fatal("stalest key (100) should have been evicted")
	}
	if r := m.mds[mdKey(80, "")]; r != r80 {
		t.Fatal("recently touched 80 pipeline must survive the eviction")
	}
	if got := m.mdFor(80, ""); got != r80 {
		t.Fatal("recalled key must be an LRU hit")
	}
	// A doc-color variant at the same width is a distinct pipeline.
	ru := m.mdFor(80, "uvoice")
	if ru == r80 || ru.doc != "uvoice" {
		t.Fatal("doc variants must not share pipelines")
	}
	if r := m.mds[mdKey(80, "uvoice")]; r != ru {
		t.Fatal("doc-variant pipeline must be published in the LRU")
	}
}

// TestSideToggleReWrapsTranscript pins the width half of the idle-frame
// gate: opening the sidebar narrows the transcript pane, the rows re-wrap
// on the next frame, and the following idle frame reuses the fresh rows.
func TestSideToggleReWrapsTranscript(t *testing.T) {
	m := scrollModel(t, 20)
	_ = m.View()
	w0 := m.lastTransWidth
	if w0 == 0 {
		t.Fatal("first frame must record the transcript width")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	w1 := m.transcriptWidth()
	if w1 == w0 {
		t.Skipf("pane width unchanged at this window size (%d)", w1)
	}
	f1 := m.View()
	if got := m.lastTransWidth; got != w1 {
		t.Fatalf("width gate did not fire after the toggle: lastTransWidth=%d want %d", got, w1)
	}
	if f2 := m.View(); f2 != f1 {
		t.Fatal("idle frames after the re-wrap must be stable")
	}
}

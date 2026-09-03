package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dsh-cli/internal/app"
	"dsh-cli/internal/i18n"
	"dsh-cli/internal/protocol"

	"github.com/charmbracelet/bubbletea"
)

// TestModelBarReadout pins the model readout: one line of name + effort
// (or name alone) riding the top bar, dropping out entirely when no model
// is known — the bottom status line no longer carries it, the permission
// chip opens that line instead.
func TestModelBarReadout(t *testing.T) {
	// Dead endpoint (not the real :3080): a running dev server must not
	// leak its host default model into this test — the unselected-readout
	// assertion wants the host fallback to be empty.
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	a := app.New(deadURL)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a.Start(ctx)
	m := NewModel(a)
	m.W, m.H = 120, 40
	m.st.SetActive("s1")

	if styled, plain := m.modelReadout(); styled != "" || plain != "" {
		t.Fatalf("unselected readout = %q/%q (want empty)", stripANSI(styled), plain)
	}

	m.st.ApplyModelSelection("s1", &protocol.ModelSelection{
		Provider: "acme", Model: "acme-alpha", ReasoningEffort: "high",
	})
	styled, plain := m.modelReadout()
	if !strings.Contains(plain, "acme-alpha · high") {
		t.Fatalf("readout = %q (want name + effort on one line)", plain)
	}
	if strings.Contains(plain, "\n") {
		t.Fatalf("readout split across lines: %q", plain)
	}
	if stripANSI(styled) != plain {
		t.Fatalf("styled/plain disagree: %q vs %q", stripANSI(styled), plain)
	}

	// Effort-free selection: the name stays, the effort part drops.
	m.st.ApplyModelSelection("s1", &protocol.ModelSelection{Provider: "acme", Model: "acme-beta"})
	_, plain = m.modelReadout()
	if !strings.Contains(plain, "acme-beta") || strings.Contains(plain, "effort") {
		t.Fatalf("readout after plain switch = %q", plain)
	}

	// With the session's advertised catalog cached, the raw id resolves to
	// the display name the model picker shows — the top bar consistency
	// pin. The later frame assertions want the raw id back, so the cache
	// is dropped again.
	m.st.SetModels("s1", &protocol.SessionModels{
		Groups: []protocol.ModelProviderGroup{{Id: "acme", Name: "Acme",
			Models: []protocol.ModelCatalogModel{{Id: "acme-beta", Name: "Acme Beta"}}}},
	})
	_, plain = m.modelReadout()
	if !strings.Contains(plain, "Acme Beta") || strings.Contains(plain, "acme-beta") {
		t.Fatalf("readout with catalog = %q (want the display name)", plain)
	}
	m.st.SetModels("s1", nil)

	// The model readout rides the top bar (name + effort or name alone);
	// the permission chip sits at the left of the status line, separating
	// itself — the bottom line no longer carries the model at all.
	pb, _ := json.Marshal(map[string]any{"currentValue": "read-only"})
	m.st.MuxProjection("s1", "permissions", 10, pb)
	if snap := m.st.Get("s1"); snap.Permission == nil || snap.Permission.CurrentValue != "read-only" {
		t.Fatalf("permission projection not applied: %+v", snap.Permission)
	}
	frame := stripANSI(m.View())
	top, bottom := "", ""
	for _, ln := range strings.Split(frame, "\n") {
		if strings.Contains(ln, "acme-beta") {
			top = ln
		}
		if strings.Contains(ln, m.th.Glyph.Shield) {
			bottom = ln
		}
	}
	if !strings.Contains(top, "· acme-beta") {
		t.Fatalf("top bar missing the model readout: %q", top)
	}
	if !strings.Contains(bottom, "Read Only") {
		t.Fatalf("status line missing the permission chip: %q", bottom)
	}
	if strings.Contains(bottom, "acme-beta") {
		t.Fatalf("model readout must not sit on the status line: %q", bottom)
	}
	if i := strings.Index(bottom, m.th.Glyph.Shield); i > 0 {
		t.Fatalf("unexpected prefix before the permission chip: %q", bottom[:i])
	}

	// The readout stays in the top bar: above the input line, never on
	// the status line below it.
	modelIdx, statusIdx, inputIdx := -1, -1, -1
	for i, ln := range strings.Split(frame, "\n") {
		if strings.Contains(ln, "acme-beta") {
			modelIdx = i
		}
		// The input line carries the prompt caret (its own caret is now an
		// inverse-video cell, not the block glyph).
		if strings.Contains(ln, m.th.Glyph.Caret) {
			inputIdx = i
		}
		if strings.Contains(ln, "ctrl+q quit") {
			statusIdx = i
		}
	}
	if modelIdx < 0 || statusIdx < 0 || inputIdx < 0 {
		t.Fatalf("frame missing readout/input/status lines: %q", frame)
	}
	if modelIdx >= statusIdx {
		t.Fatalf("model readout (line %d) must no longer share the status line (%d)",
			modelIdx, statusIdx)
	}
	if inputIdx <= modelIdx {
		t.Fatalf("model readout must sit in the top bar above the input line: input %d, model %d",
			inputIdx, modelIdx)
	}
}

// TestTranscriptFlushesOnModelSwitch pins the fix for "after switching the
// model the transcript keeps the previous name": assistant headers render
// the live model label and the render cache is invalidated when it changes.
func TestTranscriptFlushesOnModelSwitch(t *testing.T) {
	a := app.New("http://127.0.0.1:3080")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a.Start(ctx)
	m := NewModel(a)
	m.W, m.H = 120, 40
	m.st.SetActive("s1")
	m.st.ApplyModelSelection("s1", &protocol.ModelSelection{Provider: "acme", Model: "acme-alpha"})

	mk := func(seq int64, typ string, v any) *protocol.SessionEvent {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal %s: %v", typ, err)
		}
		return &protocol.SessionEvent{Type: typ, Seq: seq, Time: 1000, Data: b}
	}
	m.st.Event("s1", mk(2, "assistant/message", protocol.AssistantMessageEventData{
		Turn: 1, Step: 1,
		Message: protocol.Message{Role: "assistant",
			Content: []protocol.ContentBlock{{Type: "text", Text: "hello"}}},
	}))
	snap := m.st.Get("s1")
	if len(snap.Items) == 0 {
		t.Fatal("assistant item was not folded")
	}
	items := snap.Items

	if !m.trans.apply(m, items) {
		t.Fatal("first apply should render the items")
	}
	if m.trans.apply(m, items) {
		t.Fatal("second apply must be a cache hit")
	}
	got := strings.Join(m.trans.lines, "\n")
	if !strings.Contains(got, "acme-alpha") {
		t.Fatalf("assistant header should show the current model: %q", got)
	}

	// Switching the model must re-render the cached items under the new
	// label, not keep the previous name.
	m.st.ApplyModelSelection("s1", &protocol.ModelSelection{
		Provider: "acme", Model: "acme-beta", ReasoningEffort: "high",
	})
	if !m.trans.apply(m, items) {
		t.Fatal("model switch must invalidate the render cache")
	}
	got = strings.Join(m.trans.lines, "\n")
	if strings.Contains(got, "acme-alpha") {
		t.Fatalf("stale model name after switch: %q", got)
	}
	if !strings.Contains(got, "acme-beta") {
		t.Fatalf("new model name missing: %q", got)
	}
}

// TestPickerEnterRecordsSelection pins the fix at the source: a confirmed
// picker selection is recorded in the store immediately, so the label is
// fresh before the next turn's request/context event ever arrives.
func TestPickerEnterRecordsSelection(t *testing.T) {
	var gotEffort string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/session.selectModel" {
			http.NotFound(w, r)
			return
		}
		var req struct {
			Payload json.RawMessage `json:"payload"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var p struct {
			ReasoningEffort string `json:"reasoningEffort"`
		}
		_ = json.Unmarshal(req.Payload, &p)
		gotEffort = p.ReasoningEffort
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"type":"server-response","result":{"ok":true,"value":{"selected":{"provider":"acme","model":"acme-beta","reasoningEffort":"high"}}}}`)
	}))
	defer srv.Close()

	a := app.New(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a.Start(ctx)
	m := NewModel(a)
	m.splashOff = true
	m.st.SetActive("s1")

	pk := newModelPicker()
	pk.fill(&protocol.SessionModels{
		Groups: []protocol.ModelProviderGroup{{Id: "acme", Name: "Acme",
			Models: []protocol.ModelCatalogModel{{Id: "acme-alpha"}, {Id: "acme-beta"}}}},
		Current: protocol.ModelSelection{Provider: "acme", Model: "acme-alpha"},
	})
	m.mods = append(m.mods, pk)

	cmd, handled := m.handleModalKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !handled || cmd == nil {
		t.Fatal("enter on the picker must confirm the selection")
	}
	msg := cmd()
	if e, ok := msg.(rpcErrMsg); ok {
		t.Fatalf("select model failed: %v", e.err)
	}
	if len(m.mods) != 0 {
		t.Fatal("picker should be closed after confirming")
	}
	if gotEffort != "" {
		t.Fatalf("default effort must be sent as empty, got %q", gotEffort)
	}
	snap := m.st.Get("s1")
	if snap.ModelSel == nil || snap.ModelSel.Model != "acme-beta" ||
		snap.ModelSel.ReasoningEffort != "high" {
		t.Fatalf("ModelSel = %+v (want acme/acme-beta high)", snap.ModelSel)
	}
	if label := m.ctxLabel(); label != "acme-beta" {
		t.Fatalf("ctxLabel = %q, want acme-beta", label)
	}
}

// TestPickerHeaderUsesDisplayName pins the picker's header line: it shows
// the display name the roster advertises (and the top bar does), not the
// raw model id.
func TestPickerHeaderUsesDisplayName(t *testing.T) {
	a := app.New("http://127.0.0.1:3080")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a.Start(ctx)
	m := NewModel(a)
	m.W, m.H = 120, 40

	pk := newModelPicker()
	pk.fill(&protocol.SessionModels{
		Groups: []protocol.ModelProviderGroup{{Id: "custom", Name: "Custom",
			Models: []protocol.ModelCatalogModel{{Id: "qwen3.8-27b-uc", Name: "qwen3.8-27b"}}}},
		Current: protocol.ModelSelection{
			Provider: "custom", Model: "qwen3.8-27b-uc", ReasoningEffort: "high",
		},
	})
	header := pk.view(m, 100, 20)[0]
	if !strings.Contains(header, "qwen3.8-27b (high)") {
		t.Fatalf("picker header = %q (want the display name + effort)", header)
	}
	if strings.Contains(header, "qwen3.8-27b-uc") {
		t.Fatalf("picker header carries the raw id: %q", header)
	}
}

// TestStatusTokenTotals pins the bottom-line token readout: the session's
// cumulative in/out totals (same aggregation as the turn markers) close
// the status bar, and the removed key hints stay gone.
func TestStatusTokenTotals(t *testing.T) {
	a := app.New("http://127.0.0.1:3080")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a.Start(ctx)
	m := NewModel(a)
	m.W, m.H = 120, 40
	m.st.SetActive("s1")

	// Hermetic face: a user-edited zh catalog that localizes the token
	// label (and its arrow words). The arrows are theme glyphs appended
	// by the status bar, so they must survive the catalog edit — the
	// test pins exactly that property.
	dir := t.TempDir()
	t.Setenv("DSH_LOCALES", dir)
	t.Setenv("HOME", dir)
	if err := os.WriteFile(filepath.Join(dir, "zh.json"),
		[]byte(`{"status.tokens":" tokens: %s 入 / %s 出"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	l, _, err := i18n.Load("zh_CN.UTF-8")
	if err != nil {
		t.Fatal(err)
	}
	m.loc = l

	mk := func(seq int64, usage *protocol.TokenUsage) *protocol.SessionEvent {
		b, _ := json.Marshal(protocol.AssistantMessageEventData{Turn: int(seq), Step: 1,
			Usage: usage,
			Message: protocol.Message{Role: "assistant",
				Content: []protocol.ContentBlock{{Type: "text", Text: "x"}}}})
		return &protocol.SessionEvent{Type: "assistant/message", Seq: seq, Time: int64(seq), Data: b}
	}
	m.st.Event("s1", mk(1, &protocol.TokenUsage{InputTokens: 5, CacheReadTokens: 2, OutputTokens: 1}))
	m.st.Event("s1", mk(2, &protocol.TokenUsage{InputTokens: 4, OutputTokens: 3}))

	line := ""
	for _, ln := range strings.Split(stripANSI(m.View()), "\n") {
		if strings.Contains(ln, "ctrl+q quit") {
			line = ln
			break
		}
	}
	if line == "" {
		t.Fatal("status line not found")
	}
	// in = (5+2) + 4, out = 1 + 3 — the documented aggregation. The
	// in/out arrows are theme glyphs (appended by the status bar, not
	// cataloged), so build the want from the live theme; the edited
	// catalog's 入/出 words ride the line, the arrows do not.
	wantTok := "tokens: " + m.th.Glyph.TokenIn + "11 入 / " + m.th.Glyph.TokenOut + "4 出"
	if !strings.Contains(line, wantTok) {
		t.Fatalf("token readout missing or wrong (want %q): %q", wantTok, line)
	}
	// The words in/out no longer ride the status line.
	for _, gone := range []string{" 11 in", " 4 out"} {
		if strings.Contains(line, gone) {
			t.Fatalf("word in/out still on the status line: %q (line: %q)", gone, line)
		}
	}
	for _, gone := range []string{"\u21b5 send", "ctrl+n", "ctrl+m", "ctrl+z"} {
		if strings.Contains(line, gone) {
			t.Fatalf("removed hint still on the status line: %q (line: %q)", gone, line)
		}
	}
}

// TestStatusContextBar pins the bottom-line context progress bar: the
// request/context window drives a 10-cell bar with its percentage behind
// the key hints, and the bar stays hidden until a window is advertised.
func TestStatusContextBar(t *testing.T) {
	a := app.New("http://127.0.0.1:3080")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a.Start(ctx)
	m := NewModel(a)
	m.W, m.H = 120, 40
	m.st.SetActive("s1")
	m.loc = i18n.English() // hermetic: the bar assertions pin the built-in hint face

	statusLine := func() string {
		for _, ln := range strings.Split(stripANSI(m.View()), "\n") {
			if strings.Contains(ln, "ctrl+q quit") {
				return ln
			}
		}
		t.Fatal("status line not found")
		return ""
	}

	// No context window advertised yet: the bar is off.
	if line := statusLine(); strings.Contains(line, "100%") || strings.Contains(line, "\u2588") {
		t.Fatalf("bar shown without a context window: %q", line)
	}

	cdb, _ := json.Marshal(protocol.RequestContextData{
		Provider: "acme", Model: "acme-sonnet", ContextWindow: 1000,
	})
	m.st.Event("s1", &protocol.SessionEvent{
		Type: "request/context", Seq: 1, Time: 1, Data: cdb,
	})
	// used = 400 + 150 + 50 = 600 of a 1000 window -> 60%.
	amb, _ := json.Marshal(protocol.AssistantMessageEventData{Turn: 1, Step: 1,
		Usage: &protocol.TokenUsage{InputTokens: 400, CacheReadTokens: 150, OutputTokens: 50},
		Message: protocol.Message{Role: "assistant",
			Content: []protocol.ContentBlock{{Type: "text", Text: "x"}}}})
	m.st.Event("s1", &protocol.SessionEvent{
		Type: "assistant/message", Seq: 2, Time: 2, Data: amb,
	})

	line := statusLine()
	icon := m.th.Glyph.Ctx
	want := "ctrl+q quit  " + icon + " " + strings.Repeat("\u2588", 6) + strings.Repeat("\u2591", 4) + " 60%"
	if !strings.Contains(line, want) {
		t.Fatalf("context bar missing after the hints: %q", line)
	}
	// The bar sits before the padding/connection side of the line. The
	// connection marker depends on whether the live downlink (the app
	// points at the real :3080 when a server happens to run there) was
	// up at frame time, so accept either face — it just must stay after
	// the bar.
	conn := m.loc.T("status.reconnect")
	if !strings.Contains(line, conn) {
		conn = m.loc.T("status.live")
	}
	if i, j := strings.Index(line, "ctrl+q quit"), strings.Index(line, conn); j < i {
		t.Fatalf("bar must precede the connection status: %q", line)
	}
}

// TestStatusContextPressureBar pins the context bar against the host's
// live "contextPressure" projection (the readout's primary source): the
// tail-page baseline and the mux push both feed it, the bar renders
// pressureTokens against contextWindow, and a projection without a
// window leaves the bar off.
func TestStatusContextPressureBar(t *testing.T) {
	a := app.New("http://127.0.0.1:3080")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a.Start(ctx)
	m := NewModel(a)
	m.W, m.H = 120, 40
	m.st.SetActive("s1")
	m.loc = i18n.English() // hermetic: the bar assertions pin the built-in hint face

	statusLine := func() string {
		for _, ln := range strings.Split(stripANSI(m.View()), "\n") {
			if strings.Contains(ln, "ctrl+q quit") {
				return ln
			}
		}
		t.Fatal("status line not found")
		return ""
	}

	// Baseline: the tail page's projection carries the window and the
	// current footprint: 105301 of 200000 = 52%.
	m.st.LoadTail("s1", &protocol.HistoryResponse{
		Projections: &protocol.ProjectionsBlock{
			AsOfSeq: 1,
			Values: map[string]json.RawMessage{
				"contextPressure": json.RawMessage(`{"pressureTokens":105301,"projectedTokens":105606,"contextWindow":200000}`),
			},
		},
	})
	line := statusLine()
	icon := m.th.Glyph.Ctx
	want := "ctrl+q quit  " + icon + " " + strings.Repeat("\u2588", 5) + strings.Repeat("\u2591", 5) + " 52%"
	if !strings.Contains(line, want) {
		t.Fatalf("pressure bar missing after the hints: %q", line)
	}

	// Live push: a newer mux projection moves the bar (160000 -> 80%).
	m.st.MuxProjection("s1", "contextPressure", 2,
		json.RawMessage(`{"pressureTokens":160000,"projectedTokens":160300,"contextWindow":200000}`))
	line = statusLine()
	want = "ctrl+q quit  " + icon + " " + strings.Repeat("\u2588", 8) + strings.Repeat("\u2591", 2) + " 80%"
	if !strings.Contains(line, want) {
		t.Fatalf("live pressure bar missing: %q", line)
	}

	// A projection without a window carries no data: the bar goes off
	// again (no request/context event was adopted in this session).
	m.st.MuxProjection("s1", "contextPressure", 3, json.RawMessage(`{}`))
	line = statusLine()
	if strings.Contains(line, "%") {
		t.Fatalf("bar shown without a known window: %q", line)
	}
}

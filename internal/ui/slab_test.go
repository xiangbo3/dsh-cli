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

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestCodeSlabPalette(t *testing.T) {
	th := NewTheme()
	th.NoColor = false
	th.LightBgn = false
	th.setPalette()
	if th.CCodeBG == "" {
		t.Error("dark palette has no code slab")
	}
	th.LightBgn = true
	th.setPalette()
	if th.CCodeBG == "" {
		t.Error("light palette has no code slab")
	}
	th.NoColor = true
	th.setPalette()
	if th.CCodeBG != "" {
		t.Error("NoColor must have no slab (the region stays on the page)")
	}
}

func TestPaintCodeSlab(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	a := app.New(newFakeHost(t).URL)
	m := NewModel(a)
	m.th.Profile = termenv.TrueColor
	m.th.NoColor = false
	m.th.setPalette()
	m.th.styleGen++
	lines := paintCodeSlab(m.th, []string{"  hello"}, 10)
	if len(lines) != 1 || !strings.Contains(lines[0], "48;") {
		t.Fatalf("slab line = %q, want a background", lines[0])
	}
	if plainWidth(lines[0]) != 10 {
		t.Errorf("slab line width = %d, want 10", plainWidth(lines[0]))
	}
	// A palette without a slab color leaves the lines untouched.
	m.th.NoColor = true
	m.th.setPalette()
	m.th.styleGen++
	if got := paintCodeSlab(m.th, []string{"  hello"}, 10); got[0] != "  hello" {
		t.Error("empty slab must pass the lines through")
	}
}

func TestSlabOnlyOnCodeCards(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	a := app.New(newFakeHost(t).URL)
	m := NewModel(a)
	m.th.Profile = termenv.TrueColor
	m.th.NoColor = false
	m.th.setPalette()
	m.th.styleGen++
	code := renderToolCard(m, &core.ToolBlock{Name: "run_code", ArgsFull: `{"description":"probe"}`, Done: true}, 60)
	if !strings.Contains(code[0], "48;") {
		t.Error("run_code card must ride the slab")
	}
	other := renderToolCard(m, &core.ToolBlock{Name: "bash", ArgsFull: `{"command":"ls"}`, Done: true}, 60)
	if strings.Contains(other[0], "48;") {
		t.Error("bash card must stay on the page")
	}
}

// slabFrameModel is a 120x40 model whose transcript holds a run_code card
// with a reasoning fold right below it: the shape the code slab used to
// bleed onto.
func slabFrameModel(t *testing.T) *Model {
	t.Helper()
	lipgloss.SetColorProfile(termenv.TrueColor)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a := app.New(newFakeHost(t).URL)
	a.Start(ctx)
	t.Setenv("DSH_CLI_HOME", t.TempDir())
	m := NewModel(a)
	m.W, m.H = 120, 40
	m.th.Profile = termenv.TrueColor
	m.th.NoColor = false
	m.th.setPalette()
	m.th.styleGen++
	m.st.SetSessions([]protocol.SessionSummary{{SessionId: "s1", Cwd: "/tmp/x"}})
	m.st.SetActive("s1")
	m.st.LoadTail("s1", &protocol.HistoryResponse{Events: []protocol.HistoryEntry{
		{Event: protocol.SessionEvent{Type: "user/message", Seq: 1, Data: json.RawMessage(
			`{"role":"user","content":[{"type":"text","text":"hi"}]}`)}},
		{Event: protocol.SessionEvent{Type: "assistant/message", Seq: 2, Data: json.RawMessage(
			`{"turn":1,"step":1,"message":{"role":"assistant","content":[{"type":"tool-call","id":"c1","name":"run_code","arguments":"{\"description\":\"probe\"}"},{"type":"reasoning","text":"musing"}]}}`)}},
		{Event: protocol.SessionEvent{Type: "tool/result", Seq: 3, Data: json.RawMessage(
			`{"turn":1,"toolCallId":"c1","content":[{"type":"text","text":"ok"}]}`)}},
	}})
	return m
}

// TestCodeSlabClosesEachLine pins the slab line as self-contained: it still
// carries the code surface, but it ends on a reset instead of a live state.
func TestCodeSlabClosesEachLine(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	a := app.New(newFakeHost(t).URL)
	m := NewModel(a)
	m.th.Profile = termenv.TrueColor
	m.th.NoColor = false
	m.th.setPalette()
	m.th.styleGen++
	for _, ln := range paintCodeSlab(m.th, []string{"  hello", "  second line, longer"}, 20) {
		if !strings.Contains(ln, "48;") {
			t.Errorf("slab line lost its surface: %q", ln)
		}
		if st := tailState(ln); len(st.bg) > 0 {
			t.Errorf("slab line ends with a live bg %v: %q", st.bg, ln)
		}
	}
}

// TestSlabStopsAtThePane is the ctrl+t regression: with the dock open beside
// a run_code card, the code surface stops at the transcript pane. The live
// tail used to color the gutter and the dock's whole column on those rows,
// and — a newline carries no reset — the row below the card, which read as
// one slab row too many (the thinking fold came up on the slab).
func TestSlabStopsAtThePane(t *testing.T) {
	m := slabFrameModel(t)
	m.dockVisible = true
	codeBG := m.th.CodeState().bg
	_, transW, _ := m.paneWidths(m.W)
	rows := strings.Split(m.View(), "\n")

	slabRows, dockClean := 0, true
	for y := 2; y < 2+m.transH && y < len(rows); y++ {
		ln := rows[y]
		for _, c := range scanStyled(ln) {
			if len(c.state.bg) == 0 || !eqParams(c.state.bg, codeBG) {
				continue
			}
			if c.col >= transW {
				t.Errorf("row %d col %d (gutter/dock) rides the code slab: %q",
					y, c.col, strings.TrimRight(stripANSI(ln), " "))
				dockClean = false
			}
		}
		if st := tailState(ln); len(st.bg) > 0 {
			t.Errorf("row %d ends on a live bg %v: the row below inherits it: %q",
				y, st.bg, strings.TrimRight(stripANSI(ln), " "))
		}
		if strings.Contains(stripANSI(ln), "run_code") {
			slabRows++
		}
	}
	if slabRows == 0 {
		t.Fatal("the probe must put a run_code card on screen")
	}
	if dockClean {
		// The slab itself must survive the fix: the card row carries it.
		found := false
		for _, ln := range rows {
			for _, c := range scanStyled(ln) {
				if len(c.state.bg) > 0 && eqParams(c.state.bg, codeBG) {
					found = true
				}
			}
		}
		if !found {
			t.Error("the run_code card lost its slab")
		}
	}
}

// TestSlabClipClosesTail is the same smear through the pane clip: a slab
// line the transcript pane cuts (tool output in CJK wraps late, so it
// overruns its budget) must still end on a clean state — otherwise the
// columns right of the pane and the row below ride the code surface.
func TestSlabClipClosesTail(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	a := app.New(newFakeHost(t).URL)
	m := NewModel(a)
	m.th.Profile = termenv.TrueColor
	m.th.NoColor = false
	m.th.setPalette()
	m.th.styleGen++
	m.verbose = true
	tb := &core.ToolBlock{
		Name:       "run_code",
		ArgsFull:   `{"description":"probe"}`,
		Done:       true,
		ResultText: strings.Repeat("宽字符输出没有空格可断句", 6),
	}
	const w = 30
	var wide bool
	for _, ln := range renderToolCard(m, tb, w) {
		if plainWidth(ln) > w {
			wide = true
		}
	}
	if !wide {
		t.Fatal("the probe needs a line the pane will clip")
	}
	for _, ln := range padLines(renderToolCard(m, tb, w), w, 8) {
		if st := tailState(ln); len(st.bg) > 0 {
			t.Errorf("clipped slab line ends with a live bg %v: %q", st.bg, ln)
		}
	}
}

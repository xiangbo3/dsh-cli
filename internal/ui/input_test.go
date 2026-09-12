// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"dsh-cli/internal/app"
	"dsh-cli/internal/core"
	"dsh-cli/internal/protocol"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

// keyModel builds a headless 100x30 model for bare-inputLine key tests:
// handleKey's multi-line caret walk needs the window size and the theme,
// both of which live on the model.
func keyModel(t *testing.T) *Model {
	t.Helper()
	m := NewModel(app.New("http://127.0.0.1:3999"))
	m.splashOff = true
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return m
}

// TestInputCtrlBindings pins the editor key bindings of the input box:
// ctrl+a / ctrl+e jump to the logical line start/end, ctrl+b / ctrl+f
// move the cursor one rune left/right, ctrl+d deletes the char at the
// caret, and ctrl+u kills from the caret to the line end (newlines
// survive).
func TestInputCtrlBindings(t *testing.T) {
	press := func(in *inputLine, typ tea.KeyType) {
		ok, send := in.handleKey(keyModel(t), tea.KeyMsg{Type: typ})
		if !ok || send {
			t.Fatalf("ctrl key %v: want consumed, got ok=%v send=%v", typ, ok, send)
		}
	}
	// Single line: homing, stepping and delete-forward.
	in := newInputLine()
	for _, r := range "hello" {
		in.insertRune(r)
	}
	press(in, tea.KeyCtrlB)
	if in.cur != 4 {
		t.Fatalf("ctrl+b: cur %d, want 4", in.cur)
	}
	press(in, tea.KeyCtrlF)
	if in.cur != 5 {
		t.Fatalf("ctrl+f: cur %d, want 5", in.cur)
	}
	press(in, tea.KeyCtrlA) // home
	press(in, tea.KeyCtrlF)
	press(in, tea.KeyCtrlF)
	press(in, tea.KeyCtrlF)
	press(in, tea.KeyCtrlF) // caret before "o"
	press(in, tea.KeyCtrlD) // removes "o"
	if got := in.value(); got != "hell" {
		t.Fatalf("ctrl+d: text %q, want %q", got, "hell")
	}
	// Delete at the very end is a no-op.
	in2 := newInputLine()
	for _, r := range "ab" {
		in2.insertRune(r)
	}
	press(in2, tea.KeyCtrlD)
	if got := in2.value(); got != "ab" {
		t.Fatalf("ctrl+d at end: text %q, want %q", got, "ab")
	}

	// Home/end and kill-to-end-of-line.
	in3 := newInputLine()
	for _, r := range "abcdef" {
		in3.insertRune(r)
	}
	press(in3, tea.KeyCtrlA) // start of line
	press(in3, tea.KeyCtrlF) // step in
	press(in3, tea.KeyCtrlF) // step in (cur == 2)
	press(in3, tea.KeyCtrlU) // kill "cdef"
	if got := in3.value(); got != "ab" {
		t.Fatalf("ctrl+u: text %q, want %q", got, "ab")
	}
	press(in3, tea.KeyCtrlE) // end of line
	if in3.cur != 2 {
		t.Fatalf("ctrl+e: cur %d, want 2", in3.cur)
	}
	press(in3, tea.KeyCtrlA) // back to the start
	press(in3, tea.KeyCtrlU) // kill the whole line
	if got := in3.value(); got != "" {
		t.Fatalf("ctrl+u full: text %q, want empty", got)
	}
	// Deleting into an empty caret spot must not panic or drop runes.
	press(in3, tea.KeyCtrlD)
	if got := in3.value(); got != "" {
		t.Fatalf("ctrl+d empty: text %q, want empty", got)
	}

	// Multi-line: ctrl+a / ctrl+e / ctrl+u stay within the logical line.
	in4 := newInputLine()
	for _, r := range "abc\nxyz" {
		in4.insertRune(r)
	}
	press(in4, tea.KeyCtrlA) // caret on the second line after typing
	if in4.cur != 4 {
		t.Fatalf("ctrl+a: cur %d, want 4 (second line start)", in4.cur)
	}
	press(in4, tea.KeyCtrlU) // kill "xyz", keep the newline
	if got := in4.value(); got != "abc\n" {
		t.Fatalf("ctrl+u multiline: text %q, want %q", got, "abc\n")
	}
	press(in4, tea.KeyCtrlB) // step back onto the first line
	press(in4, tea.KeyCtrlE) // first line end
	if in4.cur != 3 {
		t.Fatalf("ctrl+e: cur %d, want 3 (first line end)", in4.cur)
	}
	press(in4, tea.KeyCtrlD) // remove the newline
	if got := in4.value(); got != "abc" {
		t.Fatalf("ctrl+d newline: text %q, want %q", got, "abc")
	}
}

// TestInputCtrlKeysEndToEnd pipes raw terminal bytes (typed runes plus the
// six ctrl control codes) through the real bubbletea input reader, proving
// the byte-decode → cursor/edit chain end to end: the unit test above
// constructs tea.KeyMsgs directly and would still pass if the terminal
// bytes were decoded as plain letters.
func TestInputCtrlKeysEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fh := newFakeHost(t)
	fh.sessions = []protocol.SessionSummary{{SessionId: "s1"}} // a row to boot: no auto-create mid-keystream
	a := app.New(fh.URL)
	a.Start(ctx)
	m := NewModel(a)
	m.splashOff = true // headless key test: no boot animation

	var out bytes.Buffer
	// Type "abcd", then:
	//  0x01 ctrl+a home        -> caret 0
	//  0x02 ctrl+b left        -> caret 0 (already there)
	//  0x06 ctrl+f right       -> caret 1
	//  0x15 ctrl+u kill to eol -> "a"
	//  0x05 ctrl+e end of line -> caret 1
	//  0x04 ctrl+d delete fwd  -> no-op at end
	raw := strings.NewReader("abcd\x01\x02\x06\x15\x05\x04")
	prog := tea.NewProgram(m,
		tea.WithInput(raw),
		tea.WithOutput(&out),
		tea.WithoutRenderer(),
		tea.WithContext(ctx),
	)
	m.SetProg(prog)

	go func() {
		prog.Send(tea.WindowSizeMsg{Width: 120, Height: 40})
		// Let the reader drain and the model process all the key messages.
		time.Sleep(400 * time.Millisecond)
		prog.Quit()
	}()

	if _, err := prog.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := m.inp.value(); got != "a" {
		t.Fatalf("input = %q, want %q (ctrl+u killed the rest of the line)", got, "a")
	}
	if m.inp.cur != 1 {
		t.Fatalf("caret = %d, want 1 (end of line)", m.inp.cur)
	}
}

// cursorSeq returns the SGR prefix the theme emits for the caret cell
// (profile-independent; same technique as BarBGSeq).
func cursorSeq(th *Theme) string {
	raw := th.BarCursor().Render("x")
	if i := strings.IndexByte(raw, 'x'); i > 0 {
		return raw[:i]
	}
	return ""
}

// spaceCursorSeq is the SGR prefix of the end-of-line caret (the
// underlined space): lipgloss renders a lone space through its space
// styler, whose SGR differs from the text cells' (no attribute doubling),
// so the space cell's prefix must be probed with a space of its own.
func spaceCursorSeq(th *Theme) string {
	raw := th.BarCursor().Render(" ")
	if i := strings.IndexByte(raw, ' '); i > 0 {
		return raw[:i]
	}
	return ""
}

// TestInputRenderCaretAndTransparentSurface pins the input box contract:
// no baked background anywhere on the line (the text rides the terminal's
// own background), and the caret drawn as the accent underlined char at
// the cursor position (an underlined space when the input is empty).
func TestInputRenderCaretAndTransparentSurface(t *testing.T) {
	// Stable color profile + palette for assertions (tests normally run
	// piped, and sandboxes often export NO_COLOR).
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Setenv("NO_COLOR", "")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a := app.New(newFakeHost(t).URL)
	a.Start(ctx)

	m := NewModel(a)
	m.W, m.H = 120, 40
	m.inp.insertRune('h')
	m.inp.insertRune('i')

	raw := m.inp.render(m, m.W)[0]
	if strings.Contains(raw, m.th.Glyph.Cursor) {
		t.Fatalf("foreign block glyph still drawn as the caret: %q", stripANSI(raw))
	}
	// "hi" typed → the caret sits past the 'i': an underlined space.
	if !strings.Contains(raw, spaceCursorSeq(m.th)+" ") {
		t.Fatalf("inverted end-of-line caret missing: %q", stripANSI(raw))
	}
	if !strings.Contains(stripANSI(raw), "hi") {
		t.Fatalf("typed text missing: %q", stripANSI(raw))
	}
	// Transparent surface: no baked background behind the typed text.
	if strings.Contains(raw, "\x1b[48;") {
		t.Fatalf("input text still bakes a background: %q", raw)
	}

	// Empty input still shows a caret hint at the cursor.
	e := NewModel(a)
	e.W, e.H = 120, 40
	if !strings.Contains(e.inp.render(e, 120)[0], spaceCursorSeq(e.th)+" ") {
		t.Fatal("caret hint missing for empty input")
	}
}

// TestInputRenderCaretOverChar pins the accent underlined caret over
// typed chars: the char under the cursor is drawn in the accent with an
// underline in place (no block glyph, no extra cell — wide chars stay
// wide), and only a cursor parked past the last char adds the
// underlined-space cell.
func TestInputRenderCaretOverChar(t *testing.T) {
	// Stable color profile + palette for assertions (tests normally run
	// piped, and sandboxes often export NO_COLOR).
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Setenv("NO_COLOR", "")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a := app.New(newFakeHost(t).URL)
	a.Start(ctx)

	m := NewModel(a)
	m.W, m.H = 120, 40
	seq := cursorSeq(m.th)

	in := newInputLine()
	for _, r := range "hi" {
		in.insertRune(r)
	}
	in.moveLeft() // caret over the 'i'

	raw := in.render(m, 120)[0]
	if !strings.Contains(raw, seq+"i") {
		t.Fatalf("char under the caret not drawn in the accent in place: %q", stripANSI(raw))
	}
	if strings.Contains(raw, m.th.Glyph.Cursor) {
		t.Fatalf("foreign block glyph still drawn: %q", stripANSI(raw))
	}
	if !strings.Contains(stripANSI(raw), "hi") {
		t.Fatalf("typed text mangled: %q", stripANSI(raw))
	}

	// A double-width char under the caret: accented like any other, and the
	// caret adds no cell of its own (the char keeps its two columns).
	in2 := newInputLine()
	in2.insertRune('世')
	in2.cur = 0
	wide := in2.render(m, 120)[0]
	if !strings.Contains(wide, seq+"世") {
		t.Fatalf("wide char under the caret not drawn in the accent: %q", stripANSI(wide))
	}
	// Measure without padding: at a width the content exactly fills, the
	// end-of-line underlined space must be exactly one added cell.
	over4 := in2.render(m, 4)[0]

	in2.cur = 1
	if !strings.Contains(over4, seq+"世") {
		t.Fatalf("wide char under the caret not drawn in the accent (tight width): %q", stripANSI(over4))
	}
	tail := in2.render(m, 120)[0]
	if !strings.Contains(tail, spaceCursorSeq(m.th)+" ") {
		t.Fatalf("underlined-space end caret missing: %q", stripANSI(tail))
	}
	tail4 := in2.render(m, 4)[0]
	// A full row: the end caret has no cell of its own (the added space
	// would overrun the frame and get clipped off) — the last char
	// carries the cursor style instead, so the row stays exactly full.
	if got, want := plainWidth(tail4), plainWidth(over4); got != want {
		t.Fatalf("full-row end-caret line width %d, want the over-char width %d (no added cell)", got, want)
	}
	if !strings.Contains(tail4, seq+"世") {
		t.Fatalf("full-row end caret must fall back to the last char: %q", stripANSI(tail4))
	}
}

// TestInputBarTransparentSurface verifies the transparent input surface:
// no cell of the input line — typed text, prompt, trailing padding —
// carries a painted background, so the whole bar rides the terminal's
// own background, and the caret is an accent underlined cell (no reverse
// video, no painted surface on it).
func TestInputBarTransparentSurface(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Setenv("NO_COLOR", "")
	t.Setenv("COLORFGBG", "15;0")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a := app.New(newFakeHost(t).URL)
	a.Start(ctx)

	m := NewModel(a)
	m.W, m.H = 120, 40
	for _, r := range "hello 世界" {
		m.inp.insertRune(r)
	}
	rows := m.inp.render(m, 120)
	for _, ln := range rows {
		if cells := sgrBakedBg(ln); len(cells) > 0 {
			t.Fatalf("cells with a painted background: %q (first at %d)", stripANSI(ln), cells[0])
		}
	}
	// The caret cell is the accent underline: no reverse video left on it.
	if strings.Contains(rows[0], "\x1b[7m") {
		t.Fatalf("caret back to reverse video: %q", stripANSI(rows[0]))
	}
	if !sgrHasParam(rows[0], "4") {
		t.Fatalf("caret lost its underline: %q", stripANSI(rows[0]))
	}
}

// TestInputBarSelectionUnderline pins the selection mark of the input
// line: picked chars keep their plain ink and gain an underline — no
// painted band behind them, no reverse-video fallback.
func TestInputBarSelectionUnderline(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Setenv("NO_COLOR", "")
	t.Setenv("COLORFGBG", "15;0")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a := app.New(newFakeHost(t).URL)
	a.Start(ctx)

	m := NewModel(a)
	m.W, m.H = 120, 40
	in := m.inp
	for _, r := range "hello world" {
		in.insertRune(r)
	}
	in.cur = 5 // the pick [0,5) covers "hello"
	in.sel.anchor = 0
	in.sel.on = true

	line := in.render(m, 120)[0]
	// No painted band behind the picked run (nor the caret's cell).
	if cells := sgrBakedBg(line); len(cells) > 0 {
		t.Fatalf("selection paints a background: %q (first at %d)", stripANSI(line), cells[0])
	}
	// No reverse-video fallback on any cell.
	if strings.Contains(line, "\x1b[7m") {
		t.Fatalf("selection fell back to reverse video: %q", stripANSI(line))
	}
	// The underline carries the pick (and the caret).
	if !sgrHasParam(line, "4") {
		t.Fatalf("selection lost its underline: %q", stripANSI(line))
	}
	if !strings.Contains(stripANSI(line), "hello world") {
		t.Fatalf("typed text mangled: %q", stripANSI(line))
	}
}

// sgrHasParam reports whether any SGR sequence in raw carries param as a
// standalone parameter (the underline's bare "4", the reverse's "7" —
// never a component inside a 38;2;r;g;b / 48;5;n payload).
func sgrHasParam(raw, param string) bool {
	for _, seq := range ansiRe.FindAllString(raw, -1) {
		body := strings.TrimSuffix(seq, seq[len(seq)-1:])
		body = strings.TrimPrefix(body, "\x1b[")
		for _, p := range strings.Split(body, ";") {
			if p == param {
				return true
			}
		}
	}
	return false
}

// sgrBakedBg reports the offsets of visible bytes drawn with an explicit
// background currently set (48;... or the 40-47 / 100-107 codes) — the
// painted-background cells, for surfaces that must stay transparent.
// Underline (4) and reverse video (7) are attributes, not painted
// backgrounds, so the caret's cells stay clean.
func sgrBakedBg(raw string) []int {
	var out []int
	bg := false
	i := 0
	for i < len(raw) {
		if raw[i] == '\x1b' && strings.HasPrefix(raw[i:], "\x1b[") {
			end := strings.IndexByte(raw[i+2:], 'm')
			if end < 0 {
				break
			}
			params := strings.Split(raw[i+2:i+2+end], ";")
			k := 0
			for k < len(params) {
				switch p := params[k]; {
				case p == "0":
					bg = false
					k++
				case p == "49":
					bg = false
					k++
				case p == "48":
					// 48;2;r;g;b or 48;5;n: skip the mode and its
					// components, then the background is explicitly set.
					if mode := paramsValue(params, k+1); mode == "2" {
						k += 5
					} else {
						k += 3
					}
					bg = true
				case p == "38" || p == "39":
					// Foreground selection: consume the mode and its
					// components, background untouched.
					if mode := paramsValue(params, k+1); mode == "2" {
						k += 5
					} else {
						k += 3
					}
				case p == "40" && p <= "47", p == "100" && p <= "107":
					bg = true
					k++
				default:
					// Other attributes (bold, italic, 30-37 fg, 90-97,
					// reverse...): no background change.
					k++
				}
			}
			i += end + 3
			continue
		}
		if bg && raw[i] != 0 {
			out = append(out, i)
		}
		i++
	}
	return out
}

// paramsValue returns params[i] or "" past the end.
func paramsValue(params []string, i int) string {
	if i >= len(params) {
		return ""
	}
	return params[i]
}

// colorSeq returns the SGR prefix the theme emits for one color
// (profile-independent, same technique as cursorSeq).
func colorSeq(th *Theme, c string) string {
	raw := th.fg(c).Render("x")
	if i := strings.IndexByte(raw, 'x'); i > 0 {
		return raw[:i]
	}
	return ""
}

// TestUserLinesVoiceColor pins the user-message contract at the render
// level: the prompt text is told apart from the model's voice by its own
// foreground color (no background SGR anywhere on the line), inline code
// inside a prompt keeps the code color, and every rendered line still
// pads to the full pane width.
func TestUserLinesVoiceColor(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Setenv("NO_COLOR", "")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a := app.New(newFakeHost(t).URL)
	a.Start(ctx)

	m := NewModel(a)
	m.W, m.H = 100, 10
	m.th.Profile = termenv.TrueColor // deterministic SGR bytes for assertions

	userSeq := colorSeq(m.th, m.th.CUser)
	codeSeq := colorSeq(m.th, m.th.CCode)
	if userSeq == "" || codeSeq == "" || userSeq == codeSeq {
		t.Fatalf("bad seqs: user=%q code=%q", userSeq, codeSeq)
	}
	check := func(what string, ln string) {
		t.Helper()
		if plainWidth(ln) != m.transcriptWidth() {
			t.Fatalf("%s width %d, want full pane width %d", what, plainWidth(ln), m.transcriptWidth())
		}
		if strings.Contains(ln, "\x1b[48;") || strings.Contains(ln, "\x1b[49m") {
			t.Fatalf("%s still carries a background SGR: %q", what, ln)
		}
		if !strings.Contains(ln, userSeq) {
			t.Fatalf("%s missing the user-voice color: %q", what, ln)
		}
	}

	it := &core.Item{Kind: core.KindUser, Text: "这是一条用户消息", Time: 1}
	lines := renderUser(m, it, m.transcriptWidth())
	if len(lines) != 1 {
		t.Fatalf("user message rendered %d lines, want 1", len(lines))
	}
	if !strings.Contains(stripANSI(lines[0]), "这是一条用户消息") {
		t.Fatalf("text missing from the line: %q", stripANSI(lines[0]))
	}
	check("user line", lines[0])

	// Optimistic echo line: same single-line, voice-color rendering (the
	// glyph is the state color, the text the voice color).
	echo := &core.Item{Kind: core.KindUser, Text: "这是一条用户消息", Time: 1, EchoRpcId: "rpc-x"}
	el := renderUser(m, echo, m.transcriptWidth())
	if len(el) != 1 {
		t.Fatalf("echo rendered %d lines, want 1", len(el))
	}
	check("echo line", el[0])

	// Markdown in the prompt: every rendered line stays exactly
	// full-width and carries the voice color with no background SGR;
	// inline code in the prompt wears the code color on top of the voice.
	mdText := "run `git status` then **commit** and see https://example.com/a"
	mdItem := &core.Item{Kind: core.KindUser, Text: mdText, Time: 1}
	mdLines := renderUser(m, mdItem, m.transcriptWidth())
	for i, ln := range mdLines {
		check(fmt.Sprintf("markdown line %d", i), ln)
	}
	if !strings.Contains(strings.Join(mdLines, ""), codeSeq) {
		t.Fatalf("inline code in the prompt missing the code color: %q", stripANSI(strings.Join(mdLines, " ")))
	}

	// Multi-line prompt: continuation lines carry the voice color too.
	multi := &core.Item{Kind: core.KindUser, Text: "first line\nsecond `code` line", Time: 1}
	for i, ln := range renderUser(m, multi, m.transcriptWidth()) {
		check(fmt.Sprintf("multiline line %d", i), ln)
	}

	// The model's voice stays the body color: an assistant text block must
	// not wear the user-voice color.
	asst := &core.Item{Kind: core.KindAssistant, Blocks: []core.ABlock{{Kind: "text", Text: "an answer"}}}
	al := renderAssistant(m, asst, m.transcriptWidth(), "agent")
	for _, ln := range al {
		if strings.Contains(ln, userSeq) {
			t.Fatalf("assistant line wears the user-voice color: %q", ln)
		}
	}
	if !strings.Contains(strings.Join(al, ""), colorSeq(m.th, m.th.CFG)) {
		t.Fatalf("assistant text missing the body color: %q", stripANSI(strings.Join(al, " ")))
	}
}

// TestInputHistory pins the prompt-history browse of the input box:
// up/recall saves the current draft, walks older entries (holding at the
// oldest), down steps newer and finally restores the draft with its
// caret; edits detach the browse; record dedupes and caps the list.
func TestInputHistory(t *testing.T) {
	in := newInputLine()
	// No history: the arrows are left for the caller (transcript scroll).
	if in.histUp() {
		t.Fatal("histUp must report false on an empty history")
	}
	if in.histDown() {
		t.Fatal("histDown must report false when not browsing")
	}
	// record: order, consecutive dedupe, cap.
	in.record("one")
	in.record("two")
	in.record("two")
	if len(in.hist) != 2 {
		t.Fatalf("history %q: consecutive duplicate not dropped", in.hist)
	}
	for i := 0; i < histCap; i++ {
		in.record(fmt.Sprintf("item-%d", i))
	}
	if len(in.hist) != histCap {
		t.Fatalf("history cap broken: %d entries, want %d", len(in.hist), histCap)
	}
	if in.hist[0] == "one" {
		t.Fatal("oldest entry must fall off at the cap")
	}
	in = newInputLine()
	in.record("old")
	in.record("new")

	// Draft with a mid-buffer caret starts the browse.
	for _, r := range "draft" {
		in.insertRune(r)
	}
	in.cur = 2
	if ok, send := in.handleKey(keyModel(t), tea.KeyMsg{Type: tea.KeyUp}); !ok || send {
		t.Fatalf("up with history: want consumed, got ok=%v send=%v", ok, send)
	}
	if got, want := in.value(), "new"; got != want {
		t.Fatalf("first up: value %q, want most recent %q", got, want)
	}
	if in.cur != len(in.val) {
		t.Fatalf("caret %d, want end of recalled entry", in.cur)
	}
	if ok, _ := in.handleKey(keyModel(t), tea.KeyMsg{Type: tea.KeyUp}); !ok {
		t.Fatal("second up: want consumed")
	}
	if in.value() != "old" {
		t.Fatalf("second up: value %q, want %q", in.value(), "old")
	}
	// At the oldest entry up holds its ground.
	in.handleKey(keyModel(t), tea.KeyMsg{Type: tea.KeyUp})
	if in.value() != "old" {
		t.Fatalf("up at oldest: value %q, want it to hold", in.value())
	}
	// Down steps back toward the draft and finally restores it.
	in.handleKey(keyModel(t), tea.KeyMsg{Type: tea.KeyDown})
	if in.value() != "new" {
		t.Fatalf("down: value %q, want %q", in.value(), "new")
	}
	if ok, _ := in.handleKey(keyModel(t), tea.KeyMsg{Type: tea.KeyDown}); !ok {
		t.Fatal("down out of the browse: want consumed")
	}
	if in.value() != "draft" || in.cur != 2 {
		t.Fatalf("browse ended: value %q caret %d, want the draft %q caret 2", in.value(), in.cur, "draft")
	}
	if ok, _ := in.handleKey(keyModel(t), tea.KeyMsg{Type: tea.KeyDown}); ok {
		t.Fatal("down without a browse must not be consumed")
	}

	// Any content edit abandons the browse and keeps the edited buffer.
	in.handleKey(keyModel(t), tea.KeyMsg{Type: tea.KeyUp}) // "new"
	in.insertRune('!')
	if in.value() != "new!" || in.histPos != -1 {
		t.Fatalf("edit must detach: value %q pos %d", in.value(), in.histPos)
	}
	in.handleKey(keyModel(t), tea.KeyMsg{Type: tea.KeyUp}) // "new" (most recent)
	in.delBack()
	if in.value() != "ne" || in.histPos != -1 {
		t.Fatalf("backspace must detach: value %q pos %d", in.value(), in.histPos)
	}
	// clear() also leaves the browse behind.
	in.histUp()
	in.clear()
	if in.histPos != -1 {
		t.Fatal("clear must abandon the browse")
	}

	// Multi-line entries recall verbatim.
	in.record("line one\nline two")
	in.histUp()
	if in.value() != "line one\nline two" {
		t.Fatalf("multiline recall: %q", in.value())
	}
}

// TestSubmitRecordsHistoryEndToEnd pins the model wiring: a submitted
// prompt lands in the input history, and up/down on the real key path
// recall it from an empty input (history takes the arrows before the
// transcript scroll) and put it back.
func TestSubmitRecordsHistoryEndToEnd(t *testing.T) {
	a := app.New("http://127.0.0.1:3999")
	m := NewModel(a)
	m.splashOff = true

	typePrompt := func(s string) {
		for _, r := range s {
			m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
	}
	typePrompt("recall me")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter}) // queue (no live host)
	if len(m.inp.hist) != 1 || m.inp.hist[0] != "recall me" {
		t.Fatalf("history after submit: %q", m.inp.hist)
	}
	if m.inp.value() != "" {
		t.Fatalf("input not cleared after submit: %q", m.inp.value())
	}
	// Up from the empty input recalls the prompt instead of scrolling.
	m.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	if got := m.inp.value(); got != "recall me" {
		t.Fatalf("up after submit: input %q, want the submitted prompt", got)
	}
	// Down hands the (empty) draft back.
	m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.inp.value() != "" {
		t.Fatalf("down: input %q, want the pre-browse buffer back", m.inp.value())
	}
	// Submitting the same prompt twice does not duplicate the entry.
	typePrompt("recall me")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.inp.hist) != 1 {
		t.Fatalf("history after re-submit: %q", m.inp.hist)
	}
}

// TestInputWrapCaretOnMultiRows pins the caret over wrapped text: a
// multi-byte (CJK) row must not swallow the later rows' caret positions
// (the row's extent is runes, not bytes), a wrapped row drops its
// break-point space (it must not overrun its width), and a caret parked
// in the dropped gap sits on the previous row's end.
func TestInputWrapCaretOnMultiRows(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	t.Setenv("NO_COLOR", "")

	a := app.New("http://127.0.0.1:3999")
	m := NewModel(a)
	m.splashOff = true
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	seq := cursorSeq(m.th)

	// A 10-wide bar: prompt 2 + usable 8 (the wrap floor keeps it at
	// 8). CJK chars are 2 cells, so four (8 cells) fill a row and the
	// fifth wraps. The caret must follow the text onto the second row.
	in := newInputLine()
	for _, r := range "中文测试中文" {
		in.insertRune(r)
	}
	in.cur = 5 // after "中文测试中": the caret sits on the second physical row
	rows := in.render(m, 10)
	if len(rows) != 2 {
		t.Fatalf("wrapped rows = %d, want 2: %q", len(rows), rows)
	}
	if !strings.Contains(rows[1], seq+"文") {
		t.Fatalf("caret lost on the wrapped CJK row: %q", stripANSI(rows[1]))
	}
	if strings.Contains(rows[0], seq) {
		t.Fatalf("first row swallowed the caret: %q", stripANSI(rows[0]))
	}
	// A caret parked at the first row's end: the row is full (8 cells),
	// so the end caret falls back to its last char (no added cell).
	in.cur = 4
	rows = in.render(m, 10)
	if !strings.Contains(rows[0], seq+"试") {
		t.Fatalf("full-row end caret must fall back to the last char: %q", stripANSI(rows[0]))
	}
	if strings.Contains(rows[0], spaceCursorSeq(m.th)+" ") {
		t.Fatalf("full row must not add the caret cell: %q", stripANSI(rows[0]))
	}

	// Break-point spaces: "abcdefgh  world" fills row one (8 cells),
	// wraps, and drops both break spaces — the wrapped row stays within
	// its width, and a caret parked in the dropped gap sits on the
	// previous row's end (the full row's end caret falls back to its
	// last char).
	in2 := newInputLine()
	for _, r := range "abcdefgh  world" {
		in2.insertRune(r)
	}
	in2.cur = 9 // between the dropped spaces
	rows = in2.render(m, 10)
	if len(rows) != 2 {
		t.Fatalf("spaced rows = %d, want 2: %q", len(rows), rows)
	}
	if got := strings.TrimSpace(stripANSI(rows[1])); got != "world" {
		t.Fatalf("wrapped row kept the break space: %q", got)
	}
	if !strings.Contains(rows[0], seq+"h") {
		t.Fatalf("gap caret must park on the previous row's end: %q", stripANSI(rows[0]))
	}
	if pw := plainWidth(rows[0]); pw != 10 {
		t.Fatalf("full row width %d, want exactly 10", pw)
	}

	// The caret at a full row's end takes the last char's cell (the
	// added space would be clipped by the frame): "abcdefgh" exactly
	// fills the 8-wide budget.
	in3 := newInputLine()
	for _, r := range "abcdefgh" {
		in3.insertRune(r)
	}
	in3.cur = 8
	row := in3.render(m, 10)[0]
	if !strings.Contains(row, seq+"h") {
		t.Fatalf("full-row end caret: want cursor over the last char: %q", stripANSI(row))
	}
	if pw := plainWidth(row); pw != 10 {
		t.Fatalf("full-row caret line width %d, want exactly 10", pw)
	}

	// A caret at the end of a row that is NOT full still gets its
	// underlined-space cell (the budget allows it).
	in4 := newInputLine()
	for _, r := range "ab" {
		in4.insertRune(r)
	}
	in4.cur = 2
	row = in4.render(m, 10)[0]
	if !strings.Contains(row, spaceCursorSeq(m.th)+" ") {
		t.Fatalf("non-full row-end caret lost its added cell: %q", stripANSI(row))
	}
}

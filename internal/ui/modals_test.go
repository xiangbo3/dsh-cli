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
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestHelpToggleCtrlH checks that ctrl+h opens the help and a second
// ctrl+h closes it (toggle) instead of stacking another help modal.
func TestHelpToggleCtrlH(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a := app.New("http://127.0.0.1:3080")
	a.Start(ctx)
	m := NewModel(a)
	m.W, m.H = 120, 40

	press := func() { m.Update(tea.KeyMsg{Type: tea.KeyCtrlH}) }
	press() // open
	if got := len(m.mods); got != 1 {
		t.Fatalf("after first ctrl+h: modals = %d (want 1)", got)
	}
	if _, ok := m.topModal().(*helpModal); !ok {
		t.Fatalf("top modal = %T (want *helpModal)", m.topModal())
	}
	press() // toggle closed, not stacked
	if got := len(m.mods); got != 0 {
		t.Fatalf("after second ctrl+h: modals = %d (want 0)", got)
	}
	press() // open again
	if got := len(m.mods); got != 1 {
		t.Fatalf("after third ctrl+h: modals = %d (want 1)", got)
	}
}

// TestModalBoxCardSurface verifies the modal popup box's opaque-card
// contract: the box is as wide as the slot on every line, and every
// cell the box paints carries a background — the card surface on the
// chrome and the blank cells, the row's own band where one exists — so
// spliceRow takes the box's cell as painted and nothing of the
// underlying screen shows through the box (the old floating look left
// the blank cells bare, the transcript showing through behind the
// glyphs; the old title line used to be measured with raw StringWidth,
// which counts every byte of the SGR codes: the trailing padding was
// then skipped and the title line ended short of w, exposing the
// terminal background after the title). The nil-body box is pure
// chrome: hairline border and title glyphs over the card surface.
func TestModalBoxCardSurface(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Setenv("NO_COLOR", "")
	t.Setenv("COLORFGBG", "15;0")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a := app.New("http://127.0.0.1:3080")
	a.Start(ctx)
	m := NewModel(a)
	m.W, m.H = 100, 30
	if m.th.CCardBG == "" {
		t.Skip("theme has no card surface in this profile")
	}

	const w = 80
	for title, body := range map[string][]string{
		"help":             (&helpModal{}).view(m, w-4, 16),
		"DeepSeek Harness": nil,
	} {
		lines := strings.Split(frame(m, title, "  esc close", body, w, 20), "\n")
		if len(lines) != 20 {
			t.Fatalf("%q: box emitted %d lines, want 20", title, len(lines))
		}
		if pw := plainWidth(lines[1]); pw != w {
			t.Fatalf("%q: title line visible width %d, want %d", title, pw, w)
		}
		// The opaque-card contract: every cell of the box carries a
		// background — the card surface on the chrome, the title row,
		// the padding and the blank tail, the row's own surface (search
		// line, cursor band) on the styled body lines. A background-less
		// cell is a hole the old floating look showed through.
		for i, ln := range lines {
			for _, c := range scanStyled(ln) {
				if len(c.state.bg) == 0 {
					t.Fatalf("%q: line %d col %d: transparent cell (want the card surface)", title, i, c.col)
				}
			}
		}
	}
}

// TestHelpBoxFillsSlot checks that frame() emits exactly as many lines
// as the box height it is given, and that every chrome line of the box
// is card-filled (the old floating look left the chrome blank cells
// bare: a short frame would leave a strip without card background and
// the transcript would show through — the help modal's "cracked"
// bottom).
func TestHelpBoxFillsSlot(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Setenv("NO_COLOR", "")
	t.Setenv("COLORFGBG", "15;0")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a := app.New("http://127.0.0.1:3080")
	a.Start(ctx)
	m := NewModel(a)
	if m.th.CCardBG == "" {
		t.Skip("theme has no card surface in this profile")
	}

	const w = 100
	body := (&helpModal{}).view(m, w-4, 16)
	for _, h := range []int{6, 7, 10, 20, 30, 35, 41, 42, 60} {
		bh := len(body) + 4
		if bh > h {
			bh = h
		}
		if bh < 6 {
			bh = 6
		}
		lines := strings.Split(frame(m, "help", "  hint", body, w, bh), "\n")
		if len(lines) != bh {
			t.Fatalf("h=%d: frame emitted %d lines, want %d (transparent crack)", h, len(lines), bh)
		}
		for i, ln := range lines {
			if pw := plainWidth(ln); pw != w {
				t.Fatalf("h=%d line %d: visible width %d, want %d", h, i, pw, w)
			}
			// Styled body lines (search line, cursor band) keep their own
			// surface; every chrome line's cell — glyph or blank — carries
			// the card surface.
			if i >= 2 && i < bh-2 {
				continue
			}
			for _, c := range scanStyled(ln) {
				if len(c.state.bg) == 0 {
					t.Fatalf("h=%d line %d col %d: transparent cell (want the card surface)", h, i, c.col)
				}
			}
		}
	}
}

// TestModalInputsMatchMainInput pins the modal text fields on the main
// input box's behavior and look: the inverse-video caret (an inverted char
// under the cursor, an inverted space at the line end) and the
// ctrl+a/e/b/f/d/u editor bindings.
func TestModalInputsMatchMainInput(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Setenv("NO_COLOR", "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a := app.New("http://127.0.0.1:3080")
	a.Start(ctx)
	m := NewModel(a)
	m.W, m.H = 120, 40

	seq := func(st lipgloss.Style) string {
		raw := st.Render("x")
		if i := strings.IndexByte(raw, 'x'); i > 0 {
			return raw[:i]
		}
		return ""
	}
	caretSeq := seq(m.th.CardCursor())
	if caretSeq == "" {
		t.Fatal("card cursor style rendered without an SGR prefix")
	}
	key := func(typ tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: typ} }

	// rename: type mid-line, then drive it with the main input's bindings.
	rn := &renameModal{edit: lineEdit{val: []rune("refactor"), cur: 3}}
	rn.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("foo")})
	if got := rn.edit.string(); got != "reffooactor" {
		t.Fatalf("rename mid-line insert = %q, want %q", got, "reffooactor")
	}
	rn.update(key(tea.KeyCtrlA)) // line start
	rn.update(key(tea.KeyCtrlF)) // step in
	rn.update(key(tea.KeyCtrlF)) // step in (caret before the second "f")
	rn.update(key(tea.KeyCtrlD)) // delete the char under the caret
	if got := rn.edit.string(); got != "refooactor" {
		t.Fatalf("rename ctrl+d = %q, want %q", got, "refooactor")
	}
	rn.update(key(tea.KeyCtrlE)) // line end
	rn.update(key(tea.KeyCtrlB)) // one back
	rn.update(key(tea.KeyCtrlU)) // kill to the end
	if got, want := rn.edit.string(), "refooacto"; got != want {
		t.Fatalf("rename ctrl+u = %q, want %q", got, want)
	}

	// rename view: an inverted cell, never the old block glyph.
	rn.edit.cur = len(rn.edit.val) // end of line
	if ln := rn.view(m, 0, 0)[0]; !strings.Contains(ln, caretSeq+" ") || strings.Contains(ln, m.th.Glyph.Cursor) {
		t.Fatalf("rename end caret: %q", stripANSI(ln))
	}
	rn.edit.cur = 0 // over the first char
	if ln := rn.view(m, 0, 0)[0]; !strings.Contains(ln, caretSeq+"r") {
		t.Fatalf("rename char under caret not inverted: %q", stripANSI(ln))
	}

	// search: the query field edits like the main input.
	sm := &searchModal{ed: lineEdit{val: []rune("git"), cur: 3}}
	sm.update(key(tea.KeyCtrlB)) // left
	sm.update(key(tea.KeyCtrlD)) // delete the "t"
	if got := sm.ed.string(); got != "gi" {
		t.Fatalf("search ctrl+d = %q, want %q", got, "gi")
	}
	sm.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hub")})
	if got := sm.ed.string(); got != "gihub" {
		t.Fatalf("search type = %q, want %q", got, "gihub")
	}
	sm.update(key(tea.KeyCtrlA))
	sm.update(key(tea.KeyCtrlU)) // kill the whole query
	if got := sm.ed.string(); got != "" {
		t.Fatalf("search ctrl+a+u = %q, want empty", got)
	}
	if ln := sm.view(m, 0, 0)[0]; !strings.Contains(ln, caretSeq+" ") {
		t.Fatalf("search empty query missing inverted space: %q", stripANSI(ln))
	}

	// workspace add: the path field shares the bindings and the caret style.
	wm := &workspaceModal{st: m.st, mode: wsAdd}
	wm.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/tmp/foo")})
	wm.update(key(tea.KeyCtrlB))
	wm.update(key(tea.KeyCtrlB))
	wm.update(key(tea.KeyCtrlD)) // deletes the "o"
	if got := wm.input.string(); got != "/tmp/fo" {
		t.Fatalf("workspace ctrl+d = %q, want %q", got, "/tmp/fo")
	}
	wm.update(key(tea.KeyCtrlE))
	if wm.input.cur != 7 {
		t.Fatalf("workspace ctrl+e: cur %d, want 7", wm.input.cur)
	}
	lines := wm.view(m, 100, 20)
	field := ""
	for _, ln := range lines {
		if strings.Contains(stripANSI(ln), "path >") {
			field = ln
			break
		}
	}
	if field == "" {
		t.Fatal("workspace view missing the path > field line")
	}
	if !strings.Contains(field, caretSeq) {
		t.Fatalf("workspace path field missing the inverted caret: %q", stripANSI(field))
	}

	// question: the "other:" field edits like the main input, and ctrl+a is
	// the line start, not the option select (select moved to space).
	pen := &core.QuestionPend{Questions: []protocol.QuestionItem{
		{Id: "q1", Question: "pick one", Options: []protocol.QuestionOption{{Label: "one"}}},
	}}
	qm := newQuestionModal(pen)
	qm.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ab")})
	qm.update(key(tea.KeyCtrlA))
	qm.update(key(tea.KeyCtrlD)) // delete the "a"
	if got := qm.cust[0].string(); got != "b" {
		t.Fatalf("question other-field ctrl+d = %q, want %q", got, "b")
	}
	if s, ok := qm.sel[0]; ok && len(s) > 0 {
		t.Fatalf("ctrl+a must not select an option: %v", s)
	}
	qm.update(tea.KeyMsg{Type: tea.KeySpace}) // option select lives here now
	if s, ok := qm.sel[0]; !ok || !s["one"] {
		t.Fatalf("space must select the option: %v", qm.sel[0])
	}
	otherLine := ""
	for _, ln := range qm.view(m, 100, 0) {
		if strings.Contains(stripANSI(ln), "other:") {
			otherLine = ln
			break
		}
	}
	if otherLine == "" {
		t.Fatal("question view missing the other: field line")
	}
	if !strings.Contains(otherLine, caretSeq+"b") {
		t.Fatalf("question other-field caret not inverted: %q", stripANSI(otherLine))
	}
}

// TestQuestionAnswersCanonicalEncoding pins the batch payload against
// the host's matchesQuestions gates: sessionId rides from the pending
// frame, selected never serializes as null, a free-text answer on a
// single-select question stands alone (it replaces the selection), on a
// multi-select it accompanies the selected labels, and enter confirms
// the highlighted option only when the "other:" field is empty.
func TestQuestionAnswersCanonicalEncoding(t *testing.T) {
	key := func(typ tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: typ} }
	runes := func(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

	pen := &core.QuestionPend{RpcId: "r1", SessionId: "s1", Questions: []protocol.QuestionItem{
		{Id: "q1", Question: "pick one", Options: []protocol.QuestionOption{{Label: "one"}, {Label: "two"}}},
		{Id: "q2", Question: "multi", MultiSelect: true, Options: []protocol.QuestionOption{{Label: "a"}, {Label: "b"}}},
		{Id: "q3", Question: "free text only"},
	}}
	qm := newQuestionModal(pen)

	// q1: space selects the highlighted option, no free text.
	qm.update(key(tea.KeySpace))
	// q2: toggle both options, then type in the "other:" field.
	qm.update(key(tea.KeyRight))
	qm.update(key(tea.KeySpace))
	qm.update(key(tea.KeyDown))
	qm.update(key(tea.KeySpace))
	qm.update(runes("both plus this"))
	// q3: free text only (the question carries no options).
	qm.update(key(tea.KeyRight))
	qm.update(runes("  just text  "))

	ans := qm.answers()
	if ans.SessionId != "s1" {
		t.Fatalf("sessionId = %q, want %q (from the pending frame)", ans.SessionId, "s1")
	}
	if got := ans.Answer.Answers; len(got) != 3 {
		t.Fatalf("answers = %d, want 3", len(got))
	}
	if got := ans.Answer.Answers[0]; len(got.Selected) != 1 || got.Selected[0] != "one" || got.Custom != "" {
		t.Fatalf("q1 = %+v, want selected [one] and no custom", got)
	}
	if got := ans.Answer.Answers[1]; len(got.Selected) != 2 || got.Selected[0] != "a" || got.Selected[1] != "b" || got.Custom != "both plus this" {
		t.Fatalf("q2 = %+v, want selected [a b] accompanied by the custom text", got)
	}
	if got := ans.Answer.Answers[2]; len(got.Selected) != 0 || got.Custom != "just text" {
		t.Fatalf("q3 = %+v, want an empty selected and trimmed custom", got)
	}
	raw, err := json.Marshal(ans)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"selected":[]`) {
		t.Fatalf("a no-option answer must serialize selected as [], not null: %s", raw)
	}

	// The "other" flow the user hit: select an option, then type free
	// text, then enter — the free text must stand alone and enter must
	// not keep the highlighted option selected.
	pen2 := &core.QuestionPend{RpcId: "r2", SessionId: "s2", Questions: []protocol.QuestionItem{
		{Id: "q1", Question: "pick one", Options: []protocol.QuestionOption{{Label: "one"}}},
	}}
	qm2 := newQuestionModal(pen2)
	qm2.update(key(tea.KeySpace)) // select "one"
	qm2.update(runes("my own answer"))
	qm2.update(key(tea.KeyEnter)) // last question: submit
	if !qm2.sub {
		t.Fatal("enter on the last question with free text must submit")
	}
	if got := qm2.answers().Answer.Answers[0]; len(got.Selected) != 0 || got.Custom != "my own answer" {
		t.Fatalf("other answer = %+v, want selected [] + custom", got)
	}

	// Enter confirms the highlighted option, replacing any earlier
	// selection (a single-select answer carries at most one label).
	pen3 := &core.QuestionPend{RpcId: "r3", SessionId: "s3", Questions: []protocol.QuestionItem{
		{Id: "q1", Question: "pick one", Options: []protocol.QuestionOption{{Label: "one"}, {Label: "two"}}},
	}}
	qm3 := newQuestionModal(pen3)
	qm3.update(key(tea.KeySpace)) // select "one"
	qm3.update(key(tea.KeyDown))  // highlight "two"
	qm3.update(key(tea.KeyEnter)) // confirm the highlight
	if got := qm3.answers().Answer.Answers[0]; len(got.Selected) != 1 || got.Selected[0] != "two" {
		t.Fatalf("enter confirmation = %+v, want exactly [two]", got)
	}
}

// TestQuestionModalSpaceMarksNotTab pins the popup's mark key: space
// toggles the highlighted option (mark / unmark on multi-select) while
// tab no longer owns the mark — a bare tab is dropped and a tab rune
// falls through to the "other:" field (a literal tab, like the main
// input's plain tab).
func TestQuestionModalSpaceMarksNotTab(t *testing.T) {
	pen := &core.QuestionPend{Questions: []protocol.QuestionItem{
		{Id: "q1", Question: "multi", MultiSelect: true, Options: []protocol.QuestionOption{{Label: "a"}, {Label: "b"}}},
	}}
	qm := newQuestionModal(pen)

	qm.update(tea.KeyMsg{Type: tea.KeySpace})
	if s := qm.sel[0]; !s["a"] {
		t.Fatalf("space must mark the highlighted option: %v", s)
	}
	qm.update(tea.KeyMsg{Type: tea.KeyDown})
	qm.update(tea.KeyMsg{Type: tea.KeyTab})
	if s := qm.sel[0]; s["b"] {
		t.Fatalf("tab must no longer mark the highlighted option: %v", s)
	}
	qm.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x\t")})
	if got := qm.cust[0].string(); got != "x\t" {
		t.Fatalf("tab rune in the other field = %q, want %q", got, "x\t")
	}
}

// TestHelpEntriesFitThePopupBox pins the help text against the popup
// width: every entry must render inside the box's inner minus the
// two-cell body margin (70 outer cap → 66 cells), or frame() truncates
// its tail. Ambiguous arrow glyphs count wide (worst-case terminal).
func TestHelpEntriesFitThePopupBox(t *testing.T) {
	for i, e := range helpEntries {
		if e == "" {
			continue
		}
		if w := plainWidth(e); w > 66 {
			t.Fatalf("entry %d is %d cells wide (> 66), the popup would clip it: %q", i, w, e)
		}
	}
}

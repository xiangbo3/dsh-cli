package ui

import (
	"testing"
	"time"

	"dsh-cli/internal/app"

	tea "github.com/charmbracelet/bubbletea"
)

// csiMsg mimics the v1 reader's unexported unknownCSISequenceMsg (a
// []byte-based message) to drive the structural CSI-u matcher.
type csiMsg []byte

// TestCopyPasteChords pins the explicit copy/paste chords (crush's
// ctrl+shift+c / ctrl+shift+v): the Alt-bit form (alacritty's ESC+ctrl-char,
// tmux's M-C-c/M-C-v re-encode) and the kitty CSI-u form both copy an
// active pick even with a non-empty input — where plain ctrl+c would
// clear the input — and both paste the clipboard.
func TestCopyPasteChords(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	m := NewModel(app.New("http://127.0.0.1:3999"))
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.trans.lines = []string{"alpha beta gamma", "delta epsilon"}
	m.transX, m.transY = 0, 2
	m.follow = false
	m.scroll = 0

	pick := func(x1, x2 int) {
		m.lastPressT = time.Time{}
		m.Update(tea.MouseMsg{X: x1, Y: 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
		m.Update(tea.MouseMsg{X: x2, Y: 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
		m.Update(tea.MouseMsg{X: x2, Y: 2, Button: tea.MouseButtonNone, Action: tea.MouseActionRelease})
	}

	// An active transcript pick and a non-empty draft: plain ctrl+c
	// would clear the draft, the explicit chord must copy instead.
	pick(0, 4)
	for _, r := range []rune{'d', 'r'} {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if !m.sel.active || m.inp.empty() {
		t.Fatalf("pick=%+v input empty=%v", m.sel, m.inp.empty())
	}

	// (1) ctrl+shift+c (Alt bit) copies the pick and keeps the draft.
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC, Alt: true}); cmd == nil {
		t.Fatal("ctrl+shift+c must copy the pick even with a non-empty input")
	}
	if m.inp.empty() {
		t.Fatal("ctrl+shift+c must not clear the draft (plain ctrl+c's job)")
	}

	// (2) With nothing picked, the chord copies nothing and clears
	// nothing (no quit).
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.sel.active {
		t.Fatal("esc must clear the pick")
	}
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC, Alt: true}); cmd != nil {
		t.Fatal("ctrl+shift+c with nothing picked must not copy")
	}
	if m.inp.empty() || m.quitting {
		t.Fatal("ctrl+shift+c with nothing picked must not clear the input or quit")
	}

	// (3) ctrl+shift+v pastes the clipboard (the plain ctrl+v and the
	// shift chord resolve to the same binding).
	if findClipReadTool().path != "" {
		if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlV, Alt: true}); cmd == nil {
			t.Fatal("ctrl+shift+v must read the clipboard")
		}
	}

	// (4) The kitty CSI-u forms route through Update's default case.
	pick(0, 4)
	if got := m.selectionText(); got != "alpha" {
		t.Fatalf("pick text: %q", got)
	}
	if _, cmd := m.Update(csiMsg("\x1b[99;3u")); cmd == nil {
		t.Fatal("CSI-u ctrl+shift+c must copy the pick")
	}
	if got := m.selectionText(); got != "alpha" {
		t.Fatalf("CSI-u copy must not disturb the pick: %q", got)
	}
	if findClipReadTool().path != "" {
		if _, cmd := m.Update(csiMsg("\x1b[118;3u")); cmd == nil {
			t.Fatal("CSI-u ctrl+shift+v must read the clipboard")
		}
	}
}

// TestCsiuChord pins the CSI-u chord parser: only 'c'/'v' with the ctrl
// modifier bit (2) are the copy/paste chords; shift alone is a plain
// letter, alt is a different key, and non-u CSIs never match.
func TestCsiuChord(t *testing.T) {
	cases := []struct {
		seq   string
		cop   bool
		paste bool
	}{
		{"\x1b[99;3u", true, false},  // ctrl+shift+c
		{"\x1b[118;3u", false, true}, // ctrl+shift+v
		{"\x1b[99;2u", true, false},  // ctrl+c (a terminal that CSI-u's it)
		{"\x1b[118;2u", false, true}, // ctrl+v
		{"\x1b[99;19u", true, false}, // ctrl+shift+capslock
		{"\x1b[99;1u", false, false}, // shift+c = a plain 'c'
		{"\x1b[97;3u", false, false}, // ctrl+shift+a
		{"\x1b[99;4u", false, false}, // alt+c
		{"\x1b[99u", false, false},   // no modifier field
		{"\x1b[99;3A", false, false}, // arrow-key encoding
		{"\x1b[1;6A", false, false},  // legacy modified arrow
	}
	for _, tc := range cases {
		cop, paste := csiuChord([]byte(tc.seq))
		if cop != tc.cop || paste != tc.paste {
			t.Errorf("csiuChord(%q) = (%v, %v), want (%v, %v)",
				tc.seq, cop, paste, tc.cop, tc.paste)
		}
	}
}

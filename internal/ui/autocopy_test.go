// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"reflect"
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

	// (4) The kitty CSI-u forms route through Update's default case —
	// the bytes tmux re-encodes the chord to (MOK2 modifier values,
	// measured against tmux 3.7c with the omarchy config).
	pick(0, 4)
	if got := m.selectionText(); got != "alpha" {
		t.Fatalf("pick text: %q", got)
	}
	if _, cmd := m.Update(csiMsg("\x1b[99;6u")); cmd == nil {
		t.Fatal("CSI-u ctrl+shift+c must copy the pick")
	}
	if got := m.selectionText(); got != "alpha" {
		t.Fatalf("CSI-u copy must not disturb the pick: %q", got)
	}
	if findClipReadTool().path != "" {
		if _, cmd := m.Update(csiMsg("\x1b[118;6u")); cmd == nil {
			t.Fatal("CSI-u ctrl+shift+v must read the clipboard")
		}
	}

	// (5) modifyOtherKeys2 forms (terminals honouring the mode dsh-cli
	// requests, e.g. foot/xterm/kitty): decode to the same chords and
	// leave the pick and the draft intact.
	pick(0, 4)
	if got := m.selectionText(); got != "alpha" {
		t.Fatalf("pick text: %q", got)
	}
	if _, cmd := m.Update(csiMsg("\x1b[27;6;99~")); cmd == nil {
		t.Fatal("MOK2 ctrl+shift+c must copy the pick")
	}
	if got := m.selectionText(); got != "alpha" {
		t.Fatalf("MOK2 copy must not disturb the pick: %q", got)
	}
	if m.inp.empty() {
		t.Fatal("MOK2 ctrl+shift+c must not clear the draft")
	}
	if findClipReadTool().path != "" {
		if _, cmd := m.Update(csiMsg("\x1b[27;6;118~")); cmd == nil {
			t.Fatal("MOK2 ctrl+shift+v must read the clipboard")
		}
	}
}

// TestMok2Key pins the modifyOtherKeys2 decoder: the copy/paste chords,
// ctrl+letter as its KeyCtrl form, the legacy-byte equivalents for
// modified digits/specials, alt letters, super stripping — and the
// shapes that are not MOK2 at all.
func TestMok2Key(t *testing.T) {
	cases := []struct {
		seq  string
		want tea.KeyMsg
		ok   bool
	}{
		{"\x1b[27;6;99~", tea.KeyMsg{Type: tea.KeyCtrlC, Alt: true}, true},  // ctrl+shift+c
		{"\x1b[27;6;118~", tea.KeyMsg{Type: tea.KeyCtrlV, Alt: true}, true}, // ctrl+shift+v
		{"\x1b[27;5;99~", tea.KeyMsg{Type: tea.KeyCtrlC}, true},             // ctrl+c
		{"\x1b[27;5;97~", tea.KeyMsg{Type: tea.KeyCtrlA}, true},
		{"\x1b[27;5;113~", tea.KeyMsg{Type: tea.KeyCtrlQ}, true},
		{"\x1b[27;7;99~", tea.KeyMsg{Type: tea.KeyCtrlC, Alt: true}, true},  // alt+ctrl+c
		{"\x1b[27;8;99~", tea.KeyMsg{Type: tea.KeyCtrlC, Alt: true}, true},  // all mods
		{"\x1b[27;14;99~", tea.KeyMsg{Type: tea.KeyCtrlC, Alt: true}, true}, // +super: stripped
		{"\x1b[27;3;99~", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}, Alt: true}, true},
		{"\x1b[27;4;99~", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'C'}, Alt: true}, true},
		{"\x1b[27;2;99~", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'C'}}, true},
		{"\x1b[27;2;9~", tea.KeyMsg{Type: tea.KeyShiftTab}, true},
		{"\x1b[27;3;9~", tea.KeyMsg{Type: tea.KeyTab, Alt: true}, true},
		{"\x1b[27;5;9~", tea.KeyMsg{Type: tea.KeyTab}, true},    // ctrl+tab: legacy HT
		{"\x1b[27;5;13~", tea.KeyMsg{Type: tea.KeyEnter}, true}, // ctrl+enter: legacy CR
		{"\x1b[27;3;13~", tea.KeyMsg{Type: tea.KeyEnter, Alt: true}, true},
		{"\x1b[27;6;13~", tea.KeyMsg{Type: tea.KeyEnter, Alt: true}, true},
		{"\x1b[27;3;27~", tea.KeyMsg{Type: tea.KeyEsc, Alt: true}, true},
		{"\x1b[27;5;56~", tea.KeyMsg{Type: tea.KeyBackspace}, true}, // ctrl+8
		{"\x1b[27;5;57~", tea.KeyMsg{Type: tea.KeyTab}, true},       // ctrl+9
		{"\x1b[27;5;48~", tea.KeyMsg{Type: tea.KeyCtrlAt}, true},    // ctrl+0
		{"\x1b[27;5;49~", tea.KeyMsg{Type: tea.KeyCtrlA}, true},     // ctrl+1
		{"\x1b[27;3;49~", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}, Alt: true}, true},
		{"\x1b[27;2;13~", tea.KeyMsg{}, false},    // shift+enter: mok2ShiftEnter's
		{"\x1b[27;5;65329~", tea.KeyMsg{}, false}, // ctrl+left keysym: swallow
		{"\x1b[99;3u", tea.KeyMsg{}, false},       // kitty form
		{"\x1b[27;5;99", tea.KeyMsg{}, false},     // missing terminator
		{"\x1b[1;6D", tea.KeyMsg{}, false},        // legacy modified arrow
	}
	for _, tc := range cases {
		got, ok := mok2Key([]byte(tc.seq))
		if ok != tc.ok || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("mok2Key(%q) = (%+v, %v), want (%+v, %v)",
				tc.seq, got, ok, tc.want, tc.ok)
		}
	}
	if !mok2ShiftEnter([]byte("\x1b[27;2;13~")) {
		t.Error("mok2ShiftEnter must match the MOK2 shift+enter")
	}
	if mok2ShiftEnter([]byte("\x1b[27;5;13~")) || mok2ShiftEnter([]byte("\x1b[13;2u")) {
		t.Error("mok2ShiftEnter over-matches")
	}
}

// TestCsiuKey pins the kitty CSI-u decoder — the form tmux re-encodes
// every modified key to once it knows the client honours extended keys:
// the same (mods, sym) mapping as TestMok2Key (the copy/paste chords,
// ctrl/alt letters, the legacy-byte equivalents, super stripping) plus
// the arrows and friends the kitty terminals send, and the shapes that
// are not CSI-u at all.
func TestCsiuKey(t *testing.T) {
	cases := []struct {
		seq  string
		want tea.KeyMsg
		ok   bool
	}{
		{"\x1b[99;6u", tea.KeyMsg{Type: tea.KeyCtrlC, Alt: true}, true},  // ctrl+shift+c
		{"\x1b[118;6u", tea.KeyMsg{Type: tea.KeyCtrlV, Alt: true}, true}, // ctrl+shift+v
		{"\x1b[99;5u", tea.KeyMsg{Type: tea.KeyCtrlC}, true},             // ctrl+c
		{"\x1b[97;5u", tea.KeyMsg{Type: tea.KeyCtrlA}, true},
		{"\x1b[116;5u", tea.KeyMsg{Type: tea.KeyCtrlT}, true},            // ctrl+t (dock)
		{"\x1b[104;5u", tea.KeyMsg{Type: tea.KeyCtrlH}, true},            // ctrl+h (help)
		{"\x1b[99;7u", tea.KeyMsg{Type: tea.KeyCtrlC, Alt: true}, true},  // alt+ctrl+c
		{"\x1b[99;14u", tea.KeyMsg{Type: tea.KeyCtrlC, Alt: true}, true}, // +super: stripped
		{"\x1b[99;3u", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}, Alt: true}, true},
		{"\x1b[99;4u", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'C'}, Alt: true}, true},
		{"\x1b[99;2u", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'C'}}, true},
		{"\x1b[9;2u", tea.KeyMsg{Type: tea.KeyShiftTab}, true},
		{"\x1b[13;5u", tea.KeyMsg{Type: tea.KeyEnter}, true},            // ctrl+enter: legacy CR
		{"\x1b[13;3u", tea.KeyMsg{Type: tea.KeyEnter, Alt: true}, true}, // alt+enter: queue
		{"\x1b[13;6u", tea.KeyMsg{Type: tea.KeyEnter, Alt: true}, true}, // ctrl+shift+enter
		{"\x1b[13;2u", tea.KeyMsg{}, false},                             // shift+enter: csiuShiftEnter's
		{"\x1b[56;5u", tea.KeyMsg{Type: tea.KeyBackspace}, true},        // ctrl+8
		{"\x1b[57;5u", tea.KeyMsg{Type: tea.KeyTab}, true},              // ctrl+9
		{"\x1b[48;5u", tea.KeyMsg{Type: tea.KeyCtrlAt}, true},           // ctrl+0
		{"\x1b[1;2u", tea.KeyMsg{Type: tea.KeyShiftUp}, true},
		{"\x1b[4;5u", tea.KeyMsg{Type: tea.KeyCtrlLeft}, true},
		{"\x1b[5;1u", tea.KeyMsg{Type: tea.KeyPgUp}, true},
		{"\x1b[7;2u", tea.KeyMsg{Type: tea.KeyHome}, true},
		{"\x1b[10;5u", tea.KeyMsg{Type: tea.KeyDelete}, true},
		{"\x1b[99u", tea.KeyMsg{}, false},      // no modifier field
		{"\x1b[99;3A", tea.KeyMsg{}, false},    // legacy modified arrow
		{"\x1b[1;6A", tea.KeyMsg{}, false},     // legacy modified arrow
		{"\x1b[27;5;99~", tea.KeyMsg{}, false}, // the MOK2 form
		{"\x1b[5;99~", tea.KeyMsg{}, false},    // ~ terminator, not u
	}
	for _, tc := range cases {
		got, ok := csiuKey([]byte(tc.seq))
		if ok != tc.ok || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("csiuKey(%q) = (%+v, %v), want (%+v, %v)",
				tc.seq, got, ok, tc.want, tc.ok)
		}
	}
	if !csiuShiftEnter([]byte("\x1b[13;2u")) {
		t.Error("csiuShiftEnter must match the kitty shift+enter")
	}
	if csiuShiftEnter([]byte("\x1b[13;3u")) || csiuShiftEnter([]byte("\x1b[27;2;13~")) {
		t.Error("csiuShiftEnter over-matches (alt+enter is the queue chord)")
	}
}

// TestTmuxCsiuFlow is the tmux regression: with extended keys on and
// the csi-u format (the omarchy default), tmux re-encodes every
// modified key as kitty CSI-u once it learns the client honours the
// MOK2 mode dsh-cli requests. The sequences below are the exact bytes
// the app then receives (measured against tmux 3.7c in that setup);
// each must drive the key it names instead of dying as an unknown CSI.
func TestTmuxCsiuFlow(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	m := NewModel(app.New("http://127.0.0.1:3999"))
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// ctrl+t (ESC[116;5u) toggles the dock.
	m.Update(csiMsg("\x1b[116;5u"))
	if !m.dockVisible {
		t.Fatal("tmux CSI-u ctrl+t must open the dock")
	}
	m.Update(csiMsg("\x1b[116;5u"))
	if m.dockVisible {
		t.Fatal("tmux CSI-u ctrl+t must close the dock")
	}

	// ctrl+a (ESC[97;5u) homes the caret in a non-empty input.
	for _, r := range []rune{'a', 'b', 'c'} {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m.Update(csiMsg("\x1b[97;5u"))
	if m.inp.cur != 0 {
		t.Fatalf("tmux CSI-u ctrl+a must home the caret: cur=%d", m.inp.cur)
	}

	// plain ctrl+c (ESC[99;5u) clears the non-empty input.
	m.Update(csiMsg("\x1b[99;5u"))
	if !m.inp.empty() {
		t.Fatal("tmux CSI-u ctrl+c must clear the input")
	}

	// alt+enter (ESC[13;3u) arms the queued send; a plain enter submits.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m.Update(csiMsg("\x1b[13;3u"))
	if !m.inp.forceQueue {
		t.Fatal("tmux CSI-u alt+enter must arm the queued send")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.inp.empty() {
		t.Fatal("the queued send must submit and clear the input")
	}

	// shift+enter (ESC[13;2u) inserts a newline, never sends.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m.Update(csiMsg("\x1b[13;2u"))
	if got := m.inp.value(); got != "x\n" {
		t.Fatalf("tmux CSI-u shift+enter must insert a newline: %q", got)
	}

	// ctrl+shift+c (ESC[99;6u) copies an active pick — crush's chord.
	m.trans.lines = []string{"alpha beta gamma", "delta epsilon"}
	m.transX, m.transY = 0, 2
	m.follow = false
	m.scroll = 0
	m.lastPressT = time.Time{}
	m.Update(tea.MouseMsg{X: 0, Y: 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m.Update(tea.MouseMsg{X: 4, Y: 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	m.Update(tea.MouseMsg{X: 4, Y: 2, Button: tea.MouseButtonNone, Action: tea.MouseActionRelease})
	if !m.sel.active {
		t.Fatalf("pick must be active: %+v", m.sel)
	}
	m.sel.copied = false // the release auto-copy already fired; test the chord
	if _, cmd := m.Update(csiMsg("\x1b[99;6u")); cmd == nil {
		t.Fatal("tmux CSI-u ctrl+shift+c must copy the pick")
	}
	if !m.sel.copied {
		t.Fatal("tmux CSI-u ctrl+shift+c must mark the pick copied")
	}
}

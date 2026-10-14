// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"io"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestKeyModesRequestKitty pins the boot request: modifyOtherKeys2 alone
// leaves the kitty-protocol terminals (kitty, wezterm, ghostty, foot,
// iTerm2) answering a bare CR for shift+enter — the same bytes as enter,
// so the line sends instead of breaking. Both protocols are asked for,
// and both are dropped again at exit.
func TestKeyModesRequestKitty(t *testing.T) {
	for _, want := range []string{"\x1b[>4;2m", "\x1b[>1u"} {
		if !strings.Contains(KeyModesOn, want) {
			t.Errorf("boot must request %q: %q", want, KeyModesOn)
		}
	}
	for _, want := range []string{"\x1b[>4m", "\x1b[<1u"} {
		if !strings.Contains(KeyModesOff, want) {
			t.Errorf("exit must drop %q: %q", want, KeyModesOff)
		}
	}
}

// TestInitWritesKeyModes pins the write itself: Init must put the request
// on the tty (a constant nobody writes fixes nothing). The program is
// never Run, only the nil-guarded boot write is exercised.
func TestInitWritesKeyModes(t *testing.T) {
	pipeR, pipeW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = pipeW
	m := keyModel(t)
	m.prog = tea.NewProgram(m, tea.WithOutput(io.Discard))
	m.Init()
	os.Stdout = old
	pipeW.Close()
	defer pipeR.Close()
	out, err := io.ReadAll(pipeR)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), KeyModesOn) {
		t.Fatalf("Init must write the extended-key request %q, wrote %q", KeyModesOn, string(out))
	}
}

// TestNewlineWireForms pins both newline keys through Update: shift+enter
// on the wire (the kitty CSI-u form the disambiguate flag enabled at boot
// buys, and the modifyOtherKeys2 form) and ctrl+j (the bare LF every
// terminal sends, the newline where shift+enter cannot be told from
// enter). None of them sends.
func TestNewlineWireForms(t *testing.T) {
	// shift+enter arrives as an unnamed CSI sequence; ctrl+j arrives as a
	// named key (the reader has a byte for it), so the two take different
	// doors into the same newline.
	cases := []struct {
		name string
		msg  tea.Msg
	}{
		{"shift+enter (kitty CSI-u)", csiMsg("\x1b[13;2u")},
		{"shift+enter (modifyOtherKeys2)", csiMsg("\x1b[27;2;13~")},
		{"ctrl+j (bare LF)", tea.KeyMsg{Type: tea.KeyCtrlJ}},
	}
	for _, c := range cases {
		m := keyModel(t)
		for _, r := range "ab" {
			m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
		m.Update(c.msg)
		if got := m.inp.value(); got != "ab\n" {
			t.Errorf("%s: val=%q, want the newline, not a send", c.name, got)
		}
	}
}

// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"dsh-cli/internal/app"
	"dsh-cli/internal/protocol"

	"github.com/charmbracelet/bubbletea"
)

// TestClipboardNoTerminalPark pins crush's clipboard pattern: the copy
// (ctrl+shift+c) and the paste (ctrl+shift+v) run their clipboard tool
// in the command's own goroutine instead of parking the terminal in
// tea.Exec — and bubbletea v1's terminal release disables mouse cell
// motion without restoring it, so a parked copy would hand selection
// drags to the terminal's native highlight (its own bright color) and
// blind the in-app pick. The captured frame stream must therefore show
// the mouse enabled once (startup) and disabled only once (shutdown).
func TestClipboardNoTerminalPark(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	fh := newFakeHost(t)
	fh.sessions = []protocol.SessionSummary{{SessionId: "s1"}} // a row to boot: no auto-create mid-keystream
	a := app.New(fh.URL)
	a.Start(ctx)
	m := NewModel(a)
	m.splashOff = true // headless key test: no boot animation

	var out bytes.Buffer
	prog := tea.NewProgram(m,
		tea.WithInput(strings.NewReader("")),
		tea.WithOutput(&out),
		tea.WithMouseCellMotion(),
		tea.WithContext(ctx),
	)
	m.SetProg(prog)

	go func() {
		prog.Send(tea.WindowSizeMsg{Width: 120, Height: 40})
		time.Sleep(600 * time.Millisecond) // boot baseline + first frames
		// Copy: type "hello", select the line, ctrl+shift+c (the v1
		// reader reports the shift chord as ctrl+c with the Alt bit).
		for _, r := range "hello" {
			prog.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
			time.Sleep(20 * time.Millisecond)
		}
		prog.Send(tea.KeyMsg{Type: tea.KeyShiftHome})
		time.Sleep(50 * time.Millisecond)
		prog.Send(tea.KeyMsg{Type: tea.KeyCtrlC, Alt: true})
		time.Sleep(600 * time.Millisecond) // copy settles
		// Paste: ctrl+shift+v (reports as plain ctrl+v) reads the tool.
		prog.Send(tea.KeyMsg{Type: tea.KeyCtrlV, Alt: true})
		time.Sleep(600 * time.Millisecond) // paste settles
		// Clear whatever the paste left (quit needs an empty input):
		// home, then kill to the line end.
		prog.Send(tea.KeyMsg{Type: tea.KeyCtrlA})
		time.Sleep(50 * time.Millisecond)
		prog.Send(tea.KeyMsg{Type: tea.KeyCtrlU})
		time.Sleep(50 * time.Millisecond)
		prog.Send(tea.KeyMsg{Type: tea.KeyCtrlQ})
	}()

	if _, err := prog.Run(); err != nil {
		t.Fatalf("run: %v (stream tail: %s)", err, tail(out.String()))
	}

	s := out.String()
	on, off := "?1002h", "?1002l" // mouse cell motion on/off
	if got := strings.Count(s, on); got != 1 {
		t.Fatalf("mouse cell motion enabled %d times (want 1, the startup):\n%s", got, tail(s))
	}
	if got := strings.Count(s, off); got != 1 {
		t.Fatalf("mouse cell motion disabled %d times (want 1, the shutdown — a clipboard exec parked the terminal):\n%s",
			got, tail(s))
	}
}

// tail is the last chunk of a captured frame stream (test failure context).
func tail(s string) string {
	if len(s) > 400 {
		return "…" + s[len(s)-400:]
	}
	return s
}

package ui

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"dsh-cli/internal/app"

	"github.com/charmbracelet/bubbletea"
)

// TestTUISmokeHeadless runs the whole TUI headless (no renderer) against a
// live-or-dead server, feeding keys and exercising render paths.
func TestTUISmokeHeadless(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	a := app.New("http://127.0.0.1:3080")
	a.Start(ctx)

	m := NewModel(a)
	m.splashOff = true // headless key test: no boot animation
	in := strings.NewReader("")
	var out bytes.Buffer
	prog := tea.NewProgram(m,
		tea.WithInput(in),
		tea.WithOutput(&out),
		tea.WithoutRenderer(),
		tea.WithContext(ctx),
	)
	m.SetProg(prog)

	go func() {
		prog.Send(tea.WindowSizeMsg{Width: 120, Height: 40})
		time.Sleep(150 * time.Millisecond)
		// Type a slash command, open help, toggle list/dock, press keys.
		for _, r := range "/help" {
			prog.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
		time.Sleep(100 * time.Millisecond)
		prog.Send(tea.KeyMsg{Type: tea.KeyEnter}) // opens the help modal
		time.Sleep(100 * time.Millisecond)
		prog.Send(tea.KeyMsg{Type: tea.KeyEsc}) // close it: later keys are global
		time.Sleep(100 * time.Millisecond)
		prog.Send(tea.KeyMsg{Type: tea.KeyCtrlS}) // session window (toggles shut on the second press)
		time.Sleep(50 * time.Millisecond)
		prog.Send(tea.KeyMsg{Type: tea.KeyCtrlB})
		time.Sleep(50 * time.Millisecond)
		prog.Send(tea.KeyMsg{Type: tea.KeyUp})
		time.Sleep(50 * time.Millisecond)
		prog.Send(tea.KeyMsg{Type: tea.KeyDown})
		time.Sleep(100 * time.Millisecond)
		prog.Send(tea.KeyMsg{Type: tea.KeyCtrlP}) // permission cycle
		time.Sleep(50 * time.Millisecond)
		prog.Send(tea.KeyMsg{Type: tea.KeyEsc}) // close the session window (empty query)
		time.Sleep(150 * time.Millisecond)
		prog.Send(tea.KeyMsg{Type: tea.KeyCtrlD})
	}()

	if _, err := prog.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	// The model should have rendered at least one frame at some point.
	if m.W == 0 || m.H == 0 {
		t.Fatal("window size message was not processed")
	}
}

// TestInputSpaceAndQuit checks the space bar reaches the input line (it
// arrives as tea.KeySpace, not a rune) and that ctrl+q quits on empty
// input. The first chars are plain runes, so they must reach the input
// line rather than any global binding.
func TestInputSpaceAndQuit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	a := app.New("http://127.0.0.1:3080")
	a.Start(ctx)
	m := NewModel(a)
	m.splashOff = true // headless key test: no boot animation
	var out bytes.Buffer
	prog := tea.NewProgram(m,
		tea.WithInput(strings.NewReader("")),
		tea.WithOutput(&out),
		tea.WithoutRenderer(),
		tea.WithContext(ctx),
	)
	m.SetProg(prog)

	go func() {
		prog.Send(tea.WindowSizeMsg{Width: 120, Height: 40})
		time.Sleep(100 * time.Millisecond)
		sendRune := func(r rune) {
			prog.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
			time.Sleep(30 * time.Millisecond)
		}
		// Probe the input line on the event loop (happens-before edge):
		// reading m.inp from this goroutine would race the key handlers.
		probe := func() string {
			r := make(chan string, 1)
			prog.Send(inputProbeMsg{reply: r})
			return <-r
		}
		sendRune('z')
		prog.Send(tea.KeyMsg{Type: tea.KeySpace})
		time.Sleep(30 * time.Millisecond)
		sendRune('i')
		if got := probe(); got != "z i" {
			t.Errorf("input = %q, want %q", got, "z i")
		}
		time.Sleep(50 * time.Millisecond)
		// ctrl+c clears a non-empty input and quits when it is empty, on a
		// path that does not depend on live session state (esc instead
		// interrupts a running queued session and leaves the text).
		prog.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
		time.Sleep(50 * time.Millisecond)
		if got := probe(); got != "" {
			t.Errorf("clear failed: input = %q", got)
		}
		prog.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	}()

	if _, err := prog.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !m.quitting {
		t.Fatal("ctrl+q on empty input should quit")
	}
}

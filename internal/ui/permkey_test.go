package ui

import (
	"context"
	"testing"
	"time"

	"dsh-cli/internal/app"
	"dsh-cli/internal/protocol"

	tea "github.com/charmbracelet/bubbletea"
)

// newPermTestModel builds a model with one active session whose
// permissions projection is not baselined yet (the cycle therefore takes
// the tail-baseline path).
func newPermTestModel(t *testing.T) *Model {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a := app.New("http://127.0.0.1:1")
	a.Start(ctx)
	m := NewModel(a)
	m.W, m.H = 120, 40
	m.st.SetSessions([]protocol.SessionSummary{{SessionId: "s1", Cwd: "/tmp"}})
	m.st.SetActive("s1")
	return m
}

func keyCtrlP() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyCtrlP} }

func TestCtrlPCycleRequiresEmptyInput(t *testing.T) {
	m := newPermTestModel(t)
	// Non-empty input: the stray ctrl+p stays with the text — no
	// permission cycle is issued (a terminal keymap, a held-key
	// auto-repeat or a pasted 0x10 byte must not flip the sandbox
	// preset and stack "permission → …" toasts).
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if _, cmd := m.Update(keyCtrlP()); cmd != nil {
		t.Fatalf("ctrl+p with non-empty input issued a cycle")
	}
	// Empty input: the chord cycles (the fresh session has no permissions
	// projection, so the tail-baseline path is taken).
	m.Update(tea.KeyMsg{Type: tea.KeyEsc}) // clears the input
	if _, cmd := m.Update(keyCtrlP()); cmd == nil {
		t.Fatal("ctrl+p with empty input must issue the cycle")
	}
}

func TestCtrlPBaselineNoStack(t *testing.T) {
	m := newPermTestModel(t)
	// First press queues the tail-baseline pull.
	if _, cmd := m.Update(keyCtrlP()); cmd == nil {
		t.Fatal("first press must queue the baseline cycle")
	}
	// A rapid second press (baseline still in flight) must not stack a
	// second baseline + a second cycle.
	if _, cmd := m.Update(keyCtrlP()); cmd != nil {
		t.Fatal("second press while the baseline is in flight issued another cycle")
	}
	// The queued baseline landing resumes exactly one cycle …
	if _, cmd := m.Update(permBaselineMsg{id: "s1"}); cmd == nil {
		t.Fatal("the baseline landing must resume the cycle")
	}
	// … and the next press starts a fresh baseline again.
	if _, cmd := m.Update(keyCtrlP()); cmd == nil {
		t.Fatal("a press after the baseline landed must queue a new baseline")
	}
}

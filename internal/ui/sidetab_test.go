// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"testing"

	"dsh-cli/internal/app"

	tea "github.com/charmbracelet/bubbletea"
)

// TestSideVisibleKeepsTabChords pins ctrl+1..5 while the right-side window
// is open: the window keeps every other key, the tab jumps still land.
func TestSideVisibleKeepsTabChords(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	m := NewModel(app.New("http://127.0.0.1:3999"))
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.dockVisible = true
	m.sideVisible = true
	m.dockTab = dockTodos
	m.Update(tea.KeyMsg{Type: keyCtrlDigit3})
	if m.dockTab != dockTodos+2 {
		t.Fatalf("ctrl+3 with side open: dockTab=%d, want %d", m.dockTab, dockTodos+2)
	}
	if !m.sideVisible {
		t.Fatal("the tab chord must not close the side window")
	}
}

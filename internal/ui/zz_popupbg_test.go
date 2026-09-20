// Built with AI-assisted development (DeepSeek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	"dsh-cli/internal/app"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestPopupOpaqueCard pins the popup's solid-card contract: the built-in
// card surface is the terminal's own background (SGR 49, the same color
// the main window rides), so every cell the box paints - border, title
// row, padding, blank tail, footer - carries a background and nothing of
// the underlying screen shows through the box.
func TestPopupOpaqueCard(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a := app.New(newFakeHost(t).URL)
	a.Start(ctx)
	m := NewModel(a)
	m.W, m.H = 100, 40
	m.th.Profile = termenv.TrueColor // go test stdout is not a TTY
	if m.th.CCardBG == "" {
		t.Fatal("built-in card surface empty: the popup would ride transparent")
	}
	if card := m.th.CardState(); len(card.bg) != 1 || card.bg[0] != 49 {
		t.Fatalf("built-in card surface %v, want the terminal default background (49)", card.bg)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlH}) // open the help popup
	if m.topModal() == nil {
		t.Fatal("help popup did not open")
	}
	box, _, _, _, _ := m.modalBox(m.topModal())
	for i, ln := range strings.Split(box, "\n") {
		for _, c := range scanStyled(ln) {
			if len(c.state.bg) == 0 {
				t.Fatalf("box row %d col %d is transparent (want the card surface)", i, c.col)
			}
		}
	}
}

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

// TestPopupOpaqueCard pins the popup's solid-card contract: the card is a
// painted panel of its own - never the terminal's default background,
// which is exactly the color the main window rides, so a popup wearing it
// goes unnoticed - and every cell the box paints (border, title row,
// padding, blank tail, footer) carries a background, so nothing of the
// underlying screen shows through the box.
func TestPopupOpaqueCard(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a := app.New(newFakeHost(t).URL)
	a.Start(ctx)
	m := NewModel(a)
	m.W, m.H = 100, 40
	m.th.Profile = termenv.TrueColor // go test stdout is not a TTY
	card := m.th.CardState()
	if len(card.bg) == 0 || (len(card.bg) == 1 && card.bg[0] == 49) {
		t.Fatalf("built-in card surface %v: the popup rides the terminal's own background", card.bg)
	}
	if m.th.CCardEdge == "" {
		t.Fatal("no popup frame edge: the outline would die on the panel it borders")
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

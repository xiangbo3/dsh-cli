package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestDebugBilling(t *testing.T) {
	t.Setenv("DSH_CLI_HOME", t.TempDir())
	m, _ := seedUsageModel(t)
	m.localSlash("status", "")
	sm := m.topModal().(*statusModal)

	steps := []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'4'}},
		{Type: tea.KeyRunes, Runes: []rune{'8'}},
		{Type: tea.KeyRunes, Runes: []rune{'3'}},
		{Type: tea.KeyRunes, Runes: []rune{'.'}},
		{Type: tea.KeyRunes, Runes: []rune{'3'}},
		{Type: tea.KeyDown},
		{Type: tea.KeyRunes, Runes: []rune{'2'}},
		{Type: tea.KeyRunes, Runes: []rune{'.'}},
		{Type: tea.KeyRunes, Runes: []rune{'5'}},
		{Type: tea.KeyBackspace},
		{Type: tea.KeyBackspace},
		{Type: tea.KeyBackspace},
	}
	for i, km := range steps {
		m.Update(km)
		t.Logf("step %2d: in=%q out=%q inPrice=%v outPrice=%v sec=%d cur=%d",
			i, string(sm.priceInEd.val), string(sm.priceOutEd.val),
			m.tokenInPrice, m.tokenOutPrice, sm.sec, sm.cur)
	}
}

package ui

import (
	"testing"

	"dsh-cli/internal/protocol"

	"github.com/charmbracelet/bubbletea"
)

// TestPickerToggleChords pins the popup toggle rule: while a picker owns
// the keyboard, pressing the chord that opened it a second time closes
// the window without confirming. (The help modal toggles on ctrl+h inside
// its own update and the session window on ctrl+s in the 1a block; both
// are covered by their own tests.)
func TestPickerToggleChords(t *testing.T) {
	// Model picker: ctrl+n toggles shut, no wire action.
	m := keyModel(t)
	pk := newModelPicker()
	pk.fill(&protocol.SessionModels{
		Groups: []protocol.ModelProviderGroup{{Id: "acme", Name: "Acme",
			Models: []protocol.ModelCatalogModel{{Id: "acme-alpha"}}}},
	})
	m.openModal(pk)
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlN})
	if top := m.topModal(); top != nil {
		t.Fatalf("ctrl+n must close the model picker, top = %T", top)
	}

	// Mode picker: ctrl+o toggles shut.
	m = keyModel(t)
	m.openModal(&modePicker{presets: []protocol.AgentPresetEntry{
		{Id: "standard"}, {Id: "minimal"},
	}, loc: m.loc})
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlO})
	if top := m.topModal(); top != nil {
		t.Fatalf("ctrl+o must close the mode picker, top = %T", top)
	}

	// Workspace browser: ctrl+w toggles shut; esc closes from the pick
	// face, but from the add sub-mode it only goes back.
	m = keyModel(t)
	m.st.SetWorkspaces([]protocol.WorkspaceView{
		{WorkspaceId: "w1", Path: "/home/u/alpha", Title: "alpha"},
	}, nil)
	m.openWorkspace()
	if _, ok := m.topModal().(*workspaceModal); !ok {
		t.Fatalf("openWorkspace did not open the browser: %T", m.topModal())
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlW})
	if top := m.topModal(); top != nil {
		t.Fatalf("ctrl+w must close the workspace browser, top = %T", top)
	}

	m.openWorkspace()
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if top := m.topModal(); top != nil {
		t.Fatalf("esc must close the workspace browser from the pick face, top = %T", top)
	}

	m.openWorkspace()
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	mod, ok := m.topModal().(*workspaceModal)
	if !ok || mod.mode != wsAdd {
		t.Fatalf("a must enter the add sub-mode: %T mode=%v", m.topModal(), mod.mode)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	top := m.topModal()
	if top == nil {
		t.Fatal("esc in the add sub-mode must go back to the pick face, not close")
	}
	if w, ok2 := top.(*workspaceModal); !ok2 || w.mode != wsPick {
		t.Fatalf("esc in the add sub-mode must land on the pick face: %T", top)
	}

	// And the picker chords stay inert under other popups: ctrl+n with
	// the mode picker open must not close it.
	m = keyModel(t)
	m.openModal(&modePicker{presets: []protocol.AgentPresetEntry{{Id: "standard"}}, loc: m.loc})
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlN})
	if top := m.topModal(); top == nil {
		t.Fatal("ctrl+n must not close a picker it did not open")
	}
}

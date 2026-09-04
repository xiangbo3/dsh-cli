// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"testing"

	"dsh-cli/internal/app"
	"dsh-cli/internal/protocol"

	"github.com/charmbracelet/bubbletea"
)

func keyDown() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyDown} }

func TestResolveWorkspaceRef(t *testing.T) {
	a := app.New(newFakeHost(t).URL)
	m := NewModel(a)
	m.st.SetWorkspaces([]protocol.WorkspaceView{
		{WorkspaceId: "w1", Path: "/home/u/alpha", Title: "alpha"},
		{WorkspaceId: "w2", Path: "/home/u/alpha-clone", Title: "alpha clone"},
		{WorkspaceId: "w3", Path: "/srv/beta", Title: "beta"},
	}, nil)

	cases := []struct {
		name      string
		wantID    string
		wantDir   string
		wantError bool
	}{
		{"alpha", "w1", "", false},                      // exact title
		{"Alpha", "w1", "", false},                      // case-insensitive title
		{"bet", "w3", "", false},                        // unique prefix
		{"ALPHA-CLONE", "w2", "", false},                // other exact
		{"al", "", "", true},                            // ambiguous prefix
		{"gamma", "", "", true},                         // no such title
		{"/home/u/alpha", "w1", "", false},              // registered path
		{"/srv/elsewhere", "", "/srv/elsewhere", false}, // unregistered path
		{"", "", "", true},                              // empty
	}
	for _, c := range cases {
		ws, dir, problem := m.resolveWorkspaceRef(c.name)
		switch {
		case c.wantError:
			if problem == "" {
				t.Errorf("resolve(%q) succeeded (%+v %q), want failure", c.name, ws, dir)
			}
		case ws != nil:
			if ws.WorkspaceId != c.wantID {
				t.Errorf("resolve(%q) = %s, want %s", c.name, ws.WorkspaceId, c.wantID)
			}
		default:
			if dir != c.wantDir {
				t.Errorf("resolve(%q) path = %q, want %q", c.name, dir, c.wantDir)
			}
		}
	}
}

func TestWorkspaceModalRowsAndPerform(t *testing.T) {
	a := app.New(newFakeHost(t).URL)
	m := NewModel(a)
	m.W, m.H = 100, 30
	m.st.SetWorkspaces([]protocol.WorkspaceView{
		{WorkspaceId: "w1", Path: "/home/u/alpha", Title: "alpha", SessionIds: []string{"s1"}},
		{WorkspaceId: "w2", Path: "/home/u/beta", Title: "beta"},
	}, nil)

	cmd := m.openWorkspace()
	if cmd != nil {
		t.Fatal("openWorkspace should not return a cmd")
	}
	mod, ok := m.topModal().(*workspaceModal)
	if !ok {
		t.Fatalf("top modal = %T", m.topModal())
	}
	if mod.mode != wsPick || mod.cur != 0 {
		t.Fatalf("open state = %+v", mod)
	}
	body := mod.view(m, m.transcriptWidth(), 0)
	// Rows only: the key-hint footer moved to the popup chrome (hint()).
	if len(body) != 3 {
		t.Fatalf("view rows: %v", body)
	}
	// enter on the first workspace row returns the switch cmd (not yet run).
	if _, handled := mod.update(keyDown()); !handled {
		t.Fatal("down key not handled")
	}
	if cmd, ok := m.workspacePerform(mod); !ok || cmd == nil {
		t.Fatalf("perform on row: ok=%v cmd=%v", ok, cmd)
	}
	if len(m.mods) != 0 {
		t.Fatal("modal must close on open")
	}
}

// TestWorkspaceModalInputDoesNotAutoSubmit: typing into the add/rename line
// must accumulate characters through the normal key path without closing the
// modal or firing a wire action (the first char used to submit immediately).
func TestWorkspaceModalInputDoesNotAutoSubmit(t *testing.T) {
	a := app.New(newFakeHost(t).URL)
	m := NewModel(a)
	m.W, m.H = 100, 30
	m.st.SetWorkspaces([]protocol.WorkspaceView{
		{WorkspaceId: "w1", Path: "/home/u/alpha", Title: "alpha"},
	}, nil)
	m.openWorkspace()
	mod, ok := m.topModal().(*workspaceModal)
	if !ok {
		t.Fatalf("top modal = %T", m.topModal())
	}
	if _, handled := mod.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")}); !handled {
		t.Fatal("a key not handled in pick mode")
	}
	if mod.mode != wsAdd {
		t.Fatalf("mode = %d, want add", mod.mode)
	}
	for _, ch := range "/tmp/foo" {
		if _, handled := m.handleModalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}}); !handled {
			t.Fatalf("rune %q not handled", ch)
		}
		if len(m.mods) != 1 {
			t.Fatalf("modal auto-closed after first char %q", ch)
		}
	}
	if got := mod.input.string(); got != "/tmp/foo" {
		t.Fatalf("input = %q, want %q", got, "/tmp/foo")
	}
	if len(m.mods) != 1 {
		t.Fatal("modal auto-closed while typing")
	}
	if top, ok := m.topModal().(*workspaceModal); !ok || top != mod {
		t.Fatalf("top modal changed: %T", m.topModal())
	}
}

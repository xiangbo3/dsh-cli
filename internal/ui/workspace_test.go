// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	"dsh-cli/internal/app"
	"dsh-cli/internal/i18n"
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

// TestWorkspaceAddMissingDir: an absolute path the host cannot list
// flips the add line into a create confirmation — esc returns to the
// (kept, editable) path, y creates the missing segments on the host and
// runs the add. An existing directory adds without any prompt.
func TestWorkspaceAddMissingDir(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	fh := newFakeHost(t)
	fh.dirs = map[string]bool{"/tmp": true}
	a := app.New(fh.URL)
	a.Start(ctx)
	m := NewModel(a)
	m.loc = i18n.English()
	m.W, m.H = 100, 30

	// openAdd opens the browser, types the path and returns the probe
	// cmd enter submitted.
	openAdd := func(dir string) *workspaceModal {
		t.Helper()
		m.openWorkspace()
		mod, ok := m.topModal().(*workspaceModal)
		if !ok {
			t.Fatalf("top modal = %T", m.topModal())
		}
		if _, handled := m.handleModalKey(keyRunes("a")); !handled {
			t.Fatal("a not handled")
		}
		for _, ch := range dir {
			m.handleModalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
		}
		cmd, _ := m.handleModalKey(tea.KeyMsg{Type: tea.KeyEnter})
		if cmd == nil {
			t.Fatal("enter should return the probe cmd")
		}
		if got := mod.input.string(); got != dir {
			t.Fatalf("typed path = %q, want %q", got, dir)
		}
		m.Update(cmd()) // the probe answer: confirmation, or add + close
		return mod
	}

	mod := openAdd("/tmp/new/proj")
	if mod.mode != wsMkdir || mod.mkdirPath != "/tmp/new/proj" {
		t.Fatalf("after probe: mode=%d path=%q (want the create confirmation)", mod.mode, mod.mkdirPath)
	}
	face := strings.Join(mod.view(m, 90, 0), "\n")
	if !strings.Contains(face, "/tmp/new/proj") || !strings.Contains(face, "does not exist") {
		t.Fatalf("confirmation face = %q", stripANSI(face))
	}

	// esc: back to the line, the typed path kept for editing.
	if _, handled := m.handleModalKey(tea.KeyMsg{Type: tea.KeyEsc}); !handled {
		t.Fatal("esc not handled on the confirmation")
	}
	if mod.mode != wsAdd || mod.input.string() != "/tmp/new/proj" {
		t.Fatalf("after esc: mode=%d input=%q", mod.mode, mod.input.string())
	}

	// Re-confirm, then y: the host gets one createDirectory per missing
	// segment and the add runs under it.
	cmd, _ := m.handleModalKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.Update(cmd())
	if mod.mode != wsMkdir {
		t.Fatalf("mode=%d (want the confirmation again)", mod.mode)
	}
	cmd, _ = m.handleModalKey(keyRunes("y"))
	if cmd == nil {
		t.Fatal("y should return the create+add cmd")
	}
	cmd()
	fh.mu.Lock()
	made := append([]string{}, fh.made...)
	fh.mu.Unlock()
	if len(made) != 2 || made[0] != "/tmp/new" || made[1] != "/tmp/new/proj" {
		t.Fatalf("host directories created = %v", made)
	}
	if len(m.mods) != 0 {
		t.Fatalf("modal still open after y: %d", len(m.mods))
	}

	// An existing directory: the probe answer adds straight through, no
	// confirmation face.
	mod = openAdd("/tmp")
	if len(m.mods) != 0 {
		t.Fatalf("existing dir left %d modals open", len(m.mods))
	}
	_ = mod
}

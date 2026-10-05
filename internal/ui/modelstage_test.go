// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"dsh-cli/internal/app"
	"dsh-cli/internal/i18n"
	"dsh-cli/internal/protocol"

	tea "github.com/charmbracelet/bubbletea"
)

// TestStagedModelNoSession pins the session-less model pick: it stages
// (no host RPC), the top bar shows it immediately, and the first session
// that becomes current takes the pick over the wire.
func TestStagedModelNoSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	fh := newFakeHost(t)
	fh.sessions = []protocol.SessionSummary{{SessionId: "s1"}}
	fh.models = protocol.SessionModels{
		Groups: []protocol.ModelProviderGroup{
			{Id: "acme", Name: "Acme", Models: []protocol.ModelCatalogModel{
				{Id: "a1", Name: "A1"}, {Id: "a2", Name: "A2"},
			}},
		},
	}
	a := app.New(fh.URL)
	a.Start(ctx)
	m := NewModel(a)
	m.loc = i18n.English()
	m.W, m.H = 120, 40
	// Roster s1 exists on the host but nothing is current in the store.
	if m.activeID() != "" {
		t.Fatalf("active = %q (want none)", m.activeID())
	}

	cmd := m.openModelPicker()
	if cmd == nil {
		t.Fatal("picker did not open without a session")
	}
	m.Update(cmd())
	pk := m.topModal().(*modelPicker)
	if len(pk.rows) != 2 {
		t.Fatalf("rows = %d (want 2)", len(pk.rows))
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("staged pick must not RPC, got %T", cmd)
	}
	if fh.called("session.selectModel") {
		t.Fatal("session.selectModel hit the host during staging")
	}
	if m.stagedModel == nil || m.stagedModel.Model != "a2" || m.stagedModel.Provider != "acme" {
		t.Fatalf("staged = %+v (want acme/a2)", m.stagedModel)
	}
	// The top bar carries the staged pick at once.
	top := strings.Split(stripANSI(m.View()), "\n")[0]
	if !strings.Contains(top, "a2") {
		t.Fatalf("top bar after staging = %q (want the staged model)", top)
	}

	// A session becomes current: the pick lands on it over the wire.
	_, c := m.Update(createMsg{id: "s1"})
	if m.stagedModel != nil {
		t.Fatal("staged pick not consumed on session activation")
	}
	if m.activeID() != "s1" {
		t.Fatalf("active = %q (want s1)", m.activeID())
	}
	drainSeq(t, m, c)
	if !fh.called("session.selectModel") {
		t.Fatal("session.selectModel not sent for the staged pick")
	}
	// The catalog cache now resolves the readout to the display name.
	top = strings.Split(stripANSI(m.View()), "\n")[0]
	if !strings.Contains(top, "A2") {
		t.Fatalf("top bar after activation = %q (want A2)", top)
	}
}

// TestModelPickReadoutImmediate pins the optimistic readout: the top bar
// carries the confirmed model the moment the picker closes — before the
// selectModel round-trip has run.
func TestModelPickReadoutImmediate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	fh := newFakeHost(t)
	fh.sessions = []protocol.SessionSummary{{SessionId: "s1"}}
	fh.models = protocol.SessionModels{
		Groups: []protocol.ModelProviderGroup{
			{Id: "acme", Name: "Acme", Models: []protocol.ModelCatalogModel{
				{Id: "a1", Name: "A1"}, {Id: "a2", Name: "A2"},
			}},
		},
		Current: protocol.ModelSelection{Provider: "acme", Model: "a1"},
	}
	a := app.New(fh.URL)
	a.Start(ctx)
	m := NewModel(a)
	m.loc = i18n.English()
	m.W, m.H = 120, 40
	m.st.SetActive("s1")

	cmd := m.openModelPicker()
	if cmd == nil {
		t.Fatal("picker did not open")
	}
	m.Update(cmd())
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("confirm must send the pick over the wire")
	}
	// The readout moved with the pick, not with the round-trip.
	top := strings.Split(stripANSI(m.View()), "\n")[0]
	if !strings.Contains(top, "A2") {
		t.Fatalf("top bar on confirm = %q (want A2)", top)
	}
	if fh.called("session.selectModel") {
		t.Fatal("selectModel ran before the cmd — the readout is not optimistic")
	}
	drainSeq(t, m, cmd)
	if !fh.called("session.selectModel") {
		t.Fatal("selectModel never sent")
	}
	if label := m.ctxLabel(); label != "A2" {
		t.Fatalf("ctxLabel after confirm = %q (want A2)", label)
	}
}

// drainSeq runs a command tree to exhaustion the way the bubbletea
// program does: a command whose result is itself a command list (the
// unexported sequenceMsg, a []Cmd) is expanded and walked recursively,
// every other message is fed back through Update, and the command Update
// returns continues the walk.
func drainSeq(t *testing.T, m *Model, start tea.Cmd) {
	t.Helper()
	cmdType := reflect.TypeOf(tea.Cmd(nil))
	expand := func(c tea.Cmd) []tea.Cmd {
		if c == nil {
			return nil
		}
		msg := c()
		if rv := reflect.ValueOf(msg); rv.Kind() == reflect.Slice && rv.Type().Elem() == cmdType {
			return rv.Convert(reflect.SliceOf(cmdType)).Interface().([]tea.Cmd)
		}
		_, next := m.Update(msg)
		if next == nil {
			return nil
		}
		return []tea.Cmd{next}
	}
	queue := expand(start)
	for len(queue) > 0 {
		head, rest := queue[0], queue[1:]
		queue = append(expand(head), rest...)
	}
}

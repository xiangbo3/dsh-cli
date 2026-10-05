// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"dsh-cli/internal/app"
	"dsh-cli/internal/i18n"
	"dsh-cli/internal/protocol"
)

func keyRunes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// TestModelPickerFetchAdd pins the "fetch model list" flow: f probes the
// cursor provider's endpoint, the models the catalog lacks open a
// selection dialog (every row pre-checked), space toggles, enter confirms
// the add via settings.mutate, and the catalog refresh pulls them into the
// roster.
func TestModelPickerFetchAdd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	fh := newFakeHost(t)
	// The boot roster refresh re-baselines the store: s1 must exist on
	// the host or the background sync drops the test's active session.
	fh.sessions = []protocol.SessionSummary{{SessionId: "s1"}}
	fh.models = protocol.SessionModels{
		Groups: []protocol.ModelProviderGroup{
			{Id: "custom", Name: "Custom", Models: []protocol.ModelCatalogModel{{Id: "m1", Name: "M1"}}},
		},
	}
	a := app.New(fh.URL)
	a.Start(ctx)
	m := NewModel(a)
	m.loc = i18n.English()
	m.st.SetActive("s1")

	// Open the picker and deliver the catalog.
	m.openModelPicker()
	if _, cmd := m.Update(modelsLoadedMsg{id: "s1", models: &fh.models}); cmd != nil {
		t.Fatalf("unexpected cmd: %v", cmd)
	}
	if _, ok := m.topModal().(*modelPicker); !ok {
		t.Fatal("the picker should be open")
	}

	// f probes the endpoint; the result opens the add dialog with only m2
	// (m1 is already in the catalog).
	_, cmd := m.Update(keyRunes("f"))
	if cmd == nil {
		t.Fatal("f should return a fetch cmd")
	}
	m.Update(cmd())
	ap, ok := m.topModal().(*addModelPicker)
	if !ok {
		t.Fatalf("top modal = %T (want the add dialog)", m.topModal())
	}
	if len(ap.models) != 1 || ap.models[0].Id != "m2" {
		t.Fatalf("dialog models = %+v (want only m2)", ap.models)
	}
	if ap.checkedCount() != 1 {
		t.Fatalf("the missing model should start checked (count=%d)", ap.checkedCount())
	}

	// space toggles the check off; enter with nothing checked is a refused
	// no-op (no RPC).
	m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if _, c := m.Update(tea.KeyMsg{Type: tea.KeyEnter}); c != nil {
		t.Fatal("enter with nothing checked should be a no-op")
	}
	m.Update(tea.KeyMsg{Type: tea.KeySpace})

	// enter confirms: append m2 to the provider's models array, close the
	// dialog, kick the catalog refresh.
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should return an add cmd")
	}
	fh.models.Groups[0].Models = append(fh.models.Groups[0].Models, protocol.ModelCatalogModel{Id: "m2", Name: "M2"})
	_, refresh := m.Update(cmd())
	if _, ok := m.topModal().(*modelPicker); !ok {
		t.Fatalf("top modal = %T (want the picker back)", m.topModal())
	}
	var mutate struct {
		Ns  string               `json:"ns"`
		Ops []protocol.SettingOp `json:"ops"`
	}
	if err := json.Unmarshal([]byte(fh.lastMutatePayload()), &mutate); err != nil {
		t.Fatalf("bad mutate payload: %v", err)
	}
	if mutate.Ns != "llm-pi-ai" || len(mutate.Ops) != 1 {
		t.Fatalf("mutate = %+v", mutate)
	}
	if got := strings.Join(mutate.Ops[0].Path, "/"); got != "providers/custom/models" {
		t.Fatalf("op path = %q", got)
	}
	raw, _ := json.Marshal(mutate.Ops[0].Value)
	var models []map[string]any
	if err := json.Unmarshal(raw, &models); err != nil {
		t.Fatalf("bad models value: %v", err)
	}
	if len(models) != 2 || models[0]["id"] != "m1" || models[1]["id"] != "m2" {
		t.Fatalf("models = %s", raw)
	}
	if models[1]["contextWindow"] != float64(4096) {
		t.Fatalf("m2 entry = %v (want contextWindow 4096)", models[1])
	}

	// the refresh pulls m2 into the roster.
	m.Update(refresh())
	pk := m.topModal().(*modelPicker)
	if len(pk.rows) != 2 {
		t.Fatalf("after refresh: rows=%d (want 2)", len(pk.rows))
	}
}

// TestModelPickerFetchUnknownProvider pins the no-settings face: a provider
// with no settings entry gets the host-shaped note, and the picker stays
// open (no dialog).
func TestModelPickerFetchUnknownProvider(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	fh := newFakeHost(t)
	fh.sessions = []protocol.SessionSummary{{SessionId: "s1"}}
	fh.models = protocol.SessionModels{
		Groups: []protocol.ModelProviderGroup{
			{Id: "acme", Name: "Acme", Models: []protocol.ModelCatalogModel{{Id: "a1"}}},
		},
	}
	a := app.New(fh.URL)
	a.Start(ctx)
	m := NewModel(a)
	m.loc = i18n.English()
	m.st.SetActive("s1")

	m.openModelPicker()
	m.Update(modelsLoadedMsg{id: "s1", models: &fh.models})
	_, cmd := m.Update(keyRunes("f"))
	m.Update(cmd())
	pk, ok := m.topModal().(*modelPicker)
	if !ok {
		t.Fatalf("top modal = %T (the picker should stay open)", m.topModal())
	}
	if !strings.Contains(pk.fetchNote, "no settings") {
		t.Fatalf("fetchNote = %q (want the no-settings note)", pk.fetchNote)
	}
}

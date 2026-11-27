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

// bootModelPicker boots a model against the private host with one custom
// provider (settings store m1; the catalog advertises m1 and mz) and
// opens the picker with the catalog delivered.
func bootModelPicker(t *testing.T, ctx context.Context) (*Model, *fakeHost) {
	t.Helper()
	fh := newFakeHost(t)
	fh.sessions = []protocol.SessionSummary{{SessionId: "s1"}}
	fh.models = protocol.SessionModels{
		Groups: []protocol.ModelProviderGroup{
			{Id: "custom", Name: "Custom", Models: []protocol.ModelCatalogModel{
				{Id: "m1", Name: "M1"}, {Id: "mz", Name: "MZ"},
			}},
		},
	}
	a := app.New(fh.URL)
	a.Start(ctx)
	m := NewModel(a)
	m.loc = i18n.English()
	m.st.SetActive("s1")
	m.openModelPicker()
	m.Update(modelsLoadedMsg{id: "s1", models: &fh.models})
	if _, ok := m.topModal().(*modelPicker); !ok {
		t.Fatalf("top modal = %T (want the picker)", m.topModal())
	}
	return m, fh
}

// mutateModels decodes the last settings.mutate into the written models
// array (the op path is checked by the caller's expectations).
func mutateModels(t *testing.T, fh *fakeHost) ([]map[string]any, string) {
	t.Helper()
	var mutate struct {
		Ns  string               `json:"ns"`
		Ops []protocol.SettingOp `json:"ops"`
	}
	if err := json.Unmarshal([]byte(fh.lastMutatePayload()), &mutate); err != nil {
		t.Fatalf("bad mutate payload: %v", err)
	}
	if len(mutate.Ops) != 1 {
		t.Fatalf("mutate ops = %+v", mutate.Ops)
	}
	raw, _ := json.Marshal(mutate.Ops[0].Value)
	var models []map[string]any
	if err := json.Unmarshal(raw, &models); err != nil {
		t.Fatalf("bad models value: %s", raw)
	}
	return models, strings.Join(mutate.Ops[0].Path, "/")
}

// TestModelPickerAddEntry pins the `a` key: an empty form for the cursor
// provider, a rejected empty id, then a typed entry appended to the
// provider's stored models with the write reported as a toast.
func TestModelPickerAddEntry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	m, fh := bootModelPicker(t, ctx)

	if _, cmd := m.Update(keyRunes("a")); cmd != nil {
		t.Fatal("a should just open the form")
	}
	ed, ok := m.topModal().(*modelEdit)
	if !ok {
		t.Fatalf("top modal = %T (want the entry form)", m.topModal())
	}
	if ed.origId != "" || ed.provider != "custom" {
		t.Fatalf("form = %+v (want a fresh custom entry)", ed)
	}

	// Enter on the empty id is refused in-form (no RPC).
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if ed.err == "" {
		t.Fatal("the empty id should be rejected")
	}
	if fh.lastMutatePayload() != "" {
		t.Fatal("the rejected draft must not write settings")
	}

	// id, name, context window; enter saves.
	m.Update(keyRunes("m3"))
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(keyRunes("M Three"))
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(keyRunes("200000"))
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should return the save cmd")
	}
	if !ed.saving {
		t.Fatal("the form should show the save in flight")
	}
	_, refresh := m.Update(cmd())

	models, path := mutateModels(t, fh)
	if path != "providers/custom/models" {
		t.Fatalf("op path = %q", path)
	}
	if len(models) != 2 || models[0]["id"] != "m1" || models[1]["id"] != "m3" {
		t.Fatalf("models = %+v (want m1 kept, m3 appended)", models)
	}
	if models[1]["name"] != "M Three" || models[1]["contextWindow"] != float64(200000) {
		t.Fatalf("m3 entry = %+v", models[1])
	}
	if _, ok := m.topModal().(*modelPicker); !ok {
		t.Fatalf("top modal = %T (want the picker back after the save)", m.topModal())
	}
	if n := m.toasts[len(m.toasts)-1]; n.level != "ok" || !strings.Contains(n.text, "m3") {
		t.Fatalf("toast = %+v (want the add report)", n)
	}

	// The refresh re-pulls the catalog with the new entry listed.
	fh.models.Groups[0].Models = append(fh.models.Groups[0].Models, protocol.ModelCatalogModel{Id: "m3", Name: "M Three"})
	if bm, ok := refresh().(tea.BatchMsg); ok {
		for _, sub := range bm {
			if msg := sub(); msg != nil {
				m.Update(msg)
			}
		}
	}
	pk := m.topModal().(*modelPicker)
	if len(pk.rows) != 3 {
		t.Fatalf("after refresh: rows=%d (want 3)", len(pk.rows))
	}
}

// TestModelPickerAddDuplicate pins the roster guard: a fresh entry whose
// id the provider's roster already lists is refused in-form.
func TestModelPickerAddDuplicate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	m, fh := bootModelPicker(t, ctx)

	m.Update(keyRunes("a"))
	ed := m.topModal().(*modelEdit)
	m.Update(keyRunes("m1"))
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if ed.err != m.loc.T("model.exists", "m1", "custom") {
		t.Fatalf("err = %q (want the already-in-roster note)", ed.err)
	}
	if fh.lastMutatePayload() != "" {
		t.Fatal("the duplicate must not write settings")
	}
}

// TestModelPickerEditEntry pins the `e` key: the stored entry prefills
// the form, an edited capacity rewrites the entry in place (fields the
// form does not carry stay stored), and the roster keeps its order.
func TestModelPickerEditEntry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	fh := newFakeHost(t)
	fh.sessions = []protocol.SessionSummary{{SessionId: "s1"}}
	fh.settings = map[string]json.RawMessage{
		"llm-pi-ai": []byte(`{"providers":{"custom":{"baseURL":"http://upstream/v1","api":"openai-completions","models":[{"id":"m1","name":"M1","input":["text"],"maxTokens":8192}]}}}`),
	}
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
	m.openModelPicker()
	m.Update(modelsLoadedMsg{id: "s1", models: &fh.models})

	if _, cmd := m.Update(keyRunes("e")); cmd == nil {
		t.Fatal("e should return the entry-load cmd")
	} else {
		m.Update(cmd())
	}
	ed, ok := m.topModal().(*modelEdit)
	if !ok {
		t.Fatalf("top modal = %T (want the entry form)", m.topModal())
	}
	if got := ed.edits[0].string(); got != "m1" {
		t.Fatalf("id field = %q", got)
	}
	if got := ed.edits[1].string(); got != "M1" {
		t.Fatalf("name field = %q (want the stored name)", got)
	}
	if got := ed.edits[3].string(); got != "8192" {
		t.Fatalf("max field = %q (want the stored maxTokens)", got)
	}

	// A non-numeric capacity is refused; the context window then saves.
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(keyRunes("x"))
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if ed.err != m.loc.T("moledit.err.ctx") {
		t.Fatalf("err = %q (want the capacity note)", ed.err)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	m.Update(keyRunes("131072"))
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should return the save cmd")
	}
	if ed.err != "" {
		t.Fatalf("err = %q after a valid draft", ed.err)
	}
	m.Update(cmd())

	models, _ := mutateModels(t, fh)
	if len(models) != 1 {
		t.Fatalf("models = %+v (want the entry replaced in place)", models)
	}
	if models[0]["contextWindow"] != float64(131072) || models[0]["maxTokens"] != float64(8192) {
		t.Fatalf("entry = %+v (want the edited window over the stored max)", models[0])
	}
	if models[0]["input"] == nil {
		t.Fatalf("entry = %+v (want the stored modalities kept)", models[0])
	}
}

// TestModelPickerEditInputTypes pins the form's input-type row: the
// stored modalities prefill the boxes, the last check refuses to clear,
// and a checked box rewrites the key under the route's own name.
func TestModelPickerEditInputTypes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	fh := newFakeHost(t)
	fh.sessions = []protocol.SessionSummary{{SessionId: "s1"}}
	fh.settings = map[string]json.RawMessage{
		"llm-pi-ai": []byte(`{"providers":{"custom":{"baseURL":"http://upstream/v1","api":"openai-completions","models":[{"id":"m1","name":"M1","input":["text"]}]}}}`),
	}
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
	m.openModelPicker()
	m.Update(modelsLoadedMsg{id: "s1", models: &fh.models})

	_, cmd := m.Update(keyRunes("e"))
	m.Update(cmd())
	ed, ok := m.topModal().(*modelEdit)
	if !ok {
		t.Fatalf("top modal = %T (want the entry form)", m.topModal())
	}
	if !ed.inputOn[0] || ed.inputOn[1] || ed.inputDirty {
		t.Fatalf("modalities = %v (want the stored text-only input prefilled)", ed.inputOn)
	}
	if ln := ed.view(m, 0, 0)[modInputRow]; !strings.Contains(ln, "[x]") || !strings.Contains(ln, "[ ]") {
		t.Fatalf("input row = %q (want text checked, image clear)", stripANSI(ln))
	}

	// Walk to the input row; space on the only check is refused (the
	// set never empties).
	for i := 0; i < modInputRow; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	if ed.cur != modInputRow {
		t.Fatalf("cursor on field %d (want the input row)", ed.cur)
	}
	m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if !ed.inputOn[0] || ed.inputDirty {
		t.Fatalf("the last check cleared (inputOn=%v dirty=%v)", ed.inputOn, ed.inputDirty)
	}

	// Right onto image, check it, save: the pi-ai route stores "input".
	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if !ed.inputOn[1] || !ed.inputDirty {
		t.Fatalf("image toggle lost (inputOn=%v dirty=%v)", ed.inputOn, ed.inputDirty)
	}
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should return the save cmd")
	}
	m.Update(cmd())

	models, _ := mutateModels(t, fh)
	in, _ := models[0]["input"].([]any)
	if len(in) != 2 || in[0] != "text" || in[1] != "image" {
		t.Fatalf("entry = %+v (want input rewritten to text+image)", models[0])
	}
}

// TestMergeModelEntryInput pins the write-side key mapping: the modal
// key only moves once a toggle touched the set, the pi-ai routes keep
// their own key name, and dropping image drops its knobs.
func TestMergeModelEntryInput(t *testing.T) {
	base := json.RawMessage(`{"id":"m1","inputModalities":["text","image"],"imageMaxBytes":4194304}`)
	// Untouched: the stored key stays as it was.
	e, err := mergeModelEntry(base, "m1", "", 0, 0, "acme", []string{"text"}, false)
	if err != nil {
		t.Fatalf("untouched merge: %v", err)
	}
	if arr, ok := e["inputModalities"].([]any); !ok || len(arr) != 2 {
		t.Fatalf("untouched merge = %+v", e)
	}
	if _, ok := e["imageMaxBytes"]; !ok {
		t.Fatal("untouched merge dropped the stored knob")
	}
	// Checked down to text: the knob goes with the image input.
	e, _ = mergeModelEntry(base, "m1", "", 0, 0, "acme", []string{"text"}, true)
	if got := e["inputModalities"].([]string); len(got) != 1 || got[0] != "text" {
		t.Fatalf("modalities = %v", got)
	}
	if _, ok := e["imageMaxBytes"]; ok {
		t.Fatal("imageMaxBytes should clear with the image input")
	}
	// A pi-ai route keeps its own key name (and loses the foreign one).
	e, _ = mergeModelEntry(base, "m1", "", 0, 0, "llm-pi-ai", []string{"image"}, true)
	if got := e["input"].([]string); len(got) != 1 || got[0] != "image" {
		t.Fatalf("pi-ai input = %v", got)
	}
	if _, ok := e["inputModalities"]; ok {
		t.Fatal("the foreign key should not linger")
	}
	// A fresh entry without a touch gets no modal key at all.
	e, _ = mergeModelEntry(nil, "m2", "", 0, 0, "acme", []string{"text"}, false)
	if _, ok := e["inputModalities"]; ok {
		t.Fatalf("untouched fresh entry = %+v (want no modal key)", e)
	}
}

// TestModelPickerDeleteEntry pins the `d` key: a stored entry is removed
// from the provider's models (the rest kept), and a catalog-only model
// reports "not a stored entry" without a write.
func TestModelPickerDeleteEntry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	m, fh := bootModelPicker(t, ctx)

	// m1 is stored: remove it.
	_, cmd := m.Update(keyRunes("d"))
	if cmd == nil {
		t.Fatal("d should return the delete cmd")
	}
	m.Update(cmd())
	models, path := mutateModels(t, fh)
	if path != "providers/custom/models" {
		t.Fatalf("op path = %q", path)
	}
	if len(models) != 0 {
		t.Fatalf("models = %+v (want m1 gone)", models)
	}
	if n := m.toasts[len(m.toasts)-1]; n.level != "ok" || !strings.Contains(n.text, "m1") {
		t.Fatalf("toast = %+v (want the removal report)", n)
	}

	// mz is catalog-only: nothing to remove, no write.
	pk := m.topModal().(*modelPicker)
	if pk.cur < len(pk.rows)-1 {
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	before := fh.lastMutatePayload()
	_, cmd = m.Update(keyRunes("d"))
	m.Update(cmd())
	if got := fh.lastMutatePayload(); got != before {
		t.Fatalf("settings written for a catalog-only model: %s", got)
	}
	if n := m.toasts[len(m.toasts)-1]; n.level != "warn" || !strings.Contains(n.text, "mz") {
		t.Fatalf("toast = %+v (want the not-stored note)", n)
	}
}

// TestModelPickerEditNoSettings pins the no-settings face of `e`: the
// provider without a settings entry gets the note as a toast and the
// picker stays open (no form).
func TestModelPickerEditNoSettings(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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

	_, cmd := m.Update(keyRunes("e"))
	m.Update(cmd())
	if _, ok := m.topModal().(*modelPicker); !ok {
		t.Fatalf("top modal = %T (the picker should stay open)", m.topModal())
	}
	if n := m.toasts[len(m.toasts)-1]; n.level != "warn" || !strings.Contains(n.text, "acme") {
		t.Fatalf("toast = %+v (want the no-settings note)", n)
	}
}

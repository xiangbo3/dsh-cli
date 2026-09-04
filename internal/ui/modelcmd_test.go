// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	"dsh-cli/internal/app"
	"dsh-cli/internal/protocol"
)

// catalogForResolve builds the two-provider catalog the resolver tests run
// against: "acme" serves two models that share a prefix, "globex" one.
func catalogForResolve() *protocol.SessionModels {
	return &protocol.SessionModels{
		Groups: []protocol.ModelProviderGroup{
			{
				Id: "acme", Name: "Acme",
				Models: []protocol.ModelCatalogModel{
					{Id: "acme-alpha", Name: "Acme Alpha"},
					{Id: "acme-alpha-turbo", Name: "Acme Alpha Turbo"},
					{Id: "acme-beta"},
				},
			},
			{
				Id: "globex", Name: "Globex",
				Models: []protocol.ModelCatalogModel{
					{Id: "globex-one"},
				},
			},
		},
	}
}

func TestResolveModelName(t *testing.T) {
	cat := catalogForResolve()
	cases := []struct {
		name     string
		wantProv string
		wantID   string
	}{
		{"acme-alpha", "acme", "acme-alpha"},                  // exact id
		{"Acme Alpha", "acme", "acme-alpha"},                  // display name
		{"acme/acme-beta", "acme", "acme-beta"},               // provider/model form
		{"ACME/ACME-BETA", "acme", "acme-beta"},               // case-insensitive parts
		{"acme/Acme Alpha Turbo", "acme", "acme-alpha-turbo"}, // display name with provider
		{"globex-one", "globex", "globex-one"},                // exact id, other group
		{"ACME-ALPHA", "acme", "acme-alpha"},                  // case-insensitive id
		{"globex/one-shorthand", "", ""},                      // no match under provider
		{"nowhere/acme-beta", "", ""},                         // unknown provider
	}
	for _, c := range cases {
		p, id, problem := resolveModelName(cat, c.name)
		if c.wantID == "" {
			if problem == "" || (p != "" || id != "") {
				t.Errorf("resolve(%q) = %q/%q, %q (want a failure)", c.name, p, id, problem)
			}
			continue
		}
		if p != c.wantProv || id != c.wantID || problem != "" {
			t.Errorf("resolve(%q) = %q/%q, %q (want %q/%q)", c.name, p, id, problem, c.wantProv, c.wantID)
		}
	}

	// A unique prefix resolves; an ambiguous fragment lists candidates.
	if p, id, problem := resolveModelName(cat, "globex-one"); p != "globex" || id != "globex-one" || problem != "" {
		t.Errorf("resolve(globex-one) = %q/%q, %q", p, id, problem)
	}
	if p, id, problem := resolveModelName(cat, "glo"); p != "globex" || id != "globex-one" || problem != "" {
		t.Errorf("prefix resolve(glo) = %q/%q, %q", p, id, problem)
	}
	if p, id, problem := resolveModelName(cat, "acme-alpha-t"); p != "acme" || id != "acme-alpha-turbo" || problem != "" {
		t.Errorf("unique prefix resolve(acme-alpha-t) = %q/%q, %q", p, id, problem)
	}
	if _, _, problem := resolveModelName(cat, "alpha"); !strings.Contains(problem, "acme/acme-alpha") || !strings.Contains(problem, "acme/acme-alpha-turbo") {
		t.Errorf("ambiguous resolve should list candidates, got %q", problem)
	}
	if _, _, problem := resolveModelName(cat, "zzz"); !strings.Contains(problem, "no match") {
		t.Errorf("no-match resolve should say so, got %q", problem)
	}
	if _, _, problem := resolveModelName(cat, "  "); !strings.Contains(problem, "empty") {
		t.Errorf("blank name should be rejected, got %q", problem)
	}
}

// TestModelCommandRouting pins the /model dispatch: a name starts the
// direct switch (no picker), and a bare /model opens the picker.
func TestModelCommandRouting(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a := app.New(newFakeHost(t).URL)
	a.Start(ctx)
	m := NewModel(a)
	m.st.SetActive("s1")

	cmd, handled := m.localSlash("model", "acme-alpha")
	if !handled || cmd == nil {
		t.Fatal("/model <name> should kick off the direct switch")
	}
	cmd, handled = m.localSlash("model", "")
	if !handled || cmd == nil {
		t.Fatal("/model should open the picker")
	}
	if _, ok := m.topModal().(*modelPicker); !ok {
		t.Fatalf("top modal = %T (want *modelPicker)", m.topModal())
	}
}

// TestModelPickerFillClearsLoading pins the fix for "picker stuck on
// loading catalog…": filling the catalog must clear the loading flag, so
// the loaded rows are actually listed instead of the loading line.
func TestModelPickerFillClearsLoading(t *testing.T) {
	pk := newModelPicker()
	pk.loading = true
	pk.fill(&protocol.SessionModels{
		Groups: []protocol.ModelProviderGroup{
			{Id: "prov", Name: "Prov", Models: []protocol.ModelCatalogModel{{Id: "m1", Name: "M One"}}},
		},
	})
	if pk.loading {
		t.Fatal("fill should clear the loading flag")
	}
	if len(pk.rows) != 1 {
		t.Fatalf("rows = %d (want 1)", len(pk.rows))
	}
	view := stripANSI(strings.Join(pk.view(&Model{th: NewTheme()}, 0, 0), "\n"))
	if strings.Contains(view, "loading catalog") {
		t.Fatalf("view still shows the loading line: %q", view)
	}
	if !strings.Contains(view, "M One") {
		t.Fatalf("view does not list the loaded model: %q", view)
	}
}

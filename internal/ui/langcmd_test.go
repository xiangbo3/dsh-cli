package ui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dsh-cli/internal/app"
	"dsh-cli/internal/config"

	tea "github.com/charmbracelet/bubbletea"
)

// langTestEnv writes a zh catalog into a fresh DSH_LOCALES dir and pins
// the env so the test never inherits the host's language or config; it
// returns the locale dir and the isolated config (DSH_CLI_HOME) dir.
func langTestEnv(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	cfgDir := t.TempDir()
	t.Setenv("DSH_LOCALES", dir)
	t.Setenv("HOME", t.TempDir()) // isolate the locale storage dir
	t.Setenv("DSH_CLI_HOME", cfgDir) // no inherited startup preference
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "C")
	zh := `{
		"no.session": "（无会话）",
		"side.title": "会话",
		"lang.switched": "界面语言：%s",
		"lang.list": "界面语言：%s — 可用：%s",
		"common.untitled": "（未命名）"
	}`
	if err := os.WriteFile(filepath.Join(dir, "zh.json"), []byte(zh), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, cfgDir
}

// TestLanguageSwitchCommand pins /language with a name: it swaps the
// face (covered strings change, uncovered ones fall back to English),
// an unknown name keeps the current face, "en" restores it. (The bare
// command opens the picker — TestLanguagePicker.)
func TestLanguageSwitchCommand(t *testing.T) {
	langTestEnv(t)
	a := app.New("http://127.0.0.1:3999")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a.Start(ctx)
	m := NewModel(a)
	if m.loc.Lang != "en" {
		t.Fatalf("model starts in %q, want the built-in English", m.loc.Lang)
	}

	// Switch to Chinese.
	if cmd, handled := m.localSlash("language", "zh"); !handled || cmd != nil {
		t.Fatal("/language zh should switch synchronously")
	}
	if m.loc.Lang != "zh" {
		t.Fatalf("loc = %q, want zh", m.loc.Lang)
	}
	if got := m.loc.T("no.session"); got != "（无会话）" {
		t.Errorf("zh no.session = %q", got)
	}
	// Uncovered keys fall back to the English face.
	if got := m.loc.T("dock.no.jobs"); got != "(no background jobs)" {
		t.Errorf("fallback key = %q", got)
	}
	// The rendered chrome follows the locale.
	if got := m.topBar(100); !strings.Contains(got, "（无会话）") {
		t.Errorf("top bar = %q, want the zh no-session title", got)
	}
	if got := m.loc.T("lang.switched", m.loc.Name()); got != "界面语言：中文" {
		t.Errorf("switch toast = %q", got)
	}
	// The switch persists as the startup default (config.json language).
	if got := config.Load().Language; got != "zh" {
		t.Fatalf("config language after /language zh = %q, want zh persisted", got)
	}

	// Unknown language: keep the current face.
	if _, handled := m.localSlash("language", "xx-yy"); !handled {
		t.Fatal("/language xx-yy should be handled")
	}
	if m.loc.Lang != "zh" {
		t.Fatalf("loc = %q after a failed load, want zh kept", m.loc.Lang)
	}

	// Back to the built-in English.
	if _, handled := m.localSlash("language", "english"); !handled {
		t.Fatal("/language english should be handled")
	}
	if m.loc.Lang != "en" || m.loc.T("no.session") != "(no session)" {
		t.Fatalf("loc = %q %q after /language english", m.loc.Lang, m.loc.T("no.session"))
	}
	if got := config.Load().Language; got != "en" {
		t.Fatalf("config language after /language en = %q, want en persisted", got)
	}
}

// TestLanguagePicker pins the bare /language picker: it opens a window
// with a search line on top of the available catalogs; typing filters
// the roster (code or native name); enter on a row switches the face
// and closes; esc clears an open query first, then closes; a query
// with no match keeps the window open on the empty face.
func TestLanguagePicker(t *testing.T) {
	langTestEnv(t)
	a := app.New("http://127.0.0.1:3999")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a.Start(ctx)
	m := NewModel(a)
	m.W, m.H = 100, 30

	// Bare opens the picker with the on-disk roster (the test catalog
	// gives en + zh).
	if _, handled := m.localSlash("language", ""); !handled {
		t.Fatal("bare /language should be handled")
	}
	lm, ok := m.topModal().(*languageModal)
	if !ok {
		t.Fatalf("top modal = %T, want *languageModal", m.topModal())
	}
	if got := len(lm.rows); got != 2 {
		t.Fatalf("roster = %d rows, want 2 (en + zh)", got)
	}
	rendered := ""
	for _, ln := range lm.view(m, 100, 20) {
		rendered += stripANSI(ln) + "\n"
	}
	if !strings.Contains(rendered, "English") || !strings.Contains(rendered, "中文") {
		t.Fatalf("picker view missing the roster: %q", rendered)
	}
	if !strings.Contains(rendered, "(current)") {
		t.Fatalf("picker view missing the current-language mark: %q", rendered)
	}

	// Typing filters the roster (a Chinese query matches the native
	// name); the cursor clamps onto a surviving row.
	lm.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("中")})
	if got := lm.filtered(); len(got) != 1 || got[0].code != "zh" {
		t.Fatalf("filter 中 = %v, want only zh", got)
	}
	lm.cur = 5
	lm.clampCur()
	if lm.cur != 0 {
		t.Fatalf("cursor after filter = %d, want 0", lm.cur)
	}

	// Enter on the highlighted row switches the face and closes.
	if _, handled := m.handleModalKey(tea.KeyMsg{Type: tea.KeyEnter}); !handled {
		t.Fatal("enter in the picker should be handled")
	}
	if m.topModal() != nil {
		t.Fatal("the picker should close on enter")
	}
	if m.loc.Lang != "zh" {
		t.Fatalf("loc = %q after enter on zh, want zh", m.loc.Lang)
	}

	// Reopen: a query with no match keeps the window open (enter on the
	// empty face is a no-op); esc clears the query, a second esc closes
	// — and neither esc touches the language.
	m.localSlash("language", "")
	lm = m.topModal().(*languageModal)
	lm.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("zz")})
	if got := lm.filtered(); len(got) != 0 {
		t.Fatalf("filter zz = %v, want no match", got)
	}
	m.handleModalKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.topModal() == nil {
		t.Fatal("enter on the no-match face must keep the window open")
	}
	m.handleModalKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.topModal() == nil {
		t.Fatal("the first esc clears the query and keeps the window open")
	}
	if lm.ed.string() != "" {
		t.Fatalf("query after the first esc = %q, want cleared", lm.ed.string())
	}
	m.handleModalKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.topModal() != nil {
		t.Fatal("the second esc must close the window")
	}
	if m.loc.Lang != "zh" {
		t.Fatalf("loc = %q after esc closes, want zh kept", m.loc.Lang)
	}
}

// TestLanguageBootDetection pins the boot path: a zh locale environment
// loads the catalog and the UI starts in Chinese; a language without a
// catalog degrades to the English face.
func TestLanguageBootDetection(t *testing.T) {
	dir, _ := langTestEnv(t)
	t.Setenv("LANG", "zh_CN.UTF-8")

	a := app.New("http://127.0.0.1:3999")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a.Start(ctx)
	m := NewModel(a)
	if m.loc.Lang != "zh" {
		t.Fatalf("boot locale = %q, want zh from the environment", m.loc.Lang)
	}
	if len(m.toasts) == 0 || !strings.Contains(m.toasts[len(m.toasts)-1].text, "中文") {
		t.Errorf("boot toast = %v, want the Chinese language note", m.toasts)
	}
	// A language with no catalog file falls back to English.
	t.Setenv("DSH_LOCALES", filepath.Join(dir, "empty"))
	t.Setenv("LANG", "de_DE.UTF-8")
	m2 := NewModel(a)
	if m2.loc.Lang != "en" {
		t.Fatalf("boot locale = %q, want the English fallback", m2.loc.Lang)
	}
}

// TestLanguageMenuEntry pins the /language slot in the slash menu: the
// command list carries it and the menu renders it.
func TestLanguageMenuEntry(t *testing.T) {
	langTestEnv(t)
	a := app.New("http://127.0.0.1:3999")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a.Start(ctx)
	m := NewModel(a)
	found := false
	for _, c := range m.mergedCmds {
		if c.Name == "language" {
			found = true
		}
	}
	if !found {
		t.Fatal("/language missing from the command menu list")
	}
	m.W, m.H = 100, 30
	for _, r := range []rune{'/', 'l', 'a', 'n'} {
		m.inp.insertRune(r)
	}
	m.inp.refreshMenu(m.mergedCmds)
	if !m.inp.menuOpen {
		t.Fatal("typing /lan did not open the menu")
	}
}

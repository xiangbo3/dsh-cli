// Package config tests: the precedence ladder and the silent-failure
// semantics of an optional preference file.
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, File), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestResolveURLPrecedence pins --url/$DSH_URL > config.json > DefaultURL.
func TestResolveURLPrecedence(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DSH_CLI_HOME", dir)
	// Absent file: the default applies.
	if got := ResolveURL(""); got != DefaultURL {
		t.Fatalf("no file: ResolveURL = %q, want %q", got, DefaultURL)
	}
	// An explicit value beats the default.
	if got := ResolveURL("http://explicit:1"); got != "http://explicit:1" {
		t.Fatalf("explicit: ResolveURL = %q", got)
	}
	// A configured file beats the default.
	writeConfig(t, dir, `{"url": "http://cfg:9000"}`)
	if got := ResolveURL(""); got != "http://cfg:9000" {
		t.Fatalf("config file: ResolveURL = %q, want the file value", got)
	}
	// An explicit value beats the file.
	if got := ResolveURL("http://flag:1"); got != "http://flag:1" {
		t.Fatalf("explicit over file: ResolveURL = %q", got)
	}
	// An empty url in the file is the same as no file.
	writeConfig(t, dir, `{"url": ""}`)
	if got := ResolveURL(""); got != DefaultURL {
		t.Fatalf("empty file url: ResolveURL = %q, want %q", got, DefaultURL)
	}
}

// TestSaveLanguageRoundTrip pins the default-language persistence: a
// Load-mutate-Save round trip keeps the url and stores the language for
// the startup default (i18n.LoadDefault).
func TestSaveLanguageRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DSH_CLI_HOME", dir)
	writeConfig(t, dir, `{"url": "http://cfg:9000"}`)
	c := Load()
	if c.Language != "" {
		t.Fatalf("pre-set language = %q, want empty", c.Language)
	}
	c.Language = "zh"
	if err := Save(c); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if c2 := Load(); c2.Language != "zh" || c2.URL != "http://cfg:9000" {
		t.Fatalf("round trip = %+v, want the url kept and language zh", c2)
	}
}

// TestLoadMalformedIsSilent pins that the preference file never becomes a
// boot error: a bad parse or unknown future keys degrade to defaults.
func TestLoadMalformedIsSilent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DSH_CLI_HOME", dir)
	writeConfig(t, dir, "{not json")
	if c := Load(); c.URL != "" {
		t.Fatalf("malformed file: Load = %+v, want empty", c)
	}
	writeConfig(t, dir, `{"url": "http://ok:1", "future": 42}`)
	if c := Load(); c.URL != "http://ok:1" {
		t.Fatalf("unknown key: Load = %+v, want the url kept", c)
	}
}

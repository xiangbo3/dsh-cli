// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

// Package config tests: the precedence ladder and the silent-failure
// semantics of an optional preference file.
package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"dsh-cli/internal/version"
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

// TestPlainHTTP pins the clear-transport detection shared by the one-shot
// stderr note and the TUI's persistent status-bar marker.
func TestPlainHTTP(t *testing.T) {
	cases := []struct {
		u    string
		want bool
	}{
		{u: "http://10.0.0.5:3080", want: true},
		{u: "http://host.example:3080", want: true},
		{u: "https://host.example:3080", want: false},
		{u: "http://127.0.0.1:3080", want: false},
		{u: "http://[::1]:3080", want: false},
		{u: "http://localhost:3080", want: false},
		{u: "", want: false},
		{u: "not a url", want: false},
		{u: "http://", want: false},
	}
	for _, c := range cases {
		if got := PlainHTTP(c.u); got != c.want {
			t.Errorf("PlainHTTP(%q) = %v, want %v", c.u, got, c.want)
		}
	}
}

// TestConfigVersionMigration pins the _version gate: a stale config file
// (no marker, or stamped by another dsh-cli version) is completed with
// the current version's options and re-stamped, keeping user values; a
// current file is left byte-identical; a malformed file is left alone.
func TestConfigVersionMigration(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DSH_CLI_HOME", dir)

	// Unmarked file from an older build: values kept, marker added.
	writeConfig(t, dir, "{ \"url\": \"http://old:1\", \"language\": \"zh\" }")
	c := Load()
	if c.URL != "http://old:1" || c.Language != "zh" || c.Version != version.Version {
		t.Fatalf("migrated config = %+v, want the values kept and the current version", c)
	}
	onDiskB, err := os.ReadFile(filepath.Join(dir, File))
	if err != nil {
		t.Fatal(err)
	}
	var onDisk Config
	if err := json.Unmarshal(onDiskB, &onDisk); err != nil {
		t.Fatal(err)
	}
	if onDisk.Version != version.Version {
		t.Errorf("stored file version = %q, want %q", onDisk.Version, version.Version)
	}

	// File stamped by another version: re-stamped, values kept.
	writeConfig(t, dir, "{ \"_version\": \"0.0.1\", \"url\": \"http://old2:1\" }")
	c = Load()
	if c.URL != "http://old2:1" || c.Version != version.Version {
		t.Fatalf("re-stamped config = %+v, want the url kept and the current version", c)
	}

	// A current file survives Load byte-identical (no rewrite churn).
	cur := Config{Version: version.Version, URL: "http://cur:1"}
	curB, err := json.MarshalIndent(cur, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, File), append(curB, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	Load()
	b, err := os.ReadFile(filepath.Join(dir, File))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b, append(curB, '\n')) {
		t.Errorf("Load rewrote a current-version file: %s", b)
	}

	// Malformed: the silent-default path must not clobber the file.
	writeConfig(t, dir, "{not json")
	Load()
	b, err = os.ReadFile(filepath.Join(dir, File))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "{not json" {
		t.Errorf("Load rewrote a malformed file: %s", b)
	}
}

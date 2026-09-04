// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

// Fill's URL defaulting: an empty Opts.URL falls back to the config file,
// then the built-in default (the flag layer already merged --url/$DSH_URL
// into Opts.URL, so here "empty" means neither was given).
package oneoff

import (
	"os"
	"path/filepath"
	"testing"

	"dsh-cli/internal/config"
)

// TestFillURLFromConfig pins the config-file leg of the URL precedence
// ladder at the Fill layer.
func TestFillURLFromConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DSH_CLI_HOME", dir)
	// No file: the built-in default.
	o := Opts{}
	if err := o.Fill(); err != nil {
		t.Fatal(err)
	}
	if o.URL != config.DefaultURL {
		t.Fatalf("no file: Fill URL = %q, want %q", o.URL, config.DefaultURL)
	}
	// A configured file wins over the default.
	if err := os.WriteFile(filepath.Join(dir, config.File),
		[]byte(`{"url": "http://cfg:9000"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	o = Opts{}
	if err := o.Fill(); err != nil {
		t.Fatal(err)
	}
	if o.URL != "http://cfg:9000" {
		t.Fatalf("config file: Fill URL = %q, want the file value", o.URL)
	}
	// A parsed-but-invalid file URL fails fast with the clear message.
	if err := os.WriteFile(filepath.Join(dir, config.File),
		[]byte(`{"url": "not a url"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	o = Opts{}
	if err := o.Fill(); err == nil {
		t.Fatalf("bad file url: Fill must fail, got URL %q", o.URL)
	}
}

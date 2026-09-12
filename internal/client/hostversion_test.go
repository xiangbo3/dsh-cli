// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package client

import (
	"os"
	"path/filepath"
	"testing"
)

// TestHostVersionFallback pins the launcher probe: the first
// version-shaped token of the dsh --version printout is what the
// snapshot reports (either print shape parses), and the probe runs once
// per process (a missing binary caches as "").
func TestHostVersionFallback(t *testing.T) {
	reset := func() {
		hostVerMu.Lock()
		hostVerTried, hostVer = false, ""
		hostVerMu.Unlock()
	}
	reset()
	t.Cleanup(reset)

	script := filepath.Join(t.TempDir(), "fake-dsh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho \"dsh 9.9.9-test\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envBinLocal, script)

	if got := hostVersionFallback(); got != "9.9.9-test" {
		t.Fatalf("fallback = %q, want 9.9.9-test", got)
	}
	// A second probe serves the cached value without re-running the
	// launcher.
	if got := hostVersionFallback(); got != "9.9.9-test" {
		t.Fatalf("cached probe = %q, want 9.9.9-test", got)
	}

	// A missing binary reads as "" (and caches the miss).
	t.Setenv(envBinLocal, filepath.Join(t.TempDir(), "nowhere"))
	reset()
	if got := hostVersionFallback(); got != "" {
		t.Fatalf("missing binary: fallback = %q, want empty", got)
	}
}

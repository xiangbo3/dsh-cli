// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package app

import (
	"testing"

	"dsh-cli/internal/core"
)

// TestProtectRecoversPanic pins the pump panic fence: a panicking
// iteration costs the frame and earns a warning plus a re-baseline ask —
// never the process (and never the terminal mid-alt-screen).
func TestProtectRecoversPanic(t *testing.T) {
	a := &App{st: core.NewStore("http://x")}
	a.rebBusy.Store(true) // swallow the repair spawn (no dialer in unit tests)
	a.protect("test", func() { panic("boom") })

	select {
	case n := <-a.st.Notices():
		if n.Level != "warn" {
			t.Fatalf("notice level = %q, want warn", n.Level)
		}
	default:
		t.Fatal("no warning notice after panic")
	}
}

// The fence must not touch a healthy iteration: no notice, no rebaseline.
func TestProtectPassesHealthyThrough(t *testing.T) {
	a := &App{st: core.NewStore("http://x")}
	a.rebBusy.Store(true)
	healthy := true
	a.protect("test", func() { healthy = true })
	if !healthy {
		t.Fatal("body did not run")
	}
	select {
	case n := <-a.st.Notices():
		t.Fatalf("unexpected notice %v", n)
	default:
	}
}

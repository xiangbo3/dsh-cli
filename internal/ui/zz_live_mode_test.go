// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dsh-cli/internal/app"
	"dsh-cli/internal/protocol"
)

// liveToken reads the stored launch token from the real home config (the
// package TestMain redirects DSH_CLI_HOME to a throwaway dir).
func liveToken(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("home dir: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(home, ".dsh-cli", "config.json"))
	if err != nil {
		t.Skipf("read config: %v", err)
	}
	var c struct {
		Token string `json:"token"`
	}
	if json.Unmarshal(raw, &c) != nil || c.Token == "" {
		t.Skip("no stored launch token")
	}
	return c.Token
}

// TestZZLiveModeSwitch drives the real /mode path against the live host:
// the roster re-baselines (the upgraded build carries each session preset
// in the row's projections), a blank session picks a different mode, and
// the switch must land as an ok notice, not an RPC error.
func TestZZLiveModeSwitch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	a := app.NewWith("http://127.0.0.1:3080", liveToken(t))
	a.Start(ctx)
	m := NewModel(a)
	m.splashOff = true

	roster, err := a.Presets(ctx)
	if err != nil {
		t.Skipf("no live server (%v)", err)
	}
	target := "ptc"
	found := false
	for i := range roster.Presets {
		if roster.Presets[i].Id == target {
			found = true
		}
	}
	if !found {
		t.Skip("live roster has no ptc preset")
	}

	// Roster re-baseline: on the upgraded build the preset arrives in the
	// row's projection, so at least one listed session must carry one.
	sess, err := a.Client().ListSessions(ctx)
	if err != nil {
		t.Skipf("list failed: %v", err)
	}
	m.st.SetSessions(sess.Items)
	var blankId string
	for _, it := range sess.Items {
		if it.Blank {
			if snap := m.st.Get(it.SessionId); snap != nil && snap.Summary.AgentPreset != "" {
				blankId = it.SessionId
				break
			}
		}
	}
	if blankId == "" {
		// No listed blank session carries a preset: create one.
		created, err := a.CreateSession(ctx, protocol.SessionCreateRequest{Cwd: "/tmp", AgentPreset: "standard"})
		if err != nil {
			t.Skipf("create failed: %v", err)
		}
		blankId = created
		m.st.SetSessions(sess.Items)
	}
	m.st.SetActive(blankId)

	snap := m.st.Get(blankId)
	if snap != nil && snap.Summary.AgentPreset == target {
		// Re-picking the current mode is a no-op: aim at another one.
		target = "minimal"
	}
	cmd := m.cmdSelectModeByName(target)
	msg := cmd()
	if e, ok := msg.(rpcErrMsg); ok {
		t.Fatalf("cmd error: %v", e.err)
	}
	done := false
	deadline := time.Now().Add(8 * time.Second)
	for !done {
		select {
		case n := <-m.st.Notices():
			if strings.Contains(n.Text, "mode →") {
				if n.Level != "ok" {
					t.Fatalf("mode switch notice level = %s: %s", n.Level, n.Text)
				}
				done = true
			}
		case <-time.After(time.Until(deadline)):
			t.Fatal("no mode switch notice")
		}
	}
}

// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"encoding/json"
	"testing"

	"dsh-cli/internal/protocol"
)

// TestNextPresetUsesHostCatalog pins the cycle's table source: the host's
// process-level catalog (the session projection publishes only the current
// preset), with the projection's inline options ahead of it for older
// builds and the standard table as the last resort.
func TestNextPresetUsesHostCatalog(t *testing.T) {
	m := newPermTestModel(t)
	// Nothing known: the standard table's first preset.
	if got := m.nextPreset("s1"); got != "read-only" {
		t.Fatalf("nextPreset with no state = %q, want read-only", got)
	}
	// A stock two-preset deployment plus a live Auto: read-only is not a
	// preset this host has, so it must never be sent.
	m.st.SetPermissionCatalog([]protocol.PermissionOption{
		{Value: "workspace-write", Name: "workspace-write"},
		{Value: "danger-full-access", Name: "danger-full-access"},
		{Value: "auto", Name: "Auto"},
	}, true)
	if got := m.nextPreset("s1"); got != "workspace-write" {
		t.Fatalf("nextPreset with no baseline = %q, want the catalog's first preset", got)
	}
	// From a real current preset the cycle walks the catalog order.
	m.st.MuxProjection("s1", "permissions", 10, json.RawMessage(`{"currentValue":"danger-full-access"}`))
	if got := m.nextPreset("s1"); got != "auto" {
		t.Fatalf("nextPreset after full access = %q, want auto", got)
	}
	// A derived "custom" knob pair (a knob moved outside any preset, e.g.
	// the approval policy alone) is not a switch target: the cycle restarts.
	m.st.MuxProjection("s1", "permissions", 11, json.RawMessage(`{"currentValue":"custom"}`))
	if got := m.nextPreset("s1"); got != "workspace-write" {
		t.Fatalf("nextPreset from custom = %q, want the catalog's first preset", got)
	}
	// The projection's inline options (an older build) outrank the catalog.
	m.st.MuxProjection("s1", "permissions", 12, json.RawMessage(
		`{"options":[{"value":"a","name":"a"},{"value":"b","name":"b"}],"currentValue":"a"}`))
	if got := m.nextPreset("s1"); got != "b" {
		t.Fatalf("nextPreset with inline options = %q, want b", got)
	}
}

// TestPermReadyGatesTheColdPress pins the cold-boot gate: a cycle may only
// run off real state, because guessing the standard table can name a
// preset this deployment does not have.
func TestPermReadyGatesTheColdPress(t *testing.T) {
	m := newPermTestModel(t)
	if m.permReady("s1") {
		t.Fatal("ready before the permissions baseline landed")
	}
	if _, cmd := m.Update(keyCtrlP()); cmd == nil {
		t.Fatal("the cold press must queue the readiness pull")
	}
	if _, cmd := m.Update(keyCtrlP()); cmd != nil {
		t.Fatal("a second press while the pull is in flight must not stack another")
	}
	// The baseline alone is not enough while the preset table is unknown.
	m.st.MuxProjection("s1", "permissions", 10, json.RawMessage(`{"currentValue":"workspace-write"}`))
	if m.permReady("s1") {
		t.Fatal("ready before the deployment's preset table is known")
	}
	// The deployment's answer that it publishes no catalog counts as known.
	m.st.SetPermissionCatalog(nil, true)
	if !m.permReady("s1") {
		t.Fatal("a catalog-less deployment must be ready to cycle")
	}
}

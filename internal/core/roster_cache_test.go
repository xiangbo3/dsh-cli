// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package core

import (
	"encoding/json"
	"testing"

	"dsh-cli/internal/protocol"
)

// The roster row carries the session's projection baseline: titles (and
// permissions / context pressure) must be readable the moment session.list
// lands, without the per-session tail load.
func TestStoreSetSessionsDecodesRosterProjections(t *testing.T) {
	pb, err := json.Marshal(protocol.ProjectionsBlock{
		AsOfSeq: 42,
		Values: map[string]json.RawMessage{
			"title": mustJSON(`"roster title"`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := NewStore("http://x")
	s.SetSessions([]protocol.SessionSummary{{
		SessionId: "s1", UpdatedAt: 10, Cwd: "/home/x/proj", Projections: pb,
	}})
	if got := s.TitleFor("s1"); got != "roster title" {
		t.Fatalf("roster title = %q", got)
	}
	snap := s.Get("s1")
	if snap == nil || snap.Title != "roster title" {
		t.Fatalf("snapshot title = %+v", snap)
	}
	// A live projection at a higher seq still wins over the roster value.
	s.MuxProjection("s1", "title", 50, mustJSON(`"live"`))
	if got := s.TitleFor("s1"); got != "live" {
		t.Fatalf("live title lost to roster: %q", got)
	}
	// A tail page older than the live state must not demote it
	// (higher-seq-wins against the roster baseline too).
	s.LoadTail("s1", &protocol.HistoryResponse{
		Projections: &protocol.ProjectionsBlock{
			AsOfSeq: 30,
			Values:  map[string]json.RawMessage{"title": mustJSON(`"stale"`)},
		},
	})
	if got := s.TitleFor("s1"); got != "live" {
		t.Fatalf("stale tail page demoted the title: %q", got)
	}
	// A roster re-baseline with no projections must not erase a known title.
	s.SetSessions([]protocol.SessionSummary{{SessionId: "s1", UpdatedAt: 11}})
	if got := s.TitleFor("s1"); got != "live" {
		t.Fatalf("re-baseline erased the title: %q", got)
	}
}

// The creation-time session→workspace binding resolves the chip before the
// registry row's session account or the cwd path names the session.
func TestStoreSeedWorkspaceBinding(t *testing.T) {
	s := NewStore("http://x")
	s.SetWorkspaces([]protocol.WorkspaceView{{
		WorkspaceId: "w1", Path: "/home/x/proj", Title: "proj",
		// SessionIds deliberately empty: the row does not account s1 yet.
	}}, nil)
	s.SeedWorkspace("s1", "w1")
	if ws := s.WorkspaceForSession("s1"); ws == nil || ws.WorkspaceId != "w1" {
		t.Fatalf("binding lookup = %+v", ws)
	}
	// A removed workspace clears its bindings.
	s.WorkspaceRemove("w1")
	if ws := s.WorkspaceForSession("s1"); ws != nil {
		t.Fatalf("stale binding survived workspace removal: %+v", ws)
	}
	// A re-baseline that drops the workspace clears the bindings too.
	s.SeedWorkspace("s2", "w1")
	s.SetWorkspaces(nil, nil)
	if ws := s.WorkspaceForSession("s2"); ws != nil {
		t.Fatalf("stale binding survived re-baseline: %+v", ws)
	}
	// Session removal clears the binding.
	s.SetWorkspaces([]protocol.WorkspaceView{{WorkspaceId: "w2", Path: "/p2", Title: "p2"}}, nil)
	s.SeedWorkspace("s3", "w2")
	s.HostFrame(&protocol.HostFrame{Type: protocol.FHostSessionRemoved, SessionId: "s3"})
	if ws := s.WorkspaceForSession("s3"); ws != nil {
		t.Fatalf("stale binding survived session removal: %+v", ws)
	}
}

// The boot cache warms the chrome: titles and workspace resolution work
// before any live baseline; live baselines then overwrite row by row.
func TestStoreBootCache(t *testing.T) {
	s := NewStore("http://x")
	if s.RosterReady() || s.WorkspacesReady() {
		t.Fatal("an empty store must not be ready")
	}
	if !s.CacheWorkspaces([]protocol.WorkspaceView{{
		WorkspaceId: "w1", Path: "/home/x/proj", Title: "proj",
		SessionIds: []string{"s2"},
	}}, nil) {
		t.Fatal("cache registry install failed")
	}
	if !s.CacheRoster([]CacheRow{
		{Id: "s1", UpdatedAt: 10, Cwd: "/home/x/proj", Title: "cached title"},
		{Id: "s2", UpdatedAt: 9, Blank: true},
	}) {
		t.Fatal("cache roster install failed")
	}
	if !s.RosterReady() || !s.WorkspacesReady() {
		t.Fatal("cache must make the store ready")
	}
	if s.RosterBaselined() || s.WorkspacesBaselined() {
		t.Fatal("cache must not count as a live baseline")
	}
	if got := s.TitleFor("s1"); got != "cached title" {
		t.Fatalf("cached title = %q", got)
	}
	// s1 resolves by the cached cwd path match, s2 by the cached account.
	if ws := s.WorkspaceForSession("s1"); ws == nil || ws.WorkspaceId != "w1" {
		t.Fatalf("cwd resolution from cache = %+v", ws)
	}
	if ws := s.WorkspaceForSession("s2"); ws == nil || ws.WorkspaceId != "w1" {
		t.Fatalf("account resolution from cache = %+v", ws)
	}
	// Rows exist now: a second install is a no-op.
	if s.CacheRoster(nil) {
		t.Fatal("second cache install must be a no-op")
	}
	// The live roster overwrites: removed sessions drop, new ones land.
	s.SetSessions([]protocol.SessionSummary{{SessionId: "s3", UpdatedAt: 20, Cwd: "/home/x/proj"}})
	if !s.RosterBaselined() {
		t.Fatal("live baseline must report baselined")
	}
	if s.Get("s1") != nil {
		t.Fatal("removed session must drop from the live baseline")
	}
	// The live registry overwrites the cached one (full replace).
	s.SetWorkspaces([]protocol.WorkspaceView{{WorkspaceId: "w2", Path: "/home/x/proj", Title: "proj2"}}, nil)
	if !s.WorkspacesBaselined() {
		t.Fatal("live registry must report baselined")
	}
	if ws := s.WorkspaceForSession("s3"); ws == nil || ws.WorkspaceId != "w2" {
		t.Fatalf("live registry resolution = %+v", ws)
	}
}

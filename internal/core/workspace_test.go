package core

import (
	"encoding/json"
	"strings"
	"testing"

	"dsh-cli/internal/protocol"
)

func TestStoreWorkspaceRegistryAndFrames(t *testing.T) {
	s := NewStore("http://x")
	if s.WorkspacesBaselined() {
		t.Fatal("fresh store must not report a workspace baseline")
	}
	s.SetWorkspaces([]protocol.WorkspaceView{
		{WorkspaceId: "w1", Path: "/tmp/a", Title: "a", SessionIds: []string{"s1"}},
		{WorkspaceId: "w2", Path: "/tmp/b", Title: "b"},
	}, nil)
	if got := len(s.Workspaces()); got != 2 {
		t.Fatalf("workspaces = %d, want 2", got)
	}
	if s.Workspaces()[0].WorkspaceId != "w1" {
		t.Fatalf("baseline order broken: %+v", s.Workspaces())
	}

	// host/workspace-changed upserts a new row at the tail.
	raw, _ := json.Marshal(protocol.WorkspaceView{
		WorkspaceId: "w3", Path: "/tmp/c", Title: "c",
		SessionIds: []string{"s9"},
	})
	s.HostFrame(&protocol.HostFrame{Type: protocol.FHostWorkspaceChanged, Workspace: raw})
	if got := len(s.Workspaces()); got != 3 {
		t.Fatalf("after upsert = %d, want 3", got)
	}
	if s.Workspaces()[2].WorkspaceId != "w3" {
		t.Fatalf("upsert order: %+v", s.Workspaces())
	}

	// host/workspace-order-changed installs a complete display order.
	s.HostFrame(&protocol.HostFrame{
		Type: protocol.FHostOrderChanged, WorkspaceIds: []string{"w3", "w1", "w2"},
	})
	got := make([]string, 0, 3)
	for _, w := range s.Workspaces() {
		got = append(got, w.WorkspaceId)
	}
	if strings.Join(got, ",") != "w3,w1,w2" {
		t.Fatalf("order = %v", got)
	}

	// host/workspace-removed drops one row.
	s.HostFrame(&protocol.HostFrame{Type: protocol.FHostWorkspaceRemoved, WorkspaceId: "w2"})
	if w := s.WorkspaceByID("w2"); w != nil {
		t.Fatalf("w2 still present: %+v", w)
	}

	// host/archived-sessions-changed replaces the archive set.
	s.HostFrame(&protocol.HostFrame{
		Type: protocol.FHostArchived, ArchivedSessionIds: []string{"s4"},
	})
	if !s.IsArchived("s4") || s.IsArchived("s1") {
		t.Fatalf("archived = %v", s.ArchivedIDs())
	}
}

func TestStoreWorkspaceForSession(t *testing.T) {
	s := NewStore("http://x")
	s.SetWorkspaces([]protocol.WorkspaceView{
		{WorkspaceId: "w1", Path: "/tmp/proj", Title: "proj", SessionIds: []string{"s1"}},
	}, nil)

	// Accounted membership resolves even before any session rows exist.
	if w := s.WorkspaceForSession("s1"); w == nil || w.WorkspaceId != "w1" {
		t.Fatalf("accounted: %+v", w)
	}

	// A session whose cwd matches a workspace path resolves too (the
	// attach frame may not have landed).
	s.SetSessions([]protocol.SessionSummary{
		{SessionId: "s7", Cwd: "/tmp/proj/"},
	})
	if w := s.WorkspaceForSession("s7"); w == nil || w.WorkspaceId != "w1" {
		t.Fatalf("cwd fallback: %+v", w)
	}
	if w := s.WorkspaceForPath("/tmp/proj"); w == nil || w.WorkspaceId != "w1" {
		t.Fatalf("path lookup: %+v", w)
	}
	if w := s.WorkspaceForSession("nope"); w != nil {
		t.Fatalf("unknown session resolved: %+v", w)
	}
	if w := s.WorkspaceForPath("/elsewhere"); w != nil {
		t.Fatalf("unknown path resolved: %+v", w)
	}
}

func TestStoreSideGroupingAndArchivedRoster(t *testing.T) {
	s := NewStore("http://x")
	s.SetSessions([]protocol.SessionSummary{
		{SessionId: "s1", Cwd: "/tmp/proj", UpdatedAt: 30},
		{SessionId: "s2", Cwd: "/other", UpdatedAt: 20},
		{SessionId: "s3", Cwd: "/tmp/proj", UpdatedAt: 10},
		{SessionId: "s4", Cwd: "/archived", UpdatedAt: 90},
	})
	s.SetWorkspaces([]protocol.WorkspaceView{
		{WorkspaceId: "w1", Path: "/tmp/proj", Title: "proj",
			SessionIds: []string{"s1", "s3"}}, // manual account order
	}, []string{"s4"})

	// The roster hides the archived session and stamps workspace fields.
	rows := s.Roster()
	if len(rows) != 3 {
		t.Fatalf("roster = %d rows, want 3 (archived hidden)", len(rows))
	}
	byID := map[string]Row{}
	for _, r := range rows {
		byID[r.Id] = r
		if r.Id == "s1" || r.Id == "s3" {
			if r.WsID != "w1" || r.WsTitle != "proj" {
				t.Fatalf("%s workspace stamp missing: %+v", r.Id, r)
			}
		}
	}
	if _, ok := byID["s4"]; ok {
		t.Fatal("archived s4 leaked into the roster")
	}

	// Sidebar: workspace group in account order, then the ungrouped tail.
	side := s.Side()
	var seq []string
	for _, r := range side {
		if r.Header {
			seq = append(seq, "#"+r.Title)
		} else {
			seq = append(seq, r.Row.Id)
		}
	}
	if strings.Join(seq, ",") != "#proj,s1,s3,#ungrouped,s2" {
		t.Fatalf("side layout = %v", seq)
	}
	if got := s.SideIDs(); strings.Join(got, ",") != "s1,s3,s2" {
		t.Fatalf("SideIDs = %v", got)
	}

	// With no registered workspace the list falls back to a flat roster.
	s2 := NewStore("http://x")
	s2.SetSessions([]protocol.SessionSummary{{SessionId: "s5", Cwd: "/x"}})
	side2 := s2.Side()
	if len(side2) != 1 || side2[0].Header {
		t.Fatalf("flat fallback: %+v", side2)
	}
}

// TestStoreCurrentSideScope pins the session window's scope: the list is
// the active session's workspace (header + its sessions in account
// order); sessions the workspace rosters do not account (cwd-only
// attaches, no workspace at all) keep the full side, so the window can
// never hide its own session.
func TestStoreCurrentSideScope(t *testing.T) {
	s := NewStore("http://x")
	s.SetWorkspaces([]protocol.WorkspaceView{
		{WorkspaceId: "w1", Path: "/tmp/a", Title: "alpha", SessionIds: []string{"s1", "s2"}},
		{WorkspaceId: "w2", Path: "/tmp/b", Title: "beta", SessionIds: []string{"s3"}},
	}, nil)
	s.SetSessions([]protocol.SessionSummary{
		{SessionId: "s1", Cwd: "/tmp/a"},
		{SessionId: "s2", Cwd: "/tmp/a"},
		{SessionId: "s3", Cwd: "/tmp/b"},
		{SessionId: "s4", Cwd: "/tmp/loose"},
	})

	// Accounted to w1: only w1's header and sessions, in account order.
	rows := s.CurrentSide("s2")
	if len(rows) != 3 || !rows[0].Header || rows[0].Title != "alpha" || rows[0].Path != "/tmp/a" {
		t.Fatalf("scoped to s2 = %+v", rows)
	}
	if rows[1].Row.Id != "s1" || rows[2].Row.Id != "s2" {
		t.Fatalf("scoped order = %+v", rows)
	}
	if got := s.CurrentSideIDs("s1"); strings.Join(got, ",") != "s1,s2" {
		t.Fatalf("CurrentSideIDs(s1) = %v", got)
	}

	// w2 scope from its only member.
	if got := s.CurrentSideIDs("s3"); strings.Join(got, ",") != "s3" {
		t.Fatalf("CurrentSideIDs(s3) = %v", got)
	}

	// Untitled workspaces fall back to the path as the group title.
	s2 := NewStore("http://x")
	s2.SetWorkspaces([]protocol.WorkspaceView{
		{WorkspaceId: "w1", Path: "/tmp/raw", SessionIds: []string{"s1"}},
	}, nil)
	s2.SetSessions([]protocol.SessionSummary{{SessionId: "s1", Cwd: "/tmp/raw"}})
	if got := s2.CurrentSide("s1"); len(got) != 2 || !got[0].Header || got[0].Title != "/tmp/raw" {
		t.Fatalf("untitled scope = %+v", got)
	}

	// The full side is the fallback: no session at all…
	if got := len(s.CurrentSide("")); got != len(s.Side()) {
		t.Fatalf("CurrentSide(\"\") = %d rows, want the full side (%d)", got, len(s.Side()))
	}
	// …or a session no workspace accounts (cwd-only attach: s5 matches
	// /tmp/b but w2's roster does not carry it).
	s.SetSessions([]protocol.SessionSummary{
		{SessionId: "s1", Cwd: "/tmp/a"},
		{SessionId: "s2", Cwd: "/tmp/a"},
		{SessionId: "s3", Cwd: "/tmp/b"},
		{SessionId: "s4", Cwd: "/tmp/loose"},
		{SessionId: "s5", Cwd: "/tmp/b"},
	})
	if got := s.CurrentSideIDs("s5"); len(got) != 5 || got[0] != "s1" {
		t.Fatalf("cwd-only attach should keep the full roster (s5 in the ungrouped tail): %v", got)
	}
	if got := s.CurrentSideIDs("s4"); len(got) != 5 || got[4] != "s5" {
		t.Fatalf("ungrouped scope should keep the full roster: %v", got)
	}
	// The w2 scope itself stays account-only: s5 never lands in it.
	if got := s.CurrentSideIDs("s3"); strings.Join(got, ",") != "s3" {
		t.Fatalf("w2 scope must stay account-only: %v", got)
	}

	// Archiving hides from the scoped list like the full roster.
	s.ArchivedChanged([]string{"s2"})
	if got := s.CurrentSideIDs("s1"); strings.Join(got, ",") != "s1" {
		t.Fatalf("archived session leaked into the scoped list: %v", got)
	}
}

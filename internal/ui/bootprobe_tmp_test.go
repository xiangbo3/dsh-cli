// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"context"
	"fmt"

	"strings"
	"testing"
	"time"

	"dsh-cli/internal/app"
	"dsh-cli/internal/client"
	"dsh-cli/internal/protocol"

	tea "github.com/charmbracelet/bubbletea"
)

// TestBootCtrlPEndToEnd reproduces "start, then press ctrl+p immediately":
// it waits for boot to pick the active session, presses ctrl+p three times
// with pauses, and logs what the cycle actually sent. Dev probe:
// meaningful only against a live server, so skip otherwise.
func TestBootCtrlPEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	probe := client.New("http://127.0.0.1:3080")
	pctx, pcancel := context.WithTimeout(ctx, 500*time.Millisecond)
	if _, err := probe.Describe(pctx); err != nil {
		t.Skipf("no live server at 127.0.0.1:3080: %v", err)
	}
	pcancel()

	a := app.New("http://127.0.0.1:3080")
	a.Start(ctx)
	m := NewModel(a)
	m.splashOff = true // headless key test: no boot animation
	prog := tea.NewProgram(m, tea.WithoutRenderer(), tea.WithContext(ctx),
		tea.WithInput(strings.NewReader("")))
	m.SetProg(prog)

	go func() {
		prog.Send(tea.WindowSizeMsg{Width: 120, Height: 40})
		t.Log("goroutine started")
		// Wait for boot: host + active session picked.
		deadline := time.Now().Add(10 * time.Second)
		for i := 0; time.Now().Before(deadline); i++ {
			hostOK := m.st.Host() != nil
			act := m.st.Active()
			if i%20 == 0 {
				t.Logf("wait poll %d: host=%v active=%q", i, hostOK, act)
			}
			if hostOK && act != "" {
				t.Log("boot condition met")
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Logf("boot wait over: host=%v active=%q", m.st.Host() != nil, m.st.Active())
		if perm := permOf(a, a.Store().Active()); perm != nil {
			t.Logf("initial store permission: current=%q", perm.Current)
		} else {
			t.Log("initial store permission: nil")
		}
		for i := 1; i <= 3; i++ {
			time.Sleep(1500 * time.Millisecond)
			prog.Send(tea.KeyMsg{Type: tea.KeyCtrlP})
			time.Sleep(800 * time.Millisecond)
			perm := permOf(a, a.Store().Active())
			if perm == nil {
				t.Logf("press %d: store permission still nil", i)
			} else {
				t.Logf("press %d: store permission current=%q", i, perm.Current)
			}
			lastCmds(t, a, a.Store().Active())
		}
		time.Sleep(500 * time.Millisecond)
		prog.Send(tea.KeyMsg{Type: tea.KeyCtrlD})
	}()

	if _, err := prog.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func permOf(a *app.App, id string) *permView {
	snap := a.Store().Get(id)
	if snap == nil || snap.Permission == nil {
		return nil
	}
	return &permView{Current: snap.Permission.CurrentValue}
}

type permView struct{ Current string }

// lastCmds logs the most recent permission command/run events server-side.
func lastCmds(t *testing.T, a *app.App, id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := a.Client().History(ctx, id, 0, 40)
	if err != nil {
		t.Logf("history: %v", err)
		return
	}
	var lines []string
	for _, e := range resp.Events {
		ev := e.Event
		if ev.Type != "command/run" || !strings.Contains(string(ev.Data), "permission") {
			continue
		}
		data := string(ev.Data)
		arg := data
		if i := strings.Index(data, "\"args\":\""); i >= 0 {
			if j := strings.Index(data[i+2:], "\""); j >= 0 {
				arg = data[i+2 : i+2+j]
			}
		}
		lines = append(lines, fmt.Sprintf("%d:%s", ev.Seq, arg))
	}
	if len(lines) > 4 {
		lines = lines[len(lines)-4:]
	}
	t.Logf("server-side /permission commands (last 4): %v", lines)
}

// TestBootResumeLastAndHiddenList pins the startup contract: the session
// list starts hidden, and once the host probe lands the TUI resumes the
// previous session — the roster's most recent non-blank one (the web
// client's "resume last" rule, with its transcript tail loaded). A
// brand-new session in the host cwd is opened only when there is nothing
// to resume.
func TestBootResumeLastAndHiddenList(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	a := app.New("http://127.0.0.1:3080")
	a.Start(ctx)
	m := NewModel(a)
	m.splashOff = true
	if m.sideVisible {
		t.Fatal("session list should start hidden (ctrl+l shows it)")
	}

	// Host probe lands while the roster holds sessions: boot must resume
	// the most recent non-blank one (old-session — newer-blank is newer
	// but blank), not open a fresh one.
	st := m.st
	st.SetHost(&protocol.HostDescription{Version: "test", Cwd: "/tmp"})
	st.SetSessions([]protocol.SessionSummary{
		{SessionId: "newer-blank", UpdatedAt: 2, Blank: true},
		{SessionId: "old-session", UpdatedAt: 1, Blank: false},
	})
	if st.Active() != "" {
		t.Fatalf("starting active = %q (want empty)", st.Active())
	}
	_, cmd := m.Update(dirtyMsg{})
	if !m.booted {
		t.Fatal("boot should be marked done once the previous session is resumed")
	}
	if m.st.Active() != "old-session" {
		t.Fatalf("boot active = %q, want the resumed old-session", m.st.Active())
	}
	if cmd == nil {
		t.Fatal("boot should kick off the resumed session's tail load, got no cmd")
	}
	t.Logf("boot resume cmd %T", cmd)

	// A roster without a single non-blank session: boot opens a fresh
	// one in the host cwd (the ctrl+n path).
	a2 := app.New("http://127.0.0.1:3080")
	m2 := NewModel(a2)
	m2.splashOff = true
	m2.st.SetHost(&protocol.HostDescription{Version: "test", Cwd: "/tmp"})
	m2.st.SetSessions([]protocol.SessionSummary{
		{SessionId: "blank-only", UpdatedAt: 1, Blank: true},
	})
	_, cmd2 := m2.Update(dirtyMsg{})
	if cmd2 == nil {
		t.Fatal("boot with nothing to resume should kick off a fresh session creation")
	}
	if m2.st.Active() != "" {
		t.Fatalf("blank-only boot active = %q (want empty until the create lands)", m2.st.Active())
	}
	if !m2.booted {
		t.Fatal("boot should be marked done once the fresh create is in flight")
	}
	// Run the creation cmd: against a live server the new session becomes
	// active; otherwise the error-notice path fires.
	msg := cmd2()
	if c, ok := msg.(createMsg); ok && c.id != "" {
		m2.Update(msg)
		if m2.st.Active() != c.id {
			t.Fatalf("active = %q, want the new session %q", m2.st.Active(), c.id)
		}
	} else {
		t.Logf("no live server for the auto-create (message %T); error notice path", msg)
	}
}

// TestBootWorkspaceAutoSwitch pins the boot auto workspace switch: while
// the splash runs, resume-last picks the roster's most recent session and
// the app lands in that session's workspace — bottom-bar chip, scoped
// session window, one info toast — whether the workspace registry
// baselines before or after the pick; a session the registry does not
// own resolves to none (check closed, no toast, ungrouped as before).
func TestBootWorkspaceAutoSwitch(t *testing.T) {
	ws := func() []protocol.WorkspaceView {
		return []protocol.WorkspaceView{
			{WorkspaceId: "w1", Path: "/tmp/a", Title: "alpha", SessionIds: []string{"s1", "s2"}},
		}
	}
	roster := func() []protocol.SessionSummary {
		return []protocol.SessionSummary{
			{SessionId: "s1", Cwd: "/tmp/a", UpdatedAt: 3, Blank: false},
			{SessionId: "s2", Cwd: "/tmp/a", UpdatedAt: 2, Blank: false},
		}
	}
	boot := func() *Model {
		// No a.Start: a hermetic boot (the pick + switch is pure store
		// state; the tail load cmd is returned, not run).
		m := NewModel(app.New("http://127.0.0.1:3080"))
		m.splashOff = true
		m.st.SetHost(&protocol.HostDescription{Version: "test", Cwd: "/tmp/a"})
		return m
	}
	hasToast := func(m *Model, s string) bool {
		for _, tt := range m.toasts {
			if strings.Contains(tt.text, s) {
				return true
			}
		}
		return false
	}

	// 1) Registry baselined at pick time: the switch lands with the pick.
	m := boot()
	m.st.SetWorkspaces(ws(), nil)
	m.st.SetSessions(roster())
	_, _ = m.Update(dirtyMsg{})
	if m.st.Active() != "s1" {
		t.Fatalf("boot active = %q, want the resumed s1", m.st.Active())
	}
	if !m.bootWsChecked {
		t.Fatal("boot workspace switch should be closed once resolved")
	}
	if !hasToast(m, "alpha") {
		t.Fatal("boot switch should toast the workspace")
	}
	if got := m.st.CurrentSideIDs("s1"); len(got) != 2 || got[0] != "s1" || got[1] != "s2" {
		t.Fatalf("scoped window ids = %v, want the alpha members only", got)
	}
	m.W, m.H = 100, 30
	if !strings.Contains(m.statusBar(100), "alpha") {
		t.Fatal("status bar must carry the workspace chip")
	}

	// 2) Registry lands after the pick: the check stays open on the first
	// pulse and resolves on the registry's pulse.
	m2 := boot()
	m2.st.SetSessions(roster())
	_, _ = m2.Update(dirtyMsg{})
	if m2.st.Active() != "s1" {
		t.Fatalf("boot active = %q, want s1", m2.st.Active())
	}
	if m2.bootWsChecked {
		t.Fatal("check must stay open while the registry is unbaselined")
	}
	if hasToast(m2, "alpha") {
		t.Fatal("no toast before the registry resolves")
	}
	m2.st.SetWorkspaces(ws(), nil)
	_, _ = m2.Update(dirtyMsg{})
	if !m2.bootWsChecked {
		t.Fatal("check must close once the registry baselines")
	}
	if !hasToast(m2, "alpha") {
		t.Fatal("registry pulse should complete the switch with a toast")
	}

	// 3) No workspace owns the session: the check closes with no toast.
	m3 := boot()
	m3.st.SetWorkspaces(ws(), nil)
	m3.st.SetSessions([]protocol.SessionSummary{
		{SessionId: "s9", Cwd: "/elsewhere", UpdatedAt: 3, Blank: false},
	})
	_, _ = m3.Update(dirtyMsg{})
	if m3.st.Active() != "s9" {
		t.Fatalf("boot active = %q, want s9", m3.st.Active())
	}
	if !m3.bootWsChecked {
		t.Fatal("check must close even when no workspace owns the session")
	}
	if hasToast(m3, "alpha") {
		t.Fatal("ungrouped boot must not toast a workspace")
	}
}

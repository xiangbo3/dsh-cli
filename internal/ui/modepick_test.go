// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dsh-cli/internal/app"
	"dsh-cli/internal/protocol"

	"github.com/charmbracelet/bubbletea"
)

func modeRoster() []protocol.AgentPresetEntry {
	return []protocol.AgentPresetEntry{
		{Id: "standard", Trust: "system", IsDefault: true, Name: "标准模式", Description: "功能完整的编码 Agent。"},
		{Id: "code", Trust: "system", Name: "PTC 模式", Description: "Code Mode SDK。"},
		{Id: "minimal", Trust: "system"},
		{Id: "cordis", Trust: "system"},
		{Id: "my-kit", Trust: "user", Name: "My Toolkit", Description: "a user preset"},
	}
}

// TestTopBarModeLabel pins the session-header mode label (the web's
// read-only agent-preset label): the top bar names the shipped mode for a
// preset the session runs, and stays clean when the deployment recorded
// none.
func TestTopBarModeLabel(t *testing.T) {
	a := app.New(newFakeHost(t).URL)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a.Start(ctx)
	m := NewModel(a)
	m.splashOff = true
	m.W, m.H = 140, 40

	m.st.SetSessions([]protocol.SessionSummary{
		{SessionId: "s1", AgentPreset: "code"},
		{SessionId: "s2"},
	})
	m.st.SetActive("s1")
	if line := m.topBar(m.W); !strings.Contains(line, "PTC mode") {
		t.Fatalf("topBar = %q (want PTC mode label)", line)
	}
	m.st.SetActive("s2")
	if line := m.topBar(m.W); strings.Contains(line, "mode") {
		t.Fatalf("topBar with no preset = %q (want no mode label)", line)
	}
}

// TestModePickerView pins the picker rendering: canonical English names for
// shipped modes (over localized file metadata), the default and current
// marks, user trust, broken rows, and the blank-vs-ran hint.
func TestModePickerView(t *testing.T) {
	a := app.New(newFakeHost(t).URL)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a.Start(ctx)
	m := NewModel(a)
	m.splashOff = true
	m.W, m.H = 140, 40
	m.st.SetSessions([]protocol.SessionSummary{{SessionId: "s1", AgentPreset: "code"}})
	m.st.SetActive("s1")

	pk := &modePicker{presets: modeRoster(), cur: 1}
	body := strings.Join(pk.view(m, 120, 40), "\n")
	for _, want := range []string{"Standard mode", "PTC mode", "Minimal mode", "Creator mode", ""} {
		if want != "" && !strings.Contains(body, want) {
			t.Fatalf("picker missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "标准模式") || strings.Contains(body, "PTC 模式") {
		t.Fatalf("localized file metadata must not leak into rows:\n%s", body)
	}
	if !strings.Contains(body, "(default)") {
		t.Fatalf("default mark missing:\n%s", body)
	}
	// Current mark on the PTC row only.
	ptcLine := ""
	stdLine := ""
	for _, ln := range strings.Split(body, "\n") {
		if strings.Contains(ln, "PTC mode") {
			ptcLine = ln
		}
		if strings.Contains(ln, "Standard mode") {
			stdLine = ln
		}
	}
	if !strings.Contains(ptcLine, m.th.Glyph.Bullet) || !strings.Contains(ptcLine, "current") {
		t.Fatalf("PTC row missing current mark: %q", ptcLine)
	}
	if strings.Contains(stdLine, "current") {
		t.Fatalf("Standard row wrongly marked current: %q", stdLine)
	}
	if !strings.Contains(body, "(user)") {
		t.Fatalf("user trust mark missing:\n%s", body)
	}
	// A ran session (s1 has a preset, non-blank by SetSessions default?) —
	// drive both footer hints explicitly: view no longer carries the key
	// map, the popup chrome renders it from hint().
	if !strings.Contains(pk.hint(), "stages for new sessions") {
		t.Fatalf("ran-session hint missing: %q", pk.hint())
	}

	m.st.SetSessions([]protocol.SessionSummary{{SessionId: "s1", Blank: true, AgentPreset: "code"}})
	body = strings.Join(pk.view(m, 120, 40), "\n")
	if !strings.Contains(pk.hint(), "applies to this session") {
		t.Fatalf("blank-session hint missing: %q / %s", pk.hint(), body)
	}

	broken := append([]protocol.AgentPresetEntry{}, modeRoster()...)
	broken[3] = protocol.AgentPresetEntry{Id: "cordis", Trust: "system", Broken: "plugin cordis/x did not load"}
	pk = &modePicker{presets: broken, cur: 0}
	body = strings.Join(pk.view(m, 120, 40), "\n")
	if !strings.Contains(body, "plugin cordis/x did not load") {
		t.Fatalf("broken reason missing:\n%s", body)
	}
}

// modeTestServer serves the agent-preset and session.create endpoints,
// recording each create's requested preset, cwd and workspace id.
type modeTestServer struct {
	srv       *httptest.Server
	selectGot map[string]string
	created   []string
	cwd       []string
	workspace []string
}

func newModeTestServer(t *testing.T) *modeTestServer {
	t.Helper()
	ms := &modeTestServer{selectGot: map[string]string{}}
	ms.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Payload json.RawMessage `json:"payload"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/agentPreset.list":
			fmt.Fprint(w, `{"type":"server-response","result":{"ok":true,"value":{`+
				`"presets":[`+
				`{"id":"standard","trust":"system","isDefault":true},`+
				`{"id":"code","trust":"system"},`+
				`{"id":"minimal","trust":"system"},`+
				`{"id":"cordis","trust":"system"}`+
				`],"authorable":true,"hasDocument":true}}}`)
		case "/api/agentPreset.select":
			var p struct {
				SessionId   string `json:"sessionId"`
				AgentPreset string `json:"agentPreset"`
			}
			_ = json.Unmarshal(req.Payload, &p)
			ms.selectGot[p.SessionId] = p.AgentPreset
			fmt.Fprintf(w, `{"type":"server-response","result":{"ok":true,"value":{"agentPreset":%q}}}`, p.AgentPreset)
		case "/api/session.create":
			var p struct {
				AgentPreset string `json:"agentPreset"`
				Cwd         string `json:"cwd"`
				WorkspaceId string `json:"workspaceId"`
			}
			_ = json.Unmarshal(req.Payload, &p)
			ms.created = append(ms.created, p.AgentPreset)
			ms.cwd = append(ms.cwd, p.Cwd)
			ms.workspace = append(ms.workspace, p.WorkspaceId)
			fmt.Fprint(w, `{"type":"server-response","result":{"ok":true,"value":{"sessionId":"s9","agentPreset":"code"}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	return ms
}

// TestModePickBlankApplies pins the in-place switch: a blank session
// recomposes through agentPreset.select, the store converges on the host's
// answer, and nothing is staged.
func TestModePickBlankApplies(t *testing.T) {
	ms := newModeTestServer(t)
	a := app.New(ms.srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a.Start(ctx)
	m := NewModel(a)
	m.splashOff = true
	m.st.SetSessions([]protocol.SessionSummary{{SessionId: "s1", Blank: true, AgentPreset: "standard"}})
	m.st.SetActive("s1")

	entry := protocol.AgentPresetEntry{Id: "code", Trust: "system"}
	if err := m.applyModePick(context.Background(), "s1", &entry); err != nil {
		t.Fatalf("applyModePick: %v", err)
	}
	if want := "code"; ms.selectGot["s1"] != want {
		t.Fatalf("agentPreset.select got %q, want %q", ms.selectGot["s1"], want)
	}
	snap := m.st.Get("s1")
	if snap.Summary.AgentPreset != "code" {
		t.Fatalf("store preset = %q, want code", snap.Summary.AgentPreset)
	}
	if m.stagedMode != "" {
		t.Fatalf("stagedMode = %q after in-place apply", m.stagedMode)
	}
}

// TestModePickStagesWhenRan pins the web hero-chip rule: a session that has
// already run keeps its composition, so the pick stages for the next new
// session instead of failing.
func TestModePickStagesWhenRan(t *testing.T) {
	fh := newFakeHost(t)
	a := app.New(fh.URL) // private host: an accidental select RPC shows up here
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a.Start(ctx)
	m := NewModel(a)
	m.splashOff = true
	m.st.SetSessions([]protocol.SessionSummary{{SessionId: "s1", AgentPreset: "standard"}})
	m.st.SetActive("s1")

	entry := protocol.AgentPresetEntry{Id: "cordis", Trust: "system"}
	if err := m.applyModePick(context.Background(), "s1", &entry); err != nil {
		t.Fatalf("applyModePick: %v", err)
	}
	if m.stagedMode != "cordis" {
		t.Fatalf("stagedMode = %q, want cordis", m.stagedMode)
	}
	if snap := m.st.Get("s1"); snap.Summary.AgentPreset != "standard" {
		t.Fatalf("ran session must keep its preset, got %q", snap.Summary.AgentPreset)
	}

	// Picking the mode the session already runs clears any stage.
	m.stagedMode = "code"
	same := protocol.AgentPresetEntry{Id: "standard", Trust: "system"}
	if err := m.applyModePick(context.Background(), "s1", &same); err != nil {
		t.Fatalf("applyModePick(same): %v", err)
	}
	if m.stagedMode != "" {
		t.Fatalf("stagedMode = %q after same-pick", m.stagedMode)
	}
	// A ran session stages for the next session: neither pick may have
	// switched it in place.
	if fh.called(protocol.MAgentPresetSelect) {
		t.Fatal("ran-session pick must stage, not select")
	}
}

// TestNewSessionConsumesStagedMode pins the stage hand-off: the next new
// session is created from the staged mode (staged beats inherited), and
// the stage is cleared for later sessions — which then inherit the
// current session's mode.
func TestNewSessionConsumesStagedMode(t *testing.T) {
	ms := newModeTestServer(t)
	a := app.New(ms.srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a.Start(ctx)
	m := NewModel(a)
	m.splashOff = true
	m.st.SetSessions([]protocol.SessionSummary{{SessionId: "s1", AgentPreset: "standard"}})
	m.st.SetActive("s1")

	m.stagedMode = "code"
	cmd := m.cmdNewSession("")
	msg := cmd()
	if cm, ok := msg.(createMsg); ok && cm.err != nil {
		t.Fatalf("create failed: %v", cm.err)
	}
	if len(ms.created) != 1 || ms.created[0] != "code" {
		t.Fatalf("session.create presets = %v, want [code]", ms.created)
	}
	if m.stagedMode != "" {
		t.Fatalf("stagedMode = %q after hand-off", m.stagedMode)
	}
	m.cmdNewSession("")()
	// The stage is spent; the second create inherits the active
	// session's preset instead.
	if len(ms.created) != 2 || ms.created[1] != "standard" {
		t.Fatalf("second create must inherit the active preset, got %v", ms.created)
	}
}

// TestNewSessionInheritsWorkspaceAndMode pins the /new inheritance rule:
// a bare /new carries over the current session's workspace and mode (an
// explicit cwd argument still wins), a cwd owned by a registered
// workspace travels as the workspace id, and with no session active the
// host cwd + deployment default apply as before.
func TestNewSessionInheritsWorkspaceAndMode(t *testing.T) {
	ms := newModeTestServer(t)
	a := app.New(ms.srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a.Start(ctx)
	m := NewModel(a)
	m.splashOff = true
	m.st.SetHost(&protocol.HostDescription{Version: "test", Cwd: "/host"})
	m.st.SetSessions([]protocol.SessionSummary{
		{SessionId: "s1", Cwd: "/tmp/proj", AgentPreset: "code", UpdatedAt: 1},
	})
	m.st.SetActive("s1")

	// Bare /new: inherit the current session's workspace and mode.
	m.cmdNewSession("")()
	if len(ms.created) != 1 || ms.created[0] != "code" {
		t.Fatalf("first create presets = %v, want [code]", ms.created)
	}
	if ms.cwd[0] != "/tmp/proj" {
		t.Fatalf("inherited cwd = %q, want /tmp/proj", ms.cwd[0])
	}

	// An explicit cwd argument wins over the inherited workspace (the
	// mode still inherits).
	m.cmdNewSession("/elsewhere")()
	if len(ms.cwd) != 2 || ms.cwd[1] != "/elsewhere" {
		t.Fatalf("explicit cwd = %v, want /elsewhere", ms.cwd)
	}
	if ms.created[1] != "code" {
		t.Fatalf("preset with explicit cwd = %q, want code", ms.created[1])
	}

	// A cwd owned by a registered workspace travels as the workspace id
	// (the web New-Session rule: accounted from birth).
	m.st.SetWorkspaces([]protocol.WorkspaceView{
		{WorkspaceId: "w1", Path: "/tmp/proj", Title: "proj"},
	}, nil)
	m.cmdNewSession("")()
	if len(ms.workspace) != 3 || ms.workspace[2] != "w1" || ms.cwd[2] != "" {
		t.Fatalf("registered-workspace create = cwd %v workspace %v, want w1 + empty cwd", ms.cwd, ms.workspace)
	}
	if ms.created[2] != "code" {
		t.Fatalf("workspace-inherited preset = %q, want code", ms.created[2])
	}

	// A session the registry owns but whose summary carries no cwd: the
	// bound workspace's path stands in.
	m.st.SetSessions([]protocol.SessionSummary{
		{SessionId: "s2", UpdatedAt: 2},
	})
	m.st.SetWorkspaces([]protocol.WorkspaceView{
		{WorkspaceId: "w1", Path: "/tmp/proj", Title: "proj", SessionIds: []string{"s2"}},
	}, nil)
	m.st.SetActive("s2")
	m.cmdNewSession("")()
	if len(ms.workspace) != 4 || ms.workspace[3] != "w1" || ms.cwd[3] != "" {
		t.Fatalf("bound-workspace create = cwd %v workspace %v, want w1 + empty cwd", ms.cwd, ms.workspace)
	}

	// No active session: the host cwd + deployment default ("" preset)
	// apply — the boot/auto-create behavior.
	m.st.SetActive("")
	m.cmdNewSession("")()
	if len(ms.created) != 5 {
		t.Fatalf("create calls = %d, want 5", len(ms.created))
	}
	if ms.cwd[4] != "/host" || ms.created[4] != "" || ms.workspace[4] != "" {
		t.Fatalf("host-fallback create = cwd %q preset %q workspace %q, want /host + default",
			ms.cwd[4], ms.created[4], ms.workspace[4])
	}
}

// TestCmdSelectModeByName pins /mode <name>: the roster is fetched, the
// alias resolved (ptc -> code), and the switch applied on a blank session.
func TestCmdSelectModeByName(t *testing.T) {
	ms := newModeTestServer(t)
	a := app.New(ms.srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a.Start(ctx)
	m := NewModel(a)
	m.splashOff = true
	m.st.SetSessions([]protocol.SessionSummary{{SessionId: "s1", Blank: true, AgentPreset: "standard"}})
	m.st.SetActive("s1")

	cmd := m.cmdSelectModeByName("ptc")
	msg := cmd()
	if e, ok := msg.(rpcErrMsg); ok {
		t.Fatalf("cmd error: %v", e.err)
	}
	if got := ms.selectGot["s1"]; got != "code" {
		t.Fatalf("agentPreset.select got %q, want code", got)
	}
	// Unknown names notify a warning without failing the command.
	cmd = m.cmdSelectModeByName("nowhere")
	cmd()
	warn := ""
	deadline := time.Now().Add(2 * time.Second)
	for i := 0; i < 3; i++ {
		select {
		case n := <-m.st.Notices():
			if strings.Contains(n.Text, "no such mode") {
				warn = n.Text
			}
		case <-time.After(time.Until(deadline)):
			i = 3
		}
		if warn != "" {
			break
		}
	}
	if warn == "" {
		t.Fatal("unknown /mode name must notify a warning")
	}
}

// TestModePickerEnterFlow pins the full keyboard path: open the picker,
// the roster loads with the current mode marked, enter confirms through
// agentPreset.select.
func TestModePickerEnterFlow(t *testing.T) {
	ms := newModeTestServer(t)
	a := app.New(ms.srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a.Start(ctx)
	m := NewModel(a)
	m.splashOff = true
	m.st.SetSessions([]protocol.SessionSummary{{SessionId: "s1", Blank: true, AgentPreset: "minimal"}})
	m.st.SetActive("s1")

	openCmd := m.openModePicker()
	msg := openCmd() // loads roster
	if pl, ok := msg.(presetsLoadedMsg); ok {
		m.Update(pl)
	} else {
		t.Fatalf("openModePicker delivered %T", msg)
	}
	pk, ok := m.topModal().(*modePicker)
	if !ok {
		t.Fatalf("top modal = %T, want *modePicker", m.topModal())
	}
	if pk.loading {
		t.Fatal("picker still loading after roster landed")
	}
	if pk.cur != 2 { // minimal is index 2 in the served roster
		t.Fatalf("current row = %d, want 2 (minimal)", pk.cur)
	}
	// Move to code and confirm; the returned cmd runs the select RPC.
	pk.cur = 1
	enterCmd, handled := m.handleModalKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !handled {
		t.Fatal("enter on the mode picker must be handled")
	}
	msg = enterCmd()
	if e, ok := msg.(rpcErrMsg); ok {
		t.Fatalf("select failed: %v", e.err)
	}
	if len(m.mods) != 0 {
		t.Fatal("picker should close after confirming")
	}
	if got := ms.selectGot["s1"]; got != "code" {
		t.Fatalf("agentPreset.select got %q, want code", got)
	}
	if snap := m.st.Get("s1"); snap.Summary.AgentPreset != "code" {
		t.Fatalf("store preset = %q, want code", snap.Summary.AgentPreset)
	}
}

// TestAgentPresetSelectedEvent pins the fold of the host's committed-switch
// event so other clients' picks converge into the store.
func TestAgentPresetSelectedEvent(t *testing.T) {
	a := app.New(newFakeHost(t).URL)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a.Start(ctx)
	m := NewModel(a)
	m.splashOff = true
	m.st.SetSessions([]protocol.SessionSummary{{SessionId: "s1", AgentPreset: "standard"}})

	data, _ := json.Marshal(map[string]string{"agentPreset": "minimal"})
	ev := &protocol.SessionEvent{Type: "agent-preset/selected", Seq: 5, Time: 5, Data: data}
	m.st.Event("s1", ev)
	if snap := m.st.Get("s1"); snap.Summary.AgentPreset != "minimal" {
		t.Fatalf("store preset = %q, want minimal", snap.Summary.AgentPreset)
	}
}

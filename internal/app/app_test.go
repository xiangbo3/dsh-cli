// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"dsh-cli/internal/protocol"
)

// TestMain disables the boot cache for this test binary: the httptest
// hosts land real baselines, and without the gate the fixture rows would
// persist into — and later load out of — the developer's real ~/.dsh-cli.
func TestMain(m *testing.M) {
	os.Setenv("DSH_CLI_NO_BOOT_CACHE", "1")
	os.Exit(m.Run())
}

func writeRPCValue(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	env := protocol.Envelope{
		Type:   protocol.TypeServerResponse,
		RpcId:  "test-rpc",
		Result: &protocol.Result{Ok: true},
	}
	env.Result.Value = raw
	body, _ := json.Marshal(env)
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(body); err != nil {
		t.Fatal(err)
	}
}

// workspaceHost serves the registry, roster and create endpoints of a DSH
// host with one registered workspace and one blank session in it.
func workspaceHost(t *testing.T, createSeen *map[string]any) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/workspace.list", func(w http.ResponseWriter, r *http.Request) {
		writeRPCValue(t, w, protocol.WorkspaceListResponse{
			Items: []protocol.WorkspaceView{{
				WorkspaceId: "w1", Path: "/tmp/proj", Title: "proj",
				SessionIds: []string{"s1"},
			}},
		})
	})
	mux.HandleFunc("/api/session.list", func(w http.ResponseWriter, r *http.Request) {
		writeRPCValue(t, w, protocol.SessionListResponse{
			Items: []protocol.SessionSummary{
				{SessionId: "s1", Blank: true, Cwd: "/tmp/proj", UpdatedAt: 10},
			},
		})
	})
	mux.HandleFunc("/api/session.create", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var p map[string]any
		if createSeen != nil {
			env := map[string]any{}
			_ = json.Unmarshal(raw, &env)
			payload, _ := env["payload"].(map[string]any)
			*createSeen = payload
		}
		_ = p
		writeRPCValue(t, w, protocol.SessionCreateResponse{SessionId: "s2"})
	})
	return httptest.NewServer(mux)
}

func TestConnectWorkspaceReusesBlankSession(t *testing.T) {
	var createSeen map[string]any
	srv := workspaceHost(t, &createSeen)
	defer srv.Close()
	a := New(srv.URL)
	ctx := context.Background()
	if err := a.RefreshWorkspaces(ctx); err != nil {
		t.Fatal(err)
	}
	if err := a.RefreshRoster(ctx); err != nil {
		t.Fatal(err)
	}
	sid, err := a.ConnectWorkspace(ctx, "w1")
	if err != nil {
		t.Fatal(err)
	}
	if sid != "s1" {
		t.Fatalf("ConnectWorkspace = %q, want the existing blank s1", sid)
	}
	if createSeen != nil {
		t.Fatalf("blank session must be reused, not re-created: %+v", createSeen)
	}
}

func TestConnectWorkspaceCreatesOnWorkspace(t *testing.T) {
	var createSeen map[string]any
	srv := workspaceHost(t, &createSeen)
	defer srv.Close()
	a := New(srv.URL)
	ctx := context.Background()
	if err := a.RefreshWorkspaces(ctx); err != nil {
		t.Fatal(err)
	}
	if err := a.RefreshRoster(ctx); err != nil {
		t.Fatal(err)
	}
	// The only blank is no longer blank: it has run a turn.
	a.Store().HostFrame(&protocol.HostFrame{
		Type: protocol.FHostSessionStatus, SessionId: "s1", Running: true,
	})
	sid, err := a.ConnectWorkspace(ctx, "w1")
	if err != nil {
		t.Fatal(err)
	}
	if sid != "s2" {
		t.Fatalf("ConnectWorkspace = %q, want the freshly created s2", sid)
	}
	if createSeen["workspaceId"] != "w1" {
		t.Fatalf("create payload = %+v, want workspaceId w1", createSeen)
	}
	if createSeen["cwd"] != nil {
		t.Fatalf("create payload must not carry cwd: %+v", createSeen)
	}
}

func TestCreateSessionMapsRegisteredPathToWorkspace(t *testing.T) {
	var createSeen map[string]any
	srv := workspaceHost(t, &createSeen)
	defer srv.Close()
	a := New(srv.URL)
	ctx := context.Background()
	if err := a.RefreshWorkspaces(ctx); err != nil {
		t.Fatal(err)
	}
	id, err := a.CreateSession(ctx, protocol.SessionCreateRequest{Cwd: "/tmp/proj"})
	if err != nil {
		t.Fatal(err)
	}
	if id != "s2" {
		t.Fatalf("CreateSession = %q", id)
	}
	if createSeen["workspaceId"] != "w1" || createSeen["cwd"] != nil {
		t.Fatalf("registered path must travel as workspaceId: %+v", createSeen)
	}
}

// presetCreateHost serves a preset roster and the create endpoint, echoing
// the create payload for the pin assertions.
func presetCreateHost(t *testing.T, presets []protocol.AgentPresetEntry, createSeen *map[string]any) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/agentPreset.list", func(w http.ResponseWriter, r *http.Request) {
		writeRPCValue(t, w, protocol.AgentPresetListResponse{Presets: presets})
	})
	mux.HandleFunc("/api/session.create", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		env := map[string]any{}
		_ = json.Unmarshal(raw, &env)
		payload, _ := env["payload"].(map[string]any)
		if createSeen != nil {
			*createSeen = payload
		}
		writeRPCValue(t, w, protocol.SessionCreateResponse{SessionId: "s2"})
	})
	return httptest.NewServer(mux)
}

// TestCreateSessionPinsStaleDefault is the upgrade case: the host's
// configured default is a preset id the roster no longer supplies (no
// default-flagged row), so an unnamed create would be rejected — the client
// pins the deployment's base default instead.
func TestCreateSessionPinsStaleDefault(t *testing.T) {
	var createSeen map[string]any
	srv := presetCreateHost(t, []protocol.AgentPresetEntry{
		{Id: "standard", Trust: "system"},
		{Id: "ptc", Trust: "system"},
		{Id: "minimal", Trust: "system"},
		{Id: "cordis", Trust: "system"},
	}, &createSeen)
	defer srv.Close()
	a := New(srv.URL)
	if _, err := a.CreateSession(context.Background(), protocol.SessionCreateRequest{Cwd: "/tmp/proj"}); err != nil {
		t.Fatal(err)
	}
	if createSeen["agentPreset"] != "standard" {
		t.Fatalf("stale default must pin standard: %+v", createSeen)
	}
}

// TestCreateSessionPinsFirstIntactWhenStandardBroken: a stale default with
// standard itself broken falls to the first intact row (roster order).
func TestCreateSessionPinsFirstIntactWhenStandardBroken(t *testing.T) {
	var createSeen map[string]any
	srv := presetCreateHost(t, []protocol.AgentPresetEntry{
		{Id: "standard", Trust: "system", Broken: "unparsable yaml"},
		{Id: "ptc", Trust: "system"},
	}, &createSeen)
	defer srv.Close()
	a := New(srv.URL)
	if _, err := a.CreateSession(context.Background(), protocol.SessionCreateRequest{Cwd: "/tmp/proj"}); err != nil {
		t.Fatal(err)
	}
	if createSeen["agentPreset"] != "ptc" {
		t.Fatalf("broken standard must fall to the first intact row: %+v", createSeen)
	}
}

// TestCreateSessionKeepsIntactDefault: a roster that flags its default row
// resolves on the host side; the client pins nothing, and an explicit
// preset travels as given.
func TestCreateSessionKeepsIntactDefault(t *testing.T) {
	presets := []protocol.AgentPresetEntry{
		{Id: "standard", Trust: "system", IsDefault: true},
		{Id: "ptc", Trust: "system"},
	}
	var createSeen map[string]any
	srv := presetCreateHost(t, presets, &createSeen)
	defer srv.Close()
	a := New(srv.URL)
	if _, err := a.CreateSession(context.Background(), protocol.SessionCreateRequest{Cwd: "/tmp/proj"}); err != nil {
		t.Fatal(err)
	}
	if createSeen["agentPreset"] != nil {
		t.Fatalf("intact default must stay unnamed: %+v", createSeen)
	}
	if _, err := a.CreateSession(context.Background(), protocol.SessionCreateRequest{Cwd: "/tmp/proj", AgentPreset: "minimal"}); err != nil {
		t.Fatal(err)
	}
	if createSeen["agentPreset"] != "minimal" {
		t.Fatalf("explicit preset must travel unchanged: %+v", createSeen)
	}
}

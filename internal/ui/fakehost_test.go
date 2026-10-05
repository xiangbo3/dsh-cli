// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"dsh-cli/internal/protocol"

	"github.com/coder/websocket"
)

// fakeHost is a hermetic stand-in for the DSH web server: the boot
// baseline unary methods, the common session/workspace methods, and idle
// WS downlinks (auto-pong). Before it, most ui tests shared the live
// 127.0.0.1:3080 boot and raced its real session state; now each test
// boots against its own private host.
type fakeHost struct {
	*httptest.Server
	describe   protocol.HostDescription
	sessions   []protocol.SessionSummary
	workspaces []protocol.WorkspaceView
	histories  map[string][]protocol.HistoryEntry
	subagents  []protocol.SubagentListEntry
	models     protocol.SessionModels

	// settings is the in-memory settings tree (ns → value); mutate ops
	// apply to it, describe answers from it.
	settings   map[string]json.RawMessage
	discovered map[string][]protocol.DiscoveredModel
	lastMutate [][]byte

	mu      sync.Mutex
	calls   []string
	nextSeq int
}

// newFakeHost boots a private host. The roster stays empty unless the
// test fills fh.sessions before app.Start.
func newFakeHost(t *testing.T) *fakeHost {
	t.Helper()
	fh := &fakeHost{
		describe:  protocol.HostDescription{Version: "fake", Cwd: "/tmp/fakehost", Home: "/tmp/fakehost", CanOpenPath: true},
		histories: map[string][]protocol.HistoryEntry{},
		settings: map[string]json.RawMessage{
			"llm-pi-ai": []byte(`{"providers":{"custom":{"baseURL":"http://upstream/v1","api":"openai-completions","models":[{"id":"m1","name":"M1"}]}}}`),
		},
		discovered: map[string][]protocol.DiscoveredModel{
			"custom": {{Id: "m1", Name: "M1"}, {Id: "m2", Name: "M2", ContextWindow: 4096}},
		},
	}
	fh.Server = httptest.NewServer(http.HandlerFunc(fh.handle))
	t.Cleanup(fh.Server.Close)
	return fh
}

// called reports whether the unary method hit the host (tests pin
// "must not RPC" behavior against the private host).
func (fh *fakeHost) called(method string) bool {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	for _, m := range fh.calls {
		if m == method {
			return true
		}
	}
	return false
}

// fakeLatency paces unary answers the way a real network would: without
// it the boot baseline (describe → session.list → workspace.list) lands in
// microseconds and races the tests that seed store state right after
// app.Start. 25ms keeps the synchronous setup winning the race.
var fakeLatency = 25 * time.Millisecond

func (fh *fakeHost) handle(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/")
	if path == "events.mux" || path == "events.host" {
		// Idle downlink: consume frames (and auto-pong) until the client
		// leaves.
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		for {
			if _, _, err := c.Read(r.Context()); err != nil {
				return
			}
		}
	}
	var env protocol.Envelope
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		http.Error(w, "bad envelope", http.StatusBadRequest)
		return
	}
	fh.mu.Lock()
	fh.calls = append(fh.calls, path)
	fh.mu.Unlock()
	if path == "respond" {
		// The legacy answer channel acks with a bare receipt (accepted
		// + reason), not a server-response envelope: the client refuses
		// to settle a modal on an unrecognized ack.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accepted":true}`))
		return
	}
	time.Sleep(fakeLatency)
	value, _ := json.Marshal(fh.unary(path, env.Payload))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(protocol.Envelope{
		Type:   protocol.TypeServerResponse,
		RpcId:  env.RpcId,
		Result: &protocol.Result{Ok: true, Value: value},
	})
}

// unary answers one method; an unknown method gets an empty success so a
// wrapper's decode lands on a zero value instead of an error.
func (fh *fakeHost) unary(method string, payload json.RawMessage) any {
	switch method {
	case protocol.MHostDescribe:
		return fh.describe
	case protocol.MSessionList:
		return protocol.SessionListResponse{Items: fh.sessions}
	case protocol.MWorkspaceList:
		return protocol.WorkspaceListResponse{Items: fh.workspaces}
	case protocol.MAgentPresetList:
		return protocol.AgentPresetListResponse{
			Presets: []protocol.AgentPresetEntry{
				{Id: "standard", Trust: "system", IsDefault: true},
				{Id: "ptc", Trust: "system"},
				{Id: "minimal", Trust: "system"},
				{Id: "cordis", Trust: "system"},
			},
			Authorable: true,
			HasDoc:     true,
		}
	case protocol.MSessionCreate:
		var p struct {
			AgentPreset string `json:"agentPreset"`
		}
		_ = json.Unmarshal(payload, &p)
		fh.mu.Lock()
		fh.nextSeq++
		id := fmt.Sprintf("f%d", fh.nextSeq)
		fh.mu.Unlock()
		return protocol.SessionCreateResponse{SessionId: id, AgentPreset: p.AgentPreset}
	case protocol.MSessionFork:
		fh.mu.Lock()
		fh.nextSeq++
		id := fmt.Sprintf("f%d", fh.nextSeq)
		fh.mu.Unlock()
		return map[string]any{"sessionId": id}
	case protocol.MSessionHistory:
		var p struct {
			SessionId string `json:"sessionId"`
		}
		_ = json.Unmarshal(payload, &p)
		return protocol.HistoryResponse{Events: fh.histories[p.SessionId]}
	case protocol.MSubagentList:
		return protocol.SubagentCatalog{Entries: fh.subagents}
	case protocol.MSubagentHistory:
		var p struct {
			ChildSessionId string `json:"childSessionId"`
		}
		_ = json.Unmarshal(payload, &p)
		return protocol.HistoryResponse{Events: fh.histories[p.ChildSessionId]}
	case protocol.MSubagentInterrupt:
		return map[string]any{"ok": true}
	case protocol.MSessionPrompt:
		return protocol.PromptResponse{Accepted: true}
	case protocol.MSessionRename:
		var p struct {
			Title string `json:"title"`
		}
		_ = json.Unmarshal(payload, &p)
		return map[string]any{"title": p.Title}
	case protocol.MAgentPresetSelect:
		var p struct {
			AgentPreset string `json:"agentPreset"`
		}
		_ = json.Unmarshal(payload, &p)
		return map[string]any{"agentPreset": p.AgentPreset}
	case protocol.MSessionModels:
		return fh.models
	case protocol.MSessionSelectModel:
		var p struct {
			Provider        string `json:"provider"`
			Model           string `json:"model"`
			ReasoningEffort string `json:"reasoningEffort"`
		}
		_ = json.Unmarshal(payload, &p)
		return map[string]any{"selected": protocol.ModelSelection{
			Provider: p.Provider, Model: p.Model, ReasoningEffort: p.ReasoningEffort,
		}}
	case protocol.MLlmModels:
		var provider string
		_ = json.Unmarshal(payload, &provider)
		fh.mu.Lock()
		list := fh.discovered[provider]
		fh.mu.Unlock()
		return list
	case "settings.describe":
		fh.mu.Lock()
		var ns []protocol.SettingNamespaceView
		for name, raw := range fh.settings {
			ns = append(ns, protocol.SettingNamespaceView{Ns: name, Value: raw})
		}
		fh.mu.Unlock()
		return map[string]any{"writable": true, "namespaces": ns}
	case "settings.mutate":
		var p struct {
			Ns  string             `json:"ns"`
			Ops []protocol.SettingOp `json:"ops"`
		}
		_ = json.Unmarshal(payload, &p)
		fh.mu.Lock()
		fh.lastMutate = append(fh.lastMutate, payload)
		fh.mu.Unlock()
		value := fh.applySettings(p.Ns, p.Ops)
		return map[string]any{"ns": p.Ns, "value": value, "revision": 1}
	case protocol.MWorkspaceCreate:
		var p struct {
			Path string `json:"path"`
		}
		_ = json.Unmarshal(payload, &p)
		return protocol.WorkspaceCreateResponse{
			Workspace: &protocol.WorkspaceView{WorkspaceId: "w1", Path: p.Path},
			Created:   true,
		}
	}
	return map[string]any{}
}

// lastMutatePayload returns the recorded settings.mutate envelope ("" if
// none landed).
func (fh *fakeHost) lastMutatePayload() string {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	if len(fh.lastMutate) == 0 {
		return ""
	}
	return string(fh.lastMutate[len(fh.lastMutate)-1])
}

// applySettings applies path ops to one namespace value in memory.
func (fh *fakeHost) applySettings(ns string, ops []protocol.SettingOp) json.RawMessage {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	var doc map[string]any
	if raw, ok := fh.settings[ns]; ok {
		_ = json.Unmarshal(raw, &doc)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	for _, op := range ops {
		cur := doc
		for i, key := range op.Path {
			if i == len(op.Path)-1 {
				if op.Op == "unset" {
					delete(cur, key)
				} else {
					cur[key] = op.Value
				}
				break
			}
			next, ok := cur[key].(map[string]any)
			if !ok {
				next = map[string]any{}
				cur[key] = next
			}
			cur = next
		}
	}
	raw, _ := json.Marshal(doc)
	fh.settings[ns] = raw
	return raw
}

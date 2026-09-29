// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

// Package functional is the hermetic end-to-end suite: it drives the built
// dsh-cli binary against a private fake dsh web and pins every command
// surface — status, ls, new, history, models, workspaces, one-shot (arg,
// pipe, flags-after-prompt), and the error paths — so a feature upgrade
// that breaks a command is caught by make test / make check.
package functional

import (
	"context"
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

// fakeHost is a hermetic legacy-dialect dsh web: the unary /api methods the
// one-shot commands use, the dual events downlinks (mux carries the
// session/event stream, host idles), and a canned completed turn per prompt
// when armed. Before it, end-to-end tests raced the live 127.0.0.1:3080
// host and its real session state.
type fakeHost struct {
	*httptest.Server

	mu         sync.Mutex
	describe   protocol.HostDescription
	sessions   []protocol.SessionSummary
	workspaces []protocol.WorkspaceView
	archived   []string
	histories  map[string][]protocol.HistoryEntry
	models     protocol.SessionModels

	// armed turn: when on, each accepted prompt gets a completed turn
	// pushed on the mux downlink (user echo, assistant answer, turn/end).
	turnOnPrompt bool
	answer       string

	nextSeq  int64
	nextID   int
	creates  []protocol.SessionCreateRequest
	prompts  []promptCall
	muxConns map[*websocket.Conn]struct{}
}

// promptCall is one recorded session.prompt.
type promptCall struct {
	SessionId string
	Mode      string
	Text      string
}

// newFakeHost boots a private host. The roster and registry stay empty
// until the test seeds them; describe always answers (the readiness probe).
func newFakeHost(t *testing.T) *fakeHost {
	t.Helper()
	fh := &fakeHost{}
	// Seed under the lock: the handler goroutines (in this same test
	// process) read the state without any happens-before edge to the
	// seeding (the child's requests cross a socket, invisible to -race).
	fh.mu.Lock()
	fh.describe = protocol.HostDescription{
		Version: "fake-host-1", Cwd: "/tmp/fakehost", Home: "/tmp/fakehost",
		Provider: "deepseek", Model: "fake-model",
		AttachedSessions: 1, CanOpenPath: true,
	}
	fh.histories = map[string][]protocol.HistoryEntry{}
	fh.models = protocol.SessionModels{
		Current:  protocol.ModelSelection{Provider: "deepseek", Model: "fake-model"},
		Routable: true,
		Groups: []protocol.ModelProviderGroup{
			{Id: "deepseek", Name: "DeepSeek", Models: []protocol.ModelCatalogModel{
				{Id: "fake-model", Name: "Fake Model"},
				{Id: "fake-fast"},
			}},
			{Id: "openai", Name: "OpenAI", Models: []protocol.ModelCatalogModel{
				{Id: "gpt-5"},
			}},
		},
		Failures: []protocol.ModelCatalogFailure{
			{Id: "anthropic", Name: "Anthropic", Message: "lookup failed"},
		},
	}
	fh.muxConns = map[*websocket.Conn]struct{}{}
	fh.mu.Unlock()
	fh.Server = httptest.NewServer(http.HandlerFunc(fh.handle))
	t.Cleanup(fh.Close)
	return fh
}

// seedSession appends one roster row.
func (fh *fakeHost) seedSession(id string, running, blank bool, preset, cwd string, updatedAt int64) {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	fh.sessions = append(fh.sessions, protocol.SessionSummary{
		SessionId: id, Running: running, Blank: blank, AgentPreset: preset, Cwd: cwd, UpdatedAt: updatedAt,
	})
}

// seedWorkspace appends one registry row with an ordered session account.
func (fh *fakeHost) seedWorkspace(id, title, path string, sessions ...string) {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	fh.workspaces = append(fh.workspaces, protocol.WorkspaceView{
		WorkspaceId: id, Title: title, Path: path, SessionIds: sessions,
	})
}

// seedHistory installs the durable log of one session (the history pages).
func (fh *fakeHost) seedHistory(sid string, evs ...protocol.SessionEvent) {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	entries := make([]protocol.HistoryEntry, len(evs))
	for i, e := range evs {
		entries[i] = protocol.HistoryEntry{Event: e}
	}
	fh.histories[sid] = entries
}

// turnOn arms the canned completed turn: every accepted prompt then gets
// its user echo, the assistant answer, and the turn/end that ends the
// one-shot wait.
func (fh *fakeHost) turnOn(answer string) {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	fh.turnOnPrompt = true
	fh.answer = answer
}

// createCalls lists the recorded session.create payloads, in order.
func (fh *fakeHost) createCalls() []protocol.SessionCreateRequest {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	return append([]protocol.SessionCreateRequest(nil), fh.creates...)
}

// promptCalls lists the recorded session.prompt payloads, in order.
func (fh *fakeHost) promptCalls() []promptCall {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	return append([]promptCall(nil), fh.prompts...)
}

// handle routes one request: the index probe, the two events sockets, and
// the unary /api methods.
func (fh *fakeHost) handle(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/":
		// 200 index: the dialect probe classifies this as the legacy
		// build (a cookie-gated build answers 401 with the gate marker).
		fmt.Fprint(w, "<html><body>fake dsh web</body></html>")
		return
	case protocol.StreamMux, protocol.StreamHost:
		fh.downlink(w, r)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}
	method := strings.TrimPrefix(r.URL.Path, "/api/")
	var env protocol.Envelope
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		http.Error(w, "bad envelope", http.StatusBadRequest)
		return
	}
	value, rerr := fh.unary(method, env)
	res := &protocol.Result{Ok: true, Value: value}
	if rerr != nil {
		res = &protocol.Result{Ok: false, Error: rerr}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(protocol.Envelope{
		Type: protocol.TypeServerResponse, RpcId: env.RpcId, Result: res,
	})
}

// downlink accepts one events socket. The mux socket is the session/event
// channel (push delivers to it); the host socket idles. The coder library
// auto-pongs the client's pings, so the read loop only consumes frames.
func (fh *fakeHost) downlink(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	isMux := r.URL.Path == protocol.StreamMux
	if isMux {
		fh.mu.Lock()
		fh.muxConns[c] = struct{}{}
		fh.mu.Unlock()
	}
	defer func() {
		c.Close(websocket.StatusNormalClosure, "")
		if isMux {
			fh.mu.Lock()
			delete(fh.muxConns, c)
			fh.mu.Unlock()
		}
	}()
	for {
		if _, _, err := c.Read(r.Context()); err != nil {
			return
		}
	}
}

// unary answers one method; an unknown method gets an empty success so a
// caller's decode lands on a zero value instead of an error.
func (fh *fakeHost) unary(method string, env protocol.Envelope) (json.RawMessage, *protocol.RpcError) {
	// Snapshot the seed state under the lock (see newFakeHost), then
	// marshal the copies: no field of the host is read bare in the
	// handler goroutines.
	fh.mu.Lock()
	describe := fh.describe
	sessions := fh.sessions
	workspaces := fh.workspaces
	archived := fh.archived
	histories := fh.histories
	models := fh.models
	fh.mu.Unlock()
	switch method {
	case protocol.MHostDescribe:
		return mustJSON(describe), nil
	case protocol.MSessionList:
		return mustJSON(protocol.SessionListResponse{Items: sessions}), nil
	case protocol.MWorkspaceList:
		return mustJSON(protocol.WorkspaceListResponse{Items: workspaces, ArchivedSessionIds: archived}), nil
	case protocol.MAgentPresetList:
		return mustJSON(protocol.AgentPresetListResponse{
			Presets: []protocol.AgentPresetEntry{{Id: "standard", Trust: "system", IsDefault: true}},
		}), nil
	case protocol.MSessionCreate:
		var p protocol.SessionCreateRequest
		_ = json.Unmarshal(env.Payload, &p)
		fh.mu.Lock()
		fh.nextID++
		id := fmt.Sprintf("f%d", fh.nextID)
		fh.creates = append(fh.creates, p)
		fh.mu.Unlock()
		return mustJSON(protocol.SessionCreateResponse{SessionId: id, AgentPreset: p.AgentPreset}), nil
	case protocol.MSessionHistory:
		var p struct {
			SessionId string `json:"sessionId"`
		}
		_ = json.Unmarshal(env.Payload, &p)
		evs, ok := histories[p.SessionId]
		if !ok {
			return nil, &protocol.RpcError{Code: protocol.ErrSessionNotFound, Message: "session " + p.SessionId + " not found"}
		}
		return mustJSON(protocol.HistoryResponse{Events: evs}), nil
	case protocol.MSessionModels:
		return mustJSON(models), nil
	case protocol.MSessionPrompt:
		var p protocol.PromptRequest
		_ = json.Unmarshal(env.Payload, &p)
		var text strings.Builder
		for _, part := range p.Content {
			if part.Type == "text" {
				text.WriteString(part.Text)
			}
		}
		fh.mu.Lock()
		fh.prompts = append(fh.prompts, promptCall{SessionId: p.SessionId, Mode: p.Mode, Text: text.String()})
		armed, answer := fh.turnOnPrompt, fh.answer
		fh.mu.Unlock()
		if armed {
			fh.pushTurn(p.SessionId, text.String(), env.RpcId, answer)
		}
		return mustJSON(protocol.PromptResponse{Accepted: true}), nil
	case protocol.MSessionCancel:
		return mustJSON(map[string]any{}), nil
	}
	return mustJSON(map[string]any{}), nil
}

// pushTurn delivers one canned completed turn for sid: the user echo
// (stamped with the prompt's rpcId so the optimistic echo reconciles), the
// assistant answer, and the turn/end that ends the one-shot wait. It waits
// for the mux socket first: a frame pushed before the client's downlink
// lands is lost (the legacy wire has no replay).
func (fh *fakeHost) pushTurn(sid, prompt, rpcId, answer string) {
	fh.waitMux(10 * time.Second)
	fh.mu.Lock()
	now := time.Now().UnixMilli()
	fh.nextSeq += 3
	s1, s2, s3 := fh.nextSeq-2, fh.nextSeq-1, fh.nextSeq
	fh.mu.Unlock()
	evs := []protocol.SessionEvent{
		{Type: "user/message", Seq: s1, Time: now, Data: mustJSON(protocol.Message{
			Id: "u1", Role: "user",
			Content: []protocol.ContentBlock{{Type: "text", Text: prompt}},
			Source:  protocol.MessageSource{Kind: "user", RpcId: rpcId},
		})},
		{Type: "assistant/message", Seq: s2, Time: now + 10, Data: mustJSON(protocol.AssistantMessageEventData{
			Turn: 1, Step: 1,
			Message: protocol.Message{Id: "a1", Role: "assistant",
				Content: []protocol.ContentBlock{{Type: "text", Text: answer}}},
			Usage: &protocol.TokenUsage{InputTokens: 10, OutputTokens: 20},
		})},
		{Type: "turn/end", Seq: s3, Time: now + 20, Data: mustJSON(map[string]any{
			"turn":   1,
			"reason": map[string]any{"kind": "completed"},
		})},
	}
	for i, ev := range evs {
		if i > 0 {
			time.Sleep(30 * time.Millisecond) // pace: one frame per pump tick
		}
		fh.push(sid, ev)
	}
}

// push sends one session/event frame on the mux downlink.
func (fh *fakeHost) push(sid string, ev protocol.SessionEvent) {
	body, _ := json.Marshal(protocol.MuxSessionEvent{
		Type: protocol.FMuxEvent, SessionId: sid, Event: ev,
	})
	raw, _ := json.Marshal(protocol.Envelope{
		Type: protocol.TypeServerRequest, RpcId: protocol.NewRPCId(), Payload: body,
	})
	fh.mu.Lock()
	conns := make([]*websocket.Conn, 0, len(fh.muxConns))
	for c := range fh.muxConns {
		conns = append(conns, c)
	}
	fh.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, c := range conns {
		_ = c.Write(ctx, websocket.MessageText, raw)
	}
}

// waitMux blocks until at least one mux socket is connected (or the
// deadline: a client that never opens the downlink must not hang a turn).
func (fh *fakeHost) waitMux(timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for fh.muxCount() == 0 {
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (fh *fakeHost) muxCount() int {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	return len(fh.muxConns)
}

// mustJSON marshals a wire value (fake data, so a failure is a bug).
func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

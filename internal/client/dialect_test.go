// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"dsh-cli/internal/protocol"

	"github.com/coder/websocket"
)

// fakeNewHost is a hermetic stand-in for the cookie-gated (new) web build:
// the index 401s without a cookie, the launch-token exchange mints one, and
// every /api surface (HTTP + the remote.mux WS) gates on it.
type fakeNewHost struct {
	*httptest.Server
	token  string
	minted string

	mu      sync.Mutex
	argLog  []string // "<method> <args-json>" per unary hit
	pageSeq int64    // the session log tail seq the page endpoint sees
}

func newFakeNewHost(t *testing.T, token string) *fakeNewHost {
	t.Helper()
	f := &fakeNewHost{token: token}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.handle(w, r)
	}))
	t.Cleanup(f.Close)
	return f
}

// cookieValue returns the exchanged cookie ("name=value") or "" before
// exchange.
func (f *fakeNewHost) cookieValue() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.minted
}

func (f *fakeNewHost) handle(w http.ResponseWriter, r *http.Request) {
	// The index: the cookie gate's 401 and the launch-token mint.
	if r.URL.Path == "/" {
		if r.URL.Query().Has("token") {
			if r.URL.Query().Get("token") != f.token {
				w.WriteHeader(http.StatusUnauthorized)
				io.WriteString(w, "stale token")
				return
			}
			if strings.Contains(r.Header.Get("Cookie"), "dsh-auth-") {
				w.WriteHeader(http.StatusBadRequest) // a cookie on the mint is a client bug
				return
			}
			name := "dsh-auth-" + fakeAuthority(r)
			val := "v1.test.sig"
			f.mu.Lock()
			f.minted = name + "=" + val
			c := f.minted
			f.mu.Unlock()
			w.Header().Set("Set-Cookie", c+"; Max-Age=2592000; Path=/; HttpOnly; SameSite=Strict")
			w.Header().Set("Location", "/")
			w.WriteHeader(http.StatusSeeOther)
			return
		}
		if strings.Contains(r.Header.Get("Cookie"), "dsh-auth-") {
			io.WriteString(w, "<html/>") // a valid cookie answers the index
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, "dsh web authentication required")
		return
	}
	if r.URL.Path == "/api/remote.mux" {
		f.mux(w, r)
		return
	}
	// Unary: gate on the minted cookie, then answer by method.
	f.mu.Lock()
	want := f.minted
	f.mu.Unlock()
	if !hasCookie(r.Header.Get("Cookie"), want) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, "unauthorized")
		return
	}
	var env protocol.Envelope
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	var ap struct {
		Args json.RawMessage `json:"args"`
	}
	_ = json.Unmarshal(env.Payload, &ap)
	f.mu.Lock()
	f.argLog = append(f.argLog, env.Method+" "+string(ap.Args))
	f.mu.Unlock()
	f.unary(w, env, r)
}

// mux serves the remote.mux downlink: $events ready + a session/follow
// snapshot (its cursor feeds the page endpoint's throughSeq).
func (f *fakeNewHost) mux(w http.ResponseWriter, r *http.Request) {
	if !hasCookie(r.Header.Get("Cookie"), f.cookieValue()) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.Close(websocket.StatusNormalClosure, "")
	send := func(v any) {
		b, _ := json.Marshal(v)
		_ = c.Write(r.Context(), websocket.MessageText, b)
	}
	for {
		_, data, err := c.Read(r.Context())
		if err != nil {
			return
		}
		var msg struct {
			Type     string `json:"type"`
			StreamId string `json:"streamId"`
			Endpoint string `json:"endpoint"`
		}
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		if msg.Type != "open" {
			continue
		}
		switch msg.Endpoint {
		case "$events":
			send(map[string]any{"type": "item", "streamId": msg.StreamId, "value": map[string]any{
				"type": "ready", "clientId": "cli-1", "host": map[string]any{"home": "/tmp/fakehome"},
			}})
		case "session/follow":
			f.mu.Lock()
			seq := f.pageSeq
			f.mu.Unlock()
			send(map[string]any{"type": "item", "streamId": msg.StreamId, "value": map[string]any{
				"type":   "snapshot",
				"header": map[string]any{"id": "s1", "createdAt": 1, "cwd": "/tmp"},
				"cursor": seq,
				"records": []any{
					map[string]any{"type": "event", "event": map[string]any{"type": "session/title", "seq": seq, "time": 1, "data": map[string]any{"title": "t"}}},
				},
			}})
		}
	}
}

// unary answers the new-build method names (the dot→slash rename).
func (f *fakeNewHost) unary(w http.ResponseWriter, env protocol.Envelope, r *http.Request) {
	var ap struct {
		Args json.RawMessage `json:"args"`
	}
	_ = json.Unmarshal(env.Payload, &ap)
	var value any
	switch env.Method {
	case "session/modelCatalog":
		value = map[string]any{"default": map[string]any{"provider": "deepseek", "model": "chat"}}
	case "session/canOpenWorkspacePath":
		value = true
	case "session/list":
		value = protocol.SessionListResponse{Items: []protocol.SessionSummary{{SessionId: "s1", UpdatedAt: 5, Running: false}}}
	case "session/page":
		var req struct {
			Args struct {
				Request struct {
					ThroughSeq int64 `json:"throughSeq"`
					Address    struct {
						SessionId string `json:"sessionId"`
					} `json:"address"`
				} `json:"request"`
			} `json:"args"`
		}
		_ = json.Unmarshal(ap.Args, &req)
		value = map[string]any{
			"records": []any{
				map[string]any{"type": "event", "event": map[string]any{"type": "session/title", "seq": req.Args.Request.ThroughSeq, "time": 1, "data": map[string]any{"title": "t"}}},
			},
			"hasMore": false,
		}
	case "$events/result":
		value = map[string]any{"ok": true}
	case "agentPresets/select":
		value = "minimal"
	default:
		value = map[string]any{}
	}
	b, _ := json.Marshal(value)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(protocol.Envelope{
		Type:   protocol.TypeServerResponse,
		RpcId:  env.RpcId,
		Result: &protocol.Result{Ok: true, Value: b},
	})
}

func fakeAuthority(r *http.Request) string {
	return r.Host
}

func hasCookie(header, want string) bool {
	if want == "" {
		return false
	}
	for _, kv := range strings.Split(header, ";") {
		kv = strings.TrimSpace(kv)
		if kv == want {
			return true
		}
	}
	return false
}

// TestDialectDetect pins the probe: a 401 index is the new (cookie-gated)
// build, a 200 index is the legacy build.
func TestDialectDetect(t *testing.T) {
	f := newFakeNewHost(t, "tok")
	conn := ConnFor(f.URL)
	d, err := conn.Detect(context.Background(), f.Client())
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if d != DialectNew {
		t.Fatalf("dialect = %v, want DialectNew", d)
	}

	legacy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "<html/>")
	}))
	defer legacy.Close()
	ld, _ := ConnFor(legacy.URL).Detect(context.Background(), legacy.Client())
	if ld != DialectOld {
		t.Fatalf("legacy dialect = %v, want DialectOld", ld)
	}
}

// TestCookieExchange pins the mint: the token GET returns the 303 and the
// dsh-auth-* cookie, which the conn then holds.
func TestCookieExchange(t *testing.T) {
	f := newFakeNewHost(t, "tok")
	conn := ConnFor(f.URL)
	conn.SetToken("tok")
	if err := conn.Exchange(context.Background(), f.Client()); err != nil {
		t.Fatalf("exchange: %v", err)
	}
	ck := conn.Cookie()
	if !strings.HasPrefix(ck, "dsh-auth-") || !strings.Contains(ck, "=") {
		t.Fatalf("cookie = %q, want a dsh-auth-* name=value", ck)
	}
}

// TestCallNewDialect pins the unary translation end to end: the host sees
// the slash method + the minted cookie, and the facade decodes the value.
func TestCallNewDialect(t *testing.T) {
	f := newFakeNewHost(t, "tok")
	c := New(f.URL)
	c.conn.SetToken("tok")
	if err := c.conn.Exchange(context.Background(), c.http); err != nil {
		t.Fatalf("exchange: %v", err)
	}
	got, err := c.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].SessionId != "s1" {
		t.Fatalf("items = %+v, want the one s1 row", got.Items)
	}
	f.mu.Lock()
	joined := strings.Join(f.argLog, " | ")
	f.mu.Unlock()
	if !strings.Contains(joined, "session/list") {
		t.Fatalf("host saw %q, want the session/list endpoint", joined)
	}
	// The reserved empty-list slot must ride the args wrapper.
	if !strings.Contains(joined, `session/list {"_request":{}}`) {
		t.Fatalf("session/list args = %q, want the _request slot", joined)
	}
}

// TestCall401Retry pins the re-exchange path: a request that 401s (a stale
// or absent cookie) clears with one exchange + retry.
func TestCall401Retry(t *testing.T) {
	f := newFakeNewHost(t, "tok")
	// Start with no cookie held: the first list 401s, the retry (after
	// exchange) lands.
	c := New(f.URL)
	c.conn.SetToken("tok")
	got, err := c.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions (should 401 then re-exchange + retry): %v", err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("items = %+v, want the s1 row", got.Items)
	}
}

// TestHistoryThroughSeq pins the page cursor: the tail seq the page request
// carries comes from a follow snapshot (via the throwaway peek when no live
// follow has landed).
func TestHistoryThroughSeq(t *testing.T) {
	f := newFakeNewHost(t, "tok")
	f.mu.Lock()
	f.pageSeq = 42
	f.mu.Unlock()
	c := New(f.URL)
	c.conn.SetToken("tok")
	if err := c.conn.Exchange(context.Background(), c.http); err != nil {
		t.Fatalf("exchange: %v", err)
	}
	resp, err := c.History(context.Background(), "s1", 0, 120)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(resp.Events) != 1 {
		t.Fatalf("events = %d, want 1", len(resp.Events))
	}
	f.mu.Lock()
	log := strings.Join(f.argLog, " | ")
	f.mu.Unlock()
	if !strings.Contains(log, `"throughSeq":42`) {
		t.Fatalf("page request = %q, want throughSeq 42 from the follow snapshot", log)
	}
}

// TestRespondNewDialect pins the answer channel: an approval answer posts
// to $events/result with the clientId + eventId and the outcome string.
func TestRespondNewDialect(t *testing.T) {
	f := newFakeNewHost(t, "tok")
	c := New(f.URL)
	c.conn.SetToken("tok")
	if err := c.conn.Exchange(context.Background(), c.http); err != nil {
		t.Fatalf("exchange: %v", err)
	}
	c.conn.SetHostFacts("cli-1", "/tmp")
	err := c.Respond(context.Background(), "evt-9", protocol.ApprovalResponse{
		SessionId: "s1", ApprovalId: "a1", Outcome: "allowed-once",
	})
	if err != nil {
		t.Fatalf("Respond: %v", err)
	}
	f.mu.Lock()
	log := strings.Join(f.argLog, " | ")
	f.mu.Unlock()
	if !strings.Contains(log, "$events/result") ||
		!strings.Contains(log, `"clientId":"cli-1"`) ||
		!strings.Contains(log, `"eventId":"evt-9"`) ||
		!strings.Contains(log, "allowed-once") {
		t.Fatalf("$events/result = %q, want clientId+eventId+outcome", log)
	}
}

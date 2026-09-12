// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

// Package client is the DSH transport layer: unary HTTP RPC, the answer
// channel, and the downlink — speaking both the pre-token build (dual
// events streams, /api/respond) and the cookie-gated build (one
// multiplexed remote.mux, the $events/result channel).
package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"dsh-cli/internal/protocol"
	"dsh-cli/internal/textutil"

	"github.com/coder/websocket"
)

// maxBody bounds any unary response: the host answers with JSON of at most
// a few MB (the largest legitimate payload is a 200-event history page),
// so a runaway body is a faulty host, not data.
const maxBody = 16 << 20

// Client is a unary RPC caller for one DSH web server.
type Client struct {
	base string
	conn *Conn
	http *http.Client
	// token is the DSH_TOKEN bearer credential ("" = none; the host
	// decides whether a connection without one is acceptable).
	token string
	// insecure marks the DSH_INSECURE boot: the transport skips the
	// server certificate check (DSH_CA extra-trust does not count).
	insecure bool
}

// New builds a client for base (e.g. "http://127.0.0.1:3080").
//
// Transport environment:
//
//	DSH_TOKEN     send Authorization: Bearer <token> on every request
//	DSH_CA        extra CA bundle (PEM) for https verification (self-signed
//	              bridge certificates)
//	DSH_INSECURE  =1/=true: https without server certificate verification
func New(base string) *Client {
	var transport http.RoundTripper = http.DefaultTransport
	insecure := false
	if ca := os.Getenv("DSH_CA"); ca != "" {
		if pem, err := os.ReadFile(ca); err == nil {
			pool := x509.NewCertPool()
			pool.AppendCertsFromPEM(pem)
			if dt, ok := http.DefaultTransport.(*http.Transport); ok {
				t := dt.Clone()
				t.TLSClientConfig = &tls.Config{RootCAs: pool}
				transport = t
			}
		}
	} else if envBool("DSH_INSECURE") {
		if dt, ok := http.DefaultTransport.(*http.Transport); ok {
			t := dt.Clone()
			t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // #nosec G402 — explicit env opt-in
			transport = t
			insecure = true
		}
	}
	b := trimBase(base)
	return &Client{
		base:     b,
		conn:     ConnFor(b),
		token:    os.Getenv("DSH_TOKEN"),
		insecure: insecure,
		http: &http.Client{
			Timeout:   60 * time.Second,
			Transport: transport,
			// A redirected RPC POST replays its JSON body (the prompt)
			// to the redirect target: treat any redirect as the final
			// answer instead of following it.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 1 {
					return http.ErrUseLastResponse
				}
				return nil
			},
		},
	}
}

// Insecure reports whether DSH_INSECURE is active for this client (the
// transport skips the server certificate check).
func (c *Client) Insecure() bool { return c.insecure }

// IsDown reports whether err is a transport-level HTTP failure —
// connection refused, DNS miss, dead route: the kind of error that means
// no DSH server is listening at the configured address. A timeout is NOT
// down (the server exists but is slow), and an answered request (any HTTP
// status) is not down either: call() reports those without a *url.Error.
func IsDown(err error) bool {
	var ue *url.Error
	return errors.As(err, &ue) && !ue.Timeout()
}

// BaseURL returns the configured server base.
func (c *Client) BaseURL() string { return c.base }

// Conn exposes the client's shared per-base state (dialect, cookie, host
// facts) — the app and the stream wire their boot through it.
func (c *Client) Conn() *Conn { return c.conn }

// ensureDialect resolves (and caches) the protocol generation: the first
// unary call and the stream dial both settle it before speaking.
func (c *Client) ensureDialect(ctx context.Context) (Dialect, error) {
	if d := c.conn.Dialect(); d != 0 {
		return d, nil
	}
	return c.conn.Detect(ctx, c.http)
}

// call performs one unary RPC and decodes the result value into target.
// The dialect is resolved on first use: old hosts keep the legacy wire,
// new (cookie-gated) hosts go through the translation.
func (c *Client) call(ctx context.Context, method string, payload any, target any) error {
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode %s: %w", method, err)
	}
	d, err := c.ensureDialect(ctx)
	if err != nil {
		return err
	}
	if d != DialectNew {
		_, err := c.post(ctx, method, method, rawPayload, false, "", target)
		if isNotFound(err) {
			// The endpoint 404s on the legacy wire: the probe misread
			// the generation (a body-stripping proxy hides the gate
			// marker) — speak the new wire once.
			return c.callNew(ctx, method, rawPayload, target)
		}
		return err
	}
	err = c.newWireCall(ctx, method, rawPayload, target)
	if isNotFound(err) {
		// The mirror case: a new-wire 404 means the host is the
		// legacy build after all — speak the legacy wire once.
		_, err = c.post(ctx, method, method, rawPayload, false, "", target)
	}
	return err
}

// newWireCall runs one unary on the cookie-gated wire (the composed
// describe and workspace answers included).
func (c *Client) newWireCall(ctx context.Context, method string, rawPayload []byte, target any) error {
	switch method {
	case protocol.MHostDescribe:
		h, err := c.describeNew(ctx)
		if err != nil {
			return err
		}
		return json.Unmarshal(mustJSON(h), target)
	case protocol.MWorkspaceList:
		v, err := c.workspacesNew(ctx)
		if err != nil {
			return err
		}
		return json.Unmarshal(mustJSON(v), target)
	}
	return c.callNew(ctx, method, rawPayload, target)
}

// isNotFound reports a 404 (the post errors wrap ErrNotFound).
func isNotFound(err error) bool { return errors.Is(err, ErrNotFound) }

// callNew speaks the cookie-gated wire: the translated endpoint and
// args-wrapped payload, one 401 → re-exchange → retry, and the value
// remapped back to the old facade shape.
func (c *Client) callNew(ctx context.Context, method string, rawPayload []byte, target any) error {
	endpoint := newEndpoint(method)
	newPayload, err := newArgs(method, rawPayload, protocol.NewRPCId())
	if err != nil {
		return err
	}
	// The page endpoint verifies throughSeq against the log tail, and
	// the tail seq rides no unary surface: a known follow cursor pages
	// it, and without one a throwaway follow snapshot supplies it.
	if method == protocol.MSessionHistory || method == protocol.MSubagentHistory {
		sid, err := historySessionId(method, rawPayload)
		if err != nil {
			return err
		}
		cur, err := c.historyCursor(ctx, sid)
		if err != nil {
			return err
		}
		if cur >= 0 {
			newPayload, err = setThroughSeq(newPayload, cur)
			if err != nil {
				return err
			}
		}
	}
	_, err = c.post(ctx, endpoint, method, newPayload, true, method, target)
	return err
}

// historySessionId extracts the paged log's session id (subagent pages
// ride the child's log).
func historySessionId(method string, rawPayload []byte) (string, error) {
	if method == protocol.MSubagentHistory {
		var p protocol.SubagentHistoryRequest
		if err := json.Unmarshal(rawPayload, &p); err != nil {
			return "", err
		}
		return p.ChildSessionId, nil
	}
	var p struct {
		SessionId string `json:"sessionId"`
	}
	if err := json.Unmarshal(rawPayload, &p); err != nil {
		return "", err
	}
	return p.SessionId, nil
}

// historyCursor resolves the log's tail seq: a live follow's snapshot
// cursor (waited on briefly — the UI opens the follow alongside the
// page request), else a throwaway follow snapshot (the new build's only
// tail-seq surface; no dial means the page cannot be requested at all,
// so the error propagates).
func (c *Client) historyCursor(ctx context.Context, sid string) (int64, error) {
	deadline := time.Now().Add(2 * time.Second)
	for {
		if cur := c.conn.Cursor(sid); cur >= 0 {
			return cur, nil
		}
		if time.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return -1, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	return c.peekFollow(ctx, sid)
}

// peekFollow takes one session/follow snapshot over a throwaway
// remote.mux dial and returns its cursor.
func (c *Client) peekFollow(ctx context.Context, sid string) (int64, error) {
	dctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	wsURL := strings.TrimRight(c.base, "/")
	if strings.HasPrefix(wsURL, "http://") {
		wsURL = "ws://" + strings.TrimPrefix(wsURL, "http://") + protocol.StreamRemoteMux
	} else {
		wsURL = "wss://" + strings.TrimPrefix(wsURL, "https://") + protocol.StreamRemoteMux
	}
	hdr := http.Header{}
	if ck := c.conn.Cookie(); ck != "" {
		hdr.Set("Cookie", ck)
	}
	conn, _, err := websocket.Dial(dctx, wsURL, &websocket.DialOptions{HTTPHeader: hdr})
	if err != nil {
		return -1, fmt.Errorf("follow snapshot: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	// A snapshot replays the whole log: long transcripts exceed the 32KiB
	// default frame limit (the persistent stream raises it the same way).
	conn.SetReadLimit(4 << 20)
	body, _ := json.Marshal(map[string]any{
		"type":     "open",
		"streamId": "peek",
		"endpoint": "session/follow",
		"payload":  map[string]any{"args": map[string]any{"request": map[string]any{"address": map[string]any{"kind": "session", "sessionId": sid}}}},
	})
	wctx, wcancel := context.WithTimeout(ctx, 3*time.Second)
	err = conn.Write(wctx, websocket.MessageText, body)
	wcancel()
	if err != nil {
		return -1, fmt.Errorf("follow snapshot: %w", err)
	}
	for {
		rctx, rcancel := context.WithTimeout(ctx, 5*time.Second)
		_, data, err := conn.Read(rctx)
		rcancel()
		if err != nil {
			return -1, fmt.Errorf("follow snapshot: %w", err)
		}
		var msg struct {
			StreamId string          `json:"streamId"`
			Value    json.RawMessage `json:"value"`
		}
		if json.Unmarshal(data, &msg) != nil || msg.StreamId != "peek" {
			continue
		}
		var snap struct {
			Type   string `json:"type"`
			Cursor int64  `json:"cursor"`
		}
		if json.Unmarshal(msg.Value, &snap) == nil && snap.Type == "snapshot" {
			c.conn.SetCursor(sid, snap.Cursor)
			return snap.Cursor, nil
		}
	}
}

// setThroughSeq stamps the page request's throughSeq onto a translated
// payload (the placeholder throughSeq 0 stands until the cursor lands).
// setThroughSeq patches the page request's throughSeq (the log tail the
// host verifies against), keeping the rest of the request intact.
func setThroughSeq(payload []byte, seq int64) ([]byte, error) {
	var p struct {
		Args struct {
			Request map[string]any `json:"request"`
		} `json:"args"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, err
	}
	if p.Args.Request == nil {
		return nil, fmt.Errorf("page payload lacks the request slot")
	}
	p.Args.Request["throughSeq"] = seq
	return json.Marshal(p)
}

// post POSTs one client-request envelope to endpoint and decodes the
// result value (through the remap when remapMethod is set) into target.
// A 401 on a cookie-gated host clears with one re-exchange and retry;
// on the legacy build 401 is a plain error (its gate is the bearer).
func (c *Client) post(ctx context.Context, endpoint, label string, payload []byte, gated bool, remapMethod string, target any) (*protocol.Envelope, error) {
	body, err := json.Marshal(protocol.Envelope{
		Type:    protocol.TypeClientRequest,
		RpcId:   protocol.NewRPCId(),
		Method:  endpoint,
		Payload: payload,
	})
	if err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/"+endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		c.stamp(req)
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 && gated {
			// The cookie gate rejected us: re-exchange (a stale cookie
			// after a host secret reset, or a never-held one) and retry
			// once. A missing token cannot clear it.
			if _, err := c.conn.RefreshCookie(ctx, c.http); err != nil {
				// Carry the exchange cause (it wraps ErrAuthRequired): a
				// missing or rejected token cannot clear a 401, and the
				// cause names the fix.
				return nil, fmt.Errorf("%s: HTTP 401: %s: %w", label, snippet(string(raw), 120), err)
			}
			continue
		}
		if resp.StatusCode != http.StatusOK {
			if resp.StatusCode == http.StatusUnsupportedMediaType {
				return nil, fmt.Errorf("%s: server refused content type (415)", label)
			}
			if resp.StatusCode == http.StatusNotFound {
				return nil, fmt.Errorf("%s: HTTP 404: %s: %w", label, snippet(string(raw), 200), ErrNotFound)
			}
			if resp.StatusCode == http.StatusUnauthorized && gated {
				return nil, fmt.Errorf("%s: HTTP 401: %s: %w", label, snippet(string(raw), 120), ErrAuthRequired)
			}
			return nil, fmt.Errorf("%s: HTTP %d: %s", label, resp.StatusCode, snippet(string(raw), 200))
		}
		var env protocol.Envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return nil, fmt.Errorf("%s: bad envelope: %v", label, err)
		}
		if env.Result == nil {
			return nil, fmt.Errorf("%s: missing result", label)
		}
		if !env.Result.Ok {
			if env.Result.Error != nil {
				return nil, env.Result.Error
			}
			return nil, fmt.Errorf("%s: failed without error details", label)
		}
		if target != nil && len(env.Result.Value) > 0 {
			value := env.Result.Value
			if remapMethod != "" {
				value, err = newValue(remapMethod, value)
				if err != nil {
					return nil, err
				}
			}
			if err := json.Unmarshal(value, target); err != nil {
				return nil, fmt.Errorf("%s: decode value: %w", label, err)
			}
		}
		return &env, nil
	}
	return nil, fmt.Errorf("%s: HTTP 401: still unauthorized after re-exchange: %w", label, ErrAuthRequired)
}

// Call is the untyped escape hatch for methods without a facade.
func (c *Client) Call(ctx context.Context, method string, payload any, target any) error {
	return c.call(ctx, method, payload, target)
}

// newEndpoint maps an old method name to its new wire path.
func newEndpoint(method string) string {
	if r, ok := newRoutes[method]; ok {
		return r.path
	}
	return method
}

// stamp adds the bearer credential (legacy bridge) and the browser
// session cookie (the new host's gate). A nil conn (a bare test client)
// simply carries no cookie.
func (c *Client) stamp(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.conn != nil {
		if ck := c.conn.Cookie(); ck != "" {
			req.Header.Set("Cookie", ck)
		}
	}
}

// Respond answers one answerable server-request frame, echoing its rpcId.
// The old build posts the client-response to /api/respond; the new build
// answers over the $events result channel, keyed by the $events
// generation id (clientId) and the frame's eventId (the old rpcId slot).
func (c *Client) Respond(ctx context.Context, rpcId string, value any) error {
	d := DialectOld
	if c.conn != nil {
		// The first answer on a fresh boot may precede any unary call:
		// settle the dialect (a failed probe keeps the legacy channel).
		if dd, err := c.ensureDialect(ctx); err == nil {
			d = dd
		}
	}
	if d == DialectNew {
		return c.respondNew(ctx, rpcId, value)
	}
	return c.respondOld(ctx, rpcId, value)
}

// respondOld is the legacy /api/respond channel.
func (c *Client) respondOld(ctx context.Context, rpcId string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	body, err := json.Marshal(protocol.Envelope{
		Type:   protocol.TypeClientResponse,
		RpcId:  rpcId,
		Result: &protocol.Result{Ok: true, Value: raw},
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+protocol.PathRespond, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.stamp(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	rawBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("respond: HTTP %d: %s", resp.StatusCode, snippet(string(rawBody), 200))
	}
	// The carrier acks a rejected payload with HTTP 200 +
	// {accepted:false, reason}: without this check a bad answer closes
	// the modal and vanishes (the pending question never settles on the
	// host, and the user sees no toast).
	var receipt struct {
		Accepted bool   `json:"accepted"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal(rawBody, &receipt); err == nil && !receipt.Accepted {
		if receipt.Reason != "" {
			return fmt.Errorf("respond: %s", receipt.Reason)
		}
		return fmt.Errorf("respond: not accepted")
	}
	return nil
}

// respondNew maps the old answer payloads onto the $events result
// channel: an approval outcome string or the question answer list, under
// outcome.kind "result".
func (c *Client) respondNew(ctx context.Context, rpcId string, value any) error {
	clientID := c.conn.ClientID()
	if clientID == "" {
		return fmt.Errorf("respond: events stream not connected (clientId unknown)")
	}
	var outcome any
	switch v := value.(type) {
	case protocol.ApprovalResponse:
		outcome = v.Outcome // allowed-once | rejected
	case protocol.QuestionAnswer:
		outcome = map[string]any{"answers": v.Answer.Answers}
	default:
		outcome = value
	}
	payload, err := json.Marshal(map[string]any{"args": map[string]any{
		"clientId": clientID,
		"eventId":  rpcId,
		"outcome":  map[string]any{"kind": "result", "value": outcome},
	}})
	if err != nil {
		return err
	}
	if _, err := c.post(ctx, "$events/result", "respond", payload, true, "", nil); err != nil {
		return fmt.Errorf("respond: %w", err)
	}
	return nil
}

// envBool reports an env var set to 1/true/yes (case-insensitive).
func envBool(k string) bool {
	switch strings.ToLower(os.Getenv(k)) {
	case "1", "true", "yes":
		return true
	}
	return false
}

func snippet(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	return textutil.Truncate(s, n, "…")
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

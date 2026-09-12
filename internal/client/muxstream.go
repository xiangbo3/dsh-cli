// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

// The cookie-gated build's downlink: one /api/remote.mux WebSocket
// carrying every logical stream ($events, session/follow per open
// transcript, the global session/control, workspace/follow). The
// re-emit keeps the legacy DownlinkFrame vocabulary, so the app and the
// store do not see a second protocol.
package client

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"dsh-cli/internal/protocol"

	"github.com/coder/websocket"
)

// FollowAddr names one session/follow target: a session or one of its
// subagent transcripts.
type FollowAddr struct {
	Kind            string // "session" | "subagent"
	SessionId       string
	ParentSessionId string
	ChildSessionId  string
	Mode            string // subagent: one-shot | continuable
}

// FollowAddrKey is the dedupe key of a follow target.
func (a FollowAddr) Key() string {
	if a.Kind == "subagent" {
		return "subagent:" + a.ChildSessionId
	}
	return "session:" + a.SessionId
}

// Follow opens (or keeps) the follow stream of one transcript. Idempotent
// per address; it opens immediately when the mux epoch is live, and at
// epoch open otherwise. No-op on the legacy build (its mux follows every
// session by subscription).
func (s *Stream) Follow(a FollowAddr) {
	s.followMu.Lock()
	s.follows[a.Key()] = a
	live := s.mux
	s.followMu.Unlock()
	if live != nil {
		live.open(a)
	}
}

// Unfollow cancels one transcript's follow stream (and forgets it for
// later epochs).
func (s *Stream) Unfollow(a FollowAddr) {
	s.followMu.Lock()
	delete(s.follows, a.Key())
	live := s.mux
	s.followMu.Unlock()
	if live != nil {
		live.cancelAddr(a)
	}
}

// muxStream is one live /api/remote.mux epoch: the socket, the open
// streams, and the re-emit of their frames.
type muxStream struct {
	s    *Stream
	conn *websocket.Conn

	writeMu sync.Mutex
	idGen   uint32

	mu      sync.Mutex
	byID    map[string]*muxSub // streamId → subscription
	byAddr  map[string]string  // follow key → streamId (follows only)
	follows []FollowAddr
}

type muxSub struct {
	endpoint string // $events | session/follow | session/control | workspace/follow
	addr     FollowAddr
}

// muxLoop drives the reconnect ladder of the new dialect: dial with the
// session cookie (exchanging first when a token is held and the cookie
// is missing or was just rejected by the gate), open the fixed streams
// plus every active follow, and hold until death.
// On death the whole epoch is torn down and re-dialed: the streams
// re-baseline (snapshots, control and workspace baselines), which the
// status pulse tells the app to re-baseline against.
func (s *Stream) muxLoop(ctx context.Context) {
	// Drop approval sides whose other half died with a previous epoch:
	// a side waiting on a delivery the stream lost is dead weight (the
	// host re-asks on the next turn).
	s.conn.StaleApprovals(time.Now().Add(-2 * time.Minute))
	var backoff time.Duration
	lastDialFailed := false
	for {
		if s.conn.Token() != "" && (s.conn.Cookie() == "" || lastDialFailed) {
			// The WS gate rejects a missing or untrusted cookie before
			// any HTTP retry path could re-exchange it: exchange up
			// front when a token is held and no cookie is, or after a
			// failed dial (a held cookie the host no longer accepts -
			// expired, or the host secret rotated - needs a fresh
			// mint). The exchange is self-throttled, so the retry
			// ladder costs at most one mint per window; a failed one
			// (host down) just costs the dial the same backoff.
			dctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			_ = s.conn.Exchange(dctx, s.http)
			cancel()
		}
		dctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		conn, _, err := websocket.Dial(dctx, s.muxURL(), s.wsOpts())
		cancel()
		if err != nil {
			lastDialFailed = true
			select {
			case <-ctx.Done():
				return
			default:
			}
			backoff = nextBackoff(backoff)
			s.emitStatus(false)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			continue
		}
		lastDialFailed = false
		backoff = 0
		conn.SetReadLimit(4 << 20)
		m := &muxStream{
			s:      s,
			conn:   conn,
			byID:   map[string]*muxSub{},
			byAddr: map[string]string{},
		}
		s.followMu.Lock()
		s.mux = m
		s.followMu.Unlock()
		defer func() {
			s.followMu.Lock()
			if s.mux == m {
				s.mux = nil
			}
			s.followMu.Unlock()
		}()

		// Fixed stream set: the event bus, the global control surface,
		// and the workspace registry. Follows ride per transcript.
		m.openFixed()
		s.followMu.Lock()
		m.follows = make([]FollowAddr, 0, len(s.follows))
		for _, a := range s.follows {
			m.follows = append(m.follows, a)
		}
		s.followMu.Unlock()
		for _, a := range m.follows {
			m.open(a)
		}

		dead := make(chan struct{})
		var once sync.Once
		die := func() { once.Do(func() { close(dead) }) }
		go m.reader(ctx, die)
		pingStop := make(chan struct{})
		var pingWG sync.WaitGroup
		pingWG.Add(1)
		go func() {
			defer pingWG.Done()
			t := time.NewTicker(30 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-pingStop:
					return
				case <-t.C:
					if err := conn.Ping(ctx); err != nil {
						die()
						return
					}
				}
			}
		}()
		s.emitStatus(true)

		select {
		case <-ctx.Done():
		case <-dead:
			s.emitStatus(false)
		}
		close(pingStop)
		pingWG.Wait()
		conn.Close(websocket.StatusNormalClosure, "")
	}
}

// muxURL is the new build's single downlink address.
func (s *Stream) muxURL() string {
	return strings.TrimRight(s.wsBase, "/") + protocol.StreamRemoteMux
}

func (s *Stream) wsOpts() *websocket.DialOptions {
	// The gate wants the session cookie; the Origin fence wants a
	// truthful origin (the scheme the socket rides on).
	hdr := http.Header{"Origin": []string{s.origin}}
	if ck := s.conn.Cookie(); ck != "" {
		hdr.Set("Cookie", ck)
	}
	return &websocket.DialOptions{HTTPHeader: hdr}
}

// reader pumps the socket until it dies, routing every frame to its
// stream's re-emit.
func (m *muxStream) reader(ctx context.Context, die func()) {
	defer die()
	for {
		readCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		_, data, err := m.conn.Read(readCtx)
		cancel()
		if err != nil {
			return
		}
		m.route(data)
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

// openFixed opens the always-on streams of an epoch.
func (m *muxStream) openFixed() {
	m.openStream("$events", FollowAddr{})
	m.openStream("session/control", FollowAddr{})
	m.openStream("workspace/follow", FollowAddr{})
}

// open opens (or re-opens) one transcript follow.
func (m *muxStream) open(a FollowAddr) {
	m.mu.Lock()
	if _, dup := m.byAddr[a.Key()]; dup {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()
	m.openStream("session/follow", a)
}

// cancelAddr cancels one transcript follow.
func (m *muxStream) cancelAddr(a FollowAddr) {
	m.mu.Lock()
	id, ok := m.byAddr[a.Key()]
	if ok {
		delete(m.byAddr, a.Key())
		if sub, ok2 := m.byID[id]; ok2 {
			delete(m.byID, id)
			sub.endpoint = ""
		}
	}
	m.mu.Unlock()
	if ok {
		m.send(map[string]any{"type": "cancel", "streamId": id})
	}
}

// openStream allocates a stream id, registers the subscription, and sends
// the open message.
func (m *muxStream) openStream(endpoint string, a FollowAddr) {
	id := "c" + strconv.FormatUint(uint64(m.nextID()), 10)
	m.mu.Lock()
	m.byID[id] = &muxSub{endpoint: endpoint, addr: a}
	if endpoint == "session/follow" {
		m.byAddr[a.Key()] = id
	}
	m.mu.Unlock()

	payload := map[string]any{"args": map[string]any{}}
	if endpoint == "session/follow" {
		var address map[string]any
		if a.Kind == "subagent" {
			address = map[string]any{
				"kind":            "subagent",
				"parentSessionId": a.ParentSessionId,
				"childSessionId":  a.ChildSessionId,
				"mode":            a.Mode,
			}
		} else {
			address = map[string]any{"kind": "session", "sessionId": a.SessionId}
		}
		payload = map[string]any{"args": map[string]any{
			"request": map[string]any{"address": address},
		}}
	}
	m.send(map[string]any{
		"type":     "open",
		"streamId": id,
		"endpoint": endpoint,
		"payload":  payload,
	})
}

func (m *muxStream) nextID() uint32 {
	m.idGen++
	return m.idGen
}

// send writes one multiplex message.
func (m *muxStream) send(v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		return
	}
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	wctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = m.conn.Write(wctx, websocket.MessageText, raw)
}

// route dispatches one socket frame to its stream's re-emit.
func (m *muxStream) route(data []byte) {
	var msg struct {
		Type     string          `json:"type"`
		StreamId string          `json:"streamId"`
		Value    json.RawMessage `json:"value"`
		Error    json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}
	sub := m.subFor(msg.StreamId)
	switch msg.Type {
	case "item":
		if sub == nil || len(msg.Value) == 0 {
			return
		}
		m.emit(sub, msg.Value)
	case "end":
		// A stream ended (the host closed it): drop the registration.
		// The epoch's teardown + re-open re-baselines on reconnect.
		if sub == nil {
			return
		}
		m.mu.Lock()
		if sub.addr.Key() != "" && sub.endpoint == "session/follow" {
			delete(m.byAddr, sub.addr.Key())
		}
		delete(m.byID, msg.StreamId)
		sub.endpoint = ""
		m.mu.Unlock()
	case "error":
		var e struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		code, message := "", ""
		if json.Unmarshal(msg.Error, &e) == nil {
			code, message = e.Error.Code, e.Error.Message
		}
		m.emitFrame(DownlinkFrame{
			Kind:    protocol.FStreamError,
			Payload: mustJSON(protocol.StreamError{Type: protocol.FStreamError, Error: protocol.RpcError{Code: code, Message: message}}),
		})
	}
}

func (m *muxStream) subFor(id string) *muxSub {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.byID[id]
}

// emit re-emits one stream item as legacy downlink frames.
func (m *muxStream) emit(sub *muxSub, value json.RawMessage) {
	switch sub.endpoint {
	case "$events":
		m.emitEvents(value)
	case "session/follow":
		m.emitFollow(sub.addr, value)
	case "session/control":
		m.emitControl(value)
	case "workspace/follow":
		m.emitWorkspaces(value)
	}
}

// emitEvents re-emits the $events stream (ready, emits, waterfalls).
func (m *muxStream) emitEvents(value json.RawMessage) {
	var v struct {
		Type     string `json:"type"`
		ClientId string `json:"clientId"`
		Host     *struct {
			Home string `json:"home"`
		} `json:"host"`
		Event   string            `json:"event"`
		Args    []json.RawMessage `json:"args"`
		EventId string            `json:"eventId"`
		AgentId string            `json:"agentId"`
		Request json.RawMessage   `json:"request"`
	}
	if err := json.Unmarshal(value, &v); err != nil {
		return
	}
	switch v.Type {
	case "ready":
		home := ""
		if v.Host != nil {
			home = v.Host.Home
		}
		m.s.conn.SetHostFacts(v.ClientId, home)
	case "emit":
		m.reEmit(v.Event, v.Args)
	case "waterfall":
		m.reWaterfall(v.Event, v.EventId, v.AgentId, v.Request)
	}
}

// reEmit maps a forwarded bus emit onto the legacy host-frame vocabulary
// (the new host pushes the session registry and preset changes on the
// bus instead of the old host stream).
func (m *muxStream) reEmit(event string, args []json.RawMessage) {
	one := func(i int) (string, bool) {
		if i >= len(args) {
			return "", false
		}
		var s string
		if json.Unmarshal(args[i], &s) != nil {
			return "", false
		}
		return s, true
	}
	host := func(f protocol.HostFrame) {
		m.emitFrame(DownlinkFrame{Kind: f.Type, Payload: mustJSON(f)})
	}
	switch event {
	case "api-session/added":
		if len(args) < 1 {
			return
		}
		var sum protocol.SessionSummary
		if json.Unmarshal(args[0], &sum) != nil {
			return
		}
		host(protocol.HostFrame{
			Type:            protocol.FHostSessionAdded,
			SessionId:       sum.SessionId,
			Blank:           sum.Blank,
			ParentSessionId: sum.ParentSessionId,
			Origin:          sum.Origin,
			Cwd:             sum.Cwd,
			AgentPreset:     sum.AgentPreset,
		})
	case "api-session/removed":
		if sid, ok := one(0); ok {
			host(protocol.HostFrame{Type: protocol.FHostSessionRemoved, SessionId: sid})
		}
	case "api-session/status":
		if sid, ok := one(0); ok {
			var running bool
			if len(args) > 1 {
				_ = json.Unmarshal(args[1], &running)
			}
			host(protocol.HostFrame{Type: protocol.FHostSessionStatus, SessionId: sid, Running: running})
		}
	case "api-session/error":
		if sid, ok := one(0); ok {
			msg, _ := one(1)
			host(protocol.HostFrame{Type: protocol.FHostAgentError, SessionId: sid, Message: msg})
		}
	case "api-session/activity":
		if sid, ok := one(0); ok {
			var ts int64
			if len(args) > 1 {
				_ = json.Unmarshal(args[1], &ts)
			}
			m.emitFrame(DownlinkFrame{
				Kind: protocol.FMuxActivity,
				Payload: mustJSON(struct {
					Type      string `json:"type"`
					SessionId string `json:"sessionId"`
					UpdatedAt int64  `json:"updatedAt"`
				}{protocol.FMuxActivity, sid, ts}),
			})
		}
	case "agent-preset/selected":
		if sid, ok := one(0); ok {
			if p, ok2 := one(1); ok2 {
				m.emitFrame(DownlinkFrame{
					Kind: protocol.FMuxPresetSelected,
					Payload: mustJSON(struct {
						Type        string `json:"type"`
						SessionId   string `json:"sessionId"`
						AgentPreset string `json:"agentPreset"`
					}{protocol.FMuxPresetSelected, sid, p}),
				})
			}
		}
	}
	// The rest (commands/change, settings/document-updated, the
	// credentials and llm notices, cordis/*) need no store reaction:
	// the legacy build did not push them either.
}

// reWaterfall maps a forwarded waterfall event onto the legacy
// answerable-frame vocabulary. The approval id join lives on the conn:
// the session log side (approval/asked) and this side (the answerable
// eventId) arrive independently.
func (m *muxStream) reWaterfall(event, eventId, agentId string, request json.RawMessage) {
	switch event {
	case "approval/request":
		var req struct {
			ToolName string `json:"toolName"`
			CallId   string `json:"callId"`
			Reason   string `json:"reason"`
		}
		if json.Unmarshal(request, &req) != nil {
			return
		}
		approvalId, complete := m.s.conn.ApprovalWaterfall(agentId, eventId, req.ToolName, req.CallId, req.Reason)
		if !complete {
			return // the asked side has not landed yet (or will not)
		}
		m.emitApproval(agentId, eventId, approvalId, req.ToolName, req.CallId, req.Reason)
	case "user-questions/request":
		var req struct {
			Questions []protocol.QuestionItem `json:"questions"`
		}
		if json.Unmarshal(request, &req) != nil {
			return
		}
		m.emitFrame(DownlinkFrame{
			RpcId: eventId,
			Kind:  protocol.FMuxQuestionReq,
			Payload: mustJSON(protocol.MuxQuestionRequested{
				Type:      protocol.FMuxQuestionReq,
				SessionId: agentId,
				Questions: req.Questions,
			}),
		})
	}
}

// emitApproval emits the answerable approval frame once both id sides
// are joined.
func (m *muxStream) emitApproval(sessionId, requestId, approvalId, toolName, callId, reason string) {
	m.emitFrame(DownlinkFrame{
		RpcId: requestId,
		Kind:  protocol.FMuxApprovalReq,
		Payload: mustJSON(protocol.MuxApprovalRequested{
			Type:       protocol.FMuxApprovalReq,
			SessionId:  sessionId,
			ApprovalId: approvalId,
			ToolName:   toolName,
			CallId:     callId,
			Reason:     reason,
		}),
	})
}

// emitFollow re-emits one transcript follow stream: the snapshot
// (watermark + projection baseline) and the live events, plus the
// approval join from the session-log side.
func (m *muxStream) emitFollow(addr FollowAddr, value json.RawMessage) {
	var v struct {
		Type   string `json:"type"`
		Header *struct {
			Id string `json:"id"`
		} `json:"header"`
		Cursor  int64 `json:"cursor"`
		Records []struct {
			Type  string                `json:"type"`
			Event protocol.SessionEvent `json:"event"`
		} `json:"records"`
		Projections *struct {
			AsOfSeq int64                      `json:"asOfSeq"`
			Values  map[string]json.RawMessage `json:"values"`
		} `json:"projections"`
		Event protocol.SessionEvent `json:"event"`
	}
	if err := json.Unmarshal(value, &v); err != nil {
		return
	}
	sid := addr.SessionId
	if v.Header != nil && v.Header.Id != "" {
		sid = v.Header.Id
	}
	switch v.Type {
	case "snapshot":
		// The page endpoint verifies its throughSeq against the log
		// tail: record the cursor so the unary side can page it.
		m.s.conn.SetCursor(sid, v.Cursor)
		m.emitFrame(DownlinkFrame{
			Kind: protocol.FMuxSubscribed,
			Payload: mustJSON(struct {
				Type      string `json:"type"`
				SessionId string `json:"sessionId"`
				LastSeq   int64  `json:"lastSeq"`
			}{protocol.FMuxSubscribed, sid, v.Cursor}),
		})
		if v.Projections != nil {
			// The tail page of the new build carries no projection
			// baseline: this is the only one the store gets, so it
			// lands here as legacy projection frames (seq = the
			// baseline watermark, which the per-key guard accepts only
			// when no newer live value is held).
			for key, val := range v.Projections.Values {
				m.emitProjection(sid, key, v.Projections.AsOfSeq, val)
			}
		}
	case "event":
		m.handleFollowEvent(sid, v.Event)
	}
}

// handleFollowEvent re-emits one session-log event and runs the
// approval join (the asked/decided data carries the approval id; the
// answerable eventId rides the $events waterfall side).
func (m *muxStream) handleFollowEvent(sid string, ev protocol.SessionEvent) {
	switch ev.Type {
	case "approval/asked":
		var d struct {
			Id       string `json:"id"`
			ToolName string `json:"toolName"`
			CallId   string `json:"callId"`
			Reason   string `json:"reason"`
		}
		if json.Unmarshal(ev.Data, &d) == nil && d.Id != "" {
			requestId, complete := m.s.conn.ApprovalAsked(sid, d.Id, d.ToolName, d.CallId, d.Reason)
			if complete {
				m.emitApproval(sid, requestId, d.Id, d.ToolName, d.CallId, d.Reason)
			}
		}
	case "approval/decided":
		var d struct {
			Id      string `json:"id"`
			Outcome string `json:"outcome"`
		}
		if json.Unmarshal(ev.Data, &d) == nil && d.Id != "" {
			m.emitFrame(DownlinkFrame{
				Kind: protocol.FMuxApprovalRes,
				Payload: mustJSON(protocol.MuxApprovalResolved{
					Type:       protocol.FMuxApprovalRes,
					SessionId:  sid,
					ApprovalId: d.Id,
					Outcome:    d.Outcome,
				}),
			})
		}
	}
	// Every log event (including the approval ones: the transcript
	// renders them) rides the legacy session/event frame.
	m.emitFrame(DownlinkFrame{
		Kind:    protocol.FMuxEvent,
		Payload: mustJSON(protocol.MuxSessionEvent{Type: protocol.FMuxEvent, SessionId: sid, Event: ev}),
	})
}

// emitProjection emits one legacy projection frame.
func (m *muxStream) emitProjection(sessionId, key string, seq int64, value json.RawMessage) {
	m.emitFrame(DownlinkFrame{
		Kind: protocol.FMuxProjection,
		Payload: mustJSON(protocol.MuxProjection{
			Type:      protocol.FMuxProjection,
			SessionId: sessionId,
			Key:       key,
			Value:     value,
			Seq:       seq,
		}),
	})
}

// emitControl re-emits the global control stream (the pending-inbox,
// job, and projection surfaces of every session).
func (m *muxStream) emitControl(value json.RawMessage) {
	var v struct {
		Type      string          `json:"type"`
		Value     json.RawMessage `json:"value"` // baseline block or one projection value
		SessionId string          `json:"sessionId"`
		Items     json.RawMessage `json:"items"`
		Jobs      json.RawMessage `json:"jobs"`
		Key       string          `json:"key"`
		Seq       int64           `json:"seq"`
	}
	if err := json.Unmarshal(value, &v); err != nil {
		return
	}
	switch v.Type {
	case "baseline":
		var bl struct {
			Queues      map[string]json.RawMessage `json:"queues"`
			Jobs        map[string]json.RawMessage `json:"jobs"`
			Projections map[string]json.RawMessage `json:"projections"`
		}
		if json.Unmarshal(v.Value, &bl) != nil {
			return
		}
		for sid, raw := range bl.Queues {
			m.emitQueued(sid, raw)
		}
		for sid, raw := range bl.Jobs {
			m.emitJobs(sid, raw)
		}
		for sid, raw := range bl.Projections {
			var pb struct {
				AsOfSeq int64                      `json:"asOfSeq"`
				Values  map[string]json.RawMessage `json:"values"`
			}
			if json.Unmarshal(raw, &pb) != nil {
				continue
			}
			for key, val := range pb.Values {
				m.emitProjection(sid, key, pb.AsOfSeq, val)
			}
		}
	case "queue":
		m.emitQueued(v.SessionId, v.Items)
	case "jobs":
		m.emitJobs(v.SessionId, v.Jobs)
	case "projection":
		m.emitProjection(v.SessionId, v.Key, v.Seq, v.Value)
	}
}

// emitQueued maps a new queue snapshot onto the legacy inbox shape.
func (m *muxStream) emitQueued(sessionId string, raw json.RawMessage) {
	var items []struct {
		Id        string `json:"id"`
		Placement string `json:"placement"`
		RpcId     string `json:"rpcId"`
		Message   *struct {
			Id      string          `json:"id"`
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(raw, &items) != nil {
		return
	}
	out := make([]protocol.QueuedInboxItem, 0, len(items))
	for _, it := range items {
		q := protocol.QueuedInboxItem{Id: it.Id, Placement: it.Placement}
		if it.Message != nil {
			q.Message = protocol.Message{Id: it.Message.Id, Role: "user"}
			if len(it.Message.Content) > 0 {
				_ = json.Unmarshal(it.Message.Content, &q.Message.Content)
			}
		}
		out = append(out, q)
	}
	m.emitFrame(DownlinkFrame{
		Kind:    protocol.FMuxQueue,
		Payload: mustJSON(protocol.MuxQueue{Type: protocol.FMuxQueue, SessionId: sessionId, Items: out}),
	})
}

// emitJobs re-emits one session's job snapshot (same shape across
// builds).
func (m *muxStream) emitJobs(sessionId string, raw json.RawMessage) {
	var jobs []protocol.JobView
	if json.Unmarshal(raw, &jobs) != nil {
		return
	}
	m.emitFrame(DownlinkFrame{
		Kind:    protocol.FMuxJobs,
		Payload: mustJSON(protocol.MuxJobs{Type: protocol.FMuxJobs, SessionId: sessionId, Jobs: jobs}),
	})
}

// emitWorkspaces folds the workspace/follow stream into the registry
// mirror and re-emits the legacy host frames.
func (m *muxStream) emitWorkspaces(value json.RawMessage) {
	var v struct {
		Type  string `json:"type"`
		Value *struct {
			Items              []protocol.WorkspaceView `json:"items"`
			ArchivedSessionIds []string                 `json:"archivedSessionIds"`
		} `json:"value"`
		Workspace    *protocol.WorkspaceView `json:"workspace"`
		WorkspaceId  string                  `json:"workspaceId"`
		WorkspaceIds []string                `json:"workspaceIds"`
		Archived     []string                `json:"archivedSessionIds"`
	}
	if err := json.Unmarshal(value, &v); err != nil {
		return
	}
	host := func(f protocol.HostFrame) {
		m.emitFrame(DownlinkFrame{Kind: f.Type, Payload: mustJSON(f)})
	}
	switch v.Type {
	case "baseline":
		if v.Value == nil {
			return
		}
		m.s.conn.SetWorkspaceBaseline(v.Value.Items, v.Value.ArchivedSessionIds)
		for i := range v.Value.Items {
			w := v.Value.Items[i]
			raw := mustJSON(w)
			host(protocol.HostFrame{Type: protocol.FHostWorkspaceChanged, Workspace: raw})
		}
		if v.Value.ArchivedSessionIds != nil {
			host(protocol.HostFrame{Type: protocol.FHostArchived, ArchivedSessionIds: v.Value.ArchivedSessionIds})
		}
	case "upsert":
		if v.Workspace == nil {
			return
		}
		raw := mustJSON(*v.Workspace)
		m.s.conn.WorkspaceApply("upsert", v.Workspace, "", nil, nil)
		host(protocol.HostFrame{Type: protocol.FHostWorkspaceChanged, Workspace: raw})
	case "remove":
		m.s.conn.WorkspaceApply("remove", nil, v.WorkspaceId, nil, nil)
		host(protocol.HostFrame{Type: protocol.FHostWorkspaceRemoved, WorkspaceId: v.WorkspaceId})
	case "order":
		m.s.conn.WorkspaceApply("order", nil, "", v.WorkspaceIds, nil)
		host(protocol.HostFrame{Type: protocol.FHostOrderChanged, WorkspaceIds: v.WorkspaceIds})
	case "archived":
		m.s.conn.WorkspaceApply("archived", nil, "", nil, v.Archived)
		host(protocol.HostFrame{Type: protocol.FHostArchived, ArchivedSessionIds: v.Archived})
	}
}

// emitFrame is the shared frame queue with the slow-consumer drop watch.
func (m *muxStream) emitFrame(f DownlinkFrame) {
	if f.Raw == nil {
		f.Raw = f.Payload
	}
	select {
	case m.s.frames <- f:
		m.s.consec.Store(0)
	case <-time.After(2 * time.Second):
		m.s.dropped.Add(1)
		if m.s.consec.Add(1) >= dropWatch {
			m.s.consec.Store(0)
			select {
			case m.s.drops <- struct{}{}:
			default:
			}
		}
	}
}

// dialect resolves the build generation for the stream: a probe before
// the first dial (the transport errors keep the backoff ladder going).
func (s *Stream) dialect(ctx context.Context) (Dialect, error) {
	if d := s.conn.Dialect(); d != 0 {
		return d, nil
	}
	return s.conn.Detect(ctx, s.http)
}

// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package client

import (
	"testing"
	"time"
)

// newMuxTest builds a Stream + muxStream pair that bypasses the socket so
// the re-emit (new wire → legacy DownlinkFrame) can be driven directly.
func newMuxTest(t *testing.T) (*Stream, *muxStream) {
	t.Helper()
	s := NewStream("http://127.0.0.1:0")
	m := &muxStream{s: s, byID: map[string]*muxSub{}, byAddr: map[string]string{}}
	return s, m
}

// drainFrames returns the frames emitted so far (bounded by a short wait).
func drainFrames(t *testing.T, s *Stream) []DownlinkFrame {
	t.Helper()
	var out []DownlinkFrame
	for {
		select {
		case f := <-s.frames:
			out = append(out, f)
		case <-time.After(20 * time.Millisecond):
			return out
		}
	}
}

func findFrame(frames []DownlinkFrame, kind string) *DownlinkFrame {
	for i := range frames {
		if frames[i].Kind == kind {
			return &frames[i]
		}
	}
	return nil
}

// TestMuxReadyRecordsHostFacts pins the $events ready frame: the clientId
// and host home land on the shared conn (the result channel + describe use
// them).
func TestMuxReadyRecordsHostFacts(t *testing.T) {
	s, m := newMuxTest(t)
	m.emit(&muxSub{endpoint: "$events"}, []byte(`{"type":"ready","clientId":"cli-7","host":{"home":"/srv"}}`))
	if s.conn.ClientID() != "cli-7" {
		t.Fatalf("clientId = %q, want cli-7", s.conn.ClientID())
	}
	if s.conn.Home() != "/srv" {
		t.Fatalf("home = %q, want /srv", s.conn.Home())
	}
}

// TestMuxFollowEventReemits pins the transcript re-emit: a session/follow
// event frame becomes a legacy session/event DownlinkFrame for that
// session.
func TestMuxFollowEventReemits(t *testing.T) {
	s, m := newMuxTest(t)
	sub := &muxSub{endpoint: "session/follow", addr: FollowAddr{Kind: "session", SessionId: "s1"}}
	m.emit(sub, []byte(`{"type":"event","event":{"type":"session/title","seq":3,"time":1,"data":{"title":"t"}}}`))
	frames := drainFrames(t, s)
	f := findFrame(frames, "session/event")
	if f == nil {
		t.Fatalf("frames = %+v, want a session/event frame", frames)
	}
}

// TestMuxSnapshotSetsCursorAndSubscribed pins the follow snapshot: it
// records the tail cursor on the conn and emits the legacy subscribed
// watermark.
func TestMuxSnapshotSetsCursorAndSubscribed(t *testing.T) {
	s, m := newMuxTest(t)
	sub := &muxSub{endpoint: "session/follow", addr: FollowAddr{Kind: "session", SessionId: "s1"}}
	m.emit(sub, []byte(`{"type":"snapshot","header":{"id":"s1"},"cursor":42,"records":[]}`))
	if got := s.conn.Cursor("s1"); got != 42 {
		t.Fatalf("cursor = %d, want 42", got)
	}
	frames := drainFrames(t, s)
	if findFrame(frames, "session/subscribed") == nil {
		t.Fatalf("frames = %+v, want a session/subscribed frame", frames)
	}
}

// TestMuxApprovalJoinAcrossStreams pins the two-sided approval join: the
// session-log approval/asked (follow stream) and the answerable
// approval/request (the $events waterfall) arrive independently and only
// when both sides are in does the legacy approval/requested frame fire,
// carrying the waterfall's eventId as its rpcId and the asked's id as the
// approval id.
func TestMuxApprovalJoinAcrossStreams(t *testing.T) {
	s, m := newMuxTest(t)
	follow := &muxSub{endpoint: "session/follow", addr: FollowAddr{Kind: "session", SessionId: "s1"}}
	events := &muxSub{endpoint: "$events"}

	// Asked side first: parks, no answerable frame yet.
	m.emit(follow, []byte(`{"type":"event","event":{"type":"approval/asked","seq":5,"time":1,"data":{"id":"a1","toolName":"bash","callId":"c1","reason":"needs write"}}}`))
	drainFrames(t, s) // (drops the transcript session/event frame)

	// Waterfall side: completes the join → the answerable frame.
	m.emit(events, []byte(`{"type":"waterfall","event":"approval/request","eventId":"evt-9","agentId":"s1","request":{"toolName":"bash","callId":"c1","reason":"needs write"}}`))
	frames := drainFrames(t, s)
	f := findFrame(frames, "approval/requested")
	if f == nil {
		t.Fatalf("frames = %+v, want an approval/requested frame", frames)
	}
	if f.RpcId != "evt-9" {
		t.Fatalf("rpcId = %q, want the waterfall eventId evt-9", f.RpcId)
	}
	if f.Payload == nil || !contains(f.Payload, `"approvalId":"a1"`) {
		t.Fatalf("payload = %s, want approvalId a1", f.Payload)
	}
}

// TestMuxControlBaselineReemits pins the global control baseline: queues,
// jobs, and per-session projection values each become their legacy frames.
func TestMuxControlBaselineReemits(t *testing.T) {
	s, m := newMuxTest(t)
	sub := &muxSub{endpoint: "session/control"}
	m.emit(sub, []byte(`{"type":"baseline","value":{"queues":{"s1":[{"id":"q1","placement":"queued","message":{"id":"q1","content":[{"type":"text","text":"hi"}]}}]},"jobs":{"s1":[]},"projections":{"s1":{"asOfSeq":9,"values":{"title":"t"}}}}}`))
	frames := drainFrames(t, s)
	if findFrame(frames, "session/queue") == nil {
		t.Fatalf("frames = %+v, want a session/queue frame", frames)
	}
	if findFrame(frames, "session/projection") == nil {
		t.Fatalf("frames = %+v, want a session/projection frame", frames)
	}
}

// TestMuxWorkspaceBaselineFeedsMirror pins the workspace/follow baseline:
// it lands on the shared conn's registry mirror (workspace.list reads it).
func TestMuxWorkspaceBaselineFeedsMirror(t *testing.T) {
	s, m := newMuxTest(t)
	sub := &muxSub{endpoint: "workspace/follow"}
	m.emit(sub, []byte(`{"type":"baseline","value":{"items":[{"workspaceId":"w1","path":"/a","title":"a","sessionIds":[]}],"archivedSessionIds":["s9"]}}`))
	items, archived, ok := s.conn.WorkspaceBaseline(t.Context())
	if !ok {
		t.Fatalf("workspace baseline not installed")
	}
	if len(items) != 1 || items[0].WorkspaceId != "w1" {
		t.Fatalf("items = %+v, want the w1 row", items)
	}
	if len(archived) != 1 || archived[0] != "s9" {
		t.Fatalf("archived = %+v, want s9", archived)
	}
}

func contains(b []byte, sub string) bool {
	for i := 0; i+len(sub) <= len(b); i++ {
		if string(b[i:i+len(sub)]) == sub {
			return true
		}
	}
	return false
}

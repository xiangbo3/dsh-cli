package core

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"dsh-cli/internal/protocol"
)

func TestStoreProjectionHigherSeqWins(t *testing.T) {
	s := NewStore("http://x")
	s.MuxProjection("s1", "title", 10, mustJSON(`"one"`))
	s.MuxProjection("s1", "title", 5, mustJSON(`"old"`)) // lower seq: ignored
	s.MuxProjection("s1", "title", 20, mustJSON(`"two"`))
	if got := s.TitleFor("s1"); got != "two" {
		t.Fatalf("title = %q", got)
	}
}

// TestCachedTitleYieldsToLive pins the boot-cache title's precedence: the
// cache installs names at event level (CacheRoster), so a live title event
// and a re-baselined title projection — both newer than the previous boot
// by definition — must win over it.
func TestCachedTitleYieldsToLive(t *testing.T) {
	s := NewStore("http://x")
	if !s.CacheRoster([]CacheRow{{Id: "s1", Title: "cached name"}}) {
		t.Fatal("cache did not install")
	}
	if got := s.TitleFor("s1"); got != "cached name" {
		t.Fatalf("cached title = %q", got)
	}
	// A live title event is newer than the previous boot.
	data, _ := json.Marshal(protocol.TitleEventData{Title: "renamed"})
	s.Event("s1", &protocol.SessionEvent{Type: "session/title", Seq: 1, Time: 1, Data: data})
	if got := s.TitleFor("s1"); got != "renamed" {
		t.Fatalf("after title event = %q", got)
	}
	// A re-baselined title projection outranks both.
	s.MuxProjection("s1", "title", 2, mustJSON(`"projected"`))
	if got := s.TitleFor("s1"); got != "projected" {
		t.Fatalf("after projection = %q", got)
	}
}

func TestStoreHostFrameStatusAndBlank(t *testing.T) {
	s := NewStore("http://x")
	s.HostFrame(&protocol.HostFrame{
		Type: protocol.FHostSessionAdded, SessionId: "s1", Blank: true, Cwd: "/tmp/proj",
	})
	if sum := s.SummaryFor("s1"); sum == nil || !sum.Blank || sum.Cwd != "/tmp/proj" {
		t.Fatalf("after added: %+v", sum)
	}
	s.HostFrame(&protocol.HostFrame{Type: protocol.FHostSessionStatus, SessionId: "s1", Running: true})
	if sum := s.SummaryFor("s1"); !sum.Running || sum.Blank {
		t.Fatalf("after running: %+v (blank must clear on first run)", sum)
	}
	s.HostFrame(&protocol.HostFrame{Type: protocol.FHostSessionStatus, SessionId: "s1", Running: false})
	if sum := s.SummaryFor("s1"); sum.Running || sum.Blank {
		t.Fatalf("after idle: %+v (blank stays cleared)", sum)
	}
}

func TestStoreTurnEndChannel(t *testing.T) {
	s := NewStore("http://x")
	s.Sess("s1")
	select {
	case info := <-s.EndOfTurn():
		t.Fatalf("premature turn end: %+v", info)
	default:
	}
	// turn/start then turn/end (events only drive the fold).
	s.Event("s1", ev(t, "turn/start", 1, map[string]any{"turn": 1}))
	s.Event("s1", ev(t, "turn/end", 2, map[string]any{
		"turn": 1, "reason": map[string]any{"kind": "completed"},
	}))
	select {
	case info := <-s.EndOfTurn():
		if info.Kind != "completed" || info.Sess != "s1" {
			t.Fatalf("info = %+v", info)
		}
	case <-time.After(time.Second):
		t.Fatal("no turn-end event delivered")
	}
}

func TestStoreQueueAndJobs(t *testing.T) {
	s := NewStore("http://x")
	s.MuxQueue("s1", []protocol.QueuedInboxItem{{Id: "a", Placement: "queued",
		Message: protocol.Message{Id: "m1", Role: "user",
			Content: []protocol.ContentBlock{{Type: "text", Text: "later"}}}},
	})
	items := s.QueueFor("s1")
	if len(items) != 1 || items[0].Id != "a" {
		t.Fatalf("queue = %+v", items)
	}
	s.MuxJobs("s1", []protocol.JobView{{Id: "bash-1", Kind: "bash", Label: "make", Status: "running",
		StartedAt: time.Now().UnixMilli()}})
	jobs := s.JobsFor("s1")
	if len(jobs) != 1 || jobs[0].Id != "bash-1" {
		t.Fatalf("jobs = %+v", jobs)
	}
}

func TestStoreApprovalLifecycle(t *testing.T) {
	s := NewStore("http://x")
	s.ApprovalRequested("s1", "rpc-1", "appr-1", "bash", "call-1", "needs approval", `{"cmd":"rm -rf /"}`)
	pends := s.ApprovalsFor("s1")
	if len(pends) != 1 {
		t.Fatalf("pends = %+v", pends)
	}
	s.ApprovalResolved("appr-1", "allowed-once")
	if pends = s.ApprovalsFor("s1"); len(pends) != 0 {
		t.Fatalf("after resolve = %+v", pends)
	}
}

func TestStoreLoadTailRebuilds(t *testing.T) {
	s := NewStore("http://x")
	resp := &protocol.HistoryResponse{HasMore: true}
	resp.Events = append(resp.Events, protocol.HistoryEntry{
		Event: *ev(t, "user/message", 1, map[string]any{
			"id": "u1", "role": "user",
			"content": []any{map[string]any{"type": "text", "text": "hello"}},
			"source":  map[string]any{"kind": "user"},
		}),
	})
	s.LoadTail("s1", resp)
	snap := s.Get("s1")
	if snap == nil || len(snap.Items) != 1 {
		t.Fatalf("snap = %+v", snap)
	}
	if !snap.HasMore {
		t.Fatal("hasMore lost")
	}
	// Live event beyond the tail seq applies.
	changed := s.Event("s1", ev(t, "user/message", 2, map[string]any{
		"id": "u2", "role": "user",
		"content": []any{map[string]any{"type": "text", "text": "world"}},
		"source":  map[string]any{"kind": "user"},
	}))
	if !changed {
		t.Fatal("live event should change state")
	}
	if got := s.Get("s1"); len(got.Items) != 2 {
		t.Fatalf("items = %d", len(got.Items))
	}
	// Duplicate tail event is dropped by the watermark.
	dup := s.Event("s1", &resp.Events[0].Event)
	if dup {
		t.Fatal("duplicate seq should not change state")
	}
}

func mustJSON(s string) json.RawMessage { return json.RawMessage(s) }

func TestNextPermissionCycle(t *testing.T) {
	opts := []protocol.PermissionOption{
		{Value: "read-only", Name: "read-only"},
		{Value: "workspace-write", Name: "workspace-write"},
		{Value: "danger-full-access", Name: "danger-full-access"},
	}
	want := map[string]string{
		"":                   "read-only",
		"read-only":          "workspace-write",
		"workspace-write":    "danger-full-access",
		"danger-full-access": "read-only", // wraps
		"custom":             "read-only", // derived, never a switch target
		"unknown":            "read-only", // outside the table
	}
	for cur, gotWant := range want {
		if got := NextPermission(cur, opts); got != gotWant {
			t.Fatalf("NextPermission(%q) = %q, want %q", cur, got, gotWant)
		}
	}
	// "custom" in the table is not skipped over by a neighbor in one step.
	opts = append(opts, protocol.PermissionOption{Value: "custom", Name: "Custom"})
	if got := NextPermission("danger-full-access", opts); got != "read-only" {
		t.Fatalf("wrap with custom in table = %q", got)
	}
	// Empty table falls back to the standard dsh-base cycle.
	std := map[string]string{
		"":                   "read-only",
		"read-only":          "workspace-write",
		"workspace-write":    "danger-full-access",
		"danger-full-access": "read-only",
	}
	for cur, want := range std {
		if got := NextPermission(cur, nil); got != want {
			t.Fatalf("fallback cycle(%q) = %q, want %q", cur, got, want)
		}
	}
}

const permWire = `{"options":[{"value":"read-only","name":"read-only"},{"value":"workspace-write","name":"workspace-write"},{"value":"danger-full-access","name":"Full access","description":"no confinement"}],"currentValue":"workspace-write"}`

func TestStorePermissionsProjection(t *testing.T) {
	s := NewStore("http://x")
	s.MuxProjection("s1", "permissions", 10, mustJSON(permWire))
	snap := s.Get("s1")
	if snap == nil || snap.Permission == nil || snap.Permission.CurrentValue != "workspace-write" {
		t.Fatalf("snap = %+v", snap.Permission)
	}
	if len(snap.Permission.Options) != 3 || snap.Permission.Options[2].Name != "Full access" {
		t.Fatalf("options = %+v", snap.Permission.Options)
	}
	// Live push: the host switched the preset.
	const fullAccess = `{"options":[{"value":"read-only","name":"read-only"},{"value":"workspace-write","name":"workspace-write"},{"value":"danger-full-access","name":"Full access","description":"no confinement"}],"currentValue":"danger-full-access"}`
	s.MuxProjection("s1", "permissions", 11, mustJSON(fullAccess))
	if got := s.Get("s1").Permission.CurrentValue; got != "danger-full-access" {
		t.Fatalf("after push = %q", got)
	}
	// A push below the watermark must not regress the value.
	s.MuxProjection("s1", "permissions", 5, mustJSON(permWire))
	if got := s.Get("s1").Permission.CurrentValue; got != "danger-full-access" {
		t.Fatalf("stale push landed: %q", got)
	}
}

func TestStoreLoadTailPermissionsBaseline(t *testing.T) {
	s := NewStore("http://x")
	resp := &protocol.HistoryResponse{}
	resp.Events = append(resp.Events, protocol.HistoryEntry{
		Event: *ev(t, "user/message", 1, map[string]any{
			"id": "u1", "role": "user",
			"content": []any{map[string]any{"type": "text", "text": "hi"}},
			"source":  map[string]any{"kind": "user"},
		}),
	})
	resp.Projections = &protocol.ProjectionsBlock{AsOfSeq: 1, Values: map[string]json.RawMessage{
		"permissions": mustJSON(permWire),
	}}
	s.LoadTail("s1", resp)
	snap := s.Get("s1")
	if snap.Permission == nil || snap.Permission.CurrentValue != "workspace-write" {
		t.Fatalf("baseline missing: %+v", snap.Permission)
	}
	// Next cycle step from the baselined value.
	if got := NextPermission(snap.Permission.CurrentValue, snap.Permission.Options); got != "danger-full-access" {
		t.Fatalf("next = %q", got)
	}
}

// TestStoreLoadTailStalePageKeepsLiveState reproduces the boot double-load
// race: a tail page fetched before a live permission change lands after the
// change's projection push, and must not rewind the store to its stale cut.
func TestStoreLoadTailStalePageKeepsLiveState(t *testing.T) {
	s := NewStore("http://x")
	page := func(seq int64, items int, current string) *protocol.HistoryResponse {
		resp := &protocol.HistoryResponse{}
		for i := items; i >= 1; i-- {
			es := seq - int64(i-1)
			id := fmt.Sprintf("u%d", es)
			resp.Events = append(resp.Events, protocol.HistoryEntry{
				Event: *ev(t, "user/message", es, map[string]any{
					"id": id, "role": "user",
					"content": []any{map[string]any{"type": "text", "text": "hi"}},
					"source":  map[string]any{"kind": "user"},
				}),
			})
		}
		resp.Projections = &protocol.ProjectionsBlock{AsOfSeq: seq, Values: map[string]json.RawMessage{
			"permissions": mustJSON(permWireWith(current)),
		}}
		return resp
	}
	s.LoadTail("s1", page(1, 1, "read-only"))
	snap := s.Get("s1")
	if snap.Permission == nil || snap.Permission.CurrentValue != "read-only" {
		t.Fatalf("baseline missing: %+v", snap.Permission)
	}
	// Live frames advance past the page's cut.
	s.Event("s1", ev(t, "user/message", 2, map[string]any{
		"id": "u2", "role": "user",
		"content": []any{map[string]any{"type": "text", "text": "world"}},
		"source":  map[string]any{"kind": "user"},
	}))
	s.MuxProjection("s1", "permissions", 5, mustJSON(permWireWith("workspace-write")))
	if snap := s.Get("s1"); snap.Permission.CurrentValue != "workspace-write" {
		t.Fatalf("live update lost: %+v", snap.Permission)
	}
	// The stale page (cut at 1) lands late.
	stale := page(1, 1, "read-only")
	s.LoadTail("s1", stale)
	snap = s.Get("s1")
	if snap.Permission == nil || snap.Permission.CurrentValue != "workspace-write" {
		t.Fatalf("stale page rewound permission: %+v", snap.Permission)
	}
	if len(snap.Items) != 2 {
		t.Fatalf("stale page reverted the transcript: items = %d", len(snap.Items))
	}
	// The log advances further with another switch.
	s.MuxProjection("s1", "permissions", 7, mustJSON(permWireWith("danger-full-access")))
	// A fresh page cut past every live watermark re-baselines everything.
	s.LoadTail("s1", page(8, 3, "danger-full-access"))
	snap = s.Get("s1")
	if snap.Permission == nil || snap.Permission.CurrentValue != "danger-full-access" {
		t.Fatalf("fresh page must win: %+v", snap.Permission)
	}
	if len(snap.Items) != 3 {
		t.Fatalf("fresh page rebuild failed: items = %d", len(snap.Items))
	}
}

func permWireWith(current string) string {
	return `{"options":[{"value":"read-only","name":"read-only"},{"value":"workspace-write","name":"workspace-write"},{"value":"danger-full-access","name":"Full access","description":"no confinement"}],"currentValue":"` + current + `"}`
}

func TestDecodePermissionInvalid(t *testing.T) {
	if decodePermission(nil) != nil {
		t.Fatal("nil must decode to nil")
	}
	if decodePermission(mustJSON(`"str"`)) != nil {
		t.Fatal("string must decode to nil")
	}
	if decodePermission(mustJSON(`{}`)) != nil {
		t.Fatal("empty object must decode to nil")
	}
}

// TestStoreLoadTailAbsorbsRequestContext pins the context-window baseline:
// the transcript fold treats request/context as transparent metadata, so
// LoadTail must still adopt the window the session history advertises —
// otherwise the UI's context bar only appears after a live turn.
func TestStoreLoadTailAbsorbsRequestContext(t *testing.T) {
	s := NewStore("http://x")
	s.LoadTail("s1", &protocol.HistoryResponse{Events: []protocol.HistoryEntry{
		{Event: *ev(t, "turn/start", 1, map[string]any{"turn": 1})},
		{Event: *ev(t, "request/context", 12, map[string]any{
			"provider": "custom", "model": "test-model", "contextWindow": 200000,
		})},
		{Event: *ev(t, "request/context", 40, map[string]any{
			"provider": "custom", "model": "test-model", "contextWindow": 128000,
		})},
	}})
	st := s.Get("s1")
	if st.Ctx == nil || st.Ctx.ContextWindow != 128000 || st.Ctx.Model != "test-model" {
		t.Fatalf("Ctx = %+v (want the newest window 128000 / test-model)", st.Ctx)
	}
}

// TestStoreTurnEndSettlesRunning pins R5: turn/end settles the local
// running flags when no queued work remains (deployments that stop sending
// authoritative status frames must not leave a session stuck in running),
// while a non-empty queue keeps them set.
func TestStoreTurnEndSettlesRunning(t *testing.T) {
	s := NewStore("http://x")
	s.Sess("s1")
	s.Event("s1", ev(t, "turn/start", 1, map[string]any{"turn": 1}))
	if sum := s.SummaryFor("s1"); !sum.Running {
		t.Fatalf("after turn/start: %+v (want running)", sum)
	}
	s.Event("s1", ev(t, "turn/end", 2, map[string]any{
		"turn": 1, "reason": map[string]any{"kind": "completed"},
	}))
	if sum := s.SummaryFor("s1"); sum.Running {
		t.Fatalf("after turn/end (empty queue): %+v (flags must settle)", sum)
	}

	p := NewStore("http://x")
	p.Sess("s2")
	p.Event("s2", ev(t, "turn/start", 1, map[string]any{"turn": 1}))
	p.MuxQueue("s2", []protocol.QueuedInboxItem{{Id: "a", Placement: "queued",
		Message: protocol.Message{Id: "m1", Role: "user",
			Content: []protocol.ContentBlock{{Type: "text", Text: "next"}}}}})
	p.Event("s2", ev(t, "turn/end", 2, map[string]any{
		"turn": 1, "reason": map[string]any{"kind": "completed"},
	}))
	if sum := p.SummaryFor("s2"); !sum.Running {
		t.Fatalf("after turn/end (non-empty queue): %+v (running must stay)", sum)
	}
}

func TestStoreTitleCacheAndCtxModel(t *testing.T) {
	s := NewStore("http://x")
	s.SetHost(&protocol.HostDescription{Model: "host-model"})
	s.SetActive("s1")

	// No session context yet: the host default answers.
	if got := s.CtxModel("s1"); got != "host-model" {
		t.Fatalf("CtxModel fallback = %q", got)
	}
	if got := s.CtxModel("unknown"); got != "host-model" {
		t.Fatalf("CtxModel unknown session = %q", got)
	}
	// The request/context event wins over the host fallback.
	cdb, _ := json.Marshal(protocol.RequestContextData{
		Provider: "acme", Model: "sess-model", ContextWindow: 1000,
	})
	s.Event("s1", &protocol.SessionEvent{
		Type: "request/context", Seq: 1, Time: 1, Data: cdb,
	})
	if got := s.CtxModel("s1"); got != "sess-model" {
		t.Fatalf("CtxModel = %q, want the session context model", got)
	}
	// The advertised catalog resolves the raw id to the display name the
	// model picker shows (the top bar consistency pin).
	s.SetModels("s1", &protocol.SessionModels{
		Groups: []protocol.ModelProviderGroup{{Id: "acme", Name: "Acme",
			Models: []protocol.ModelCatalogModel{{Id: "sess-model", Name: "Session Model"}}}},
	})
	if got := s.CtxModel("s1"); got != "Session Model" {
		t.Fatalf("CtxModel with catalog = %q, want the display name", got)
	}
	// A catalog that does not carry the model keeps the raw id.
	s.SetModels("s1", &protocol.SessionModels{
		Groups: []protocol.ModelProviderGroup{{Id: "acme", Name: "Acme",
			Models: []protocol.ModelCatalogModel{{Id: "foreign", Name: "Foreign"}}}},
	})
	if got := s.CtxModel("s1"); got != "sess-model" {
		t.Fatalf("CtxModel with foreign catalog = %q, want the raw id", got)
	}
	// Dropping the catalog reverts to the raw id.
	s.SetModels("s1", nil)
	if got := s.CtxModel("s1"); got != "sess-model" {
		t.Fatalf("CtxModel after catalog drop = %q, want the raw id", got)
	}
	// The host fallback resolves through the session's catalog too (a
	// resumed session may not have had a request/context event yet); a
	// session row without a catalog keeps the raw host id.
	s.SetModels("s2", &protocol.SessionModels{
		Groups: []protocol.ModelProviderGroup{{Id: "acme", Name: "Acme",
			Models: []protocol.ModelCatalogModel{{Id: "host-model", Name: "Host Model"}}}},
	})
	if got := s.CtxModel("s2"); got != "Host Model" {
		t.Fatalf("CtxModel host fallback with catalog = %q, want the display name", got)
	}
	if got := s.CtxModel("no-such-session"); got != "host-model" {
		t.Fatalf("CtxModel host fallback without a session row = %q, want the raw id", got)
	}
}

func TestStoreTitleCacheWriteThrough(t *testing.T) {
	s := NewStore("http://x")
	r0 := s.Rev()
	s.MuxProjection("s1", "title", 10, mustJSON(`"hello"`))
	if got := s.TitleFor("s1"); got != "hello" {
		t.Fatalf("title = %q", got)
	}
	if s.Rev() <= r0 {
		t.Fatal("rev must advance on every dirty pulse")
	}
	// Lower seq: the projection AND its decoded cache keep the newer value.
	s.MuxProjection("s1", "title", 5, mustJSON(`"stale"`))
	if got := s.TitleFor("s1"); got != "hello" {
		t.Fatalf("lower-seq title overwrote the cache: %q", got)
	}
	// A newer projection replaces the cached decode.
	s.MuxProjection("s1", "title", 20, mustJSON(`"world"`))
	if got := s.TitleFor("s1"); got != "world" {
		t.Fatalf("title = %q", got)
	}
	// Snapshot path and sidebar rows share the same cached read.
	snap := s.Get("s1")
	if snap == nil || snap.Title != "world" {
		t.Fatalf("snapshot title = %+v", snap)
	}
	for _, r := range s.Roster() {
		if r.Id == "s1" && r.Title != "world" {
			t.Fatalf("roster title = %q", r.Title)
		}
	}
}

func TestStoreNoticeStripsControl(t *testing.T) {
	s := NewStore("http://x")
	s.Notify(Notice{Level: "err", Text: "bad\x1b[31mred\x1b[38m"})
	select {
	case n := <-s.Notices():
		if n.Text != "bad[31mred[38m" {
			t.Fatalf("notice text = %q", n.Text)
		}
	case <-time.After(time.Second):
		t.Fatal("no notice delivered")
	}
}

// TestStoreBootStaleRunningSettles reproduces the boot bug: the warm cache
// records a session as running (stale-while-revalidate), the auto-loaded
// session's replayed tail ends mid-turn (turn/start, no turn/end), and the
// live session.list then reports the session as NOT running. Before the fix
// the liveness stayed stuck (stale st.Running + dangling TurnActive), so the
// top bar spun the "thinking" verb forever with a stale elapsed time.
func TestStoreBootStaleRunningSettles(t *testing.T) {
	s := NewStore("http://x")

	// 1) boot cache installs a stale running=true for the session.
	if !s.CacheRoster([]CacheRow{{Id: "s1", Running: true, Cwd: "/w", Title: "t"}}) {
		t.Fatal("cache should install")
	}

	// 2) tail load replays a log whose last turn never ended.
	resp := &protocol.HistoryResponse{HasMore: false}
	resp.Events = append(resp.Events, protocol.HistoryEntry{
		Event: *ev(t, "turn/start", 2, map[string]any{"turn": 1}),
	})
	s.LoadTail("s1", resp)
	if snap := s.Get("s1"); snap == nil || !snap.TurnActive {
		t.Fatalf("expected a dangling (active) turn after replay, got %+v", snap)
	}

	// 3) live session.list lands: the host says the session is NOT running.
	s.SetSessions([]protocol.SessionSummary{{SessionId: "s1", Running: false, Cwd: "/w"}})

	snap := s.Get("s1")
	if snap == nil {
		t.Fatal("nil snap")
	}
	// The authoritative liveness flag must be re-baselined to false even
	// though the folded log still shows a dangling turn (TurnActive may
	// remain true; liveness is now the running flag alone).
	if snap.Running {
		t.Fatalf("Running = true after live list says not running (stale cache not re-baselined)")
	}
}

// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package core

import (
	"encoding/json"
	"testing"

	"dsh-cli/internal/protocol"
)

func ev(t *testing.T, typ string, seq int64, data any) *protocol.SessionEvent {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return &protocol.SessionEvent{Type: typ, Seq: seq, Time: seq * 1000, Data: raw}
}

func assistantMsg(id string, blocks ...string) map[string]any {
	return map[string]any{
		"turn": 1,
		"step": 1,
		"message": map[string]any{
			"id":      id,
			"role":    "assistant",
			"content": contentBlocksRaw(blocks),
			"source":  map[string]any{"kind": "model"},
		},
		"usage": map[string]any{"inputTokens": 100, "outputTokens": 20},
	}
}

func contentBlocksRaw(types []string) []any {
	out := []any{}
	for _, ty := range types {
		b := map[string]any{"type": ty}
		b["text"] = "text-" + string(rune('a'+len(out)))
		if ty == "tool-call" {
			b["id"] = "call-1"
			b["name"] = "bash"
			b["arguments"] = `{"cmd":"ls"}`
			b["text"] = ""
		}
		if ty == "tool-result" {
			b["toolCallId"] = "call-1"
			b["content"] = []any{map[string]any{"type": "text", "text": "ok output"}}
			b["text"] = ""
		}
		out = append(out, b)
	}
	return out
}

func TestFoldStreamingAssembly(t *testing.T) {
	tr := NewTranscript()
	tr.Apply(ev(t, "turn/start", 1, map[string]any{"turn": 1}))

	chunk := func(seq int64, data map[string]any) {
		t.Helper()
		tr.Apply(ev(t, "assistant/chunk", seq, map[string]any{
			"turn": 1, "step": 1, "chunk": data,
		}))
	}
	chunk(2, map[string]any{"type": "block-start", "index": 0})
	chunk(3, map[string]any{"type": "text-delta", "index": 0, "text": "Hel"})
	chunk(4, map[string]any{"type": "text-delta", "index": 0, "text": "lo"})
	chunk(5, map[string]any{"type": "block-start", "index": 1})
	chunk(6, map[string]any{"type": "reasoning-delta", "index": 1, "text": "hm…"})
	chunk(7, map[string]any{"type": "block-end", "index": 0})
	chunk(8, map[string]any{"type": "block-end", "index": 1})

	if len(tr.Items) != 1 {
		t.Fatalf("want 1 streaming item, got %d", len(tr.Items))
	}
	it := tr.Items[0]
	if it.Kind != KindAssistant {
		t.Fatalf("kind = %v", it.Kind)
	}
	if len(it.Blocks) != 2 {
		t.Fatalf("blocks = %d", len(it.Blocks))
	}
	if it.Blocks[0].Text != "Hello" {
		t.Fatalf("text = %q", it.Blocks[0].Text)
	}
	if it.Blocks[1].Kind != "reasoning" || it.Blocks[1].Text != "hm…" {
		t.Fatalf("reasoning block = %+v", it.Blocks[1])
	}

	// Canonical message finalizes: the streaming item is replaced by the
	// canonical content.
	tr.Apply(ev(t, "assistant/message", 9, assistantMsg("m1", "text")))
	if len(tr.Items) != 1 {
		t.Fatalf("items after canonical = %d", len(tr.Items))
	}
	last := tr.Items[0]
	if last.Seq != 9 {
		t.Fatalf("canonical item must replace the partial (seq = %d)", last.Seq)
	}
	if len(last.Blocks) != 1 || last.Blocks[0].Kind != "text" {
		t.Fatalf("canonical = %+v", last.Blocks)
	}
	if tr.TurnTokens.Out != 20 || tr.TurnTokens.In != 100 {
		t.Fatalf("tokens = %+v", tr.TurnTokens)
	}

	// Turn end marker.
	tr.Apply(ev(t, "turn/end", 10, map[string]any{
		"turn": 1, "reason": map[string]any{"kind": "completed"},
	}))
	if len(tr.Items) != 2 {
		t.Fatalf("items after turn/end = %d", len(tr.Items))
	}
	end := tr.Items[1]
	if end.Kind != KindTurnEnd || end.TurnEnd.Kind != "completed" {
		t.Fatalf("turn end = %+v", end)
	}
	if end.TurnMs <= 0 {
		t.Fatalf("turn ms = %d", end.TurnMs)
	}
	if tr.TurnActive {
		t.Fatal("turn should be inactive")
	}
}

// The usage chunk (the adapter's step accounting, landing ahead of the
// canonical message) counts live: the turn tokens, the in-flight item and
// the generation sample move at once, and the canonical message folds only
// the remainder (no double count, no double sample).
func TestFoldUsageChunkLive(t *testing.T) {
	tr := NewTranscript()
	tr.Apply(ev(t, "turn/start", 1, map[string]any{"turn": 1}))
	chunk := func(seq int64, data map[string]any) {
		t.Helper()
		tr.Apply(ev(t, "assistant/chunk", seq, map[string]any{
			"turn": 1, "step": 1, "chunk": data,
		}))
	}
	chunk(2, map[string]any{"type": "block-start", "index": 0, "blockType": "text"})
	chunk(3, map[string]any{"type": "text-delta", "index": 0, "text": "Hel"})
	chunk(4, map[string]any{"type": "text-delta", "index": 0, "text": "lo"})

	// The usage chunk: the step's accounting lands before the canonical.
	chunk(5, map[string]any{"type": "usage", "usage": map[string]any{
		"inputTokens": 100, "cacheReadTokens": 30, "outputTokens": 20,
	}})
	if tr.TurnTokens.In != 130 || tr.TurnTokens.Out != 20 || tr.TurnTokens.Cache != 30 {
		t.Fatalf("turn tokens after usage chunk = %+v, want In 130 Out 20 Cache 30", tr.TurnTokens)
	}
	if s := tr.Streaming(); s == nil || s.Usage == nil {
		t.Fatalf("in-flight item carries no live usage: %+v", s)
	} else if s.Usage.InputTokens != 100 || s.Usage.CacheReadTokens != 30 || s.Usage.OutputTokens != 20 {
		t.Fatalf("live usage = %+v", s.Usage)
	}
	if len(tr.Recent) != 1 || tr.Recent[0].Out != 20 {
		t.Fatalf("gen samples after usage chunk = %+v, want one (20 out)", tr.Recent)
	}

	// The canonical message with the same usage folds no remainder.
	msg := assistantMsg("m1", "text")
	msg["usage"] = map[string]any{"inputTokens": 100, "cacheReadTokens": 30, "outputTokens": 20}
	tr.Apply(ev(t, "assistant/message", 6, msg))
	if tr.TurnTokens.In != 130 || tr.TurnTokens.Out != 20 || tr.TurnTokens.Cache != 30 {
		t.Fatalf("turn tokens after canonical = %+v (double count)", tr.TurnTokens)
	}
	if len(tr.Recent) != 1 {
		t.Fatalf("gen samples after canonical = %d, want 1 (double sample)", len(tr.Recent))
	}
	last := tr.Items[len(tr.Items)-1]
	if last.Seq != 6 || last.Usage == nil || tr.Streaming() != nil {
		t.Fatalf("canonical = %+v, streaming = %+v", last, tr.Streaming())
	}

	// The turn-end marker carries the counted totals.
	tr.Apply(ev(t, "turn/end", 7, map[string]any{
		"turn": 1, "reason": map[string]any{"kind": "completed"},
	}))
	end := tr.Items[len(tr.Items)-1]
	if end.Kind != KindTurnEnd || end.TurnTok.In != 130 || end.TurnTok.Out != 20 {
		t.Fatalf("turn end = %+v", end)
	}
}

// A canonical message for a step that streamed no usage chunk still counts
// from the canonical (the preview is nil, the delta is the full usage); a
// partial preview folds the remainder on top.
func TestFoldUsageChunkDelta(t *testing.T) {
	tr := NewTranscript()
	tr.Apply(ev(t, "turn/start", 1, map[string]any{"turn": 1}))
	chunk := func(seq int64, step int, data map[string]any) {
		t.Helper()
		tr.Apply(ev(t, "assistant/chunk", seq, map[string]any{
			"turn": 1, "step": step, "chunk": data,
		}))
	}

	// Step 1: no usage chunk — the canonical alone counts the step.
	chunk(2, 1, map[string]any{"type": "block-start", "index": 0, "blockType": "text"})
	chunk(3, 1, map[string]any{"type": "text-delta", "index": 0, "text": "one"})
	msg1 := assistantMsg("m1", "text")
	msg1["step"] = 1
	tr.Apply(ev(t, "assistant/message", 4, msg1))
	if tr.TurnTokens.In != 100 || tr.TurnTokens.Out != 20 {
		t.Fatalf("step 1 tokens = %+v, want In 100 Out 20", tr.TurnTokens)
	}

	// Step 2: a partial preview, then the canonical's fuller accounting.
	chunk(5, 2, map[string]any{"type": "text-delta", "index": 0, "text": "two"})
	chunk(6, 2, map[string]any{"type": "usage", "usage": map[string]any{
		"inputTokens": 50, "outputTokens": 10,
	}})
	msg2 := assistantMsg("m2", "text")
	msg2["step"] = 2
	msg2["usage"] = map[string]any{"inputTokens": 50, "outputTokens": 12}
	tr.Apply(ev(t, "assistant/message", 7, msg2))
	if tr.TurnTokens.In != 150 || tr.TurnTokens.Out != 32 {
		t.Fatalf("step 2 tokens = %+v, want In 150 Out 32 (preview + remainder)", tr.TurnTokens)
	}
	if len(tr.Recent) != 2 {
		t.Fatalf("gen samples = %d, want 2 (one per step)", len(tr.Recent))
	}
}

// A canonical message for a different step does not supersede the in-flight
// partial of another step; both rows are kept.
func TestFoldStreamingStepMismatch(t *testing.T) {
	tr := NewTranscript()
	tr.Apply(ev(t, "turn/start", 1, map[string]any{"turn": 1}))
	chunk := func(seq int64, step any, data map[string]any) {
		t.Helper()
		tr.Apply(ev(t, "assistant/chunk", seq, map[string]any{
			"turn": 1, "step": step, "chunk": data,
		}))
	}
	chunk(2, 1, map[string]any{"type": "block-start", "index": 0})
	chunk(3, 1, map[string]any{"type": "text-delta", "index": 0, "text": "partial"})

	msg := assistantMsg("m9", "text")
	msg["step"] = 2
	tr.Apply(ev(t, "assistant/message", 4, msg))

	if len(tr.Items) != 2 {
		t.Fatalf("items = %d, want 2 (partial kept + canonical appended)", len(tr.Items))
	}
	if tr.Items[0].Blocks[0].Text != "partial" {
		t.Fatalf("partial lost: %+v", tr.Items[0].Blocks)
	}
	if tr.Items[1].Seq != 4 {
		t.Fatalf("canonical not appended: %+v", tr.Items[1])
	}
	if tr.streamItem != nil {
		t.Fatal("streamItem should be cleared after finalization")
	}
}
func TestFoldToolResultMatching(t *testing.T) {
	tr := NewTranscript()
	tr.Apply(ev(t, "tool/call", 1, map[string]any{
		"turn": 1, "step": 1, "callId": "call-1", "name": "bash", "arguments": "ls -la",
	}))
	res := assistantMsg("m1", "tool-call")
	tr.Apply(ev(t, "assistant/message", 2, res))
	tr.Apply(ev(t, "tool/result", 3, map[string]any{
		"turn": 1, "step": 1,
		"message": map[string]any{
			"id":      "r1",
			"role":    "tool",
			"content": contentBlocksRaw([]string{"tool-result"}),
			"source":  map[string]any{"kind": "tool"},
		},
	}))
	// No "unmatched result" note should appear, and the registered tool
	// block must be marked done.
	for _, it := range tr.Items {
		if it.Kind == KindNote {
			t.Fatalf("unexpected note: %q", it.Note)
		}
	}
	if len(tr.openTools) != 0 {
		t.Fatalf("open tools remain: %v", tr.openTools)
	}
	// The assistant item's tool block is shared and marked done.
	found := false
	for _, it := range tr.Items {
		for _, b := range it.Blocks {
			if b.Tool != nil && b.Tool.Id == "call-1" {
				found = true
				if !b.Tool.Done || b.Tool.ResultText == "" {
					t.Fatalf("tool block = %+v", b.Tool)
				}
			}
		}
	}
	if !found {
		t.Fatal("no tool block with id call-1 in the assistant item")
	}
}

func TestFoldWatermarkAndEcho(t *testing.T) {
	tr := NewTranscript()
	tr.Apply(ev(t, "user/message", 5, map[string]any{
		"id": "u1", "role": "user",
		"content": []any{map[string]any{"type": "text", "text": "hi"}},
		"source":  map[string]any{"kind": "user"},
	}))
	// Duplicate (same or lower seq) is dropped.
	if changed, _ := tr.Apply(ev(t, "user/message", 5, map[string]any{
		"id": "u1", "role": "user",
		"content": []any{map[string]any{"type": "text", "text": "hi"}},
		"source":  map[string]any{"kind": "user"},
	})); changed {
		t.Fatal("duplicate seq should not change the fold")
	}
	if len(tr.Items) != 1 {
		t.Fatalf("items = %d", len(tr.Items))
	}

	// Optimistic echo reconciles with the event carrying the rpcId.
	tr.AddPendingUser("ping", "rpc-1", 6000)
	if len(tr.Items) != 2 {
		t.Fatalf("items with pending = %d", len(tr.Items))
	}
	tr.Apply(ev(t, "user/message", 6, map[string]any{
		"id": "u2", "role": "user",
		"content": []any{map[string]any{"type": "text", "text": "ping"}},
		"source":  map[string]any{"kind": "user", "rpcId": "rpc-1"},
	}))
	n := 0
	for _, it := range tr.Items {
		if it.Kind == KindUser {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("user items = %d (want 2: reconciled pending + canonical)", n)
	}
}

// TestFoldContextInjectionHidden checks that model-facing context
// injections (the runtime-context snapshot, the skill catalog — user/message
// events carrying a context form in source.form) stay out of the transcript
// while still advancing the seq watermark.
func TestFoldContextInjectionHidden(t *testing.T) {
	tr := NewTranscript()
	tr.Apply(ev(t, "user/message", 1, map[string]any{
		"id": "u1", "role": "user",
		"content": []any{map[string]any{"type": "text", "text": "hello"}},
		"source":  map[string]any{"kind": "user"},
	}))
	// The runtime-context snapshot: plugin source with form snapshot.
	tr.Apply(ev(t, "user/message", 2, map[string]any{
		"id": "ctx1", "role": "user",
		"content": []any{map[string]any{"type": "text", "text": "Current runtime context. ..."}},
		"source":  map[string]any{"kind": "plugin", "plugin": "@deepseek-ai/dsh-system-prompt", "form": "snapshot"},
	}))
	if len(tr.Items) != 1 || tr.Items[0].Text != "hello" {
		t.Fatalf("snapshot must not render: items = %+v", tr.Items)
	}
	// The snapshot's seq counts for the watermark: a resend is dropped.
	if changed, _ := tr.Apply(ev(t, "user/message", 2, map[string]any{
		"id": "ctx1", "role": "user",
		"content": []any{map[string]any{"type": "text", "text": "Current runtime context. ..."}},
		"source":  map[string]any{"kind": "plugin", "plugin": "@deepseek-ai/dsh-system-prompt", "form": "snapshot"},
	})); changed {
		t.Fatal("resend of the snapshot must be dropped")
	}
	// User messages keep rendering after the hidden one.
	tr.Apply(ev(t, "user/message", 3, map[string]any{
		"id": "u3", "role": "user",
		"content": []any{map[string]any{"type": "text", "text": "next"}},
		"source":  map[string]any{"kind": "user"},
	}))
	if len(tr.Items) != 2 || tr.Items[1].Text != "next" {
		t.Fatalf("items = %+v", tr.Items)
	}
	// The skill catalog injection is hidden the same way.
	tr.Apply(ev(t, "user/message", 5, map[string]any{
		"id": "sk1", "role": "user",
		"content": []any{map[string]any{"type": "text", "text": "A skill is a reusable set of task-specific instructions..."}},
		"source":  map[string]any{"kind": "skill-catalog", "form": "catalog"},
	}))
	// Plugin notices without a declared form (the user-approval policy
	// change messages) are model notes too: hidden as well.
	tr.Apply(ev(t, "user/message", 6, map[string]any{
		"id": "n1", "role": "user",
		"content": []any{map[string]any{"type": "text", "text": "The approval policy changed from \"ask\" to \"never\" (changed by the user)."}},
		"source":  map[string]any{"kind": "plugin", "plugin": "user-approval"},
	}))
	if len(tr.Items) != 2 {
		t.Fatalf("injections must not render: items = %+v", tr.Items)
	}
}

func TestFoldPrependOrdering(t *testing.T) {
	tr := NewTranscript()
	// Live session already absorbed seq 10..12.
	for _, e := range []*protocol.SessionEvent{
		ev(t, "user/message", 10, map[string]any{"id": "x", "role": "user",
			"content": []any{map[string]any{"type": "text", "text": "live"}},
			"source":  map[string]any{"kind": "user"}}),
	} {
		tr.Apply(e)
	}
	// An older page (7..9) is prepended out of order.
	older := []*protocol.SessionEvent{
		ev(t, "user/message", 9, map[string]any{"id": "z", "role": "user",
			"content": []any{map[string]any{"type": "text", "text": "old2"}},
			"source":  map[string]any{"kind": "user"}}),
		ev(t, "user/message", 7, map[string]any{"id": "a", "role": "user",
			"content": []any{map[string]any{"type": "text", "text": "old1"}},
			"source":  map[string]any{"kind": "user"}}),
	}
	tr.Prepend(older)
	if len(tr.Items) != 3 {
		t.Fatalf("items = %d", len(tr.Items))
	}
	if tr.Items[0].Text != "old1" || tr.Items[1].Text != "old2" || tr.Items[2].Text != "live" {
		t.Fatalf("order = %q %q %q", tr.Items[0].Text, tr.Items[1].Text, tr.Items[2].Text)
	}
}

func TestFoldUnknownEventFallback(t *testing.T) {
	tr := NewTranscript()
	tr.Apply(ev(t, "my-plugin/thing", 1, map[string]any{"x": 1}))
	if len(tr.Items) != 1 {
		t.Fatalf("items = %d", len(tr.Items))
	}
	if tr.Items[0].Kind != KindUnknown || tr.Items[0].Note != "my-plugin/thing" {
		t.Fatalf("item = %+v", tr.Items[0])
	}
}

// TestUserMessageDropsMatchingEcho pins the double-line fix: the host may
// stamp the user/message event with its own rpcId, so the id-based echo
// reconcile misses; the pending placeholder of the same text must still be
// dropped, leaving exactly one line for the prompt.
func TestUserMessageDropsMatchingEcho(t *testing.T) {
	t.Helper()
	tt := NewTranscript()
	tt.AddPendingUser("只回复一个词：好", "client-uuid-1", 1)
	ev := ev(t, "user/message", 7, protocol.Message{
		Id:      "m1",
		Role:    "user",
		Source:  protocol.MessageSource{Kind: "user", RpcId: "host-uuid-2"},
		Content: []protocol.ContentBlock{{Type: "text", Text: "只回复一个词：好"}},
	})
	tt.Apply(ev)
	if len(tt.Items) != 1 {
		t.Fatalf("items = %d, want exactly one (the real message)", len(tt.Items))
	}
	if tt.Items[0].EchoRpcId != "" {
		t.Fatalf("surviving item is the optimistic echo: %+v", tt.Items[0])
	}
}

// TestItemRenderIdentity pins the Gen/Ver contract the UI render cache
// relies on: Gen is unique per transcript and stable per item; every
// mutation republishes the item through a fresh pointer (COW) with the
// same Gen and a bumped Ver, so a changed row re-renders and an untouched
// row does not.
func TestItemRenderIdentity(t *testing.T) {
	tr := NewTranscript()
	u1 := &Item{Kind: KindUser, Text: "one"}
	u2 := &Item{Kind: KindUser, Text: "two"}
	tr.Items = append(tr.Items, tr.stamp(u1), tr.stamp(u2))
	if u1.Gen == u2.Gen || u1.Gen == 0 {
		t.Fatalf("Gen must be non-zero and unique: %d %d", u1.Gen, u2.Gen)
	}

	// Streaming assembly: each delta republishes the in-flight item with
	// the same Gen and a bumped Ver.
	tr.Apply(ev(t, "turn/start", 1, map[string]any{"turn": 1}))
	tr.Apply(ev(t, "assistant/chunk", 2, map[string]any{
		"turn": 1, "step": 1,
		"chunk": map[string]any{"type": "text-delta", "index": 0, "text": "he"},
	}))
	first := tr.Items[len(tr.Items)-1]
	vAfterDelta := first.Ver
	genAfterDelta := first.Gen
	tr.Apply(ev(t, "assistant/chunk", 3, map[string]any{
		"turn": 1, "step": 1,
		"chunk": map[string]any{"type": "text-delta", "index": 0, "text": "llo"},
	}))
	second := tr.Items[len(tr.Items)-1]
	if second == first {
		t.Fatal("COW: the second delta must publish a fresh item pointer")
	}
	if second.Gen != genAfterDelta || second.Ver != vAfterDelta+1 {
		t.Fatalf("second delta: Gen %d Ver %d, want Gen %d Ver %d",
			second.Gen, second.Ver, genAfterDelta, vAfterDelta+1)
	}

	// Tool result updates the live block: the displaying item is
	// republished carrying a private copy with the result attached.
	tr2 := NewTranscript()
	a := tr2.buildAssistantItem(&protocol.Message{
		Content: []protocol.ContentBlock{{Type: "tool-call", Id: "call-x", Name: "bash", Arguments: `{"a":1}`}},
	}, nil, 1, 1, 10, 1000)
	tr2.Items = append(tr2.Items, tr2.stamp(a))
	res := toolResultEv(t, "call-x")
	tr2.Apply(res)
	b := tr2.Items[len(tr2.Items)-1]
	if b == a {
		t.Fatal("COW: the result must publish a fresh item pointer")
	}
	if b.Gen != a.Gen || b.Ver != a.Ver+1 {
		t.Fatalf("result re-publish: Gen %d Ver %d, want Gen %d Ver %d",
			b.Gen, b.Ver, a.Gen, a.Ver+1)
	}
	// Gen stays untouched (identity, not a mutation counter) and differs
	// from every other item in its transcript.
	if b.Gen <= 0 {
		t.Fatal("Gen must be stamped")
	}

	// command/done attach republishes the item with Ver bumped.
	tr3 := NewTranscript()
	ci := tr3.stamp(&Item{Kind: KindCommand, Seq: 5, CmdRun: &protocol.CommandRunData{CommandId: "c1", Name: "model"}})
	tr3.Items = append(tr3.Items, ci)
	tr3.Apply(ev(t, "command/done", 6, protocol.CommandDoneData{CommandId: "c1", Kind: "success"}))
	d := tr3.Items[0]
	if d == ci {
		t.Fatal("COW: the done attach must publish a fresh item pointer")
	}
	if d.Gen != ci.Gen || d.Ver == 0 || d.CmdDone == nil {
		t.Fatalf("done attach: Gen %d Ver %d CmdDone %v", d.Gen, d.Ver, d.CmdDone)
	}
}

func toolResultEv(t *testing.T, callId string) *protocol.SessionEvent {
	t.Helper()
	return ev(t, "tool/result", 20, map[string]any{
		"message": map[string]any{
			"id":   "m1",
			"role": "user",
			"content": []any{map[string]any{"type": "tool-result", "toolCallId": callId,
				"content": []any{map[string]any{"type": "text", "text": "done"}}}},
		},
	})
}

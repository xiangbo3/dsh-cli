// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

// Package core is the state engine between the wire and the UI: a pure
// event-log fold that turns the DSH session log (history replay and live
// frames alike) into transcript items, plus a per-session store that keeps
// summary state, projections, docks and pending answerable frames.
package core

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"dsh-cli/internal/protocol"
	"dsh-cli/internal/textutil"
)

// ItemKind classifies one transcript row.
type ItemKind int

const (
	KindUser ItemKind = iota
	KindAssistant
	KindTurnEnd
	KindCommand
	KindNote
	KindUnknown
)

// ToolBlock is one tool call with, once matched, its result.
type ToolBlock struct {
	Id         string
	Name       string
	Args       string
	ArgsFull   string
	ResultText string
	IsError    bool
	Done       bool
	CallSeq    int64
	CallTime   int64
	DoneTime   int64 // 0 until the result lands (or the turn-end sweep)
}

// ABlock is one assistant content block in display order.
type ABlock struct {
	Kind string // "text" | "reasoning" | "tool"
	Text string
	Tool *ToolBlock
}

// Item is one transcript row.
type Item struct {
	Kind ItemKind
	Seq  int64
	Time int64
	// KindUser
	Text      string
	ImageCnt  int
	EchoRpcId string // optimistic placeholder until the event reconciles it
	// KindAssistant
	Blocks []ABlock
	Usage  *protocol.TokenUsage
	Turn   int
	Step   int
	// KindTurnEnd
	TurnMs  int64
	TurnTok TurnTokens
	TurnEnd *protocol.TurnEndReason
	// KindCommand
	CmdRun  *protocol.CommandRunData
	CmdDone *protocol.CommandDoneData
	// KindNote / KindUnknown
	Note      string
	Raw       string
	Ignorable bool
	// Render identity for the UI cache: Gen is unique per transcript and
	// item (assigned at creation, immune to GC address reuse); Ver bumps
	// on every in-place mutation so a changed item re-renders.
	Gen int
	Ver int
}

// TurnTokens aggregates a turn's usage.
type TurnTokens struct {
	In  int // input plus cache reads
	Out int
	// Cache is the re-read cache share of In: the turn's fresh
	// throughput is In + Out - Cache (a cache hit re-serves already
	// processed context, not new work).
	Cache int
}

// Transcript is the folded view of one session's event log.
type Transcript struct {
	Items []*Item

	maxSeq int64
	// streaming assembly for the in-flight assistant message
	streaming bool
	curTurn   int
	curStep   int
	curBlocks map[int]*ABlock
	curOrder  []int
	curItem   *Item
	// curItemIdx is curItem's position in Items (-1 while absent): every
	// other mutation goes through COW (fresh slices / fresh item copies),
	// so the index only moves when Prepend or removeItem shifts it.
	curItemIdx int
	streamItem *Item // in-flight streaming item; superseded by assistant/message
	// live usage preview for the in-flight step (a usage chunk's payload):
	// folded into TurnTokens as it lands (the canonical message then folds
	// only the delta), and carried by the in-flight item for the readouts.
	streamUsage   *protocol.TokenUsage
	streamSampled bool // the step's generation sample is on record
	// tool-call result matching
	openTools map[string]*ToolBlock
	genSeq    int
	// turn bookkeeping (exposed for store-level stats)
	TurnStartAt  int64
	TurnStartNum int // turn number of the turn/start that set TurnStartAt (0: unknown)
	TurnTokens   TurnTokens
	TurnActive   bool
	// Recent holds the server's token-generation samples (one per
	// assistant message, output at arrival), rolling over GenWindow —
	// the status bar's t/s readout averages these.
	Recent []GenSample
}

// GenSample is one point of the server's token generation: the output
// tokens an assistant message carried, at the time it arrived.
type GenSample struct {
	At  int64 // unix milliseconds
	Out int
}

// GenWindow bounds the rolling window of generation samples (the
// status bar's t/s readout's "recent").
const GenWindow = 60 * time.Second

// attrTurn keeps TurnStartAt attributed to the turn that started it. A
// re-baseline tail page that predates the current turn's turn/start may
// carry an OLDER turn's start instead: any turn-bound event whose number
// differs from the recorded one (both known) invalidates the start, so
// the elapsed readout degrades to "0s" instead of counting from a foreign
// (or 1970) timestamp.
func (t *Transcript) attrTurn(n int) {
	if n > 0 && t.TurnStartNum > 0 && n != t.TurnStartNum {
		t.TurnStartAt = 0
		t.TurnStartNum = n
	}
}

// NewTranscript returns an empty transcript fold.
func NewTranscript() *Transcript {
	return &Transcript{
		curItemIdx: -1,
		curBlocks:  map[int]*ABlock{},
		openTools:  map[string]*ToolBlock{},
	}
}

// stamp marks it as the genSeq'th item of this transcript (render identity).
func (t *Transcript) stamp(it *Item) *Item {
	t.genSeq++
	it.Gen = t.genSeq
	return it
}

// MaxSeq is the highest event seq this fold has absorbed.
func (t *Transcript) MaxSeq() int64 { return t.maxSeq }

// AddUser records a canonical user message (from history or live log).
func (t *Transcript) AddUser(m *protocol.Message, time int64, seq int64) {
	t.noteSeq(seq, time)
	it := t.stamp(&Item{Kind: KindUser, Seq: seq, Time: time, Text: protocol.SessionTextMessage(m)})
	for _, b := range m.Content {
		if b.Type == "image" {
			it.ImageCnt++
		}
	}
	t.Items = append(t.Items, it)
}

// AddPendingUser records an optimistic echo of a prompt we just sent. It is
// replaced when the matching user/message event (source.rpcId) arrives.
func (t *Transcript) AddPendingUser(text, rpcId string, time int64) {
	t.Items = append(t.Items, t.stamp(&Item{Kind: KindUser, Time: time, Text: text, EchoRpcId: rpcId}))
}

// ReconcileEcho drops the optimistic placeholder for a sent prompt once its
// canonical event has been applied (or never sent, e.g. a slash command).
func (t *Transcript) ReconcileEcho(rpcId string) bool {
	for i, it := range t.Items {
		if it.EchoRpcId == rpcId {
			t.removeItem(i)
			return true
		}
	}
	return false
}

// dropPendingEchoText drops the most recent optimistic placeholder whose
// text matches the arriving user message. The host may stamp the
// user/message event with its own rpcId, so the id-based reconcile can
// miss; the text match keeps a prompt from rendering twice.
func dropPendingEchoText(t *Transcript, text string) bool {
	for i := len(t.Items) - 1; i >= 0; i-- {
		it := t.Items[i]
		if it.Kind == KindUser && it.EchoRpcId != "" && it.Text == text {
			t.removeItem(i)
			return true
		}
	}
	return false
}

// Apply absorbs one session event in order. It returns whether the fold
// changed. Out-of-order events (seq <= maxSeq) are dropped.
func (t *Transcript) Apply(ev *protocol.SessionEvent) (bool, *protocol.TokenUsage) {
	if ev.Seq > 0 && ev.Seq <= t.maxSeq {
		return false, nil
	}
	switch ev.Type {
	case "turn/start":
		var d protocol.TurnStartEventData
		if err := json.Unmarshal(ev.Data, &d); err != nil {
			return false, nil
		}
		t.TurnActive = true
		t.TurnStartAt = ev.Time
		t.TurnStartNum = d.Turn
		t.TurnTokens = TurnTokens{}
		return true, nil
	case "turn/end":
		var d protocol.TurnEndEventData
		if err := json.Unmarshal(ev.Data, &d); err != nil {
			return false, nil
		}
		t.attrTurn(d.Turn)
		t.finalizeStreaming()
		ms := int64(0)
		if t.TurnStartAt > 0 && ev.Time > t.TurnStartAt {
			ms = ev.Time - t.TurnStartAt
		}
		t.Items = append(t.Items, t.stamp(&Item{
			Kind:    KindTurnEnd,
			Seq:     ev.Seq,
			Time:    ev.Time,
			TurnMs:  ms,
			TurnTok: t.TurnTokens,
			TurnEnd: &d.Reason,
		}))
		t.TurnActive = false
		for _, tb := range t.openTools {
			tb.Done = true // an open tool call at turn end got no result
			tb.DoneTime = ev.Time
		}
		t.openTools = map[string]*ToolBlock{}
		t.noteSeq(ev.Seq, ev.Time)
		return true, nil
	case "step/start", "step/end":
		t.noteSeq(ev.Seq, ev.Time)
		return false, nil // step boundaries stay invisible; the spinner tracks them
	case "user/message":
		var m protocol.Message
		if err := json.Unmarshal(ev.Data, &m); err != nil {
			return false, nil
		}
		if m.Source.RpcId != "" {
			t.ReconcileEcho(m.Source.RpcId)
		}
		if contextInjection(&m) {
			// Model-facing context injection, not a user utterance: keep it
			// out of the transcript but advance the seq watermark so replay
			// and out-of-order drops behave the same.
			t.noteSeq(ev.Seq, ev.Time)
			return true, nil
		}
		dropPendingEchoText(t, protocol.SessionTextMessage(&m))
		t.AddUser(&m, ev.Time, ev.Seq)
		return true, nil
	case "assistant/message":
		var d protocol.AssistantMessageEventData
		if err := json.Unmarshal(ev.Data, &d); err != nil {
			return false, nil
		}
		// The live preview belongs to the in-flight (turn, step); a
		// canonical for another step (a replay gap) counts from scratch.
		if !(t.streaming && t.curTurn == d.Turn && t.curStep == d.Step) {
			t.streamUsage = nil
			t.streamSampled = false
		}
		t.finalizeStreaming()
		t.attrTurn(d.Turn)
		it := t.buildAssistantItem(&d.Message, d.Usage, d.Turn, d.Step, ev.Seq, ev.Time)
		if i := t.replaceStreamed(it); i >= 0 {
			t.replaceItem(i, it)
		} else {
			t.Items = append(t.Items, it)
		}
		t.noteSeq(ev.Seq, ev.Time)
		return true, d.Usage
	case "assistant/chunk":
		var d protocol.ChunkEventData
		if err := json.Unmarshal(ev.Data, &d); err != nil {
			return false, nil
		}
		t.attrTurn(d.Turn)
		if !t.beginStreaming(d.Turn, d.Step) {
			// A chunk for a different in-flight turn: attach to the live one.
			t.beginStreaming(d.Turn, d.Step)
		}
		t.applyChunk(d.Chunk, ev.Time)
		t.noteSeq(ev.Seq, ev.Time)
		return true, nil
	case "tool/call":
		var d protocol.ToolCallEventData
		if err := json.Unmarshal(ev.Data, &d); err != nil {
			return false, nil
		}
		t.attrTurn(d.Turn)
		tb := &ToolBlock{Id: d.CallId, Name: textutil.StripANSI(d.Name), Args: d.Arguments, ArgsFull: d.Arguments, CallSeq: ev.Seq, CallTime: ev.Time}
		t.ensureToolBlock(tb)
		t.noteSeq(ev.Seq, ev.Time)
		return true, nil
	case "tool/result":
		var d protocol.ToolResultEventData
		if err := json.Unmarshal(ev.Data, &d); err != nil {
			return false, nil
		}
		t.attrTurn(d.Turn)
		matched := false
		for _, b := range d.Message.Content {
			if b.Type != "tool-result" {
				continue
			}
			if tb, ok := t.openTools[b.ToolCallId]; ok {
				tb.ResultText = protocol.ToolResultText(b.Content)
				tb.IsError = b.IsError || d.Error != nil
				tb.Done = true
				tb.DoneTime = ev.Time
				matched = true
				t.bumpToolItems(tb)
				delete(t.openTools, b.ToolCallId)
			}
		}
		if !matched {
			text := protocol.ToolResultText(d.Message.Content)
			if len(text) > 200 {
				text = textutil.Truncate(text, 200, "…")
			}
			t.Items = append(t.Items, t.stamp(&Item{Kind: KindNote, Seq: ev.Seq, Time: ev.Time, Note: "tool result " + text}))
		}
		t.noteSeq(ev.Seq, ev.Time)
		return true, nil
	case "todo/write":
		var d protocol.TodoWriteEventData
		if err := json.Unmarshal(ev.Data, &d); err != nil {
			return false, nil
		}
		t.noteSeq(ev.Seq, ev.Time)
		return len(d.Todos) > 0, nil // side state; no transcript row
	case "session/title":
		t.noteSeq(ev.Seq, ev.Time)
		return false, nil // side state (title)
	case "command/run":
		var d protocol.CommandRunData
		if err := json.Unmarshal(ev.Data, &d); err != nil {
			return false, nil
		}
		t.Items = append(t.Items, t.stamp(&Item{Kind: KindCommand, Seq: ev.Seq, Time: ev.Time, CmdRun: &d}))
		return true, nil
	case "command/done":
		var d protocol.CommandDoneData
		if err := json.Unmarshal(ev.Data, &d); err != nil {
			return false, nil
		}
		for i := len(t.Items) - 1; i >= 0; i-- {
			if t.Items[i].Kind == KindCommand && t.Items[i].CmdRun != nil && t.Items[i].CmdDone == nil && t.Items[i].CmdRun.CommandId == d.CommandId {
				cp := *t.Items[i]
				cp.CmdDone = &d
				cp.Ver++
				t.replaceItem(i, &cp)
				t.noteSeq(ev.Seq, ev.Time)
				return true, nil
			}
		}
		// no matching run in view: render standalone
		t.Items = append(t.Items, t.stamp(&Item{Kind: KindCommand, Seq: ev.Seq, Time: ev.Time, CmdDone: &d}))
		t.noteSeq(ev.Seq, ev.Time)
		return true, nil
	case "compaction/start", "compaction/end", "compaction/summary", "compaction/prune":
		var d struct {
			Summary string `json:"summary,omitempty"`
		}
		_ = json.Unmarshal(ev.Data, &d)
		label := "compaction " + ev.Type[len("compaction/"):]
		if ev.Type == "compaction/end" {
			label = "context compacted"
		}
		t.Items = append(t.Items, t.stamp(&Item{Kind: KindNote, Seq: ev.Seq, Time: ev.Time, Note: label}))
		t.noteSeq(ev.Seq, ev.Time)
		return true, nil
	case "llm/retry", "llm/retry-started":
		var d protocol.LlmRetryData
		_ = json.Unmarshal(ev.Data, &d)
		note := "model retry"
		if d.Message != "" {
			note += ": " + d.Message
		}
		t.Items = append(t.Items, t.stamp(&Item{Kind: KindNote, Seq: ev.Seq, Time: ev.Time, Note: note}))
		t.noteSeq(ev.Seq, ev.Time)
		return true, nil
	case "plan/mode", "sandbox/mode", "permission/preset", "agent-preset/selected",
		"request/header", "request/context", "session/end-seed", "session/title-llm-request",
		"agent/inbox/spliced", "hook/invoked", "hook/result", "schedule/change",
		"subagent/descriptor", "team/member", "team/task", "team/message/queued", "team/message/delivered",
		"approval/asked", "approval/decided", "approval/policy",
		"feedback/record", "tool/code-dispatch", "tool/code-dispatch-start",
		"goal/change", "web/deepseek-search-llm-request":
		// Side-state or metadata events: no transcript row of their own.
		t.noteSeq(ev.Seq, ev.Time)
		return false, nil
	case "tool-workflow/run-start", "tool-workflow/run-end",
		"tool-workflow/agent-start", "tool-workflow/agent-end":
		var d struct {
			Name string `json:"name,omitempty"`
			Kind string `json:"kind,omitempty"`
		}
		_ = json.Unmarshal(ev.Data, &d)
		note := "workflow " + ev.Type[strings.LastIndex(ev.Type, "/")+1:]
		if d.Name != "" {
			note += " " + d.Name
		}
		t.Items = append(t.Items, t.stamp(&Item{Kind: KindNote, Seq: ev.Seq, Time: ev.Time, Note: note}))
		t.noteSeq(ev.Seq, ev.Time)
		return true, nil
	default:
		raw := strings.TrimSpace(string(ev.Data))
		if len(raw) > 160 {
			raw = textutil.Truncate(raw, 160, "…")
		}
		t.Items = append(t.Items, t.stamp(&Item{Kind: KindUnknown, Seq: ev.Seq, Time: ev.Time, Note: ev.Type, Raw: raw, Ignorable: ev.Ignorable}))
		t.noteSeq(ev.Seq, ev.Time)
		return true, nil
	}
}

// Prepend applies older history events (lower seqs) to a live fold. They
// are folded on a scratch transcript and merged in before the existing
// items; events at or above the current watermark are skipped.
func (t *Transcript) Prepend(events []*protocol.SessionEvent) {
	sorted := make([]*protocol.SessionEvent, len(events))
	copy(sorted, events)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Seq < sorted[j].Seq })
	scratch := NewTranscript()
	for _, ev := range sorted {
		if ev.Seq > t.maxSeq {
			continue
		}
		scratch.Apply(ev)
	}
	for _, tb := range scratch.openTools {
		if _, ok := t.openTools[tb.Id]; !ok {
			t.openTools[tb.Id] = tb
		}
	}
	if len(scratch.Items) == 0 {
		return
	}
	merged := make([]*Item, 0, len(scratch.Items)+len(t.Items))
	merged = append(merged, scratch.Items...)
	merged = append(merged, t.Items...)
	t.Items = merged
	if t.curItemIdx >= 0 {
		t.curItemIdx += len(scratch.Items)
	}
	// Streaming continues from the pre-existing state if any.
}

// contextInjection reports whether a user message is a model-facing context
// injection rather than a user utterance. Injections arrive as user/message
// events: context forms (the runtime-context snapshot, the skill catalog)
// carry source.form, and plugin notices (the user-approval policy-change
// messages, compaction summaries) carry source.kind "plugin". Real user
// sends are kind "user".
func contextInjection(m *protocol.Message) bool {
	return m.Source.Kind == "plugin" || m.Source.Form != ""
}

// bumpToolItems re-marks every item that displays the (mutated) shared tool
// block, so the render cache re-renders exactly those rows. Items hold
// private copies of their tool blocks (COW), so a match is by tool id and
// the new copy carries the just-updated block state.
func (t *Transcript) bumpToolItems(tb *ToolBlock) {
	if tb.Id == "" {
		return
	}
	for i := len(t.Items) - 1; i >= 0; i-- {
		it := t.Items[i]
		if it.Kind != KindAssistant {
			continue
		}
		hit := -1
		for j := range it.Blocks {
			if b := it.Blocks[j].Tool; b != nil && b.Id == tb.Id {
				hit = j
				break
			}
		}
		if hit < 0 {
			continue
		}
		cp := *it
		cp.Blocks = append([]ABlock(nil), it.Blocks...)
		tbc := *tb
		cp.Blocks[hit].Tool = &tbc
		cp.Ver++
		t.replaceItem(i, &cp)
	}
}

// replaceItem publishes a new item at index i through a fresh slice: the
// snapshot of Items taken earlier still owns its own slice header and the
// replaced *Item, neither of which we mutate any more (COW rule).
func (t *Transcript) replaceItem(i int, it *Item) {
	ns := make([]*Item, len(t.Items))
	copy(ns, t.Items)
	ns[i] = it
	t.Items = ns
}

// removeItem drops index i through a fresh slice (an in-place compaction
// would race with a snapshot that walks the old slice element by element).
func (t *Transcript) removeItem(i int) {
	ns := make([]*Item, 0, len(t.Items)-1)
	ns = append(ns, t.Items[:i]...)
	ns = append(ns, t.Items[i+1:]...)
	t.Items = ns
	switch {
	case t.curItemIdx == i:
		t.curItemIdx = -1
		t.curItem = nil
	case t.curItemIdx > i:
		t.curItemIdx--
	}
}

func (t *Transcript) noteSeq(seq int64, time int64) {
	if seq > t.maxSeq {
		t.maxSeq = seq
	}
}

// Streaming is the in-flight item the fold is assembling (nil when no
// step is mid-stream): its Usage is the live preview (a usage chunk),
// nil until one lands.
func (t *Transcript) Streaming() *Item {
	if t.streaming {
		return t.streamItem
	}
	return nil
}

// ---- streaming assembly ----------------------------------------------------

func (t *Transcript) beginStreaming(turn, step int) bool {
	if t.streaming && t.curTurn == turn && t.curStep == step {
		return true
	}
	if t.streaming {
		t.finalizeStreaming()
	}
	t.streaming = true
	t.curTurn = turn
	t.curStep = step
	t.curBlocks = map[int]*ABlock{}
	t.curOrder = nil
	t.curItem = nil
	t.streamUsage = nil
	t.streamSampled = false
	return true
}

func (t *Transcript) applyChunk(c protocol.StreamChunk, time int64) {
	switch c.Type {
	case "block-start":
		if _, ok := t.curBlocks[c.Index]; !ok {
			b := &ABlock{Kind: "text"}
			if c.Block != nil {
				switch c.Block.Type {
				case "reasoning":
					b.Kind = "reasoning"
				case "tool-call":
					b.Kind = "tool"
					b.Tool = &ToolBlock{Id: c.Block.Id, Name: textutil.StripANSI(c.Block.Name), ArgsFull: c.Block.Arguments, Args: c.Block.Arguments, CallTime: time}
				default:
					b.Kind = "text"
				}
			}
			t.curBlocks[c.Index] = b
			t.curOrder = append(t.curOrder, c.Index)
		}
	case "text-delta":
		b := t.ensureStreamBlock(c.Index, "text", time)
		b.Text += c.Text
	case "reasoning-delta":
		b := t.ensureStreamBlock(c.Index, "reasoning", time)
		b.Text += c.Text
	case "tool-call-delta":
		b := t.ensureStreamBlock(c.Index, "tool", time)
		if b.Tool == nil {
			b.Tool = &ToolBlock{CallTime: time}
		}
		if c.Id != "" {
			b.Tool.Id = c.Id
		}
		if c.Name != "" {
			b.Tool.Name = textutil.StripANSI(c.Name)
		}
		b.Tool.Args += c.ArgumentsDelta
		b.Tool.ArgsFull += c.ArgumentsDelta
	case "block-end":
		if c.Block != nil {
			if b, ok := t.curBlocks[c.Index]; ok {
				switch c.Block.Type {
				case "reasoning":
					b.Kind = "reasoning"
				case "tool-call":
					b.Kind = "tool"
					b.Tool = &ToolBlock{Id: c.Block.Id, Name: textutil.StripANSI(c.Block.Name), ArgsFull: c.Block.Arguments, Args: c.Block.Arguments, CallTime: time}
				}
			}
		}
	case "usage":
		// The step's usage lands ahead of the canonical message: count it
		// live (the canonical folds only the delta) and keep it on the
		// in-flight item for the status readouts.
		if c.Usage != nil {
			addUsageDelta(&t.TurnTokens, t.streamUsage, c.Usage)
			t.streamUsage = c.Usage
			if !t.streamSampled && c.Usage.OutputTokens > 0 {
				t.addGenSample(time, c.Usage.OutputTokens)
				t.streamSampled = true
			}
		}
	case "finish":
		if c.Reason != nil && (c.Reason.Kind == "max-tokens" || c.Reason.Kind == "error" || c.Reason.Kind == "aborted") {
			// The turn/end event carries the authoritative outcome; the
			// partial stays for the canonical message to finalize.
		}
	}
	t.refreshStreamingItem(time)
}

func (t *Transcript) ensureStreamBlock(index int, kind string, time int64) *ABlock {
	b, ok := t.curBlocks[index]
	if !ok {
		b = &ABlock{Kind: kind}
		t.curBlocks[index] = b
		t.curOrder = append(t.curOrder, index)
	} else if kind != b.Kind && b.Text == "" && b.Tool == nil {
		b.Kind = kind
	}
	if b.Kind == "tool" && b.Tool == nil {
		b.Tool = &ToolBlock{CallTime: time}
	}
	return b
}

func (t *Transcript) refreshStreamingItem(time int64) {
	sort.Slice(t.curOrder, func(i, j int) bool { return t.curOrder[i] < t.curOrder[j] })
	blocks := make([]ABlock, 0, len(t.curOrder))
	for _, idx := range t.curOrder {
		b := t.curBlocks[idx]
		cp := *b
		if b.Tool != nil {
			// COW: the item keeps its own private copy of the tool block;
			// deltas keep mutating the live curBlocks entry, and results
			// re-mark items by tool id (bumpToolItems).
			tbc := *b.Tool
			tbc.Args = truncateMiddle(tbc.ArgsFull, 120)
			cp.Tool = &tbc
		}
		blocks = append(blocks, cp)
	}
	it := &Item{Kind: KindAssistant, Time: time, Turn: t.curTurn, Step: t.curStep, Usage: t.streamUsage, Blocks: blocks}
	if t.curItem == nil {
		t.Items = append(t.Items, t.stamp(it))
		t.curItem = it
		t.curItemIdx = len(t.Items) - 1
		t.streamItem = it
		return
	}
	// COW: swap the published slot instead of overwriting the item the
	// snapshots may still be reading. Carry the render identity forward
	// (same gen, ver bumped) so the row re-renders via the cache.
	it.Gen = t.curItem.Gen
	it.Ver = t.curItem.Ver + 1
	t.replaceItem(t.curItemIdx, it)
	t.curItem = it
	t.streamItem = it
}

func (t *Transcript) finalizeStreaming() {
	if !t.streaming {
		return
	}
	// A message that never reached assistant/message (interrupt mid-stream)
	// keeps its partial, marked as such by the turn/end marker.
	t.streaming = false
	t.curItem = nil
	t.curItemIdx = -1
	t.curBlocks = map[int]*ABlock{}
	t.curOrder = nil
}

// replaceStreamed returns the index of the in-flight streaming item if the
// canonical message finalizes it (same turn and step), else -1. The partial
// is superseded by the canonical content and must not be displayed twice.
func (t *Transcript) replaceStreamed(it *Item) int {
	if t.streamItem == nil || it.Turn != t.curTurn || it.Step != t.curStep {
		t.streamItem = nil
		return -1
	}
	for i, cur := range t.Items {
		if cur == t.streamItem {
			t.streamItem = nil
			return i
		}
	}
	t.streamItem = nil
	return -1
}

func (t *Transcript) buildAssistantItem(m *protocol.Message, usage *protocol.TokenUsage, turn, step int, seq, time int64) *Item {
	it := t.stamp(&Item{Kind: KindAssistant, Seq: seq, Time: time, Usage: usage, Turn: turn, Step: step})
	// The live preview (a usage chunk) already folded this step's usage;
	// add only the remainder (zero when it carried the full usage).
	addUsageDelta(&t.TurnTokens, t.streamUsage, usage)
	if usage != nil && usage.OutputTokens > 0 && !t.streamSampled {
		t.addGenSample(time, usage.OutputTokens)
		t.streamSampled = true
	}
	t.streamUsage = nil
	t.streamSampled = false
	for _, b := range m.Content {
		switch b.Type {
		case "text":
			it.Blocks = append(it.Blocks, ABlock{Kind: "text", Text: b.Text})
		case "reasoning":
			it.Blocks = append(it.Blocks, ABlock{Kind: "reasoning", Text: b.Text})
		case "tool-call":
			tb := &ToolBlock{Id: b.Id, Name: textutil.StripANSI(b.Name), Args: truncateMiddle(b.Arguments, 120), ArgsFull: b.Arguments, CallSeq: seq, CallTime: time}
			t.ensureToolBlock(tb)
			ref := tb
			if tb.Id != "" {
				if old, ok := t.openTools[tb.Id]; ok {
					ref = old
				}
			}
			// COW: the item keeps a private copy of the tool block; the
			// live ref (openTools) keeps receiving result updates and
			// re-marks this row via bumpToolItems.
			tbc := *ref
			it.Blocks = append(it.Blocks, ABlock{Kind: "tool", Tool: &tbc})
		}
	}
	return it
}

func (t *Transcript) ensureToolBlock(tb *ToolBlock) {
	if tb.Id != "" {
		if old, ok := t.openTools[tb.Id]; ok {
			*tb = *old
			return
		}
		t.openTools[tb.Id] = tb
	}
}

// addUsageDelta folds (next - prev) into tt: the live preview is counted
// as it lands, and the canonical message folds only the remainder. Nil
// sides count as a zero usage.
func addUsageDelta(tt *TurnTokens, prev, next *protocol.TokenUsage) {
	var p, n protocol.TokenUsage
	if prev != nil {
		p = *prev
	}
	if next != nil {
		n = *next
	}
	tt.In += n.InputTokens - p.InputTokens + n.CacheReadTokens - p.CacheReadTokens
	tt.Out += n.OutputTokens - p.OutputTokens
	tt.Cache += n.CacheReadTokens - p.CacheReadTokens
}

// addGenSample records one assistant message's output tokens (the
// server's generation, as distinct from the input it was served) and
// prunes samples that have fallen out of the rolling window.
func (t *Transcript) addGenSample(at int64, out int) {
	t.Recent = append(t.Recent, GenSample{At: at, Out: out})
	cut := at - GenWindow.Milliseconds()
	i := 0
	for i < len(t.Recent) && t.Recent[i].At < cut {
		i++
	}
	t.Recent = t.Recent[i:]
}

// truncateMiddle keeps head + tail (shared rune-safe helper).
func truncateMiddle(s string, n int) string { return textutil.TruncateMiddle(s, n) }

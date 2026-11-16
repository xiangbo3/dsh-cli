package core

import (
	"testing"

	"dsh-cli/internal/protocol"
)

// TestFoldTotalsMirror pins the fold's session-lifetime usage mirror: it
// must equal the sum over every folded item's usage (the aggregation the
// status readout renders), absorb prepended history pages, and ignore
// re-delivered events.
func TestFoldTotalsMirror(t *testing.T) {
	tr := NewTranscript()
	am := func(seq int64, in, cache, out int) *protocol.SessionEvent {
		return ev(t, "assistant/message", seq, map[string]any{
			"turn": 1, "step": 1,
			"message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "x"}}},
			"usage":   map[string]any{"inputTokens": in, "cacheReadTokens": cache, "outputTokens": out},
		})
	}
	tr.Apply(am(5, 100, 50, 20))
	tr.Apply(am(6, 80, 0, 30))
	tr.Apply(am(6, 80, 0, 30)) // re-delivery: dropped by the seq watermark
	tr.Prepend([]*protocol.SessionEvent{am(1, 10, 5, 1)})
	var in, out int
	for _, it := range tr.Items {
		if it.Kind == KindAssistant && it.Usage != nil {
			in += it.Usage.InputTokens + it.Usage.CacheReadTokens
			out += it.Usage.OutputTokens
		}
	}
	if tr.Totals.In != in || tr.Totals.Out != out {
		t.Fatalf("mirror %d/%d != item sum %d/%d", tr.Totals.In, tr.Totals.Out, in, out)
	}
}

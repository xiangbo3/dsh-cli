// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"encoding/json"
	"testing"

	"dsh-cli/internal/app"
)

// TestCtxBarTracksProjectedTokens: pressureTokens is the last request
// and holds still mid-turn; projectedTokens is the host forecast that
// moves as the turn streams. The bar must read the forecast first (the
// web client order), fall back without one, and follow a compaction
// shrink downward.
func TestCtxBarTracksProjectedTokens(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	m := NewModel(app.New("http://127.0.0.1:3999"))
	m.st.SetActive("s1")
	pressure := func(seq int64, v string) {
		m.st.MuxProjection("s1", "contextPressure", seq, json.RawMessage(v))
	}

	pressure(1, `{"pressureTokens":1000,"projectedTokens":5000,"contextWindow":10000}`)
	if used, window, pct, known := m.ctxUsage(); !known || used != 5000 || window != 10000 || pct != 50 {
		t.Fatalf("with a forecast: used=%d window=%d pct=%d known=%v, want 5000/10000/50", used, window, pct, known)
	}

	pressure(2, `{"pressureTokens":2000,"contextWindow":10000}`)
	if used, _, pct, _ := m.ctxUsage(); used != 2000 || pct != 20 {
		t.Fatalf("without a forecast: used=%d pct=%d, want the pressure fallback 2000/20", used, pct)
	}

	pressure(3, `{"pressureTokens":8000,"projectedTokens":3000,"contextWindow":10000}`)
	if used, _, _, _ := m.ctxUsage(); used != 3000 {
		t.Fatalf("after a compaction the forecast must win downward: used=%d, want 3000", used)
	}
}

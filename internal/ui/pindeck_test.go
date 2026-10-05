// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"strings"
	"testing"
	"time"

	"dsh-cli/internal/protocol"
)

// TestPinDeck pins the input deck (frame + status strip) to the window
// bottom: toasts, the queue strip and the slash menu overlay the
// transcript and must not move the bar.
func TestPinDeck(t *testing.T) {
	m := scrollModel(t, 4)
	m.W, m.H = 80, 24

	deckTop := func() int {
		rows := strings.Split(stripCSI(m.View()), "\n")
		for i, ln := range rows {
			if strings.HasPrefix(strings.TrimSpace(ln), "╭") {
				return i
			}
		}
		t.Fatal("input frame not found")
		return -1
	}

	base := deckTop()

	m.toasts = append(m.toasts,
		toast{level: "ok", text: "saved", until: time.Now().Add(time.Minute)},
		toast{level: "warn", text: "slow", until: time.Now().Add(time.Minute)},
	)
	if got := deckTop(); got != base {
		t.Fatalf("toasts moved the deck: %d -> %d", base, got)
	}
	m.toasts = nil

	m.inp.val = []rune("/m")
	m.inp.cur = 2
	m.inp.refreshMenu(m.mergedCmds)
	if !m.inp.menuOpen {
		t.Fatal("slash menu did not open")
	}
	if got := deckTop(); got != base {
		t.Fatalf("slash menu moved the deck: %d -> %d", base, got)
	}
	m.inp.val = nil
	m.inp.cur = 0
	m.inp.menuOpen = false
	m.inp.menu = nil

	m.st.MuxQueue("s1", []protocol.QueuedInboxItem{{
		Id: "q1", Placement: "queued",
		Message: protocol.Message{Role: "user",
			Content: []protocol.ContentBlock{{Type: "text", Text: "later"}}},
	}})
	if got := deckTop(); got != base {
		t.Fatalf("queue strip moved the deck: %d -> %d", base, got)
	}
}

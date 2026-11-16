// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package core

import (
	"encoding/json"
	"testing"
	"time"

	"dsh-cli/internal/protocol"
)

func titleEv(seq int64) *protocol.SessionEvent {
	return &protocol.SessionEvent{Type: "session/title", Seq: seq,
		Time: time.Now().UnixMilli(), Data: json.RawMessage(`{"title":"t"}`)}
}

// TestSeqGapReportedOnce pins the gap signal in Event: a live stream
// that skips seqs returns the watermark the hole opened behind; every
// other shape (contiguous, first event, subscribed baseline) stays 0.
func TestSeqGapReportedOnce(t *testing.T) {
	s := NewStore("http://x")
	if _, _, g := s.Event("s1", titleEv(1)); g != 0 {
		t.Fatal("the first event is a baseline, not a gap")
	}
	if _, _, g := s.Event("s1", titleEv(2)); g != 0 {
		t.Fatal("contiguous stream must not report a gap")
	}
	if _, _, g := s.Event("s1", titleEv(5)); g != 2 {
		t.Fatalf("gap report = %d, want the watermark 2 behind the hole", g)
	}
	if _, _, g := s.Event("s1", titleEv(6)); g != 0 {
		t.Fatal("resumed stream must not report a gap")
	}

	s2 := NewStore("http://x")
	s2.Subscribed("s1", 100)
	if _, _, g := s2.Event("s1", titleEv(101)); g != 0 {
		t.Fatal("tail continuing the baseline must not report a gap")
	}
}

// TestMergeTailClosesGap: a history page reaching behind the hole
// merges the missed events in order (old ones prepend, new ones apply);
// a page that still skips hands the job back with a fresh gap seq.
func TestMergeTailClosesGap(t *testing.T) {
	s := NewStore("http://x")
	s.Event("s1", titleEv(1))
	s.Event("s1", titleEv(2))
	_, _, g := s.Event("s1", titleEv(5)) // 3 and 4 lost
	if g != 2 {
		t.Fatalf("gap seq = %d, want 2", g)
	}
	page := &protocol.HistoryResponse{}
	for _, seq := range []int64{2, 3, 4, 5} {
		page.Events = append(page.Events, protocol.HistoryEntry{Event: *titleEv(seq)})
	}
	if fresh := s.MergeTail("s1", page); fresh != 0 {
		t.Fatalf("merge of a covering page reported a fresh gap: %d", fresh)
	}
	// 6 applies cleanly on the repaired watermark.
	if _, _, g := s.Event("s1", titleEv(6)); g != 0 {
		t.Fatalf("post-merge stream reported a gap: %d", g)
	}

	s2 := NewStore("http://x")
	s2.Event("s1", titleEv(1))
	s2.Event("s1", titleEv(10))
	page2 := &protocol.HistoryResponse{}
	for _, seq := range []int64{10, 20} {
		page2.Events = append(page2.Events, protocol.HistoryEntry{Event: *titleEv(seq)})
	}
	if fresh := s2.MergeTail("s1", page2); fresh != 10 {
		t.Fatalf("skipping page must hand back its own gap (10), got %d", fresh)
	}
}

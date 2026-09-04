// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package oneoff

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"dsh-cli/internal/protocol"
)

// historyHost serves N session events (seqs 1..N) as 200-row pages,
// honoring beforeSeq==0 as the tail-page request.
func historyHost(t *testing.T, n int, calls *int32) *httptest.Server {
	t.Helper()
	events := make([]protocol.HistoryEntry, n)
	for i := 0; i < n; i++ {
		seq := int64(i + 1)
		events[i] = protocol.HistoryEntry{Event: protocol.SessionEvent{
			Type: "user/message", Seq: seq, Time: seq * 1000,
			Data: json.RawMessage(fmt.Sprintf(
				`{"message":{"id":"m%d","role":"user","content":[{"type":"text","text":"row %d"}]}}`, seq, seq)),
		}}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/host.describe", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent) // quiet the background baseline
	})
	mux.HandleFunc("/api/session.history", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(calls, 1)
		raw, _ := io.ReadAll(r.Body)
		var env protocol.Envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		var p struct {
			BeforeSeq   int64 `json:"beforeSeq"`
			MaxMessages int   `json:"maxMessages"`
		}
		_ = json.Unmarshal(env.Payload, &p)
		page := make([]protocol.HistoryEntry, 0, p.MaxMessages)
		for i := len(events) - 1; i >= 0; i-- {
			e := events[i].Event
			if p.BeforeSeq != 0 && e.Seq >= p.BeforeSeq {
				continue
			}
			if len(page) == p.MaxMessages {
				break
			}
			page = append(page, events[i])
		}
		for i, j := 0, len(page)-1; i < j; i, j = i+1, j-1 { // oldest-first
			page[i], page[j] = page[j], page[i]
		}
		more := p.BeforeSeq == 0 && n > p.MaxMessages
		if p.BeforeSeq != 0 {
			more = len(page) == p.MaxMessages
		}
		value, _ := json.Marshal(protocol.HistoryResponse{Events: page, HasMore: more})
		resp := protocol.Envelope{
			Type:   protocol.TypeServerResponse,
			RpcId:  env.RpcId,
			Result: &protocol.Result{Ok: true, Value: value},
		}
		body, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
	return httptest.NewServer(mux)
}

// TestHistoryDumpsEveryPage pins the R1 fix: the old loop condition
// (`before < 0`) stopped after the tail page plus one, so a 550-row session
// printed only 400 rows. Every page must be pulled until HasMore clears.
func TestHistoryDumpsEveryPage(t *testing.T) {
	const n = 550
	var calls int32
	srv := historyHost(t, n, &calls)
	defer srv.Close()

	o := Opts{URL: srv.URL, Timeout: 10 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := History(ctx, o, "s1"); err != nil {
		t.Fatalf("History: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got < 3 {
		t.Fatalf("session.history RPC calls = %d, want >= 3 for %d rows in 200-row pages", got, n)
	}
}

// TestHistorySinglePageIsOneCall guards the no-pagination path.
func TestHistorySinglePageIsOneCall(t *testing.T) {
	var calls int32
	srv := historyHost(t, 50, &calls)
	defer srv.Close()

	o := Opts{URL: srv.URL, Timeout: 10 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := History(ctx, o, "s1"); err != nil {
		t.Fatalf("History: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("session.history RPC calls = %d, want 1 for a single page", got)
	}
}

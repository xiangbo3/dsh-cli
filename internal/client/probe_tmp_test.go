// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package client

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestMuxProjectionProbe logs mux frames for a short window while a
// permission change happens, to verify session/projection pushes arrive.
// Dev probe: meaningful only against a live server, so skip otherwise.
func TestMuxProjectionProbe(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	base := "http://127.0.0.1:3080"
	pctx, pcancel := context.WithTimeout(ctx, 500*time.Millisecond)
	if _, err := New(base).Describe(pctx); err != nil {
		t.Skipf("no live server at %s: %v", base, err)
	}
	pcancel()
	s := NewStream(base)
	s.Start(ctx)

	sid := "session-ea6a4310-8aba-4508-b4f3-fef0ac4c6351"
	go func() {
		time.Sleep(2 * time.Second)
		body := fmt.Sprintf(`{"type":"client-request","rpcId":"probe-t2","method":"commands/execute","payload":{"args":{"agentId":%q,"line":"/permission read-only","images":[]}}}`, sid)
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/commands/execute", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Logf("change: %v", err)
			return
		}
		resp.Body.Close()
		t.Log("change issued")
	}()

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		case fr := <-s.Frames():
			raw := string(fr.Raw)
			if len(raw) > 900 {
				raw = raw[:900] + "..."
			}
			t.Logf("frame kind=%s raw=%s", fr.Kind, raw)
		case <-time.After(250 * time.Millisecond):
		}
	}
}

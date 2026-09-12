// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dsh-cli/internal/protocol"
)

// TestExchangeErrors pins the cause sentinels: a tokenless exchange wraps
// ErrNoToken, a rejected-token exchange wraps ErrTokenRejected (both under
// ErrAuthRequired), so a caller shows the matching recovery hint.
func TestExchangeErrors(t *testing.T) {
	bare := ConnFor("http://127.0.0.1:9")
	if err := bare.Exchange(context.Background(), http.DefaultClient); !errors.Is(err, ErrNoToken) || !errors.Is(err, ErrAuthRequired) {
		t.Fatalf("no-token exchange = %v, want ErrNoToken under ErrAuthRequired", err)
	}
	f := newFakeNewHost(t, "tok")
	c := New(f.URL)
	c.conn.SetToken("stale")
	if err := c.conn.Exchange(context.Background(), c.http); !errors.Is(err, ErrTokenRejected) || !errors.Is(err, ErrAuthRequired) {
		t.Fatalf("rejected exchange = %v, want ErrTokenRejected under ErrAuthRequired", err)
	}
}

// TestDialectMarker pins the 401 body sniff: a marker-bearing 401 index is
// the cookie-gated build; a markerless 401 is a legacy bearer-gated host
// (it keeps the legacy wire, whose bearer rides the DSH_TOKEN env).
func TestDialectMarker(t *testing.T) {
	marker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, "dsh web authentication required; reopen the URL printed by dsh web.")
	}))
	defer marker.Close()
	if d, _ := ConnFor(marker.URL).Detect(context.Background(), marker.Client()); d != DialectNew {
		t.Fatalf("marker 401 dialect = %v, want DialectNew", d)
	}
	bare401 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, "bearer token required")
	}))
	defer bare401.Close()
	if d, _ := ConnFor(bare401.URL).Detect(context.Background(), bare401.Client()); d != DialectOld {
		t.Fatalf("markerless 401 dialect = %v, want DialectOld (legacy bearer-gate)", d)
	}
}

// TestCallCrossWire404 pins the misclassification safety net: the index
// 401 lacks the gate marker (a body-stripping proxy), so the probe reads
// the legacy wire; the dot-method 404s, and the call retries once on the
// new wire, which lands.
func TestCallCrossWire404(t *testing.T) {
	f := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, "unauthorized") // no marker: the probe reads legacy
			return
		}
		if strings.Contains(r.URL.Path, ".") { // a legacy dot-method
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Path == "/api/session/list" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(protocol.Envelope{
				Type:   protocol.TypeServerResponse,
				Result: &protocol.Result{Ok: true, Value: mustJSON(protocol.SessionListResponse{Items: []protocol.SessionSummary{{SessionId: "s1"}}})},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer f.Close()
	c := New(f.URL)
	var resp protocol.SessionListResponse
	if err := c.Call(context.Background(), protocol.MSessionList, map[string]any{}, &resp); err != nil {
		t.Fatalf("Call (cross-wire 404 retry): %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].SessionId != "s1" {
		t.Fatalf("items = %+v, want the one s1 row", resp.Items)
	}
}

// TestStreamReexchange pins the stale-cookie recovery on the downlink: a
// held cookie the gate rejects 401s the upgrade, and the loop must
// re-exchange (the token is held) before the next dial lands — the WS gate
// rejects before any HTTP retry path could run.
func TestStreamReexchange(t *testing.T) {
	f := newFakeNewHost(t, "tok")
	conn := ConnFor(f.URL)
	conn.SetToken("tok")
	conn.SetCookie("dsh-auth-stale=stale.sig") // the gate rejects it
	s := NewStream(f.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s.Start(ctx)
	deadline := time.After(15 * time.Second)
	up := false
	for !up {
		select {
		case v := <-s.Status():
			up = v
		case <-deadline:
			t.Fatal("downlink never came up after the rejected-cookie dial")
		}
	}
	ck := conn.Cookie()
	if ck == "dsh-auth-stale=stale.sig" || !strings.HasPrefix(ck, "dsh-auth-") {
		t.Fatalf("cookie = %q, want the re-exchanged mint", ck)
	}
}

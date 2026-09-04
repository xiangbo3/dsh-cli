// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRespondReceipt pins the answer channel's failure surface: the
// carrier acks a rejected payload with HTTP 200 + {accepted:false,
// reason}, which Respond must surface as an error (a silent success
// closes the modal while the host's question stays pending), while a
// bare {accepted:true} and a non-200 both behave as before.
func TestRespondReceipt(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantErr string // "" = no error; otherwise a substring of the error
	}{
		{"accepted", 200, `{"accepted":true}`, ""},
		{"rejected with reason", 200, `{"accepted":false,"reason":"bad-response"}`, "bad-response"},
		{"rejected without reason", 200, `{"accepted":false}`, "not accepted"},
		{"non-200", 400, `boom`, "HTTP 400"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/respond" {
					t.Errorf("path = %s, want /api/respond", r.URL.Path)
				}
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			c := &Client{base: srv.URL, http: srv.Client()}
			err := c.Respond(context.Background(), "rpc-1", map[string]any{"sessionId": "s1"})
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want a message containing %q", err, tc.wantErr)
			}
		})
	}
}

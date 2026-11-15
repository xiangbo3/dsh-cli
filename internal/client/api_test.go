// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package client

import (
	"context"
	"strings"
	"testing"
)

// TestSelectModelReplyShapes pins what a model switch accepts as the host's
// answer: the bare selection the upgraded host hands over after the wire
// shim unwraps its envelope, the wrapped {"selected":…} form a legacy host
// answers with, and the empty object a host returns when it changed nothing
// — the last must stay "nothing to report", so the UI keeps the model it
// asked for instead of painting an empty selection over it.
func TestSelectModelReplyShapes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		reply any
		want  string
	}{
		{"bare", map[string]any{"provider": "deepseek", "model": "chat-v4"}, "chat-v4"},
		{"wrapped", map[string]any{"selected": map[string]any{"provider": "deepseek", "model": "chat-v4"}}, "chat-v4"},
		{"effort", map[string]any{"provider": "deepseek", "model": "chat-v4", "reasoningEffort": "high"}, "chat-v4"},
		{"empty", map[string]any{}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeNewHost(t, "tok")
			f.selectModel = tc.reply
			c := New(f.URL)
			c.conn.SetToken("tok")
			if err := c.conn.Exchange(context.Background(), c.http); err != nil {
				t.Fatalf("exchange: %v", err)
			}
			sel, err := c.SelectModel(context.Background(), "s1", "deepseek", "chat-v4", "")
			if err != nil {
				t.Fatalf("SelectModel: %v", err)
			}
			if tc.want == "" {
				if sel != nil {
					t.Fatalf("sel = %+v, want nil when the host names no model", sel)
				}
				return
			}
			if sel == nil || sel.Provider != "deepseek" || sel.Model != tc.want {
				t.Fatalf("sel = %+v, want deepseek/%s", sel, tc.want)
			}
		})
	}
}

// TestCommandExecuteWire pins the commands/execute args shape the host
// typert gateway validates: the attachment slot is submittedAttachments
// (an empty list for a plain invocation), not the legacy images alias.
func TestCommandExecuteWire(t *testing.T) {
	f := newFakeNewHost(t, "tok")
	c := New(f.URL)
	c.conn.SetToken("tok")
	if err := c.conn.Exchange(context.Background(), c.http); err != nil {
		t.Fatalf("exchange: %v", err)
	}
	res, matched, err := c.CommandExecute(context.Background(), "s1", "/permission workspace-write")
	if err != nil {
		t.Fatalf("CommandExecute: %v", err)
	}
	if !matched {
		t.Fatalf("matched = false, want the command resolved")
	}
	if res.Result.Kind != "success" {
		t.Fatalf("result = %+v, want kind success", res.Result)
	}
	f.mu.Lock()
	log := strings.Join(f.argLog, " | ")
	f.mu.Unlock()
	for _, want := range []string{
		"commands/execute",
		`"agentId":"s1"`,
		`"line":"/permission workspace-write"`,
		`"submittedAttachments":[]`,
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("host saw %q, want %s", log, want)
		}
	}
	if strings.Contains(log, `"images"`) {
		t.Fatalf("host saw %q, want no images alias", log)
	}
}

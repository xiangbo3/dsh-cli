// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package client

import (
	"context"
	"strings"
	"testing"
)

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

// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package core

import (
	"testing"
)

func hasEcho(s *Snapshot, rpcId string) bool {
	for _, it := range s.Items {
		if it.EchoRpcId == rpcId {
			return true
		}
	}
	return false
}

// TestPromptFailureClearsEcho pins the failed-send path: the optimistic
// echo lives while the send is in flight, and App.Prompt's error path
// (PromptCommand with a nil response) must take it back out — a prompt
// that never left must not linger in the transcript as sent.
func TestPromptFailureClearsEcho(t *testing.T) {
	s := NewStore("http://x")
	s.PromptSent("s1", "hello world", "rpc-1")
	if !hasEcho(s.Get("s1"), "rpc-1") {
		t.Fatal("echo missing right after submit")
	}
	s.PromptCommand("s1", "rpc-1", nil) // the send-failed settle
	if hasEcho(s.Get("s1"), "rpc-1") {
		t.Fatal("echo survived a failed send")
	}
}

// An unrelated rpcId settle must not sweep other in-flight echoes.
func TestPromptFailureClearsOwnEchoOnly(t *testing.T) {
	s := NewStore("http://x")
	s.PromptSent("s1", "first", "rpc-1")
	s.PromptSent("s1", "second", "rpc-2")
	s.PromptCommand("s1", "rpc-1", nil)
	if hasEcho(s.Get("s1"), "rpc-1") || !hasEcho(s.Get("s1"), "rpc-2") {
		t.Fatal("settle must clear exactly its own echo")
	}
}

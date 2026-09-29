// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package functional

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dsh-cli/internal/protocol"
	"dsh-cli/internal/version"
)

// binPath is the binary under test, resolved once in TestMain.
var binPath string

func TestMain(m *testing.M) {
	binPath = resolveBinary()
	os.Exit(m.Run())
}

// resolveBinary picks the binary under test: $FUNC_BIN (make test-func
// passes the fresh make build), a fresh ./dsh-cli in the module root, or a
// build of the current source. The build inherits GOCACHE/GOTMPDIR from
// the make targets; a bare go test needs them set, as every other build
// here does.
func resolveBinary() string {
	if p := os.Getenv("FUNC_BIN"); p != "" {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	root, _ := filepath.Abs("..")
	if cand := filepath.Join(root, "dsh-cli"); binaryFresh(cand, root) {
		return cand
	}
	tmp, err := os.MkdirTemp("", "dsh-cli-func-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "functional: %v\n", err)
		os.Exit(1)
	}
	out := filepath.Join(tmp, "dsh-cli")
	cmd := exec.Command("go", "build", "-buildvcs=false", "-o", out, ".")
	cmd.Dir = root
	if b, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "functional: go build: %v\n%s\n", err, b)
		os.Exit(1)
	}
	return out
}

// binaryFresh reports whether bin is at least as new as every source file
// in the module (a stale root binary is rebuilt, not trusted).
func binaryFresh(bin, root string) bool {
	bfi, err := os.Stat(bin)
	if err != nil {
		return false
	}
	fresh := true
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if p != root {
				switch name {
				case ".git", ".gocache", ".gotmp", "releases", "node_modules":
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") && name != "go.mod" && name != "go.sum" {
			return nil
		}
		if fi, err := d.Info(); err == nil && fi.ModTime().After(bfi.ModTime()) {
			fresh = false
		}
		return nil
	})
	return fresh
}

type runResult struct {
	code int
	home string
	out  string
	err  string
}

// run executes the binary hermetically: HOME and DSH_CLI_HOME point at a
// fresh temp dir (config, locales, usage, webhost log all land there),
// every DSH_* override is stripped, and the cwd is that same dir (no stray
// ./locales beside the checkout). in pipes stdin ("" = closed).
func run(t *testing.T, in string, args ...string) runResult {
	t.Helper()
	home := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binPath, args...)
	cmd.Dir = home
	cmd.Env = hermeticEnv(home)
	if in != "" {
		cmd.Stdin = strings.NewReader(in)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	res := runResult{home: home}
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			res.code = ee.ExitCode()
		} else {
			t.Fatalf("run %v: %v (stderr: %s)", args, err, errb.String())
		}
	}
	res.out, res.err = out.String(), errb.String()
	return res
}

// hermeticEnv drops HOME and every DSH_* override, then isolates the data
// dirs (HOME for the i18n locale storage, DSH_CLI_HOME for config/usage).
func hermeticEnv(home string) []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if k == "HOME" || strings.HasPrefix(k, "DSH_") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "HOME="+home, "DSH_CLI_HOME="+home)
}

func wantExit(t *testing.T, res runResult, code int) {
	t.Helper()
	if res.code != code {
		t.Fatalf("exit %d, want %d\nstdout: %s\nstderr: %s", res.code, code, res.out, res.err)
	}
}

func wantExact(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func wantContains(t *testing.T, s string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			t.Fatalf("output missing %q:\n%s", sub, s)
		}
	}
}

// linesContaining picks the lines of s that contain sub, in order.
func linesContaining(s, sub string) []string {
	var out []string
	for _, ln := range strings.Split(s, "\n") {
		if strings.Contains(ln, sub) {
			out = append(out, ln)
		}
	}
	return out
}

// ---- event builders (the canned durable log) -------------------------------

func ev(seq int64, at int64, typ string, data any) protocol.SessionEvent {
	return protocol.SessionEvent{Type: typ, Seq: seq, Time: at, Data: mustJSON(data)}
}

func textBlock(s string) protocol.ContentBlock {
	return protocol.ContentBlock{Type: "text", Text: s}
}

func message(id, role string, content []protocol.ContentBlock) protocol.Message {
	return protocol.Message{Id: id, Role: role, Content: content}
}

func assistantMsg(id string, turn, step int, content []protocol.ContentBlock) protocol.AssistantMessageEventData {
	return protocol.AssistantMessageEventData{
		Turn: turn, Step: step,
		Message: message(id, "assistant", content),
	}
}

// ---- tests ------------------------------------------------------------------

func TestVersion(t *testing.T) {
	res := run(t, "", "--version")
	wantExit(t, res, 0)
	wantExact(t, res.out, fmt.Sprintf("dsh-cli %s\n", version.Version))
}

func TestStatus(t *testing.T) {
	fh := newFakeHost(t)
	now := time.Now().UnixMilli()
	fh.seedSession("s1", true, false, "ptc", "/tmp/projA", now)
	fh.seedSession("s2", false, false, "standard", "/tmp/projB", now)
	fh.seedWorkspace("ws1", "Alpha", "/srv/alpha", "s1", "s2")
	fh.seedWorkspace("ws2", "Beta", "/srv/beta")
	res := run(t, "", "--no-autostart", "--url", fh.URL, "status")
	wantExit(t, res, 0)
	wantContains(t, res.out,
		version.Version,
		"fake-host-1 (host)",
		"/tmp/fakehost",
		"deepseek / fake-model",
		"sessions 2 (1 running, attached 1)",
		"workspaces 2 (registered)",
	)
}

func TestLs(t *testing.T) {
	fh := newFakeHost(t)
	now := time.Now().UnixMilli()
	fh.seedSession("s1", true, false, "ptc", "/tmp/projA", now)
	fh.seedSession("s2", false, false, "standard", "/tmp/projB", now)
	fh.seedSession("s3", false, true, "", "/tmp/projC", now)
	res := run(t, "", "--no-autostart", "--url", fh.URL, "ls")
	wantExit(t, res, 0)
	if running := linesContaining(res.out, "running"); len(running) != 1 {
		t.Fatalf("want one running row, got %d:\n%s", len(running), res.out)
	} else {
		wantContains(t, running[0], "ptc", "projA", "(untitled)")
	}
	if idle := linesContaining(res.out, "idle"); len(idle) != 1 {
		t.Fatalf("want one idle row, got %d:\n%s", len(idle), res.out)
	} else {
		wantContains(t, idle[0], "standard", "projB")
	}
	if blank := linesContaining(res.out, "blank"); len(blank) != 1 {
		t.Fatalf("want one blank row, got %d:\n%s", len(blank), res.out)
	} else {
		wantContains(t, blank[0], "projC")
	}
}

func TestLsEmpty(t *testing.T) {
	fh := newFakeHost(t)
	res := run(t, "", "--no-autostart", "--url", fh.URL, "ls")
	wantExit(t, res, 0)
	wantExact(t, res.out, "(no sessions)\n")
}

func TestNew(t *testing.T) {
	fh := newFakeHost(t)
	fh.seedWorkspace("ws1", "Alpha", "/srv/alpha")
	res := run(t, "", "--no-autostart", "--url", fh.URL, "new")
	wantExit(t, res, 0)
	wantExact(t, res.out, "f1\n")
	res = run(t, "", "--no-autostart", "--url", fh.URL, "new", "--cwd", "/srv/alpha")
	wantExit(t, res, 0)
	wantExact(t, res.out, "f2\n")
	res = run(t, "", "--no-autostart", "--url", fh.URL, "new", "--preset", "ptc mode")
	wantExit(t, res, 0)
	wantExact(t, res.out, "f3\n")
	creates := fh.createCalls()
	if len(creates) != 3 {
		t.Fatalf("want 3 create calls, got %d", len(creates))
	}
	if c := creates[0]; c.WorkspaceId != "" || c.Cwd != "" || c.AgentPreset != "" {
		t.Fatalf("bare new: unexpected payload %+v", c)
	}
	// a cwd over a registered workspace is sent as the workspace id
	if c := creates[1]; c.WorkspaceId != "ws1" || c.Cwd != "" {
		t.Fatalf("workspace cwd: unexpected payload %+v", c)
	}
	// "X mode" names resolve to the preset id before the wire
	if c := creates[2]; c.AgentPreset != "ptc" {
		t.Fatalf("preset: unexpected payload %+v", c)
	}
}

func TestHistory(t *testing.T) {
	fh := newFakeHost(t)
	now := time.Now().UnixMilli()
	fh.seedSession("s1", false, false, "standard", "/tmp/projA", now)
	fh.seedHistory("s1",
		ev(1, now, "user/message", message("u1", "user", []protocol.ContentBlock{textBlock("hello")})),
		ev(2, now+10, "assistant/message", assistantMsg("a1", 1, 1, []protocol.ContentBlock{
			textBlock("Let me check."),
			{Type: "tool-call", Id: "c1", Name: "run", Arguments: "{\"cmd\":\"ls\"}"},
		})),
		ev(3, now+20, "tool/result", protocol.ToolResultEventData{
			Turn: 1, Step: 1,
			Message: message("t1", "tool", []protocol.ContentBlock{
				{Type: "tool-result", ToolCallId: "c1", Content: []protocol.ContentBlock{textBlock("a b c")}},
			}),
		}),
		ev(4, now+30, "assistant/message", assistantMsg("a2", 1, 2,
			[]protocol.ContentBlock{textBlock("All files are there.")})),
		ev(5, now+40, "turn/end", map[string]any{"turn": 1, "reason": map[string]any{"kind": "completed"}}),
	)
	res := run(t, "", "--no-autostart", "--url", fh.URL, "history", "s1")
	wantExit(t, res, 0)
	wantContains(t, res.out,
		"You: hello",
		"Agent: Let me check.",
		"▸ run {\"cmd\":\"ls\"} ✓",
		"Agent: All files are there.",
		"turn completed",
	)
}

func TestHistoryUnknown(t *testing.T) {
	fh := newFakeHost(t)
	res := run(t, "", "--no-autostart", "--url", fh.URL, "history", "nothere")
	wantExit(t, res, 1)
	wantContains(t, res.err, "session-not-found")
}

func TestModels(t *testing.T) {
	fh := newFakeHost(t)
	now := time.Now().UnixMilli()
	fh.seedSession("s1", false, false, "standard", "/tmp/projA", now)
	res := run(t, "", "--no-autostart", "--url", fh.URL, "models", "s1")
	wantExit(t, res, 0)
	wantContains(t, res.out,
		"current: deepseek / fake-model",
		"DeepSeek (deepseek)",
		"  fake-model  Fake Model",
		"  fake-fast",
		"  gpt-5",
		"! Anthropic: lookup failed",
	)
	// no session named: the most recent non-blank roster row is resolved
	res = run(t, "", "--no-autostart", "--url", fh.URL, "models")
	wantExit(t, res, 0)
	wantContains(t, res.out, "current: deepseek / fake-model", "DeepSeek (deepseek)")
}

func TestWorkspaces(t *testing.T) {
	fh := newFakeHost(t)
	fh.seedWorkspace("ws1", "Alpha", "/srv/alpha", "s1", "s2")
	fh.seedWorkspace("ws2", "Beta", "/srv/beta")
	fh.mu.Lock()
	fh.archived = []string{"old1", "old2"}
	fh.mu.Unlock()
	res := run(t, "", "--no-autostart", "--url", fh.URL, "workspaces")
	wantExit(t, res, 0)
	wantContains(t, res.out,
		"Alpha", "/srv/alpha", "2 sessions",
		"Beta", "/srv/beta", "0 sessions",
		"2 sessions registry-wide",
	)
}

func TestOneShotNew(t *testing.T) {
	fh := newFakeHost(t)
	fh.turnOn("hello from fake host")
	res := run(t, "", "--no-autostart", "--url", fh.URL,
		"--new", "--timeout", "20s", "Reply with exactly: OK.")
	wantExit(t, res, 0)
	wantExact(t, res.out, "hello from fake host")
	calls := fh.promptCalls()
	if len(calls) != 1 {
		t.Fatalf("want 1 prompt, got %d", len(calls))
	}
	if calls[0].Text != "Reply with exactly: OK." || calls[0].Mode != "queue" {
		t.Fatalf("unexpected prompt %+v", calls[0])
	}
	if !strings.HasPrefix(calls[0].SessionId, "f") {
		t.Fatalf("--new prompt must land on a created session, got %q", calls[0].SessionId)
	}
}

func TestOneShotPipe(t *testing.T) {
	fh := newFakeHost(t)
	fh.turnOn("piped answer")
	res := run(t, "piped prompt\n", "--no-autostart", "--url", fh.URL,
		"run", "--new", "--timeout", "20s")
	wantExit(t, res, 0)
	wantExact(t, res.out, "piped answer")
	calls := fh.promptCalls()
	if len(calls) != 1 || calls[0].Text != "piped prompt" {
		t.Fatalf("unexpected prompts %+v", calls)
	}
}

func TestOneShotExistingSession(t *testing.T) {
	fh := newFakeHost(t)
	fh.seedSession("s1", false, false, "standard", "/tmp/projA", time.Now().UnixMilli())
	fh.turnOn("answer for s1")
	res := run(t, "", "--no-autostart", "--url", fh.URL,
		"--session", "s1", "--timeout", "20s", "again?")
	wantExit(t, res, 0)
	wantExact(t, res.out, "answer for s1")
	calls := fh.promptCalls()
	if len(calls) != 1 || calls[0].SessionId != "s1" || calls[0].Text != "again?" {
		t.Fatalf("unexpected prompts %+v", calls)
	}
}

func TestOneShotFlagsAfterPrompt(t *testing.T) {
	fh := newFakeHost(t)
	fh.turnOn("reordered")
	res := run(t, "", "--no-autostart", "--url", fh.URL,
		"--new", "trailing flag prompt", "--timeout", "20s")
	wantExit(t, res, 0)
	wantExact(t, res.out, "reordered")
	calls := fh.promptCalls()
	if len(calls) != 1 || calls[0].Text != "trailing flag prompt" {
		t.Fatalf("flags after the prompt must not leak into it: %+v", calls)
	}
}

func TestRunCommand(t *testing.T) {
	fh := newFakeHost(t)
	fh.turnOn("run cmd answer")
	res := run(t, "", "--no-autostart", "--url", fh.URL,
		"run", "explicit run prompt")
	wantExit(t, res, 0)
	wantExact(t, res.out, "run cmd answer")
	calls := fh.promptCalls()
	if len(calls) != 1 || calls[0].Text != "explicit run prompt" {
		t.Fatalf("unexpected prompts %+v", calls)
	}
}

func TestOneShotTimeout(t *testing.T) {
	fh := newFakeHost(t) // turn not armed: the turn never ends
	res := run(t, "", "--no-autostart", "--url", fh.URL,
		"--new", "--timeout", "3s", "stall")
	wantExit(t, res, 3)
	wantContains(t, res.err, "timed out after 3s")
}

func TestBadURL(t *testing.T) {
	res := run(t, "", "--no-autostart", "--url", "http://", "status")
	wantExit(t, res, 1)
	wantContains(t, res.err, "bad server URL")
}

func TestHostDown(t *testing.T) {
	res := run(t, "", "--no-autostart", "--url", "http://127.0.0.1:1", "status")
	wantExit(t, res, 1)
	wantContains(t, res.err, "dsh is not running")
}

// TestLocaleSeeded pins the i18n boot contract: every run seeds the stored
// locale catalogs under the data dir, stamped with the generating version.
func TestLocaleSeeded(t *testing.T) {
	res := run(t, "", "--no-autostart", "--url", "http://127.0.0.1:1", "status")
	_ = res // the host is down; the locale seed lands before the probe
	for _, name := range []string{"en.json", "zh.json"} {
		b, err := os.ReadFile(filepath.Join(res.home, ".dsh-cli", "locales", name))
		if err != nil {
			t.Fatalf("locale %s: %v", name, err)
		}
		var doc map[string]string
		if err := json.Unmarshal(b, &doc); err != nil {
			t.Fatalf("locale %s: %v", name, err)
		}
		if doc["_version"] != version.Version {
			t.Fatalf("locale %s stamped %q, want %q", name, doc["_version"], version.Version)
		}
	}
}

// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package webhost

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"dsh-cli/internal/app"
	"dsh-cli/internal/config"
)

// gatedBootFake is a gated-build boot stand-in: it binds at once and
// answers the bare index with 404 for the boot window (the web server
// listens before its routes are claimed), then the gate 401 (the mint
// works only with the fresh token). The ?token= line lands after the
// window + late — past the launch's ready grace.
func gatedBootFake(t *testing.T, port int, window, late time.Duration, tok string) string {
	t.Helper()
	requirePython(t)
	code := fmt.Sprintf(
		"import sys, http.server, socketserver, threading, time\n"+
			"a = sys.argv\n"+
			"port = int(a[a.index(\"--port\") + 1])\n"+
			"window = %f\n"+
			"tok = %q\n"+
			"body = b\"dsh web authentication required\"\n"+
			"t0 = [time.time()]\n"+
			"class H(http.server.BaseHTTPRequestHandler):\n"+
			"    def _send(self, code, extra=None):\n"+
			"        self.send_response(code)\n"+
			"        for k, v in (extra or {}).items():\n"+
			"            self.send_header(k, v)\n"+
			"        self.send_header(\"Content-Length\", str(len(body)))\n"+
			"        self.send_header(\"Connection\", \"close\")\n"+
			"        self.end_headers()\n"+
			"        self.wfile.write(body)\n"+
			"    def do_GET(self):\n"+
			"        i = self.path.find(\"?token=\")\n"+
			"        if i >= 0 and self.path[i+7:].split(\"&\")[0] == tok:\n"+
			"            self._send(303, {\"Set-Cookie\": \"dsh-auth-fake=fake.sig; Path=/; HttpOnly; SameSite=Strict\", \"Location\": \"/\"})\n"+
			"            return\n"+
			"        if time.time() - t0[0] > window:\n"+
			"            self._send(401)\n"+
			"        else:\n"+
			"            self._send(404)\n"+
			"    def log_message(self, *a):\n"+
			"        pass\n"+
			"socketserver.ThreadingTCPServer.allow_reuse_address = True\n"+
			"srv = socketserver.ThreadingTCPServer((\"127.0.0.1\", port), H)\n"+
			"threading.Thread(target=srv.serve_forever, daemon=True).start()\n"+
			"time.sleep(window + %f)\n"+
			"print(\"dsh web: http://127.0.0.1:%%d/?token=%%s\" %% (port, tok), flush=True)\n"+
			"time.sleep(300)\n",
		window.Seconds(), tok, late.Seconds())
	script := "#!/bin/sh\nexec python3 -c '" + code + "' \"$@\"\n"
	p := filepath.Join(t.TempDir(), "dsh-gatedboot.sh")
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestConnectGatedBootWindow pins the launch's verdict on the 404 boot
// window: a gated host whose index 404s (routes unclaimed) at the
// grace probe must still yield its late token — settling token-less
// leaves the caller holding the stale credentials the new gate rejects
// (connecting until a manual --token re-run).
func TestConnectGatedBootWindow(t *testing.T) {
	port := freePort(t)
	// window (3s) outlasts the ready grace (2s): the grace probe lands
	// inside the 404 window. The token line is at window+late (4.5s).
	t.Setenv("DSH_BIN", gatedBootFake(t, port, 3*time.Second, 1500*time.Millisecond, "WIN-TOK-789"))
	t.Setenv("DSH_CLI_HOME", t.TempDir())

	start := time.Now()
	h, tok, err := Connect(downBase(port), "STALE-TOK", true)
	if err != nil {
		t.Fatal(err)
	}
	if tok != "WIN-TOK-789" {
		t.Fatalf("token = %q, want the late token (the 404 window must not settle token-less)", tok)
	}
	if h.Pid() == 0 {
		t.Fatal("no pid")
	}
	if d := time.Since(start); d > 30*time.Second {
		t.Fatalf("connect took %s", d)
	}
	StopAll()
}

// buildBootfake compiles the full-protocol boot fake (the child the
// launch spawns in the TUI boot race test).
func buildBootfake(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go unavailable for the bootfake build")
	}
	bin := filepath.Join(t.TempDir(), "bootfake")
	cmd := exec.Command("go", "build", "-o", bin, "./testdata/bootfake")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build bootfake: %v\n%s", err, out)
	}
	return bin
}

// TestTUIBootFreshHost pins the full autostart boot race (the stuck
// "connecting" report): the TUI boots with the previous lifetime's
// credentials while the child dsh web comes up (down → bind → 404
// window → gate → the late token line). The app must converge on the
// fresh host — the store gets the host facts and the downlink reports
// up. Before the dialect fix, a probe inside the 404 window pinned the
// legacy wire: the fresh token never got exchanged, the legacy downlink
// dials endpoints the new host does not serve, and the boot stayed
// "connecting" until a manual restart (which hits the settled host and
// pins the right generation).
func TestTUIBootFreshHost(t *testing.T) {
	port := freePort(t)
	t.Setenv("DSH_BIN", buildBootfake(t))
	t.Setenv("DSH_BOOTFAKE_TOKEN", "BOOT-TOK-123")
	t.Setenv("DSH_CLI_HOME", t.TempDir())
	base := downBase(port)

	// The previous lifetime's credentials (the stale pair the new
	// gate rejects).
	_ = config.Save(config.Config{
		Token:     "STALE-TOK",
		Cookie:    "dsh-auth-old=old.sig",
		CookieFor: base,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The TUI flow: the launch runs alongside the boot; its outcome
	// installs the fresh token when it lands.
	resCh := make(chan Result, 1)
	go func() {
		h, tk, err := Connect(base, "", true)
		resCh <- Result{Host: h, Token: tk, Err: err}
	}()
	a := app.NewWith(base, "")
	st := a.Start(ctx)
	go func() {
		r := <-resCh
		if r.Err != nil || r.Host == nil {
			return
		}
		if r.Token != "" {
			a.Client().Conn().SetToken(r.Token)
		}
	}()

	// The token line lands ~5.5s in; convergence needs the fresh token
	// plus one exchange (a few seconds more). A host that never
	// converges would otherwise spin until the deadline.
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		if st.Host() != nil && st.Connected() {
			StopAll()
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	StopAll()
	t.Fatalf("boot did not converge (host=%v connected=%v): the 404 boot window pinned the legacy wire", st.Host() != nil, st.Connected())
}

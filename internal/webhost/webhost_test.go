// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package webhost

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"dsh-cli/internal/protocol"
)

// python3 is the fakes' runtime (a stable long-lived listener, unlike a
// one-connection-per-process nc loop); the tests skip without it.
func requirePython(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 unavailable")
	}
}

// fakeDsh is a python stand-in for the dsh launcher: it prints the boot
// token line (to stdout, which the launch redirects to the log file),
// then answers every request to the port with a 200.
func fakeDsh(t *testing.T, port int) string {
	t.Helper()
	return fakeDshPrint(t, port,
		"print(\"dsh web: http://127.0.0.1:%d/?token=FAKE-TOK-123\" % port, flush=True)\n")
}

// noTokenFake is the old-build stand-in (a pre-gate dsh web): it prints a
// boot line without a ?token= parameter, then serves.
func noTokenFake(t *testing.T, port int) string {
	t.Helper()
	return fakeDshPrint(t, port,
		"print(\"dsh web: http://127.0.0.1:%d\" % port, flush=True)\n")
}

func fakeDshPrint(t *testing.T, port int, bootLine string) string {
	t.Helper()
	requirePython(t)
	code := "import sys, http.server, socketserver\n" +
		"a = sys.argv\n" +
		"port = int(a[a.index(\"--port\") + 1])\n" +
		bootLine +
		"class H(http.server.BaseHTTPRequestHandler):\n" +
		"    def _ok(self):\n" +
		"        self.send_response(200)\n" +
		"        self.send_header(\"Content-Length\", \"2\")\n" +
		"        self.send_header(\"Connection\", \"close\")\n" +
		"        self.end_headers()\n" +
		"        self.wfile.write(b\"ok\")\n" +
		"    do_GET = do_POST = _ok\n" +
		"    def log_message(self, *a):\n" +
		"        pass\n" +
		"socketserver.ThreadingTCPServer.allow_reuse_address = True\n" +
		"socketserver.ThreadingTCPServer((\"127.0.0.1\", port), H).serve_forever()\n"
	script := "#!/bin/sh\nexec python3 -c '" + code + "' \"$@\"\n"
	p := filepath.Join(t.TempDir(), "dsh-fake.sh")
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func downBase(port int) string {
	return "http://127.0.0.1:" + strconv.Itoa(port)
}

// legacyFake is a live un-gated host: a 200 index and a host.describe
// answer — the live + valid pre-check path.
func legacyFake(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" && r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, "ok")
			return
		}
		if r.URL.Path == "/api/host.describe" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(protocol.Envelope{
				Type:   protocol.TypeServerResponse,
				Result: &protocol.Result{Ok: true, Value: json.RawMessage(`{"cwd":"/x"}`)},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

// staleFakeOnPort is a live cookie-gated host on a fixed port whose held
// token is rejected: a 401 (with the gate marker) on every request — the
// bare index, the /?token= mint (no Set-Cookie = rejected), and the /api/
// paths alike. It runs as its OWN process (killWeb kills the fake, not
// the test — what happens to a real foreign dsh web).
func staleFakeOnPort(t *testing.T, port int) *exec.Cmd {
	t.Helper()
	requirePython(t)
	code := "import sys, http.server, socketserver\n" +
		"port = int(sys.argv[1])\n" +
		"body = b\"dsh web authentication required\"\n" +
		"class H(http.server.BaseHTTPRequestHandler):\n" +
		"    def _unauth(self):\n" +
		"        self.send_response(401)\n" +
		"        self.send_header(\"Content-Length\", str(len(body)))\n" +
		"        self.send_header(\"Connection\", \"close\")\n" +
		"        self.end_headers()\n" +
		"        self.wfile.write(body)\n" +
		"    do_GET = do_POST = _unauth\n" +
		"    def log_message(self, *a):\n" +
		"        pass\n" +
		"socketserver.ThreadingTCPServer.allow_reuse_address = True\n" +
		"socketserver.ThreadingTCPServer((\"127.0.0.1\", port), H).serve_forever()\n"
	cmd := exec.Command("python3", "-c", code, strconv.Itoa(port))
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd
}

// waitPort waits until the fake accepts connections.
func waitPort(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 200*time.Millisecond); err == nil {
			_ = c.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("fake did not come up")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// TestConnectNoop pins the (nil, held, nil) fall-throughs: feature off,
// non-loopback, a live + valid host, and a down host with no launcher.
func TestConnectNoop(t *testing.T) {
	t.Setenv("DSH_CLI_HOME", t.TempDir())
	port := freePort(t)

	if h, tok, err := Connect(downBase(port), "held", false); h != nil || tok != "held" || err != nil {
		t.Fatalf("disabled: (%v, %q, %v)", h, tok, err)
	}
	if h, tok, err := Connect("http://10.9.8.7:4000", "held", true); h != nil || tok != "held" || err != nil {
		t.Fatalf("non-loopback: (%v, %q, %v)", h, tok, err)
	}
	legacy := legacyFake(t)
	defer legacy.Close()
	if h, tok, err := Connect(legacy.URL, "held", true); h != nil || tok != "held" || err != nil {
		t.Fatalf("live + valid: (%v, %q, %v)", h, tok, err)
	}
	t.Setenv("PATH", t.TempDir()) // empty PATH: LookPath("dsh") misses
	if h, tok, err := Connect(downBase(port), "held", true); h != nil || tok != "held" || err != nil {
		t.Fatalf("no launcher: (%v, %q, %v)", h, tok, err)
	}
}

// TestConnectNoAutoEnv pins the DSH_NO_AUTOSTART escape hatch.
func TestConnectNoAutoEnv(t *testing.T) {
	t.Setenv("DSH_NO_AUTOSTART", "1")
	t.Setenv("DSH_CLI_HOME", t.TempDir())
	if h, tok, err := Connect(downBase(freePort(t)), "held", true); h != nil || tok != "held" || err != nil {
		t.Fatalf("no-autostart env: (%v, %q, %v)", h, tok, err)
	}
}

// TestConnectBrokenBin pins the explicit-DSH_BIN failure: an absent file
// is a real error (the caller asked for that exact launcher).
func TestConnectBrokenBin(t *testing.T) {
	t.Setenv("DSH_CLI_HOME", t.TempDir())
	t.Setenv("DSH_BIN", filepath.Join(t.TempDir(), "absent"))
	if _, _, err := Connect(downBase(freePort(t)), "held", true); err == nil {
		t.Fatal("want an error for an absent DSH_BIN")
	}
	StopAll()
}

// TestConnectLaunch pins the cold-host flow: a down base gets a child,
// its printed token is captured and stored, the registry holds the child,
// and Stop takes it down (the port stops answering).
func TestConnectLaunch(t *testing.T) {
	port := freePort(t)
	t.Setenv("DSH_BIN", fakeDsh(t, port))
	t.Setenv("DSH_CLI_HOME", t.TempDir()) // hermetic for SaveToken

	h, tok, err := Connect(downBase(port), "", true)
	if err != nil {
		t.Fatal(err)
	}
	if tok != "FAKE-TOK-123" {
		t.Fatalf("token = %q", tok)
	}
	if h.Pid() == 0 {
		t.Fatal("no pid")
	}
	if h.Restarted() {
		t.Fatal("cold launch should not be a restart")
	}
	if Current() != h {
		t.Fatal("Current() != the launched host")
	}
	cfg, _ := os.ReadFile(filepath.Join(os.Getenv("DSH_CLI_HOME"), "config.json"))
	if len(cfg) == 0 {
		t.Fatal("SaveToken wrote no config")
	}

	h.Stop()
	dl := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 200*time.Millisecond)
		if err != nil {
			break // refused: the child is down
		}
		_ = conn.Close()
		if time.Now().After(dl) {
			t.Fatal("child still serving after Stop")
		}
		time.Sleep(100 * time.Millisecond)
	}
	StopAll()
	if Current() != nil {
		t.Fatal("Current() should be nil after StopAll")
	}
}

// TestConnectLaunchNoToken pins the old-build flow: a launcher that binds
// without printing a ?token= line settles as a ready host with an empty
// token (a bare connection works) — instead of waiting out the 90s token
// deadline and erroring (the boot stall this test guards).
func TestConnectLaunchNoToken(t *testing.T) {
	port := freePort(t)
	t.Setenv("DSH_BIN", noTokenFake(t, port))
	t.Setenv("DSH_CLI_HOME", t.TempDir())

	start := time.Now()
	h, tok, err := Connect(downBase(port), "", true)
	if err != nil {
		t.Fatal(err)
	}
	if tok != "" {
		t.Fatalf("token = %q, want empty for a token-less old build", tok)
	}
	if h.Pid() == 0 {
		t.Fatal("no pid")
	}
	if d := time.Since(start); d > 20*time.Second {
		t.Fatalf("connect took %s: a token-less host must settle on ready (grace is 2s)", d)
	}
	StopAll()
}

// TestConnectLiveStale pins the stale-credentials flow: a live gated host
// that rejects the held token is killed (lsof) and relaunched on the same
// port; the fresh token is captured and the host reports a restart.
func TestConnectLiveStale(t *testing.T) {
	if _, err := exec.LookPath("lsof"); err != nil {
		t.Skip("lsof unavailable")
	}
	port := freePort(t)
	staleFakeOnPort(t, port)
	waitPort(t, port)
	t.Setenv("DSH_BIN", fakeDsh(t, port))
	t.Setenv("DSH_CLI_HOME", t.TempDir())

	h, tok, err := Connect(downBase(port), "STALE", true)
	if err != nil {
		t.Fatal(err)
	}
	if tok != "FAKE-TOK-123" {
		t.Fatalf("token = %q", tok)
	}
	if !h.Restarted() {
		t.Fatal("want the restart flag")
	}
	if Current() != h {
		t.Fatal("Current() != the relaunched host")
	}
	StopAll()
}

// TestConnectChildDied pins the failure report: a launcher that exits
// before printing a token yields an error carrying its output tail.
func TestConnectChildDied(t *testing.T) {
	p := filepath.Join(t.TempDir(), "dsh-dying.sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\necho boot-fail-marker\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DSH_BIN", p)
	t.Setenv("DSH_CLI_HOME", t.TempDir())
	if _, _, err := Connect(downBase(freePort(t)), "", true); err == nil {
		t.Fatal("want an error for a token-less dying child")
	} else if got := err.Error(); !strings.Contains(got, "boot-fail-marker") {
		t.Fatalf("error lacks the output tail: %v", err)
	}
	StopAll()
}

// TestConnectSlowHost pins the probe nuance: a host that merely times
// out (not refused) is not relaunched — the accept-and-hold listener eats
// connections without answering.
func TestConnectSlowHost(t *testing.T) {
	port := freePort(t)
	l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() { time.Sleep(2 * time.Second); _ = c.Close() }()
		}
	}()
	t.Setenv("DSH_CLI_HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir()) // no launcher: a false relaunch would error
	if h, tok, err := Connect(downBase(port), "held", true); h != nil || tok != "held" || err != nil {
		t.Fatalf("slow host relaunched: (%v, %q, %v)", h, tok, err)
	}
}

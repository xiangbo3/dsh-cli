// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

// Package webhost manages a loopback dsh web across dsh-cli boots: a down
// host is launched (the printed launch token captured and stored), a live
// host that rejects the held credentials is killed and relaunched, and the
// server PERSISTS after dsh-cli exits (the child is re-parented; its stdio
// rides a log file in the data dir, not dsh-cli's pipes). A non-loopback
// base is never launched or killed.
package webhost

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"dsh-cli/internal/app"
	"dsh-cli/internal/client"
	"dsh-cli/internal/config"
)

const (
	probeTimeout    = 1500 * time.Millisecond
	tokenDeadline   = 90 * time.Second       // boot cap: token or bind, whichever settles first
	readyDeadline   = 20 * time.Second       // bind after the token print
	readyPoll       = 300 * time.Millisecond // bind-probe pace (watchReady, waitReady)
	tokenGrace      = 2 * time.Second        // ready-without-token: grace for a still-draining token line
	checkDeadline   = 15 * time.Second       // live-host credential pre-check
	stopGrace       = 3 * time.Second        // SIGTERM → SIGKILL
	freePortTimeout = 5 * time.Second        // kill → port released
	tailBytes       = 512
	outCap          = 1 << 16
	pollInterval    = 100 * time.Millisecond
)

// Env overrides: DSH_BIN names the dsh launcher explicitly; a non-empty
// DSH_NO_AUTOSTART disables the feature entirely (debug / a deliberate
// cold host).
const (
	envBin    = "DSH_BIN"
	envNoAuto = "DSH_NO_AUTOSTART"
)

// Host is one auto-started dsh web child (persisting past dsh-cli exit).
type Host struct {
	base      string
	restarted bool // true: a live web with stale credentials was killed first
	cmd       *exec.Cmd
	logPath   string
	mu        sync.Mutex
	out       bytes.Buffer
	token     chan string
	done      chan struct{} // child process exited
	stopped   atomic.Bool
}

var (
	curMu   sync.Mutex
	current *Host // the live child of THIS dsh-cli (nil = none)
)

// Pid is the child process id (0 when not running).
func (h *Host) Pid() int {
	if h.cmd.Process == nil {
		return 0
	}
	return h.cmd.Process.Pid
}

// Base is the server base the child serves.
func (h *Host) Base() string { return h.base }

// Restarted reports whether a live web with rejected credentials was
// killed before this launch (the caller picks the matching hint line).
func (h *Host) Restarted() bool { return h.restarted }

// LogPath is the child's stdout/stderr log (the data dir).
func (h *Host) LogPath() string { return h.logPath }

// Current is this dsh-cli's live child (nil when none).
func Current() *Host {
	curMu.Lock()
	defer curMu.Unlock()
	return current
}

// StopAll kills this dsh-cli's live child (idempotent). Exit no longer
// calls it — the web persists — but the kill-and-relaunch flow and tests
// use it.
func StopAll() {
	curMu.Lock()
	h := current
	current = nil
	curMu.Unlock()
	if h != nil {
		h.Stop()
	}
}

// SaveToken persists the captured launch token to the config
// (best-effort: a read-only home must not block the boot).
func SaveToken(tok string) {
	c := config.Load()
	c.Token = tok
	_ = config.Save(c)
}

// Result carries Connect's outcome to an async caller (the TUI boot):
// the launched or restarted host (nil when nothing was started), its
// fresh token ("" when the host printed none — an old build), and the
// boot error (nil otherwise; then Host is nil too).
type Result struct {
	Host  *Host
	Token string
	Err   error
}

// Connect makes base reachable with working credentials: a down host is
// launched (fresh token captured + stored); a live host is pre-checked
// with the held credentials, and a rejected pre-check kills the live web
// and relaunches it (fresh token). It returns (nil, held, nil) when
// nothing was started (live + valid, or the normal flow should proceed),
// or (host, token, nil) when a child is up — with token "" when the
// child came up without printing a token line (an old, pre-gate build:
// a bare connection works).
func Connect(base, held string, enabled bool) (*Host, string, error) {
	if !enabled || os.Getenv(envNoAuto) != "" {
		return nil, held, nil
	}
	if !loopbackBase(base) {
		return nil, held, nil // remote: never launch or kill a foreign server
	}
	if !up(base) {
		h, tok, err := launch(base, false)
		if h == nil && err == nil {
			return nil, held, nil // no launcher: the host.down flow reports it
		}
		return h, tok, err
	}
	// Live: do the held credentials (stored token, stored cookie, bare)
	// actually connect? The pre-check is one Describe on a throwaway app
	// over the shared per-base conn.
	a := app.NewWith(base, held)
	ctx, cancel := context.WithTimeout(context.Background(), checkDeadline)
	defer cancel()
	_, err := a.Client().Describe(ctx)
	if err == nil {
		return nil, held, nil // live + valid: untouched
	}
	if !errors.Is(err, client.ErrAuthRequired) {
		return nil, held, nil // transport/down mid-check: the normal flow reports
	}
	if err := killWeb(base); err != nil {
		return nil, "", fmt.Errorf("kill stale dsh web: %w", err)
	}
	return launch(base, true)
}

// launch spawns `dsh web` for base, waits for its launch token and its
// bind in parallel (a token-less old build is fine: it settles ready
// without a token), stores the token when one arrives, and returns the
// host. The child's stdio goes to a log file
// in the data dir (not pipes) so the server outlives dsh-cli without
// dying on a half-closed pipe write.
func launch(base string, restarted bool) (*Host, string, error) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return nil, "", err
	}
	bin := os.Getenv(envBin)
	if bin == "" {
		if bin, err = exec.LookPath("dsh"); err != nil {
			return nil, "", nil // no launcher: the host.down flow reports it
		}
	}
	if up(base) {
		return nil, "", nil // a sibling dsh-cli beat us between probe and spawn
	}
	port := portOf(u)
	logPath := "" // best-effort: an unresolvable home still launches (no log)
	if dir, err := config.DataDir(); err == nil {
		logPath = dir + "/webhost-" + port + ".log"
	}
	childF, ferr := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if ferr != nil && logPath != "" {
		logPath = "" // fall back to /dev/null
		childF, ferr = os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	}
	if ferr != nil {
		return nil, "", ferr
	}
	// exec hands the child its own dup of the write fd and invalidates the
	// parent's: the pump reads the same file through a separate read fd.
	readPath := logPath
	if readPath == "" {
		readPath = os.DevNull
	}
	readF, rerr := os.OpenFile(readPath, os.O_RDONLY, 0)
	if rerr != nil {
		_ = childF.Close()
		return nil, "", rerr
	}

	h := &Host{
		base:      base,
		restarted: restarted,
		logPath:   logPath,
		token:     make(chan string, 1),
		done:      make(chan struct{}),
	}
	h.cmd = exec.Command(bin, "web", "--no-open", "--host", u.Hostname(), "--port", port)
	h.cmd.Stdout = childF
	h.cmd.Stderr = childF
	setGroup(h.cmd) // own process group: the kill takes the whole tree
	if err := h.cmd.Start(); err != nil {
		_ = childF.Close()
		_ = readF.Close()
		return nil, "", fmt.Errorf("start %s: %w", bin, err)
	}
	go h.pump(readF)
	go func() { _ = h.cmd.Wait(); close(h.done) }()

	// The token and the bind settle independently: an old dsh web may
	// bind without printing a token line (a pre-gate build), and a
	// token-only wait would stall the boot for the full deadline. The
	// first select takes whichever side settles first; the second wait
	// settles the other on its own (shorter) deadline. The closed
	// readyCh is consumed once and never re-selected — waiting on it a
	// second time would spin until the token deadline (the old-build
	// boot stall).
	readyCh := make(chan struct{})
	go watchReady(base, h.done, readyCh)

	var (
		tok     string
		tokSeen bool
	)
	select {
	case tok = <-h.token:
		tokSeen = true
	case <-readyCh:
		// Ready first: an old build, or a token line the pump is still
		// draining — the short grace decides.
		select {
		case tok = <-h.token:
			tokSeen = true
		case <-time.After(tokenGrace):
		case <-h.done:
			h.Stop()
			if up(base) {
				return nil, "", nil // a sibling took the port after our child exited
			}
			return nil, "", fmt.Errorf("dsh web exited during boot: %s", h.tail())
		}
	case <-h.done:
		h.Stop()
		if up(base) {
			return nil, "", nil // a sibling beat us to the bind; its token is in the config
		}
		return nil, "", fmt.Errorf("dsh web exited before printing a launch token: %s", h.tail())
	case <-time.After(tokenDeadline):
		h.Stop()
		return nil, "", fmt.Errorf("dsh web printed no launch token within %s: %s", tokenDeadline, h.tail())
	}
	if tokSeen && !h.waitReady() {
		// Token first: the bind may still lag.
		h.Stop()
		return nil, "", fmt.Errorf("dsh web answered no probe within %s after printing its token: %s", readyDeadline, h.tail())
	}
	if tokSeen {
		SaveToken(tok)
	}
	curMu.Lock()
	current = h
	curMu.Unlock()
	return h, tok, nil
}

// pump tails the child's log file into the capped buffer and emits the
// launch token once it is fully printed (dsh web prints one http://…/?token=
// line at boot). It ends when the token lands or the child exits.
func (h *Host) pump(f *os.File) {
	defer f.Close()
	var off int64
	buf := make([]byte, 64*1024)
	for {
		if tok := h.drain(f, &off, buf); tok != "" {
			h.emit(tok)
			return
		}
		select {
		case <-h.done:
			// the child is gone: take its final bytes, then out
			h.drain(f, &off, buf)
			return
		default:
		}
		time.Sleep(pollInterval)
	}
}

// drain reads the log's new bytes into the capped buffer and reports a
// fully printed launch token ("" = none yet).
func (h *Host) drain(f *os.File, off *int64, buf []byte) string {
	st, err := f.Stat()
	if err != nil || st.Size() <= *off {
		return ""
	}
	if _, err := f.Seek(*off, 0); err != nil {
		return ""
	}
	n, _ := f.Read(buf)
	*off += int64(n)
	if n <= 0 {
		return ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.out.Len()+n > outCap {
		_ = h.out.Next(h.out.Len() - (outCap - n))
	}
	h.out.Write(buf[:n])
	return h.scan()
}

// scan extracts the launch token from the buffer: the first ?token= whose
// value already hit a non-token byte (the printed line ends with newline
// or whitespace; & would trail extra parameters). The caller holds h.mu.
func (h *Host) scan() string {
	s := h.out.String()
	i := strings.Index(s, "?token=")
	if i < 0 {
		return ""
	}
	rest := s[i+len("?token="):]
	for j := 0; j < len(rest); j++ {
		if !isTokByte(rest[j]) {
			return rest[:j]
		}
	}
	return "" // value not terminated yet: more chunks may arrive
}

// emit is called by pump with the scanner's verdict (h.mu released).
func (h *Host) emit(tok string) {
	if tok == "" {
		return
	}
	select {
	case h.token <- tok:
	default:
	}
}

func isTokByte(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '+'
}

// waitReady polls the index until any HTTP response (a gated build
// answers 401 bare; both prove the bind).
func (h *Host) waitReady() bool {
	dl := time.Now().Add(readyDeadline)
	for time.Now().Before(dl) {
		select {
		case <-h.done:
			return false // the child died; the tail is in the caller's error
		default:
		}
		if up(h.base) {
			return true
		}
		time.Sleep(readyPoll)
	}
	return up(h.base)
}

// watchReady closes ch once base first answers a probe (up counts a
// timeout as up, like waitReady). It ends without a verdict when the
// child exits — the caller's done arm settles the outcome.
func watchReady(base string, done <-chan struct{}, ch chan<- struct{}) {
	defer close(ch)
	for {
		if up(base) {
			return
		}
		select {
		case <-done:
			return
		case <-time.After(readyPoll):
		}
	}
}

// Stop ends the child: SIGTERM to its process group, SIGKILL after the
// grace, then reaps it. Idempotent; a self-exited child is just reaped.
func (h *Host) Stop() {
	if h.stopped.Swap(true) {
		return
	}
	if h.cmd.Process == nil {
		return
	}
	killGroup(h.cmd.Process.Pid, sigTerm)
	select {
	case <-h.done:
	case <-time.After(stopGrace):
		killGroup(h.cmd.Process.Pid, sigKill)
	}
	go func() { _ = h.cmd.Wait() }() // reap (pump's Wait already did: idempotent)
}

// tail is the end of the child's output for a failure report. The log
// file is the source of truth (the pump may still be draining when a
// fast exit reaches the caller); the buffer is the /dev/null fallback.
func (h *Host) tail() string {
	if h.logPath != "" {
		if b, err := os.ReadFile(h.logPath); err == nil && len(b) > 0 {
			s := string(b)
			if len(s) > tailBytes {
				s = "…" + s[len(s)-tailBytes:]
			}
			return strings.TrimSpace(s)
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.out.String()
	if len(s) > tailBytes {
		s = "…" + s[len(s)-tailBytes:]
	}
	return strings.TrimSpace(s)
}

// killWeb ends whatever listens on base's port (the live web — ours or
// the user's): SIGTERM the lsof-reported pids, SIGKILL after the grace,
// then wait for the port to free. lsof-missing falls back to pkill -f.
func killWeb(base string) error {
	port := portOf(mustParse(base))
	pids, lerr := listeners(port)
	if lerr != nil {
		// lsof missing (or unreadable): the broad fallback, then poll
		if pk := exec.Command("pkill", "-f", "dsh web"); pk.Run() != nil && len(pids) == 0 {
			return fmt.Errorf("no lsof and pkill found no dsh web: %w", lerr)
		}
	}
	for _, p := range pids {
		syskill(p, sigTerm)
	}
	dl := time.Now().Add(stopGrace)
	for time.Now().Before(dl) {
		if !bound(base) {
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	for _, p := range pids {
		syskill(p, sigKill)
	}
	dl = time.Now().Add(freePortTimeout)
	for time.Now().Before(dl) {
		if !bound(base) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("port %s still bound after killing dsh web", port)
}

// listeners reports the pids holding a LISTEN socket on port (lsof).
func listeners(port string) ([]int, error) {
	out, err := exec.Command("lsof", "-nP", "-iTCP:"+port, "-sTCP:LISTEN", "-t").Output()
	if err != nil {
		return nil, err
	}
	var pids []int
	for _, f := range strings.Fields(string(out)) {
		if p, err := strconv.Atoi(f); err == nil {
			pids = append(pids, p)
		}
	}
	return pids, nil
}

// bound is a raw dial probe (faster than an HTTP round trip, and true as
// soon as the socket accepts, mid-shutdown or not).
func bound(base string) bool {
	c, err := net.DialTimeout("tcp", mustParse(base).Host, 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// up probes the index with a short timeout. A timeout counts as up (a
// slow server must not trigger a second launch).
func up(base string) bool {
	hc := &http.Client{Timeout: probeTimeout}
	req, err := http.NewRequest(http.MethodGet, base+"/", nil)
	if err != nil {
		return false
	}
	resp, err := hc.Do(req)
	if err != nil {
		var ue *url.Error
		return errors.As(err, &ue) && ue.Timeout()
	}
	_ = resp.Body.Close()
	return true
}

func mustParse(base string) *url.URL {
	u, _ := url.Parse(base)
	if u == nil {
		u = &url.URL{}
	}
	return u
}

func portOf(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	if u.Scheme == "https" {
		return "443"
	}
	return "80"
}

func loopbackBase(base string) bool {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return false
	}
	return loopback(u.Hostname())
}

func loopback(h string) bool {
	if h == "localhost" || h == "127.0.0.1" || h == "::1" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

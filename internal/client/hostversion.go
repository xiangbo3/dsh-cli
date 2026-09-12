// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package client

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// envBinLocal names the dsh launcher explicitly (the auto-start override
// the webhost carries; kept local so the client package stays free of it).
const envBinLocal = "DSH_BIN"

var (
	hostVerMu    sync.Mutex
	hostVerTried bool
	hostVer      string

	verTokRe = regexp.MustCompile(`^\d+\.\d+`)
)

// hostVersionFallback probes the local dsh launcher once for its version:
// the new (cookie-gated) build publishes no version on the wire (the
// ready frame carries home only), so the composed snapshot falls back to
// the binary the auto-start would launch ($DSH_BIN or PATH). "" until a
// probe succeeds; a failed probe is cached too (the launcher is either
// there or not for this whole run).
func hostVersionFallback() string {
	hostVerMu.Lock()
	defer hostVerMu.Unlock()
	if hostVerTried {
		return hostVer
	}
	hostVerTried = true
	bin := os.Getenv(envBinLocal)
	if bin == "" {
		b, err := exec.LookPath("dsh")
		if err != nil {
			return ""
		}
		bin = b
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").CombinedOutput()
	if err != nil {
		return ""
	}
	// The printout is "0.1.2-rc.1" (or "<name> 0.1.2-rc.1"): take the
	// first version-shaped token.
	for _, tok := range strings.Fields(string(out)) {
		if verTokRe.MatchString(tok) {
			hostVer = tok
			return hostVer
		}
	}
	return ""
}

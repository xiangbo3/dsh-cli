// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"strings"
	"testing"

	"dsh-cli/internal/app"
)

// TestStatusBarPlainHTTPMarker pins the persistent plain-remote marker:
// an http:// off-loopback remote (prompts travel in the clear) keeps a
// warn-colored readout on the status bar, while https and loopback
// remotes keep the plain live readout.
func TestStatusBarPlainHTTPMarker(t *testing.T) {
	cases := []struct {
		base string
		key  string // the connection readout the bar must carry
	}{
		{base: "http://10.0.0.5:3080", key: "status.plainhttp"},
		{base: "https://10.0.0.5:3080", key: "status.live"},
		{base: "http://127.0.0.1:3080", key: "status.live"},
		{base: "http://localhost:3080", key: "status.live"},
	}
	for _, c := range cases {
		m := NewModel(app.New(c.base))
		m.splashOff = true
		m.st.SetConnected(true)
		line := stripANSI(m.statusBar(140))
		want := m.loc.T(c.key)
		if !strings.Contains(line, want) {
			t.Errorf("base %s: statusBar missing %q: %q", c.base, want, line)
		}
	}
}

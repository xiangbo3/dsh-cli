// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package textutil

import (
	"testing"
	"time"
)

func TestStripControl(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"clean", "plain text\nline 2", "plain text\nline 2"},
		{"ansi", "a\x1b[31mred\x1b[0m b", "a[31mred[0m b"},
		{"crlf", "a\rb\nc", "ab\nc"},
		{"del", "a\x7fb", "ab"},
		{"c1", "a\u009db", "ab"},
		{"nbsp", "a\u00a0b", "a\u00a0b"},
		{"cjk", "世界\nok", "世界\nok"},
		{"empty", "", ""},
		{"newline-only", "\n", "\n"},
	}
	for _, c := range cases {
		if got := StripControl(c.in); got != c.want {
			t.Errorf("%s: StripControl(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
	// The common clean input must be the same string (no allocation
	// surprise, no copy).
	in := "no controls here\n"
	if got := StripControl(in); got != in {
		t.Fatalf("clean input rewrote the string: %q", got)
	}
}
func TestHumanDuration(t *testing.T) {
	cases := []struct {
		name string
		in   time.Duration
		want string
	}{
		{"zero", 0, "0s"},
		{"negative", -3 * time.Second, "0s"},
		{"sub-second", 999 * time.Millisecond, "0s"},
		{"seconds", 45 * time.Second, "45s"},
		{"exact minute", time.Minute, "1m"},
		{"minute plus seconds", 90 * time.Second, "1m 30s"},
		{"many minutes", 5 * time.Minute, "5m"},
		{"exact hour", time.Hour, "1h"},
		{"hour plus minute", time.Hour + time.Minute, "1h 1m"},
		{"two hours five minutes", 2*time.Hour + 5*time.Minute, "2h 5m"},
		{"exact day", 24 * time.Hour, "1d"},
		{"day plus hours", 26*time.Hour + 24*time.Minute, "1d 2h"},
		{"millisecond precision", 45999 * time.Millisecond, "45s"},
	}
	for _, c := range cases {
		if got := HumanDuration(c.in); got != c.want {
			t.Errorf("%s: HumanDuration(%s) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestStripANSI(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "no escapes here", "no escapes here"},
		{"csi", "a\x1b[31mred\x1b[0m b", "ared b"},
		{"csi-param", "a\x1b[1;32;44mgreen\x1b[0m", "agreen"},
		{"csi-question", "a\x1b[?25lhidden\x1b[?25h b", "ahidden b"},
		{"osc-bel", "a\x1b]0;evil title\x07kept", "akept"},
		{"osc-st", "a\x1b]8;;https://x\x1b\\link\x1b]8;;\x1b\\ b", "alink b"},
		{"dcs", "a\x1bPq\x1b\\b", "ab"},
		{"esc-pair", "a\x1b[Ab", "ab"},
		{"trailing-esc", "ab\x1b", "ab"},
		{"unterminated-osc", "a\x1b]0;never closed", "a"},
		{"nested-ansi", "\x1b[31m\x1b[32mx\x1b[0m", "x"},
		{"keep-text", "\x1b[1mbold\x1b[0m plain", "bold plain"},
		{"cjk", "\x1b]7;file=//host/world\x07世界", "世界"},
	}
	for _, c := range cases {
		if got := StripANSI(c.in); got != c.want {
			t.Errorf("%s: StripANSI(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
	// fast path: no ESC byte means the text comes back unchanged
	s := "plain text"
	if got := StripANSI(s); got != s {
		t.Error("fast path changed the text")
	}
}

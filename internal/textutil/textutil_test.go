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

// Package textutil holds rune-safe text helpers shared by every layer that
// truncates host- or model-controlled strings (error messages, transcript
// notes, sidebar rows). Byte-wise slicing can split a multi-byte rune and
// feed invalid UTF-8 to the terminal or shift display widths.
package textutil

import (
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Truncate returns s clipped to at most n bytes; if the cut would split a
// rune, it backs off to the previous boundary and appends the cutter
// ("…" by default, "" for a pure byte clip). The result is at most n
// bytes including the cutter.
func Truncate(s string, n int, cutter string) string {
	if n < 0 {
		n = 0
	}
	if len(s) <= n {
		return s
	}
	if cutter != "" && n < len(cutter) {
		n = len(cutter)
	}
	cut := n
	if cutter != "" {
		cut = n - len(cutter)
	}
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	if cut == 0 {
		return string(cutter)
	}
	return s[:cut] + cutter
}

// Clip is a rune-safe byte clip without a cutter.
func Clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// TruncateMiddle keeps the first and last parts of s within n bytes total,
// separated by "…"; rune boundaries on both ends are respected.
func TruncateMiddle(s string, n int) string {
	if len(s) <= n {
		return s
	}
	head := (n - 1) / 2
	tail := n - 1 - head
	cut := head
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	start := len(s) - tail
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[:cut] + "…" + s[start:]
}

// StripControl removes Cc control runes from host-provided text, keeping
// line breaks: a hostile or buggy host can answer with raw ANSI/VT
// sequences, which would otherwise be written to the terminal verbatim and
// re-interpreted as cursor moves or colour changes. The byte pre-scan keeps
// the common (already clean) input allocation-free: only ASCII control bytes
// and 0xC2 0x80–0x9F (the C1 range) can start a control rune — multi-byte
// continuation bytes (0x80–0xBF) and leading bytes above 0xC2 never match.
func StripControl(s string) string {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 0x20 && c != '\n') || c == 0x7f || (c == 0xC2 && i+1 < len(s) && s[i+1] <= 0x9f) {
			var b strings.Builder
			b.Grow(len(s))
			for _, r := range s {
				if r != '\n' && unicode.IsControl(r) {
					continue
				}
				b.WriteRune(r)
			}
			return b.String()
		}
	}
	return s
}

// HumanDuration renders d in user-friendly units instead of raw seconds: the
// two most significant non-zero units at most — "45s", "2m 5s", "1h 30m",
// "1d 4h". Trailing zero units are omitted (an exact minute reads "5m",
// never "5m 0s"), and a negative or sub-second duration reads "0s".
func HumanDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	secs := int64(d / time.Second)
	units := []struct {
		size int64
		suf  string
	}{
		{86400, "d"},
		{3600, "h"},
		{60, "m"},
		{1, "s"},
	}
	parts := make([]string, 0, 2)
	for _, u := range units {
		if v := secs / u.size; v > 0 {
			parts = append(parts, strconv.FormatInt(v, 10)+u.suf)
			secs %= u.size
			if len(parts) == 2 {
				break
			}
		}
	}
	if len(parts) == 0 {
		return "0s"
	}
	return strings.Join(parts, " ")
}

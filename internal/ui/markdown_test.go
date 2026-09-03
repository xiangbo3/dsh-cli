package ui

import (
	"strings"
	"testing"

	"dsh-cli/internal/app"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestMarkdownFootnotesAndShortcodes pins the transcript markdown
// pipeline: `:name:` emoji shortcodes stay LITERAL (the shortcode
// extension is off, so hex/MAC-adjacent tokens survive verbatim), raw
// emoji pass through, footnote markers stay literal "[^n]" in the body,
// the trailing footnote list renders as a numbered list in reference
// order (unreferenced definitions are dropped, upstream-style), and no
// rendered line overflows the width.
func TestMarkdownFootnotesAndShortcodes(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	a := app.New("http://127.0.0.1:3999")
	m := NewModel(a)
	m.th.Profile = termenv.TrueColor            // shortcodes need a colored tier
	lipgloss.SetColorProfile(termenv.TrueColor) // go test's stdout is not a TTY

	// Emoji shortcodes stay literal (no :name: conversion); raw emoji
	// pass through untouched.
	lines := m.mdRender("hi :smile: :tada: and 😀 here", 60, "")
	out := strings.Join(lines, "\n")
	plain := stripANSI(out)
	if !strings.Contains(plain, ":smile:") || !strings.Contains(plain, ":tada:") {
		t.Fatalf("shortcodes must stay literal: %q", plain)
	}
	if !strings.Contains(plain, "😀") {
		t.Fatalf("raw emoji lost: %q", plain)
	}
	for _, ln := range lines {
		if w := plainWidth(ln); w > 60 {
			t.Fatalf("emoji line %d cells wide, want <= 60: %q", w, ln)
		}
	}

	// Footnotes: definitions listed out of reference order on purpose.
	foot := m.mdRender("refs[^1] and again[^1] then[^2]\n\n[^2]: note two\n\n[^1]: note one", 60, "")
	plain = stripANSI(strings.Join(foot, "\n"))
	if iOne, iTwo := strings.Index(plain, "note one"), strings.Index(plain, "note two"); iOne < 0 || iTwo < 0 || iOne > iTwo {
		t.Fatalf("footnote list missing or out of order: %q", plain)
	}
	if !strings.Contains(plain, "[^1]") || !strings.Contains(plain, "[^2]") {
		t.Fatalf("footnote markers lost: %q", plain)
	}
	for _, ln := range foot {
		if w := plainWidth(ln); w > 60 {
			t.Fatalf("footnote line %d cells wide, want <= 60: %q", w, ln)
		}
	}
	numbered := 0
	for _, ln := range strings.Split(plain, "\n") {
		if strings.Contains(ln, "note") {
			numbered++
		}
	}
	if numbered < 2 || strings.Count(plain, ". note") < 2 {
		t.Fatalf("expected two numbered footnote lines: %q", plain)
	}

	// An unreferenced definition stays out of the list.
	orphan := stripANSI(strings.Join(m.mdRender("body only\n\n[^9]: orphan", 60, ""), "\n"))
	if strings.Contains(orphan, "orphan") {
		t.Fatalf("unreferenced footnote must not render: %q", orphan)
	}
}

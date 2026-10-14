// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"os"
	"testing"

	"github.com/mattn/go-runewidth"
)

// TestMain redirects the app's data directory to a throwaway dir and
// disables the boot cache (DSH_CLI_NO_BOOT_CACHE): test models that Start
// against a live server — or the developer's own running TUI — would
// otherwise persist fixture rows into the shared test data dir (and the
// user's real ~/.dsh-cli), and every later Start loads those rows back:
// stale titles, rosters and workspaces leaking between tests.
func TestMain(m *testing.M) {
	if d, err := os.MkdirTemp("", "dsh-cli-ui-test-"); err == nil {
		os.Setenv("DSH_CLI_HOME", d)
	}
	os.Setenv("DSH_CLI_NO_BOOT_CACHE", "1")
	// The layout tests' width invariants assume ambiguous-width runes
	// (─ … ▓) take one cell; runewidth follows the locale, so pin that
	// reading and keep the suite green under a CJK LC_CTYPE too.
	runewidth.DefaultCondition.EastAsianWidth = false
	runewidth.DefaultCondition.StrictEmojiNeutral = true
	os.Exit(m.Run())
}

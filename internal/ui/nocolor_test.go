package ui

import (
	"context"
	"testing"
	"time"

	"dsh-cli/internal/app"

	"github.com/muesli/termenv"
)

// TestNoColorCodeBlockNoPanic pins the fix for the NO_COLOR crash: the
// NoColor palette speaks in 256-level indices and the word "default",
// which the chroma entry parser rejects — the first fenced code block
// used to panic the whole TUI. TERM is set so the color profile is not
// Ascii (Ascii would skip the chroma path and mask the bug).
func TestNoColorCodeBlockNoPanic(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "xterm-256color")
	th := NewTheme()
	if !th.NoColor {
		t.Fatal("expected a NoColor theme")
	}
	// Under go test stdout is not a TTY, so the detected profile is Ascii
	// and the chroma path (the one that used to panic) is skipped — pin a
	// real color profile to exercise it.
	th.Profile = termenv.TrueColor
	a := app.New("http://127.0.0.1:3999")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a.Start(ctx)
	m := NewModel(a)
	m.th = th
	m.W, m.H = 100, 30

	out := m.mdRender("```go\nfunc main() { println(\"hi\") }\n```\n", 90, "")
	if len(out) == 0 {
		t.Fatal("code block rendered nothing")
	}
}

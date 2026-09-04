// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"dsh-cli/internal/app"
)

func TestZZSyncDebug(t *testing.T) {
	if len(os.Getenv("SYNC_DEBUG")) == 0 {
		t.Skip("no SYNC_DEBUG")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a := app.New("http://127.0.0.1:3080")
	a.Start(ctx)
	m := NewModel(a)
	m.W, m.H = 100, 30
	start := time.Now()
	m.splashStart = start
	m.now = start.Add(splashReveal + 50*time.Millisecond)
	view := m.splashView()
	// locate the art block by finding the row that equals row 0
	lines := strings.Split(stripANSI(view), "\n")
	var art []string
	for i := 0; i < len(lines)-7; i++ {
		if strings.TrimSpace(lines[i]) == strings.TrimSpace(splashArt[0]) {
			art = lines[i : i+8]
			break
		}
	}
	if len(art) != 8 {
		t.Fatal("art block not found")
	}
	for r := 0; r < 8; r++ {
		want := string([]rune(strings.TrimLeft(art[r], " ")))[:len(splashArt[r])]
		if want != splashArt[r] {
			t.Logf("row %d MISMATCH:\n got %q\nwant %q", r, want, splashArt[r])
		} else {
			t.Logf("row %d ok", r)
		}
	}
}

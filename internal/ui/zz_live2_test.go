// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"context"
	"testing"
	"time"

	"dsh-cli/internal/app"
)

func TestZZLiveModelSwitch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	a := app.New("http://127.0.0.1:3080")
	a.Start(ctx)
	m := NewModel(a)
	newCmd, _ := m.localSlash("new", "")
	c, ok := newCmd().(createMsg)
	if !ok || c.id == "" {
		t.Skipf("no live server for session creation (%T)", c)
	}
	id := c.id
	// The create worker only reports the id (it must not touch model
	// state); apply the focus change here, as Update does for
	// createMsg.
	m.st.SetActive(id)
	t.Logf("created session %s", id)
	c2, cancel2 := context.WithTimeout(ctx, 15*time.Second)
	defer cancel2()
	cat, err := a.Models(c2, id)
	if err != nil {
		t.Skipf("catalog fetch failed: %v", err)
	}
	target := cat.Current.Model
	if target == "" {
		for _, g := range cat.Groups {
			for _, mo := range g.Models {
				target = mo.Id
				break
			}
			if target != "" {
				break
			}
		}
	}
	if target == "" {
		t.Skip("no models advertised by live server")
	}
	t.Logf("switching by name %q (current %s/%s)", target, cat.Current.Provider, cat.Current.Model)
	cmd, _ := m.localSlash("model", target)
	msg := cmd()
	t.Logf("switch result: %T", msg)
	select {
	case n := <-m.st.Notices():
		t.Logf("notice: [%s] %s", n.Level, n.Text)
	case <-time.After(5 * time.Second):
		t.Log("no notice within 5s")
	}
}

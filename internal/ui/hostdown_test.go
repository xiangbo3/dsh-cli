// Host-down hint tests: the "start dsh web" toast fires once per outage,
// hostDownGrace after the downlink stays dark, and re-arms after recovery.
package ui

import (
	"context"
	"testing"
	"time"

	"dsh-cli/internal/app"
)

// hostDownModel builds a model whose downlink never reports up: the app
// points at a dead port, so nothing flips the store's Connected flag.
func hostDownModel(t *testing.T) *Model {
	t.Helper()
	a := app.New("http://127.0.0.1:3999") // dead port: fixture only
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	a.Start(ctx)
	m := NewModel(a)
	m.splashOff = true
	m.W, m.H = 120, 40
	return m
}

// TestHostDownHintFiresOnce pins the hint's cadence: no toast before the
// grace window, exactly one warn toast once it elapses, no re-toast while
// the outage persists, and a re-armed hint after recovery + re-outage.
func TestHostDownHintFiresOnce(t *testing.T) {
	m := hostDownModel(t)
	if m.st.Connected() {
		t.Fatal("fixture must start disconnected")
	}
	t0 := time.Now()
	m.Update(tickMsg{t: t0})
	m.Update(tickMsg{t: t0.Add(hostDownGrace - time.Millisecond)})
	if len(m.toasts) != 0 {
		t.Fatalf("toasts before grace = %d, want 0", len(m.toasts))
	}
	m.Update(tickMsg{t: t0.Add(hostDownGrace + time.Millisecond)})
	if len(m.toasts) != 1 || m.toasts[0].level != "warn" || m.toasts[0].text != m.loc.T("host.down") {
		t.Fatalf("toasts = %+v, want the single host-down hint", m.toasts)
	}
	// The outage persists: later ticks must not stack more hints.
	m.Update(tickMsg{t: t0.Add(10 * time.Second)})
	if got := len(m.toasts); got != 1 {
		t.Fatalf("toasts during the outage = %d, want 1", got)
	}
	// Recovery resets the tracker; a later outage earns the hint again
	// (the first toast has expired by then, so the shelf holds the new one).
	m.st.SetConnected(true)
	m.Update(tickMsg{t: t0.Add(11 * time.Second)})
	m.st.SetConnected(false)
	m.Update(tickMsg{t: t0.Add(13 * time.Second)})
	m.Update(tickMsg{t: t0.Add(15*time.Second + time.Millisecond)})
	if got := len(m.toasts); got != 1 || m.toasts[0].text != m.loc.T("host.down") {
		t.Fatalf("toasts after the re-outage = %+v, want the re-fired hint", m.toasts)
	}
}

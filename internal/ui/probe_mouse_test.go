package ui

import (
	"strings"
	"testing"

	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// stripCSI removes CSI sequences (SGR styling, cursor moves).
func stripCSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] == ';' || (s[j] >= '0' && s[j] <= '9')) {
				j++
			}
			if j < len(s) {
				i = j
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// TestProbeMouse verifies the rendered frame <-> mouse mapping end to end:
// find a transcript marker's screen row in the real frame, click there
// through the public Update path, and check the anchored line.
func TestProbeMouse(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	lipgloss.SetColorProfile(termenv.TrueColor)
	m := scrollModel(t, 4)
	m.th.Profile = termenv.TrueColor
	m.W, m.H = 100, 28

	frame := m.View()
	rows := strings.Split(frame, "\n")
	t.Logf("frame rows: %d (W=%d H=%d transX=%d transY=%d transH=%d)",
		len(rows), m.W, m.H, m.transX, m.transY, m.transH)
	markerRow := -1
	for i, r := range rows {
		if strings.Contains(stripCSI(r), "line-004") {
			markerRow = i
		}
	}
	if markerRow < 0 {
		t.Fatalf("marker not on screen:\n%s", stripCSI(frame))
	}
	t.Logf("marker on screen row %d", markerRow)

	x := m.transX + 10
	m.Update(tea.MouseMsg{X: x, Y: markerRow, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m.Update(tea.MouseMsg{X: x, Y: markerRow, Button: tea.MouseButtonNone, Action: tea.MouseActionRelease})
	t.Logf("sel after click: %+v", m.sel)

	abs := -1
	for i, ln := range m.trans.lines {
		if strings.Contains(stripCSI(ln), "line-004") {
			abs = i
		}
	}
	// A plain click anchors a zero-size pick at the clicked cell (crush's
	// click ladder: nothing is banded until a drag/word/line happens).
	if m.sel.anchorLine != abs || m.sel.anchorCol != 10 {
		t.Fatalf("click on screen row %d anchored (%d,%d), marker absolute line is %d",
			markerRow, m.sel.anchorLine, m.sel.anchorCol, abs)
	}

	// Drag: anchor at marker row, extend one row up. The ladder resets,
	// or this press would count as a double-click on the marker cell.
	m.lastPressT = time.Time{}
	m.Update(tea.MouseMsg{X: x, Y: markerRow, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m.Update(tea.MouseMsg{X: x, Y: markerRow - 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	m.Update(tea.MouseMsg{X: x, Y: markerRow - 1, Button: tea.MouseButtonNone, Action: tea.MouseActionRelease})
	t.Logf("sel after drag: %+v", m.sel)
	f, fc, l, lc := m.sel.bounds(len(m.trans.lines))
	if f != abs-1 || fc != 10 || l != abs || lc != 10 {
		t.Fatalf("drag anchored [%d %d %d %d], wanted [%d 10 %d 10]", f, fc, l, lc, abs-1, abs)
	}

	// The pick band must actually paint on screen rows markerRow-1..markerRow
	// (the band SGR carries a background code; a full-width band fast path or
	// a partial cell merge both emit "48;" for the palette theme).
	frame2 := m.View()
	rows2 := strings.Split(frame2, "\n")
	for _, rr := range []int{markerRow - 1, markerRow} {
		if !strings.Contains(rows2[rr], "48;") {
			t.Fatalf("row %d has no background styling under the pick band: %q", rr, rows2[rr])
		}
	}
	if strings.Contains(rows2[markerRow-2], "48;") {
		t.Fatalf("row %d (above the pick) unexpectedly carries the band", markerRow-2)
	}
	for i := 0; i < len(rows2); i++ {
		if strings.Contains(rows2[i], "\x1b[") {
			t.Logf("row %d raw: %q", i, rows2[i])
		}
	}
	t.Log("ok")
}

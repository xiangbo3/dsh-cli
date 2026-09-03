package ui

import (
	"encoding/json"
	"strings"
	"testing"

	"dsh-cli/internal/protocol"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// selFrameModel builds a model whose transcript mixes CJK user lines and a
// Go fenced block (tabs and all) — the content class that mangled the
// indentation before.
func selFrameModel(t *testing.T) *Model {
	t.Helper()
	m := scrollModel(t, 4)
	m.W, m.H = 120, 35
	for i, txt := range []string{
		"先看一下 main.go 里的 handleMouse 函数",
		"```go\nfunc main() {\n\tif x > 0 {\n\t\tfmt.Println(x)\n\t}\n}\n```",
	} {
		b, err := json.Marshal(protocol.Message{Role: "user",
			Content: []protocol.ContentBlock{{Type: "text", Text: txt}}})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		m.st.Event("s1", &protocol.SessionEvent{Type: "user/message", Seq: int64(100 + i), Time: 1000, Data: b})
	}
	return m
}

// frameWidths returns each frame row's visible cell width.
func frameWidths(frame string) []int {
	var out []int
	for _, ln := range strings.Split(frame, "\n") {
		out = append(out, plainWidth(ln))
	}
	return out
}

// TestSelectionFrameKeepsWidth is the regression for "selecting mangles the
// whole screen": a single frame row wider than the window wraps on the
// terminal and shifts every row below it (the bubbletea renderer then
// drifts). It pins that (a) no row of the frame — picked or not — exceeds
// the window width with a Go code block + CJK in the transcript, and (b)
// the pick band changes no visible width at all (lipgloss/cellbuf keeps
// the row's cell count).
func TestSelectionFrameKeepsWidth(t *testing.T) {
	for name, profile := range map[string]termenv.Profile{
		"truecolor": termenv.TrueColor,
		"noColor":   termenv.Ascii,
	} {
		t.Run(name, func(t *testing.T) {
			if profile == termenv.Ascii {
				t.Setenv("NO_COLOR", "")
			} else {
				t.Setenv("NO_COLOR", "")
			}
			lipgloss.SetColorProfile(profile)
			m := selFrameModel(t)
			m.th.Profile = profile
			m.View() // publish the pane origin

			before := frameWidths(m.View())
			var over int
			for i, w := range before {
				if w > m.W {
					over++
					t.Errorf("row %d is %d cells in a %d-cell window", i, w, m.W)
				}
			}

			// Drag a pick across the code block.
			m.Update(tea.MouseMsg{X: m.transX + 2, Y: m.transY + 4, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
			m.Update(tea.MouseMsg{X: m.transX + 2, Y: m.transY + 9, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
			m.Update(tea.MouseMsg{X: m.transX + 2, Y: m.transY + 9, Button: tea.MouseButtonNone, Action: tea.MouseActionRelease})
			if !m.sel.active {
				t.Fatalf("drag did not pick a range: %+v", m.sel)
			}
			f, _, l, _ := m.sel.bounds(len(m.trans.lines))
			if f < 0 || f >= l {
				t.Fatalf("drag pick must span lines: %d %d", f, l)
			}
			after := frameWidths(m.View())
			for i := 0; i < len(before) && i < len(after); i++ {
				if before[i] != after[i] {
					t.Errorf("row %d width %d -> %d under the pick band", i, before[i], after[i])
				}
				if after[i] > m.W {
					t.Errorf("row %d is %d cells in a %d-cell window (with pick)", i, after[i], m.W)
				}
			}
			if over > 0 {
				t.Errorf("%d rows already overflowed the window before picking", over)
			}
		})
	}
}

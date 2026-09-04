// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"dsh-cli/internal/core"
	"dsh-cli/internal/i18n"
	"dsh-cli/internal/modes"
	"dsh-cli/internal/protocol"

	"github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
)

// presetsLoadedMsg delivers the agent-preset roster to the mode picker.
type presetsLoadedMsg struct {
	presets []protocol.AgentPresetEntry
}

// modePicker is the TUI face of the deployment's mode roster (the web's
// agent-preset surfaces): one row per preset — the four shipped modes
// (Standard / PTC / Minimal / Creator) plus any user-authored presets —
// with the active session's current mode marked. A pick applies in place
// while the session is still blank; a session that has already run keeps
// the composition it started under, so the pick stages for the next new
// session (the web hero-chip rule).
type modePicker struct {
	loading bool
	err     string
	presets []protocol.AgentPresetEntry
	cur     int
	// note is the context-dependent apply/stage line (set by view from
	// the active session; the box footer renders it under the key map).
	note string
	loc  *i18n.Locale
}

func (p *modePicker) title() string { return p.loc.T("mode.title") }

func (p *modePicker) hint() string {
	h := p.loc.T("mode.hint")
	if p.note != "" {
		h += " · " + p.note
	}
	return h
}

func (p *modePicker) view(m *Model, w, h int) []string {
	th := m.th
	if p.loading {
		return []string{th.CardStyle(th.Subtle()).Render(p.loc.T("mode.loading"))}
	}
	if p.err != "" {
		return []string{th.CardStyle(th.Err()).Render("  " + p.err)}
	}
	if len(p.presets) == 0 {
		return []string{th.CardStyle(th.Faint()).Render(p.loc.T("mode.none"))}
	}
	cur, blank := "", true
	if id := m.activeID(); id != "" {
		if snap := m.st.Get(id); snap != nil {
			cur = snap.Summary.AgentPreset
			blank = snap.Summary.Blank
		}
	}
	// The pick's effect depends on the active session: apply in place
	// while it is still blank, stage for new sessions once it has run.
	if blank {
		p.note = p.loc.T("mode.note.apply")
	} else {
		p.note = p.loc.T("mode.note.stage")
	}
	var lines []string
	for i, e := range p.presets {
		cursor := th.Card().Render("    ")
		if i == p.cur {
			cursor = th.CardStyle(th.Accent()).Render("  " + th.Glyph.Caret + " ")
		}
		var marks []string
		if e.IsDefault {
			marks = append(marks, p.loc.T("mode.mark.default"))
		}
		if e.Id == cur {
			marks = append(marks, th.Glyph.Bullet+p.loc.T("mode.mark.current"))
		}
		if e.Trust == "user" {
			marks = append(marks, p.loc.T("mode.mark.user"))
		}
		suffix := ""
		if len(marks) > 0 {
			suffix = "  · " + strings.Join(marks, " · ")
		}
		lines = append(lines, cursor+th.CardStyle(th.Plain()).Render(modes.Name(e))+th.CardStyle(th.Faint()).Render(suffix))
		// The box body is not truncated by the chrome, so clip here.
		if clip := w - 4; clip > 0 {
			if e.Broken != "" {
				lines = append(lines, th.CardStyle(th.Err()).Render("    ✗ "+truncateTo(e.Broken, clip)))
			} else if desc := modes.Description(e); desc != "" {
				lines = append(lines, th.CardStyle(th.Faint()).Render("    "+truncateTo(desc, clip)))
			}
		}
	}
	return lines
}

// truncateTo width-clips s to n cells with a tail ellipsis.
func truncateTo(s string, n int) string {
	if n < 1 {
		n = 1
	}
	if runewidth.StringWidth(s) <= n {
		return s
	}
	return runewidth.Truncate(s, n, "…")
}

func (p *modePicker) update(km tea.KeyMsg) (tea.Cmd, bool) {
	switch {
	case km.Type == tea.KeyUp:
		if p.cur > 0 {
			p.cur--
		}
	case km.Type == tea.KeyDown:
		if p.cur < len(p.presets)-1 {
			p.cur++
		}
	case km.Type == tea.KeyEnter:
		return nil, true
	case km.Type == tea.KeyEsc:
		return nil, true
	}
	return nil, false
}

// openModePicker loads the roster and opens the picker, marking the active
// session's current mode.
func (m *Model) openModePicker() tea.Cmd {
	id := m.activeID()
	if id == "" {
		m.addToast(core.Notice{Level: "info", Text: m.loc.T("mode.no.session")})
		return nil
	}
	pk := &modePicker{loading: true, loc: m.loc}
	m.openModal(pk)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		res, err := m.app.Presets(ctx)
		if err != nil {
			if p, ok := m.topModal().(*modePicker); ok {
				p.loading = false
				p.err = errText(err, "modes")
			}
			return dirtyMsg{}
		}
		return presetsLoadedMsg{presets: res.Presets}
	}
}

// applyModePick applies one mode pick to the session: a blank session
// recomposes in place (agentPreset.select); a session that has already
// run keeps its composition, so the pick stages for the next new session.
func (m *Model) applyModePick(ctx context.Context, id string, entry *protocol.AgentPresetEntry) error {
	if id == "" {
		return nil
	}
	snap := m.st.Get(id)
	if snap == nil {
		return nil
	}
	if entry.Id == snap.Summary.AgentPreset {
		m.stagedMode = ""
		return nil
	}
	if snap.Summary.Blank {
		m.stagedMode = ""
		got, err := m.app.SelectMode(ctx, id, entry.Id)
		if err == nil {
			m.st.Notify(core.Notice{Level: "ok", Text: m.loc.T("mode.applied", m.modeLabel(got))})
		}
		return err
	}
	m.stagedMode = entry.Id
	m.addToast(core.Notice{Level: "info", Text: m.loc.T("mode.staged", modes.Name(*entry))})
	return nil
}

// cmdSelectModeByName applies /mode <name>: the roster is fetched, the name
// resolved against it (modes.Resolve — shipped aliases like ptc/creator
// included), and the pick applied or staged per the blank-session rule.
func (m *Model) cmdSelectModeByName(name string) tea.Cmd {
	id := m.activeID()
	if id == "" {
		m.addToast(core.Notice{Level: "info", Text: m.loc.T("mode.no.session")})
		return nil
	}
	return m.runCmd("mode", func(ctx context.Context) error {
		res, err := m.app.Presets(ctx)
		if err != nil {
			return fmt.Errorf("roster: %w", err)
		}
		entry, problem := modes.Resolve(res.Presets, name)
		if entry == nil {
			m.st.Notify(core.Notice{Level: "warn", Text: m.loc.T("mode.problem", problem)})
			return nil
		}
		return m.applyModePick(ctx, id, entry)
	})
}

// consumeStagedMode hands back a staged mode pick for the next new
// session and clears it; "" starts the session on the deployment default.
func (m *Model) consumeStagedMode() string {
	p := m.stagedMode
	m.stagedMode = ""
	return p
}

package ui

import (
	"fmt"
	"strings"
	"time"

	"dsh-cli/internal/i18n"
	"dsh-cli/internal/usage"
	"dsh-cli/internal/version"

	tea "github.com/charmbracelet/bubbletea"
)

// statusModal is the /status popup: the host line plus the token-usage
// report in three sections — overview (all-time total, this month, this
// week, today), per workspace, per session. ←/→ walks the sections
// (1/2/3 jump, tab cycles), up/down walks the section's rows,
// esc/enter/q close. The numbers are the persistent ~/.dsh-cli
// statistics (usage.Recorder).
type statusModal struct {
	loc   *i18n.Locale
	sec   int // 0 overview · 1 workspaces · 2 sessions
	cur   int // cursor over the current section's rows
	rowsN int // last rendered section's row count (the update clamp)
}

const statusSections = 3

var _ modal = (*statusModal)(nil)

func newStatusModal(l *i18n.Locale) *statusModal { return &statusModal{loc: l} }

func (s *statusModal) title() string { return s.loc.T("status.title") }

func (s *statusModal) hint() string { return s.loc.T("status.hint") }

// report pulls the live statistics (nil-safe: a recorder-less model
// renders the "not recording" face).
func (s *statusModal) report(m *Model) *usage.Report {
	if m.usage == nil {
		return nil
	}
	return m.usage.Report(time.Now())
}

// view renders the section tab line plus the current section's windowed
// rows (hgt <= 0: the full section, test probes).
func (s *statusModal) view(m *Model, w, hgt int) []string {
	th := m.th
	w = maxInt(1, w)

	// Tab line (body row 0): the three sections, the active one in accent.
	tabs := []string{s.loc.T("status.tab.overview"), s.loc.T("status.tab.workspaces"), s.loc.T("status.tab.sessions")}
	var tab string
	for i, name := range tabs {
		cell := fmt.Sprintf("[%d] %s", i+1, name)
		if i == s.sec {
			tab += th.CardStyle(th.Accent()).Render(cell + " ")
		} else {
			tab += th.CardStyle(th.Faint()).Render(cell + " ")
		}
	}
	lines := []string{truncDisplay(tab, w)}

	rows := s.sectionRows(m, w)
	s.rowsN = len(rows)
	if hgt <= 0 {
		return append(lines, rows...)
	}
	budget := hgt - 1
	if budget < 1 {
		budget = 1
	}
	if s.cur > len(rows)-1 {
		s.cur = maxInt(0, len(rows)-1)
	}
	start := 0
	if s.cur > start+budget-1 {
		start = s.cur - budget + 1
	}
	for i := start; i < len(rows) && i < start+budget; i++ {
		lines = append(lines, rows[i])
	}
	return lines
}

func (s *statusModal) sectionRows(m *Model, w int) []string {
	switch s.sec {
	case 1:
		return s.workspaceRows(m, w)
	case 2:
		return s.sessionRows(m, w)
	default:
		return s.overviewRows(m, w)
	}
}

// overviewRows: the connection line (the old /status toast, now in the
// window) then the four fixed windows.
func (s *statusModal) overviewRows(m *Model, w int) []string {
	th := m.th
	var lines []string
	if h := m.st.Host(); h == nil {
		lines = append(lines, th.CardStyle(th.Faint()).Render(s.loc.T("toast.host.connecting")))
	} else {
		text := s.loc.T("toast.status", version.Version, h.Version, len(m.st.Roster()), h.Cwd)
		lines = append(lines, th.CardStyle(th.Faint()).Render(truncDisplay(text, w)))
	}
	rep := s.report(m)
	if rep == nil {
		lines = append(lines, "")
		lines = append(lines, th.CardStyle(th.Faint()).Render(s.loc.T("status.no.recorder")))
		return lines
	}
	lines = append(lines, "")
	metrics := []struct {
		key string
		t   usage.Totals
	}{
		{"status.ov.total", rep.Total},
		{"status.ov.month", rep.Month},
		{"status.ov.week", rep.Week},
		{"status.ov.day", rep.Day},
	}
	for _, mt := range metrics {
		lines = append(lines, s.metricRow(th, w, s.loc.T(mt.key), mt.t))
	}
	if rep.Total.Count() == 0 {
		lines = append(lines, th.CardStyle(th.Faint()).Render(s.loc.T("status.ov.none")))
	}
	return lines
}

// metricRow renders one overview metric: the label left, the "in / out"
// value right-aligned in accent.
func (s *statusModal) metricRow(th *Theme, w int, label string, t usage.Totals) string {
	val := s.tokText(t)
	vw := plainWidth(val)
	lw := w - vw - 2
	if lw < 4 {
		lw = 4
	}
	lab := truncDisplay(label, lw)
	gap := strings.Repeat(" ", maxInt(0, w-vw-plainWidth(lab)))
	return th.CardStyle(th.Plain()).Render(lab) + th.Card().Render(gap) + th.CardStyle(th.Accent()).Render(val)
}

// workspaceRows: one row per workspace, busiest first.
func (s *statusModal) workspaceRows(m *Model, w int) []string {
	th := m.th
	rep := s.report(m)
	if rep == nil || len(rep.Workspaces) == 0 {
		return []string{th.CardStyle(th.Faint()).Render(s.loc.T("status.ws.none"))}
	}
	known := map[string]string{}
	for _, v := range m.st.Workspaces() {
		name := v.Title
		if name == "" {
			name = v.Path
		}
		if name != "" {
			known[v.WorkspaceId] = name
		}
	}
	var lines []string
	for i, wu := range rep.Workspaces {
		label := wu.Key
		if name, ok := known[wu.Key]; ok {
			label = name
		}
		val := s.tokText(wu.T)
		if i == s.cur {
			lines = append(lines, th.SessBand().Render(statusPad(th, s.nameValueText(w, label, val), maxInt(1, w-2))))
			continue
		}
		lines = append(lines, s.nameValueRow(th, w, label, val))
	}
	return lines
}

// sessionRows: one row per session, most recent first, the cursor band on
// the walked row.
func (s *statusModal) sessionRows(m *Model, w int) []string {
	th := m.th
	rep := s.report(m)
	if rep == nil || len(rep.Sessions) == 0 {
		return []string{th.CardStyle(th.Faint()).Render(s.loc.T("status.sess.none"))}
	}
	titles := map[string]string{}
	for _, r := range m.st.Roster() {
		titles[r.Id] = r.Title
	}
	active := m.activeID()
	var lines []string
	for i, su := range rep.Sessions {
		label := titles[su.ID]
		if label == "" {
			label = su.ID
			if len(label) > 8 {
				label = label[:8]
			}
		}
		if su.ID == active {
			label += s.loc.T("status.sess.current")
		}
		val := s.tokText(su.T)
		if i == s.cur {
			lines = append(lines, th.SessBand().Render(statusPad(th, s.nameValueText(w, label, val), maxInt(1, w-2))))
			continue
		}
		lines = append(lines, s.nameValueRow(th, w, label, val))
	}
	return lines
}

// tokText formats one counter set the way the bottom line does.
func (s *statusModal) tokText(t usage.Totals) string {
	return s.loc.T("status.ov.tok", compactInt(int64(t.In)), compactInt(int64(t.Out)))
}

// nameValueText is the plain "label … value" layout (the cursor band
// renders it on the accent surface).
func (s *statusModal) nameValueText(w int, label, value string) string {
	vw := plainWidth(value)
	lw := w - vw - 2
	if lw < 2 {
		lw = 2
	}
	lab := truncDisplay(label, lw)
	gap := strings.Repeat(" ", maxInt(0, w-vw-plainWidth(lab)))
	return lab + gap + value
}

// nameValueRow is the same layout styled: the label in subtle ink, the
// value in accent, on the card surface.
func (s *statusModal) nameValueRow(th *Theme, w int, label, value string) string {
	vw := plainWidth(value)
	lw := w - vw - 2
	if lw < 2 {
		lw = 2
	}
	lab := truncDisplay(label, lw)
	gap := strings.Repeat(" ", maxInt(0, w-vw-plainWidth(lab)))
	return th.CardStyle(th.Subtle()).Render(lab) + th.Card().Render(gap) + th.CardStyle(th.Accent()).Render(value)
}

// statusPad extends s to width on the card surface (padding cells).
func statusPad(th *Theme, s string, w int) string {
	if p := plainWidth(s); p < w {
		s += th.Card().Render(strings.Repeat(" ", w-p))
	}
	return s
}

// update walks the popup (it owns the keyboard while open): up/down the
// section's rows, ←/→ / tab the sections, 1-3 a direct jump; the closing
// chords (esc / enter / q) are consumed here and closed by
// handleModalKey.
func (s *statusModal) update(km tea.KeyMsg) (tea.Cmd, bool) {
	switch {
	case km.Type == tea.KeyUp:
		// The overview is a fixed summary (no rows to walk); the two
		// list sections own the cursor.
		if s.sec != 0 && s.cur > 0 {
			s.cur--
		}
		return nil, true
	case km.Type == tea.KeyDown:
		if s.sec != 0 && s.cur < s.rowsN-1 {
			s.cur++
		}
		return nil, true
	// ←/→ walk the sections (replacing the old [ ] chords). While the
	// popup is open it owns the keyboard (handleKey absorbs unclaimed
	// keys), so the arrows never reach the input line; closing the
	// popup restores the arrows' normal caret-binding on the input.
	case km.Type == tea.KeyLeft:
		s.sec = (s.sec + statusSections - 1) % statusSections
		s.cur = 0
		return nil, true
	case km.Type == tea.KeyRight:
		s.sec = (s.sec + 1) % statusSections
		s.cur = 0
		return nil, true
	case km.Type == tea.KeyTab:
		s.sec = (s.sec + 1) % statusSections
		s.cur = 0
		return nil, true
	case km.Type == tea.KeyRunes && (km.String() == "1" || km.String() == "2" || km.String() == "3"):
		s.sec = int(km.String()[0] - '1')
		s.cur = 0
		return nil, true
	case km.Type == tea.KeyEsc || km.Type == tea.KeyEnter ||
		(km.Type == tea.KeyRunes && km.String() == "q"):
		return nil, true
	}
	return nil, false
}

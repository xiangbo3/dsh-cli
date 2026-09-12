// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"dsh-cli/internal/config"
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
	m     *Model // the opener: price edits update its prices, the report's source
	loc   *i18n.Locale
	sec   int // 0 overview · 1 workspaces · 2 sessions · 3 billing
	cur   int // cursor over the current section's rows (the billing's active field)
	rowsN int // last rendered section's row count (the update clamp)
	// priceInEd/priceOutEd edit the per-million-tokens input / output
	// costs (the billing section; applied live as typed).
	priceInEd  lineEdit
	priceOutEd lineEdit
}

const statusSections = 4

var _ modal = (*statusModal)(nil)

func newStatusModal(m *Model) *statusModal {
	s := &statusModal{m: m, loc: m.loc}
	s.priceInEd.val = priceText(m.tokenInPrice)
	s.priceInEd.cur = len(s.priceInEd.val)
	s.priceOutEd.val = priceText(m.tokenOutPrice)
	s.priceOutEd.cur = len(s.priceOutEd.val)
	return s
}

// priceText is a price's field text ("" at zero: an unset price shows an
// empty field, not "0").
func priceText(v float64) []rune {
	if v == 0 {
		return nil
	}
	return []rune(strconv.FormatFloat(v, 'f', -1, 64))
}

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
	tabs := []string{s.loc.T("status.tab.overview"), s.loc.T("status.tab.workspaces"), s.loc.T("status.tab.sessions"), s.loc.T("status.tab.billing")}
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
	case 3:
		return s.billingRows(m, w)
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
		lines = append(lines, s.metricRow(th, w, s.loc.T(mt.key), mt.t, m.tokenInPrice, m.tokenOutPrice))
	}
	if rep.Total.Count() == 0 {
		lines = append(lines, th.CardStyle(th.Faint()).Render(s.loc.T("status.ov.none")))
	}
	return lines
}

// metricRow renders one overview metric: the label left, the "in / out"
// value (plus the cost at the set prices) right-aligned in accent.
func (s *statusModal) metricRow(th *Theme, w int, label string, t usage.Totals, inPrice, outPrice float64) string {
	val := s.tokText(t, inPrice, outPrice)
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
		val := s.tokText(wu.T, m.tokenInPrice, m.tokenOutPrice)
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
		val := s.tokText(su.T, m.tokenInPrice, m.tokenOutPrice)
		if i == s.cur {
			lines = append(lines, th.SessBand().Render(statusPad(th, s.nameValueText(w, label, val), maxInt(1, w-2))))
			continue
		}
		lines = append(lines, s.nameValueRow(th, w, label, val))
	}
	return lines
}

// billingRows: the per-million-tokens price fields (the whole section
// is the setting; ↑↓ walks them) plus the key hint. The active field
// carries the live caret; the prices apply as they are typed.
func (s *statusModal) billingRows(m *Model, w int) []string {
	th := m.th
	rows := []struct {
		key string
		ed  *lineEdit
	}{
		{"status.price.in", &s.priceInEd},
		{"status.price.out", &s.priceOutEd},
	}
	var lines []string
	for i, r := range rows {
		focused := i == s.cur
		label := s.loc.T(r.key)
		lw := w - 12
		if lw < 4 {
			lw = 4
		}
		field := cardEditLine(th, focused, r.ed)
		lines = append(lines, th.CardStyle(th.Plain()).Render(truncDisplay(label, lw)+"  "+field))
	}
	lines = append(lines, th.CardStyle(th.Faint()).Render(s.loc.T("status.price.hint")))
	return lines
}

// applyPrices commits the just-edited field live: a blank field clears
// its price, a valid non-negative number takes effect at once (no
// confirm key), and an in-between invalid state ("1.2.") simply does
// not apply yet. The model is the live source; config keeps the value
// across runs.
func (s *statusModal) applyPrices(ed *lineEdit) {
	if v, ok := priceValue(ed.string()); ok {
		if ed == &s.priceInEd {
			s.m.tokenInPrice = v
		} else {
			s.m.tokenOutPrice = v
		}
	}
	cfg := config.Load()
	cfg.TokenInPrice = s.m.tokenInPrice
	cfg.TokenOutPrice = s.m.tokenOutPrice
	_ = config.Save(cfg)
}

// priceValue parses a field's text as a price: blank is 0 (clearing),
// otherwise a non-negative number; ok=false marks the in-between
// invalid states (the last applied value stays). A trailing dot is a
// number mid-edit (typing or backspacing past "2."): it applies once
// the next digit lands.
func priceValue(raw string) (float64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, true
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || v < 0 {
		return 0, false
	}
	if strings.HasSuffix(raw, ".") {
		return 0, false
	}
	return v, true
}

// tokText formats one counter set the way the bottom line does, with the
// cost in parens when prices are set.
func (s *statusModal) tokText(t usage.Totals, inPrice, outPrice float64) string {
	txt := s.loc.T("status.ov.tok", compactInt(int64(t.In)), compactInt(int64(t.Out)))
	if c := costText(t, inPrice, outPrice); c != "" {
		txt += s.loc.T("status.cost", c)
	}
	return txt
}

// costText is one counter set's cost at the per-million input / output
// prices ("" when neither is set): in*inPrice/1M + out*outPrice/1M;
// whole at 100+, one decimal at 1+, two below that.
func costText(t usage.Totals, inPrice, outPrice float64) string {
	if inPrice <= 0 && outPrice <= 0 {
		return ""
	}
	cost := float64(t.In)*inPrice/1e6 + float64(t.Out)*outPrice/1e6
	if cost <= 0 {
		return ""
	}
	switch {
	case cost >= 100:
		return strconv.FormatFloat(cost, 'f', 0, 64)
	case cost >= 1:
		return strconv.FormatFloat(cost, 'f', 1, 64)
	default:
		return strconv.FormatFloat(cost, 'f', 2, 64)
	}
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
// section's rows, ←/→ / tab the sections, 1-4 a direct jump; the closing
// chords (esc / enter / q) are consumed here and closed by
// handleModalKey. In the billing section the price fields own the
// keyboard instead: ↑↓ picks the field, and every valid edit applies
// the price at once (enter then closes like anywhere else).
func (s *statusModal) update(km tea.KeyMsg) (tea.Cmd, bool) {
	if s.sec == 3 {
		ed := &s.priceInEd
		if s.cur == 1 {
			ed = &s.priceOutEd
		}
		switch km.Type {
		case tea.KeyUp:
			if s.cur > 0 {
				s.cur = 0
			}
			return nil, true
		case tea.KeyDown:
			if s.cur < 1 {
				s.cur = 1
			}
			return nil, true
		case tea.KeyBackspace:
			ed.delBack()
			s.applyPrices(ed)
			return nil, true
		case tea.KeyHome, tea.KeyEnd, tea.KeyCtrlA, tea.KeyCtrlE,
			tea.KeyCtrlB, tea.KeyCtrlF, tea.KeyCtrlD, tea.KeyCtrlU:
			ed.handleKey(km)
			s.applyPrices(ed)
			return nil, true
		case tea.KeyRunes:
			r := km.Runes[0]
			if r == 'q' {
				return nil, true
			}
			if (r >= '0' && r <= '9') || r == '.' {
				ed.insert(r)
				s.applyPrices(ed)
			}
			return nil, true
		}
	}
	switch {
	case km.Type == tea.KeyUp:
		// The overview is a fixed summary (no rows to walk); the two
		// list sections own the cursor.
		if (s.sec == 1 || s.sec == 2) && s.cur > 0 {
			s.cur--
		}
		return nil, true
	case km.Type == tea.KeyDown:
		if (s.sec == 1 || s.sec == 2) && s.cur < s.rowsN-1 {
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
	case km.Type == tea.KeyRunes && (km.String() == "1" || km.String() == "2" || km.String() == "3" || km.String() == "4"):
		s.sec = int(km.String()[0] - '1')
		s.cur = 0
		return nil, true
	case km.Type == tea.KeyEsc || km.Type == tea.KeyEnter ||
		(km.Type == tea.KeyRunes && km.String() == "q"):
		return nil, true
	}
	return nil, false
}

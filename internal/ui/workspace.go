// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"context"
	"path"
	"strings"
	"time"

	"dsh-cli/internal/core"
	"dsh-cli/internal/i18n"
	"dsh-cli/internal/protocol"

	"github.com/charmbracelet/bubbletea"
)

// workspaceModal is the workspace browser: the registry in durable display
// order with add / rename / delete actions, and enter to open (switch to) a
// workspace's blank session — the web sidebar's workspace browser plus the
// hero-chip switch in one modal.
type workspaceModal struct {
	st  *core.Store
	loc *i18n.Locale
	// delTarget captures the row a delete confirmation refers to (the cursor
	// may move while confirming).
	delTarget string
	delLabel  string
	cur       int
	mode      wsMode
	input     lineEdit
	err       string
}

type wsMode int

const (
	wsPick wsMode = iota
	wsAdd
	wsRename
	wsDelete
)

func (w *workspaceModal) title() string { return w.loc.T("ws.title") }

func (w *workspaceModal) hint() string {
	return w.loc.T("ws.hint")
}

func (w *workspaceModal) list() []protocol.WorkspaceView {
	return w.st.Workspaces()
}

func (w *workspaceModal) pickCount() int {
	if w.mode == wsPick {
		return len(w.list()) + 1 // + the "+ add" row
	}
	return len(w.list())
}

func (w *workspaceModal) clamp() {
	if w.cur < 0 {
		w.cur = 0
	}
	if w.cur >= w.pickCount() {
		w.cur = w.pickCount() - 1
	}
}

func (w *workspaceModal) current(offset int) *protocol.WorkspaceView {
	ws := w.list()
	i := w.cur + offset
	if i < 0 || i >= len(ws) {
		return nil
	}
	c := ws[i]
	return &c
}

func (w *workspaceModal) view(m *Model, wdt, h int) []string {
	w.clamp()
	th := m.th
	g := m.th.Glyph
	ws := w.list()
	lines := make([]string, 0, len(ws)+5)
	for i, wkv := range ws {
		var cursor string
		if i == w.cur {
			cursor = th.CardStyle(th.Accent()).Render(g.Caret + " ")
		} else {
			cursor = th.Card().Render("  " + g.Dot + " ")
		}
		meta := w.loc.T("ws.row.meta", wkv.Title,
			len(wkv.SessionIds), plural(int32(len(wkv.SessionIds))))
		if wkv.Path != "" {
			meta += "  " + wkv.Path
		}
		lines = append(lines, cursor+th.CardStyle(th.Plain()).Render(truncDisplay(meta, wdt-4)))
	}
	if w.mode == wsPick {
		add := th.Card().Render("  " + g.Dot + " " + w.loc.T("ws.add.row"))
		if w.cur == len(ws) {
			add = th.CardStyle(th.Accent()).Render(g.Caret + " " + w.loc.T("ws.add.row"))
		}
		lines = append(lines, add)
	}
	switch w.mode {
	case wsAdd:
		lines = append(lines, "",
			th.CardStyle(th.Subtle()).Render(w.loc.T("ws.path.pref"))+cardEditLine(th, true, &w.input))
	case wsRename:
		lines = append(lines, "",
			th.CardStyle(th.Subtle()).Render(w.loc.T("ws.title.pref"))+cardEditLine(th, true, &w.input))
	case wsDelete:
		lines = append(lines, "", w.loc.T("ws.delete.note", w.delLabel))
	}
	if w.err != "" {
		lines = append(lines, th.CardStyle(th.Warn()).Render("  "+w.err))
	}
	return lines
}

func plural(n int32) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// mouseFields locates the add/rename path or title field (one blank row
// below the row list, which is what the view draws first).
func (w *workspaceModal) mouseFields(m *Model) []editField {
	if w.mode != wsAdd && w.mode != wsRename {
		return nil
	}
	prefix := m.th.CardStyle(m.th.Subtle()).Render("path >  ")
	if w.mode == wsRename {
		prefix = m.th.CardStyle(m.th.Subtle()).Render("title > ")
	}
	return []editField{{e: &w.input, row: len(w.list()) + 1, prefix: prefix}}
}

func (w *workspaceModal) paste(text string) bool {
	if w.mode != wsAdd && w.mode != wsRename {
		return false
	}
	return w.input.paste(text)
}

// update owns the modal's keyboard: row movement, the add/rename input
// lines and the delete confirmation. Enter (and y) return handled without a
// cmd; the type switch in handleModalKey performs the wire action.
func (w *workspaceModal) update(km tea.KeyMsg) (tea.Cmd, bool) {
	switch w.mode {
	case wsAdd, wsRename:
		switch {
		case km.Type == tea.KeyEsc:
			w.mode = wsPick
			w.input.reset()
			w.err = ""
			return nil, true
		case km.Type == tea.KeyEnter:
			if strings.TrimSpace(w.input.string()) == "" {
				w.err = w.loc.T("ws.err.empty")
				return nil, true
			}
			return nil, true
		default:
			// The path/title field edits like the main input box: caret
			// plus the ctrl+a/e/b/f/d/u bindings.
			if w.input.handleKey(km) {
				w.err = ""
				return nil, true
			}
		}
		return nil, false
	case wsDelete:
		switch {
		case km.Type == tea.KeyEsc:
			w.mode = wsPick
			w.err = ""
			return nil, true
		case km.String() == "y" || km.Type == tea.KeyEnter:
			return nil, true
		case km.Type == tea.KeyUp || km.Type == tea.KeyDown:
			w.mode = wsPick
			w.err = ""
			return nil, true
		}
		return nil, false
	default:
		switch {
		case km.Type == tea.KeyUp:
			w.cur = (w.cur - 1 + w.pickCount()) % w.pickCount()
			w.err = ""
			return nil, true
		case km.Type == tea.KeyDown:
			w.cur = (w.cur + 1) % w.pickCount()
			w.err = ""
			return nil, true
		case km.Type == tea.KeyEnter:
			return nil, true
		case km.String() == "a":
			w.mode = wsAdd
			w.input.reset()
			w.err = ""
			return nil, true
		case km.String() == "r":
			if ws := w.current(0); ws != nil {
				w.mode = wsRename
				w.input = lineEdit{val: []rune(ws.Title), cur: len(ws.Title)}
				w.err = ""
				return nil, true
			}
			return nil, false // add row: let the key fall through
		case km.String() == "d":
			if ws := w.current(0); ws != nil {
				w.delTarget, w.delLabel = ws.WorkspaceId, ws.Title
				w.mode = wsDelete
				w.err = ""
				return nil, true
			}
			return nil, false // add row: let the key fall through
		case km.Type == tea.KeyEsc:
			return nil, true
		}
	}
	return nil, false
}

func contextWithTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

// workspacePathLabel is the chip/row label for a workspace (title, falling
// back to the path basename, then the raw path).
func workspacePathLabel(ws *protocol.WorkspaceView) string {
	if ws == nil {
		return ""
	}
	if ws.Title != "" {
		return ws.Title
	}
	if base := path.Base(ws.Path); base != "" && base != "/" && base != "." {
		return base
	}
	return ws.Path
}

// resolveWorkspaceRef resolves a /workspace argument. Accepted: an existing
// workspace title (exact, then case-insensitive, then a unique prefix), or a
// directory path — registered (returns the workspace) or not (returns the
// path to create). Failures carry a user-facing reason.
func (m *Model) resolveWorkspaceRef(name string) (ws *protocol.WorkspaceView, dir string, problem string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, "", m.loc.T("ws.err.name")
	}
	if strings.HasPrefix(name, "/") || strings.HasPrefix(name, "~") || strings.Contains(name, "/") {
		if w := m.st.WorkspaceForPath(name); w != nil {
			return w, "", ""
		}
		return nil, name, ""
	}
	match := func(pred func(t string) bool) ([]protocol.WorkspaceView, bool) {
		var hits []protocol.WorkspaceView
		for _, wkv := range m.st.Workspaces() {
			if pred(wkv.Title) {
				hits = append(hits, wkv)
			}
		}
		return hits, len(hits) == 1
	}
	list := func(pred func(t string) bool) string {
		var out []string
		for _, wkv := range m.st.Workspaces() {
			if pred(wkv.Title) {
				out = append(out, wkv.Title)
			}
		}
		return strings.Join(out, ", ")
	}
	prefix := strings.ToLower(name)
	if hits, ok := match(func(t string) bool { return t == name }); ok {
		return &hits[0], "", ""
	}
	if hits, ok := match(func(t string) bool { return strings.EqualFold(t, name) }); ok {
		return &hits[0], "", ""
	}
	if hits, ok := match(func(t string) bool {
		return strings.HasPrefix(strings.ToLower(t), prefix)
	}); ok {
		return &hits[0], "", ""
	}
	if hits := list(func(t string) bool {
		return strings.HasPrefix(strings.ToLower(t), prefix)
	}); hits != "" {
		return nil, "", m.loc.T("ws.err.ambiguous", name, hits)
	}
	return nil, "", m.loc.T("ws.err.notfound", name)
}

// openWorkspace opens the browser (ctrl+w / bare /workspace), preselecting
// the active session's workspace.
func (m *Model) openWorkspace() tea.Cmd {
	if !m.st.WorkspacesBaselined() {
		m.addToast(core.Notice{Level: "info", Text: m.loc.T("ws.pending")})
	}
	w := &workspaceModal{st: m.st, loc: m.loc, cur: 0, mode: wsPick}
	if id := m.activeID(); id != "" {
		if ws := m.st.WorkspaceForSession(id); ws != nil {
			for i, wkv := range m.st.Workspaces() {
				if wkv.WorkspaceId == ws.WorkspaceId {
					w.cur = i
				}
			}
		}
	}
	m.openModal(w)
	return nil
}

// workspacePerform dispatches the type-switch half of the modal: the wire
// action behind a handled enter/y.
func (m *Model) workspacePerform(mod *workspaceModal) (tea.Cmd, bool) {
	switch mod.mode {
	case wsPick:
		wsList := m.st.Workspaces()
		if mod.cur < len(wsList) {
			id := wsList[mod.cur].WorkspaceId
			m.closeModal()
			return m.cmdWorkspaceSwitch(id), true
		}
		// The "+ add" row was selected: drop into add mode instead.
		mod.mode = wsAdd
		mod.input.reset()
		return nil, true
	case wsAdd:
		val := strings.TrimSpace(mod.input.string())
		if val == "" {
			return nil, true
		}
		m.closeModal()
		return m.cmdWorkspaceAdd(val), true
	case wsRename:
		ws := mod.current(0)
		if ws == nil {
			return nil, true
		}
		val := strings.TrimSpace(mod.input.string())
		if val == "" || val == ws.Title {
			mod.err = ""
			return nil, true
		}
		id := ws.WorkspaceId
		m.closeModal()
		return m.cmdWorkspaceRename(id, val), true
	case wsDelete:
		ws := m.st.WorkspaceByID(mod.delTarget)
		if ws == nil {
			return nil, true
		}
		m.closeModal()
		return m.cmdWorkspaceDelete(ws.WorkspaceId), true
	}
	return nil, true
}

// cmdWorkspaceSwitch opens (or reuses) the workspace's blank session and
// activates it — the web hero-chip workspace switch.
func (m *Model) cmdWorkspaceSwitch(workspaceID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := contextWithTimeout()
		defer cancel()
		sid, err := m.app.ConnectWorkspace(ctx, workspaceID)
		if err != nil {
			m.st.Notify(core.Notice{Level: errLevel(err), Text: errText(err, "workspace"), Bell: true})
			return dirtyMsg{}
		}
		m.st.SetActive(sid)
		m.resetTrans()
		m.follow = true
		m.scroll = 0
		m.inp.clear()
		m.st.Notify(core.Notice{Level: "ok", Text: m.loc.T("ws.switched", workspacePathLabel(m.st.WorkspaceByID(workspaceID)))})
		return createMsg{id: sid}
	}
}

// cmdWorkspaceAdd registers an existing directory (idempotent) and opens it.
func (m *Model) cmdWorkspaceAdd(dirPath string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := contextWithTimeout()
		defer cancel()
		ws, created, err := m.app.WorkspaceCreate(ctx, dirPath)
		if err != nil {
			m.st.Notify(core.Notice{Level: errLevel(err), Text: errText(err, "workspace add"), Bell: true})
			return dirtyMsg{}
		}
		if ws == nil {
			return dirtyMsg{}
		}
		if created {
			m.st.Notify(core.Notice{Level: "ok", Text: m.loc.T("ws.added", workspacePathLabel(ws))})
		} else {
			m.st.Notify(core.Notice{Level: "info", Text: m.loc.T("ws.exists", workspacePathLabel(ws))})
		}
		sid, err := m.app.ConnectWorkspace(ctx, ws.WorkspaceId)
		if err != nil {
			m.st.Notify(core.Notice{Level: errLevel(err), Text: errText(err, "workspace"), Bell: true})
			return dirtyMsg{}
		}
		m.st.SetActive(sid)
		m.resetTrans()
		m.follow = true
		m.scroll = 0
		m.inp.clear()
		return createMsg{id: sid}
	}
}

// cmdWorkspaceRename applies a new display title.
func (m *Model) cmdWorkspaceRename(workspaceID, title string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := contextWithTimeout()
		defer cancel()
		ws, err := m.app.WorkspaceRename(ctx, workspaceID, title)
		if err != nil {
			m.st.Notify(core.Notice{Level: errLevel(err), Text: errText(err, "workspace rename"), Bell: true})
			return dirtyMsg{}
		}
		if ws != nil {
			m.st.Notify(core.Notice{Level: "ok", Text: m.loc.T("ws.renamed", workspacePathLabel(ws))})
		}
		return dirtyMsg{}
	}
}

// cmdWorkspaceDelete deregisters one workspace (sessions stay, ungrouped).
func (m *Model) cmdWorkspaceDelete(workspaceID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := contextWithTimeout()
		defer cancel()
		label := workspacePathLabel(m.st.WorkspaceByID(workspaceID))
		if err := m.app.WorkspaceDelete(ctx, workspaceID); err != nil {
			m.st.Notify(core.Notice{Level: errLevel(err), Text: errText(err, "workspace delete"), Bell: true})
			return dirtyMsg{}
		}
		m.st.Notify(core.Notice{Level: "info", Text: m.loc.T("ws.deleted", label)})
		return dirtyMsg{}
	}
}

// cmdWorkspaceArchive files the active session in the archive set (the web
// grouping surfaces hide it; the log and accounting slot remain).
func (m *Model) cmdWorkspaceArchive() tea.Cmd {
	id := m.activeID()
	return m.runCmd("archive", func(ctx context.Context) error {
		if err := m.app.WorkspaceArchiveSession(ctx, id); err != nil {
			return err
		}
		m.st.SetActive("")
		m.resetTrans()
		m.follow = true
		m.scroll = 0
		m.inp.clear()
		m.st.Notify(core.Notice{Level: "ok", Text: m.loc.T("ws.archived")})
		return nil
	})
}

// localSlashWorkspace handles /workspace <arg>: bare opens the browser,
// "archive" files the active session, anything else resolves as a title or
// path (creating the path when unregistered) and switches to it.
func (m *Model) localSlashWorkspace(rest string) tea.Cmd {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return m.openWorkspace()
	}
	fields := strings.Fields(rest)
	if fields[0] == "archive" && len(fields) == 1 {
		if id := m.activeID(); id == "" {
			m.addToast(core.Notice{Level: "info", Text: m.loc.T("ws.no.session")})
			return nil
		}
		return m.cmdWorkspaceArchive()
	}
	ws, dirPath, problem := m.resolveWorkspaceRef(rest)
	if problem != "" {
		m.addToast(core.Notice{Level: "warn", Text: problem})
		return nil
	}
	if ws != nil {
		return m.cmdWorkspaceSwitch(ws.WorkspaceId)
	}
	return m.cmdWorkspaceAdd(dirPath)
}

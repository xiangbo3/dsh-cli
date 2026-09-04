// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"dsh-cli/internal/app"
	"dsh-cli/internal/config"
	"dsh-cli/internal/core"
	"dsh-cli/internal/i18n"
	"dsh-cli/internal/modes"
	"dsh-cli/internal/protocol"
	"dsh-cli/internal/textutil"
	"dsh-cli/internal/usage"

	"github.com/charmbracelet/bubbletea"
	"github.com/muesli/termenv"
)

// Model is the root of the TUI. It owns layout state, the modal stack,
// toasts and the render cache; all session state lives in the core store.
type Model struct {
	app  *app.App
	st   *core.Store
	prog *tea.Program
	th   *Theme

	// loc is the active UI language (model-goroutine face); locRef is
	// the lock-free view for worker goroutines — the store's turn-end
	// toast formatter reads it without holding the model.
	loc    *i18n.Locale
	locRef atomic.Pointer[i18n.Locale]

	// Markdown pipelines per wrap width (LRU, see mdFor): a resize wobble
	// must not rebuild the goldmark/glamour pipeline on every frame.
	mds     map[string]*mdRenderer
	mdOrder []string

	now time.Time

	W int
	H int

	trans  *transCache
	follow bool
	// followBase: transcript line count when follow disarmed; the "N new"
	// pill counts lines landed since (transFor pins the owning session).
	followBase int
	transFor   string
	// termTitle caches the last OSC 0 title emitted (no re-write unchanged).
	termTitle string
	// turnEndSeen: last-seen turn-end seq per session; flashEnds marks
	// roster rows whose turn just ended elsewhere for a one-shot flash.
	turnEndSeen map[string]int64
	flashEnds   map[string]time.Time
	sel         textSel // transcript text pick (mouse drag → clipboard; crush-style, see select.go)
	// band SGR state of the pick highlight, probed once per theme.
	band       sgrState
	bandTheme  *Theme
	pressCount int // multi-click ladder (crush: press/word/line)
	// Origin of the transcript pane in the last rendered frame
	// (screen cells); -1 x while a splash/mini frame has no pane. Mouse
	// events are mapped through the real painted position, not a guess.
	transX int
	transY int
	// Origin + height of the input bar in the last rendered frame
	// (screen cells); inpY -1 while a splash/mini frame has no bar. The
	// bar spans the full width from x=0 (the row below the second rule,
	// above the status line).
	inpY int
	inpH int
	// Live input drags: left button held over the main bar (inpDrag) or
	// a modal edit field (dragEdit); motion extends that field's pick.
	inpDrag  bool
	dragEdit *lineEdit
	// Multi-click tracking (crush's pattern): presses on the same cell
	// inside the window count up: 1 anchor, 2 word, 3 line.
	lastPressT    time.Time
	lastPressCell [2]int
	// Idle-frame short-circuit bookkeeping: transDirty is set by anything
	// that invalidates the cached rows without a store mutation (resize,
	// verbose toggle, cache reset); lastTransRev is the store rev of the
	// last transcript re-sync.
	transDirty     bool
	lastTransRev   uint64
	lastTransWidth int
	scroll         int // line offset from the top; clamped to [0, max(0,total-vh)]
	transH         int // transcript pane height measured in the last frame (0: unmeasured)

	sideVisible bool
	// sideSearch is the session window's search line: the always-focussed
	// edit field of the centered popup (sideVisible).
	sideSearch lineEdit
	// sideCursorID is the session row the popup cursor rests on: the arrows
	// walk it (a move, not a pick), space commits it as the active session
	// (the select) and enter confirms (open it and close). It starts on the
	// active session and follows the live search filter; an empty/stale id
	// falls back to the active session.
	sideCursorID string
	dockVisible  bool
	dockTab      int
	subDockCur   int // subs-tab cursor over the cached child catalog
	verbose      bool
	// expandedTool: the tool card whose result is inlined (x/space peek),
	// independent of the global verbose flag.
	expandedTool *core.ToolBlock
	subFetching  bool // one child-catalog fetch in flight

	inp        *inputLine
	mergedCmds []slashCmd

	// skillsFetching guards the async skill-catalog fetch (set and cleared
	// only on the model goroutine; the worker only produces a message).
	skillsFetching bool

	// themeProbe counts ticks since the last terminal re-query; the
	// themeProbing/osc* fields state an in-flight re-query, whose OSC
	// 10/11 answer rides back over the key reader (ingestThemeReply).
	// All touched on the model goroutine only.
	themeProbe   int
	themeProbing bool
	oscBG        termenv.Color
	oscFG        termenv.Color
	oscBuf       string // report payload being reassembled from key events
	oscTail      bool   // the answer's terminator event (ST backslash / BEL) is still due
	oscTailEsc   bool   // the ST terminator's ESC crossed a read (the reader emits it as a plain Escape; its backslash is still due)
	oscDeadline  time.Time

	mods []modal
	// openedInter records which parked answerable frames already had a
	// modal opened (auto-open fires once per rpcId; esc-closed frames
	// stay parked and reopen via ctrl+i).
	openedInter map[string]bool
	toasts      []toast

	// hostDownSince / hostDownHinted implement the "start the server"
	// hint: one toast per outage, fired hostDownGrace after the
	// downlink went dark; both reset when the connection recovers so
	// the next outage hints again.
	hostDownSince  time.Time
	hostDownHinted bool

	// usage is the persistent token-usage statistics the /status popup
	// renders (nil when the app attached no recorder: the popup shows
	// its "not recording" face).
	usage *usage.Recorder

	booted bool
	// bootWsChecked closes the boot auto workspace switch: once the
	// resumed session's workspace has resolved (to a workspace, or to
	// none for a session the registry does not own), later dirty pulses
	// stop re-running the check.
	bootWsChecked bool
	quitting      bool
	bell          bool
	flashWindow   time.Time

	// splashStart marks the running boot animation (zero: not running);
	// splashOff disables it (headless key tests).
	splashStart time.Time
	splashOff   bool

	// pendingPrompt / pendingCycle are actions parked while a session is
	// being created; createMsg continues them in the new session.
	pendingPrompt string
	pendingCycle  bool

	// baselinePending tracks the session whose permissions tail is being
	// pulled for a permission cycle: a rapid second press must not stack
	// a second baseline + a second cycle when both land.
	baselinePending string

	// stagedMode holds a mode pick for a session that could not take it
	// in place (it had already run); the next new session starts from it,
	// mirroring the web hero-chip staging.
	stagedMode string
}

type toast struct {
	level string
	text  string
	until time.Time
}

// NewModel builds the TUI model.
func NewModel(a *app.App) *Model {
	// The stored preference (config.json "language") wins over the
	// locale environment; a switch via /language persists it back.
	loc := i18n.LoadDefault()
	m := &Model{
		app:         a,
		st:          a.Store(),
		th:          NewTheme(),
		trans:       newTransCache(),
		inp:         newInputLine(),
		openedInter: map[string]bool{},
		loc:         loc,
		usage:       a.Usage(),
		follow:      true,
		transDirty:  true, // first frame renders from a cold cache
	}
	m.locRef.Store(loc)
	// The store's turn-end toast lands on the store's goroutine, so its
	// text is formatted through the lock-free locale view.
	m.st.SetTurnEndTexter(func(info *core.TurnEndInfo) string {
		return turnEndText(m.locRef.Load(), info)
	})
	// DSH_INSECURE skips the server certificate: the env opted in for
	// this boot — say it once, plainly.
	if a.Client().Insecure() {
		m.toasts = append(m.toasts, toast{
			level: "warn",
			text:  loc.T("toast.insecure.tls"),
		})
	}
	if loc.Lang != "en" {
		m.toasts = append(m.toasts, toast{
			level: "info",
			text:  loc.T("lang.switched", loc.Name()),
			until: time.Now().Add(6 * time.Second),
		})
		// A stored locale file behind the built-in table renders the new
		// strings in English (correct fallback, mixed face): say it once,
		// with the fix.
		if n := loc.MissingKeys(); n > 0 {
			m.toasts = append(m.toasts, toast{
				level: "info",
				text:  loc.T("toast.locale.lag", n, loc.Lang),
				until: time.Now().Add(6 * time.Second),
			})
		}
	}
	m.refreshCmds()
	return m
}

// SetProg wires the program (needed before the first Init).
func (m *Model) SetProg(p *tea.Program) { m.prog = p }

type dirtyMsg struct{}

// permBaselineMsg reports that a session's tail (carrying the permissions
// projection baseline) has been pulled; the pending permission cycle then
// proceeds from the real current preset.
type permBaselineMsg struct{ id string }
type noticeMsg struct{ n core.Notice }
type tickMsg struct{ t time.Time }

type rpcErrMsg struct {
	op  string
	err error
}
type modelsLoadedMsg struct {
	id     string
	models *protocol.SessionModels
	// err != "" while the catalog fetch failed (loading stays off, the
	// picker body shows the problem).
	err string
}
type skillsLoadedMsg struct {
	id     string
	skills []protocol.SkillEntry
	err    error
}
type searchLoadedMsg struct {
	query string
	res   *protocol.SessionSearchResponse
	err   error
}
type createMsg struct {
	id  string
	err error
}

// clipDoneMsg reports the copy behind a text pick: the channel that
// carried the text (a clipboard binary, or OSC 52) and its size.
type clipDoneMsg struct {
	via   string
	chars int
	err   error
}

// clipReadMsg reports a failed or empty system-clipboard read (the
// explicit paste key); a successful read replays as a tea.PasteMsg and
// never reaches this type.
type clipReadMsg struct {
	err error
}

// pasteTextMsg replays a successful system-clipboard read as a paste
// (there is no tea.PasteMsg in the v1 reader to return from the worker).
type pasteTextMsg struct{ text string }

// inputProbeMsg asks the event loop for the current input line (see the
// Update case). reply is always buffered one; the test waits on it.
type inputProbeMsg struct{ reply chan string }

// Init starts the store pumps.
func (m *Model) Init() tea.Cmd {
	go func() {
		for range m.st.Dirty() {
			if m.prog != nil {
				m.prog.Send(dirtyMsg{})
			}
		}
	}()
	go func() {
		for n := range m.st.Notices() {
			if m.prog != nil {
				m.prog.Send(noticeMsg{n: n})
			}
		}
	}()
	if !m.splashOff {
		m.splashStart = time.Now()
	}
	// Bracketed paste is a command in the v1 reader (no program option):
	// terminal pastes then arrive as KeyMsgs flagged Paste, folded into
	// the focused field by handlePaste.
	enablePaste := func() tea.Msg { return tea.EnableBracketedPaste() }
	return tea.Batch(m.tick(), enablePaste)
}

// tickInterval paces the render clock. The frame must stay above it: the
// spinner animates at 110ms, the status verb at 1.7s, toasts expire on the
// second — a faster clock only burns CPU re-rendering an unchanged frame.
const tickInterval = 100 * time.Millisecond

// hostDownGrace is the dark stretch that earns the "start the server"
// hint: an up host answers its first status frame within moments, so
// two seconds of silence is evidence, not mere boot latency.
const hostDownGrace = 2 * time.Second

// hostDownToastLife keeps the hint on screen long enough to read while
// the outage state itself stays visible in the top bar's connecting
// indicator for as long as it lasts.
const hostDownToastLife = 10 * time.Second

// themeProbeEvery paces the live theme re-query: a theme switch is a rare,
// user-driven event, so a few seconds of lag is ample. The probe is a bare
// OSC 10/11 write (the answer rides the key reader), so it costs nothing
// on screen — the cadence just keeps the query traffic off the tty.
const themeProbeEvery = 60 // ticks (≈6 s at tickInterval)

// oscProbeTimeout is how long an unanswered probe stays open: a real
// answer lands in milliseconds, so past this the terminal has nothing to
// say and the probe closes with whatever it has.
const oscProbeTimeout = 1500 * time.Millisecond

func (m *Model) tick() tea.Cmd {
	// The boot animation runs on a finer clock: the wordmark sweep, the
	// tagline dissolve and the rail draw would stutter on the 100ms frame.
	interval := tickInterval
	if m.splashActive() {
		interval = tickInterval / 2
	}
	return func() tea.Msg {
		time.Sleep(interval)
		return tickMsg{t: time.Now()}
	}
}

// sendThemeQuery is the theme-probe worker: it writes the OSC 11/10
// status-report queries to the tty and lets the answer ride the key
// reader, which ingestThemeReply captures before any key handler can eat
// it. No terminal handoff — the old ReleaseTerminal/RestoreTerminal
// round trip left the alt screen for the exchange and blipped the main
// screen behind it once per probe: the every-6s flicker.
func (m *Model) sendThemeQuery() tea.Cmd {
	return func() tea.Msg {
		if !probeLive() {
			debugTheme("probe skipped (no live tty)")
			return nil
		}
		debugTheme("probe query sent")
		_, _ = probeOutput().WriteString(
			termenv.OSC + "11;?" + termenv.ST + termenv.OSC + "10;?" + termenv.ST)
		debugTheme("probe report pending")
		return nil
	}
}

// beginThemeProbe arms the live re-query from the tick handler. The
// answer lands over the key reader (ingestThemeReply); the probe closes
// when the report stream's terminators are consumed (both colors in
// hand), or at its deadline with whatever it has — an unanswered slot
// keeps the current palette via applySystem's guards.
func (m *Model) beginThemeProbe() {
	m.themeProbing = true
	m.oscTail = false
	m.oscTailEsc = false
	m.oscBG, m.oscFG = nil, nil
	m.oscDeadline = m.now.Add(oscProbeTimeout)
}

// finishThemeProbe closes an open probe and applies its report.
func (m *Model) finishThemeProbe() {
	if !m.themeProbing {
		return
	}
	m.themeProbing = false
	m.oscTail = false
	m.oscTailEsc = false
	applied := m.th.applySystem(m.oscBG, m.oscFG)
	debugTheme("probe closed bg=%v fg=%v applied=%v", m.oscBG, m.oscFG, applied)
	if applied {
		// The palette moved: cached rendered rows and markdown
		// pipelines still hold the old colors — drop both so the next
		// frame repaints with the live theme.
		m.resetTrans()
		m.mds, m.mdOrder = nil, nil
	}
}

// ingestThemeReply captures the OSC 11/10 status report the key reader
// delivers for the live probe. The reader parses the answer's leading ESC
// as the Alt modifier, and bubbletea takes exactly one rune after it, so a
// report arrives as a small event stream: Alt+"]" (the OSC intro), a
// plain run carrying the "11;rgb:.." or "10;rgb:.." payload, and the
// report terminator — Alt+\\ (the ST tail) or Ctrl-g for
// BEL-terminated reports. A terminator whose ESC lands at the end of a
// read is emitted as a plain Escape, and its backslash then arrives in the
// next read as an ordinary rune — the collector re-pairs them, and the
// probe only closes once the terminators are consumed, so a stream that
// outlives "both colors in hand" cannot leak into the key handlers. The
// collector stays quiet until the intro lands, so an answerless probe
// never eats real typing. It reports true when the key was probe traffic.
func (m *Model) ingestThemeReply(km tea.KeyMsg) bool {
	if !m.themeProbing {
		return false
	}
	switch {
	case km.Type == tea.KeyCtrlG:
		if m.oscTail {
			m.oscTail = false
			m.finishIfComplete()
			return true
		}
		return false
	case km.Type == tea.KeyEsc:
		// The ST terminator split across a read boundary: its trailing
		// ESC sits alone at the end of a (short) read, so the reader
		// emits it as a plain Escape key; the backslash arrives in the
		// next read as an ordinary rune (finishIfComplete runs there).
		if m.oscTail {
			m.oscTail = false
			m.oscTailEsc = true
			return true
		}
		return false
	case km.Type != tea.KeyRunes:
		return false
	}
	if km.Alt && len(km.Runes) == 1 {
		switch {
		case m.oscTail && km.Runes[0] == '\\':
			m.oscTail = false
			m.finishIfComplete()
			return true // the ST terminator
		case !m.oscTail && km.Runes[0] == ']':
			m.oscBuf = "]"
			return true // a report intro (re)starts the assembly
		}
		return false // a real Alt keypress
	}
	if km.Alt {
		return false
	}
	if m.oscTailEsc {
		if len(km.Runes) == 1 && km.Runes[0] == '\\' {
			m.oscTailEsc = false
			m.finishIfComplete()
			return true // the ST backslash that crossed the read
		}
		m.oscTailEsc = false // a real key: the split half-tail is spent
		return false
	}
	if m.oscBuf == "" {
		return false // a real keypress (or the intro never landed)
	}
	m.oscBuf += string(km.Runes)
	code, c, ok := parseOSCReport(m.oscBuf)
	if !ok {
		return true // truncated: keep assembling until the deadline
	}
	m.oscBuf = ""
	m.oscTail = true
	switch code {
	case oscBGCode:
		m.oscBG = c
	case oscFGCode:
		m.oscFG = c
	}
	// Even with both colors in hand the probe must not close here: the
	// second report's terminator is still in flight (or split across a
	// read) and would leak into the key handlers. The tail events — or
	// the deadline, if one was lost — close the probe.
	return true
}

// finishIfComplete closes the probe once its report stream is fully
// consumed and both colors were captured (a probe with a missing reply
// stays open until its deadline instead).
func (m *Model) finishIfComplete() {
	if m.themeProbing && !themeColorNil(m.oscBG) && !themeColorNil(m.oscFG) {
		m.finishThemeProbe()
	}
}

// debugTheme logs to stderr when DSH_THEME_DBG is set (live-pty debugging
// of the OSC 11/10 re-query; the pty carries stderr into the same stream).
func debugTheme(format string, args ...any) {
	if os.Getenv("DSH_THEME_DBG") == "" {
		return
	}
	fmt.Fprintf(os.Stderr, "DBG t=%s "+format+"\n",
		append([]any{time.Now().Format("15:04:05.0")}, args...)...)
}

// runCmd wraps an RPC-ish closure with a timeout, mapping errors to toasts.
func (m *Model) runCmd(op string, f func(ctx context.Context) error) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := f(ctx); err != nil {
			errText := errText(err, op)
			m.st.Notify(core.Notice{Level: errLevel(err), Text: errText, Bell: true})
			return rpcErrMsg{op: op, err: err}
		}
		return dirtyMsg{}
	}
}

func errLevel(err error) string {
	if _, ok := err.(*protocol.RpcError); ok {
		return "err"
	}
	return "warn"
}

func errText(err error, op string) string {
	// Host-shaped error text (RpcError.message, wrapped or bare) is the
	// one place terminal code can ride into a toast — strip control runes.
	var re *protocol.RpcError
	if errors.As(err, &re) {
		return op + ": " + textutil.StripControl(re.Message)
	}
	return op + ": " + err.Error()
}

// turnEndText is the locale face of the store's turn-end toast copy
// (the store calls it on its own goroutine; loc arrives through the
// lock-free view and may legitimately be the built-in English).
func turnEndText(loc *i18n.Locale, i *core.TurnEndInfo) string {
	dur := textutil.HumanDuration(time.Duration(i.Ms) * time.Millisecond)
	switch i.Kind {
	case "completed":
		return loc.T("notice.turn.done", dur)
	case "interrupted", "aborted":
		return loc.T("notice.turn.interrupted", dur)
	case "max-tokens":
		return loc.T("notice.turn.maxtokens")
	default:
		t := loc.T("notice.turn.ended", i.Kind)
		if i.Error != "" {
			t += " — " + i.Error
		}
		return t
	}
}

// activeID returns the currently active session ("" if none).
func (m *Model) activeID() string { return m.st.Active() }

// modeLabel renders a preset id in the active language: the localized
// display name for the four shipped modes, the canonical copy otherwise
// (user-authored presets keep their published name on every face).
func (m *Model) modeLabel(id string) string {
	if _, ok := modes.InfoOf(id); ok {
		return m.loc.T("mode.label." + id)
	}
	return modes.Label(id)
}

// ctxLabel is the model label for the transcript header: one cheap store
// read (no snapshot copies), resolved once per render pass and handed to
// the item renderers as a parameter.
func (m *Model) ctxLabel() string { return m.st.CtxModel(m.activeID()) }

// resetTrans discards the transcript render cache and marks the next frame
// dirty: without the mark, the idle-frame short-circuit would keep serving
// the previous session's rows from the now-empty cache.
func (m *Model) resetTrans() {
	m.trans = newTransCache()
	m.transDirty = true
}

// Update is the tea model update.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.W, m.H = msg.Width, msg.Height
		// A new width invalidates the wrap width of every cached row.
		m.transDirty = true
		return m, nil

	case tickMsg:
		m.now = msg.t
		// Host-down hint: the downlink stayed dark past the grace
		// window, so tell the user how to start the server — once per
		// outage (the flags reset on recovery, so a later drop hints
		// again). The top bar keeps showing the outage meanwhile.
		if !m.st.Connected() {
			if m.hostDownSince.IsZero() {
				m.hostDownSince = m.now
			}
			if !m.hostDownHinted && m.now.Sub(m.hostDownSince) >= hostDownGrace {
				m.toasts = append(m.toasts, toast{
					level: "warn",
					text:  m.loc.T("host.down"),
					until: m.now.Add(hostDownToastLife),
				})
				if len(m.toasts) > 3 {
					m.toasts = m.toasts[len(m.toasts)-3:]
				}
				m.hostDownHinted = true
			}
		} else if !m.hostDownSince.IsZero() || m.hostDownHinted {
			m.hostDownSince = time.Time{}
			m.hostDownHinted = false
		}
		if m.splashActive() && m.now.Sub(m.splashStart) >= splashDuration {
			m.splashStart = time.Time{}
		}
		// Splash phase: fold the landing baseline / auto-pick / tail into
		// the transcript cache so the first visible frame is warm.
		m.preloadTrans()
		var cmds []tea.Cmd
		cmds = append(cmds, m.tick())
		// Periodically re-read the live terminal theme so a theme switch
		// while the TUI is up follows through: the probe writes the OSC
		// 10/11 query, the answer rides the key reader, ingestThemeReply
		// applies it.
		m.themeProbe++
		if m.themeProbe >= themeProbeEvery {
			m.themeProbe = 0
			if !m.splashActive() && m.th.sysCapable() && !m.themeProbing {
				m.beginThemeProbe()
				cmds = append(cmds, m.sendThemeQuery())
			}
		}
		// A probe whose answer never landed (the terminal stayed silent)
		// closes at its deadline with whatever it has.
		if m.themeProbing && !m.now.Before(m.oscDeadline) {
			m.finishThemeProbe()
		}
		// expire toasts and flash
		for i := len(m.toasts) - 1; i >= 0; i-- {
			if m.toasts[i].until.Before(m.now) {
				m.toasts = append(m.toasts[:i], m.toasts[i+1:]...)
			}
		}
		return m, tea.Batch(cmds...)

	case dirtyMsg:
		// The state moved; the next frame re-syncs the transcript cache
		// (the rev check in transcriptView catches it — the flag is set
		// too so a pulse always costs at most one full pass). During the
		// splash that re-sync happens right here (preloadTrans), so the
		// interface starts on the first frame after the splash.
		m.transDirty = true
		m.preloadTrans()
		m.flashTurnEnds()
		// A parked answerable frame (ask-user question / tool approval)
		// for the active session: surface it the way the web client
		// does — the answer blocks the running turn, so it can't sit
		// unseen in the store.
		m.surfacePending(true)
		var cmds []tea.Cmd
		if c := m.refreshCmds(); c != nil {
			cmds = append(cmds, c)
		}
		// bootIfNeeded may kick off the first tail load.
		if c := m.bootIfNeeded(); c != nil {
			cmds = append(cmds, c)
		}
		// Subs tab on screen: keep the child catalog fresh (the cache TTL
		// alone would leave the tab on its last snapshot).
		if m.dockVisible && m.dockTab == dockSubs {
			if c := m.subDockRefresh(); c != nil {
				cmds = append(cmds, c)
			}
		}
		switch len(cmds) {
		case 0:
			return m, nil
		case 1:
			return m, cmds[0]
		default:
			return m, tea.Batch(cmds...)
		}

	case noticeMsg:
		m.addToast(msg.n)
		var cmd tea.Cmd
		if msg.n.Bell {
			m.bell = true
		}
		if msg.n.Level == "ok" || msg.n.Level == "err" {
			m.flashFor(msg.n)
		}
		return m, cmd

	// inputProbeMsg is a test hook: headless tests read the input line
	// off the model goroutine and need a happens-before edge for it;
	// the reply (sent here, on the event loop) provides one.
	case inputProbeMsg:
		msg.reply <- m.inp.value()
		return m, nil

	case rpcErrMsg:
		// already toasted via store notice
		return m, nil

	case clipDoneMsg:
		if msg.err == nil && m.sel.copied {
			// The pick's copy landed; crush clears the mouse here.
			m.sel.reset()
		}
		if msg.err != nil {
			m.addToast(core.Notice{Level: "warn", Text: "copy failed: " + msg.err.Error()})
		} else {
			m.addToast(core.Notice{Level: "ok", Text: fmt.Sprintf("copied %d chars · %s", msg.chars, msg.via)})
		}
		return m, nil

	case clipReadMsg:
		// The async clipboard read came back without text (a read
		// failure, or an empty clipboard); a successful read replays as
		// the pasteTextMsg case below, so here we only report the miss.
		if msg.err != nil {
			m.addToast(core.Notice{Level: "warn", Text: "clipboard: " + msg.err.Error()})
		} else {
			m.addToast(core.Notice{Level: "info", Text: "clipboard is empty"})
		}
		return m, nil

	case pasteTextMsg:
		// A successful clipboard read: same path as a bracketed paste.
		return m.handlePaste(msg.text)

	case permBaselineMsg:
		m.baselinePending = ""
		// The user may have switched sessions while the baseline loaded.
		if msg.id == m.activeID() {
			return m, m.cycleStep(msg.id)
		}
		return m, nil

	case modelsLoadedMsg:
		// Cache the catalog for the top bar's model readout (the picker
		// fetch and the activation fetch both land here); a failed fetch
		// keeps the previous catalog.
		if msg.err == "" {
			m.st.SetModels(msg.id, msg.models)
		}
		if pk, ok := m.topModal().(*modelPicker); ok && msg.id == m.activeID() {
			if msg.err != "" {
				pk.loading = false
				pk.err = msg.err
				return m, nil
			}
			pk.fill(msg.models)
		}
		return m, nil

	case skillsLoadedMsg:
		// The fetch ran off the event loop; store the result (negative
		// cache included), then re-merge the menu from the fresh cache.
		skillsCachePut(msg.id, msg.skills, msg.err)
		m.skillsFetching = false
		return m, m.refreshCmds()

	case subagentsLoadedMsg:
		// The child catalog landed; store it (negative cache included),
		// clamp the dock cursor, and re-run the @ completion so the
		// children appear on the next frame. A failed fetch keeps its
		// previous rows: an RPC blip must not blank an open catalog.
		ok := msg.err == nil
		entries := msg.entries
		if !ok && len(entries) == 0 {
			if prev, had := subagentCache[msg.id]; had {
				entries = prev.entries
			}
		}
		subagentCache[msg.id] = subagentCacheEntry{at: time.Now(), entries: entries, ok: ok}
		m.subFetching = false
		m.clampSubDockCur()
		if m.inp.atOpen {
			return m, m.refreshAtMenu()
		}
		return m, nil

	case subHistoryLoadedMsg:
		// A child transcript page landed: fill the open window (if it is
		// still on top) and sort the events into chronological order.
		sort.SliceStable(msg.events, func(i, j int) bool { return msg.events[i].Event.Seq < msg.events[j].Event.Seq })
		if sm, ok := m.topModal().(*subagentModal); ok && sm.child == msg.child {
			sm.loading = false
			if msg.err != nil {
				sm.err = m.loc.T("dock.subs.error")
			}
			sm.lines = subHistoryLines(sm.loc, msg.events)
		}
		return m, nil

	case presetsLoadedMsg:
		if pk, ok := m.topModal().(*modePicker); ok {
			pk.loading = false
			pk.presets = msg.presets
			pk.cur = 0
			if id := m.activeID(); id != "" {
				if snap := m.st.Get(id); snap != nil {
					for i, e := range msg.presets {
						if e.Id == snap.Summary.AgentPreset {
							pk.cur = i
							break
						}
					}
				}
			}
		}
		return m, nil

	case searchLoadedMsg:
		// The reply may be stale (the query or the modal moved on while the
		// worker ran): apply it only while the top modal is still a search
		// box showing the very query that was answered.
		if sm, ok := m.topModal().(*searchModal); ok && strings.TrimSpace(sm.ed.string()) == msg.query {
			if msg.err != nil {
				sm.err = errText(msg.err, "search")
			} else if msg.res != nil {
				sm.res = msg.res.Items
				sm.cur = 0
			}
			sm.load = false
		}
		return m, nil

	case createMsg:
		if msg.err == nil && msg.id != "" {
			m.st.SetActive(msg.id)
			// Boot path (nothing to resume): the fresh session lands in
			// the host cwd, which may be a registered workspace — switch
			// into it like any resumed session (a manual create after the
			// boot check closed is a no-op).
			m.bootWorkspaceSwitch(msg.id)
			// The create worker only reported the id; the focus reset
			// happens here, on the model goroutine (worker-side writes
			// would race the renderer).
			m.resetTrans()
			m.follow = true
			m.inp.clear()
			// Continue the actions parked for this new session.
			var cmds []tea.Cmd
			if m.pendingPrompt != "" {
				text := m.pendingPrompt
				m.pendingPrompt = ""
				cmds = append(cmds, m.runCmd("send", func(ctx context.Context) error {
					_, err := m.app.Prompt(ctx, msg.id, text, "queue")
					return err
				}))
			}
			if m.pendingCycle {
				m.pendingCycle = false
				cmds = append(cmds, m.baselineCycle(msg.id))
			}
			cmds = append(cmds, m.cmdLoadModels(msg.id))
			return m, tea.Sequence(append(cmds, m.cmdLoadTail(msg.id))...)
		}
		// Creation failed (toasted); drop the parked actions.
		m.pendingPrompt = ""
		m.pendingCycle = false
		return m, nil

	case tea.KeyMsg:
		// Bracketed paste from the terminal arrives as a KeyMsg flagged
		// Paste (the v1 reader has no separate paste message): fold it
		// in before any chord can eat the runes, exactly like the
		// clipboard read replay below.
		if msg.Paste && msg.Type == tea.KeyRunes {
			return m.handlePaste(string(msg.Runes))
		}
		return m.handleKey(msg)
	case tea.MouseMsg:
		return m.handleMouse(msg)
	default:
		// kitty-protocol chords the v1 reader cannot name: a
		// ctrl+shift+c/v arrives as ESC[99;3u / ESC[118;3u and
		// shift+enter as ESC[13;2u, each reported as an unknown CSI
		// sequence — an unexported []byte-based message, matched
		// structurally rather than by type.
		if b := byteMsgOf(msg); b != nil {
			isCopy, isPaste := csiuChord(b)
			switch {
			case isCopy:
				if m.inpSelActive() {
					return m, m.inpCopySel()
				}
				if m.sel.active {
					return m, m.copySelection()
				}
				return m, nil
			case isPaste:
				return m, m.readClipboard()
			case csiuShiftEnter(b):
				return m, m.handleShiftEnter()
			}
		}
	}
	return m, nil
}

// byteMsgOf extracts the bytes of a message whose underlying type is
// []byte without naming the type — the v1 reader's unknownCSISequenceMsg
// is unexported and this is the only name-free way to reach it.
func byteMsgOf(msg tea.Msg) []byte {
	v := reflect.ValueOf(msg)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() == reflect.Slice && v.Type().Elem().Kind() == reflect.Uint8 && !v.IsNil() {
		return v.Bytes()
	}
	return nil
}

// csiuChord reports whether a kitty-protocol CSI-u escape (ESC
// <keycode> ; <modifier> u) carries the ctrl+shift copy/paste chord:
// keycode 99/118 ('c'/'v') with the ctrl modifier bit (2) set. Terminals
// only emit CSI-u for chords plain control bytes cannot carry, so a
// chord without the ctrl bit is a different key entirely (shift+c is a
// plain 'c').
func csiuChord(seq []byte) (isCopy, isPaste bool) {
	if len(seq) < 4 || seq[0] != '\x1b' || seq[1] != '[' || seq[len(seq)-1] != 'u' {
		return false, false
	}
	parts := strings.Split(string(seq[2:len(seq)-1]), ";")
	if len(parts) != 2 {
		return false, false
	}
	code, err1 := strconv.Atoi(string(parts[0]))
	mod, err2 := strconv.Atoi(string(parts[1]))
	if err1 != nil || err2 != nil || mod&2 == 0 {
		return false, false
	}
	switch code {
	case 99: // 'c'
		return true, false
	case 118: // 'v'
		return false, true
	}
	return false, false
}

// csiuShiftEnter reports whether a kitty-protocol CSI-u escape (ESC
// <keycode> ; <modifier> u) is shift+enter: keycode 13 ('\r') with the
// shift modifier bit (2) set. The v1 reader has no shift+enter key type,
// so the protocol's form reaches Update as an unknown CSI sequence.
func csiuShiftEnter(seq []byte) bool {
	if len(seq) < 4 || seq[0] != '\x1b' || seq[1] != '[' || seq[len(seq)-1] != 'u' {
		return false
	}
	parts := strings.Split(string(seq[2:len(seq)-1]), ";")
	if len(parts) != 2 {
		return false
	}
	code, err1 := strconv.Atoi(string(parts[0]))
	mod, err2 := strconv.Atoi(string(parts[1]))
	return err1 == nil && err2 == nil && code == 13 && mod&2 == 2
}

// handleShiftEnter is the kitty-protocol shift+enter (ESC[13;2u): a
// manual newline in the main input bar — a popup open keeps the key, its
// fields are one line. The bare-LF form (terminals that send 0x0A for
// shift+enter) is handled in inputLine.handleKey.
func (m *Model) handleShiftEnter() tea.Cmd {
	if m.topModal() != nil || m.sideVisible {
		return nil
	}
	m.inp.insertRune('\n')
	return nil
}

// ---- boot ---------------------------------------------------------------------

// bootIfNeeded resumes the previous session once the roster has landed
// (the web client's "resume last" rule): the most recent non-blank
// session becomes active with its transcript tail loaded, and a
// brand-new session in the host cwd is minted only when there is
// nothing to resume. The host probe lands before session.list answers:
// until the roster is baselined (Store.RosterBaselined) boot defers
// (staying un-booted), so the pick sees the real roster — an early dirty
// pulse would otherwise consume the one-shot boot against an empty
// roster and always mint a fresh session, and the first ctrl+p /prompt
// would have no session to cycle.
func (m *Model) bootIfNeeded() tea.Cmd {
	if m.booted {
		// The boot pick may have landed before the workspace registry
		// baselined: re-run the auto workspace switch on each pulse
		// until it resolves (bootWorkspaceSwitch is one-shot and cheap
		// once closed).
		m.bootWorkspaceSwitch(m.activeID())
		return nil
	}
	if id := m.st.Active(); id != "" {
		m.booted = true
		m.bootWorkspaceSwitch(id)
		return nil
	}
	// RosterReady (not RosterBaselined): the boot cache may have
	// installed the previous boot's rows by now, and picking against
	// them is exactly the point — the chrome starts warm and the live
	// session.list corrects it (SetSessions drops sessions the host no
	// longer lists; a vanished pick clears the selection there).
	if m.st.Host() == nil || !m.st.RosterReady() {
		return nil
	}
	m.booted = true
	// Resume last: ensureSession picks the roster's most recent
	// non-blank session (kicking off its tail load) or falls through to
	// a fresh session; createMsg/the tail load continue from there. The
	// pick's workspace is switched into under the splash as well (the
	// check stays open until the registry baselines).
	id, cmd := m.ensureSession()
	m.bootWorkspaceSwitch(id)
	return cmd
}

// bootWorkspaceSwitch lands the boot auto-switch to the resumed
// session's workspace: the moment the registry resolves the session
// (accounted membership first, then the cwd path), the app's workspace
// context — bottom-bar chip, session-window scope, workspace-modal
// preselect (all derived from the active session) — is that workspace,
// under the splash when the registry lands in time. One info toast
// marks the switch; a session the registry does not own resolves to
// none and closes the check (no toast, ungrouped as before). While the
// registry is still unbaselined the check stays open: the SetWorkspaces
// pulse re-runs it through bootIfNeeded.
func (m *Model) bootWorkspaceSwitch(id string) {
	if id == "" || m.bootWsChecked {
		return
	}
	ws := m.st.WorkspaceForSession(id)
	if ws == nil {
		if !m.st.WorkspacesReady() {
			return // registry still landing; its pulse re-checks
		}
		m.bootWsChecked = true // no workspace owns this session
		return
	}
	m.bootWsChecked = true
	// Model-goroutine toast (the notice channel is for worker-side
	// notices; this one is issued from Update).
	m.addToast(core.Notice{Level: "info", Text: m.loc.T("ws.boot.switched", workspacePathLabel(ws))})
}

func (m *Model) cmdLoadTail(id string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		m.app.LoadTail(ctx, id)
		return dirtyMsg{}
	}
}

// cmdLoadModels fetches the session's advertised model catalog at
// activation, so the top bar's model readout shows the display name the
// model picker uses without waiting for the picker to be opened. A failed
// fetch keeps the previous catalog (the message carries the error for the
// picker, if it happens to be open).
func (m *Model) cmdLoadModels(id string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		models, err := m.app.Models(ctx, id)
		if err != nil {
			return modelsLoadedMsg{id: id, err: errText(err, "models")}
		}
		return modelsLoadedMsg{id: id, models: models}
	}
}

func (m *Model) cmdLoadOlder(id string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		snap := m.st.Get(id)
		if snap == nil {
			return dirtyMsg{}
		}
		// Page before the oldest event we hold.
		before := int64(1)
		if len(snap.Items) > 0 && snap.Items[0].Seq > 1 {
			before = snap.Items[0].Seq
		}
		m.app.LoadOlder(ctx, id, before)
		return dirtyMsg{}
	}
}

// ---- toasts / flash -------------------------------------------------------------

func (m *Model) addToast(n core.Notice) {
	m.toasts = append(m.toasts, toast{level: n.Level, text: n.Text, until: m.now.Add(4 * time.Second)})
	if len(m.toasts) > 3 {
		m.toasts = m.toasts[len(m.toasts)-3:]
	}
}

func (m *Model) flashFor(n core.Notice) {
	m.bell = m.bell || n.Bell
	// The top bar renders the pending flash; keep a short window.
	if n.Bell {
		m.flashWindow = time.Now().Add(5 * time.Second)
	}
}

// ---- key handling -----------------------------------------------------------------

func (m *Model) handleKey(km tea.KeyMsg) (tea.Model, tea.Cmd) {
	// The live theme probe's OSC answer arrives through the key reader as
	// a small event stream: capture it before any handler can eat the
	// Alt-"]" intro and the payload runs.
	if m.ingestThemeReply(km) {
		return m, nil
	}
	// 0) splash: any key dismisses the boot animation early
	if m.splashActive() {
		m.splashStart = time.Time{}
		return m, nil
	}
	// 1) modal (topmost first)
	if len(m.mods) > 0 {
		if cmd, handled := m.handleModalKey(km); handled {
			return m, cmd
		}
		// The opener's own chord is the window's toggle: pressing the
		// chord that opened a picker a second time closes it (ctrl+h
		// already toggles inside helpModal's update, and the session
		// window on ctrl+s in the 1a block). The chord is otherwise
		// inert while a popup owns the keyboard, so this is a pure
		// close key.
		if m.toggleChord(km) {
			m.closeModal()
			return m, nil
		}
		// Every popup owns the keyboard while it is open (the session
		// window's rule, generalized): a key the top modal doesn't claim
		// is absorbed here, so no main-window chord or the input line
		// fires underneath. Closing the popup restores them. One
		// exception: an active pick in the popup's own field still
		// answers the copy chords — the field is the focused editor, and
		// ctrl+c (and its shift form) copy it exactly like the main bar.
		if km.Type == tea.KeyCtrlC && m.inpSelActive() {
			return m, m.inpCopySel()
		}
		return m, nil
	}

	running := m.running()
	emptyInput := m.inp.empty()

	// 1a) session window: while the centered popup is open it owns EVERY
	// key (crush's dialog rule — no chord leaks through to the main UI).
	// Text keys edit the search line. Up/down (and ←/→) MOVE the cursor
	// along the filtered roster — a move, not a pick. Space SELECTS:
	// it commits the cursor's row as the active session and stays open.
	// Enter CONFIRMS: it opens the cursor's row (committing it when it is
	// not already the active one — falling back to the first match when the
	// active session fell out of the filter) and closes. Esc CLOSES (a
	// non-empty query clears first, so one esc edits, one closes). Closing
	// it hands the keyboard back to the modals, the chords and the main
	// input bar.
	if m.sideVisible {
		switch {
		case km.Type == tea.KeyUp:
			m.sideCursorMove(-1)
			return m, nil
		case km.Type == tea.KeyDown:
			m.sideCursorMove(1)
			return m, nil
		case km.Type == tea.KeyLeft:
			m.sideCursorMove(-1)
			return m, nil
		case km.Type == tea.KeyRight:
			m.sideCursorMove(1)
			return m, nil
		case km.String() == " ":
			// Select: commit the cursor's row as the active session. The
			// window stays open and the cursor keeps its row; a no-op when
			// the cursor already rests on the active session. The key is
			// consumed either way (space no longer types into the search).
			if ids := m.sideNavIDs(); len(ids) > 0 {
				if id := ids[m.sideCursorIndex()]; id != m.activeID() {
					cmd := m.selectSession(id)
					m.sideCursorID = id
					return m, cmd
				}
			}
			return m, nil
		case km.Type == tea.KeyEnter:
			// Confirm: open the cursor's row (committing it when it is not
			// already the active one — falling back to the first match when
			// the active session fell out of the filter) and close.
			m.sideVisible = false
			if ids := m.sideNavIDs(); len(ids) > 0 {
				if id := ids[m.sideCursorIndex()]; id != m.activeID() {
					return m, m.selectSession(id)
				}
			}
			return m, nil
		case km.Type == tea.KeyEsc:
			if m.sideSearch.string() != "" {
				m.sideSearch.reset()
			} else {
				m.sideVisible = false
			}
			return m, nil
		case km.Type == tea.KeyCtrlS:
			m.sideVisible = false
			return m, nil
		}
		m.sideSearch.handleKey(km)
		return m, nil // the window keeps the key: edited or absorbed
	}

	// 2) global chords
	switch {
	// crush's explicit copy chord: ctrl+shift+c. A terminal that
	// reports the shift modifier delivers it as ctrl+c with the Alt bit
	// (alacritty forwards ESC+ctrl-char and tmux re-sends M-C-c; the v1
	// reader parses both the same); a kitty-protocol terminal sends CSI-u
	// (ESC[99;3u), which Update's default case routes here. The explicit
	// chord copies even with a non-empty input — plain ctrl+c below must
	// keep its clear/quit duty.
	case km.Type == tea.KeyCtrlC && km.Alt:
		if m.inpSelActive() {
			return m, m.inpCopySel()
		}
		if m.sel.active {
			return m, m.copySelection()
		}
		return m, nil
	// An active input pick (a modal edit field, else the main bar)
	// claims plain ctrl+c (copy) ahead of every other path: a picked
	// field is non-empty by construction, and the transcript pick's
	// plain ctrl+c below needs an empty input, so the two can never
	// collide.
	case km.Type == tea.KeyCtrlC && m.inpSelActive():
		return m, m.inpCopySel()
	// An active transcript pick claims ctrl+c (copy) ahead of the quit
	// path.
	case km.Type == tea.KeyCtrlC && m.sel.active && emptyInput:
		return m, m.copySelection()
	case km.Type == tea.KeyCtrlV:
		// Explicit clipboard paste (crush's ctrl+shift+v; the v1 key
		// reader reports both the plain and the shift chord as ctrl+v —
		// the Alt bit is irrelevant for paste, so both fall through
		// here). The chord cannot be told apart from plain ctrl+v where
		// the terminal cannot
		// tell): read the system clipboard and replay it as a paste,
		// so it shares the path with the terminal's bracketed paste.
		return m, m.readClipboard()
	case km.Type == tea.KeyCtrlB && emptyInput:
		m.dockVisible = !m.dockVisible
		return m, nil
	case km.Type == tea.KeyCtrlN && emptyInput:
		return m, m.openModelPicker()
	case km.Type == tea.KeyCtrlD && emptyInput:
		m.quitting = true
		return m, tea.Quit
	case km.Type == tea.KeyCtrlC:
		if !emptyInput {
			m.inp.clear()
			m.inp.refreshMenu(m.mergedCmds)
		} else {
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil
	case km.Type == tea.KeyCtrlQ && emptyInput:
		m.quitting = true
		return m, tea.Quit
	case km.Type == tea.KeyEsc:
		if m.inp.menuOpen {
			m.inp.menuOpen = false
			return m, nil
		}
		if running {
			id := m.activeID()
			return m, m.runCmd("interrupt", func(ctx context.Context) error {
				return m.app.Cancel(ctx, id)
			})
		}
		if m.dockVisible {
			m.dockVisible = false
			return m, nil
		}
		if m.sel.active {
			m.sel.reset()
			return m, nil
		}
		if m.topModal() == nil && m.inp.selActive() {
			// An active input pick claims esc next (a modal open owns
			// esc for itself: its field's pick dies with the plate).
			m.inp.sel.reset()
			return m, nil
		}
		if !emptyInput {
			m.inp.clear()
		}
		return m, nil
	case km.Type == tea.KeyPgUp || km.Type == tea.KeyPgDown:
		vh := m.viewportHeight()
		if km.Type == tea.KeyPgUp {
			m.scrollBy(-vh)
		} else {
			m.scrollBy(vh)
		}
		return m, nil
	case km.Type == tea.KeyHome && emptyInput:
		m.scroll = 0
		m.follow = false
		m.followBase = m.trans.total()
		return m, nil
	case km.Type == tea.KeyEnd && emptyInput:
		m.scroll = 1 << 30
		m.follow = true
		m.followBase = 0
		return m, nil
	case km.Type == tea.KeyCtrlO && emptyInput:
		return m, m.openModePicker()
	case m.dockVisible && emptyInput && (km.String() == "1" || km.String() == "2" || km.String() == "3" || km.String() == "4" || km.String() == "5"):
		// 1-5 jump to the tab at that visual position (left to right).
		m.dockTab = dockTodos + (map[string]int{"1": 0, "2": 1, "3": 2, "4": 3, "5": 4}[km.String()])
		m.subDockCur = 0
		return m, m.subDockRefresh()
	case m.dockVisible && emptyInput && km.Type == tea.KeyLeft:
		if m.dockTab > 0 {
			m.dockTab--
			m.subDockCur = 0
		}
		return m, m.subDockRefresh()
	case m.dockVisible && emptyInput && km.Type == tea.KeyRight:
		// The bound is the LAST tab, not an earlier one: capping at an
		// earlier index is how the row gets cut off partway through.
		if m.dockTab < dockGoal {
			m.dockTab++
			m.subDockCur = 0
		}
		return m, m.subDockRefresh()
	case m.dockVisible && m.dockTab == dockSubs && emptyInput && km.Type == tea.KeyUp:
		if m.subDockCur > 0 {
			m.subDockCur--
		}
		return m, nil
	case m.dockVisible && m.dockTab == dockSubs && emptyInput && km.Type == tea.KeyDown:
		if entries, _ := subagentEntries(m.activeID()); m.subDockCur < len(entries)-1 {
			m.subDockCur++
		}
		return m, nil
	case m.dockVisible && m.dockTab == dockSubs && emptyInput && km.Type == tea.KeyEnter:
		return m, m.openSubTranscript()
	case m.dockVisible && m.dockTab == dockSubs && emptyInput && km.String() == "x":
		return m, m.interruptSub()
	case km.Type == tea.KeyLeft && emptyInput:
		// ←/→ walk the sessions (the old [ ] chords). While the dock is
		// open the arrows above keep their dock-tab duty instead.
		return m, m.switchSession(-1)
	case km.Type == tea.KeyRight && emptyInput:
		return m, m.switchSession(1)
	case km.Type == tea.KeyCtrlI && emptyInput:
		// Reopen a parked question / approval. The auto-open fires once
		// per frame arrival; this chord covers frames the user esc-closed.
		if m.surfacePending(false) {
			return m, nil
		}
	case km.Type == tea.KeyCtrlH && emptyInput:
		m.openModal(&helpModal{loc: m.loc})
		return m, nil
	case km.Type == tea.KeyCtrlW && emptyInput:
		return m, m.openWorkspace()
	case km.Type == tea.KeyCtrlE && emptyInput:
		m.verbose = !m.verbose
		m.resetTrans()
		return m, nil
	case (km.Type == tea.KeyRunes && km.String() == "x" || km.Type == tea.KeySpace) && emptyInput && m.lastToolBlock() != nil:
		// Peek at the newest tool card. Gated on a card existing so the
		// first character typed into an empty input still lands in the
		// editor (the f-fork contract, extended to the peek key).
		m.toggleToolExpand()
		return m, nil
	case (km.Type == tea.KeyCtrlU || km.Type == tea.KeyCtrlK) && emptyInput && m.activeID() != "":
		id := m.activeID()
		return m, m.cmdLoadOlder(id)
	case km.Type == tea.KeyCtrlS && emptyInput:
		// Session window: the centered popup for searching and picking a
		// session. Pressing ctrl+s again inside it closes (toggle). The
		// cursor starts on the active session (the committed selection).
		m.sideVisible = true
		m.sideCursorID = m.activeID()
		return m, nil
	case km.Type == tea.KeyCtrlX && emptyInput:
		id := m.activeID()
		snap := m.st.Get(id)
		if snap != nil && len(snap.Queue) > 0 {
			q := snap.Queue[0]
			return m, m.runCmd("drop queued", func(ctx context.Context) error {
				return m.app.UpdateQueue(ctx, protocol.QueueUpdateRequest{
					SessionId: id, ItemId: q.Id, Action: protocol.QueueAction{Action: "remove"},
				})
			})
		}
		return m, nil
	case km.Type == tea.KeyCtrlP && emptyInput:
		// State-changing chord: like the other window chords it needs an
		// empty input, so a stray ctrl+p while typing (terminal keymap,
		// auto-repeat, a pasted 0x10 byte) does not flip the sandbox
		// preset and stack "permission → …" toasts.
		return m, m.cmdCyclePermission()
	case km.String() == "f" && emptyInput && m.activeID() != "":
		// Fork the active session at the view position (the web's branch
		// button, keyboard-first): the cut is the last completed turn at
		// or above the view — the newest one while the tail is pinned.
		id := m.activeID()
		return m, m.cmdFork(id, m.forkAnchorSeq(id))
	}

	// 3) input
	if ok, send := m.inp.handleKey(m, km); ok {
		// The slash menu tracks typing; while a history browse is active
		// the arrows load whole entries, and re-computing the menu here
		// would reopen it on recalled "/commands" (and steal the arrows).
		if m.inp.histPos < 0 {
			m.inp.refreshMenu(m.mergedCmds)
			if cmd := m.refreshAtMenu(); cmd != nil {
				return m, cmd
			}
		}
		if send {
			return m, m.submit()
		}
		return m, nil
	}

	// 4) transcript scroll: one line per key press while the input is
	// empty (typed text keeps the arrows for the caret).
	switch km.Type {
	case tea.KeyUp:
		if !m.inp.empty() {
			return m, nil
		}
		m.scrollBy(-1)
		return m, nil
	case tea.KeyDown:
		if !m.inp.empty() {
			return m, nil
		}
		m.scrollBy(1)
		return m, nil
	}
	return m, nil
}

func (m *Model) running() bool {
	id := m.activeID()
	if id == "" {
		return false
	}
	snap := m.st.Get(id)
	if snap == nil {
		return false
	}
	// Liveness = the host's running flag (re-baselined by the session list
	// and live turn/status events), not the folded log's TurnActive, which
	// stays true for a turn that ended in history without a turn/end (a
	// boot replay would otherwise report "running" with no live work).
	return snap.Running
}

// wheelStep is the transcript lines moved by one mouse-wheel notch.
const wheelStep = 3

// handleMouse lets the wheel scroll the transcript (wherever the pointer
// sits) and the left button pick text, crush-style: presses and drags
// route to the topmost surface under the cursor — the topmost modal's
// edit field (plate is opaque, a press on the plate that misses a field
// still eats it), else the main input bar (caret / line pick), else the
// transcript pane (crush's click ladder on the transcript: a press
// anchors a cell, a drag extends to a cell endpoint, double-click word,
// triple-click line). A finished pick stays banded until the explicit
// copy (ctrl+c), an esc, or a new press that re-anchors.
func (m *Model) handleMouse(mm tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch mm.Button {
	case tea.MouseButtonWheelUp:
		m.scrollBy(-wheelStep)
	case tea.MouseButtonWheelDown:
		m.scrollBy(wheelStep)
	}
	switch mm.Action {
	case tea.MouseActionPress:
		if mm.Button != tea.MouseButtonLeft {
			return m, nil
		}
		clicks := m.clicksAt(mm.X, mm.Y)
		// 1) modal edit field (its plate is opaque over the transcript).
		if m.mousePressEditor(mm.X, mm.Y) {
			m.sel.reset()
			if clicks >= 2 && m.dragEdit != nil {
				m.dragEdit.mouseSelectLine()
			}
			return m, nil
		}
		// 2) main input bar.
		if idx, ok := m.inpRowAt(mm.X, mm.Y); ok {
			m.sel.reset()
			m.inp.mousePress(idx)
			if clicks >= 2 {
				m.inp.mouseSelectLine()
			}
			m.inpDrag = true
			return m, nil
		}
		// 3) transcript pane; a press outside the pane — or over an
		// opaque modal plate — drops any live pick.
		m.sel.reset()
		m.inpDrag, m.dragEdit = false, nil
		row, col, ok := m.transRowAt(mm.X, mm.Y)
		if !ok || m.topModal() != nil {
			return m, nil
		}
		if row < 0 || row >= m.trans.total() {
			return m, nil
		}
		line := m.transOffset() + row
		if line < 0 || line >= len(m.trans.lines) {
			return m, nil
		}
		// crush's click ladder on the transcript.
		switch clicks {
		case 2:
			m.sel.selectWord(line, col, visibleText(m.trans.lines[line]))
		case 3:
			m.sel.selectLine(line, plainWidth(m.trans.lines[line]))
		default:
			m.sel.anchor(line, col)
		}
		return m, nil
	case tea.MouseActionMotion:
		// A held left button over an input keeps extending that field's
		// pick. (SGR stream facts, bubbletea v1 test vectors: cb=0 'm'
		// = left press, cb=32 'm' = left drag, cb=0 'M' = left release,
		// cb=35 = button-less motion.)
		if mm.Button != tea.MouseButtonLeft {
			return m, nil
		}
		if m.dragEdit != nil {
			if idx, ok := m.editorDragAt(m.dragEdit, mm.X, mm.Y); ok {
				m.dragEdit.mouseDrag(idx)
			}
			return m, nil
		}
		if m.inpDrag {
			if idx, ok := m.inpRowAt(mm.X, mm.Y); ok {
				m.inp.mouseDrag(idx)
			}
			return m, nil
		}
		if !m.sel.down {
			return m, nil
		}
		row, col, ok := m.transRowAt(mm.X, mm.Y)
		if !ok {
			return m, nil
		}
		m.sel.dragTo(m.transOffset()+row, col)
		return m, nil
	case tea.MouseActionRelease:
		// Settle an in-progress input drag (a click that never moved is a
		// plain caret; a drag keeps its range), keeping the pick
		// highlighted; copy is explicit, esc clears.
		if m.dragEdit != nil {
			m.dragEdit.mouseRelease()
			m.dragEdit = nil
			return m, nil
		}
		if m.inpDrag {
			m.inp.mouseRelease()
			m.inpDrag = false
			return m, nil
		}
		// A release reports either with no button (X10-style cb=3) or
		// still carrying the released one: the alacritty/tmux SGR stream
		// sends a left release as cb=0 with the 'M' terminator, which
		// bubbletea v1 decodes as Left+Release (its own test vectors:
		// cb=0 'M' = left release), so both settle the pick. A release
		// of another button, or a Windows Terminal-style motion-as-
		// release (button-less motion), leaves it open.
		if !m.sel.down || (mm.Button != tea.MouseButtonNone && mm.Button != tea.MouseButtonLeft) {
			return m, nil
		}
		m.sel.down = false
		return m, nil
	}
	return m, nil
}

// clicksAt counts consecutive left presses within the double-click
// window on the same cell (crush's pattern, including its 2-cell click
// tolerance): 1 a fresh press, 2 a double, 3 a triple, and a further
// press in the window restarts the ladder at 1.
func (m *Model) clicksAt(x, y int) int {
	now := time.Now()
	again := !m.lastPressT.IsZero() &&
		now.Sub(m.lastPressT) < doubleClickWindow &&
		absInt(x-m.lastPressCell[0]) <= clickTolerance &&
		absInt(y-m.lastPressCell[1]) <= clickTolerance
	if again {
		m.pressCount++
	} else {
		m.pressCount = 1
	}
	if m.pressCount > 3 {
		m.pressCount = 1
	}
	m.lastPressT = now
	m.lastPressCell = [2]int{x, y}
	return m.pressCount
}

const doubleClickWindow = 400 * time.Millisecond

// clickTolerance is the crush click-ladder tolerance in cells.
const clickTolerance = 2

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// inpRowAt maps a screen cell to the main input bar's rune index
// (ok=false when the pointer is off the bar; the bar spans the full
// width from x=0, its origin was measured by the last rendered frame).
func (m *Model) inpRowAt(x, y int) (idx int, ok bool) {
	if m.inpY < 0 || m.W <= 0 || m.inpH <= 0 {
		return 0, false
	}
	if x < 0 || x >= m.W || y < m.inpY || y >= m.inpY+m.inpH {
		return 0, false
	}
	return m.inp.indexAt(m, x, y-m.inpY)
}

// plateGeom is the topmost modal's popup box origin and size in screen
// cells (the box is pasted whole-row at the screen center; the body
// starts two rows below the origin, after the frame border + title
// row). It mirrors View's painting via popupBox, so hit-testing lands
// where the box actually is.
func (m *Model) plateGeom() (x, y, bw, bh int, ok bool) {
	_, _, ww, wh, ox, oy, bok := m.popupBox()
	return ox, oy, ww, wh, bok
}

// popupBox is the on-screen geometry of the topmost modal's centered
// popup box: the box's outer size and its origin, plus the
// inner view
// width and the body height budget. It re-derives the box through the
// same sizing + pasting math as View (modalBox), so the numbers always
// describe the frame the last render would paint. ok=false: no modal.
func (m *Model) popupBox() (innerW, budget, ww, wh, ox, oy int, ok bool) {
	mod := m.topModal()
	if mod == nil {
		return 0, 0, 0, 0, 0, 0, false
	}
	_, ox, oy, ww, wh = m.modalBox(mod)
	_, budget = m.modalBoxSize()
	innerW = m.modalInnerWidth()
	return innerW, budget, ww, wh, ox, oy, true
}

// mousePressEditor routes a press to the topmost modal's edit field
// when it lands on the field's row (caret placement / drag start); it
// reports whether the press falls on the opaque plate at all, so the
// caller can drop the transcript pick hidden beneath it.
func (m *Model) mousePressEditor(x, y int) bool {
	mod := m.topModal()
	if mod == nil {
		return false
	}
	me, ok := mod.(mouseEditor)
	if !ok {
		return false
	}
	px, py, bw, bh, ok := m.plateGeom()
	if !ok {
		return false
	}
	if x < px || x >= px+bw || y < py || y >= py+bh {
		return false
	}
	m.dragEdit = nil
	for _, f := range me.mouseFields(m) {
		if f.e == nil || y != py+2+f.row {
			continue
		}
		// The field text starts one cell inside the window frame border.
		off := x - (px + 1 + f.col())
		if off < 0 {
			off = 0
		}
		f.e.mousePress(runeIndexAt(f.e.string(), off))
		m.dragEdit = f.e
		return true
	}
	// On the plate but off every field row: the plate still eats the
	// press (the transcript under it must not be picked through).
	return true
}

// editorDragAt maps a held-button motion onto a modal field's rune index
// (ok=false off the field's row: the field keeps its last position).
func (m *Model) editorDragAt(e *lineEdit, x, y int) (int, bool) {
	mod := m.topModal()
	if mod == nil {
		return 0, false
	}
	me, ok := mod.(mouseEditor)
	if !ok {
		return 0, false
	}
	px, py, _, _, ok := m.plateGeom()
	if !ok {
		return 0, false
	}
	fields := me.mouseFields(m)
	for i := range fields {
		if fields[i].e != e {
			continue
		}
		if y != py+2+fields[i].row {
			return 0, false
		}
		// The field text starts one cell inside the window frame border.
		off := x - (px + 1 + fields[i].col())
		if off < 0 {
			off = 0
		}
		return runeIndexAt(e.string(), off), true
	}
	return 0, false
}

// transRowAt maps a screen cell to a transcript pane row (-1 when the
// pointer is outside the pane). The pane origin (transX, transY) is
// measured by the last rendered frame (View); splash/mini frames leave
// transX negative and expose no pane.
func (m *Model) transRowAt(x, y int) (row, col int, ok bool) {
	if m.transX < 0 || m.W <= 0 {
		return -1, 0, false
	}
	if x < m.transX || y < m.transY {
		return -1, 0, false
	}
	col = x - m.transX
	_, transW, _ := m.paneWidths(m.W)
	if col >= transW {
		return -1, 0, false
	}
	row = y - m.transY
	if row >= m.viewportHeight() {
		return -1, 0, false
	}
	return row, col, true
}

// copyText pushes text to the clipboard: through a clipboard binary
// where one exists, else OSC 52 (crush uses a clipboard library; the
// TUI keeps the binary + OSC 52 fallback so it needs no CGO). Shared by
// the transcript pick and every input pick; the clipDoneMsg reply toasts
// the channel that carried the text.
func (m *Model) copyText(text string) tea.Cmd {
	if text == "" {
		m.addToast(core.Notice{Level: "info", Text: "copied nothing (empty pick)"})
		return nil
	}
	chars := len([]rune(text))
	if t := findClipTool(); t.path != "" {
		cmd := exec.Command(t.path, t.args...)
		cmd.Stdin = strings.NewReader(text)
		// The tool (xclip/wl-copy) may daemonize to serve the selection;
		// point its output at /dev/null so the survivor carries its own
		// fd — not the TUI's stdout, held open past the test's exit.
		// (io.Discard would not do: exec.Cmd then runs a copy goroutine
		// that waits out the surviving child's pipe end.)
		if null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0); err == nil {
			cmd.Stdout, cmd.Stderr = null, null
		}
		return tea.ExecProcess(cmd, func(err error) tea.Msg {
			return clipDoneMsg{via: baseName(t.path), chars: chars, err: err}
		})
	}
	osc := oscCmd{payload: osc52Payload(text)}
	return tea.Exec(osc, func(error) tea.Msg {
		return clipDoneMsg{via: "OSC 52", chars: chars}
	})
}

// selectionText is the active pick's plain text ("" for a zero-size
// pick): the picked rows clipped to the pick's columns and joined with
// crush's wrap-aware row joining (crush's HighlightContent).
func (m *Model) selectionText() string {
	fl, fc, ll, lc := m.sel.bounds(len(m.trans.lines))
	if fl > ll || fl < 0 { // fl < 0: zero-size (or empty content)
		return ""
	}
	w := m.transcriptWidth()
	var rows []string
	for l := fl; l <= ll; l++ {
		cs, ce := 0, plainWidth(m.trans.lines[l])
		if l == fl {
			cs = fc
		}
		if l == ll && lc < ce {
			ce = lc
		}
		rows = append(rows, rowText(m.trans.lines[l], cs, ce))
	}
	return joinRows(rows, w)
}

// bandState is the SGR state the theme's pick band paints: a one-cell
// probe render through Sel() — the same code path as the full-row
// fast band — so the partial-row re-emission matches what lipgloss
// emits for the whole-row one. Cached per theme (the pointer is stable
// for a profile).
func (m *Model) bandState() sgrState {
	if m.bandTheme != m.th {
		m.bandTheme = m.th
		m.band = extractSGR(m.th.Sel().Render("x"))
	}
	return m.band
}

// applyBand paints the pick band over the inclusive display-column range
// [cs, ce] of a styled line. A whole-line pick goes through the theme's
// one-library-pass band (lipgloss keeps the row's own styling under it);
// a partial pick re-emits the row cell by cell, merging the band state
// into each covered cell — a cell keeps its own colors under the band,
// and a wide rune belongs to the band only when its first column is
// covered (the ultraviolet cell rule crush's highlighter applies).
func (m *Model) applyBand(line string, cs, ce int) string {
	if w := plainWidth(line); cs <= 0 && ce >= w-1 {
		return m.th.Sel().Render(line)
	}
	band := m.bandState()
	var out strings.Builder
	out.Grow(len(line) + 24)
	var prev sgrState
	have := false
	for _, c := range scanStyled(line) {
		st := c.state
		if c.col >= cs && c.col <= ce {
			st = st.withBand(band)
		}
		if !have || !st.equals(prev) {
			out.WriteString(st.marshalOrReset())
			prev = st
			have = true
		}
		if c.r != 0 {
			out.WriteRune(c.r)
		}
	}
	if have && !prev.empty() {
		out.WriteString("\x1b[0m")
	}
	return out.String()
}

// copySelection turns the active transcript pick into plain text and
// copies it; the band dies with the copy's reply (crush's ClearMouse),
// until then esc and a new press clear it and ctrl+c repeats the copy.
func (m *Model) copySelection() tea.Cmd {
	m.sel.copied = true
	return m.copyText(m.selectionText())
}

// readClipboard fetches the system clipboard with a read tool (if one
// exists) and replays it as a paste message — crush's pattern — so the
// explicit paste key and the terminal's bracketed paste share one path.
// A missing tool or a read failure toasts; an empty clipboard reports
// its own clipReadMsg.
func (m *Model) readClipboard() tea.Cmd {
	t := findClipReadTool()
	if t.path == "" {
		m.addToast(core.Notice{Level: "warn", Text: "no clipboard reader (xclip / wl-paste / pbpaste)"})
		return nil
	}
	cmd := exec.Command(t.path, t.args...)
	var out strings.Builder
	cmd.Stdout = &out
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return clipReadMsg{err: err}
		}
		text := out.String()
		if strings.TrimSpace(text) == "" {
			return clipReadMsg{}
		}
		return pasteTextMsg{text}
	})
}

// handlePaste folds one paste (a bracketed paste from the terminal, or
// the readClipboard replay) into the focused field: the topmost modal's
// edit field where one is open, else the main input bar. A paste
// replaces any active pick.
func (m *Model) handlePaste(text string) (tea.Model, tea.Cmd) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if text == "" {
		return m, nil
	}
	if mod := m.topModal(); mod != nil {
		if me, ok := mod.(mouseEditor); ok && me.paste(text) {
			return m, nil
		}
	}
	// The open session window owns pastes: they land in its search line
	// (newlines fold to spaces, like the other text fields).
	if m.sideVisible {
		m.sideSearch.paste(text)
		return m, nil
	}
	m.inp.paste(text)
	m.inp.refreshMenu(m.mergedCmds)
	return m, m.refreshAtMenu()
}

// modalPickEditor returns the topmost modal's edit field when it holds
// an active pick (nil: no modal, no field, or nothing picked).
func (m *Model) modalPickEditor() *lineEdit {
	mod := m.topModal()
	if mod == nil {
		return nil
	}
	me, ok := mod.(mouseEditor)
	if !ok {
		return nil
	}
	for _, f := range me.mouseFields(m) {
		if f.e != nil && f.e.selActive() {
			return f.e
		}
	}
	return nil
}

// inpSelActive reports an active pick in focus: the topmost modal's
// edit field first, then the main input bar.
func (m *Model) inpSelActive() bool {
	if m.modalPickEditor() != nil {
		return true
	}
	return m.inp.selActive()
}

// inpCopySel copies the focused input pick to the clipboard.
func (m *Model) inpCopySel() tea.Cmd {
	if e := m.modalPickEditor(); e != nil {
		return e.copySel(m)
	}
	return m.inp.copySel(m)
}

// transOffset is the first absolute line the transcript shows this frame
// (follow-mode pin to the tail included, clamped to the content).
func (m *Model) transOffset() int {
	maxOff := m.transMaxOff()
	if m.follow {
		return maxOff
	}
	if m.scroll < 0 {
		return 0
	}
	if m.scroll > maxOff {
		return maxOff
	}
	return m.scroll
}

// scrollBy moves the transcript offset by delta lines (negative = up).
// While follow mode is on the stored offset is stale — the view shows the
// bottom — so leaving follow first anchors it to the current bottom: the
// first upward step then moves one notch from the tail instead of jumping
// to the top. Landing back on the last line re-arms follow.
func (m *Model) scrollBy(delta int) {
	if m.follow {
		m.scroll = m.transMaxOff()
		m.follow = false
		m.followBase = m.trans.total()
	}
	m.scroll += delta
	if m.scroll < 0 {
		m.scroll = 0
	}
	if max := m.transMaxOff(); m.scroll > max {
		m.scroll = max
	}
	if m.scroll == m.transMaxOff() {
		// On the last line: follow is armed so new content keeps the
		// tail pinned (the view clamps the same way).
		m.follow = true
		m.followBase = 0
	}
}

func (m *Model) transMaxOff() int {
	max := m.trans.total() - m.viewportHeight()
	if max < 0 {
		max = 0
	}
	return max
}

// viewportHeight is the real transcript pane height measured in the last
// frame; before the first render it falls back to the approximation.
func (m *Model) viewportHeight() int {
	if m.transH > 0 {
		return m.transH
	}
	return m.transcriptHeight()
}

// transcriptHeight approximates the transcript pane height (pagination unit).
func (m *Model) transcriptHeight() int {
	h := m.H - 6
	if h < 3 {
		h = 3
	}
	return h
}

// transcriptWidth is the transcript pane width (markdown wrap width).
func (m *Model) transcriptWidth() int {
	_, trans, _ := m.paneWidths(m.W)
	return trans
}

// switchSession moves the active selection by delta rows along the sidebar
// display order (workspace groups, then ungrouped), following the live
// search filter when one narrows the list.
func (m *Model) switchSession(delta int) tea.Cmd {
	ids := m.sideNavIDs()
	if len(ids) == 0 {
		return nil
	}
	idx := -1
	for i, id := range ids {
		if id == m.activeID() {
			idx = i
			break
		}
	}
	next := idx + delta
	if idx < 0 {
		next = 0
	}
	if next < 0 {
		next = 0
	}
	if next >= len(ids) {
		next = len(ids) - 1
	}
	return m.selectSession(ids[next])
}

// sideCursorIndex is the popup cursor's position in the walkable roster
// (sideNavIDs): the stored cursor id when it survives the live filter, the
// active session's row otherwise, index 0 for an empty roster. The arrows
// walk by index, so a filter that drops the cursor re-seats it on the
// active session rather than a stale slot.
func (m *Model) sideCursorIndex() int {
	ids := m.sideNavIDs()
	if len(ids) == 0 {
		return 0
	}
	if m.sideCursorID != "" {
		for i, id := range ids {
			if id == m.sideCursorID {
				return i
			}
		}
	}
	for i, id := range ids {
		if id == m.activeID() {
			return i
		}
	}
	return 0
}

// sideCursorMove walks the popup cursor by delta rows along the walkable
// roster, clamped at both ends: a move, not a pick (the active session
// changes only when space commits the cursor or enter confirms it).
func (m *Model) sideCursorMove(delta int) {
	ids := m.sideNavIDs()
	if len(ids) == 0 {
		return
	}
	idx := m.sideCursorIndex() + delta
	if idx < 0 {
		idx = 0
	}
	if idx >= len(ids) {
		idx = len(ids) - 1
	}
	m.sideCursorID = ids[idx]
}

func (m *Model) selectSession(id string) tea.Cmd {
	if id == m.activeID() {
		return nil
	}
	m.st.SetActive(id)
	m.resetTrans()
	m.follow = true
	m.scroll = 0
	m.inp.clear()
	cmds := []tea.Cmd{m.cmdLoadTail(id), m.cmdLoadModels(id)}
	// A staged mode pick reaches its receiving session when that session
	// becomes current and is still blank (the web seat rule).
	if p := m.stagedMode; p != "" {
		if snap := m.st.Get(id); snap != nil && snap.Summary.Blank && snap.Summary.AgentPreset != p {
			entry := protocol.AgentPresetEntry{Id: p}
			cmds = append(cmds, m.runCmd("mode", func(ctx context.Context) error {
				return m.applyModePick(ctx, id, &entry)
			}))
		}
	}
	if len(cmds) == 1 {
		return cmds[0]
	}
	return tea.Batch(cmds...)
}

// ---- submit / local commands -------------------------------------------------------

// submit sends the input text (queue or steer) or dispatches a local command.
func (m *Model) submit() tea.Cmd {
	text := strings.TrimSpace(m.inp.value())
	if text != "" {
		// Prompt history: everything submitted (queue, steer, slash
		// dispatch) is recallable via the up/down keys.
		m.inp.record(text)
	}
	m.inp.clear()
	m.inp.refreshMenu(nil)
	if text == "" {
		return nil
	}

	// Local slash commands.
	if strings.HasPrefix(text, "/") {
		fields := strings.Fields(strings.TrimPrefix(text, "/"))
		if len(fields) > 0 {
			if cmd, handled := m.localSlash(fields[0], strings.Join(fields[1:], " ")); handled {
				return cmd
			}
		}
	}

	id, cmd := m.ensureSession()
	if id == "" {
		m.pendingPrompt = text
		return cmd
	}
	mode := "queue"
	if m.running() {
		mode = "steer"
	}
	if m.inp.consumeForceQueue() {
		mode = "queue"
	}
	return m.runCmd("send", func(ctx context.Context) error {
		_, err := m.app.Prompt(ctx, id, text, mode)
		return err
	})
}

// ensureSession returns the active session id, creating one when needed.
// ensureSession returns the active session, auto-picking the most recent
// non-blank one (kicking off its tail load) or starting a session
// creation. The cmd is nil once a session is ready; while createMsg is
// pending the caller parks its action in pendingPrompt/pendingCycle.
func (m *Model) ensureSession() (string, tea.Cmd) {
	id := m.activeID()
	if id != "" {
		return id, nil
	}
	rows := m.st.Roster()
	for _, r := range rows {
		if !r.Blank {
			m.st.SetActive(r.Id)
			return r.Id, tea.Sequence(m.cmdLoadTail(r.Id), m.cmdLoadModels(r.Id))
		}
	}
	// Nothing to select: create one; createMsg continues the pending
	// action.
	return "", func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		m.ensureWorkspaceBaseline(ctx)
		req := m.newSessionCreateReq("")
		nid, err := m.app.CreateSession(ctx, req)
		if err != nil {
			m.st.Notify(core.Notice{Level: "err", Text: m.loc.T("toast.new.session.pref") + err.Error(), Bell: true})
			return createMsg{err: err}
		}
		return createMsg{id: nid}
	}
}

// localSlash dispatches a local command name. It reports handled=true for
// local names even when the command is a no-op.
func (m *Model) localSlash(name, rest string) (tea.Cmd, bool) {
	switch name {
	case "help":
		m.openModal(&helpModal{loc: m.loc})
		return nil, true
	case "quit", "q":
		m.quitting = true
		return tea.Quit, true
	case "status":
		// The usage popup: host line plus the token statistics (total /
		// month / week / day, per workspace, per session).
		m.openModal(newStatusModal(m.loc))
		return nil, true
	case "new":
		return m.cmdNewSession(rest), true
	case "title":
		t := rest
		if t == "" {
			snap := m.st.Get(m.activeID())
			if snap != nil {
				t = snap.Title
			}
		}
		m.openModal(&renameModal{edit: lineEdit{val: []rune(t), cur: len(t)}, loc: m.loc})
		return nil, true
	case "model":
		if rest == "" {
			return m.openModelPicker(), true
		}
		return m.cmdSelectModelByName(rest), true
	case "mode":
		if rest == "" {
			return m.openModePicker(), true
		}
		return m.cmdSelectModeByName(rest), true
	case "search":
		m.openModal(&searchModal{ed: lineEdit{val: []rune(rest), cur: len(rest)}, loc: m.loc})
		return nil, true
	case "cancel":
		id := m.activeID()
		if id == "" {
			return nil, true
		}
		return m.runCmd("interrupt", func(ctx context.Context) error {
			return m.app.Cancel(ctx, id)
		}), true
	case "detail":
		m.verbose = !m.verbose
		m.resetTrans()
		return nil, true
	case "workspace":
		return m.localSlashWorkspace(rest), true
	case "permission":
		id := m.activeID()
		if id == "" {
			return nil, true
		}
		if rest == "" {
			if snap := m.st.Get(id); snap != nil && snap.Permission != nil {
				m.addToast(core.Notice{Level: "info", Text: m.loc.T("toast.permission.prefix") + permissionLabel(snap.Permission.CurrentValue)})
			} else {
				m.addToast(core.Notice{Level: "info", Text: m.loc.T("toast.permission.unknown")})
			}
			return nil, true
		}
		return m.runPermission(id, rest), true
	case "language":
		return m.localSlashLanguage(rest), true
	}
	return nil, false
}

// localSlashLanguage handles /language [name]: a name loads that catalog
// (aliases and locale forms accepted, "en" always loads) and swaps the UI
// face; bare opens the picker window — a searchable roster of the
// available catalogs, enter on a row switches the face. A failed load
// keeps the current face and says why. The swap re-renders from a cold
// transcript cache so no baked-in English line survives.
func (m *Model) localSlashLanguage(rest string) tea.Cmd {
	name := strings.TrimSpace(rest)
	if name == "" {
		lm := newLanguageModal()
		lm.loc = m.loc
		m.openModal(lm)
		return nil
	}
	return m.loadLanguage(name)
}

// loadLanguage swaps the UI face to lang's catalog; a failed load keeps
// the current face and says why. A successful switch is persisted as the
// startup default: the chosen code goes into config.json "language" (the
// full config is read-modify-written, so the url is not lost), best-effort
// — a read-only data dir must not block the face swap.
func (m *Model) loadLanguage(name string) tea.Cmd {
	loc, _, err := i18n.Load(name)
	if err != nil {
		m.addToast(core.Notice{Level: "warn", Text: m.loc.T("lang.failed",
			name, err.Error(), m.loc.Name())})
		return nil
	}
	m.loc = loc
	m.locRef.Store(loc)
	m.resetTrans()
	if cfg := config.Load(); cfg.Language != loc.Lang {
		cfg.Language = loc.Lang
		_ = config.Save(cfg)
	}
	m.addToast(core.Notice{Level: "ok", Text: loc.T("lang.switched", loc.Name())})
	return nil
}

func (m *Model) cmdNewSession(cwd string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		m.ensureWorkspaceBaseline(ctx)
		req := m.newSessionCreateReq(cwd)
		nid, err := m.app.CreateSession(ctx, req)
		if err != nil {
			m.st.Notify(core.Notice{Level: "err", Text: "new session: " + errText(err, "new session"), Bell: true})
			return dirtyMsg{}
		}
		if req.AgentPreset != "" {
			m.st.Notify(core.Notice{Level: "ok", Text: m.loc.T("new.session.mode", m.modeLabel(req.AgentPreset))})
		}
		return createMsg{id: nid}
	}
}

// ensureWorkspaceBaseline makes sure the workspace registry has baselined
// before a new session is minted, so the host-cwd path can travel as a
// workspace id (accounted from birth). A deployment without the workspace
// domain simply keeps the raw cwd (the error is a non-fatal warn).
func (m *Model) ensureWorkspaceBaseline(ctx context.Context) {
	if !m.st.WorkspacesBaselined() {
		m.app.RefreshWorkspaces(ctx)
	}
}

// newSessionCreateReq builds a session.create payload with the
// inheritance rule: an explicit cwd (from /new [cwd]) wins, otherwise the
// new session inherits the current session's workspace, falling back to
// the host cwd. A path on a registered workspace travels as the workspace
// id (the web New-Session rule: the session is accounted in the workspace
// from birth). The mode inherits the same way: a staged pick (one the
// current session could not take in place) wins, otherwise the current
// session's preset is carried over ("" keeps the deployment default).
func (m *Model) newSessionCreateReq(cwd string) protocol.SessionCreateRequest {
	req := protocol.SessionCreateRequest{Cwd: cwd}
	if cwd == "" {
		req.Cwd = m.inheritedCwd()
	}
	if p := m.consumeStagedMode(); p != "" {
		req.AgentPreset = p
	} else if p := m.inheritedMode(); p != "" {
		req.AgentPreset = p
	}
	return req
}

// inheritedCwd is the project a fresh session inherits from the current
// session: its working directory when one is known (the summary's cwd,
// else the bound workspace's path for a session the registry owns —
// registered or raw, CreateSession resolves it to the workspace id when
// owned), otherwise the host cwd (the boot/auto-create paths, where
// nothing is active yet).
func (m *Model) inheritedCwd() string {
	if id := m.activeID(); id != "" {
		if snap := m.st.Get(id); snap != nil && snap.Summary != nil && snap.Summary.Cwd != "" {
			return snap.Summary.Cwd
		}
		if ws := m.st.WorkspaceForSession(id); ws != nil && ws.Path != "" {
			return ws.Path
		}
	}
	if h := m.st.Host(); h != nil && h.Cwd != "" {
		return h.Cwd
	}
	return ""
}

// inheritedMode is the agent preset a fresh session inherits from the
// current session ("" when it has none: the deployment default applies).
func (m *Model) inheritedMode() string {
	if id := m.activeID(); id != "" {
		if snap := m.st.Get(id); snap != nil && snap.Summary != nil {
			return snap.Summary.AgentPreset
		}
	}
	return ""
}

// cmdFork starts a new session from the source's completed-turn prefix and
// opens it. atSeq anchors the cut (the web client's per-turn branch button);
// atSeq<=0 lets the host cut at the source's last completed turn.
func (m *Model) cmdFork(id string, atSeq int64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		nid, err := m.app.Fork(ctx, id, atSeq)
		if err != nil {
			m.st.Notify(core.Notice{Level: "err", Text: m.loc.T("toast.fork.err", errText(err, "fork")), Bell: true})
			return dirtyMsg{}
		}
		m.st.Notify(core.Notice{Level: "ok", Text: m.loc.T("toast.fork.ok", shortID(nid))})
		return createMsg{id: nid}
	}
}

// forkAnchorSeq is the fork cut point for the current view position: with
// the tail pinned, atSeq 0 (the host's last completed turn — the web's
// branch button on the newest turn); scrolled up, the last completed-turn
// boundary rendered fully above the view's top row. Row heights come from
// the last rendered frame (the rows actually on screen); if the item set
// moved since the render, the host fallback keeps the cut sane.
func (m *Model) forkAnchorSeq(id string) int64 {
	if m.follow {
		return 0
	}
	snap := m.st.Get(id)
	if snap == nil || len(snap.Items) != len(m.trans.itemsRender) {
		return 0
	}
	top := m.transOffset()
	row := 0
	var anchor int64
	for i, it := range snap.Items {
		h := len(m.trans.itemsRender[i])
		if it.Kind == core.KindTurnEnd && row+h <= top {
			anchor = it.Seq
		}
		row += h
		if row >= top {
			break
		}
	}
	return anchor
}

// cmdCyclePermission advances the active session's permission preset to the
// next one in the DSH preset cycle (read-only -> workspace-write ->
// danger-full-access) by issuing the host's /permission command. The preset
// table comes from the "permissions" projection; without it the canonical
// dsh-base table is cycled.
func (m *Model) cmdCyclePermission() tea.Cmd {
	id, cmd := m.ensureSession()
	if id == "" {
		m.pendingCycle = true
		return cmd
	}
	if snap := m.st.Get(id); snap != nil && snap.Permission != nil {
		return m.cycleStep(id)
	}
	// A baseline is already in flight for this session: the queued
	// permBaselineMsg cycles once it lands (rapid presses must not stack
	// a second baseline + a second cycle).
	if m.baselinePending == id {
		return nil
	}
	// Fresh boot or a just-picked session: the permissions projection has
	// not been baselined yet (the history tail carries it). Pull the tail,
	// then cycle from the real current preset — otherwise the first presses
	// would jump to the first standard preset instead of cycling.
	m.baselinePending = id
	return m.baselineCycle(id)
}

// baselineCycle pulls one session's tail (which carries the permissions
// projection baseline), then cycles from the loaded preset.
func (m *Model) baselineCycle(id string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		m.app.LoadTail(ctx, id)
		return permBaselineMsg{id: id}
	}
}

// cycleStep switches to the preset following the session's current one. An
// unknown current value (no projection, e.g. a deployment without the
// permission plugin) starts the standard dsh-base cycle.
func (m *Model) cycleStep(id string) tea.Cmd {
	cur := ""
	var opts []protocol.PermissionOption
	if snap := m.st.Get(id); snap != nil && snap.Permission != nil {
		cur = snap.Permission.CurrentValue
		opts = snap.Permission.Options
	}
	return m.runPermission(id, core.NextPermission(cur, opts))
}

// runPermission switches one session's preset through the host /permission
// command (dsh-permission-presets). No model turn is involved, and it works
// while a turn runs. Host-level misses (no command mounted, unknown preset)
// surface as toasts through runCmd.
func (m *Model) runPermission(id, preset string) tea.Cmd {
	return m.runCmd("permission", func(ctx context.Context) error {
		outcome, matched, err := m.app.SetPermission(ctx, id, preset)
		if err != nil {
			return err
		}
		if !matched {
			return fmt.Errorf("this deployment has no /permission command")
		}
		if outcome.Kind != "success" {
			if outcome.Text != "" {
				return fmt.Errorf("%s", outcome.Text)
			}
			return fmt.Errorf("command failed")
		}
		m.st.Notify(core.Notice{Level: permissionToastLevel(preset), Text: m.loc.T("toast.permission.switched", permissionLabel(preset))})
		return nil
	})
}

// permissionLabel renders a preset value as its display label (the DSH
// product labels for full access and workspace write, title-cased kebab
// otherwise).
func permissionLabel(value string) string {
	if value == "" {
		return ""
	}
	if value == "danger-full-access" {
		return "Full Access"
	}
	if value == "workspace-write" {
		return "Workspace"
	}
	parts := strings.Split(value, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

// permissionToastLevel grades the switch: full access is a risk gate.
func permissionToastLevel(value string) string {
	if value == "danger-full-access" {
		return "warn"
	}
	return "info"
}

// ---- modals ------------------------------------------------------------------------

func (m *Model) openModal(mod modal) { m.mods = append(m.mods, mod) }

func (m *Model) topModal() modal {
	if len(m.mods) == 0 {
		return nil
	}
	return m.mods[len(m.mods)-1]
}

// toggleChord reports whether the key is the opening chord of the top
// modal — its second press closes the window.
func (m *Model) toggleChord(km tea.KeyMsg) bool {
	switch m.topModal().(type) {
	case *modelPicker:
		return km.Type == tea.KeyCtrlN
	case *modePicker:
		return km.Type == tea.KeyCtrlO
	case *workspaceModal:
		return km.Type == tea.KeyCtrlW
	}
	return false
}

// subDockRefresh fetches the child catalog when the subs tab needs it
// (stale or missing), and clamps the cursor.
func (m *Model) subDockRefresh() tea.Cmd {
	m.clampSubDockCur()
	id := m.activeID()
	if id == "" || m.subFetching {
		return nil
	}
	if _, fresh := subagentEntries(id); !fresh {
		m.subFetching = true
		return m.cmdFetchSubagents(id)
	}
	return nil
}

// clampSubDockCur keeps the subs-tab cursor inside the cached rows.
func (m *Model) clampSubDockCur() {
	id := m.activeID()
	entries, _ := subagentEntries(id)
	if m.subDockCur >= len(entries) {
		m.subDockCur = 0
	}
}

// openSubTranscript opens the read-only transcript window for the
// cursor's child (subagent.history, one page, chronological).
func (m *Model) openSubTranscript() tea.Cmd {
	id := m.activeID()
	// A stale catalog is fine here: the modal pulls its own history page,
	// and the catalog only supplies the child's identity and label.
	entries, _ := subagentEntries(id)
	if len(entries) == 0 || m.subDockCur >= len(entries) {
		return nil
	}
	e := entries[m.subDockCur]
	label := e.Label
	if label == "" {
		label = e.Id
	}
	m.openModal(&subagentModal{parent: id, child: e.Id, mode: e.Mode, label: label, loading: true, loc: m.loc})
	return m.cmdFetchSubHistory(id, e.Id, e.Mode)
}

// interruptSub interrupts the cursor's child when it is a running
// continuable (one-shots run to completion; diagnostics have nothing
// to stop).
func (m *Model) interruptSub() tea.Cmd {
	id := m.activeID()
	entries, _ := subagentEntries(id)
	if len(entries) == 0 || m.subDockCur >= len(entries) {
		return nil
	}
	e := entries[m.subDockCur]
	if e.Kind != "child" || e.Mode != "continuable" || e.Activity != "running" {
		return nil
	}
	child := e.Id
	return m.runCmd("subagent interrupt", func(ctx context.Context) error {
		return m.app.SubagentInterrupt(ctx, id, child)
	})
}

// cmdFetchSubHistory pulls one page of a child's event log off the
// event loop.
func (m *Model) cmdFetchSubHistory(parent, child, mode string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		resp, err := m.app.SubagentHistory(ctx, protocol.SubagentHistoryRequest{
			ParentSessionId: parent, ChildSessionId: child, Mode: mode, MaxMessages: 100,
		})
		var events []protocol.HistoryEntry
		if resp != nil {
			events = resp.Events
		}
		return subHistoryLoadedMsg{child: child, events: events, err: err}
	}
}

// toggleToolExpand flips the expanded state of the latest tool card in
// the active transcript — the peek path for a tool that just finished.
func (m *Model) toggleToolExpand() {
	last := m.lastToolBlock()
	if m.expandedTool == last {
		m.expandedTool = nil
	} else {
		m.expandedTool = last
	}
	m.resetTrans()
}

// lastToolBlock returns the newest tool block in the active session's
// transcript (nil when there is none).
func (m *Model) lastToolBlock() *core.ToolBlock {
	snap := m.st.Get(m.activeID())
	if snap == nil {
		return nil
	}
	for i := len(snap.Items) - 1; i >= 0; i-- {
		it := snap.Items[i]
		if it.Kind != core.KindAssistant {
			continue
		}
		for j := len(it.Blocks) - 1; j >= 0; j-- {
			if it.Blocks[j].Tool != nil {
				return it.Blocks[j].Tool
			}
		}
	}
	return nil
}

// flashTurnEnds watches every roster row's last item: a fresh turn-end
// on a session that is not the active one flashes its roster row — the
// "who just finished" landing spot (the store carries no such marker).
// First sight of a session records its seq without flashing (history
// load, not a live event).
func (m *Model) flashTurnEnds() {
	if m.flashEnds == nil {
		m.flashEnds = map[string]time.Time{}
	}
	if m.turnEndSeen == nil {
		m.turnEndSeen = map[string]int64{}
	}
	now := time.Now()
	for sid, until := range m.flashEnds {
		if now.After(until) {
			delete(m.flashEnds, sid)
		}
	}
	for _, row := range m.st.Roster() {
		snap := m.st.Get(row.Id)
		if snap == nil {
			continue
		}
		items := snap.Items
		if len(items) == 0 || items[len(items)-1].Kind != core.KindTurnEnd {
			continue
		}
		seq := items[len(items)-1].Seq
		if prev, ok := m.turnEndSeen[row.Id]; !ok {
			m.turnEndSeen[row.Id] = seq
			continue
		} else if prev == seq {
			continue
		}
		m.turnEndSeen[row.Id] = seq
		if row.Id != m.activeID() {
			m.flashEnds[row.Id] = now.Add(1500 * time.Millisecond)
		}
	}
}

// surfacePending opens the active session's next parked answerable
// frame — a question batch before a tool approval, mirroring the web
// client's pending-interaction surfacing (the answer blocks the running
// turn, so the UI must not bury it). Auto mode (a dirty pulse) opens
// each rpcId at most once; manual mode (ctrl+i) reopens frames the user
// esc-closed. It never stacks on an already-open modal.
func (m *Model) surfacePending(auto bool) bool {
	if len(m.mods) > 0 {
		return false
	}
	id := m.activeID()
	if id == "" {
		return false
	}
	snap := m.st.Get(id)
	if snap == nil {
		return false
	}
	if len(snap.Questions) > 0 {
		qs := append([]*core.QuestionPend(nil), snap.Questions...)
		sort.Slice(qs, func(i, j int) bool { return qs[i].RpcId < qs[j].RpcId })
		for _, q := range qs {
			if auto && m.openedInter[q.RpcId] {
				continue
			}
			m.openedInter[q.RpcId] = true
			qm := newQuestionModal(q)
			qm.loc = m.loc
			m.openModal(qm)
			return true
		}
	}
	aps := append([]*core.ApprovalPend(nil), snap.Approvals...)
	sort.Slice(aps, func(i, j int) bool { return aps[i].RpcId < aps[j].RpcId })
	for _, a := range aps {
		if auto && m.openedInter[a.RpcId] {
			continue
		}
		m.openedInter[a.RpcId] = true
		m.openModal(&approvalModal{pen: a, loc: m.loc})
		return true
	}
	return false
}

// openModelPicker loads the catalog and opens the picker.
func (m *Model) openModelPicker() tea.Cmd {
	id := m.activeID()
	if id == "" {
		m.addToast(core.Notice{Level: "info", Text: "no session — create one first"})
		return nil
	}
	pk := newModelPicker()
	pk.loc = m.loc
	pk.loading = true
	m.openModal(pk)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		models, err := m.app.Models(ctx, id)
		if err != nil {
			// The picker is still the top modal on the event loop; the
			// message handler applies this (the worker must not touch it).
			return modelsLoadedMsg{id: id, err: errText(err, "models")}
		}
		return modelsLoadedMsg{id: id, models: models}
	}
}

// cmdSelectModelByName switches the active session to a model named by
// /model <name>: the catalog is fetched, the name resolved against it (see
// resolveModelName), and the selection applied with the host's default
// reasoning effort.
func (m *Model) cmdSelectModelByName(name string) tea.Cmd {
	id := m.activeID()
	if id == "" {
		m.addToast(core.Notice{Level: "info", Text: "no session — create one first"})
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		models, err := m.app.Models(ctx, id)
		if err != nil {
			m.st.Notify(core.Notice{Level: errLevel(err), Text: errText(err, "models"), Bell: true})
			return rpcErrMsg{op: "models", err: err}
		}
		provider, modelID, problem := resolveModelName(models, name)
		if problem != "" {
			m.st.Notify(core.Notice{Level: "warn", Text: "model: " + problem})
			return dirtyMsg{}
		}
		sel, err := m.app.SelectModel(ctx, id, provider, modelID, "")
		if err != nil {
			m.st.Notify(core.Notice{Level: errLevel(err), Text: errText(err, "select model"), Bell: true})
			return rpcErrMsg{op: "select model", err: err}
		}
		if sel == nil {
			sel = &protocol.ModelSelection{Provider: provider, Model: modelID}
		}
		m.st.ApplyModelSelection(id, sel)
		m.st.Notify(core.Notice{Level: "ok", Text: "model: " + provider + "/" + modelID})
		return dirtyMsg{}
	}
}

type modelCandidate struct{ provider, id, display string }

// resolveModelName resolves a /model argument against the advertised
// catalog. Accepted forms: "provider/model-id" (or provider/name), an
// exact model id, an exact display name, a case-insensitive id, or a
// unique id prefix. Failures return a user-facing reason — ambiguity is
// reported with the full provider/model candidates.
func resolveModelName(models *protocol.SessionModels, name string) (provider, modelID, problem string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "", "empty model name"
	}
	var cands []modelCandidate
	for _, g := range models.Groups {
		for _, mo := range g.Models {
			cands = append(cands, modelCandidate{g.Id, mo.Id, mo.Name})
		}
	}
	match := func(pred func(c modelCandidate) bool) (modelCandidate, bool) {
		var hit modelCandidate
		n := 0
		for _, c := range cands {
			if pred(c) {
				if n > 0 {
					return modelCandidate{}, false
				}
				hit, n = c, 1
			}
		}
		return hit, n == 1
	}
	list := func(pred func(c modelCandidate) bool) string {
		var out []string
		for _, c := range cands {
			if pred(c) {
				out = append(out, c.provider+"/"+c.id)
			}
		}
		return strings.Join(out, ", ")
	}
	// "provider/model" form.
	if i := strings.IndexByte(name, '/'); i > 0 {
		p, rest := name[:i], name[i+1:]
		if c, ok := match(func(c modelCandidate) bool {
			return strings.EqualFold(c.provider, p) &&
				(strings.EqualFold(c.id, rest) || (c.display != "" && c.display == rest))
		}); ok {
			return c.provider, c.id, ""
		}
		if list(func(c modelCandidate) bool { return strings.EqualFold(c.provider, p) }) != "" {
			return "", "", fmt.Sprintf("no model %q under provider %q", rest, p)
		}
		return "", "", fmt.Sprintf("unknown provider %q", p)
	}
	if c, ok := match(func(c modelCandidate) bool { return c.id == name }); ok {
		return c.provider, c.id, ""
	}
	if c, ok := match(func(c modelCandidate) bool { return c.display != "" && c.display == name }); ok {
		return c.provider, c.id, ""
	}
	if c, ok := match(func(c modelCandidate) bool { return strings.EqualFold(c.id, name) }); ok {
		return c.provider, c.id, ""
	}
	if len(name) >= 3 {
		if c, ok := match(func(c modelCandidate) bool { return strings.HasPrefix(c.id, name) }); ok {
			return c.provider, c.id, ""
		}
	}
	if hits := list(func(c modelCandidate) bool {
		return strings.Contains(strings.ToLower(c.id), strings.ToLower(name))
	}); hits != "" {
		return "", "", fmt.Sprintf("matches %s — be more specific", hits)
	}
	return "", "", "no match (run /model to browse)"
}

// handleModalKey routes a key to the top modal and applies its outcome.
func (m *Model) handleModalKey(km tea.KeyMsg) (tea.Cmd, bool) {
	mod := m.topModal()
	// The help window's esc is two-step (clear the search first, then
	// close): remember whether a search was open before update ran.
	helpSearchOpen := false
	if h, ok := mod.(*helpModal); ok {
		helpSearchOpen = h.ed.string() != ""
	}
	// The workspace browser's esc closes only from the pick face; from
	// the add/rename/delete sub-modes it goes back to that face instead
	// — remember the mode before update runs (the back-switch happens
	// inside update, so the post-update mode would read wsPick too).
	var wsModeBefore wsMode
	if w, ok := mod.(*workspaceModal); ok {
		wsModeBefore = w.mode
	}
	cmd, handled := mod.update(km)
	if !handled {
		return cmd, false
	}
	switch mod := mod.(type) {
	case *helpModal:
		// Only the closing chords close the window: arrows and search
		// edits keep it open (the window owns the keyboard).
		switch {
		case km.Type == tea.KeyEnter || km.Type == tea.KeyCtrlH ||
			km.Type == tea.KeyRunes && (km.String() == "h" || km.String() == "?"):
			m.closeModal()
		case km.Type == tea.KeyEsc && !helpSearchOpen:
			m.closeModal()
		}
	case *approvalModal:
		allow := km.Type == tea.KeyEnter || km.Type == tea.KeyCtrlA ||
			km.Type == tea.KeyRunes && km.String() == "a"
		// bash-family commands take a second confirming allow: the first
		// a arms (the hint flips), the second a fires; a stray key
		// disarms. Non-shell approvals decide on the first key.
		if allow && mod.bashLike() {
			if mod.armed {
				mod.armed = false
			} else {
				mod.armed = true
				return nil, false // armed, still open: absorbed by the rule below
			}
		}
		if !allow && mod.armed {
			mod.armed = false
			return nil, false // a stray key while armed: just disarmed
		}
		decide := "rejected"
		if allow {
			decide = "allowed-once"
		}
		m.closeModal()
		pen := mod.pen
		return m.runCmd("approval", func(ctx context.Context) error {
			return m.app.Client().Respond(ctx, pen.RpcId, protocol.ApprovalResponse{
				SessionId: pen.SessionId, ApprovalId: pen.ApprovalId, Outcome: decide,
			})
		}), true
	case *questionModal:
		if mod.sub {
			m.closeModal()
			qs := mod
			ans := qs.answers()
			pen := qs.pen
			return m.runCmd("answer", func(ctx context.Context) error {
				return m.app.Client().Respond(ctx, pen.RpcId, ans)
			}), true
		}
		if km.Type == tea.KeyEsc {
			m.closeModal()
			// Parked, not answered: the batch stays in the store; the
			// queue strip and ctrl+i keep it reachable.
			m.addToast(core.Notice{Level: "info", Text: m.loc.T("question.parked")})
		}
	case *modelPicker:
		if km.Type == tea.KeyEnter {
			if mod.loading {
				// The catalog is still in flight: confirming now would
				// index an empty row list.
				return nil, true
			}
			provider, modelID, effort := mod.selected()
			if modelID == "" {
				// No selectable row (error face or empty catalog): Enter
				// closes, like esc.
				m.closeModal()
				return nil, true
			}
			id := m.activeID()
			m.closeModal()
			return m.runCmd("select model", func(ctx context.Context) error {
				sel, err := m.app.SelectModel(ctx, id, provider, modelID, effort)
				if err == nil {
					if sel == nil {
						sel = &protocol.ModelSelection{Provider: provider, Model: modelID, ReasoningEffort: effort}
					}
					m.st.ApplyModelSelection(id, sel)
					m.st.Notify(core.Notice{Level: "ok", Text: "model: " + provider + "/" + modelID + effortSuffix(effort)})
				}
				return err
			}), true
		}
		if km.Type == tea.KeyEsc {
			m.closeModal()
		}
	case *modePicker:
		if km.Type == tea.KeyEnter {
			if len(mod.presets) > 0 && mod.cur < len(mod.presets) {
				entry := mod.presets[mod.cur]
				id := m.activeID()
				if entry.Broken != "" {
					m.addToast(core.Notice{Level: "warn", Text: "mode " + modes.Name(entry) + " broken: " + entry.Broken})
					return nil, true
				}
				m.closeModal()
				return m.runCmd("mode", func(ctx context.Context) error {
					return m.applyModePick(ctx, id, &entry)
				}), true
			}
			return nil, true
		}
		if km.Type == tea.KeyEsc {
			m.closeModal()
		}
	case *renameModal:
		if km.Type == tea.KeyEnter {
			title := strings.TrimSpace(mod.edit.string())
			if title == "" {
				mod.err = "title may not be empty"
				return nil, true
			}
			id := m.activeID()
			m.closeModal()
			return m.runCmd("rename", func(ctx context.Context) error {
				t, err := m.app.Rename(ctx, id, title)
				if err == nil {
					m.st.Notify(core.Notice{Level: "ok", Text: "titled: " + t})
				}
				return err
			}), true
		}
		if km.Type == tea.KeyEsc {
			m.closeModal()
		}
	case *workspaceModal:
		// Only the commit keys act on the wire: Enter, plus y while the
		// delete confirmation is open (it is a rune, not a chord). Every
		// other handled key merely adjusts the modal's state — plain
		// typing in the add/rename lines must not submit.
		if km.Type == tea.KeyEnter ||
			(mod.mode == wsDelete && km.Type == tea.KeyRunes && km.String() == "y") {
			return m.workspacePerform(mod)
		}
		// esc closes from the pick face; the same key in the sub-modes
		// just went back to it (update already switched the mode).
		if km.Type == tea.KeyEsc && wsModeBefore == wsPick {
			m.closeModal()
		}
		return nil, true
	case *subagentModal:
		// A read-only page: any of its handled chords just closes it.
		if km.Type == tea.KeyEsc || km.Type == tea.KeyEnter || km.Type == tea.KeyCtrlH {
			m.closeModal()
		}
	case *statusModal:
		// Only the closing chords close the window (the modal's update
		// consumes them); the walking keys just adjust section/cursor.
		if km.Type == tea.KeyEsc || km.Type == tea.KeyEnter ||
			(km.Type == tea.KeyRunes && km.String() == "q") {
			m.closeModal()
		}
		return nil, true
	case *languageModal:
		if km.Type == tea.KeyEnter {
			rows := mod.filtered()
			if len(rows) > 0 && mod.cur < len(rows) {
				code := rows[mod.cur].code
				m.closeModal()
				if code != m.loc.Lang {
					// Confirming the current face is a no-op: just close.
					return m.loadLanguage(code), true
				}
			}
			// No surviving row (a query matched nothing): stay open on
			// the empty face.
			return nil, true
		}
		if km.Type == tea.KeyEsc {
			// The modal's update already cleared the query on a first
			// esc (and stayed open); esc reaches here with it empty.
			m.closeModal()
		}
	case *searchModal:
		switch km.Type {
		case tea.KeyEnter:
			if len(mod.res) > 0 && mod.cur < len(mod.res) {
				target := mod.res[mod.cur].SessionId
				m.closeModal()
				return m.selectSession(target), true
			}
		case tea.KeyEsc:
			m.closeModal()
		default:
			// Debounce the query in a worker (searchCmd): the model
			// goroutine must not block on the sleep or the RPC.
			q := strings.TrimSpace(mod.ed.string())
			if mod.cur >= len(mod.res) && len(q) >= 2 {
				mod.load = true
				return m.searchCmd(q), true
			}
		}
	}
	return cmd, true
}

// searchCmd runs the debounced search in a worker: the 250ms debounce
// sleep and the 15s-bounded RPC used to run on the model goroutine
// (a keystroke past two chars stalled every key and frame). The answer
// comes back as searchLoadedMsg and is applied only while the query still
// matches the box.
func (m *Model) searchCmd(q string) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(250 * time.Millisecond)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		res, err := m.app.Search(ctx, q)
		return searchLoadedMsg{query: q, res: res, err: err}
	}
}

func (m *Model) closeModal() {
	m.mods = m.mods[:len(m.mods)-1]
}

// ---- merged slash commands -----------------------------------------------------------

// refreshCmds merges local commands with the session's skills (cached).
// refreshCmds merges the local command list with the active session's
// skill names. The caller is on the dirty-pulse hot path, so the catalog
// fetch runs as a tea.Cmd (cmdFetchSkills) instead of synchronously; a
// failed fetch is negatively cached to keep it from blocking every pulse.
// It returns the fetch to schedule, if a refresh is due and none is in
// flight.
func (m *Model) refreshCmds() tea.Cmd {
	cmds := make([]slashCmd, 0, len(localCommandsFor(m.loc))+16)
	cmds = append(cmds, localCommandsFor(m.loc)...)
	var skillCmd tea.Cmd
	id := m.activeID()
	if id != "" {
		if skills, fresh := skillsCacheFresh(id); fresh {
			for _, sk := range skills {
				desc := sk.Description
				if desc == "" {
					desc = sk.WhenToUse
				}
				cmds = append(cmds, slashCmd{Name: sk.Name, Desc: desc})
			}
		} else if !m.skillsFetching {
			m.skillsFetching = true
			skillCmd = m.cmdFetchSkills(id)
		}
	}
	m.mergedCmds = cmds
	if skillCmd != nil {
		return skillCmd
	}
	return nil
}

// cmdFetchSkills pulls the skill catalog off the event loop.
func (m *Model) cmdFetchSkills(id string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		skills, err := m.app.Client().Skills(ctx, id)
		return skillsLoadedMsg{id: id, skills: skills, err: err}
	}
}

// The subagent catalog (running children) feeds the @ completion and the
// dock roster, cached per session. A failed fetch keeps its previous rows
// but is never fresh, so a blip cannot fake an empty list.
const (
	subagentsCacheTTL = 2 * time.Second
)

type subagentCacheEntry struct {
	at      time.Time
	entries []protocol.SubagentListEntry
	ok      bool
}

var subagentCache = map[string]subagentCacheEntry{}

// atMenuItem is one @ completion candidate: a running child's label or a
// cwd-relative path (directories carry a trailing slash).
type atMenuItem struct {
	Text string // inserted on completion
	Kind string // "child" | "path"
	Info string // dimmed suffix (activity / "dir")
}

// subagentEntries returns the cached child catalog (entries, fresh).
func subagentEntries(id string) ([]protocol.SubagentListEntry, bool) {
	e, ok := subagentCache[id]
	if !ok {
		return nil, false
	}
	// A failed fetch is never fresh: it may not claim "no children", and
	// the pulse re-fetches on the normal cadence while the tab is open.
	if !e.ok {
		return e.entries, false
	}
	if time.Since(e.at) > subagentsCacheTTL {
		// Past the window the rows are stale, not gone: the dock and the
		// @ menu keep showing the last catalog while the pulse re-fetches.
		return e.entries, false
	}
	return e.entries, true
}

type subagentsLoadedMsg struct {
	id      string
	entries []protocol.SubagentListEntry
	err     error
}

type subHistoryLoadedMsg struct {
	child  string
	events []protocol.HistoryEntry
	err    error
}

// cmdFetchSubagents pulls the active session's child catalog off the
// event loop.
// refreshAtMenu recomputes the @ completion from the current text: running
// children first, then cwd paths under the token. It returns the fetch to
// schedule when the child catalog is stale.
func (m *Model) refreshAtMenu() tea.Cmd {
	in := m.inp
	pos, token := in.atToken()
	if pos < 0 {
		in.closeAt()
		return nil
	}
	in.atPos = pos
	id := m.activeID()
	var items []atMenuItem
	fresh := false
	if id != "" {
		// Stale rows still seed the menu (the fetch below re-syncs them);
		// a cold cache waits for the fetch before children appear.
		if entries, f := subagentEntries(id); f || len(entries) > 0 {
			fresh = f
			for _, e := range entries {
				if e.Kind != "child" {
					continue
				}
				label := e.Label
				if label == "" {
					label = e.Id
				}
				if !strings.HasPrefix(label, token) {
					continue
				}
				info := ""
				if e.Activity == "running" {
					info = m.loc.T("at.running")
				}
				items = append(items, atMenuItem{Text: label, Kind: "child", Info: info})
			}
		}
		if snap := m.st.Get(id); snap != nil {
			for _, p := range atPaths(snap.Summary.Cwd, token) {
				info := ""
				if strings.HasSuffix(p, "/") {
					info = m.loc.T("at.dir")
				}
				items = append(items, atMenuItem{Text: p, Kind: "path", Info: info})
			}
		}
	}
	in.atMenu = items
	in.atOpen = len(items) > 0
	if in.atOpen {
		if in.atCur >= len(items) {
			in.atCur = len(items) - 1
		}
	}
	if in.atOpen && id != "" && !fresh && !m.subFetching {
		m.subFetching = true
		return m.cmdFetchSubagents(id)
	}
	return nil
}

// atPaths lists cwd-relative candidates under root matching the token:
// a bounded walk (three levels, 800 entries, hidden and heavy dirs
// skipped), directories completed with a trailing slash.
func atPaths(root, token string) []string {
	if root == "" {
		return nil
	}
	var out []string
	seen := 0
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if seen >= 800 || depth > 3 {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if seen >= 800 {
				return
			}
			name := e.Name()
			if name == "." || strings.HasPrefix(name, ".") ||
				name == "node_modules" || name == "vendor" || name == "target" {
				continue
			}
			seen++
			rel, err := filepath.Rel(root, filepath.Join(dir, name))
			if err != nil {
				continue
			}
			if e.IsDir() {
				rel += "/"
			}
			if strings.HasPrefix(rel, token) {
				out = append(out, rel)
			}
			if e.IsDir() {
				walk(filepath.Join(dir, name), depth+1)
			}
		}
	}
	walk(root, 1)
	sort.Strings(out)
	if len(out) > 50 {
		out = out[:50]
	}
	return out
}

func (m *Model) cmdFetchSubagents(id string) tea.Cmd {
	return func() tea.Msg {
		// 15s: the host walks its session roster for the catalog, which
		// outruns a 5s budget on a loaded host and fakes an empty list.
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cat, err := m.app.Client().Subagents(ctx, id)
		var entries []protocol.SubagentListEntry
		if cat != nil {
			entries = cat.Entries
		}
		return subagentsLoadedMsg{id: id, entries: entries, err: err}
	}
}

// The skill catalog is cached per session with a success TTL and a
// separate (shorter) negative TTL: a failed skill.list must not block the
// dirty-pulse hot path with a fetch on every pulse.
const (
	skillsCacheTTL = time.Minute
	skillsNegTTL   = 30 * time.Second
)

var skillsCache = map[string]skillCacheEntry{}

type skillCacheEntry struct {
	at     time.Time
	err    bool
	skills []protocol.SkillEntry
}

// skillsCacheFresh reports a usable entry: a success within
// skillsCacheTTL, or a failure within skillsNegTTL (then the list is
// empty but the entry still counts as fresh).
func skillsCacheFresh(id string) ([]protocol.SkillEntry, bool) {
	e, ok := skillsCache[id]
	if !ok {
		return nil, false
	}
	ttl := skillsCacheTTL
	if e.err {
		ttl = skillsNegTTL
	}
	if time.Since(e.at) > ttl {
		return nil, false
	}
	return e.skills, true
}

func skillsCachePut(id string, skills []protocol.SkillEntry, err error) {
	skillsCache[id] = skillCacheEntry{at: time.Now(), err: err != nil, skills: skills}
}

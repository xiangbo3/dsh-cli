// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package core

import (
	"encoding/json"
	"path"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"dsh-cli/internal/protocol"
	"dsh-cli/internal/textutil"
)

// Notice is a user-facing toast/bell event.
type Notice struct {
	Level string // "info" | "ok" | "warn" | "err"
	Text  string
	Sess  string
	Bell  bool
	Life  time.Duration // toast lifetime (0 = UI default; longer for boot-grade hints)
}

// TurnEndInfo is the flash payload shown when a turn finishes.
type TurnEndInfo struct {
	Kind   string // protocol TurnEndReason.Kind
	Reason string
	Sess   string
	Ms     int64
	In     int // input plus cache reads (this turn)
	Out    int
	Cache  int // the re-read cache share of In
	At     time.Time
	Error  string
}

// ApprovalPend is one pending approval frame awaiting a client response.
type ApprovalPend struct {
	RpcId      string
	SessionId  string
	ApprovalId string
	ToolName   string
	CallId     string
	Reason     string
	Args       string
}

// QuestionPend is one pending ask-user batch.
type QuestionPend struct {
	RpcId     string
	SessionId string
	Questions []protocol.QuestionItem
}

// Sess is the per-session live state.
type Sess struct {
	S *protocol.SessionSummary
	T *Transcript

	Running     bool
	Todos       []protocol.TodoItem
	Goal        *protocol.GoalProjected
	Permission  *protocol.PermissionProjection // "permissions" projection (current preset + table)
	PlanMode    bool
	Ctx         *protocol.RequestContextData
	CtxPressure *ContextPressure         // "contextPressure" projection (live footprint)
	ModelSel    *protocol.ModelSelection // last applied model selection (name + effort)
	Models      *protocol.SessionModels  // advertised model catalog (id -> display name for the top bar)
	Queue       []protocol.QueuedInboxItem
	Jobs        []protocol.JobView
	HasMore     bool
	MaxMsgsOld  int64 // seq passed to the last LoadOlder (pagination cursor)

	proj      map[string]projVal
	title     string // last session/title event (projection fallback)
	titleProj string // cached decode of the "title" projection (write-through, avoids a json.Unmarshal per titleLocked call)
	approvals map[string]*ApprovalPend
	questions map[string]*QuestionPend
	flash     *TurnEndInfo
	fresh     bool // activity while inactive (list dot)
}

type projVal struct {
	Seq int64
	V   json.RawMessage
}

// Store is the shared state engine. The UI reads through RLock; writers are
// the RPC facade and the downlink pump.
type Store struct {
	mu sync.RWMutex

	base         string
	host         *protocol.HostDescription
	sessions     map[string]*Sess
	order        []string
	active       string
	connected    bool
	rosterListed bool // session.list has answered once (baseline landed)
	rosterCashed bool // boot: the previously persisted roster is installed (stale-while-revalidate; the live baseline overwrites it)

	// Workspace registry (workspace.list baseline + host-stream frames).
	workspaces map[string]*protocol.WorkspaceView
	wsOrder    []string
	archived   map[string]bool
	wsListed   bool
	wsCashed   bool              // boot: the previously persisted registry is installed
	wsBind     map[string]string // session → workspace seeded at creation (authoritative membership before the registry row lists it)

	dirty   chan struct{}
	notices chan Notice
	turnEnd chan *TurnEndInfo

	// turnEndTexter, when set, renders the turn-end toast copy on the
	// store's own goroutine (the UI installs a locale-aware one; it is
	// read through an atomic pointer because the downlink may run while
	// the UI swaps languages).
	turnEndTexter atomic.Pointer[TurnEndTexter]

	// activeChanged, when set, points the transcript follow stream at
	// the focused session (the app installs it; the downlink may run
	// while the UI swaps focus, so it is read atomically).
	activeChanged atomic.Pointer[ActiveChangedFn]

	// rev is bumped by every pushDirty: the UI compares it to skip the
	// transcript re-sync when a frame renders without a state mutation in
	// between (the idle-frame short-circuit).
	rev uint64
}

// TurnEndTexter renders one turn-end toast line.
type TurnEndTexter func(*TurnEndInfo) string

// SetTurnEndTexter installs the toast renderer (nil for the built-in
// English copy).
func (s *Store) SetTurnEndTexter(f TurnEndTexter) {
	if f == nil {
		s.turnEndTexter.Store(nil)
		return
	}
	s.turnEndTexter.Store(&f)
}

func (s *Store) formatTurnEnd(i *TurnEndInfo) string {
	if p := s.turnEndTexter.Load(); p != nil {
		return (*p)(i)
	}
	return turnEndText(i)
}

// NewStore builds an empty store.
func NewStore(base string) *Store {
	return &Store{
		base:       base,
		sessions:   map[string]*Sess{},
		workspaces: map[string]*protocol.WorkspaceView{},
		wsBind:     map[string]string{},
		archived:   map[string]bool{},
		dirty:      make(chan struct{}, 64),
		notices:    make(chan Notice, 64),
		turnEnd:    make(chan *TurnEndInfo, 16),
	}
}

// Dirty yields a pulse on every state mutation (coalesced).
func (s *Store) Dirty() <-chan struct{} { return s.dirty }

// Notices yields toast/bell events.
func (s *Store) Notices() <-chan Notice { return s.notices }

// EndOfTurn yields turn-end events for any session (one-shot waiter).
func (s *Store) EndOfTurn() <-chan *TurnEndInfo { return s.turnEnd }

func (s *Store) pushDirty() {
	s.rev++
	select {
	case s.dirty <- struct{}{}:
	default:
	}
}

// Rev returns the mutation counter (one step per pushed dirty pulse);
// readers use it to tell "state moved since my last render" apart from
// idle frames without holding the render cache open.
func (s *Store) Rev() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.rev
}

// PushDirty exposes the mutation pulse for writers outside this package.
func (s *Store) PushDirty() { s.pushDirty() }

func (s *Store) pushNotice(n Notice) {
	// Host-shaped text (errors, reasons) arrives here from JSON: strip
	// control runes once at the ingress so no toast path can inject ANSI/VT
	// into the terminal.
	n.Text = textutil.StripControl(textutil.StripANSI(n.Text))
	select {
	case s.notices <- n:
	default:
	}
}

// BaseURL returns the configured server base.
func (s *Store) BaseURL() string { return s.base }

// Host returns the describe snapshot, if known.
func (s *Store) Host() *protocol.HostDescription {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.host
}

// Connected reports downlink health.
func (s *Store) Connected() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.connected
}

// SetConnected flips downlink health.
func (s *Store) SetConnected(up bool) {
	s.mu.Lock()
	if s.connected != up {
		s.connected = up
		s.pushDirty()
	}
	s.mu.Unlock()
}

// SetHost stores the describe snapshot.
func (s *Store) SetHost(h *protocol.HostDescription) {
	s.mu.Lock()
	s.host = h
	s.pushDirty()
	s.mu.Unlock()
}

// Sess gets or registers one session row.
func (s *Store) Sess(id string) *Sess {
	st, ok := s.sessions[id]
	if !ok {
		st = &Sess{
			S:         &protocol.SessionSummary{SessionId: id},
			T:         NewTranscript(),
			proj:      map[string]projVal{},
			approvals: map[string]*ApprovalPend{},
			questions: map[string]*QuestionPend{},
		}
		s.sessions[id] = st
		s.order = append(s.order, id)
	}
	return st
}

// Order returns session ids in list order (updatedAt desc, as provided).
func (s *Store) Order() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, len(s.order))
	copy(out, s.order)
	return out
}

// Active returns the active session id ("" if none).
func (s *Store) Active() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active
}

// SetActive switches the UI focus session.
func (s *Store) SetActive(id string) {
	s.mu.Lock()
	changed := false
	if old := s.active; old != id {
		if os := s.sessions[old]; os != nil {
			os.fresh = false
		}
		if st := s.sessions[id]; st != nil {
			st.fresh = false
		}
		s.active = id
		changed = true
		s.pushDirty()
	}
	s.mu.Unlock()
	if changed {
		if p := s.activeChanged.Load(); p != nil {
			(*p)(id)
		}
	}
}

// ActiveChangedFn fires with the new focus id ("" when cleared).
type ActiveChangedFn func(id string)

// SetActiveChanged installs the focus-change hook (the app points the
// transcript follow stream at the focused session; nil detaches). It is
// read through an atomic pointer because the downlink may run while the
// UI swaps focus.
func (s *Store) SetActiveChanged(f ActiveChangedFn) {
	if f == nil {
		s.activeChanged.Store(nil)
		return
	}
	s.activeChanged.Store(&f)
}

// TouchActivity applies one activity push: the row's updatedAt moves to
// the newer stamp and the row surfaces toward the list head (the roster
// is updatedAt-descending; the new host reports activity between the
// session.list refreshes the legacy build relied on).
func (s *Store) TouchActivity(id string, updatedAt int64) {
	s.mu.Lock()
	st := s.Sess(id)
	if updatedAt <= st.S.UpdatedAt {
		s.mu.Unlock()
		return
	}
	st.S.UpdatedAt = updatedAt
	// The roster is updatedAt-descending: re-sort so the touched row
	// sits where its new stamp ranks (a peer with a newer stamp stays
	// ahead of it). Stable, so equal stamps keep their order.
	if i := s.indexOfOrder(id); i < 0 {
		s.order = append([]string{id}, s.order...)
	} else {
		s.order = s.resortOrder()
	}
	s.pushDirty()
	s.mu.Unlock()
}

// SetSessions rebaselines the roster from session.list.
// Titles/summaries refresh; per-session live state survives.
func (s *Store) SetSessions(items []protocol.SessionSummary) {
	s.mu.Lock()
	s.rosterListed = true
	newOrder := make([]string, 0, len(items))
	for _, it := range items {
		st := s.Sess(it.SessionId)
		cp := *st.S
		mergeSummary(&cp, &it)
		st.S = &cp
		// Re-baseline the authoritative liveness flag from the live list.
		// mergeSummary only rewrites the summary (st.S.Running); the store
		// flag st.Running — the one Snapshot exposes and the top bar /
		// running() read — must be re-baselined here too. The boot cache's
		// Running is stale-while-revalidate and this list is the
		// revalidation: a WS status frame fires only on change, so an idle
		// session the cache recorded as running would otherwise stay
		// "running" until a turn event that never comes.
		st.Running = it.Running
		// The roster row carries the session's projection baseline (the
		// history block shape, asOfSeq + values): decode it so titles,
		// permissions and context pressure are known the moment the
		// roster lands, without waiting for the per-session tail load.
		if len(it.Projections) > 0 {
			var pb protocol.ProjectionsBlock
			if err := json.Unmarshal(it.Projections, &pb); err == nil {
				applyProjectionValues(st, pb.AsOfSeq, pb.Values)
			}
		}
		newOrder = append(newOrder, it.SessionId)
	}
	// Drop rows the host no longer lists (they were removed).
	alive := map[string]bool{}
	for _, id := range newOrder {
		alive[id] = true
	}
	for id := range s.sessions {
		if !alive[id] {
			delete(s.sessions, id)
		}
	}
	s.order = newOrder
	if s.active != "" {
		if !alive[s.active] {
			s.active = ""
		}
	}
	s.pushDirty()
	s.mu.Unlock()
}

func mergeSummary(dst, src *protocol.SessionSummary) {
	if src.SessionId != "" {
		dst.SessionId = src.SessionId
	}
	dst.UpdatedAt = src.UpdatedAt
	dst.Running = src.Running
	dst.Blank = src.Blank
	dst.ParentSessionId = src.ParentSessionId
	dst.Origin = src.Origin
	dst.Cwd = src.Cwd
	dst.AgentPreset = src.AgentPreset
	dst.Projections = src.Projections
}

// applyProjectionValues seeds the projection state from a baseline block
// (the tail page or the roster row's projections): higher-seq-wins against
// live frames already applied, the same rule the wire contract uses for
// baselines. Title/goal/permissions/contextPressure decode through their
// write-through mirrors, like the live "title" event path.
func applyProjectionValues(st *Sess, seq int64, values map[string]json.RawMessage) {
	for k, v := range values {
		if pv, ok := st.proj[k]; ok && pv.Seq > seq {
			continue
		}
		st.proj[k] = projVal{Seq: seq, V: v}
		switch k {
		case "title":
			st.titleProj = decodeTitle(v)
		case "agentPreset":
			// The upgraded host carries the session preset here (its
			// roster rows have no top-level field): the summary value
			// feeds the /mode picker current mark and /new inheritance.
			var p string
			if json.Unmarshal(v, &p) == nil {
				st.S.AgentPreset = p
			}
		}
	}
	if pv, ok := st.proj["goal"]; ok {
		st.Goal = decodeGoal(pv.V)
	}
	if pv, ok := st.proj["permissions"]; ok {
		st.Permission = decodePermission(pv.V)
	}
	if pv, ok := st.proj["contextPressure"]; ok {
		st.CtxPressure = decodeContextPressure(pv.V)
	}
}

// setSummary applies f to a private copy of the session summary and
// republishes the pointer: snapshots taken earlier keep their immutable
// copy (the COW contract behind Snapshot.Summary).
func setSummary(st *Sess, f func(s *protocol.SessionSummary)) {
	cp := *st.S
	f(&cp)
	st.S = &cp
}

// TitleFor resolves the best-known title for a session.
func (s *Store) TitleFor(id string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := s.sessions[id]
	if st == nil {
		return ""
	}
	return s.titleLocked(st)
}

// decodeTitle decodes the "title" projection value (a JSON string).
func decodeTitle(v json.RawMessage) string {
	var t string
	if json.Unmarshal(v, &t) != nil {
		return ""
	}
	return textutil.StripANSI(t)
}

// CtxModel is the transcript-header and top-bar read: the session's
// request-context model, resolved to the display name the model catalog
// advertises for it (the same face the model picker shows), falling back to
// the host default. Unlike Get it returns a plain string — no queue/job
// slice copies, no summary clone — so per-item render passes can ask for it
// cheaply.
func (s *Store) CtxModel(id string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if st := s.sessions[id]; st != nil && st.Ctx != nil && st.Ctx.Model != "" {
		return st.Models.DisplayName(st.Ctx.Provider, st.Ctx.Model)
	}
	// Host fallback: a resumed session may not have had a request/context
	// event yet, so its top bar answers from the host default — resolved
	// through the same catalog (the host's provider hint rides along).
	if s.host != nil && s.host.Model != "" {
		if st := s.sessions[id]; st != nil {
			return st.Models.DisplayName(s.host.Provider, s.host.Model)
		}
		return s.host.Model
	}
	return ""
}

// SetModels caches the session's advertised model catalog, or drops the
// cache when models is nil. The top bar's model readout resolves through it,
// so the fetch runs off the event loop and lands here.
func (s *Store) SetModels(id string, models *protocol.SessionModels) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Sess(id).Models = models
	s.pushDirty()
}

// SummaryFor returns the stored summary (may be partial).
func (s *Store) SummaryFor(id string) *protocol.SessionSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := s.sessions[id]
	if st == nil {
		return nil
	}
	cp := *st.S
	return &cp
}

// LoadTail rebuilds the transcript from a history tail page and seeds the
// projection baseline. This is the re-baseline primitive after (re)connect.
//
// A page describes the log at its own cut. When live frames have already
// advanced past that cut (a slow response landing late, e.g. the boot
// double-load), the transcript keeps the fresh items and the projection
// watermarks keep their newer live values — higher-seq-wins, the same rule
// the wire contract uses for baselines.
func (s *Store) LoadTail(id string, resp *protocol.HistoryResponse) {
	s.mu.Lock()
	st := s.Sess(id)
	cut := int64(0)
	if resp.Projections != nil {
		cut = resp.Projections.AsOfSeq
	}
	if n := len(resp.Events); n > 0 && resp.Events[n-1].Event.Seq > cut {
		cut = resp.Events[n-1].Event.Seq
	}
	if cut >= st.T.MaxSeq() {
		st.T = NewTranscript()
		for _, entry := range resp.Events {
			st.T.Apply(&entry.Event)
			if entry.Event.Type == "request/context" {
				// Side state the fold skips: adopt the newest window the
				// session history advertises so the context bar shows at
				// baseline, not only after a live turn.
				var d protocol.RequestContextData
				if json.Unmarshal(entry.Event.Data, &d) == nil {
					applyRequestContext(st, &d)
				}
			}
		}
	}
	st.HasMore = resp.HasMore
	if resp.Projections != nil {
		applyProjectionValues(st, cut, resp.Projections.Values)
	}
	s.pushDirty()
	s.mu.Unlock()
}

// LoadOlder prepends an older history page.
func (s *Store) LoadOlder(id string, resp *protocol.HistoryResponse) {
	s.mu.Lock()
	st := s.Sess(id)
	old := make([]*protocol.SessionEvent, len(resp.Events))
	for i, entry := range resp.Events {
		old[i] = &entry.Event
	}
	st.T.Prepend(old)
	st.HasMore = resp.HasMore
	s.pushDirty()
	s.mu.Unlock()
}

// Event applies one live session event. It returns whether it changed
// state and, for assistant/message events, the parsed token usage (the
// fold already decoded the payload; the usage recorder must not decode
// it again).
func (s *Store) Event(id string, ev *protocol.SessionEvent) (bool, *protocol.TokenUsage) {
	s.mu.Lock()
	st := s.Sess(id)
	before := len(st.T.Items)
	changed, usage := st.T.Apply(ev)
	switch ev.Type {
	case "turn/start":
		st.Running = true
		setSummary(st, func(s *protocol.SessionSummary) { s.Running = true })
	case "turn/end":
		var d protocol.TurnEndEventData
		if json.Unmarshal(ev.Data, &d) == nil {
			// Some deployments never re-emit an authoritative running
			// state; settle locally unless the queue still holds
			// pending work (a later turn/start re-arms the flags).
			if len(st.Queue) == 0 {
				st.Running = false
				setSummary(st, func(s *protocol.SessionSummary) { s.Running = false })
			}
			info := &TurnEndInfo{Kind: d.Reason.Kind, Reason: textutil.StripControl(d.Reason.Reason), At: time.Now()}
			if ms := st.T.TurnStartAt; ms > 0 && info.Ms == 0 && ev.Time > ms {
				info.Ms = ev.Time - ms
			}
			info.In = st.T.TurnTokens.In
			info.Out = st.T.TurnTokens.Out
			info.Cache = st.T.TurnTokens.Cache
			if d.Reason.Error != nil {
				info.Error = textutil.StripControl(d.Reason.Error.Message)
			}
			st.flash = info
			info.Sess = id
			select {
			case s.turnEnd <- info:
			default:
			}
			if s.active == id {
				n := Notice{Level: turnEndLevel(d.Reason.Kind), Text: s.formatTurnEnd(info), Sess: id, Bell: true}
				s.pushNotice(n)
			} else {
				st.fresh = true
			}
		}
	case "todo/write":
		var d protocol.TodoWriteEventData
		if json.Unmarshal(ev.Data, &d) == nil {
			for i := range d.Todos {
				d.Todos[i].Content = textutil.StripANSI(d.Todos[i].Content)
			}
			st.Todos = d.Todos
		}
	case "session/title":
		var d protocol.TitleEventData
		if json.Unmarshal(ev.Data, &d) == nil && d.Title != "" {
			st.title = textutil.StripANSI(d.Title)
		}
	case "goal/change":
		var d protocol.GoalChangeData
		if json.Unmarshal(ev.Data, &d) == nil {
			if d.Goal != nil {
				d.Goal.Objective = textutil.StripANSI(d.Goal.Objective)
				st.Goal = &protocol.GoalProjected{Goal: d.Goal, RoundsStarted: d.RoundsStarted}
			} else {
				st.Goal = nil
			}
		}
	case "request/context":
		var d protocol.RequestContextData
		if json.Unmarshal(ev.Data, &d) == nil {
			applyRequestContext(st, &d)
		}
	case "plan/mode":
		var m map[string]any
		if json.Unmarshal(ev.Data, &m) == nil {
			st.PlanMode = planModeOn(m)
		}
	case "agent-preset/selected":
		// The host re-emits the committed switch (blank-session mode
		// change) as a session event so every client converges on the
		// mode the session actually runs.
		var d struct {
			AgentPreset string `json:"agentPreset"`
		}
		if json.Unmarshal(ev.Data, &d) == nil && d.AgentPreset != "" {
			st.S.AgentPreset = d.AgentPreset
		}
	}
	if changed {
		s.pushDirty()
	}
	s.mu.Unlock()
	return changed || before != len(st.T.Items), usage
}

// UserMessageReconcile replaces the optimistic echo for a prompt rpcId.
func (s *Store) UserMessageReconcile(id, rpcId string) {
	s.mu.Lock()
	if st := s.sessions[id]; st != nil && rpcId != "" {
		st.T.ReconcileEcho(rpcId)
		s.pushDirty()
	}
	s.mu.Unlock()
}

// PromptSent records the optimistic echo of a submitted prompt.
func (s *Store) PromptSent(id, text, rpcId string) {
	s.mu.Lock()
	st := s.Sess(id)
	st.T.AddPendingUser(text, rpcId, time.Now().UnixMilli())
	s.pushDirty()
	s.mu.Unlock()
}

// PromptCommand settles a prompt that turned out to be a slash command (no
// user/message event will arrive for it).
func (s *Store) PromptCommand(id, rpcId string, res *protocol.PromptResponse) {
	s.mu.Lock()
	st := s.Sess(id)
	st.T.ReconcileEcho(rpcId)
	if res != nil && res.Command != nil && res.Command.Text != "" {
		st.T.Items = append(st.T.Items, st.T.stamp(&Item{
			Kind: KindNote, Time: time.Now().UnixMilli(),
			Note: "command: " + res.Command.Text,
		}))
	}
	s.pushDirty()
	s.mu.Unlock()
}

// MuxQueue stores an authoritative inbox snapshot.
func (s *Store) MuxQueue(id string, items []protocol.QueuedInboxItem) {
	s.mu.Lock()
	st := s.Sess(id)
	for i := range items {
		for j := range items[i].Message.Content {
			items[i].Message.Content[j].Text = textutil.StripANSI(items[i].Message.Content[j].Text)
			items[i].Message.Content[j].Name = textutil.StripANSI(items[i].Message.Content[j].Name)
		}
	}
	st.Queue = items
	st.Running = true // pending work implies an active queue
	s.pushDirty()
	s.mu.Unlock()
}

// MuxJobs stores an authoritative job snapshot.
func (s *Store) MuxJobs(id string, jobs []protocol.JobView) {
	s.mu.Lock()
	st := s.Sess(id)
	for i := range jobs {
		jobs[i].Label = textutil.StripANSI(jobs[i].Label)
		jobs[i].Detail = textutil.StripANSI(jobs[i].Detail)
	}
	st.Jobs = jobs
	s.pushDirty()
	s.mu.Unlock()
}

// MuxProjection stores one projection value under higher-seq-wins.
func (s *Store) MuxProjection(id, key string, seq int64, value json.RawMessage) {
	s.mu.Lock()
	st := s.Sess(id)
	pv, ok := st.proj[key]
	if ok && pv.Seq > seq {
		s.mu.Unlock()
		return
	}
	st.proj[key] = projVal{Seq: seq, V: value}
	if key == "goal" {
		st.Goal = decodeGoal(value)
	}
	if key == "permissions" {
		st.Permission = decodePermission(value)
	}
	if key == "title" {
		st.titleProj = decodeTitle(value)
	}
	if key == "contextPressure" {
		st.CtxPressure = decodeContextPressure(value)
	}
	s.pushDirty()
	s.mu.Unlock()
}

// Subscribed records the per-session stream baseline (lastSeq watermark).
func (s *Store) Subscribed(id string, lastSeq int64) {
	s.mu.Lock()
	st := s.Sess(id)
	if lastSeq > st.T.MaxSeq() {
		st.T.noteSeq(lastSeq, 0)
	}
	s.mu.Unlock()
}

// HostFrame applies one host-stream frame.
func (s *Store) HostFrame(f *protocol.HostFrame) {
	s.mu.Lock()
	switch f.Type {
	case protocol.FHostSessionAdded:
		st := s.Sess(f.SessionId)
		setSummary(st, func(s *protocol.SessionSummary) {
			s.Blank = f.Blank
			s.Cwd = f.Cwd
			s.AgentPreset = f.AgentPreset
			s.Origin = f.Origin
			s.ParentSessionId = f.ParentSessionId
		})
		if existing := s.indexOfOrder(f.SessionId); existing < 0 {
			s.order = append(s.order, f.SessionId)
		}
		s.pushDirty()
	case protocol.FHostSessionRemoved:
		delete(s.sessions, f.SessionId)
		delete(s.wsBind, f.SessionId)
		if i := s.indexOfOrder(f.SessionId); i >= 0 {
			s.order = append(s.order[:i], s.order[i+1:]...)
		}
		if s.active == f.SessionId {
			s.active = ""
		}
		s.pushDirty()
	case protocol.FHostSessionStatus:
		st := s.Sess(f.SessionId)
		st.Running = f.Running
		setSummary(st, func(s *protocol.SessionSummary) {
			s.Running = f.Running
			if f.Running {
				s.Blank = false
			}
		})
		if i := s.indexOfOrder(f.SessionId); i < 0 {
			s.order = append(s.order, f.SessionId)
		}
		if s.active != f.SessionId {
			st.fresh = st.fresh || f.Running
		}
		s.pushDirty()
	case protocol.FHostAgentError:
		st := s.Sess(f.SessionId)
		st.Running = false
		s.pushNotice(Notice{Level: "err", Text: "agent error: " + f.Message, Sess: f.SessionId, Bell: s.active == f.SessionId})
		s.pushDirty()
	case protocol.FHostWorkspaceChanged:
		var v protocol.WorkspaceView
		if err := json.Unmarshal(f.Workspace, &v); err == nil && v.WorkspaceId != "" {
			s.workspaceUpsertLocked(&v)
			s.pushDirty()
		}
	case protocol.FHostWorkspaceRemoved:
		if s.workspaceRemoveLocked(f.WorkspaceId) {
			s.pushDirty()
		}
	case protocol.FHostOrderChanged:
		if s.workspaceOrderLocked(f.WorkspaceIds) {
			s.pushDirty()
		}
	case protocol.FHostArchived:
		if s.archivedSetLocked(f.ArchivedSessionIds) {
			s.pushDirty()
		}
	}
	s.mu.Unlock()
}

// ---- workspace registry -------------------------------------------------------

// SetWorkspaces rebaselines the registry from workspace.list: workspaces in
// durable display order plus the registry-global archive set.
func (s *Store) SetWorkspaces(items []protocol.WorkspaceView, archived []string) {
	s.mu.Lock()
	s.workspaces = map[string]*protocol.WorkspaceView{}
	s.wsOrder = nil
	for i := range items {
		w := items[i]
		if w.WorkspaceId == "" {
			continue
		}
		w.Title = textutil.StripANSI(w.Title)
		s.workspaces[w.WorkspaceId] = &w
		s.wsOrder = append(s.wsOrder, w.WorkspaceId)
	}
	s.archivedSetLocked(archived)
	s.wsListed = true
	// The rebaseline is complete: a binding whose workspace the host no
	// longer lists is stale (the session ungrouped or the workspace
	// deleted on another client).
	for sid, wid := range s.wsBind {
		if _, ok := s.workspaces[wid]; !ok {
			delete(s.wsBind, sid)
		}
	}
	s.pushDirty()
	s.mu.Unlock()
}

// WorkspaceUpsert installs or updates one host view (host/workspace-changed
// pushes the full row; the client upserts).
func (s *Store) WorkspaceUpsert(v *protocol.WorkspaceView) {
	s.mu.Lock()
	s.workspaceUpsertLocked(v)
	s.pushDirty()
	s.mu.Unlock()
}

// WorkspaceRemove drops one registry row (host/workspace-removed).
func (s *Store) WorkspaceRemove(id string) {
	s.mu.Lock()
	if s.workspaceRemoveLocked(id) {
		s.pushDirty()
	}
	s.mu.Unlock()
}

// WorkspaceOrderChanged installs a complete durable display order (pushed
// whole by host/workspace-order-changed).
func (s *Store) WorkspaceOrderChanged(ids []string) {
	s.mu.Lock()
	if s.workspaceOrderLocked(ids) {
		s.pushDirty()
	}
	s.mu.Unlock()
}

// ArchivedChanged installs the registry-global archive set (complete push).
func (s *Store) ArchivedChanged(ids []string) {
	s.mu.Lock()
	if s.archivedSetLocked(ids) {
		s.pushDirty()
	}
	s.mu.Unlock()
}

func (s *Store) workspaceUpsertLocked(v *protocol.WorkspaceView) {
	if v.WorkspaceId == "" {
		return
	}
	v.Title = textutil.StripANSI(v.Title)
	if _, ok := s.workspaces[v.WorkspaceId]; !ok {
		s.wsOrder = append(s.wsOrder, v.WorkspaceId)
	}
	s.workspaces[v.WorkspaceId] = v
}

func (s *Store) workspaceRemoveLocked(id string) (changed bool) {
	if _, ok := s.workspaces[id]; !ok {
		return false
	}
	delete(s.workspaces, id)
	s.wsOrder = removeStr(s.wsOrder, id)
	for sid, wid := range s.wsBind {
		if wid == id {
			delete(s.wsBind, sid)
		}
	}
	return true
}

func (s *Store) workspaceOrderLocked(ids []string) bool {
	kept := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		if _, ok := s.workspaces[id]; ok && !seen[id] {
			kept = append(kept, id)
			seen[id] = true
		}
	}
	if strings.Join(kept, "\x01") == strings.Join(s.wsOrder, "\x01") {
		return false
	}
	s.wsOrder = kept
	return true
}

func (s *Store) archivedSetLocked(ids []string) bool {
	next := archivedOf(ids)
	if setsEqual(s.archived, next) {
		return false
	}
	s.archived = next
	return true
}

func setsEqual(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func archivedOf(ids []string) map[string]bool {
	m := map[string]bool{}
	for _, id := range ids {
		m[id] = true
	}
	return m
}

func removeStr(xs []string, v string) []string {
	out := xs[:0]
	for _, x := range xs {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

// Workspaces returns the registry in durable display order (copies).
func (s *Store) Workspaces() []protocol.WorkspaceView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]protocol.WorkspaceView, 0, len(s.wsOrder))
	for _, id := range s.wsOrder {
		if w, ok := s.workspaces[id]; ok {
			out = append(out, *w)
		}
	}
	return out
}

// WorkspacesBaselined reports whether workspace.list has answered once.
func (s *Store) WorkspacesBaselined() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.wsListed
}

// WorkspacesReady reports whether the registry is known from ANY source:
// the live baseline or the boot cache (stale-while-revalidate at boot).
func (s *Store) WorkspacesReady() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.wsListed || s.wsCashed
}

// RosterBaselined reports whether session.list has answered once — the
// boot auto-pick gates on it (the host probe lands before the roster,
// and picking against an empty roster would always mint a fresh
// session).
func (s *Store) RosterBaselined() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.rosterListed
}

// RosterReady reports whether the roster is known from ANY source: the
// live baseline or the boot cache. The boot auto-pick gates on it so the
// chrome (top bar, status bar) serves the previous boot's names
// immediately while the live session.list (expensive on large hosts)
// re-baselines in the background.
func (s *Store) RosterReady() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.rosterListed || s.rosterCashed
}

// CacheRow is one persisted roster row (the boot cache's stale-while-
// revalidate source). Cwd is the FULL path (shortCwd would break the
// workspace path match); Title is the decoded title projection.
type CacheRow struct {
	Id        string
	UpdatedAt int64
	Running   bool
	Blank     bool
	Parent    string
	Origin    string
	Cwd       string
	Mode      string
	Title     string
}

// CacheRoster installs the previously persisted roster at boot: the chrome
// (top bar title, status bar workspace chip, session window) serves the
// previous boot's names immediately while the live session.list re-baselines
// (SetSessions overwrites row by row and drops removed sessions). It is a
// no-op once any live row exists. Returns true when it installed.
func (s *Store) CacheRoster(rows []CacheRow) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rosterListed || len(s.order) > 0 {
		return false
	}
	for _, r := range rows {
		if r.Id == "" {
			continue
		}
		st := s.Sess(r.Id)
		st.S.UpdatedAt = r.UpdatedAt
		st.S.Running = r.Running
		st.Running = r.Running
		st.S.Blank = r.Blank
		st.S.ParentSessionId = r.Parent
		st.S.Origin = r.Origin
		st.S.Cwd = r.Cwd
		st.S.AgentPreset = r.Mode
		// The cached title sits at EVENT level (not the projection mirror):
		// anything live — a title event or a re-baselined projection — is
		// newer than the previous boot by definition and must win.
		if r.Title != "" {
			st.title = textutil.StripANSI(r.Title)
		}
	}
	s.rosterCashed = true
	s.pushDirty()
	return true
}

// CacheWorkspaces installs the previously persisted workspace registry at
// boot (same stale-while-revalidate role; SetWorkspaces overwrites it).
// Returns true when it installed.
func (s *Store) CacheWorkspaces(items []protocol.WorkspaceView, archived []string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.wsListed || len(s.workspaces) > 0 {
		return false
	}
	for i := range items {
		w := items[i]
		if w.WorkspaceId == "" {
			continue
		}
		w.Title = textutil.StripANSI(w.Title)
		s.workspaces[w.WorkspaceId] = &w
		s.wsOrder = append(s.wsOrder, w.WorkspaceId)
	}
	s.archivedSetLocked(archived)
	s.wsCashed = true
	s.pushDirty()
	return true
}

// CacheData snapshots the current roster and registry for persistence
// (the app writes the previous boot's names to disk once the live
// baselines land, so the NEXT boot starts warm).
func (s *Store) CacheData() (rows []CacheRow, ws []protocol.WorkspaceView, archived []string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows = make([]CacheRow, 0, len(s.order))
	for _, id := range s.order {
		st := s.sessions[id]
		if st == nil || s.archived[id] {
			continue
		}
		rows = append(rows, CacheRow{
			Id: st.S.SessionId, UpdatedAt: st.S.UpdatedAt, Running: st.Running,
			Blank: st.S.Blank, Parent: st.S.ParentSessionId, Origin: st.S.Origin,
			Cwd: st.S.Cwd, Mode: st.S.AgentPreset, Title: s.titleLocked(st),
		})
	}
	ws = make([]protocol.WorkspaceView, 0, len(s.wsOrder))
	for _, wid := range s.wsOrder {
		if w := s.workspaces[wid]; w != nil {
			ws = append(ws, *w)
		}
	}
	archived = make([]string, 0, len(s.archived))
	for id := range s.archived {
		archived = append(archived, id)
	}
	return rows, ws, archived
}

// ArchivedIDs returns the registry-global archive set.
func (s *Store) ArchivedIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.archived))
	for id := range s.archived {
		out = append(out, id)
	}
	return out
}

// IsArchived reports whether one session sits in the archive set.
func (s *Store) IsArchived(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.archived[id]
}

// WorkspaceForSession resolves the workspace a session belongs to: its
// workspace's session account first, then the cwd path match (a session
// created over a registered directory before the account frame lands).
func (s *Store) WorkspaceForSession(id string) *protocol.WorkspaceView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	w := s.workspaceForSessionLocked(id)
	if w == nil {
		return nil
	}
	c := *w
	return &c
}

func (s *Store) workspaceForSessionLocked(id string) *protocol.WorkspaceView {
	// The creation-time binding is authoritative: it is set when the
	// session is minted into (or moved to) a workspace, before the
	// registry row's session account lists the id.
	if wid, ok := s.wsBind[id]; ok {
		if w := s.workspaces[wid]; w != nil {
			return w
		}
	}
	for _, wid := range s.wsOrder {
		w := s.workspaces[wid]
		if w == nil {
			continue
		}
		for _, sid := range w.SessionIds {
			if sid == id {
				return w
			}
		}
	}
	if st := s.sessions[id]; st != nil && st.S.Cwd != "" {
		for _, wid := range s.wsOrder {
			w := s.workspaces[wid]
			if w != nil && samePath(w.Path, st.S.Cwd) {
				return w
			}
		}
	}
	return nil
}

// WorkspaceByID returns one registry row ("" id: nil).
func (s *Store) WorkspaceByID(id string) *protocol.WorkspaceView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	w := s.workspaces[id]
	if w == nil {
		return nil
	}
	c := *w
	return &c
}

// SeedCwd records a freshly created session's cwd before the first roster
// refresh lands it (so workspace grouping resolves immediately). Returns
// true when the seed changed state.
func (s *Store) SeedCwd(id, cwd string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.sessions[id]
	if !ok || cwd == "" || st.S.Cwd != "" {
		return false
	}
	st.S.Cwd = cwd
	s.pushDirty()
	return true
}

// SeedWorkspace records the workspace a freshly created (or re-attached)
// session belongs to, before the registry row's session account lists the
// id: workspaceForSessionLocked consults the binding first, so the status
// bar chip resolves immediately on the switch that minted the session.
func (s *Store) SeedWorkspace(sid, wsID string) {
	if sid == "" || wsID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.wsBind[sid] == wsID {
		return
	}
	s.wsBind[sid] = wsID
	s.pushDirty()
}

// WorkspaceForPath resolves a registered workspace by directory path.
func (s *Store) WorkspaceForPath(p string) *protocol.WorkspaceView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var hit *protocol.WorkspaceView
	for _, wid := range s.wsOrder {
		w := s.workspaces[wid]
		if w != nil && samePath(w.Path, p) {
			hit = w
			break
		}
	}
	if hit == nil {
		return nil
	}
	c := *hit
	return &c
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	return path.Clean(a) == path.Clean(b)
}

// ApprovalRequested parks an answerable approval frame.
func (s *Store) ApprovalRequested(id, rpcId, approvalId, toolName, callId, reason, args string) {
	s.mu.Lock()
	st := s.Sess(id)
	st.approvals[rpcId] = &ApprovalPend{RpcId: rpcId, SessionId: id, ApprovalId: approvalId, ToolName: textutil.StripANSI(toolName), CallId: callId, Reason: textutil.StripANSI(reason), Args: args}
	s.pushDirty()
	s.mu.Unlock()
}

// ApprovalResolved clears the pending frame matched by approvalId (the
// resolved frame carries the stable approval id, not the answerable rpcId).
func (s *Store) ApprovalResolved(approvalId, outcome string) {
	s.mu.Lock()
	for _, st := range s.sessions {
		for rpcId, a := range st.approvals {
			if a.ApprovalId == approvalId {
				delete(st.approvals, rpcId)
				s.pushNotice(Notice{Level: "info", Text: "tool " + a.ToolName + ": " + outcome, Sess: st.S.SessionId})
				s.pushDirty()
			}
		}
	}
	s.mu.Unlock()
}

// QuestionRequested parks an answerable question batch.
func (s *Store) QuestionRequested(id, rpcId string, qs []protocol.QuestionItem) {
	s.mu.Lock()
	st := s.Sess(id)
	for i := range qs {
		qs[i].Question = textutil.StripANSI(qs[i].Question)
		qs[i].Detail = textutil.StripANSI(qs[i].Detail)
		qs[i].Header = textutil.StripANSI(qs[i].Header)
		for j := range qs[i].Options {
			qs[i].Options[j].Label = textutil.StripANSI(qs[i].Options[j].Label)
			qs[i].Options[j].Description = textutil.StripANSI(qs[i].Options[j].Description)
		}
	}
	st.questions[rpcId] = &QuestionPend{RpcId: rpcId, SessionId: id, Questions: qs}
	s.pushDirty()
	s.mu.Unlock()
}

// QuestionResolved clears the pending batch.
func (s *Store) QuestionResolved(rpcId, outcome string) {
	s.mu.Lock()
	st := s.findQuestion(rpcId)
	if st != nil {
		delete(st.questions, rpcId)
		s.pushNotice(Notice{Level: "info", Text: "question " + outcome, Sess: st.S.SessionId})
		s.pushDirty()
	}
	s.mu.Unlock()
}

func (s *Store) findQuestion(rpcId string) *Sess {
	for _, st := range s.sessions {
		if _, ok := st.questions[rpcId]; ok {
			return st
		}
	}
	return nil
}

func (s *Store) indexOfOrder(id string) int {
	for i, x := range s.order {
		if x == id {
			return i
		}
	}
	return -1
}

// resortOrder re-sorts the roster updatedAt-descending (stable for equal
// stamps). Callers hold s.mu.
func (s *Store) resortOrder() []string {
	sort.SliceStable(s.order, func(i, j int) bool {
		return s.sessions[s.order[i]].S.UpdatedAt > s.sessions[s.order[j]].S.UpdatedAt
	})
	return s.order
}

// Snapshot is the UI-facing read of one session; callers hold no references
// after it returns: the items/queue slices are copied, and every struct
// pointer it exposes is COW-owned by the store — published, then only
// ever replaced, never mutated in place (see setSummary and the fold's
// replaceItem/removeItem).
type Snapshot struct {
	Id          string
	Summary     *protocol.SessionSummary
	Items       []*Item
	Streaming   *Item // the in-flight item (its Usage: the live preview)
	Running     bool
	TurnActive  bool
	TurnStart   time.Time
	TurnTok     TurnTokens
	Recent      []GenSample // the rolling generation samples (t/s readout)
	Todos       []protocol.TodoItem
	Goal        *protocol.GoalProjected
	Permission  *protocol.PermissionProjection
	PlanMode    bool
	Ctx         *protocol.RequestContextData
	CtxPressure *ContextPressure
	ModelSel    *protocol.ModelSelection
	Queue       []protocol.QueuedInboxItem
	Jobs        []protocol.JobView
	HasMore     bool
	Approvals   []*ApprovalPend
	Questions   []*QuestionPend
	Flash       *TurnEndInfo
	Fresh       bool
	Title       string
}

// Get returns a point-in-time snapshot of one session.
func (s *Store) Get(id string) *Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := s.sessions[id]
	if st == nil {
		return nil
	}
	// A zero TurnStartAt is "no turn/start in view" (a re-baseline tail
	// that predates it), NOT 1970-01-01: keep the true zero time so the
	// IsZero guards in the readouts hold.
	var turnStart time.Time
	if st.T.TurnStartAt > 0 {
		turnStart = time.UnixMilli(st.T.TurnStartAt)
	}
	snap := &Snapshot{
		Id: id, Summary: st.S, Items: st.T.Items, Streaming: st.T.Streaming(),
		Running: st.Running, TurnActive: st.T.TurnActive,
		TurnStart: turnStart, TurnTok: st.T.TurnTokens,
		Recent: append([]GenSample(nil), st.T.Recent...),
		Todos:  append([]protocol.TodoItem(nil), st.Todos...),
		Goal:   st.Goal, Permission: st.Permission, PlanMode: st.PlanMode,
		Ctx: st.Ctx, CtxPressure: st.CtxPressure, ModelSel: st.ModelSel,
		Queue:   append([]protocol.QueuedInboxItem(nil), st.Queue...),
		Jobs:    append([]protocol.JobView(nil), st.Jobs...),
		HasMore: st.HasMore, Flash: st.flash, Fresh: st.fresh,
		Title: s.titleLocked(st),
	}
	for _, a := range st.approvals {
		snap.Approvals = append(snap.Approvals, a)
	}
	for _, q := range st.questions {
		snap.Questions = append(snap.Questions, q)
	}
	return snap
}

// titleLocked resolves the title without JSON: the "title" projection is
// decoded once when it is written (titleProj), so the hot per-frame title
// reads are a plain string lookup.
func (s *Store) titleLocked(st *Sess) string {
	if st.titleProj != "" {
		return st.titleProj
	}
	return st.title
}

// RosterForList returns list rows in order with resolved titles.
type Row struct {
	Id        string
	UpdatedAt int64
	Running   bool
	Blank     bool
	Title     string
	Cwd       string
	Parent    string
	Fresh     bool
	// Mode is the agent preset the session runs ("" when the deployment
	// composes no presets, so the label stays off).
	Mode string
	// WsID / WsTitle name the workspace the session belongs to ("",
	// ungrouped, when the registry knows it by neither account nor path).
	WsID    string
	WsTitle string
}

// Roster returns the ordered session list. Archived sessions hide from the
// grouping surfaces (the web sidebar rule) and with them from the roster.
func (s *Store) Roster() []Row {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows := make([]Row, 0, len(s.order))
	for _, id := range s.order {
		st := s.sessions[id]
		if st == nil || s.archived[id] {
			continue
		}
		rows = append(rows, s.rowLocked(st))
	}
	return rows
}

func (s *Store) rowLocked(st *Sess) Row {
	r := Row{
		Id: st.S.SessionId, UpdatedAt: st.S.UpdatedAt, Running: st.Running,
		Blank: st.S.Blank, Title: s.titleLocked(st), Cwd: shortCwd(st.S.Cwd),
		Parent: st.S.ParentSessionId, Fresh: st.fresh, Mode: st.S.AgentPreset,
	}
	if w := s.workspaceForSessionLocked(r.Id); w != nil {
		r.WsID, r.WsTitle = w.WorkspaceId, w.Title
	}
	return r
}

// SideRow is one rendered row of the grouped session list: a workspace
// header line or an indented session row (flat rows when the registry is
// empty).
type SideRow struct {
	Header bool
	Title  string // header: workspace title (or "ungrouped")
	Path   string // header: the workspace directory path
	Row    Row    // session rows only
}

// Side returns the sidebar layout: workspace groups (registry display
// order, sessions in the workspace's manual account order) over an
// ungrouped tail, or the flat roster while no workspace is registered.
func (s *Store) Side() []SideRow {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sideLocked()
}

func (s *Store) sideLocked() []SideRow {
	var out []SideRow
	if len(s.workspaces) == 0 {
		for _, id := range s.order {
			if st := s.sessions[id]; st != nil && !s.archived[id] {
				out = append(out, SideRow{Row: s.rowLocked(st)})
			}
		}
		return out
	}
	claimed := map[string]bool{}
	for _, wid := range s.wsOrder {
		w := s.workspaces[wid]
		if w == nil {
			continue
		}
		title := w.Title
		if title == "" {
			title = w.Path
		}
		out = append(out, SideRow{Header: true, Title: title, Path: w.Path})
		for _, sid := range w.SessionIds {
			if st := s.sessions[sid]; st != nil && !s.archived[sid] {
				claimed[sid] = true
				out = append(out, SideRow{Row: s.rowLocked(st)})
			}
		}
	}
	var un []Row
	for _, id := range s.order {
		if claimed[id] || s.archived[id] {
			continue
		}
		if st := s.sessions[id]; st != nil {
			un = append(un, s.rowLocked(st))
		}
	}
	if len(un) > 0 {
		out = append(out, SideRow{Header: true, Title: "ungrouped"})
		for _, r := range un {
			out = append(out, SideRow{Row: r})
		}
	}
	return out
}

// SideIDs returns session ids in sidebar display order (the [ / ] cycle).
func (s *Store) SideIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []string
	for _, r := range s.sideLocked() {
		if !r.Header {
			out = append(out, r.Row.Id)
		}
	}
	return out
}

// CurrentSide returns the session list scoped to the workspace that owns
// id (the session window's scope): one workspace group — header plus the
// workspace's sessions in account order — when the session is accounted
// to a workspace, and the full Side() roster otherwise (no session, or
// one the workspace rosters do not carry — a cwd-only attach — where
// scoping would hide the current session from its own window).
func (s *Store) CurrentSide(id string) []SideRow {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentSideLocked(id)
}

// CurrentSideIDs is the scoped list as session ids (the window's arrow /
// [ ] walk).
func (s *Store) CurrentSideIDs(id string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []string
	for _, r := range s.currentSideLocked(id) {
		if !r.Header {
			out = append(out, r.Row.Id)
		}
	}
	return out
}

func (s *Store) currentSideLocked(id string) []SideRow {
	w := s.workspaceForSessionLocked(id)
	if w == nil {
		return s.sideLocked()
	}
	inAccount := false
	for _, sid := range w.SessionIds {
		if sid == id {
			inAccount = true
			break
		}
	}
	if !inAccount {
		return s.sideLocked()
	}
	title := w.Title
	if title == "" {
		title = w.Path
	}
	out := []SideRow{{Header: true, Title: title, Path: w.Path}}
	for _, sid := range w.SessionIds {
		if st := s.sessions[sid]; st != nil && !s.archived[sid] {
			out = append(out, SideRow{Row: s.rowLocked(st)})
		}
	}
	return out
}

func shortCwd(c string) string {
	if i := strings.LastIndex(c, "/"); i > 0 {
		c = c[i+1:]
	}
	return textutil.Truncate(c, 24, "…")
}

// Notify enqueues a toast/bell notice (non-blocking).
func (s *Store) Notify(n Notice) { s.pushNotice(n) }

// applyRequestContext records the request-context side state shared by
// the live event path and the history baseline (the transcript fold
// treats the event as transparent metadata, so LoadTail must absorb it
// explicitly). The event also carries the model the host will actually
// use for the next request; a different model voids the displayed effort.
func applyRequestContext(st *Sess, d *protocol.RequestContextData) {
	st.Ctx = d
	if ms := st.ModelSel; ms != nil && d.Model != "" && d.Model != ms.Model {
		st.ModelSel = &protocol.ModelSelection{Provider: d.Provider, Model: d.Model}
	}
}

// ApplyModelSelection records a model selection that was just applied to
// the session, so the UI shows the new model and effort immediately — the
// matching request/context event only arrives with the next turn.
func (s *Store) ApplyModelSelection(id string, sel *protocol.ModelSelection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.Sess(id)
	st.ModelSel = sel
	if st.Ctx == nil {
		st.Ctx = &protocol.RequestContextData{Provider: sel.Provider, Model: sel.Model}
	} else {
		cp := *st.Ctx
		cp.Provider = sel.Provider
		cp.Model = sel.Model
		st.Ctx = &cp
	}
	s.pushDirty()
}

// EnsureRow registers a session row if absent (no summary refresh).
func (s *Store) EnsureRow(id string) {
	s.mu.Lock()
	s.Sess(id)
	s.mu.Unlock()
}

// SetTitle updates the last known title (from session.rename).
func (s *Store) SetTitle(id, title string) {
	s.mu.Lock()
	if st := s.sessions[id]; st != nil {
		st.title = title
		s.pushDirty()
	}
	s.mu.Unlock()
}

// SetAgentPreset records the preset a session runs (after a successful
// agentPreset.select or a create response that names one).
func (s *Store) SetAgentPreset(id, preset string) {
	s.mu.Lock()
	if st := s.sessions[id]; st != nil {
		setSummary(st, func(s *protocol.SessionSummary) { s.AgentPreset = preset })
		s.pushDirty()
	}
	s.mu.Unlock()
}

// QueueFor returns the pending inbox items of one session.
func (s *Store) QueueFor(id string) []protocol.QueuedInboxItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := s.sessions[id]
	if st == nil {
		return nil
	}
	return append([]protocol.QueuedInboxItem(nil), st.Queue...)
}

// JobsFor returns the job snapshot of one session.
func (s *Store) JobsFor(id string) []protocol.JobView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := s.sessions[id]
	if st == nil {
		return nil
	}
	return append([]protocol.JobView(nil), st.Jobs...)
}

// ApprovalsFor returns pending approvals of one session.
func (s *Store) ApprovalsFor(id string) []*ApprovalPend {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.sessions[id]
	if st == nil {
		return nil
	}
	out := make([]*ApprovalPend, 0, len(st.approvals))
	for _, a := range st.approvals {
		out = append(out, a)
	}
	return out
}

// QuestionsFor returns pending question batches of one session.
func (s *Store) QuestionsFor(id string) []*QuestionPend {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.sessions[id]
	if st == nil {
		return nil
	}
	out := make([]*QuestionPend, 0, len(st.questions))
	for _, q := range st.questions {
		out = append(out, q)
	}
	return out
}

// ContextPressure is the host's "contextPressure" projection: the live
// context footprint (pressureTokens) against the advertised window
// (contextWindow). projectedTokens forecasts the in-flight turn.
type ContextPressure struct {
	PressureTokens  int `json:"pressureTokens"`
	ProjectedTokens int `json:"projectedTokens"`
	ContextWindow   int `json:"contextWindow"`
}

// decodeContextPressure parses one stored "contextPressure" projection
// value. nil when absent or when the host has no window to measure
// against (the bar then stays off instead of guessing).
func decodeContextPressure(v json.RawMessage) *ContextPressure {
	if len(v) == 0 {
		return nil
	}
	var p ContextPressure
	if err := json.Unmarshal(v, &p); err != nil || p.ContextWindow <= 0 {
		return nil
	}
	return &p
}

func decodeGoal(v json.RawMessage) *protocol.GoalProjected {
	if len(v) == 0 {
		return nil
	}
	var g protocol.GoalProjected
	if err := json.Unmarshal(v, &g); err != nil || g.Goal == nil {
		return nil
	}
	g.Goal.Objective = textutil.StripANSI(g.Goal.Objective)
	return &g
}

// decodePermission parses one stored "permissions" projection value.
func decodePermission(v json.RawMessage) *protocol.PermissionProjection {
	if len(v) == 0 {
		return nil
	}
	var pp protocol.PermissionProjection
	if err := json.Unmarshal(v, &pp); err != nil {
		return nil
	}
	if len(pp.Options) == 0 && pp.CurrentValue == "" {
		return nil
	}
	return &pp
}

// StandardPresets is the canonical DSH sandbox-mode cycle order (dsh-base
// default preset table). It is the fallback when the deployment does not
// expose a "permissions" projection.
var StandardPresets = []string{"read-only", "workspace-write", "danger-full-access"}

// NextPermission returns the preset that follows cur in cycle order. The
// order is the deployment's preset table (projection option order); the
// derived "custom" entry is never a switch target, so a cycle starting from
// it (or from any value outside the list) lands on the first option. An
// empty table cycles StandardPresets.
func NextPermission(cur string, opts []protocol.PermissionOption) string {
	order := make([]string, 0, len(opts))
	for _, o := range opts {
		if o.Value != "" && o.Value != "custom" {
			order = append(order, o.Value)
		}
	}
	if len(order) == 0 {
		order = StandardPresets
	}
	for i, v := range order {
		if v == cur {
			return order[(i+1)%len(order)]
		}
	}
	return order[0]
}

func planModeOn(m map[string]any) bool {
	if b, ok := m["mode"].(string); ok {
		return b == "plan" || b == "on" || b == "enabled"
	}
	if b, ok := m["enabled"].(bool); ok {
		return b
	}
	if b, ok := m["active"].(bool); ok {
		return b
	}
	return false
}

func turnEndLevel(kind string) string {
	switch kind {
	case "completed":
		return "ok"
	case "interrupted", "aborted", "blocked":
		return "warn"
	default:
		return "err"
	}
}

func turnEndText(i *TurnEndInfo) string {
	dur := textutil.HumanDuration(time.Duration(i.Ms) * time.Millisecond)
	switch i.Kind {
	case "completed":
		return "turn done in " + dur
	case "interrupted", "aborted":
		return "turn interrupted after " + dur
	case "max-tokens":
		return "turn stopped at max tokens"
	default:
		t := "turn ended: " + i.Kind
		if i.Error != "" {
			t += " — " + i.Error
		}
		return t
	}
}

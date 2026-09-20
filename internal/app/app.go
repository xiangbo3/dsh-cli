// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

// Package app glues the transport layer to the state engine: the downlink
// frame pump, the re-baseline rule, and the typed RPC wrappers the UI drives.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"dsh-cli/internal/client"
	"dsh-cli/internal/config"
	"dsh-cli/internal/core"
	"dsh-cli/internal/i18n"
	"dsh-cli/internal/protocol"
	"dsh-cli/internal/usage"
)

// App owns one connection generation.
type App struct {
	cli *client.Client
	dl  *client.Stream
	st  *core.Store

	// followed tracks the transcript the cookie-gated downlink follows
	// (the focus hook un-follows it when the focus moves; no-op on the
	// legacy build, whose mux follows by subscription).
	followMu sync.Mutex
	followed string

	// usage is the persistent token-usage recorder (~/.dsh-cli); nil
	// when not attached (the statistics stay off, the app is unaffected).
	usage *usage.Recorder

	// Re-baseline throttling: flapping peers (and drop-storm pulses) must
	// not stack concurrent or back-to-back re-baselines.
	rebBusy atomic.Bool
	rebLast atomic.Int64 // unixmilli of the last re-baseline start

	// Roster-refresh coalescing (user actions): one fetch in flight and at
	// most one per 2s, so a rename/fork/create burst cannot stack concurrent
	// session.list calls.
	rosterBusy atomic.Bool
	rosterLast atomic.Int64 // unixmilli of the last roster refresh start

	// ctx is the app lifetime (Start's parameter); internal background work
	// hangs off it instead of context.Background().
	ctx context.Context
}

// New builds an App for base (e.g. "http://127.0.0.1:3080").
func New(base string) *App {
	return NewWith(base, "")
}

// NewWith is New with an explicit launch token ("" falls through to the
// stored token): the cookie-gated web build's boot credential, in
// precedence order flag > environment (merged by the caller) > config.
func NewWith(base, token string) *App {
	a := &App{
		cli: client.New(base),
		dl:  client.NewStream(base),
		st:  core.NewStore(base),
	}
	a.wireAuth(base, token)
	return a
}

// wireAuth installs the shared connection credentials: the launch token
// (explicit, else stored) and the stored cookie when it binds to this
// base; a successful exchange persists token + cookie back to the
// config.
func (a *App) wireAuth(base, token string) {
	conn := a.cli.Conn()
	c := config.Load()
	if token == "" {
		token = c.Token
	}
	if token != "" {
		conn.SetToken(token)
	}
	if c.Cookie != "" && client.TrimBase(c.CookieFor) == a.cli.BaseURL() {
		conn.SetCookie(c.Cookie)
	}
	conn.SetCookieSaver(func(tok, cookie string) {
		c := config.Load()
		c.Token = tok
		c.Cookie = cookie
		c.CookieFor = a.cli.BaseURL()
		_ = config.Save(c)
	})
}

// Client exposes the raw RPC facade (for commands without a wrapper).
func (a *App) Client() *client.Client { return a.cli }

// Store exposes the shared state engine.
func (a *App) Store() *core.Store { return a.st }

// AttachUsage installs the persistent usage recorder (nil detaches it).
func (a *App) AttachUsage(r *usage.Recorder) { a.usage = r }

// Usage exposes the recorder (nil when not attached).
func (a *App) Usage() *usage.Recorder { return a.usage }

// background returns the app's lifetime context, or a detached one before
// Start has run (tests drive the store directly).
func (a *App) background() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

// Close flushes the usage document (no-op without a recorder).
func (a *App) Close() {
	if a.usage != nil {
		a.usage.Close()
	}
}

// usageWorkspaceKey resolves the accounting key for a session's usage:
// the registered workspace id when the registry knows the session by
// account or path, the raw cwd otherwise ("" = unknown; the recorder
// files it under its own key).
func (a *App) usageWorkspaceKey(sid string) string {
	if w := a.st.WorkspaceForSession(sid); w != nil && w.WorkspaceId != "" {
		return w.WorkspaceId
	}
	if s := a.st.SummaryFor(sid); s != nil && s.Cwd != "" {
		return s.Cwd
	}
	return ""
}

// recordUsage folds one assistant event's token usage into the persistent
// statistics; the fold already parsed the payload, so no second decode here
// (nil-safe: a recorder-less app simply doesn't count). A
// projection-managed session is skipped: its host baselines already
// include the event's usage.
func (a *App) recordUsage(sid string, ev *protocol.SessionEvent, usage *protocol.TokenUsage) {
	if a.usage == nil || ev == nil || usage == nil || a.usage.ProjectionManaged(sid) {
		return
	}
	a.usage.Record(sid, a.usageWorkspaceKey(sid), ev.Time, ev.Seq, usage)
}

// recordPageUsage folds a history page's assistant usage (the same
// events the transcript will absorb — idempotent by seq, so the live
// and history paths never double-count). Projection-managed sessions
// are skipped outright: the tail's host baseline already covers the
// page's messages.
func (a *App) recordPageUsage(id string, events []protocol.HistoryEntry) {
	if a.usage == nil || a.usage.ProjectionManaged(id) {
		return
	}
	for i := range events {
		ev := &events[i].Event
		if ev.Type != "assistant/message" {
			continue
		}
		var d protocol.AssistantMessageEventData
		if json.Unmarshal(ev.Data, &d) == nil && d.Usage != nil {
			a.usage.Record(id, a.usageWorkspaceKey(id), ev.Time, ev.Seq, d.Usage)
		}
	}
}

// Start probes the host until ready, opens the downlinks, baselines the
// roster and runs the pumps until ctx ends.
func (a *App) Start(ctx context.Context) *core.Store {
	a.ctx = ctx
	a.dl.Start(ctx)
	// Point the cookie-gated transcript follow at the focused session
	// (the legacy build's mux follows by subscription; the hook is a
	// cheap no-op there).
	focus := func(id string) {
		a.unfollowFocus()
		if id != "" {
			a.dl.Follow(client.FollowAddr{Kind: "session", SessionId: id})
			a.followMu.Lock()
			a.followed = id
			a.followMu.Unlock()
		}
	}
	a.st.SetActiveChanged(focus)
	if id := a.st.Active(); id != "" {
		focus(id)
	}
	go a.pumpFrames(ctx)
	go a.pumpStatus(ctx)
	go a.pumpDrops(ctx)
	// Warm the chrome from the previous boot (top bar title, status bar
	// workspace chip, session window) before the first frame: on hosts
	// with large session counts session.list takes seconds, and waiting
	// for it to name the active session is the boot "blank bar" lag.
	a.loadBaselineCache()
	go a.baseline(ctx, true)
	return a.st
}

// unfollowFocus drops the current focus follow (the hook fires for every
// focus change, including a clear to "").
func (a *App) unfollowFocus() {
	a.followMu.Lock()
	id := a.followed
	a.followed = ""
	a.followMu.Unlock()
	if id != "" {
		a.dl.Unfollow(client.FollowAddr{Kind: "session", SessionId: id})
	}
}

// FollowSubagent opens one child transcript's follow stream (the
// cookie-gated build follows no transcript until asked; the legacy build
// no-ops).
func (a *App) FollowSubagent(parent, child, mode string) {
	a.dl.Follow(client.FollowAddr{Kind: "subagent", ParentSessionId: parent, ChildSessionId: child, Mode: mode})
}

// UnfollowSubagent closes one child transcript's follow stream.
func (a *App) UnfollowSubagent(child string) {
	a.dl.Unfollow(client.FollowAddr{Kind: "subagent", ChildSessionId: child})
}

// probe retries host.describe until it succeeds (server not up yet / just
// restarted). The UI renders off Store.Host() + Connected().
func (a *App) baseline(ctx context.Context, initial bool) {
	// A cookie-gated host that rejects the held (or missing) token
	// answers every probe with an auth-required 401: surface the fix
	// once instead of retrying into silence.
	authNoted := false
	for {
		h, err := a.cli.Describe(ctx)
		if err == nil {
			a.st.SetHost(h)
			break
		}
		if !authNoted && errors.Is(err, client.ErrAuthRequired) {
			key := "auth.needed"
			if errors.Is(err, client.ErrTokenRejected) {
				key = "auth.stale" // host restarted with a fresh token
			}
			authNoted = true
			a.st.Notify(core.Notice{Level: "err", Text: i18n.LoadDefault().T(key)})
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
	// The two baselines are independent unary calls: run them in
	// parallel so the registry (fast) does not queue behind the roster
	// (expensive on large hosts), and persist the warm cache once both
	// have answered (persistBaselineCache skips if either failed).
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); a.refreshRoster(ctx) }()
	go func() { defer wg.Done(); a.refreshWorkspaces(ctx) }()
	wg.Wait()
	a.persistBaselineCache()
}

// RefreshRoster fetches session.list synchronously and rebaselines the roster.
func (a *App) RefreshRoster(ctx context.Context) error {
	resp, err := a.cli.ListSessions(ctx)
	if err != nil {
		return fmt.Errorf("session.list: %w", err)
	}
	a.reconcileRosterUsage(resp.Items)
	a.st.SetSessions(resp.Items)
	return nil
}

// reconcileRosterUsage confirms the roster rows' tokenUsage baselines
// (each row's cumulative session total as of its asOfSeq): one
// session.list keeps every session's statistics at the host's totals —
// including sessions the TUI never loaded. Rows without a baseline
// (old hosts) fall back to per-event counting as before.
func (a *App) reconcileRosterUsage(items []protocol.SessionSummary) {
	if a.usage == nil {
		return
	}
	for i := range items {
		it := &items[i]
		if len(it.Projections) == 0 {
			continue
		}
		var pb protocol.ProjectionsBlock
		if json.Unmarshal(it.Projections, &pb) != nil || pb.AsOfSeq <= 0 {
			continue
		}
		tu := tokenUsageBaseline(pb.Values)
		if tu == nil {
			continue
		}
		a.recordProjectionBaseline(it.SessionId, pb.AsOfSeq, tu)
	}
}

// RefreshWorkspaces fetches the workspace registry baseline (the reconnect
// baseline of the host/workspace-* frames).
func (a *App) RefreshWorkspaces(ctx context.Context) error {
	resp, err := a.cli.Workspaces(ctx)
	if err != nil {
		return fmt.Errorf("workspace.list: %w", err)
	}
	a.st.SetWorkspaces(resp.Items, resp.ArchivedSessionIds)
	return nil
}

// refreshWorkspaces is the background variant; failures are notified.
func (a *App) refreshWorkspaces(ctx context.Context) {
	if err := a.RefreshWorkspaces(ctx); err != nil {
		a.st.Notify(core.Notice{Level: "warn", Text: err.Error()})
	}
}

// refreshRoster is the background variant; failures are notified, not returned.
// The downlink frames keep the roster warm between refreshes.
func (a *App) refreshRoster(ctx context.Context) {
	if err := a.RefreshRoster(ctx); err != nil {
		a.st.Notify(core.Notice{Level: "warn", Text: err.Error()})
	}
}

// refreshRosterCoalesced is the user-action path (create/rename/fork):
// at most one fetch in flight and one per 2s. The initial baseline and
// the re-baselines call refreshRoster directly — they pace themselves.
func (a *App) refreshRosterCoalesced() {
	if !a.rosterBusy.CompareAndSwap(false, true) {
		return
	}
	defer a.rosterBusy.Store(false)
	last := a.rosterLast.Load()
	now := time.Now()
	if now.UnixMilli()-last < 2000 {
		return
	}
	a.rosterLast.Store(now.UnixMilli())
	a.refreshRoster(a.background())
}

// historyPageSize bounds one history page fetch (tail and older alike).
const historyPageSize = 120

// LoadActiveTail pulls the tail page of the active session (re-baseline).
func (a *App) LoadActiveTail(ctx context.Context) {
	id := a.st.Active()
	if id == "" {
		return
	}
	a.LoadTail(ctx, id)
}

// recordProjectionBaseline confirms one host tokenUsage baseline (the
// session's cumulative total as of seq) with the persistent statistics;
// the recorder counts only the advance over the session's previous
// baseline, so the live pushes, tail pages and roster rows can all
// confirm freely without double-counting.
func (a *App) recordProjectionBaseline(sid string, seq int64, tu *protocol.TokenUsageHost) {
	if a.usage == nil || sid == "" || seq <= 0 || tu == nil {
		return
	}
	a.usage.RecordProjection(sid, a.usageWorkspaceKey(sid), 0, seq, tu)
}

// tokenUsageBaseline extracts the tail page's (or a roster row's)
// host tokenUsage baseline (nil when the host sent no projection —
// old builds fall back to per-event counting).
func tokenUsageBaseline(v map[string]json.RawMessage) *protocol.TokenUsageHost {
	raw, ok := v["tokenUsage"]
	if !ok {
		return nil
	}
	var tu protocol.TokenUsageHost
	if json.Unmarshal(raw, &tu) != nil {
		return nil
	}
	return &tu
}

// recordTailProjections confirms the tail page's host baseline (the
// cumulative total as of the page's asOfSeq, the in-flight step's
// sample included). It reports whether the page carried a baseline —
// only then does the page's per-event accounting stay off.
func (a *App) recordTailProjections(id string, resp *protocol.HistoryResponse) bool {
	if resp == nil || resp.Projections == nil {
		return false
	}
	tu := tokenUsageBaseline(resp.Projections.Values)
	if tu == nil || resp.Projections.AsOfSeq <= 0 {
		return false
	}
	a.recordProjectionBaseline(id, resp.Projections.AsOfSeq, tu)
	return true
}

// LoadTail fetches and applies one history tail page.
func (a *App) LoadTail(ctx context.Context, id string) {
	resp, err := a.cli.History(ctx, id, 0, historyPageSize)
	if err != nil {
		a.st.Notify(core.Notice{Level: "err", Text: id + " history: " + err.Error()})
		return
	}
	if !a.recordTailProjections(id, resp) {
		a.recordPageUsage(id, resp.Events)
	}
	a.st.LoadTail(id, resp)
}

// ReconcileUsage re-pulls the session's tail page and confirms its host
// tokenUsage baseline — the /status modal's live refresh: between step
// boundaries the baseline is the only signal that carries the in-flight
// step's sample, and the confirm counts only its advance.
func (a *App) ReconcileUsage(ctx context.Context, id string) {
	if a.usage == nil || id == "" {
		return
	}
	resp, err := a.cli.History(ctx, id, 0, historyPageSize)
	if err != nil {
		return
	}
	a.recordTailProjections(id, resp)
}

// LoadOlder fetches the page before the given seq.
func (a *App) LoadOlder(ctx context.Context, id string, beforeSeq int64) {
	resp, err := a.cli.History(ctx, id, beforeSeq, historyPageSize)
	if err != nil {
		a.st.Notify(core.Notice{Level: "err", Text: "older history: " + err.Error()})
		return
	}
	a.recordPageUsage(id, resp.Events)
	a.st.LoadOlder(id, resp)
}

// Rebaseline is the reconnect recovery: fresh roster + workspace registry
// + active tail page. The warm cache tracks the last good baselines too.
func (a *App) Rebaseline(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); a.refreshRoster(ctx) }()
	go func() { defer wg.Done(); a.refreshWorkspaces(ctx) }()
	wg.Wait()
	a.persistBaselineCache()
	a.LoadActiveTail(ctx)
}

// RebaselineThrottled runs Rebaseline on a background goroutine with two
// bounds: one in flight (rebBusy) and at most one per 2s (flapping peers
// emit a reconnect notice per flap; they must not hammer session.list).
// A drop pulse that loses the interval gate is dropped: its repair rides
// the next flap's baseline (at most ~2s stale), while the notice still
// fires, so the user sees the event.
// Reports whether a re-baseline was scheduled.
func (a *App) RebaselineThrottled() bool {
	if !a.rebBusy.CompareAndSwap(false, true) {
		return false
	}
	last := a.rebLast.Load()
	if now := time.Now(); now.UnixMilli()-last < 2000 {
		a.rebBusy.Store(false)
		return false
	} else {
		a.rebLast.Store(now.UnixMilli())
	}
	go func() {
		defer a.rebBusy.Store(false)
		a.Rebaseline(a.background())
	}()
	return true
}

// ---- typed wrappers (UI → RPC → store) -------------------------------------

// CreateSession creates a session and returns its id. A cwd over a
// registered workspace is sent as the workspace id (the web New-Session
// path: the host attaches the session to the workspace's account).
func (a *App) CreateSession(ctx context.Context, req protocol.SessionCreateRequest) (string, error) {
	if req.WorkspaceId == "" && req.Cwd != "" {
		if ws := a.st.WorkspaceForPath(req.Cwd); ws != nil {
			req.WorkspaceId, req.Cwd = ws.WorkspaceId, ""
		}
	}
	a.pinDefaultPreset(ctx, &req)
	resp, err := a.cli.CreateSession(ctx, req)
	if err != nil {
		return "", err
	}
	// The create value may omit the preset (deployments that answer it);
	// the requested one is still what the session runs.
	if resp.AgentPreset == "" {
		resp.AgentPreset = req.AgentPreset
	}
	a.st.EnsureRow(resp.SessionId)
	if resp.AgentPreset != "" {
		a.st.SetAgentPreset(resp.SessionId, resp.AgentPreset)
	}
	// Account the session in its workspace from birth (the status bar
	// chip must not wait for the registry row's session account or the
	// roster refresh to name the workspace).
	if req.WorkspaceId != "" {
		a.st.SeedWorkspace(resp.SessionId, req.WorkspaceId)
	}
	a.refreshRosterCoalesced()
	return resp.SessionId, nil
}

// pinDefaultPreset pins a valid preset id on a create request that names
// none, when the host's configured default no longer resolves: the roster
// flags its default row, so a roster without one carries a stale default
// (a preset id an upgrade renamed or the user deleted) that would reject
// the whole create. Pins standard (the deployment's own base default) or
// the first intact row; an unreadable roster (no preset capability) leaves
// the request to the host.
func (a *App) pinDefaultPreset(ctx context.Context, req *protocol.SessionCreateRequest) {
	if req.AgentPreset != "" {
		return
	}
	res, err := a.Presets(ctx)
	if err != nil || len(res.Presets) == 0 {
		return
	}
	for i := range res.Presets {
		if res.Presets[i].IsDefault {
			return
		}
	}
	for i := range res.Presets {
		if res.Presets[i].Id == "standard" && res.Presets[i].Broken == "" {
			req.AgentPreset = res.Presets[i].Id
			return
		}
	}
	for i := range res.Presets {
		if res.Presets[i].Broken == "" {
			req.AgentPreset = res.Presets[i].Id
			return
		}
	}
}

// WorkspaceCreate registers an existing directory as a workspace
// (idempotent) and installs the row in the registry mirror.
func (a *App) WorkspaceCreate(ctx context.Context, path string) (ws *protocol.WorkspaceView, created bool, err error) {
	resp, err := a.cli.WorkspaceCreate(ctx, strings.TrimSpace(path))
	if err != nil {
		return nil, false, err
	}
	if resp.Workspace != nil {
		a.st.WorkspaceUpsert(resp.Workspace)
	}
	return resp.Workspace, resp.Created, nil
}

// WorkspaceRename renames a workspace and mirrors the returned row.
func (a *App) WorkspaceRename(ctx context.Context, workspaceID, title string) (*protocol.WorkspaceView, error) {
	ws, err := a.cli.WorkspaceRename(ctx, workspaceID, strings.TrimSpace(title))
	if err != nil {
		return nil, err
	}
	if ws != nil {
		a.st.WorkspaceUpsert(ws)
	}
	return ws, nil
}

// WorkspaceDelete removes one workspace registration (sessions ungroup).
func (a *App) WorkspaceDelete(ctx context.Context, workspaceID string) error {
	if err := a.cli.WorkspaceDelete(ctx, workspaceID); err != nil {
		return err
	}
	a.st.WorkspaceRemove(workspaceID)
	return nil
}

// WorkspaceArchiveSession files one session in the archive set; the full
// updated set comes back from the host (mirrored locally, the changed frame
// echoes it for the other clients). Archiving the active session clears the
// selection (the web New-Session-view rule).
func (a *App) WorkspaceArchiveSession(ctx context.Context, sessionID string) error {
	ids, err := a.cli.WorkspaceArchiveSession(ctx, sessionID)
	if err != nil {
		return err
	}
	a.st.ArchivedChanged(ids)
	return nil
}

// ConnectWorkspace lands the New-Session flow in one workspace: it reuses
// the workspace's blank session when such a session is already in the
// roster, else creates a fresh one on the workspace, and returns the id to
// activate (the web hero-chip switch: picking a workspace moves the
// flow to that workspace's blank session).
func (a *App) ConnectWorkspace(ctx context.Context, workspaceID string) (string, error) {
	ws := a.st.WorkspaceByID(workspaceID)
	if ws == nil {
		return "", fmt.Errorf("workspace not in registry")
	}
	// Most recent blank in the account wins (roster is updatedAt desc).
	rows := a.st.Roster()
	for _, r := range rows {
		for _, sid := range ws.SessionIds {
			if sid == r.Id && r.Blank {
				if a.st.SeedCwd(sid, ws.Path) {
					a.st.PushDirty()
				}
				a.st.SeedWorkspace(sid, ws.WorkspaceId)
				return sid, nil
			}
		}
	}
	id, err := a.CreateSession(ctx, protocol.SessionCreateRequest{WorkspaceId: workspaceID})
	if err != nil {
		return "", err
	}
	if a.st.SeedCwd(id, ws.Path) {
		a.st.PushDirty()
	}
	return id, nil
}

// Prompt sends a prompt and returns the rpcId used for echo reconciliation.
func (a *App) Prompt(ctx context.Context, id, text, mode string) (string, error) {
	rpcId := protocol.NewRPCId()
	a.st.PromptSent(id, text, rpcId)
	req := protocol.PromptRequest{
		SessionId: id,
		Mode:      mode,
		Content:   []protocol.PromptContentPart{{Type: "text", Text: text}},
	}
	var v protocol.PromptResponse
	if err := a.cli.Call(ctx, protocol.MSessionPrompt, req, &v); err != nil {
		a.st.PromptCommand(id, rpcId, nil)
		return "", err
	}
	if v.Command != nil {
		a.st.PromptCommand(id, rpcId, &v)
	}
	return rpcId, nil
}

// PromptWithID is Prompt with an externally minted rpcId (used when the UI
// needs the id up front).
func (a *App) PromptWithID(ctx context.Context, id, text, mode, rpcId string) error {
	a.st.PromptSent(id, text, rpcId)
	req := protocol.PromptRequest{
		SessionId: id,
		Mode:      mode,
		Content:   []protocol.PromptContentPart{{Type: "text", Text: text}},
	}
	var v protocol.PromptResponse
	if err := a.cli.Call(ctx, protocol.MSessionPrompt, req, &v); err != nil {
		a.st.PromptCommand(id, rpcId, nil)
		return err
	}
	if v.Command != nil {
		a.st.PromptCommand(id, rpcId, &v)
	}
	return nil
}

// SetPermission switches one session's permission preset through the host
// /permission command (dsh-permission-presets: sandbox mode + approval
// policy bundle). It needs no model turn and works while a turn runs.
// matched is false when the deployment has no permission command.
func (a *App) SetPermission(ctx context.Context, id, preset string) (outcome *protocol.CommandOutcome, matched bool, err error) {
	res, matched, err := a.cli.CommandExecute(ctx, id, "/permission "+preset)
	if err != nil || res == nil {
		return nil, matched, err
	}
	return &res.Result, true, nil
}

// Cancel stops the active turn.
func (a *App) Cancel(ctx context.Context, id string) error {
	return a.cli.Cancel(ctx, id)
}

// Rename sets the session title.
func (a *App) Rename(ctx context.Context, id, title string) (string, error) {
	t, err := a.cli.Rename(ctx, id, title)
	if err != nil {
		return "", err
	}
	a.st.SetTitle(id, t)
	a.refreshRosterCoalesced()
	return t, nil
}

// Fork starts a session from a completed-turn prefix.
func (a *App) Fork(ctx context.Context, id string, atSeq int64) (string, error) {
	nid, err := a.cli.Fork(ctx, id, atSeq)
	if err != nil {
		return "", err
	}
	a.st.EnsureRow(nid)
	a.refreshRosterCoalesced()
	return nid, nil
}

// Search runs a bounded content search.
func (a *App) Search(ctx context.Context, query string) (*protocol.SessionSearchResponse, error) {
	return a.cli.Search(ctx, query)
}

// UpdateQueue mutates one pending queue item.
func (a *App) UpdateQueue(ctx context.Context, req protocol.QueueUpdateRequest) error {
	return a.cli.UpdateQueue(ctx, req)
}

// Models reads the catalog for a session.
func (a *App) Models(ctx context.Context, id string) (*protocol.SessionModels, error) {
	return a.cli.Models(ctx, id)
}

// SelectModel applies a model selection.
func (a *App) SelectModel(ctx context.Context, id, provider, model, effort string) (*protocol.ModelSelection, error) {
	return a.cli.SelectModel(ctx, id, provider, model, effort)
}

// SelectMode recomposes one session's agent from another preset — the
// deployment's mode switch (standard / PTC / minimal / creator). The host
// allows the switch only while the session is still blank; the returned
// value is the preset id now in force.
func (a *App) SelectMode(ctx context.Context, id, preset string) (string, error) {
	got, err := a.cli.SelectAgentPreset(ctx, id, preset)
	if err != nil {
		return "", err
	}
	a.st.SetAgentPreset(id, got)
	return got, nil
}

// Skills lists the user-invocable skills for a session.
func (a *App) Skills(ctx context.Context, id string) ([]protocol.SkillEntry, error) {
	return a.cli.Skills(ctx, id)
}

// Subagents lists direct children.
func (a *App) Subagents(ctx context.Context, parentId string) (*protocol.SubagentCatalog, error) {
	return a.cli.Subagents(ctx, parentId)
}

// SubagentHistory pages a child's event log (newest first).
func (a *App) SubagentHistory(ctx context.Context, req protocol.SubagentHistoryRequest) (*protocol.HistoryResponse, error) {
	return a.cli.SubagentHistory(ctx, req)
}

// SubagentInterrupt interrupts a running continuable child.
func (a *App) SubagentInterrupt(ctx context.Context, parentSessionId, childSessionId string) error {
	return a.cli.SubagentInterrupt(ctx, parentSessionId, childSessionId)
}

// Descriptions lists agent presets.
func (a *App) Presets(ctx context.Context) (*protocol.AgentPresetListResponse, error) {
	return a.cli.AgentPresets(ctx)
}

// ---- pumps -----------------------------------------------------------------

func (a *App) pumpFrames(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case fr := <-a.dl.Frames():
			a.handleFrame(ctx, fr)
		}
	}
}

func (a *App) pumpStatus(ctx context.Context) {
	// The first uplink is not a reconnect: boot already baselined the
	// roster and the active tail, and a second (stale) tail load here is
	// what raced the first keypress.
	first := true
	for {
		select {
		case <-ctx.Done():
			return
		case up := <-a.dl.Status():
			a.st.SetConnected(up)
			if up {
				if first {
					first = false
				} else {
					a.st.Notify(core.Notice{Level: "ok", Text: i18n.LoadDefault().T("reconnect.ok")})
					a.RebaselineThrottled()
				}
			}
		}
	}
}

// pumpDrops reacts to consumer-stall drop pulses: the frames were lost
// while the pump was stalled, so the state they carried (queue, jobs,
// projections, events) may be stale — re-baseline to repair.
func (a *App) pumpDrops(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.dl.Drops():
			a.st.Notify(core.Notice{Level: "warn", Text: i18n.LoadDefault().T("reconnect.congested")})
			a.RebaselineThrottled()
		}
	}
}

func (a *App) handleFrame(ctx context.Context, fr client.DownlinkFrame) {
	switch fr.Kind {
	case protocol.FMuxEvent:
		ev, err := protocol.DecodeMuxEvent(fr.Payload)
		if err != nil {
			return
		}
		if _, usage := a.st.Event(ev.SessionId, &ev.Event); usage != nil {
			a.recordUsage(ev.SessionId, &ev.Event, usage)
		}
	case protocol.FMuxSubscribed:
		v, err := protocol.DecodeMuxSubscribed(fr.Payload)
		if err == nil {
			a.st.Subscribed(v.SessionId, v.LastSeq)
		}
	case protocol.FMuxApprovalReq:
		v, err := protocol.DecodeMuxApproval(fr.Payload)
		if err != nil {
			return
		}
		args := a.toolArgsFor(v.SessionId, v.CallId)
		a.st.ApprovalRequested(v.SessionId, fr.RpcId, v.ApprovalId, v.ToolName, v.CallId, v.Reason, args)
	case protocol.FMuxApprovalRes:
		v, err := protocol.DecodeMuxApprovalResolved(fr.Payload)
		if err == nil {
			a.st.ApprovalResolved(v.ApprovalId, v.Outcome)
		}
	case protocol.FMuxQuestionReq:
		v, err := protocol.DecodeMuxQuestion(fr.Payload)
		if err == nil {
			a.st.QuestionRequested(v.SessionId, fr.RpcId, v.Questions)
		}
	case protocol.FMuxQuestionRes:
		v, err := protocol.DecodeMuxQuestionResolved(fr.Payload)
		if err == nil {
			a.st.QuestionResolved(v.QuestionRpcId, v.Outcome)
		}
	case protocol.FMuxQueue:
		v, err := protocol.DecodeMuxQueue(fr.Payload)
		if err == nil {
			a.st.MuxQueue(v.SessionId, v.Items)
		}
	case protocol.FMuxJobs:
		v, err := protocol.DecodeMuxJobs(fr.Payload)
		if err == nil {
			a.st.MuxJobs(v.SessionId, v.Jobs)
		}
	case protocol.FMuxProjection:
		v, err := protocol.DecodeMuxProjection(fr.Payload)
		if err != nil {
			return
		}
		a.st.MuxProjection(v.SessionId, v.Key, v.Seq, v.Value)
		if v.Key == "tokenUsage" {
			// The step just sampled: confirm the cumulative baseline —
			// the recorder counts only its advance over the previous
			// one, so this is the in-flight step's tokens landing in
			// the persistent statistics.
			tu := tokenUsageBaseline(map[string]json.RawMessage{"tokenUsage": v.Value})
			if tu != nil {
				a.recordProjectionBaseline(v.SessionId, v.Seq, tu)
			}
		}
	case protocol.FMuxActivity:
		var v struct {
			SessionId string `json:"sessionId"`
			UpdatedAt int64  `json:"updatedAt"`
		}
		if json.Unmarshal(fr.Payload, &v) == nil {
			a.st.TouchActivity(v.SessionId, v.UpdatedAt)
		}
	case protocol.FMuxPresetSelected:
		var v struct {
			SessionId   string `json:"sessionId"`
			AgentPreset string `json:"agentPreset"`
		}
		if json.Unmarshal(fr.Payload, &v) == nil {
			a.st.SetAgentPreset(v.SessionId, v.AgentPreset)
		}
	case protocol.FStreamError:
		v, err := protocol.DecodeStreamError(fr.Payload)
		if err == nil {
			a.st.Notify(core.Notice{Level: "err", Text: "stream: " + v.Error.Message})
		}
	case protocol.FHostSessionAdded, protocol.FHostSessionRemoved,
		protocol.FHostSessionStatus, protocol.FHostAgentError,
		protocol.FHostWorkspaceChanged, protocol.FHostWorkspaceRemoved,
		protocol.FHostOrderChanged, protocol.FHostArchived,
		protocol.FHostRemoteEvent:
		v, err := protocol.DecodeHostFrame(fr.Payload)
		if err == nil {
			a.st.HostFrame(v)
		}
	}
}

// toolArgsFor looks up the last matching tool call's arguments in the
// session transcript, for approval context.
func (a *App) toolArgsFor(sessionId, callId string) string {
	if callId == "" {
		return ""
	}
	snap := a.st.Get(sessionId)
	if snap == nil {
		return ""
	}
	for i := len(snap.Items) - 1; i >= 0; i-- {
		it := snap.Items[i]
		if it.Kind != core.KindAssistant {
			continue
		}
		for j := len(it.Blocks) - 1; j >= 0; j-- {
			if tb := it.Blocks[j].Tool; tb != nil && tb.Id == callId {
				return tb.ArgsFull
			}
		}
	}
	return ""
}

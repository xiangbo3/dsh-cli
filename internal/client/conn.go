// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

// Per-base connection state shared by the unary client and the downlink
// stream of one server: the protocol dialect, the browser session
// credential (the launch token dsh web prints, and the cookie it mints),
// and the host facts the new protocol publishes only on its streams.
package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"dsh-cli/internal/protocol"
)

// Dialect is the host protocol generation a base URL speaks.
type Dialect int

const (
	// DialectOld is the pre-token build: dot-named methods with raw
	// payloads, the dual events.mux/events.host downlinks, and the
	// /api/respond answer channel. It never gates on a cookie.
	DialectOld Dialect = iota
	// DialectNew gates every request on the browser session cookie:
	// ns/method endpoints with args-wrapped payloads and one
	// multiplexed /api/remote.mux downlink carrying logical streams.
	DialectNew
)

// ErrAuthRequired marks a new-host 401 the client could not clear with a
// (re)exchange — the configured token is missing or stale, so the user
// must pass the token dsh web printed at startup. ErrNoToken and
// ErrTokenRejected name the cause (and wrap it), so a caller can show
// the matching recovery hint.
var (
	ErrAuthRequired  = errors.New("auth: cookie missing or stale")
	ErrNoToken       = fmt.Errorf("%w: no launch token configured (pass the ?token=… URL dsh web printed — --token <token> or DSH_LAUNCH_TOKEN)", ErrAuthRequired)
	ErrTokenRejected = fmt.Errorf("%w: launch token rejected (dsh web may have restarted — pass a fresh --token <token>)", ErrAuthRequired)

	// ErrNotFound marks a 404: the endpoint does not exist on the wire
	// the caller spoke — the dialect probe misread the host generation
	// (a body-stripping proxy hides the gate marker), so the caller may
	// retry once on the other wire.
	ErrNotFound = errors.New("http 404")
)

// Conn is the shared state of one server base (a trimmed URL string).
type Conn struct {
	base string

	mu     sync.Mutex
	dial   Dialect // zero until detection answers
	token  string  // launch token ("" = none configured)
	cookie string  // "name=value" for this authority ("" = none held)
	// Exchange throttle: the same token must not be re-exchanged in a
	// burst when a whole request storm 401s (server restarted, stale
	// token). A retry after the window is the next opportunity. While an
	// exchange is in flight, concurrent callers wait on exchCh instead of
	// retrying early (a retry before the mint lands 401s again).
	exchToken string
	exchAt    time.Time
	exchCh    chan struct{}              // in-flight mint; closed on completion
	lastErr   error                      // last mint failure (a throttled no-op reports it)
	saver     func(token, cookie string) // persistence hook (nil = memory only)

	// New-dialect host facts: the streams publish them, the unary side
	// composes answers (host.describe, the $events/result channel) from
	// them.
	clientID   string // $events generation id (the result channel wants it)
	home       string // host home (the ready frame)
	curMu      sync.Mutex
	cursors    map[string]int64 // sessionId → follow snapshot cursor (tail seq)
	wsItems    []protocol.WorkspaceView
	wsArchived []string
	wsWake     chan struct{} // closed when the workspace baseline landed

	// Pending human interactions. The new host splits each approval
	// across two independent deliveries: the approval id rides the
	// session log (approval/asked, on the follow stream), the answerable
	// eventId rides the $events waterfall frame. They are joined on
	// (session, tool, call) before either side is surfaced, so a fast or
	// slow arrival order both resolve.
	appMu sync.Mutex
	pends map[approvalKey]approvalPend
}

type approvalKey struct {
	sessionId string
	toolName  string
	callId    string
}

type approvalPend struct {
	approvalId string // from approval/asked ("" until it lands)
	requestId  string // the waterfall eventId ("" until it lands)
	toolName   string
	callId     string
	reason     string
	at         time.Time
}

// connRegistry is the base → Conn map (Client and Stream both resolve
// their shared state through it; keyed by the trimmed base string).
var connRegistry sync.Map

// ConnFor returns the shared state for base (created on first use).
func ConnFor(base string) *Conn {
	b := trimBase(base)
	v, _ := connRegistry.LoadOrStore(b, newConn(b))
	return v.(*Conn)
}

func newConn(b string) *Conn {
	return &Conn{
		base:   b,
		wsWake: make(chan struct{}),
		pends:  map[approvalKey]approvalPend{},
	}
}

// Dialect returns the detected dialect; zero until detection has run.
func (c *Conn) Dialect() Dialect {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dial
}

// newGateMarker is the index 401 body the cookie-gated build answers
// with. A legacy host that gates on the DSH_TOKEN bearer also 401s the
// bare index but never carries it — such a host keeps the legacy wire
// (the DSH_TOKEN env supplies its bearer), so a 401 without the marker
// classifies as the old build.
const newGateMarker = "dsh web authentication required"

// Detect pins the dialect from one unauthenticated probe: a 200 index
// is the legacy build; a 401 index is the cookie-gated build when its
// body carries the gate marker (a bearer-gated legacy host 401s the
// same probe without it). Transport failure leaves the dialect
// undecided (the caller retries on its normal backoff) and reports the
// error.
func (c *Conn) Detect(ctx context.Context, hc *http.Client) (Dialect, error) {
	c.mu.Lock()
	if c.dial != 0 {
		d := c.dial
		c.mu.Unlock()
		return d, nil
	}
	c.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/", nil)
	if err != nil {
		return 0, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<10))

	c.mu.Lock()
	if c.dial == 0 { // first detector wins
		if resp.StatusCode == http.StatusUnauthorized {
			if strings.Contains(string(body), newGateMarker) {
				c.dial = DialectNew
			} else {
				c.dial = DialectOld // a bearer-gated legacy host
			}
		} else {
			c.dial = DialectOld
		}
	}
	d := c.dial
	c.mu.Unlock()
	return d, nil
}

// SetToken installs the launch token ("" clears it).
func (c *Conn) SetToken(t string) {
	c.mu.Lock()
	c.token = t
	c.mu.Unlock()
}

// SetCookie installs a persisted session cookie ("" clears it).
func (c *Conn) SetCookie(cv string) {
	c.mu.Lock()
	c.cookie = cv
	c.mu.Unlock()
}

// SetCookieSaver installs the persistence hook run after a successful
// exchange (nil detaches; the hook must not call back into the conn).
func (c *Conn) SetCookieSaver(fn func(token, cookie string)) {
	c.mu.Lock()
	c.saver = fn
	c.mu.Unlock()
}

// Token returns the configured launch token.
func (c *Conn) Token() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.token
}

// Cookie returns the session cookie for stamping requests ("").
func (c *Conn) Cookie() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cookie
}

// Exchange swaps the launch token for a fresh session cookie: a GET to
// the index with the token query (exactly one token parameter, no
// cookie), reading the Set-Cookie of the 303. It fails with ErrNoToken
// (nothing configured) or ErrTokenRejected (the host rejected the held
// one); a host that sets no cookie is an old (un-gated) build, which
// needs nothing. Self-throttled per token; a throttled no-op reports the
// remembered failure.
func (c *Conn) Exchange(ctx context.Context, hc *http.Client) error {
	c.mu.Lock()
	token := c.token
	if token == "" {
		c.lastErr = ErrNoToken
		c.mu.Unlock()
		return ErrNoToken
	}
	if c.exchToken == token && time.Since(c.exchAt) < 2*time.Second {
		if inFlight := c.exchCh; inFlight != nil {
			c.mu.Unlock()
			<-inFlight // wait for the in-flight mint to land
		} else {
			c.mu.Unlock()
		}
		// Throttled: a burst 401 must not storm the exchange. Report
		// the remembered failure so the caller still sees the cause
		// (a concurrent mint may have just failed).
		c.mu.Lock()
		err := c.lastErr
		c.mu.Unlock()
		return err
	}
	c.exchToken = token
	c.exchAt = time.Now()
	ch := make(chan struct{})
	c.exchCh = ch
	c.mu.Unlock()
	// The in-flight marker clears (and waiters wake) on every outcome,
	// however the mint ends.
	defer func() {
		c.mu.Lock()
		c.exchCh = nil
		c.mu.Unlock()
		close(ch)
	}()

	// The new host reads exactly one token parameter on the index path;
	// a second parameter or a different path misses the mint branch.
	url := c.base + "/?token=" + url.QueryEscape(token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	// The mint answers 303 → /; following it lands a second bare GET
	// (still cookie-less) that 401s, and Do would return THAT as the
	// mint's answer. Stop at the first hop.
	ehc := *hc
	if ehc.CheckRedirect == nil {
		ehc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	resp, err := ehc.Do(req)
	if err != nil {
		c.mu.Lock()
		c.lastErr = err
		c.mu.Unlock()
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))

	cookie := firstAuthCookie(resp.Header.Values("Set-Cookie"))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		if cookie == "" {
			c.mu.Lock()
			c.lastErr = ErrTokenRejected
			c.mu.Unlock()
			return ErrTokenRejected
		}
	}
	if cookie == "" {
		c.mu.Lock()
		c.lastErr = nil // host set no cookie: un-gated build, nothing to hold
		c.mu.Unlock()
		return nil
	}
	c.mu.Lock()
	c.cookie = cookie
	c.lastErr = nil
	saver := c.saver
	c.mu.Unlock()
	if saver != nil {
		saver(token, cookie)
	}
	return nil
}

// RefreshCookie clears the held cookie when the token changed and
// re-exchanges. Called on a 401: it succeeds only when a token is held
// and the exchange minted (or kept) a cookie, which the caller then
// retries the request against.
func (c *Conn) RefreshCookie(ctx context.Context, hc *http.Client) (bool, error) {
	err := c.Exchange(ctx, hc)
	if err != nil {
		return false, err
	}
	return c.Cookie() != "", nil
}

// SetCursor records a follow snapshot's cursor (the address's tail seq;
// the page endpoint wants a throughSeq the host can verify against it).
func (c *Conn) SetCursor(sessionId string, seq int64) {
	c.curMu.Lock()
	if c.cursors == nil {
		c.cursors = map[string]int64{}
	}
	c.cursors[sessionId] = seq
	c.curMu.Unlock()
}

// Cursor returns the last known tail seq of one session (-1 when no
// follow snapshot has landed for it yet).
func (c *Conn) Cursor(sessionId string) int64 {
	c.curMu.Lock()
	defer c.curMu.Unlock()
	if v, ok := c.cursors[sessionId]; ok {
		return v
	}
	return -1
}

// ClientID returns the $events generation id (new dialect; "" when the
// stream has not reported its ready frame yet).
func (c *Conn) ClientID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.clientID
}

// Home returns the host home published by the ready frame ("" until it
// lands; the unary side falls back for composed answers).
func (c *Conn) Home() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.home
}

// SetHostFacts records the ready frame payload.
func (c *Conn) SetHostFacts(clientID, home string) {
	c.mu.Lock()
	c.clientID = clientID
	c.home = home
	c.mu.Unlock()
}

// WorkspaceBaselined reports whether the workspace registry mirror
// (new dialect) received its baseline.
func (c *Conn) WorkspaceBaselined() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.wsItems != nil
}

// WorkspaceBaseline returns the registry mirror (new dialect) and, via
// the wake channel, blocks callers until the first baseline lands
// (already-landed state returns immediately; ctx cancellation or the
// timeout bound the wait).
func (c *Conn) WorkspaceBaseline(ctx context.Context) (items []protocol.WorkspaceView, archived []string, ok bool) {
	c.mu.Lock()
	if c.wsItems != nil {
		items, archived = c.wsItems, c.wsArchived
		c.mu.Unlock()
		return items, archived, true
	}
	wake := c.wsWake
	c.mu.Unlock()
	select {
	case <-wake:
	case <-ctx.Done():
	}
	c.mu.Lock()
	if c.wsItems != nil {
		items, archived = c.wsItems, c.wsArchived
	}
	c.mu.Unlock()
	return items, archived, c.wsItems != nil
}

// SetWorkspaceBaseline installs the registry mirror and wakes waiters.
func (c *Conn) SetWorkspaceBaseline(items []protocol.WorkspaceView, archived []string) {
	if items == nil {
		items = []protocol.WorkspaceView{}
	}
	c.mu.Lock()
	if c.wsItems == nil {
		close(c.wsWake)
	}
	c.wsItems = items
	c.wsArchived = archived
	c.mu.Unlock()
}

// WorkspaceApply folds one registry increment into the mirror (the
// workspace/follow stream is the new host's only registry surface).
func (c *Conn) WorkspaceApply(kind string, workspace *protocol.WorkspaceView, workspaceID string, order []string, archived []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if kind == "order" {
		c.reorder(order)
		return
	}
	if kind == "archived" {
		c.wsArchived = archived
		return
	}
	if workspaceID != "" { // remove
		out := c.wsItems[:0]
		for _, w := range c.wsItems {
			if w.WorkspaceId != workspaceID {
				out = append(out, w)
			}
		}
		c.wsItems = out
		return
	}
	for i, w := range c.wsItems { // upsert
		if w.WorkspaceId == workspace.WorkspaceId {
			c.wsItems[i] = *workspace
			return
		}
	}
	c.wsItems = append(c.wsItems, *workspace)
}

func (c *Conn) reorder(ids []string) {
	pos := map[string]int{}
	for i, id := range ids {
		pos[id] = i
	}
	out := make([]protocol.WorkspaceView, 0, len(c.wsItems))
	for _, w := range c.wsItems {
		if _, ok := pos[w.WorkspaceId]; ok {
			out = append(out, w)
		}
	}
	for i := 0; i < len(out); i++ { // stable sort by the host order
		for j := i + 1; j < len(out); j++ {
			if pos[out[j].WorkspaceId] < pos[out[i].WorkspaceId] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	c.wsItems = out
}

// ---------------------------------------------------------------------------
// pending approvals (the two-sided id join)
// ---------------------------------------------------------------------------

// ApprovalAsked parks or completes the session-log side of an approval
// (approval/asked data). It reports the full interaction when both
// sides are in; otherwise the pending entry is kept for the waterfall.
func (c *Conn) ApprovalAsked(sessionId, approvalId, toolName, callId, reason string) (requestId string, complete bool) {
	c.appMu.Lock()
	defer c.appMu.Unlock()
	k := approvalKey{sessionId, toolName, callId}
	p := c.pends[k]
	p.approvalId = approvalId
	p.reason = reason
	if p.requestId != "" {
		delete(c.pends, k)
		return p.requestId, true
	}
	p.at = time.Now()
	c.pends[k] = p
	return "", false
}

// ApprovalWaterfall parks or completes the $events waterfall side
// (the answerable eventId). It reports the approval id when both sides
// are in.
func (c *Conn) ApprovalWaterfall(sessionId, requestId, toolName, callId, reason string) (approvalId string, complete bool) {
	c.appMu.Lock()
	defer c.appMu.Unlock()
	k := approvalKey{sessionId, toolName, callId}
	p := c.pends[k]
	p.requestId = requestId
	if p.reason == "" {
		p.reason = reason
	}
	if p.approvalId != "" {
		delete(c.pends, k)
		return p.approvalId, true
	}
	p.at = time.Now()
	c.pends[k] = p
	return "", false
}

// StaleApprovals drops pending sides older than maxAge (a delivery the
// stream lost; the host re-asks on reconnect re-baselines).
func (c *Conn) StaleApprovals(maxAge time.Time) {
	c.appMu.Lock()
	defer c.appMu.Unlock()
	for k, p := range c.pends {
		if p.at.Before(maxAge) {
			delete(c.pends, k)
		}
	}
}

// firstAuthCookie extracts "name=value" from the Set-Cookie values of a
// response: the new host mints exactly one dsh-auth-* cookie per
// exchange; anything else (an old build, a proxy) sets none we want.
func firstAuthCookie(values []string) string {
	for _, v := range values {
		i := strings.IndexByte(v, ';')
		if i <= 0 {
			continue
		}
		if strings.HasPrefix(v[:i], "dsh-auth-") {
			return v[:i]
		}
	}
	return ""
}

// trimBase normalizes a base URL for conn keys and request joining.
func trimBase(u string) string { return strings.TrimRight(u, "/") }

// TrimBase is the exported form (the app compares stored cookie bases).
func TrimBase(u string) string { return strings.TrimRight(u, "/") }

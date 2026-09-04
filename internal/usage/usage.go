// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

// Package usage is dsh-cli's persistent token-usage statistics. Every
// counted assistant message folds into a per-session, per-workspace,
// per-local-day table under ~/.dsh-cli (usage.json), and the /status
// popup's report — all-time total, this month, this week, today, per
// workspace, per session — is computed from that table.
//
// Counting is idempotent per (session, event seq): a history page the
// client re-loads (boot, reconnect, scrolling to the top) never double-
// counts, because already-counted seqs are remembered per session.
// Recording is best-effort throughout — a missing or unwritable home
// directory degrades the statistics, never the app.
package usage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"dsh-cli/internal/protocol"
)

const fileVersion = 1

// flushDelay debounces the disk write behind a burst of counted
// messages (a streaming turn emits one assistant message per LLM step).
const flushDelay = time.Second

// maxSeqsPerSession bounds the per-session dedup list: the oldest seqs
// drop once the list grows past it (a re-load of history older than the
// horizon could then double-count; the horizon is far beyond any
// tail/older page a client actually pulls). Var so tests can shrink it.
var maxSeqsPerSession = 20000

// DayTotals is one local day's token counters (the counts are disjoint,
// mirroring protocol.TokenUsage).
type DayTotals struct {
	In  int `json:"in,omitempty"`
	Out int `json:"out,omitempty"`
	CR  int `json:"cr,omitempty"` // cache-read
	CW  int `json:"cw,omitempty"` // cache-write
	R   int `json:"r,omitempty"`  // reasoning
}

func (d DayTotals) Add(o DayTotals) DayTotals {
	d.In += o.In
	d.Out += o.Out
	d.CR += o.CR
	d.CW += o.CW
	d.R += o.R
	return d
}

// Totals is an aggregated counter set.
type Totals struct {
	In, Out, CR, CW, R int
}

func (t *Totals) addDay(d DayTotals) {
	t.In += d.In
	t.Out += d.Out
	t.CR += d.CR
	t.CW += d.CW
	t.R += d.R
}

// Count is the in+out token volume.
func (t Totals) Count() int { return t.In + t.Out }

// File is the on-disk document (~/.dsh-cli/usage.json).
type File struct {
	V int `json:"v"`
	// Seqs is the counted assistant-message seqs per session (the
	// idempotency set, trimmed to the recent horizon).
	Seqs map[string][]int64 `json:"seqs,omitempty"`
	// Last is the last counted usage's unixmilli per session.
	Last map[string]int64 `json:"last,omitempty"`
	// Days: session → workspace key → local day → counters.
	Days map[string]map[string]map[string]DayTotals `json:"days,omitempty"`
}

// NamedTotals is one workspace's (or any keyed bucket's) total.
type NamedTotals struct {
	Key string
	T   Totals
}

// SessionTotals is one session's total with its last-activity stamp.
type SessionTotals struct {
	ID     string
	LastTS int64
	T      Totals
}

// Report is the /status popup's data: the fixed calendar windows (local
// time) plus the per-workspace and per-session breakdowns.
type Report struct {
	Total      Totals
	Month      Totals
	Week       Totals
	Day        Totals
	Workspaces []NamedTotals
	Sessions   []SessionTotals
}

// Recorder folds usage into the document. All methods are goroutine-safe
// (the downlink pump records while the UI reads the report).
type Recorder struct {
	path string

	mu    sync.Mutex
	f     File
	seen  map[string]map[int64]bool
	dirty bool

	fmu sync.Mutex
	tm  *time.Timer
}

// DefaultPath is the document's location: ~/.dsh-cli/usage.json ("" and
// false when the home directory is unresolvable).
func DefaultPath() (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", false
	}
	return filepath.Join(home, ".dsh-cli", "usage.json"), true
}

// New opens the document at path (missing: start empty; corrupt: quarantine
// as <path>.bad-<unix> and start empty) so one bad write never hides the
// whole history again.
func New(path string) *Recorder {
	r := &Recorder{
		path: path,
		f: File{
			V:    fileVersion,
			Seqs: map[string][]int64{},
			Last: map[string]int64{},
			Days: map[string]map[string]map[string]DayTotals{},
		},
		seen: map[string]map[int64]bool{},
	}
	if b, err := os.ReadFile(path); err == nil {
		var f File
		if json.Unmarshal(b, &f) == nil && f.V == fileVersion {
			if f.Seqs == nil {
				f.Seqs = map[string][]int64{}
			}
			if f.Last == nil {
				f.Last = map[string]int64{}
			}
			if f.Days == nil {
				f.Days = map[string]map[string]map[string]DayTotals{}
			}
			r.f = f
			for sid, seqs := range f.Seqs {
				set := make(map[int64]bool, len(seqs))
				for _, s := range seqs {
					set[s] = true
				}
				r.seen[sid] = set
			}
			return r
		}
		_ = os.Rename(path, fmt.Sprintf("%s.bad-%d", path, time.Now().Unix()))
	}
	return r
}

// Record folds one assistant message's usage into the document and
// reports whether it was new (false: an already-counted seq, or zero
// counters). ts is the event's unixmilli (0: now), seq the per-session
// event seq (0: no idempotency), ws the workspace key (a workspace id,
// a cwd path, or "" filed under "unknown").
func (r *Recorder) Record(sid, ws string, ts, seq int64, u *protocol.TokenUsage) bool {
	if sid == "" || u == nil {
		return false
	}
	if u.InputTokens == 0 && u.OutputTokens == 0 && u.CacheReadTokens == 0 &&
		u.CacheWriteTokens == 0 && u.ReasoningTokens == 0 {
		return false
	}
	if ts <= 0 {
		ts = time.Now().UnixMilli()
	}
	key := ws
	if key == "" {
		key = "unknown"
	}
	day := time.UnixMilli(ts).Format("2006-01-02")

	r.mu.Lock()
	defer r.mu.Unlock()

	if seq > 0 {
		set := r.seen[sid]
		if set == nil {
			set = map[int64]bool{}
			r.seen[sid] = set
		}
		if set[seq] {
			return false
		}
		set[seq] = true
		r.f.Seqs[sid] = append(r.f.Seqs[sid], seq)
		if n := len(r.f.Seqs[sid]) - maxSeqsPerSession; n > 0 {
			// Trim the horizon and rebuild that session's set.
			kept := r.f.Seqs[sid][n:]
			set = make(map[int64]bool, len(kept))
			for _, s := range kept {
				set[s] = true
			}
			r.f.Seqs[sid] = kept
			r.seen[sid] = set
		}
	}

	sdays := r.f.Days[sid]
	if sdays == nil {
		sdays = map[string]map[string]DayTotals{}
		r.f.Days[sid] = sdays
	}
	wsdays := sdays[key]
	if wsdays == nil {
		wsdays = map[string]DayTotals{}
		sdays[key] = wsdays
	}
	wsdays[day] = wsdays[day].Add(DayTotals{
		In: u.InputTokens, Out: u.OutputTokens,
		CR: u.CacheReadTokens, CW: u.CacheWriteTokens, R: u.ReasoningTokens,
	})
	if ts > r.f.Last[sid] {
		r.f.Last[sid] = ts
	}
	r.dirty = true
	r.scheduleFlush()
	return true
}

// scheduleFlush arms the debounced write (callers hold r.mu; the timer
// callback takes the locks in the same order — mu then fmu).
func (r *Recorder) scheduleFlush() {
	r.fmu.Lock()
	defer r.fmu.Unlock()
	if r.tm != nil {
		r.tm.Stop()
	}
	r.tm = time.AfterFunc(flushDelay, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.flushLocked()
	})
}

// flushLocked writes the document (temp file + rename) when dirty.
// Callers hold r.mu; disk problems are swallowed (best-effort).
func (r *Recorder) flushLocked() {
	if !r.dirty {
		return
	}
	b, err := json.MarshalIndent(&r.f, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return
	}
	tmp := r.path + ".tmp"
	// 0600: per-session token totals — private stat, not world data
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return
	}
	if err := os.Rename(tmp, r.path); err != nil {
		_ = os.Remove(tmp)
		return
	}
	r.dirty = false
}

// Close flushes any pending totals to disk.
func (r *Recorder) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flushLocked()
}

// Report computes the popup's data. now pins the calendar (tests pass a
// fixed instant); the windows follow the local calendar: today = the
// current local day, week = the current Monday-based week, month = the
// current local month. A day that falls in several windows counts in
// each of them (today is in this week and this month).
func (r *Recorder) Report(now time.Time) *Report {
	r.mu.Lock()
	defer r.mu.Unlock()

	d := now.In(time.Local)
	today := d.Format("2006-01-02")
	month := d.Format("2006-01") + "-"
	// Monday-based ISO week: the seven day keys of the current week.
	monday := d.AddDate(0, 0, -((int(d.Weekday()) + 6) % 7))
	week := make(map[string]bool, 7)
	for i := 0; i < 7; i++ {
		week[monday.AddDate(0, 0, i).Format("2006-01-02")] = true
	}

	rep := &Report{}
	ws := map[string]*Totals{}
	sess := map[string]*SessionTotals{}
	for sid, wss := range r.f.Days {
		st := &SessionTotals{ID: sid, LastTS: r.f.Last[sid]}
		for wsKey, days := range wss {
			t := ws[wsKey]
			if t == nil {
				t = &Totals{}
				ws[wsKey] = t
			}
			for k, v := range days {
				st.T.addDay(v)
				t.addDay(v)
				rep.Total.addDay(v)
				if k == today {
					rep.Day.addDay(v)
				}
				if week[k] {
					rep.Week.addDay(v)
				}
				if strings.HasPrefix(k, month) {
					rep.Month.addDay(v)
				}
			}
		}
		sess[sid] = st
	}
	for k, t := range ws {
		rep.Workspaces = append(rep.Workspaces, NamedTotals{Key: k, T: *t})
	}
	sort.Slice(rep.Workspaces, func(i, j int) bool {
		a, b := rep.Workspaces[i], rep.Workspaces[j]
		if a.T.Count() != b.T.Count() {
			return a.T.Count() > b.T.Count()
		}
		return a.Key < b.Key
	})
	for _, st := range sess {
		rep.Sessions = append(rep.Sessions, *st)
	}
	sort.Slice(rep.Sessions, func(i, j int) bool {
		a, b := rep.Sessions[i], rep.Sessions[j]
		if a.LastTS != b.LastTS {
			return a.LastTS > b.LastTS
		}
		return a.ID < b.ID
	})
	return rep
}

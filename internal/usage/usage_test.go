// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package usage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"dsh-cli/internal/protocol"
)

func tok(in, out int) *protocol.TokenUsage {
	return &protocol.TokenUsage{InputTokens: in, OutputTokens: out}
}

// dayAt is the unixmilli of local date d at hh:mm (local calendar).
func dayAt(t *testing.T, d time.Time, hh, mm int) int64 {
	t.Helper()
	return time.Date(d.Year(), d.Month(), d.Day(), hh, mm, 0, 0, time.Local).UnixMilli()
}

func TestRecordDedup(t *testing.T) {
	r := New(filepath.Join(t.TempDir(), "usage.json"))
	u := tok(100, 50)
	if !r.Record("s1", "w1", 123, 7, u) {
		t.Fatal("first record should count")
	}
	if r.Record("s1", "w1", 123, 7, u) {
		t.Fatal("the same (sid, seq) counted twice")
	}
	// A re-load delivers older seqs out of order: the unknown old seq
	// counts, the already-counted one does not.
	if !r.Record("s1", "w1", 45, 3, u) {
		t.Fatal("new older-seq record should count")
	}
	if r.Record("s1", "w1", 45, 3, u) {
		t.Fatal("older-seq record counted twice")
	}
	if r.Record("s1", "w1", 123, 7, tok(1, 1)) {
		t.Fatal("re-delivered seq counted with fresh counters")
	}
	if rep := r.Report(time.UnixMilli(123)); rep.Total.In != 200 || rep.Total.Out != 100 {
		t.Fatalf("total = %+v, want in 200 / out 100", rep.Total)
	}
	// A different session is its own idempotency domain.
	if !r.Record("s2", "w1", 123, 7, u) {
		t.Fatal("seq 7 in another session should count")
	}
	if rep := r.Report(time.UnixMilli(123)); rep.Total.In != 300 {
		t.Fatalf("total = %+v, want in 300", rep.Total)
	}
}

// TestReportWindows pins the calendar semantics on a known calendar:
// 2025-09-03 is a Wednesday; its Monday-based week is 09-01..09-07 and
// its month is 2025-09.
func TestReportWindows(t *testing.T) {
	now := time.Date(2025, 9, 3, 12, 0, 0, 0, time.Local)
	r := New(filepath.Join(t.TempDir(), "usage.json"))
	day := time.Date(2025, 9, 3, 0, 0, 0, 0, time.Local)
	at := func(dayOffset int, hh int) int64 {
		return dayAt(t, day.AddDate(0, 0, dayOffset), hh, 0)
	}
	u := tok(10, 5)
	r.Record("s", "w", at(0, 9), 1, u)   // 09-03: today, this week, this month
	r.Record("s", "w", at(-2, 1), 2, u)  // 09-01 (Mon): this week, this month
	r.Record("s", "w", at(12, 0), 3, u)  // 09-15: this month only
	r.Record("s", "w", at(-5, 0), 4, u)  // 08-29: total only
	r.Record("s", "w", at(-19, 0), 5, u) // 08-15: total only
	rep := r.Report(now)
	want := map[string]struct{ in, out int }{
		"total": {50, 25},
		"month": {30, 15},
		"week":  {20, 10},
		"day":   {10, 5},
	}
	got := map[string]Totals{
		"total": rep.Total,
		"month": rep.Month,
		"week":  rep.Week,
		"day":   rep.Day,
	}
	for name, w := range want {
		if g := got[name]; g.In != w.in || g.Out != w.out {
			t.Errorf("%s = in %d / out %d, want in %d / out %d", name, g.In, g.Out, w.in, w.out)
		}
	}
}

func TestReportGrouping(t *testing.T) {
	now := time.Date(2025, 9, 3, 12, 0, 0, 0, time.Local)
	r := New(filepath.Join(t.TempDir(), "usage.json"))
	d0 := dayAt(t, now, 10, 0)
	r.Record("s1", "w1", d0, 1, tok(100, 10))
	r.Record("s1", "w1", d0, 2, tok(50, 5))
	r.Record("s2", "w2", d0, 1, tok(10, 1))
	r.Record("s2", "w1", d0, 2, tok(20, 2))
	r.Record("s3", "", d0, 1, tok(300, 30)) // unknown workspace
	rep := r.Report(now)

	// Workspaces: busiest first — the "" → unknown bucket (330) beats
	// w1 (170+17), which beats w2 (10+1).
	if len(rep.Workspaces) != 3 {
		t.Fatalf("workspaces = %d rows, want 3", len(rep.Workspaces))
	}
	if rep.Workspaces[0].Key != "unknown" || rep.Workspaces[0].T.In != 300 || rep.Workspaces[0].T.Out != 30 {
		t.Errorf("workspace[0] = %+v, want the unknown bucket first", rep.Workspaces[0])
	}
	if rep.Workspaces[1].Key != "w1" || rep.Workspaces[1].T.In != 170 || rep.Workspaces[1].T.Out != 17 {
		t.Errorf("workspace[1] = %+v, want w1 in 170 / out 17", rep.Workspaces[1])
	}
	if rep.Workspaces[2].Key != "w2" {
		t.Errorf("workspace[2] = %+v, want w2 last", rep.Workspaces[2])
	}
	// Sessions: most recent first; all sessions share the same stamp, so
	// the id tiebreak (ascending) orders them s1, s2, s3.
	if len(rep.Sessions) != 3 {
		t.Fatalf("sessions = %d rows, want 3", len(rep.Sessions))
	}
	if rep.Sessions[0].ID != "s1" || rep.Sessions[0].T.In != 150 || rep.Sessions[0].T.Out != 15 {
		t.Errorf("session[0] = %+v, want s1 in 150 / out 15", rep.Sessions[0])
	}
	if rep.Sessions[0].LastTS != d0 {
		t.Errorf("session[0].LastTS = %d, want %d", rep.Sessions[0].LastTS, d0)
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "usage.json")
	t1 := dayAt(t, time.Date(2025, 9, 3, 0, 0, 0, 0, time.Local), 9, 0)
	r := New(p)
	r.Record("s1", "w1", t1, 1, tok(10, 5))
	r.Record("s2", "", t1, 9, tok(7, 3))
	r.Close()
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("document not on disk: %v", err)
	}

	r2 := New(p)
	rep := r2.Report(time.Now())
	if rep.Total.In != 17 || rep.Total.Out != 8 {
		t.Fatalf("reloaded total = %+v, want in 17 / out 8", rep.Total)
	}
	if rep.Total.CR != 0 {
		t.Fatalf("reloaded total CR = %d", rep.Total.CR)
	}
	// The dedup set survived the round trip.
	if r2.Record("s1", "w1", t1, 1, tok(10, 5)) {
		t.Fatal("already-counted seq re-counted after reload")
	}
	if !r2.Record("s1", "w1", t1+1, 2, tok(1, 1)) {
		t.Fatal("new seq after reload should count")
	}
	rep = r2.Report(time.Now())
	if rep.Total.In != 18 {
		t.Fatalf("total after new record = %+v, want in 18", rep.Total)
	}
}

func TestCorruptFileQuarantined(t *testing.T) {
	p := filepath.Join(t.TempDir(), "usage.json")
	if err := os.WriteFile(p, []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := New(p)
	if rep := r.Report(time.Now()); rep.Total.Count() != 0 {
		t.Fatalf("corrupt document leaked totals: %+v", rep.Total)
	}
	matches, _ := filepath.Glob(p + ".bad-*")
	if len(matches) != 1 {
		t.Fatalf("quarantine = %v, want exactly one .bad- file", matches)
	}
}

func TestSeqTrim(t *testing.T) {
	old := maxSeqsPerSession
	maxSeqsPerSession = 3
	defer func() { maxSeqsPerSession = old }()
	r := New(filepath.Join(t.TempDir(), "usage.json"))
	for i := int64(1); i <= 5; i++ {
		if !r.Record("s", "w", 1000+i, i, tok(1, 1)) {
			t.Fatalf("record %d did not count", i)
		}
	}
	if got := len(r.f.Seqs["s"]); got != 3 {
		t.Fatalf("seqs = %d, want the trim horizon of 3", got)
	}
	// The oldest horizon (seqs 1, 2) dropped: a re-load of ancient
	// history may re-count seq 1 (and the re-record slides the horizon,
	// dropping seq 3); the still-kept horizon dedups seq 4.
	if !r.Record("s", "w", 1001, 1, tok(1, 1)) {
		t.Fatal("seq 1 should re-count after it fell off the horizon")
	}
	if r.Record("s", "w", 1004, 4, tok(1, 1)) {
		t.Fatal("seq 4 (kept) should still dedup")
	}
}

func hostTok(in, out, cr, cw int) *protocol.TokenUsageHost {
	return &protocol.TokenUsageHost{In: in, Out: out, CR: cr, CW: cw}
}

func TestRecordProjectionBaselines(t *testing.T) {
	r := New(filepath.Join(t.TempDir(), "usage.json"))
	// The first baseline counts the cumulative in full (the session has
	// no per-event record yet).
	if !r.RecordProjection("s1", "w1", 1000, 10, hostTok(100, 10, 50, 0)) {
		t.Fatal("first baseline should count")
	}
	// The next baseline counts only the advance over the previous one.
	if !r.RecordProjection("s1", "w1", 2000, 20, hostTok(160, 30, 150, 5)) {
		t.Fatal("advanced baseline should count its advance")
	}
	// A re-delivered seq is a no-op; a stale lower-seq baseline is too
	// (the cumulative is monotone in seq).
	if r.RecordProjection("s1", "w1", 3000, 20, hostTok(900, 900, 900, 900)) {
		t.Fatal("re-delivered baseline counted twice")
	}
	if r.RecordProjection("s1", "w1", 3000, 15, hostTok(900, 900, 900, 900)) {
		t.Fatal("stale baseline counted")
	}
	rep := r.Report(time.Now())
	if rep.Total.In != 160 || rep.Total.Out != 30 || rep.Total.CR != 150 || rep.Total.CW != 5 {
		t.Fatalf("total = %+v, want the final cumulative", rep.Total)
	}
	if !r.ProjectionManaged("s1") || r.ProjectionManaged("s2") {
		t.Fatal("managed flags wrong")
	}
}

func TestRecordProjectionRepairsPerEventTotals(t *testing.T) {
	r := New(filepath.Join(t.TempDir(), "usage.json"))
	// Per-event counting already recorded part of the session's
	// canonicals (what the old scheme would have on disk).
	if !r.Record("s1", "w1", 1000, 7, &protocol.TokenUsage{InputTokens: 40, OutputTokens: 4}) {
		t.Fatal("per-event record should count")
	}
	// The first host baseline counts only the difference — the
	// already-recorded part must not count again.
	if !r.RecordProjection("s1", "w1", 2000, 30, hostTok(100, 20, 80, 0)) {
		t.Fatal("first baseline over per-event totals should count the difference")
	}
	rep := r.Report(time.Now())
	if rep.Total.In != 100 || rep.Total.Out != 20 || rep.Total.CR != 80 {
		t.Fatalf("total = %+v, want exactly the host cumulative (no double count)", rep.Total)
	}
	if !r.ProjectionManaged("s1") {
		t.Fatal("session should be projection-managed")
	}
	// Subsequent baselines continue from the last confirmed value.
	if !r.RecordProjection("s1", "w1", 3000, 40, hostTok(150, 25, 200, 0)) {
		t.Fatal("second baseline should count its advance")
	}
	rep = r.Report(time.Now())
	if rep.Total.In != 150 || rep.Total.Out != 25 || rep.Total.CR != 200 {
		t.Fatalf("total = %+v, want the new cumulative", rep.Total)
	}
}

func TestRecordProjectionPersistence(t *testing.T) {
	p := filepath.Join(t.TempDir(), "usage.json")
	r := New(p)
	r.RecordProjection("s1", "w1", 1000, 10, hostTok(100, 10, 50, 0))
	r.Close()
	r2 := New(p)
	// The baseline survived the round trip: a re-delivered same-seq
	// value is a no-op; the next seq counts only the advance.
	if r2.RecordProjection("s1", "w1", 2000, 10, hostTok(100, 10, 50, 0)) {
		t.Fatal("reloaded baseline re-counted")
	}
	if !r2.RecordProjection("s1", "w1", 2000, 20, hostTok(120, 15, 60, 0)) {
		t.Fatal("advance past a reloaded baseline should count")
	}
	rep := r2.Report(time.Now())
	if rep.Total.In != 120 || rep.Total.Out != 15 || rep.Total.CR != 60 {
		t.Fatalf("total = %+v, want the cumulative", rep.Total)
	}
}

func TestZeroAndNilUsage(t *testing.T) {
	r := New(filepath.Join(t.TempDir(), "usage.json"))
	if r.Record("s", "w", 1, 1, nil) {
		t.Fatal("nil usage counted")
	}
	if r.Record("s", "w", 1, 2, &protocol.TokenUsage{}) {
		t.Fatal("all-zero usage counted")
	}
	if r.Record("", "w", 1, 3, tok(1, 1)) {
		t.Fatal("empty session id counted")
	}
	// A seqless event has no idempotency: every delivery counts.
	if !r.Record("s", "w", 1, 0, tok(1, 1)) || !r.Record("s", "w", 2, 0, tok(1, 1)) {
		t.Fatal("seqless records should count")
	}
	if rep := r.Report(time.Now()); rep.Total.In != 2 || rep.Total.Out != 2 {
		t.Fatalf("total = %+v, want in 2 / out 2", rep.Total)
	}
}

// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

// Package ui tests: parked answerable frames (question / approval) must
// be surfaced — the web client keeps them in the conversation until
// answered, and the TUI's equivalent is the auto-opened answer modal.
package ui

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"dsh-cli/internal/app"
	"dsh-cli/internal/core"
	"dsh-cli/internal/protocol"

	tea "github.com/charmbracelet/bubbletea"
)

// TestQuestionAutoOpenParkedReopen covers the surfacing chain: a
// question/requested frame for the active session auto-opens the answer
// modal on the next dirty pulse (previously the modal was never pushed,
// so the batch sat unseen in the store until the server gave up), a
// second pulse does not stack a second modal, esc parks the batch with a
// toast instead of answering, and ctrl+i reopens it.
func TestQuestionAutoOpenParkedReopen(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a := app.New(newFakeHost(t).URL)
	a.Start(ctx)
	m := NewModel(a)
	m.W, m.H = 120, 40

	st := a.Store()
	st.SetSessions([]protocol.SessionSummary{{SessionId: "s1", Cwd: "/tmp"}})
	st.SetActive("s1")
	st.QuestionRequested("s1", "rpc-1", []protocol.QuestionItem{{
		Id:       "q1",
		Question: "Pick one?",
		Options:  []protocol.QuestionOption{{Label: "A"}, {Label: "B"}},
	}})

	// dirty pulse: the frame arrived; the modal must open.
	m.Update(dirtyMsg{})
	if got := len(m.mods); got != 1 {
		t.Fatalf("after dirty pulse: modals = %d (want 1)", got)
	}
	qm, ok := m.topModal().(*questionModal)
	if !ok {
		t.Fatalf("top modal = %T (want *questionModal)", m.topModal())
	}
	if qm.pen.RpcId != "rpc-1" {
		t.Fatalf("modal bound to rpc %q (want rpc-1)", qm.pen.RpcId)
	}

	// A repeated pulse must not stack a second modal.
	m.Update(dirtyMsg{})
	if got := len(m.mods); got != 1 {
		t.Fatalf("after second pulse: modals = %d (want 1, no stack)", got)
	}

	// esc parks the batch (no answer sent) and toasts.
	t0 := len(m.toasts)
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if got := len(m.mods); got != 0 {
		t.Fatalf("after esc: modals = %d (want 0, parked)", got)
	}
	if len(m.toasts) == t0 {
		t.Fatalf("esc parked the question but added no toast")
	}

	// ctrl+i reopens the parked batch.
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlI})
	if got := len(m.mods); got != 1 {
		t.Fatalf("after ctrl+i: modals = %d (want 1, reopened)", got)
	}
	qm2, ok := m.topModal().(*questionModal)
	if !ok {
		t.Fatalf("top modal = %T (want *questionModal)", m.topModal())
	}

	// Answer the (only) question: select option A, enter submits.
	qm2.toggleOpt()
	if _, handled := qm2.update(tea.KeyMsg{Type: tea.KeyEnter}); !handled {
		t.Fatalf("enter on the final question was not handled")
	}
	if !qm2.sub {
		t.Fatalf("enter on the final question did not mark the batch submitted")
	}
	if ans := qm2.answers(); len(ans.Answer.Answers) != 1 || len(ans.Answer.Answers[0].Selected) != 1 || ans.Answer.Answers[0].Selected[0] != "A" {
		t.Fatalf("answers = %+v (want one item selecting A)", ans)
	}
}

// TestApprovalAutoOpen covers the same surfacing for approval/requested
// frames: the approval modal auto-opens on the dirty pulse, and the
// allow chord (ctrl+a) answers and closes it.
func TestApprovalAutoOpen(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a := app.New(newFakeHost(t).URL)
	a.Start(ctx)
	m := NewModel(a)
	m.W, m.H = 120, 40

	st := a.Store()
	st.SetSessions([]protocol.SessionSummary{{SessionId: "s1", Cwd: "/tmp"}})
	st.SetActive("s1")
	st.ApprovalRequested("s1", "rpc-2", "app-1", "bash", "call-1", "needs write access", "ls -la")

	m.Update(dirtyMsg{})
	if got := len(m.mods); got != 1 {
		t.Fatalf("after dirty pulse: modals = %d (want 1)", got)
	}
	am, ok := m.topModal().(*approvalModal)
	if !ok {
		t.Fatalf("top modal = %T (want *approvalModal)", m.topModal())
	}
	if am.pen.ApprovalId != "app-1" {
		t.Fatalf("modal bound to approval %q (want app-1)", am.pen.ApprovalId)
	}

	// A bash-family tool takes a second confirming allow: the first
	// ctrl+a arms (the modal stays open), the second answers and closes.
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	if got := len(m.mods); got != 1 {
		t.Fatalf("after first ctrl+a: modals = %d (want 1, armed)", got)
	}
	if !m.topModal().(*approvalModal).armed {
		t.Fatalf("first ctrl+a did not arm the bash approval")
	}
	// A reject-family key while armed backs out instead of rejecting.
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.topModal().(*approvalModal).armed {
		t.Fatalf("esc while armed did not disarm")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	if got := len(m.mods); got != 0 {
		t.Fatalf("after confirming ctrl+a: modals = %d (want 0, answered)", got)
	}
}

// TestFlashTurnEndsNonActive pins the nil-map regression (the first
// dirty pulse with turn-end rows must not panic on the unseen-seq
// record) and the flash rule: first sight is silent, a newer turn-end
// on a non-active session flashes its roster row.
func TestFlashTurnEndsNonActive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a := app.New(newFakeHost(t).URL)
	a.Start(ctx)
	m := NewModel(a)
	m.W, m.H = 120, 40

	st := a.Store()
	st.SetSessions([]protocol.SessionSummary{
		{SessionId: "s1", Cwd: "/tmp"},
		{SessionId: "s2", Cwd: "/tmp"},
	})
	st.SetActive("s1")
	mkEnd := func(seq, turn int) *protocol.SessionEvent {
		b, _ := json.Marshal(protocol.TurnEndEventData{Turn: turn, Reason: protocol.TurnEndReason{Kind: "completed"}})
		ev := protocol.SessionEvent{Type: "turn/end", Seq: int64(seq), Time: int64(seq) * 1000, Data: b}
		return &ev
	}
	st.Event("s2", mkEnd(3, 1))

	m.Update(dirtyMsg{}) // first sight: must not panic (nil maps)
	if _, ok := m.flashEnds["s2"]; ok {
		t.Fatalf("first sight flashed (want the silent record)")
	}

	st.Event("s2", mkEnd(7, 2))
	m.Update(dirtyMsg{})
	if until := m.flashEnds["s2"]; time.Until(until) <= 0 {
		t.Fatalf("a newer turn-end on the non-active session must flash its row")
	}
}

// TestApprovalNonShellSingleKey pins the non-bash path: a file-tool
// approval answers on the first allow key (no confirming second key).
func TestApprovalNonShellSingleKey(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a := app.New(newFakeHost(t).URL)
	a.Start(ctx)
	m := NewModel(a)
	m.W, m.H = 120, 40

	st := a.Store()
	st.SetSessions([]protocol.SessionSummary{{SessionId: "s1", Cwd: "/tmp"}})
	st.SetActive("s1")
	args := "{\"file_path\":\"/tmp/x\"}"
	st.ApprovalRequested("s1", "rpc-3", "app-2", "read", "call-2", "needs read access", args)

	m.Update(dirtyMsg{})
	if got := len(m.mods); got != 1 {
		t.Fatalf("after dirty pulse: modals = %d (want 1)", got)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	if got := len(m.mods); got != 0 {
		t.Fatalf("after ctrl+a: modals = %d (want 0, answered on the first key)", got)
	}
}

// drainNotices returns the notices queued so far. The tests drive the
// model without Init, so nothing consumes the store's notice channel.
func drainNotices(st *core.Store) []core.Notice {
	var out []core.Notice
	for {
		select {
		case n := <-st.Notices():
			out = append(out, n)
		default:
			return out
		}
	}
}

// TestQuestionAnsweredClearsPending pins the local settle of a parked
// batch: answering through the modal must clear the pending frame on the
// host's ack (the new dialect never sends the resolved frame, so without
// the settle the ctrl+i strip stays lit), and a resolved frame that still
// lands (the legacy dialect) is a no-op — one toast, not two.
func TestQuestionAnsweredClearsPending(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a := app.New(newFakeHost(t).URL)
	a.Start(ctx)
	m := NewModel(a)
	m.W, m.H = 120, 40

	st := a.Store()
	st.SetSessions([]protocol.SessionSummary{{SessionId: "s1", Cwd: "/tmp"}})
	st.SetActive("s1")
	st.QuestionRequested("s1", "rpc-1", []protocol.QuestionItem{{
		Id:       "q1",
		Question: "Pick one?",
		Options:  []protocol.QuestionOption{{Label: "A"}, {Label: "B"}},
	}})
	drainNotices(st)

	m.Update(dirtyMsg{})
	if m.topModal() == nil {
		t.Fatal("the parked batch did not auto-open")
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.mods) != 0 {
		t.Fatalf("after enter: modals = %d (want 0)", len(m.mods))
	}
	if cmd == nil {
		t.Fatal("enter did not return the answer command")
	}
	cmd()
	snap := st.Get("s1")
	if len(snap.Questions) != 0 {
		t.Fatalf("after the ack: pending questions = %d (want 0: the ctrl+i strip stays lit)", len(snap.Questions))
	}
	notices := drainNotices(st)
	if len(notices) != 1 {
		t.Fatalf("notices = %v (want exactly one settle toast)", notices)
	}
	// A resolved frame that still lands finds an empty slot: no second toast.
	st.QuestionResolved("rpc-1", "answered")
	if got := len(drainNotices(st)); got != 0 {
		t.Fatalf("late resolved frame toasted again: %d notices", got)
	}
}

// TestApprovalAnsweredClearsPending pins the approval twin: the allow
// chord settles the parked frame on the ack, and the decided side that
// follows (the session-log approval/decided) is a no-op.
func TestApprovalAnsweredClearsPending(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a := app.New(newFakeHost(t).URL)
	a.Start(ctx)
	m := NewModel(a)
	m.W, m.H = 120, 40

	st := a.Store()
	st.SetSessions([]protocol.SessionSummary{{SessionId: "s1", Cwd: "/tmp"}})
	st.SetActive("s1")
	st.ApprovalRequested("s1", "rpc-1", "app-1", "read", "call-1", "needs read access", `{"file_path":"/tmp/x"}`)
	drainNotices(st)

	m.Update(dirtyMsg{})
	if m.topModal() == nil {
		t.Fatal("the parked approval did not auto-open")
	}
	// "read" is not bash-family: the first allow key answers.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	if len(m.mods) != 0 {
		t.Fatalf("after ctrl+a: modals = %d (want 0)", len(m.mods))
	}
	if cmd == nil {
		t.Fatal("allow did not return the response command")
	}
	cmd()
	snap := st.Get("s1")
	if len(snap.Approvals) != 0 {
		t.Fatalf("after the ack: pending approvals = %d (want 0)", len(snap.Approvals))
	}
	notices := drainNotices(st)
	if len(notices) != 1 {
		t.Fatalf("notices = %v (want exactly one settle toast)", notices)
	}
	// The decided frame that follows finds an empty slot: no second toast.
	st.ApprovalResolved("app-1", "allowed-once")
	if got := len(drainNotices(st)); got != 0 {
		t.Fatalf("late decided frame toasted again: %d notices", got)
	}
}

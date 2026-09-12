// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package oneoff

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"dsh-cli/internal/app"
	"dsh-cli/internal/core"
	"dsh-cli/internal/i18n"
	"dsh-cli/internal/modes"
	"dsh-cli/internal/protocol"
	"dsh-cli/internal/textutil"
	"dsh-cli/internal/version"
)

// Status prints the host snapshot and session counts.
func Status(ctx context.Context, o Opts) error {
	if err := o.Fill(); err != nil {
		return err
	}
	a := app.NewWith(o.URL, o.Token)
	store := a.Start(ctx)
	h, err := a.Client().Describe(ctx)
	if err != nil {
		return err
	}
	// Fetch the roster synchronously: the background baseline may not have
	// landed session.list yet (would print "sessions 0" for a busy host).
	if err := a.RefreshRoster(ctx); err != nil {
		return err
	}
	rows := store.Roster()
	running := 0
	for _, r := range rows {
		if r.Running {
			running++
		}
	}
	loc := i18n.LoadDefault()
	fmt.Printf("%-10s %s\n", loc.T("cli.label.cli"), version.Version)
	fmt.Printf("%-10s %s (host)\n", loc.T("cli.label.host"), h.Version)
	fmt.Printf("%-10s %s\n", loc.T("cli.label.cwd"), h.Cwd)
	if h.Provider != "" {
		fmt.Printf("%-10s %s / %s\n", loc.T("cli.label.model"), h.Provider, h.Model)
	}
	fmt.Println(loc.T("cli.sessions", len(rows), running, h.AttachedSessions))
	if err := a.RefreshWorkspaces(ctx); err == nil {
		fmt.Println(loc.T("cli.workspaces", len(store.Workspaces())))
	}
	return nil
}

// Ls lists sessions.
func Ls(ctx context.Context, o Opts) error {
	if err := o.Fill(); err != nil {
		return err
	}
	a := app.NewWith(o.URL, o.Token)
	store := a.Start(ctx)
	// Wait for the host to answer, then fetch the roster synchronously: the
	// background baseline may have landed only a partial (or preset-less)
	// first page before the downlink sync settled.
	loc := i18n.LoadDefault()
	deadline := time.Now().Add(10 * time.Second)
	for store.Host() == nil {
		if time.Now().After(deadline) {
			return errors.New(loc.T("cli.no.host"))
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err := a.RefreshRoster(ctx); err != nil {
		return err
	}
	rows := store.Roster()
	if len(rows) == 0 {
		fmt.Println(loc.T("cli.no.sessions"))
		return nil
	}
	for _, r := range rows {
		state := loc.T("cli.state.idle")
		switch {
		case r.Running:
			state = loc.T("cli.state.running")
		case r.Blank:
			state = loc.T("cli.state.blank")
		}
		title := r.Title
		if title == "" {
			title = loc.T("common.untitled")
		}
		fmt.Printf("%-8s %-9s %-36s %s %s\n", state, modes.Short(r.Mode), shortTime(r.UpdatedAt), title, r.Cwd)
	}
	return nil
}

// Workspaces lists the registered workspaces (the registry baseline).
func Workspaces(ctx context.Context, o Opts) error {
	if err := o.Fill(); err != nil {
		return err
	}
	a := app.NewWith(o.URL, o.Token)
	store := a.Start(ctx)
	loc := i18n.LoadDefault()
	deadline := time.Now().Add(10 * time.Second)
	for store.Host() == nil {
		if time.Now().After(deadline) {
			return errors.New(loc.T("cli.no.host"))
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err := a.RefreshWorkspaces(ctx); err != nil {
		return err
	}
	wsList := store.Workspaces()
	if len(wsList) == 0 {
		fmt.Println(loc.T("cli.no.workspaces"))
		return nil
	}
	for _, w := range wsList {
		fmt.Printf("%-10s  %-16s  %s  %d session%s\n",
			shortWSID(w.WorkspaceId), w.Title, w.Path,
			len(w.SessionIds), pluralWS(len(w.SessionIds)))
	}
	if ids := store.ArchivedIDs(); len(ids) > 0 {
		fmt.Println(loc.T("cli.archived", len(ids), pluralWS(len(ids))))
	}
	return nil
}

func shortWSID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func pluralWS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// ActiveForModels resolves a session for `models` when none is named:
// the most recent non-blank roster row (the server's default focus).
func ActiveForModels(ctx context.Context, o Opts) string {
	if o.SessionID != "" {
		return o.SessionID
	}
	if err := o.Fill(); err != nil {
		return ""
	}
	a := app.NewWith(o.URL, o.Token)
	store := a.Start(ctx)
	deadline := time.Now().Add(8 * time.Second)
	for len(store.Roster()) == 0 {
		if time.Now().After(deadline) {
			return ""
		}
		time.Sleep(150 * time.Millisecond)
	}
	for _, r := range store.Roster() {
		if !r.Blank {
			return r.Id
		}
	}
	return ""
}

// NewSession creates a session and prints its id.
func NewSession(ctx context.Context, o Opts) (string, error) {
	if err := o.Fill(); err != nil {
		return "", err
	}
	a := app.NewWith(o.URL, o.Token)
	// Baseline the workspace registry before create: a cwd on a
	// registered workspace is sent as the workspace id (the web
	// New-Session path) so the session is born accounted — mirroring
	// the TUI's boot-create rule. Best effort: a registry miss only
	// means the create falls back to the raw cwd (a cwd-only attach).
	if o.CWD != "" {
		_ = a.RefreshWorkspaces(ctx)
	}
	return a.CreateSession(ctx, protocol.SessionCreateRequest{Cwd: o.CWD, AgentPreset: presetID(o)})
}

// History dumps one session's transcript in plain text.
func History(ctx context.Context, o Opts, sessionID string) error {
	if err := o.Fill(); err != nil {
		return err
	}
	a := app.NewWith(o.URL, o.Token)
	store := a.Start(ctx)

	// Pull all pages (tail, then older) and fold into one transcript.
	first, err := a.Client().History(ctx, sessionID, 0, 200)
	if err != nil {
		return err
	}
	all := append([]*protocol.SessionEvent(nil), pageEvents(first)...)
	cursor := oldestSeq(all)
	more := first.HasMore
	for pages := 0; more && pages < 1000; pages++ {
		next, err := a.Client().History(ctx, sessionID, cursor, 200)
		if err != nil {
			return err
		}
		evs := pageEvents(next)
		if len(evs) == 0 {
			break // defensive: HasMore with no rows would spin forever
		}
		nextCursor := oldestSeq(evs)
		if nextCursor >= cursor {
			break // cursor did not advance (host misbehavior)
		}
		all = append(evs, all...)
		cursor = nextCursor
		more = next.HasMore
	}
	tr := core.NewTranscript()
	for _, ev := range all {
		tr.Apply(ev)
		// Items hold the parsed content; drop the raw JSON as we go so a
		// long session does not keep two full copies in memory.
		ev.Data = nil
	}
	snap := &core.Snapshot{Items: tr.Items, Title: store.TitleFor(sessionID)}
	printPlain(os.Stdout, snap)
	return nil
}

func pageEvents(resp *protocol.HistoryResponse) []*protocol.SessionEvent {
	out := make([]*protocol.SessionEvent, 0, len(resp.Events))
	for _, e := range resp.Events {
		out = append(out, &e.Event)
	}
	return out
}

func oldestSeq(evs []*protocol.SessionEvent) int64 {
	var min int64 = -1
	for _, e := range evs {
		if min == -1 || e.Seq < min {
			min = e.Seq
		}
	}
	return min
}

// Models prints the model directory for a session.
func Models(ctx context.Context, o Opts, sessionID string) error {
	if err := o.Fill(); err != nil {
		return err
	}
	a := app.NewWith(o.URL, o.Token)
	ms, err := a.Client().Models(ctx, sessionID)
	if err != nil {
		return err
	}
	fmt.Printf("current: %s / %s%s\n", ms.Current.Provider, ms.Current.Model, effortNote(ms.Current.ReasoningEffort))
	if !ms.Routable {
		fmt.Println("! current provider is not routable (turns will fail)")
	}
	for _, g := range ms.Groups {
		fmt.Printf("\n%s (%s)\n", g.Name, g.Id)
		for _, mo := range g.Models {
			line := "  " + mo.Id
			if mo.Name != "" {
				line += "  " + mo.Name
			}
			fmt.Println(line)
		}
	}
	for _, f := range ms.Failures {
		fmt.Printf("  ! %s: %s\n", f.Name, f.Message)
	}
	return nil
}

func effortNote(e string) string {
	if e == "" {
		return ""
	}
	return " (" + e + ")"
}

// printPlain renders a transcript snapshot as plain text.
func printPlain(out *os.File, snap *core.Snapshot) {
	if snap.Title != "" {
		fmt.Fprintf(out, "= %s =\n\n", snap.Title)
	}
	for _, it := range snap.Items {
		switch it.Kind {
		case core.KindUser:
			fmt.Fprintf(out, "You: %s\n\n", inline(it.Text))
		case core.KindAssistant:
			for _, b := range it.Blocks {
				switch b.Kind {
				case "text":
					if b.Text != "" {
						fmt.Fprintf(out, "Agent: %s\n", inline(b.Text))
					}
				case "tool":
					if b.Tool != nil {
						state := "✓"
						if b.Tool.IsError {
							state = "✗"
						} else if !b.Tool.Done {
							state = "…"
						}
						fmt.Fprintf(out, "  ▸ %s %s %s\n", b.Tool.Name, inline(b.Tool.Args), state)
					}
				}
			}
			fmt.Fprintln(out)
		case core.KindTurnEnd:
			fmt.Fprintf(out, "— turn %s (%s) —\n", it.TurnEnd.Kind, textutil.HumanDuration(time.Duration(it.TurnMs)*time.Millisecond))
		case core.KindCommand:
			if it.CmdRun != nil {
				fmt.Fprintf(out, "%s /%s %s", "⌘", it.CmdRun.Name, it.CmdRun.Args)
				if it.CmdDone != nil {
					fmt.Fprintf(out, "  [%s] %s", it.CmdDone.Kind, it.CmdDone.Text)
				}
				fmt.Fprintln(out)
			}
		case core.KindNote:
			fmt.Fprintf(out, "· %s\n", it.Note)
		default:
			fmt.Fprintf(out, "· %s\n", it.Note)
		}
	}
}

// inline is the plain-text face of a transcript string: control runes
// are stripped so a hostile or buggy host cannot reach the terminal with
// raw ANSI/VT sequences through a history dump.
func inline(s string) string {
	return textutil.StripControl(s)
}

func shortTime(ms int64) string {
	if ms == 0 {
		return "-"
	}
	d := time.Since(time.UnixMilli(ms)).Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

// Package oneoff is the non-interactive surface: one-shot prompts, pipes,
// and the inspection commands (status / ls / new / history / models).
package oneoff

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"dsh-cli/internal/app"
	"dsh-cli/internal/config"
	"dsh-cli/internal/core"
	"dsh-cli/internal/modes"
	"dsh-cli/internal/protocol"
	"dsh-cli/internal/textutil"
	"dsh-cli/internal/usage"

	"github.com/mattn/go-isatty"
)

// Opts is the parsed CLI options shared by all one-shot commands.
type Opts struct {
	URL       string `json:"url"`
	SessionID string
	CWD       string
	Preset    string
	New       bool
	Verbose   bool
	Thinking  bool
	Timeout   time.Duration
	Token     string // launch token (cookie-gated build); "" = stored/config
	// NoAutostart disables the auto-start of a down loopback dsh web
	// (the default is to launch `dsh web` as a child and stop it on
	// exit).
	NoAutostart bool
}

// DefaultTimeout bounds a one-shot turn.
const DefaultTimeout = 10 * time.Minute

// presetID resolves the --preset flag: shipped-mode names (standard / ptc /
// minimal / creator, "X mode" forms included) map to their preset ids,
// anything else passes through for the host to resolve against its roster.
func presetID(o Opts) string {
	return modes.StaticID(o.Preset)
}

// Run executes one prompt and returns the assistant's final answer.
func Run(ctx context.Context, o Opts, prompt string) (string, error) {
	if err := o.Fill(); err != nil {
		return "", err
	}
	a := app.NewWith(o.URL, o.Token)
	// One-shot turns consume tokens too: count them into the same
	// persistent statistics the /status popup reads.
	if p, ok := usage.DefaultPath(); ok {
		a.AttachUsage(usage.New(p))
		defer a.Close()
	}
	store := a.Start(ctx)

	// Pick the target session.
	target, err := pickSession(ctx, a, o)
	if err != nil {
		return "", err
	}
	// Make the target the active session so reconnect re-baselining refetches
	// its history tail (and turn-end notices route to it).
	store.SetActive(target)

	// Watcher: incremental printing + stderr spinner.
	done := make(chan struct{})
	streamer := newStreamer(o)
	streamerStop, flushed := streamer.start(ctx, store, target, done)

	// The rpcId is only needed for the error path (the echo reconcile
	// already ran in PromptSent); the turn's end is watched via the store.
	if _, err := a.Prompt(ctx, target, prompt, "queue"); err != nil {
		streamerStop()
		return "", fmt.Errorf("send: %w", err)
	}

	// Wait for the turn to end (or the deadline).
	select {
	case <-ctx.Done():
		cctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = a.Cancel(cctx, target)
		cancel()
		streamerStop()
		return "", ctx.Err()
	case info := <-turnEndFor(ctx, store, target):
		streamerStop()
		if info.Kind == "completed" {
			// fall through to the final answer
		} else if info.Error != "" {
			return "", fmt.Errorf("turn %s: %s", info.Kind, info.Error)
		} else {
			return "", fmt.Errorf("turn ended: %s", info.Kind)
		}
	}
	close(done)
	// Let the final flush drain before reading the answer (the streamer
	// signals it; no blind sleep).
	select {
	case <-flushed:
	case <-time.After(500 * time.Millisecond):
	}

	snap := store.Get(target)
	if snap == nil {
		return "", fmt.Errorf("session vanished")
	}
	answer := finalAnswer(snap)
	// In streaming modes the answer text was already emitted incrementally.
	if !o.Verbose && !o.Thinking {
		fmt.Fprint(os.Stdout, answer)
	}
	if answer == "" {
		fmt.Fprintln(os.Stderr, "note: turn ended with no text answer")
	}
	return answer, nil
}

// turnEndFor forwards the target session's turn-end event and parks until
// it — or the caller's ctx — releases the watcher (one-shot runs die on
// their timeout; the store channel is process-global and never closes).
func turnEndFor(ctx context.Context, store *core.Store, id string) <-chan *core.TurnEndInfo {
	ch := make(chan *core.TurnEndInfo, 1)
	go func() {
		for {
			select {
			case info := <-store.EndOfTurn():
				if info.Sess == id {
					ch <- info
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch
}

// finalAnswer concatenates the last assistant message's text blocks.
func finalAnswer(snap *core.Snapshot) string {
	for i := len(snap.Items) - 1; i >= 0; i-- {
		it := snap.Items[i]
		if it.Kind != core.KindAssistant {
			continue
		}
		var b strings.Builder
		for _, bl := range it.Blocks {
			if bl.Kind == "text" && bl.Text != "" {
				if b.Len() > 0 {
					b.WriteString("\n\n")
				}
				b.WriteString(bl.Text)
			}
		}
		if b.Len() > 0 {
			return b.String()
		}
	}
	return ""
}

// pickSession resolves the one-shot target session.
func pickSession(ctx context.Context, a *app.App, o Opts) (string, error) {
	if o.SessionID != "" {
		return o.SessionID, nil
	}
	if o.New {
		id, err := a.CreateSession(ctx, protocol.SessionCreateRequest{Cwd: o.CWD, AgentPreset: presetID(o)})
		if err != nil {
			return "", err
		}
		return id, nil
	}
	rows := a.Store().Roster()
	for _, r := range rows {
		if !r.Blank {
			a.Store().SetActive(r.Id)
			return r.Id, nil
		}
	}
	return a.CreateSession(ctx, protocol.SessionCreateRequest{Cwd: o.CWD, AgentPreset: presetID(o)})
}

// Fill applies defaults (the flag defaults already cover URL/timeout).
// It also validates the server URL once, so a scheme-less or typo'd
// DSH_URL/--url fails fast with a clear message instead of dying in a
// silent websocket reconnect loop.
func (o *Opts) Fill() error {
	if o.URL == "" {
		o.URL = config.ResolveURL("")
	}
	if o.Timeout <= 0 {
		o.Timeout = DefaultTimeout
	}
	u, err := url.Parse(o.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("bad server URL %q (from --url, $DSH_URL, or ~/.dsh-cli/config.json): want http:// or https:// host:port", o.URL)
	}
	// Plain http:// off-loopback sends prompts (and tool args) in the clear.
	if config.PlainHTTP(o.URL) {
		fmt.Fprintf(os.Stderr, "note: %s is plain http (unencrypted; no auth unless DSH_TOKEN is set)\n", u.Host)
	}
	return nil
}

// streamer prints incremental agent activity (tools, thinking, text) for
// -v / --thinking, plus a stderr spinner when stderr is a TTY.
type streamer struct {
	o            Opts
	printedText  int
	printedItems int
	stderrTTY    bool
}

func newStreamer(o Opts) *streamer {
	return &streamer{o: o, stderrTTY: isatty.IsTerminal(os.Stderr.Fd())}
}

// start runs the watch loop; it returns a stop function.
// It also returns the flushed channel: closed once the post-done final
// flush has run (or the context died), so the caller does not sleep blind.
func (s *streamer) start(ctx context.Context, store *core.Store, target string, done chan struct{}) (func(), <-chan struct{}) {
	t := time.NewTicker(150 * time.Millisecond)
	flushed := make(chan struct{})
	go func() {
		defer t.Stop()
		defer close(flushed)
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				s.flush(store, target)
				s.stopSpinner()
				return
			case <-t.C:
				s.flush(store, target)
				s.spinner(store, target)
			}
		}
	}()
	stop := func() {
		t.Stop()
		s.stopSpinner()
	}
	return stop, flushed
}

func (s *streamer) flush(store *core.Store, target string) {
	snap := store.Get(target)
	if snap == nil {
		return
	}
	if s.o.Verbose {
		// Print newly finalized tool cards.
		items := snap.Items
		for i := s.printedItems; i < len(items); i++ {
			it := items[i]
			if it.Kind != core.KindAssistant {
				if it.Kind == core.KindTurnEnd {
					s.printedItems = i + 1
				}
				continue
			}
			for _, b := range it.Blocks {
				if b.Kind == "tool" && b.Tool != nil {
					fmt.Fprintf(os.Stderr, "  ▸ %s %s\n", b.Tool.Name, truncateLine(b.Tool.Args, 100))
				} else if b.Kind == "reasoning" && s.o.Thinking {
					fmt.Fprintf(os.Stderr, "  ◌ %s\n", truncateLine(b.Text, 300))
				}
			}
			s.printedItems = i + 1
		}
	}
	// Incremental answer text (streaming modes only).
	if s.o.Verbose || s.o.Thinking {
		last := lastAssistantText(snap)
		if len(last) > s.printedText {
			fmt.Fprint(os.Stdout, last[s.printedText:])
			os.Stdout.Sync()
			s.printedText = len(last)
		}
	}
}

func lastAssistantText(snap *core.Snapshot) string {
	for i := len(snap.Items) - 1; i >= 0; i-- {
		it := snap.Items[i]
		if it.Kind != core.KindAssistant {
			continue
		}
		var b strings.Builder
		for _, bl := range it.Blocks {
			if bl.Kind == "text" {
				b.WriteString(bl.Text)
			}
		}
		return b.String()
	}
	return ""
}

func (s *streamer) spinner(store *core.Store, target string) {
	if !s.stderrTTY || (s.o.Verbose || s.o.Thinking) {
		return
	}
	snap := store.Get(target)
	// Liveness = the host's running flag (not the folded log's TurnActive,
	// which stays true for a turn that ended in history without a turn/end).
	if snap == nil || !snap.Running {
		return
	}
	elapsed := "0s"
	if !snap.TurnStart.IsZero() {
		elapsed = textutil.HumanDuration(time.Since(snap.TurnStart))
	}
	fmt.Fprintf(os.Stderr, "\r  ◐ thinking… %s ", elapsed)
}

func (s *streamer) stopSpinner() {
	if s.stderrTTY {
		fmt.Fprint(os.Stderr, "\r  \033[K\n")
	}
}

// truncateLine flattens newlines and rune-clips to n bytes with an ellipsis.
func truncateLine(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	return textutil.Truncate(s, n, "…")
}

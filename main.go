// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

// dsh-cli is a terminal client for the DeepSeek Harness web server.
//
// Usage:
//
//	dsh-cli                          interactive TUI (default)
//	dsh-cli "summarize this repo"    one-shot: run a prompt, print the answer
//	echo prompt | dsh-cli            one-shot from stdin
//	dsh-cli status|ls|new|history <sid>|models <sid>|workspaces
//
// Flags may appear before or after the prompt text.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"dsh-cli/internal/app"
	"dsh-cli/internal/client"
	"dsh-cli/internal/config"
	"dsh-cli/internal/core"
	"dsh-cli/internal/i18n"
	"dsh-cli/internal/oneoff"
	"dsh-cli/internal/ui"
	"dsh-cli/internal/usage"
	"dsh-cli/internal/version"
	"dsh-cli/internal/webhost"

	"github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"
)

func main() {
	o := oneoff.Opts{}
	showVersion := false
	flag.CommandLine.BoolVar(&showVersion, "version", false, "print client version and exit")
	registerFlags(&o)
	if showVersion {
		fmt.Printf("dsh-cli %s\n", version.Version)
		return
	}
	// Locale storage: seed ~/.dsh-cli/locales (generate en.json /
	// zh.json from the built-in tables when absent) before any command
	// picks its face.
	i18n.Boot()
	pos := flag.Args()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd, rest := splitCommand(pos)
	// Dsh web lifecycle: a down loopback host is launched (token captured
	// and stored); a live host that rejects the held credentials is killed
	// and relaunched. The server persists after dsh-cli exits.
	//
	// The TUI boots alongside the launch: a slow (or token-less, old)
	// dsh web must not hold the first frame — the splash covers the gap,
	// and the outcome lands on resCh for runTUI to apply. One-shot
	// commands wait for it synchronously: their first RPC needs the
	// credentials already.
	var wh *webhost.Host
	var tok string
	var resCh chan webhost.Result
	if cmd == "" && len(rest) == 0 {
		resCh = make(chan webhost.Result, 1)
		go func() {
			h, tk, err := webhost.Connect(o.URL, o.Token, !o.NoAutostart)
			resCh <- webhost.Result{Host: h, Token: tk, Err: err}
		}()
	} else {
		var err error
		wh, tok, err = webhost.Connect(o.URL, o.Token, !o.NoAutostart)
		if err != nil {
			fatalf("webhost: %v", err)
		}
		if wh != nil {
			o.Token = tok // the launched/restarted host owns this token
			key := "webhost.launched"
			if wh.Restarted() {
				key = "webhost.restarted"
			}
			fmt.Fprintf(os.Stderr, "note: %s\n", i18n.LoadDefault().T(key, wh.Pid(), wh.Base()))
		}
	}
	// Signal routing: interrupts (Ctrl-C, kill, pty close) cancel the app
	// context so every flow finishes its normal exit path (the auto-started
	// web persists — the child is re-parented). A second signal force-exits.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		<-sigCh
		cancel()
		<-sigCh
		os.Exit(130)
	}()
	switch cmd {
	case "status":
		must(oneoff.Status(ctx, o))
	case "ls":
		must(oneoff.Ls(ctx, o))
	case "new":
		id, err := oneoff.NewSession(ctx, o)
		must(err)
		fmt.Println(id)
	case "history":
		if len(rest) == 0 {
			fatalf("usage: dsh-cli history <sessionId>")
		}
		must(oneoff.History(ctx, o, rest[0]))
	case "models":
		if len(rest) == 0 {
			sid := oneoff.ActiveForModels(ctx, o)
			if sid == "" {
				fatalf("no session: pass models <sessionId> or --session")
			}
			rest = []string{sid}
		}
		must(oneoff.Models(ctx, o, rest[0]))
	case "workspaces":
		must(oneoff.Workspaces(ctx, o))
	case "run":
		runOneShot(ctx, o, strings.Join(rest, " "))
	case "":
		if len(rest) == 0 {
			runTUI(ctx, o, resCh)
		} else {
			runOneShot(ctx, o, strings.Join(rest, " "))
		}
	default:
		fatalf("unknown command: %s", cmd)
	}
}

// splitCommand treats the first positional as a command name only for the
// known command words; everything else is prompt text.
func splitCommand(pos []string) (cmd string, rest []string) {
	if len(pos) == 0 {
		return "", nil
	}
	switch pos[0] {
	case "run", "status", "ls", "new", "history", "models", "workspaces":
		return pos[0], pos[1:]
	}
	return "", pos
}

func registerFlags(o *oneoff.Opts) {
	fs := flag.CommandLine
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: dsh-cli [flags] [prompt | command]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "commands:")
		fmt.Fprintln(os.Stderr, "  run \"prompt\"        one-shot run (default for bare prompts)")
		fmt.Fprintln(os.Stderr, "  status                host and session overview")
		fmt.Fprintln(os.Stderr, "  ls                      list sessions")
		fmt.Fprintln(os.Stderr, "  new                     create a session (prints its id)")
		fmt.Fprintln(os.Stderr, "  history <sessionId>     dump a transcript in plain text")
		fmt.Fprintln(os.Stderr, "  models [sessionId]      print the model directory")
		fmt.Fprintln(os.Stderr, "  workspaces              list the workspace registry")
		fs.PrintDefaults()
	}
	fs.StringVar(&o.URL, "url", envOr("DSH_URL", config.ResolveURL("")), "server base URL (precedence: flag > $DSH_URL > ~/.dsh-cli/config.json > "+config.DefaultURL+")")
	fs.StringVar(&o.Token, "token", envOr("DSH_LAUNCH_TOKEN", ""), "launch token the cookie-gated dsh web printed at startup (precedence: flag > $DSH_LAUNCH_TOKEN > stored; the exchanged cookie is stored for later boots)")
	fs.StringVar(&o.SessionID, "session", "", "target session id")
	fs.StringVar(&o.CWD, "cwd", "", "workspace cwd for new sessions (default: host cwd)")
	fs.StringVar(&o.Preset, "preset", "", "agent preset for new sessions")
	fs.BoolVar(&o.New, "new", false, "always create a new session")
	fs.BoolVar(&o.NoAutostart, "no-autostart", false, "do not auto-start a down loopback dsh web (default: launch it as a child and stop it with dsh-cli)")
	fs.BoolVar(&o.Verbose, "v", false, "one-shot: print tool calls and stream text live")
	fs.BoolVar(&o.Thinking, "thinking", false, "one-shot: also print reasoning blocks")
	fs.DurationVar(&o.Timeout, "timeout", oneoff.DefaultTimeout, "one-shot wait timeout")
	fs.Parse(reorderFlags(os.Args[1:]))
}

// reorderFlags moves all flag arguments (with their values) ahead of any
// positional prompt text, so flags may appear before or after the prompt.
func reorderFlags(args []string) []string {
	isValueFlag := func(name string) bool {
		switch name {
		case "url", "token", "session", "cwd", "preset", "timeout":
			return true
		}
		return false
	}
	var flags, rest []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			// End of flags: everything after is positional (prompt text),
			rest = append(rest, args[i:]...)
			break
		}
		if len(arg) > 1 && arg[0] == '-' {
			flags = append(flags, arg)
			name := strings.TrimLeft(arg, "-")
			if i := strings.IndexByte(name, '='); i >= 0 {
				name = name[:i]
			}
			if isValueFlag(name) && !strings.Contains(arg, "=") && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		rest = append(rest, arg)
	}
	return append(flags, rest...)
}

// exit is os.Exit for the post-boot paths. The auto-started dsh web
// PERSISTS across dsh-cli exits (its stdio rides a log file, not
// dsh-cli's pipes, so re-parenting is safe); defers are skipped by
// os.Exit, so the exit paths are explicit.
func exit(code int) {
	os.Exit(code)
}

func runTUI(ctx context.Context, o oneoff.Opts, resCh <-chan webhost.Result) {
	if err := o.Fill(); err != nil {
		fatalf("%v", err)
	}
	a := app.NewWith(o.URL, o.Token)
	a.Start(ctx)
	if resCh != nil {
		// The launch runs alongside the boot: apply its outcome when it
		// lands — install the fresh token (the baseline probe picks it up
		// on its next pass) and toast the launch line, or the boot error.
		// Boot-grade hint life: the default 4s toast would expire while
		// the splash still owns the screen.
		go func() {
			r := <-resCh
			if r.Err != nil {
				a.Store().Notify(core.Notice{Level: "err", Text: "webhost: " + r.Err.Error(), Life: 10 * time.Second})
				return
			}
			if r.Host == nil {
				return // already live, or autostart off: nothing to install
			}
			if r.Token != "" {
				a.Client().Conn().SetToken(r.Token)
			}
			key := "webhost.launched"
			if r.Host.Restarted() {
				key = "webhost.restarted"
			}
			a.Store().Notify(core.Notice{Level: "info", Text: i18n.LoadDefault().T(key, r.Host.Pid(), r.Host.Base()), Life: 10 * time.Second})
		}()
	}
	// Persistent token-usage statistics (~/.dsh-cli/usage.json): the
	// /status popup's data; best-effort, off when home is unresolvable.
	if p, ok := usage.DefaultPath(); ok {
		a.AttachUsage(usage.New(p))
		defer a.Close()
	}
	m := ui.NewModel(a)
	prog := tea.NewProgram(m,
		tea.WithAltScreen(),
		// Cell-motion capture: the wheel drives the transcript scroll and
		// the left-button drag drives text selection (ui.handleMouse).
		tea.WithMouseCellMotion(),
		tea.WithContext(ctx),
	)
	m.SetProg(prog)
	// A panic in the render/event path must not leave the terminal in
	// alt-screen with the cursor hidden: restore, dump the stack, exit 1.
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "dsh-cli panicked: %v\n", r)
			debug.PrintStack()
			// leave alt-screen, show cursor, drop modifyOtherKeys2
			fmt.Fprint(os.Stdout, "\x1b[?1049l\x1b[?25h\x1b[>4m")
			exit(1)
		}
	}()
	if _, err := prog.Run(); err != nil {
		fatalf("tui: %v", err)
	}
	// The next app in this terminal (the shell) should not have to
	// decode modifyOtherKeys2: drop the mode dsh-cli turned on in Init.
	fmt.Fprint(os.Stdout, "\x1b[>4m")
}

func runOneShot(ctx context.Context, o oneoff.Opts, prompt string) {
	if err := o.Fill(); err != nil {
		fatalf("%v", err)
	}
	if prompt == "" {
		// A TTY stdin would block until Ctrl-D; a bare interactive call
		// is almost always a forgotten prompt, so fail fast instead.
		if isatty.IsTerminal(os.Stdin.Fd()) {
			fatalf("no prompt: pass one as an argument, or pipe it (echo hi | dsh-cli)")
		}
		// Cap the piped prompt (a pasted file is not a prompt; the host
		// prompt limit is orders of magnitude below this). A read error
		// (e.g. a broken pipe mid-read) leaves the prompt as-is: the
		// no-prompt check below still fails fast with usage.
		if b, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20)); err == nil {
			prompt = strings.TrimSpace(string(b))
		}
	}
	if prompt == "" {
		fatalf("usage: dsh-cli [flags] \"prompt\"")
	}
	cctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()
	if _, err := oneoff.Run(cctx, o, prompt); err != nil {
		if cctx.Err() == context.DeadlineExceeded {
			fmt.Fprintf(os.Stderr, "timed out after %s\n", o.Timeout)
			exit(3)
		}
		fmt.Fprintln(os.Stderr, err)
		authHint(err)
		hostDown(err)
		exit(1)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// authHint prints the recovery line under an auth-required failure:
// the cookie-gated host refused the held (or missing) token, and the
// two causes point at different fixes.
func authHint(err error) {
	if !errors.Is(err, client.ErrAuthRequired) {
		return
	}
	key := "auth.needed"
	if errors.Is(err, client.ErrTokenRejected) {
		key = "auth.stale"
	}
	fmt.Fprintln(os.Stderr, i18n.LoadDefault().T(key))
}

// hostDown prints the "start the server" hint under a transport-level
// failure (connection refused and kin): the DSH server is not listening
// at the configured address, so naming the start command is the fix.
// Server-up failures (HTTP answers, timeouts) print no hint.
func hostDown(err error) {
	if client.IsDown(err) {
		loc := i18n.LoadDefault()
		fmt.Fprintln(os.Stderr, loc.T("host.down"))
	}
}

func must(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, err)
	authHint(err)
	hostDown(err)
	exit(1)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	exit(1)
}

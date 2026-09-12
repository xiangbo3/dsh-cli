// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

//go:build !unix

package webhost

import (
	"os"
	"os/exec"
	"syscall"
)

// Non-unix hosts (Windows): no process-group idiom and no syscall.Kill;
// os.Process.Kill force-terminates (every signal is a terminate, so the
// grace is moot).

const (
	sigTerm = syscall.SIGTERM
	sigKill = syscall.SIGKILL
)

func setGroup(cmd *exec.Cmd) {}

// killProc terminates one process; sig stays for symmetry with the unix
// file and is never consulted here.
func killProc(pid int) {
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Kill()
	}
}

func killGroup(pid int, _ syscall.Signal) { killProc(pid) }

func syskill(pid int, _ syscall.Signal) { killProc(pid) }

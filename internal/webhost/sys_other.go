// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

//go:build !unix

package webhost

import (
	"os/exec"
	"syscall"
)

// Non-unix hosts (Windows): no process-group idiom; a plain process kill
// (on Windows every signal is a terminate, so the grace is moot).

const (
	sigTerm = syscall.SIGTERM
	sigKill = syscall.SIGKILL
)

func setGroup(cmd *exec.Cmd) {}

func killGroup(pid int, sig syscall.Signal) {
	_ = syscall.Kill(pid, sig)
}

func syskill(pid int, sig syscall.Signal) {
	_ = syscall.Kill(pid, sig)
}

// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

//go:build unix

package webhost

import (
	"os/exec"
	"syscall"
)

// The child runs in its own process group (pgid = its pid), so a kill of
// the group takes the whole dsh web tree, not just the direct child.

const (
	sigTerm = syscall.SIGTERM
	sigKill = syscall.SIGKILL
)

func setGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killGroup(pid int, sig syscall.Signal) {
	_ = syscall.Kill(-pid, sig)
}

// syskill signals one process (an lsof-reported listener, not necessarily
// a group leader).
func syskill(pid int, sig syscall.Signal) {
	_ = syscall.Kill(pid, sig)
}

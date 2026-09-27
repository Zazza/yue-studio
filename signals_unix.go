//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

const (
	sigStop = syscall.SIGSTOP
	sigCont = syscall.SIGCONT
)

func uid() int { return os.Getuid() }

// pauseProcess/resumeProcess — пауза внешнего плеера сигналами.
func pauseProcess(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Signal(sigStop)
	}
}

func resumeProcess(cmd *exec.Cmd) bool {
	if cmd == nil || cmd.Process == nil {
		return false
	}
	return cmd.Process.Signal(sigCont) == nil
}

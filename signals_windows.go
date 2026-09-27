//go:build windows

package main

import "os/exec"

// На Windows нет SIGSTOP/SIGCONT и pw-play: пауза = остановка процесса,
// resume стартует трек заново (см. player_windows.go).
func pauseProcess(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

func resumeProcess(cmd *exec.Cmd) bool { return false }

func uid() int { return 0 }

//go:build !windows

package main

import (
	"fmt"
	"os/exec"
)

// playerCommand — воспроизведение через системный pw-play (PipeWire).
func playerCommand(file string, volume float64) *exec.Cmd {
	cmd := exec.Command("pw-play", "--volume", fmt.Sprintf("%.2f", volume), file)
	cmd.Env = pwEnv()
	return cmd
}

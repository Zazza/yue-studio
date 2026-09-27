//go:build !windows

package main

import "os/exec"

// playerCommand — воспроизведение через системный pw-play (PipeWire).
func playerCommand(file string) (*exec.Cmd, string) {
	cmd := exec.Command("pw-play", file)
	cmd.Env = pwEnv()
	xdg := "unset"
	for _, kv := range cmd.Env {
		if len(kv) >= 16 && kv[:16] == "XDG_RUNTIME_DIR=" {
			xdg = kv[16:]
		}
	}
	return cmd, "pw-play, XDG_RUNTIME_DIR=" + xdg
}

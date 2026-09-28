//go:build !windows

package dsp

import "os/exec"

// ffmpegCmd — на unix окна консоли не мелькают.
func ffmpegCmd(args []string) *exec.Cmd {
	return exec.Command("ffmpeg", args...)
}

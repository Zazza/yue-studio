//go:build windows

package dsp

import (
	"os/exec"
	"syscall"
)

// ffmpegCmd — ffmpeg без окна консоли: у GUI-процесса оно иначе мелькает
// на каждом применении эффектов/перемотке.
func ffmpegCmd(args []string) *exec.Cmd {
	cmd := exec.Command("ffmpeg", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return cmd
}

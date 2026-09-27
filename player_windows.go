//go:build windows

package main

import (
	"fmt"
	"net/url"
	"os/exec"
	"strings"
)

// playerCommand — воспроизведение через WPF MediaPlayer (PowerShell, STA).
// Media Foundation играет flac/mp3/wav; kill процесса (killLocked) — это стоп.
// Пауза-возобновление с позиции не поддерживается: resume стартует заново.
func playerCommand(file string) (*exec.Cmd, string) {
	uri := (&url.URL{Scheme: "file", Path: "/" + strings.ReplaceAll(file, "\\", "/")}).String()
	ps := fmt.Sprintf(
		`Add-Type -AssemblyName PresentationCore; `+
			`$m = New-Object System.Windows.Media.MediaPlayer; `+
			`$m.Open([Uri]%q); $m.Play(); `+
			`while ($true) { Start-Sleep -Seconds 5 }`, uri)
	// powershell.exe (Windows PowerShell 5.1) — MediaPlayer требует STA-поток
	cmd := exec.Command("powershell.exe", "-NoProfile", "-STA", "-WindowStyle", "Hidden", "-Command", ps)
	return cmd, "powershell MediaPlayer"
}

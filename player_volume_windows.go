//go:build windows

package main

// setVolumeLive — на Windows WPF-обёртка не даёт менять громкость на лету:
// значение применится при следующем запуске дорожки (playerCommand volume).
func setVolumeLive(file string, v float64) {}

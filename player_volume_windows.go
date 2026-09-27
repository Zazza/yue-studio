//go:build windows

package main

// setVolumeLive — живая громкость на Windows: ползунок пишет <file>.vol,
// цикл плейера (player_windows.go) перечитывает его каждые 400 мс.
func setVolumeLive(file string, v float64) {
	writeVolumeFile(file, v)
}

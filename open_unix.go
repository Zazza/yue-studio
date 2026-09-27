//go:build !windows

package main

import "os/exec"

// openExternal открывает файл в ассоциированном приложении ОС.
func openExternal(path string) error {
	return exec.Command("xdg-open", path).Start()
}

//go:build windows

package main

// openExternal открывает файл в ассоциированном приложении ОС.
func openExternal(path string) error {
	return hiddenCmd("cmd", "/c", "start", "", path).Start()
}

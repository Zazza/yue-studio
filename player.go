package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// player — встроенное воспроизведение через системный pw-play (PipeWire),
// т.к. у webview во Wails нет аудио-выхода. Пауза — SIGSTOP/SIGCONT.
type player struct {
	mu        sync.Mutex
	cmd       *exec.Cmd
	tmpFile   string
	startedAt time.Time
	pausedAt  time.Duration
	playing   bool
	loadedID  int64
	duration  time.Duration
	lastErr   string
}

var pl = &player{}

// pwEnv гарантирует XDG_RUNTIME_DIR: без него pw-play, запущенный из
// desktop-сессии (где переменной может не быть), молча не видит сокет PipeWire.
func pwEnv() []string {
	env := os.Environ()
	for _, kv := range env {
		if len(kv) >= 16 && kv[:16] == "XDG_RUNTIME_DIR=" {
			return env
		}
	}
	return append(env, fmt.Sprintf("XDG_RUNTIME_DIR=/run/user/%d", uid()))
}

// playerLog — журнал запусков pw-play для диагностики звука (~/yue-player.log).
func playerLog(format string, args ...any) {
	f, err := os.OpenFile(filepath.Join(os.Getenv("HOME"), "yue-player.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, time.Now().Format("15:04:05")+" "+format+"\n", args...)
}

func (p *player) load(id int64, wavBytes []byte, dur time.Duration) error {
	// расширение по содержимому: pw-play (libsndfile) определяет формат по
	// заголовку, но имя файла оставляем честным для прозрачности
	ext := ".wav"
	if len(wavBytes) >= 4 && string(wavBytes[:4]) == "fLaC" {
		ext = ".flac"
	}
	f, err := os.CreateTemp("", fmt.Sprintf("yue-%d-*%s", id, ext))
	if err != nil {
		return err
	}
	if _, err := f.Write(wavBytes); err != nil {
		f.Close()
		return err
	}
	f.Close()
	p.mu.Lock()
	p.stopLocked()
	p.tmpFile = f.Name()
	p.loadedID = id
	p.duration = dur
	p.pausedAt = 0
	p.mu.Unlock()
	return nil
}

// killLocked останавливает воспроизведение, но НЕ удаляет загруженный файл —
// он нужен для повторного запуска (play после стопа). Процесс «жнёт» горутина
// spawnLocked (её cmd.Wait), здесь только сигнал — иначе двойной wait.
func (p *player) killLocked() {
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	p.cmd = nil
	p.playing = false
	p.pausedAt = 0
}

func (p *player) stopLocked() {
	p.killLocked()
	if p.tmpFile != "" {
		os.Remove(p.tmpFile)
		p.tmpFile = ""
		p.loadedID = 0
		p.duration = 0
	}
}

func (p *player) spawnLocked() error {
	cmd, xdg := playerCommand(p.tmpFile)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	playerLog("spawn player %s (%s)", p.tmpFile, xdg)
	if err := cmd.Start(); err != nil {
		p.lastErr = fmt.Sprintf("player: %v", err)
		playerLog("start failed: %v", err)
		return err
	}
	p.cmd = cmd
	p.startedAt = time.Now().Add(-p.pausedAt)
	p.playing = true
	go func() {
		err := cmd.Wait()
		p.mu.Lock()
		if p.cmd == cmd {
			p.playing = false
			p.pausedAt = 0
			if err != nil {
				p.lastErr = strings.TrimSpace(stderr.String())
				if p.lastErr == "" {
					p.lastErr = err.Error()
				}
			}
		}
		p.mu.Unlock()
		playerLog("exit err=%v stderr=%q", err, strings.TrimSpace(stderr.String()))
	}()
	return nil
}

func (p *player) play() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.tmpFile == "" {
		return fmt.Errorf("nothing loaded")
	}
	p.killLocked()
	p.lastErr = ""
	return p.spawnLocked()
}

func (p *player) toggle() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd == nil || p.cmd.Process == nil {
		if p.tmpFile != "" && p.pausedAt > 0 {
			p.resumeLocked()
		} else if p.tmpFile != "" {
			p.playLocked()
		}
		return
	}
	if p.playing {
		pauseProcess(p.cmd)
		p.pausedAt = time.Since(p.startedAt)
		p.playing = false
	} else {
		p.resumeLocked()
	}
}

func (p *player) playLocked() {
	p.killLocked()
	_ = p.spawnLocked()
}

func (p *player) resumeLocked() {
	if !resumeProcess(p.cmd) {
		// Windows: процесса-плейера больше нет — играем заново с начала
		p.spawnLocked()
		return
	}
	p.startedAt = time.Now().Add(-p.pausedAt)
	p.playing = true
}

func (p *player) stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopLocked()
}

func (p *player) state() (playing bool, pos, dur time.Duration, loadedID int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pos = p.pausedAt
	if p.playing {
		pos = time.Since(p.startedAt)
	}
	return p.playing, pos, p.duration, p.loadedID
}

func (p *player) lastError() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastErr
}

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"

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
	offset    time.Duration // позиция начала файла после перемотки
	volume    float64       // 0..1, применяется при запуске плеера
}

// Player — интерфейс встроенного плеера (реализация — player ниже).
type Player interface {
	Load(id int64, data []byte, dur time.Duration) error
	Play() error
	Toggle()
	Stop()
	Seek(target time.Duration) error
	State() (playing bool, pos, dur time.Duration, loadedID int64)
	SetVolume(v float64)
	LastError() string
}

// NewPlayer — плеер по умолчанию (громкость 0.8).
func NewPlayer() Player {
	return &player{volume: 0.8}
}

var _ Player = (*player)(nil)

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

func (p *player) Load(id int64, wavBytes []byte, dur time.Duration) error {
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
	p.offset = 0
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
	cmd := playerCommand(p.tmpFile, p.volume)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		p.lastErr = fmt.Sprintf("player: %v", err)
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
	}()
	return nil
}

func (p *player) Play() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.tmpFile == "" {
		return fmt.Errorf("nothing loaded")
	}
	p.killLocked()
	p.lastErr = ""
	return p.spawnLocked()
}

func (p *player) Toggle() {
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

func (p *player) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopLocked()
}

func (p *player) State() (playing bool, pos, dur time.Duration, loadedID int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pos = p.pausedAt
	if p.playing {
		pos = time.Since(p.startedAt)
	}
	pos += p.offset
	return p.playing, pos, p.duration, p.loadedID
}

// seek — перемотка: ffmpeg -ss нарезает хвост локального файла в новый temp,
// плеер перезапускается с него (у pw-play/WPF-обёртки нет нативного seek).
func (p *player) Seek(target time.Duration) error {
	p.mu.Lock()
	src := p.tmpFile
	if src == "" {
		p.mu.Unlock()
		return fmt.Errorf("nothing loaded")
	}
	if target < 0 {
		target = 0
	}
	if p.duration > 0 && target > p.duration {
		target = p.duration
	}
	// из текущего смещения можно прыгнуть только вперёд — назад перекодируем от 0
	seekFrom := p.offset
	if target < seekFrom {
		seekFrom = 0
	}
	skip := target - seekFrom
	remaining := p.duration - target
	p.killLocked() // файл не трогаем: он источник для ffmpeg
	p.playing = false
	p.mu.Unlock()

	if remaining <= 0 {
		p.mu.Lock()
		p.offset = p.duration
		p.pausedAt = 0
		p.mu.Unlock()
		return nil
	}
	out, err := os.CreateTemp("", fmt.Sprintf("yue-seek-%d-*.flac", p.loadedID))
	if err != nil {
		return err
	}
	outName := out.Name()
	out.Close()
	cmd := exec.Command("ffmpeg", "-y", "-hide_banner", "-loglevel", "error",
		"-ss", fmt.Sprintf("%.3f", skip.Seconds()), "-i", src,
		"-t", fmt.Sprintf("%.3f", remaining.Seconds()),
		"-c", "copy", outName)
	if err := cmd.Run(); err != nil {
		os.Remove(outName)
		return fmt.Errorf("seek: ffmpeg: %v", err)
	}

	p.mu.Lock()
	if src != p.tmpFile && src != "" { // источник сменился, пока резали
		os.Remove(outName)
		p.mu.Unlock()
		return fmt.Errorf("track changed during seek")
	}
	os.Remove(src)
	p.tmpFile = outName
	p.offset = target
	p.duration = remaining
	p.pausedAt = 0
	p.lastErr = ""
	err = p.spawnLocked()
	p.mu.Unlock()
	return err
}

// setVolume — 0..1; применяется при следующем запуске дорожки (pw-play/WPF).
func (p *player) SetVolume(v float64) {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	p.mu.Lock()
	p.volume = v
	p.mu.Unlock()
}

func (p *player) LastError() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastErr
}

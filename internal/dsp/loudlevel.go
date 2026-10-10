package dsp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"slices"
)

// Уровень громких мест файла — как rms_p95_db замера воркера (worker/dsp.py analyze_file: librosa.load
// mono 22 050 Гц, librosa.feature.rms кадрами 2048 с шагом 512, центрированно, 95-й перцентиль в дБ). Им пресет
// звука доводит ОБРАБОТАННУЮ дорожку до цели level_db (этап 10, усл. 83): цель по исходной дорожке промахивалась —
// перегруз поднял гитару на +3,6 дБ над целью (#711).
const (
	loudRate  = 22050
	loudFrame = 2048
	loudHop   = 512
	loudFloor = 1e-9 // как у воркера: 20·lg(rms + 1e-9) — тишина без −∞
)

// LoudLevelDb — уровень громких мест файла, дБFS (95-й перцентиль RMS кадров моно-сигнала; моно — среднее
// каналов, как у librosa).
func LoudLevelDb(path string) (float64, error) {
	x, err := decodeMonoMean(path, loudRate)
	if err != nil {
		return 0, err
	}
	return loudLevel(x), nil
}

// decodeMonoMean — файл в моно средним каналов (ffmpeg -ac 1 на стерео даёт сумму с другим весом): декод в
// WAV float с родными каналами, число каналов — из заголовка.
func decodeMonoMean(path string, rate int) ([]float64, error) {
	cmd := ffmpegCmd([]string{"-hide_banner", "-loglevel", "error", "-i", path, "-vn",
		"-ar", fmt.Sprint(rate), "-c:a", "pcm_f32le", "-f", "wav", "pipe:1"})
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg decode: %w: %s", err, stderr.String())
	}
	b := out.Bytes()
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, errors.New("ffmpeg decode: не WAV")
	}
	ch := 0
	for p := 12; p+8 <= len(b); {
		id, size := string(b[p:p+4]), int(binary.LittleEndian.Uint32(b[p+4:p+8]))
		body := b[p+8:]
		switch id {
		case "fmt ":
			if len(body) < 4 {
				return nil, errors.New("ffmpeg decode: короткий fmt")
			}
			ch = int(binary.LittleEndian.Uint16(body[2:4]))
		case "data":
			if ch <= 0 {
				return nil, errors.New("ffmpeg decode: нет каналов")
			}
			// в трубе размер данных не известен заранее (0 или 0xFFFFFFFF) — данные до конца вывода
			n := len(body) / 4 / ch
			x := make([]float64, n)
			for i := range n {
				s := 0.0
				for c := range ch {
					s += float64(math.Float32frombits(binary.LittleEndian.Uint32(body[(i*ch+c)*4:])))
				}
				x[i] = s / float64(ch)
			}
			return x, nil
		}
		if size <= 0 || p+8+size > len(b) {
			break
		}
		p += 8 + size + size%2
	}
	return nil, errors.New("ffmpeg decode: нет данных WAV")
}

// loudLevel — 95-й перцентиль 20·lg(RMS кадра + 1e-9) по кадрам loudFrame с шагом loudHop; кадры центрированы
// (края дополнены нулями, как librosa center=True); перцентиль — линейной интерполяцией, как numpy.
func loudLevel(x []float64) float64 {
	pad := loudFrame / 2
	n := 1 + len(x)/loudHop
	db := make([]float64, 0, n)
	for k := range n {
		c := k * loudHop
		s := 0.0
		for i := c - pad; i < c-pad+loudFrame; i++ {
			if i >= 0 && i < len(x) {
				s += x[i] * x[i]
			}
		}
		db = append(db, 20*math.Log10(math.Sqrt(s/loudFrame)+loudFloor))
	}
	slices.Sort(db)
	pos := 0.95 * float64(len(db)-1)
	lo := int(math.Floor(pos))
	hi := min(lo+1, len(db)-1)
	return db[lo] + (db[hi]-db[lo])*(pos-float64(lo))
}

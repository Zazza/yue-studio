package main

import (
	"encoding/binary"
	"testing"
)

// Тесты карточки internal-own-track, этап 13, условие 96 (тест-кейс ТК135):
// loopWav(wav, phaseSec, totalSec) — WAV, начинающийся с отсчёта phase (по модулю круга)
// и повторяющий круг до длины ≥ totalSec; целые кадры; заголовок верный (RIFF- и data-размер,
// каналы, частота — как у входа); не WAV → ошибка.
// Входной WAV собирается здесь; отсчёты — счётчик кадров, чтобы проверять порядок.
// Написаны по карточке, без чтения реализации.

// makeWav — PCM 16 бит; кадр i: канал c = sample(i, c).
func makeWav(rate, channels, frames int, sample func(i, c int) int16) []byte {
	data := make([]byte, frames*channels*2)
	for i := 0; i < frames; i++ {
		for c := 0; c < channels; c++ {
			binary.LittleEndian.PutUint16(data[(i*channels+c)*2:], uint16(sample(i, c)))
		}
	}
	b := make([]byte, 44, 44+len(data))
	copy(b[0:], "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(36+len(data)))
	copy(b[8:], "WAVE")
	copy(b[12:], "fmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1) // PCM
	binary.LittleEndian.PutUint16(b[22:], uint16(channels))
	binary.LittleEndian.PutUint32(b[24:], uint32(rate))
	binary.LittleEndian.PutUint32(b[28:], uint32(rate*channels*2))
	binary.LittleEndian.PutUint16(b[32:], uint16(channels*2))
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(len(data)))
	return append(b, data...)
}

type parsedWav struct {
	format, channels, bits uint16
	rate                   uint32
	data                   []byte
}

// parseWav разбирает выход по чанкам (не полагаясь на 44-байтный заголовок) и проверяет
// согласованность размеров: RIFF-размер = длина файла − 8, data-чанк доходит ровно до конца
// своего объявленного размера и укладывается в файл.
func parseWav(t *testing.T, b []byte) parsedWav {
	t.Helper()
	if len(b) < 12 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		t.Fatalf("выход не RIFF/WAVE (%d байт)", len(b))
	}
	if got := binary.LittleEndian.Uint32(b[4:]); int(got) != len(b)-8 {
		t.Fatalf("RIFF-размер %d, want длина−8 = %d", got, len(b)-8)
	}
	var w parsedWav
	var haveFmt, haveData bool
	for off := 12; off+8 <= len(b); {
		id := string(b[off : off+4])
		size := int(binary.LittleEndian.Uint32(b[off+4:]))
		body := off + 8
		if body+size > len(b) {
			t.Fatalf("чанк %q размером %d выходит за файл (%d байт)", id, size, len(b))
		}
		switch id {
		case "fmt ":
			w.format = binary.LittleEndian.Uint16(b[body:])
			w.channels = binary.LittleEndian.Uint16(b[body+2:])
			w.rate = binary.LittleEndian.Uint32(b[body+4:])
			w.bits = binary.LittleEndian.Uint16(b[body+14:])
			haveFmt = true
		case "data":
			w.data = b[body : body+size]
			haveData = true
		}
		off = body + size + size%2
	}
	if !haveFmt || !haveData {
		t.Fatalf("нет fmt/data чанка: fmt=%v data=%v", haveFmt, haveData)
	}
	return w
}

func frameAt(w parsedWav, i, c int) int16 {
	ch := int(w.channels)
	return int16(binary.LittleEndian.Uint16(w.data[(i*ch+c)*2:]))
}

// Стерео 48 кГц, круг 0,25 с = 12000 кадров. Каналы различаются, чтобы поймать их перестановку.
const (
	lwRate   = 48000
	lwFrames = 12000 // 0,25 с
	lwCycle  = 0.25
)

func stereoCounter(i, c int) int16 {
	if c == 0 {
		return int16(i)
	}
	return int16(-i - 1)
}

// checkLoop: выход — тот же формат, целые кадры, длина ≥ total и не больше total + круг,
// каждый кадр k = кадр входа (phaseFrame + k) mod cycleFrames.
func checkLoop(t *testing.T, out []byte, rate, channels, cycleFrames, phaseFrame, totalFrames int,
	sample func(i, c int) int16) {
	t.Helper()
	w := parseWav(t, out)
	if w.format != 1 || w.bits != 16 {
		t.Errorf("формат %d, бит %d; want PCM 16", w.format, w.bits)
	}
	if int(w.channels) != channels || int(w.rate) != rate {
		t.Fatalf("каналы %d, частота %d; want как у входа %d/%d", w.channels, w.rate, channels, rate)
	}
	block := channels * 2
	if len(w.data)%block != 0 {
		t.Fatalf("data %d байт — не целое число кадров (кадр %d байт)", len(w.data), block)
	}
	n := len(w.data) / block
	if n < totalFrames {
		t.Fatalf("кадров %d, want ≥ total %d", n, totalFrames)
	}
	if n > totalFrames+cycleFrames {
		t.Errorf("кадров %d — больше total+круг (%d): лишние круги", n, totalFrames+cycleFrames)
	}
	for k := 0; k < n; k++ {
		src := (phaseFrame + k) % cycleFrames
		for c := 0; c < channels; c++ {
			if got, want := frameAt(w, k, c), sample(src, c); got != want {
				t.Fatalf("кадр %d канал %d = %d, want %d (кадр входа %d)", k, c, got, want, src)
			}
		}
	}
}

// ТК135: фаза 0, длина 2,5 круга → не короче 2,5 круга, круг повторяется подряд с начала.
func TestLoopWavPhaseZeroRepeatsCycle(t *testing.T) {
	in := makeWav(lwRate, 2, lwFrames, stereoCounter)
	out, err := loopWav(in, 0, 2.5*lwCycle)
	if err != nil {
		t.Fatalf("loopWav: %v", err)
	}
	checkLoop(t, out, lwRate, 2, lwFrames, 0, lwFrames*5/2, stereoCounter)
}

// ТК135: первые отсчёты = отсчёты входа с phase, дальше — через конец круга в его начало.
func TestLoopWavStartsAtPhase(t *testing.T) {
	in := makeWav(lwRate, 2, lwFrames, stereoCounter)
	const phase = 0.0625 // 3000 кадров
	out, err := loopWav(in, phase, 2*lwCycle)
	if err != nil {
		t.Fatalf("loopWav: %v", err)
	}
	checkLoop(t, out, lwRate, 2, lwFrames, 3000, 2*lwFrames, stereoCounter)
	w := parseWav(t, out)
	if frameAt(w, 0, 0) != 3000 || frameAt(w, 0, 1) != -3001 {
		t.Errorf("первый кадр (%d,%d), want (3000,-3001)", frameAt(w, 0, 0), frameAt(w, 0, 1))
	}
}

// ТК135: фаза больше круга — по модулю круга.
func TestLoopWavPhaseModuloCycle(t *testing.T) {
	in := makeWav(lwRate, 2, lwFrames, stereoCounter)
	for _, phase := range []float64{lwCycle + 0.0625, 10*lwCycle + 0.0625} {
		out, err := loopWav(in, phase, 1.5*lwCycle)
		if err != nil {
			t.Fatalf("loopWav(phase=%v): %v", phase, err)
		}
		checkLoop(t, out, lwRate, 2, lwFrames, 3000, lwFrames*3/2, stereoCounter)
	}
}

// Условие 96: каналы и частота — как у входа (не захардкожены 2 × 48000).
func TestLoopWavKeepsFormatOfInput(t *testing.T) {
	const rate, frames = 32000, 8000 // круг 0,25 с, моно
	mono := func(i, _ int) int16 { return int16(i * 3) }
	in := makeWav(rate, 1, frames, mono)
	out, err := loopWav(in, 0.125, 0.5) // фаза 4000 кадров, длина 16000
	if err != nil {
		t.Fatalf("loopWav: %v", err)
	}
	checkLoop(t, out, rate, 1, frames, 4000, 16000, mono)
}

// Длина меньше круга — всё равно ≥ total, с фазы.
func TestLoopWavShorterThanCycle(t *testing.T) {
	in := makeWav(lwRate, 2, lwFrames, stereoCounter)
	out, err := loopWav(in, 0.125, 0.0625) // фаза 6000, длина 3000 кадров
	if err != nil {
		t.Fatalf("loopWav: %v", err)
	}
	checkLoop(t, out, lwRate, 2, lwFrames, 6000, 3000, stereoCounter)
}

// ТК135: не WAV → ошибка.
func TestLoopWavRejectsNonWav(t *testing.T) {
	for name, b := range map[string][]byte{
		"nil":   nil,
		"мусор": []byte("this is not a wav file at all, just some bytes......"),
		"flac":  append([]byte("fLaC"), make([]byte, 60)...),
		"обрыв": makeWav(lwRate, 2, lwFrames, stereoCounter)[:20],
	} {
		if _, err := loopWav(b, 0, 1); err == nil {
			t.Errorf("%s: ошибки нет, want ошибка «не WAV»", name)
		}
	}
}

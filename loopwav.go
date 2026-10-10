package main

import (
	"encoding/binary"
	"fmt"
	"math"
)

// loopWav — круг фразы («Инструменты») в длинный WAV для плеера: с кадра phaseSec (по модулю
// круга) и дальше круг за кругом, пока не наберётся totalSec. Плеер (pw-play) не умеет играть по
// кругу, а перемотка режет файл ffmpeg — поэтому круг раскладывается заранее, с нужного места:
// новый звук после поворота крутилки подхватывается с той же доли круга без перемотки.
// Вход — PCM WAV (fmt и data чанки); заголовок выхода — fmt входа как есть + data.
func loopWav(wav []byte, phaseSec, totalSec float64) ([]byte, error) {
	if len(wav) < 12 || string(wav[0:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		return nil, fmt.Errorf("loop: не WAV")
	}
	var fmtChunk, data []byte
	for off := 12; off+8 <= len(wav); {
		id := string(wav[off : off+4])
		size := int(binary.LittleEndian.Uint32(wav[off+4:]))
		body := off + 8
		if size < 0 || body+size > len(wav) {
			return nil, fmt.Errorf("loop: чанк %q обрезан", id)
		}
		switch id {
		case "fmt ":
			fmtChunk = wav[body : body+size]
		case "data":
			data = wav[body : body+size]
		}
		off = body + size + size%2
	}
	if len(fmtChunk) < 16 || data == nil {
		return nil, fmt.Errorf("loop: нет fmt/data")
	}
	rate := int(binary.LittleEndian.Uint32(fmtChunk[4:]))
	align := int(binary.LittleEndian.Uint16(fmtChunk[12:]))
	if rate <= 0 || align <= 0 || len(data) < align {
		return nil, fmt.Errorf("loop: пустой круг")
	}
	frames := len(data) / align
	cycle := data[:frames*align]
	start := int(math.Round(phaseSec*float64(rate))) % frames
	if start < 0 {
		start += frames
	}
	need := int(math.Ceil(totalSec * float64(rate)))
	if need < 1 {
		need = 1
	}
	outLen := need * align
	hdr := 12 + 8 + len(fmtChunk) + len(fmtChunk)%2 + 8
	out := make([]byte, hdr, hdr+outLen+outLen%2)
	copy(out[0:], "RIFF")
	copy(out[8:], "WAVE")
	copy(out[12:], "fmt ")
	binary.LittleEndian.PutUint32(out[16:], uint32(len(fmtChunk)))
	copy(out[20:], fmtChunk)
	d := 20 + len(fmtChunk) + len(fmtChunk)%2
	copy(out[d:], "data")
	binary.LittleEndian.PutUint32(out[d+4:], uint32(outLen))
	out = append(out, cycle[start*align:]...)
	for len(out)-hdr < outLen {
		out = append(out, cycle...)
	}
	out = out[:hdr+outLen]
	if outLen%2 == 1 {
		out = append(out, 0)
	}
	binary.LittleEndian.PutUint32(out[4:], uint32(len(out)-8))
	return out, nil
}

package studio

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"

	"yue-studio/internal/yue"
)

// Тесты карточки internal-own-track, этап 10, условие 83 (тест-кейс ТК123): цель громкости по обработанной
// дорожке. Написаны по карточке, без чтения реализации.
//
// Контракт: у записи пресета с цепочкой движка (Engine) и level_db громкость доводится по ОБРАБОТАННОЙ
// дорожке — куску движка, который ляжет в трек: подстройка = level_db − (уровень громких мест куска −
// громкость трека), в пределах ±12 дБ; итог: уровень куска + гейн вставки − громкость трека = цель.
// Громкость трека — как раньше: сумма мощностей rms_p95_db основных дорожек (vocals, drums, bass, other)
// из замеров JobStems. Запись level_db без цепочки — как раньше, по замеру исходной дорожки.
//
// Как видно гейн вставки: обработанный кусок фейка — тон 1000 Гц постоянной амплитуды prlChunkAmp (в треке
// его нет), поэтому гейн = амплитуда 1000 Гц в пересборке к prlChunkAmp, в дБ (середина трека 2–8 с).
// Кусок вдвое громче исходного баса (+6,02 дБ). Замеры rms_p95_db фейка равны настоящим уровням тонов
// дорожек (синус A → 20·lg(A/√2)), чтобы «по исходной» и «по обработанной» различались ровно на 6 дБ.
//
// Ожидания:
//   громкость трека: vocals, other −23,01; drums, bass −29,03 → 10·lg(2·0,005 + 2·0,00125) = −19,03;
//   кусок: −23,01 → уровень куска к треку −3,98;
//   level_db −10 → гейн −6,02 (по исходной было бы 0 — цель + 6);
//   level_db −19 → подстройка −15,02 → зажата −12 (по исходной было бы −9);
//   без цепочки level_db −14 → бас по исходной: −14 − (−29,03 + 19,03) = −4, ApplyFx не вызывается.

const (
	prlVoc      = "0.1*sin(2*PI*3000*t)"
	prlOther    = "0.1*sin(2*PI*500*t)"
	prlDrums    = "0.05*sin(2*PI*5000*t)"
	prlBass     = "0.05*sin(2*PI*200*t)"
	prlChunkHz  = 1000.0
	prlChunkAmp = 0.1 // вдвое громче исходного баса (0,05): +6,02 дБ
)

func prlSineDb(a float64) float64 { return 20 * math.Log10(a/math.Sqrt2) }

// prlFake — lvFake (замеры дорожек) + движок, который отдаёт кусок «тон 1000 Гц, амплитуда prlChunkAmp».
type prlFake struct {
	*lvFake
}

func (f *prlFake) ApplyFx(_ context.Context, id int64, req yue.FxRequest) (*yue.DspVariant, error) {
	f.calls = append(f.calls, engCall{id, req})
	f.log = append(f.log, "applyfx:"+req.Source)
	if req.From == nil || req.To == nil {
		f.t.Errorf("ApplyFx без окна: from=%v to=%v", req.From, req.To)
		return nil, &yue.StatusError{Code: 422, Msg: "preview needs from/to"}
	}
	name := fmt.Sprintf("preview-fx-%08x.flac", len(f.calls))
	dur := *req.To + req.Fade - *req.From + engTail
	expr := fmt.Sprintf("%g*sin(2*PI*%g*t)", prlChunkAmp, prlChunkHz)
	chunk := decodeFile(f.t, lavfi(f.t, aeval(expr, dur), filepath.Join(f.dir, "chunk-"+name)))
	return workerFile(f.t, f.files, f.dir, id, name, chunk, *req.From, req.Pad, sr), nil
}

// prlSetup — трек: vocals 3000 + other 500 + drums 5000 + bass 200 Гц; замеры — настоящие уровни тонов.
func prlSetup(t *testing.T) (*prlFake, []float32) {
	t.Helper()
	needFFmpeg(t)
	dir := t.TempDir()
	p := func(n string) string { return filepath.Join(dir, n) }
	files := map[string]string{
		"audio.flac":       lavfi(t, aeval(strings.Join([]string{prlVoc, prlOther, prlDrums, prlBass}, "+"), lvDur), p("prl-audio.flac")),
		"stem-vocals.flac": lavfi(t, aeval(prlVoc, lvDur), p("prl-vocals.flac")),
		"stem-other.flac":  lavfi(t, aeval(prlOther, lvDur), p("prl-other.flac")),
		"stem-drums.flac":  lavfi(t, aeval(prlDrums, lvDur), p("prl-drums.flac")),
		"stem-bass.flac":   lavfi(t, aeval(prlBass, lvDur), p("prl-bass.flac")),
	}
	sf := newSecFake()
	put(sf, psJobID, files)
	ps := &psFake{
		engFake: newEngFake(t, sf),
		jobs: []yue.Job{{ID: psJobID, Title: psTitle, Status: "done", AudioFile: "audio.flac",
			DurationSec: lvDur}},
	}
	p95 := map[string]float64{
		"vocals": prlSineDb(0.1), "other": prlSineDb(0.1), "drums": prlSineDb(0.05), "bass": prlSineDb(0.05),
	}
	return &prlFake{lvFake: &lvFake{psFake: ps, p95: p95}}, decodeFile(t, files["audio.flac"])
}

// prlTrackSum — громкость трека: сумма мощностей основных дорожек, дБ (−19,03).
func prlTrackSum() float64 {
	return 10 * math.Log10(2*math.Pow(10, prlSineDb(0.1)/10)+2*math.Pow(10, prlSineDb(0.05)/10))
}

func prlRun(t *testing.T, f *prlFake, p yue.SoundPreset) []float32 {
	t.Helper()
	if _, err := ApplySoundPreset(context.Background(), f, psJobID, p); err != nil {
		t.Fatalf("ApplySoundPreset: %v", err)
	}
	name := fmt.Sprintf("dsp-preset-%d-mix.flac", p.ID)
	data, ok := f.uploads[name]
	if !ok {
		t.Fatalf("пересборки нет: загружено %v, want %s", keys(map[string][]byte(f.uploads)), name)
	}
	return decodeBytes(t, data)
}

// prlInsertGain — гейн вставки обработанного куска в пересборке, дБ.
func prlInsertGain(t *testing.T, out []float32) float64 {
	t.Helper()
	a := toneAmp(out, prlChunkHz, 2, 8)
	if a <= 0 {
		t.Fatalf("обработанного куска (%.0f Гц) в пересборке нет", prlChunkHz)
	}
	return 20 * math.Log10(a/prlChunkAmp)
}

func prlEngineSpec(level float64) yue.PresetSpec {
	return yue.PresetSpec{Stems: []string{"bass"}, Engine: engChain(), LevelDb: lvF(level)}
}

// ТК123: кусок движка на +6 дБ громче исходной → гейн по куску: уровень куска + гейн − трек = цель ±0,5
// (а не цель + 6, как при счёте по исходной дорожке).
func TestPresetLevelDbByProcessedChunk(t *testing.T) {
	f, _ := prlSetup(t)
	const target = -10.0
	out := prlRun(t, f, lvPreset(41, prlEngineSpec(target)))
	if len(f.calls) != 1 || f.calls[0].req.Source != "bass" {
		t.Fatalf("ApplyFx: %d вызовов, want 1 на bass", len(f.calls))
	}
	g := prlInsertGain(t, out)
	got := prlSineDb(prlChunkAmp) + g - prlTrackSum()
	if math.Abs(got-target) > 0.5 {
		t.Errorf("обработанный бас к треку %.2f дБ (гейн вставки %+.2f), want цель %.1f ±0,5"+
			" (по исходной дорожке вышло бы %.1f)", got, g, target, target+6.02)
	}
}

// ТК123: подстройка по обработанной дорожке зажата ±12: level_db −19 → нужно −15,02 → гейн −12
// (по исходной дорожке было бы −9).
func TestPresetLevelDbProcessedClamp(t *testing.T) {
	f, _ := prlSetup(t)
	const target = -19.0
	need := target - (prlSineDb(prlChunkAmp) - prlTrackSum())
	if need > -12.5 {
		t.Fatalf("сценарий неверен: нужная подстройка %.2f должна выходить за −12", need)
	}
	g := prlInsertGain(t, prlRun(t, f, lvPreset(42, prlEngineSpec(target))))
	if math.Abs(g - -12) > 0.5 {
		t.Errorf("гейн вставки %+.2f дБ, want −12 ±0,5 (подстройка %.2f зажата пределом)", g, need)
	}
}

// ТК123 (прежнее поведение): запись level_db без цепочки — по замеру исходной дорожки, движок не зовётся.
func TestPresetLevelDbNoEngineBySource(t *testing.T) {
	f, orig := prlSetup(t)
	const target = -14.0
	out := prlRun(t, f, lvPreset(43, yue.PresetSpec{Stems: []string{"bass"}, LevelDb: lvF(target)}))
	if len(f.calls) != 0 {
		t.Errorf("ApplyFx вызван %d раз, want 0 (запись без цепочки)", len(f.calls))
	}
	got := 20 * math.Log10(toneAmp(out, 200, 2, 8)/toneAmp(orig, 200, 2, 8))
	want := target - (prlSineDb(0.05) - prlTrackSum())
	if math.Abs(got-want) > 0.1 {
		t.Errorf("бас изменён на %+.2f дБ, want %+.2f ±0,1 (по исходной дорожке)", got, want)
	}
}

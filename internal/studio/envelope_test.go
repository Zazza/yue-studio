package studio

import (
	"context"
	"encoding/json"
	"math"
	"os/exec"
	"path/filepath"
	"testing"

	"yue-studio/internal/dsp"
)

// Тесты карточки internal-vocal-ride, условие 2: VolumeEnvelope.
// stem "" — весь трек (звук трека по FetchBase) через EnvelopeGraph, загрузка
// dsp-envelope.flac; stem vocals/drums/bass/other — только эта дорожка через
// пересборку (спека ChildID 0, Stems [stem], Envelope). Фейковый воркер (secFake)
// и синтетика — из sections_test.go/stem_fx_test.go, звуковые хелперы — из helpers_test.go.

// rampPts — огибающая кейса карточки: 0 дБ до 1 с, линейно в дБ до −12 в 3 с, дальше −12.
var rampPts = []dsp.EnvPoint{{T: 1, Db: 0}, {T: 3, Db: -12}}

const envAmp = 0.3 // тон 440 Гц трека «весь трек»

// envTrack — трек 6 с: тон 440 Гц 0.3 в audio.flac родителя.
func envTrack(t *testing.T) *secFake {
	t.Helper()
	needFFmpeg(t)
	f := newSecFake()
	put(f, parentID, map[string]string{
		"audio.flac": lavfi(t, aeval(expr440, 6), filepath.Join(t.TempDir(), "env-audio.flac")),
	})
	return f
}

// assertRamp440 — у тона 440 Гц уровень следует rampPts: исходный до 1 с, ≈ −6 в 2 с, ≈ −12 после 3 с.
func assertRamp440(t *testing.T, out []float32) {
	t.Helper()
	if d := db(toneAmp(out, 440, 0, 1)) - db(envAmp); math.Abs(d) > 0.5 {
		t.Errorf("0–1 с: %+.2f дБ, want ≈ 0 (дБ первой точки)", d)
	}
	if d := db(toneAmp(out, 440, 1.95, 2.05)) - db(envAmp); math.Abs(d-(-6)) > 1 {
		t.Errorf("в 2 с: %+.2f дБ, want −6 ± 1", d)
	}
	if d := db(toneAmp(out, 440, 4, 5)) - db(envAmp); math.Abs(d-(-12)) > 0.5 {
		t.Errorf("4–5 с: %+.2f дБ, want −12 ± 0.5 (дБ последней точки)", d)
	}
	if d := float64(len(out)) / sr; math.Abs(d-6) > 0.05 {
		t.Errorf("длина %.3f с, want 6 (длина трека)", d)
	}
}

// Кейс карточки: stem "" — загружен ровно один файл dsp-envelope.flac у той же джобы,
// уровень меняется по огибающей; возвращён вариант.
func TestVolumeEnvelopeWholeTrack(t *testing.T) {
	f := envTrack(t)
	v, err := VolumeEnvelope(context.Background(), f, parentID, "", rampPts)
	if err != nil {
		t.Fatalf("VolumeEnvelope: %v", err)
	}
	if v == nil {
		t.Fatal("вариант nil")
	}
	if len(f.uploads) != 1 {
		t.Fatalf("загрузок %d, want 1: %v", len(f.uploads), keys(f.uploads))
	}
	if len(f.uploadedTo) != 1 || f.uploadedTo[0] != parentID {
		t.Errorf("загружено к джобам %v, want [%d]", f.uploadedTo, parentID)
	}
	assertRamp440(t, secUploaded(t, f, "dsp-envelope.flac"))
}

// Кейс карточки: импортированный трек — только audio.mp3 (audio.flac → 404) — тоже работает.
func TestVolumeEnvelopeImportedMp3(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()
	flac := lavfi(t, aeval(expr440, 6), filepath.Join(dir, "src.flac"))
	mp3 := filepath.Join(dir, "base.mp3")
	if out, err := exec.Command("ffmpeg", "-y", "-loglevel", "error", "-i", flac, mp3).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	f := newSecFake()
	put(f, parentID, map[string]string{"audio.mp3": mp3})
	if _, err := VolumeEnvelope(context.Background(), f, parentID, "", rampPts); err != nil {
		t.Fatalf("VolumeEnvelope на audio.mp3: %v", err)
	}
	if len(f.uploads) != 1 {
		t.Fatalf("загрузок %d, want 1: %v", len(f.uploads), keys(f.uploads))
	}
	out := secUploaded(t, f, "dsp-envelope.flac")
	// mp3 добавляет задержку кодера (десятки мс): проверки по окнам, длина — с допуском
	if d := db(toneAmp(out, 440, 0.2, 0.8)) - db(envAmp); math.Abs(d) > 0.7 {
		t.Errorf("до 1 с: %+.2f дБ, want ≈ 0", d)
	}
	if d := db(toneAmp(out, 440, 4, 5)) - db(envAmp); math.Abs(d-(-12)) > 0.7 {
		t.Errorf("4–5 с: %+.2f дБ, want ≈ −12", d)
	}
}

// Кейс карточки: stem vocals — через пересборку меняется только голос (3000 Гц) по
// огибающей; тон другой дорожки (other, 500 Гц) в миксе не меняется (|Δ| ≤ 0.5 дБ).
func TestVolumeEnvelopeStemOnlyThatStem(t *testing.T) {
	f, _ := fxSetup(t)
	v, err := VolumeEnvelope(context.Background(), f, parentID, "vocals", rampPts)
	if err != nil {
		t.Fatalf("VolumeEnvelope vocals: %v", err)
	}
	if v == nil {
		t.Fatal("вариант nil")
	}
	out := uploadedOnly(t, f)
	if d := float64(len(out)) / sr; math.Abs(d-fxDur) > 0.05 {
		t.Errorf("длина %.3f с, want %.0f (длина трека)", d, fxDur)
	}
	if d := db(toneAmp(out, 3000, 0, 1)) - db(fxAmpA); math.Abs(d) > 0.5 {
		t.Errorf("голос 0–1 с: %+.2f дБ, want ≈ 0", d)
	}
	if d := db(toneAmp(out, 3000, 1.95, 2.05)) - db(fxAmpA); math.Abs(d-(-6)) > 1 {
		t.Errorf("голос в 2 с: %+.2f дБ, want −6 ± 1", d)
	}
	if d := db(toneAmp(out, 3000, 4, 7)) - db(fxAmpA); math.Abs(d-(-12)) > 0.5 {
		t.Errorf("голос 4–7 с: %+.2f дБ, want −12 ± 0.5", d)
	}
	for _, w := range [][2]float64{{0, 1}, {1.9, 2.1}, {4, 7}} {
		if d := db(toneAmp(out, 500, w[0], w[1])) - db(fxAmpB); math.Abs(d) > 0.5 {
			t.Errorf("other (500 Гц) %.1f–%.1f с: %+.2f дБ, want |Δ| ≤ 0.5 (чужая дорожка не трогается)", w[0], w[1], d)
		}
	}
}

// Кейс карточки: неизвестная дорожка — ошибка, ничего не загружено.
func TestVolumeEnvelopeUnknownStemIsError(t *testing.T) {
	f, _ := fxSetup(t)
	if _, err := VolumeEnvelope(context.Background(), f, parentID, "flute", rampPts); err == nil {
		t.Fatal("stem flute: want ошибку, got nil")
	}
	if len(f.uploads) != 0 {
		t.Errorf("при ошибке загружено: %v", keys(f.uploads))
	}
}

// Кейс карточки: пустые/невалидные точки — ошибка, ничего не загружено (и для трека, и для дорожки).
func TestVolumeEnvelopeEmptyPointsIsError(t *testing.T) {
	f, _ := fxSetup(t)
	cases := map[string][]dsp.EnvPoint{
		"nil":            nil,
		"пусто":          {},
		"все невалидные": {{T: -1, Db: 0}, {T: math.NaN(), Db: -3}},
	}
	for name, pts := range cases {
		for _, stem := range []string{"", "vocals"} {
			if _, err := VolumeEnvelope(context.Background(), f, parentID, stem, pts); err == nil {
				t.Errorf("%s, stem %q: want ошибку, got nil", name, stem)
			}
		}
	}
	if len(f.uploads) != 0 {
		t.Errorf("при ошибке загружено: %v", keys(f.uploads))
	}
}

// Поле envelope спеки приходит из фронта/MCP по JSON.
func TestSectionSpecEnvelopeJSON(t *testing.T) {
	var sp SectionSpec
	if err := json.Unmarshal([]byte(`{"child_id":0,"stems":["vocals"],"envelope":[{"t":1,"db":-3}]}`), &sp); err != nil {
		t.Fatal(err)
	}
	if len(sp.Envelope) != 1 || sp.Envelope[0] != (dsp.EnvPoint{T: 1, Db: -3}) {
		t.Errorf("envelope → Envelope=%+v, want [{T:1 Db:-3}]", sp.Envelope)
	}
	// без огибающей поле в JSON не появляется (omitempty)
	raw, err := json.Marshal(SectionSpec{Stems: []string{"vocals"}})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["envelope"]; ok {
		t.Errorf("пустая огибающая сериализована: %s", raw)
	}
}

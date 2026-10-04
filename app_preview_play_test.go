package main

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"yue-studio/internal/dsp"
	"yue-studio/internal/yue"
)

// Тесты карточки internal-guitar-pedals, условия 1.6 и 1.10 (путь приложения):
// YuePlayPreview играет кусок превью по имени слота ("", "P", "A"–"D") и куска
// (wet/dry/wet_solo/dry_solo), а не по произвольному пути; иное имя — ошибка, плеер
// не трогается; допустимое имя, пока превью нет, — ошибка; после YueFxPreview —
// плеер загружает кусок и стартует с заданной секунды. Превью на воркер не грузится
// (UploadDsp у fakeService не реализован — вызов уронил бы тест).

var ppSlots = []string{"", "P", "A", "B", "C", "D"}
var ppWhich = []string{"wet", "dry", "wet_solo", "dry_solo"}

// recPlayer — fakePlayer, запоминающий загруженные байты.
type recPlayer struct {
	fakePlayer
	data [][]byte
	durs []time.Duration
}

func (p *recPlayer) Load(id int64, data []byte, dur time.Duration) error {
	p.data = append(p.data, append([]byte(nil), data...))
	p.durs = append(p.durs, dur)
	return p.fakePlayer.Load(id, data, dur)
}

func (p *recPlayer) touched() bool {
	return len(p.loaded) > 0 || p.plays > 0 || len(p.seeked) > 0 || p.stops > 0
}

func ppNeedFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
}

// ppLavfi — flac 16 кГц моно из выражения aevalsrc, байты.
func ppLavfi(t *testing.T, expr string, dur float64) []byte {
	t.Helper()
	p := filepath.Join(t.TempDir(), "x.flac")
	src := fmt.Sprintf("aevalsrc=exprs='%s':d=%g:s=16000", expr, dur)
	if out, err := exec.Command("ffmpeg", "-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", src, "-ac", "1", "-ar", "16000", p).CombinedOutput(); err != nil {
		t.Fatalf("lavfi %q: %v %s", src, err, out)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// ppSecs — длительность звука в байтах, с (декод в моно 16 кГц).
func ppSecs(t *testing.T, data []byte) float64 {
	t.Helper()
	p := filepath.Join(t.TempDir(), "loaded")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error",
		"-i", p, "-f", "f32le", "-ac", "1", "-ar", "16000", "-").Output()
	if err != nil {
		t.Fatalf("загруженное в плеер не декодируется: %v", err)
	}
	return float64(len(raw)/4) / 16000
}

// ppApp — App с джобой 1 (8 с: other 440 Гц + drums 300 Гц, все стемы) и плеером-записью.
func ppApp(t *testing.T) (*App, *recPlayer) {
	t.Helper()
	ppNeedFFmpeg(t)
	s := &fakeService{
		jobs: []yue.Job{{ID: 1, Status: "done", AudioFile: "audio.flac"}},
		fetchData: map[string][]byte{
			"audio.flac":       ppLavfi(t, "0.3*sin(2*PI*440*t)+0.2*sin(2*PI*300*t)", 8),
			"stem-other.flac":  ppLavfi(t, "0.3*sin(2*PI*440*t)", 8),
			"stem-drums.flac":  ppLavfi(t, "0.2*sin(2*PI*300*t)", 8),
			"stem-bass.flac":   ppLavfi(t, "0", 8),
			"stem-vocals.flac": ppLavfi(t, "0", 8),
		},
	}
	p := &recPlayer{}
	a := newTestApp(s, p)
	t.Cleanup(a.fxCleanup)
	return a, p
}

func ppSteps() []dsp.Step { return []dsp.Step{{Chain: "eq", Params: map[string]float64{"high": -12}}} }

// Карточка 1.6: превью ещё нет — любое допустимое имя слота/куска даёт ошибку,
// плеер не трогается.
func TestPlayPreviewNotMadeIsError(t *testing.T) {
	p := &recPlayer{}
	a := newTestApp(&fakeService{jobs: []yue.Job{{ID: 1, Status: "done", AudioFile: "audio.flac"}}}, p)
	defer a.fxCleanup()
	for _, slot := range ppSlots {
		for _, which := range ppWhich {
			if err := a.YuePlayPreview(1, slot, which, 0); err == nil {
				t.Errorf("слот %q, кусок %q без превью: want ошибку «превью ещё не сделано», got nil", slot, which)
			}
		}
	}
	if p.touched() {
		t.Errorf("плеер тронут без превью: %+v", p.fakePlayer)
	}
}

// Карточка 1.6 / 1.10: неизвестный слот или кусок — ошибка, плеер не трогается.
// Превью в слотах "" и "A" сделано (на дорожку — есть и соло), поэтому ошибка
// может быть только из-за имени, а не из-за отсутствия превью.
func TestPlayPreviewUnknownNameIsError(t *testing.T) {
	a, p := ppApp(t)
	for _, slot := range []string{"", "A"} {
		if _, err := a.YueFxPreview(1, "other", ppSteps(), 2, 4, slot); err != nil {
			t.Fatalf("YueFxPreview слот %q: %v", slot, err)
		}
	}
	cases := []struct{ slot, which string }{
		{"E", "wet"}, {"a", "wet"}, {"PP", "wet"}, {"../A", "wet"}, {"/tmp", "wet"},
		{"A", ""}, {"A", "mix"}, {"A", "WET"}, {"A", "../wet"}, {"A", "solo"},
		{"", "/etc/passwd"}, {"", "wet.flac"},
	}
	for _, c := range cases {
		if err := a.YuePlayPreview(1, c.slot, c.which, 0); err == nil {
			t.Errorf("слот %q, кусок %q: want ошибку (недопустимое имя), got nil", c.slot, c.which)
		}
	}
	if p.touched() {
		t.Errorf("плеер тронут при недопустимых именах: loaded=%v plays=%d seeked=%v",
			p.loaded, p.plays, p.seeked)
	}
}

// Карточка 1.6: превью сделано в слоте — YuePlayPreview по имени загружает кусок
// в плеер и стартует с заданной секунды (Seek на неё либо кусок, начатый с неё).
// Другие допустимые слоты, где превью не делалось, — по-прежнему ошибка.
func TestPlayPreviewPlaysByNameFromSecond(t *testing.T) {
	const from, to, start = 2.0, 4.0, 0.5
	for _, slot := range ppSlots {
		t.Run("слот "+slot, func(t *testing.T) {
			a, p := ppApp(t)
			res, err := a.YueFxPreview(1, "other", ppSteps(), from, to, slot)
			if err != nil {
				t.Fatalf("YueFxPreview: %v", err)
			}
			for _, which := range ppWhich {
				before := len(p.data)
				if err := a.YuePlayPreview(1, slot, which, start); err != nil {
					t.Errorf("кусок %q: %v", which, err)
					continue
				}
				if len(p.data) != before+1 {
					t.Errorf("кусок %q: загрузок в плеер %d, want 1", which, len(p.data)-before)
					continue
				}
				// запуск — Play или Seek (Seek у Player перезапускает воспроизведение с позиции)
				if p.plays == 0 && len(p.seeked) == 0 {
					t.Errorf("кусок %q: воспроизведение не запущено (ни Play, ни Seek)", which)
				}
				full := res.DurSec
				if full <= 0 {
					full = to - from
				}
				got := ppSecs(t, p.data[len(p.data)-1])
				seekOK := len(p.seeked) > 0 && math.Abs(p.seeked[len(p.seeked)-1].Seconds()-start) < 0.05
				trimOK := math.Abs(got-(full-start)) < 0.05
				if !seekOK && !trimOK {
					t.Errorf("кусок %q: старт не с %.1f с — seek=%v, длина загруженного %.3f с (полный кусок %.3f)",
						which, start, p.seeked, got, full)
				}
			}
			other := "B"
			if slot == "B" {
				other = "C"
			}
			if err := a.YuePlayPreview(1, other, "wet", 0); err == nil {
				t.Errorf("слот %q без превью (сделано только в %q): want ошибку, got nil", other, slot)
			}
		})
	}
}

// Карточка 1.10: у превью на весь трек соло нет — wet_solo/dry_solo для него —
// ошибка, плеер не трогается; wet/dry играются.
func TestPlayPreviewWholeTrackHasNoSolo(t *testing.T) {
	a, p := ppApp(t)
	if _, err := a.YueFxPreview(1, "", ppSteps(), 2, 4, "A"); err != nil {
		t.Fatalf("YueFxPreview весь трек: %v", err)
	}
	for _, which := range []string{"wet_solo", "dry_solo"} {
		if err := a.YuePlayPreview(1, "A", which, 0); err == nil {
			t.Errorf("весь трек, кусок %q: want ошибку (соло нет), got nil", which)
		}
	}
	if p.touched() {
		t.Errorf("плеер тронут при отсутствующем соло: loaded=%v plays=%d", p.loaded, p.plays)
	}
	if err := a.YuePlayPreview(1, "A", "wet", 0); err != nil {
		t.Errorf("весь трек, кусок wet: %v", err)
	}
}

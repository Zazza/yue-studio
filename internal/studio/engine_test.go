package studio

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"yue-studio/internal/yue"
)

// Тесты карточки internal-studio-engine (этап 4 звукового движка), тест-кейсы 1–6:
// запись пересборки ChildID 0 с Engine — цепочка движка на дорожки Stems в окне
// [From, To) (To ≤ 0 — до конца трека). Дорожка считается на воркере
// (ApplyFx: source = дорожка, output solo, preview, fade, pad); в трек ложится
// обработанный кусок (+1, с хвостом) с From − f и исходная дорожка в окне
// [From − f, To + f) с линейными фейдами f (−1). f = 0,05 с (контракт).
//
// pad (условие 10): воркер отдаёт файл от начала трека — тишина round(from·sr) сэмплов,
// дальше кусок. Все фейки этого файла ведут себя как воркер при обоих значениях pad
// (workerFile): без pad — кусок без тишины спереди. Пересборка обязана просить pad.
//
// Воркер — engFake: secFake (sections_test.go) + ApplyFx. «Обработанный кусок»
// фейка — заранее известный сигнал: тон 1000 Гц первую секунду, дальше 1500 Гц,
// длиной (To + f − from) + engTail (хвост). По тому, где в треке звучат 1000/1500,
// видно, куда и с какого места куска он вклеен; по тону дорожки (500 Гц other,
// 3000 Гц vocals) — где и с какими краями вычтена исходная дорожка.

const (
	engFade = 0.05 // фейд краёв окна (muteFadeSec по контракту)
	engTail = 1.0  // хвост, который «воркер» дописывает после To + f
	engDur  = 10.0 // длина трека
	engAmp  = 0.2  // амплитуда обработанного куска
	ampTone = 0.3  // амплитуда тонов дорожек (exprA3000 / exprB500)
)

// engChunkExpr — содержимое «обработанного куска»: 1000 Гц на [0, 1) с, 1500 Гц дальше.
const engChunkExpr = "0.2*sin(2*PI*1000*t)*lt(t\\,1)+0.2*sin(2*PI*1500*t)*gte(t\\,1)"

type engCall struct {
	id  int64
	req yue.FxRequest
}

// engFake — фейковый воркер с движком: записывает вызовы ApplyFx, отдаёт
// «обработанный кусок» файлом, который потом скачивается через FetchAudio.
type engFake struct {
	*secFake
	t     *testing.T
	dir   string
	calls []engCall
	err   error // ApplyFx отвечает ошибкой (движок выключен, цепочка неверна)
	lost  bool  // ApplyFx отвечает именем файла, которого нет (скачать нельзя)

	cfg     map[string]any // ответ /config воркера (по умолчанию fx_preview: true)
	cfgErr  error          // /config отвечает ошибкой
	variant bool           // ApplyFx отвечает вариантом dsp-fx-* вместо превью (старый воркер)
}

func newEngFake(t *testing.T, f *secFake) *engFake {
	return &engFake{secFake: f, t: t, dir: t.TempDir(), cfg: map[string]any{"fx_preview": true}}
}

func (f *engFake) WorkerConfig(context.Context) (map[string]any, error) {
	if f.cfgErr != nil {
		return nil, f.cfgErr
	}
	return f.cfg, nil
}

func (f *engFake) ApplyFx(_ context.Context, id int64, req yue.FxRequest) (*yue.DspVariant, error) {
	f.calls = append(f.calls, engCall{id, req})
	if f.err != nil {
		return nil, f.err
	}
	if req.From == nil || req.To == nil {
		f.t.Errorf("ApplyFx без окна: from=%v to=%v (превью требует from/to)", req.From, req.To)
		return nil, &yue.StatusError{Code: 422, Msg: "preview needs from/to"}
	}
	name := fmt.Sprintf("preview-fx-%08x.flac", len(f.calls))
	if f.variant {
		// воркер без превью: игнорирует preview и сохраняет обычный вариант
		name = fmt.Sprintf("dsp-fx-%s-%08x.flac", req.Source, len(f.calls))
	}
	if f.lost {
		return &yue.DspVariant{File: name, DurationSec: 1}, nil
	}
	dur := *req.To + req.Fade - *req.From + engTail
	chunk := decodeFile(f.t, lavfi(f.t, aeval(engChunkExpr, dur), filepath.Join(f.dir, "chunk-"+name)))
	return workerFile(f.t, f.files, f.dir, id, name, chunk, *req.From, req.Pad, sr), nil
}

// workerFile — файл ответа превью, как его пишет воркер: pad=false — кусок seg как есть;
// pad=true — тишина a = round(from·rate) сэмплов, дальше seg (файл от начала трека).
// Регистрирует файл у джобы id; DurationSec — длина файла.
func workerFile(t *testing.T, files map[string]string, dir string, id int64, name string,
	seg []float32, from float64, pad bool, rate int) *yue.DspVariant {
	t.Helper()
	data := seg
	if pad {
		a := int(math.Round(from * float64(rate)))
		data = make([]float32, a+len(seg))
		copy(data[a:], seg)
	}
	p := filepath.Join(dir, name)
	writeFlac24(t, data, rate, p)
	files[key(id, name)] = p
	return &yue.DspVariant{File: name, DurationSec: float64(len(data)) / float64(rate)}
}

// engSetup — родитель: трек = vocals (3000 Гц) + other (500 Гц), drums/bass — тишина,
// плюс дорожка kick (не из списка изменяемых) — тон 2000 Гц, в трек не входит.
func engSetup(t *testing.T) (*engFake, []float32) {
	t.Helper()
	needFFmpeg(t)
	dir := t.TempDir()
	f := newSecFake()
	pf := map[string]string{
		"audio.flac":       lavfi(t, aeval(exprA3000+"+"+exprB500, engDur), filepath.Join(dir, "e-audio.flac")),
		"stem-vocals.flac": lavfi(t, aeval(exprA3000, engDur), filepath.Join(dir, "e-vocals.flac")),
		"stem-other.flac":  lavfi(t, aeval(exprB500, engDur), filepath.Join(dir, "e-other.flac")),
		"stem-drums.flac":  lavfi(t, aeval("0", engDur), filepath.Join(dir, "e-drums.flac")),
		"stem-bass.flac":   lavfi(t, aeval("0", engDur), filepath.Join(dir, "e-bass.flac")),
		"stem-kick.flac":   lavfi(t, aeval("0.3*sin(2*PI*2000*t)", engDur), filepath.Join(dir, "e-kick.flac")),
	}
	put(f, parentID, pf)
	return newEngFake(t, f), decodeFile(t, pf["audio.flac"])
}

// engChain — цепочка движка как из JSON (числа — float64): JCM2000 + кабинет.
func engChain() []map[string]any {
	return []map[string]any{
		{"type": "amp", "model": "JCM2000.nam", "input_db": -6.0},
		{"type": "cab", "cutoff_hz": 7000.0},
	}
}

func engSpec(stems []string, from, to, db float64) SectionSpec {
	return SectionSpec{ChildID: 0, From: from, To: to, Stems: stems, Db: db, Engine: engChain()}
}

func engRun(t *testing.T, f *engFake, specs ...SectionSpec) []float32 {
	t.Helper()
	if _, err := RebuildSections(context.Background(), f, parentID, specs); err != nil {
		t.Fatalf("RebuildSections: %v", err)
	}
	return uploadedOnly(t, f.secFake)
}

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

// --- ТК1: запрос к воркеру, скачивание куска, вставки +1 и −1 ---

// ТК1 (запрос): Engine на other в окне 2–6 → один ApplyFx у родителя с
// source=other, output=solo, preview, from = From − f, to = To, fade = f, цепочка как есть.
func TestRebuildSectionsEngineRequest(t *testing.T) {
	f, _ := engSetup(t)
	engRun(t, f, engSpec([]string{"other"}, 2, 6, 0))

	if len(f.calls) != 1 {
		t.Fatalf("ApplyFx вызван %d раз, want 1", len(f.calls))
	}
	c := f.calls[0]
	if c.id != parentID {
		t.Errorf("ApplyFx у джобы %d, want %d (родитель)", c.id, parentID)
	}
	r := c.req
	if r.Source != "other" || r.Output != "solo" || !r.Preview {
		t.Errorf("FxRequest source=%q output=%q preview=%v, want other/solo/true", r.Source, r.Output, r.Preview)
	}
	if !r.Pad {
		t.Errorf("FxRequest.Pad = false, want true (кусок — файлом от начала трека, вставка без задержки)")
	}
	if r.From == nil || !near(*r.From, 2-engFade, 1e-9) {
		t.Errorf("FxRequest.From = %v, want %.2f (From − фейд)", deref(r.From), 2-engFade)
	}
	if r.To == nil || !near(*r.To, 6, 1e-9) {
		t.Errorf("FxRequest.To = %v, want 6 (To)", deref(r.To))
	}
	if !near(r.Fade, engFade, 1e-9) {
		t.Errorf("FxRequest.Fade = %v, want %v", r.Fade, engFade)
	}
	if !reflect.DeepEqual(r.Chain, engChain()) {
		t.Errorf("FxRequest.Chain = %v, want как в записи %v", r.Chain, engChain())
	}
	want := key(parentID, "preview-fx-00000001.flac")
	if !contains(f.fetched, want) {
		t.Errorf("обработанный кусок не скачан: fetched %v, want %s", f.fetched, want)
	}
}

// ТК1 (звук): +1 — кусок целиком с From − f (с начала куска, с хвостом, без фейдов);
// −1 — исходная дорожка в [From − f, To + f) с линейными фейдами f; вне окна и
// хвоста — исходный трек; длина трека та же; чужая дорожка (vocals) не тронута.
func TestRebuildSectionsEngineInserts(t *testing.T) {
	f, base := engSetup(t)
	out := engRun(t, f, engSpec([]string{"other"}, 2, 6, 0))
	from := 2 - engFade
	chunkEnd := 6 + engFade + engTail // from + длина куска

	if d := float64(len(out)) / sr; math.Abs(d-engDur) > 0.05 {
		t.Errorf("длина выхода %.3f с, want %.0f ± 0.05 (длина трека)", d, engDur)
	}
	// до окна — исходный трек, обработанного куска нет
	if r := relDiffDb(out, base, 0, from-0.01); r > -40 {
		t.Errorf("до From − f разница с исходным %.1f дБ, want ≤ −40", r)
	}
	// +1: кусок встаёт с from и читается с начала (SkipSec 0)
	if a := toneAmp(out, 1000, from, from+0.05); !near(a, engAmp, 0.03) {
		t.Errorf("начало куска (1000 Гц) на %.2f–%.2f с: %.4f, want ≈ %.2f (кусок с From − f, с начала)", from, from+0.05, a, engAmp)
	}
	if a := toneAmp(out, 1000, 2.0, 2.9); !near(a, engAmp, 0.02) {
		t.Errorf("1000 Гц на 2.0–2.9 с: %.4f, want ≈ %.2f", a, engAmp)
	}
	if a := toneAmp(out, 1500, 3.0, 7.0); !near(a, engAmp, 0.02) {
		t.Errorf("1500 Гц на 3–7 с (с хвостом после To): %.4f, want ≈ %.2f (+1 без фейда, хвост звучит)", a, engAmp)
	}
	// −1: исходная дорожка other вычтена в окне, края — линейные фейды f
	if a := toneAmp(out, 500, 2.5, 5.5); a > 0.01 {
		t.Errorf("500 Гц (other) внутри окна %.4f, want ≈ 0 (исходная дорожка вычтена)", a)
	}
	if a := toneAmp(out, 500, from, 2.0); !near(a, ampTone/2, 0.03) {
		t.Errorf("500 Гц на фейде входа %.2f–2.00 с: %.4f, want ≈ %.2f (линейный фейд f)", from, a, ampTone/2)
	}
	if a := toneAmp(out, 500, 6.0, 6+engFade); !near(a, ampTone/2, 0.03) {
		t.Errorf("500 Гц на фейде выхода 6.00–%.2f с: %.4f, want ≈ %.2f (линейный фейд f после To)", 6+engFade, a, ampTone/2)
	}
	if a := toneAmp(out, 500, 6.1, 9.9); !near(a, ampTone, 0.02) {
		t.Errorf("500 Гц после To + f: %.4f, want ≈ %.2f (дорожка вернулась)", a, ampTone)
	}
	// чужая дорожка не тронута нигде
	for _, w := range [][2]float64{{0, 2}, {2, 6}, {6, 10}} {
		if a := toneAmp(out, 3000, w[0], w[1]); !near(a, ampTone, 0.02) {
			t.Errorf("3000 Гц (vocals) на %.0f–%.0f с: %.4f, want ≈ %.2f (эффект только на other)", w[0], w[1], a, ampTone)
		}
	}
	// после конца куска — исходный трек
	if r := relDiffDb(out, base, chunkEnd+0.01, engDur); r > -40 {
		t.Errorf("после конца куска (%.2f с) разница с исходным %.1f дБ, want ≤ −40", chunkEnd, r)
	}
}

// Краевой: From = 0 — начало окна не уходит в минус: from = 0.
func TestRebuildSectionsEngineFromZero(t *testing.T) {
	f, _ := engSetup(t)
	engRun(t, f, engSpec([]string{"other"}, 0, 4, 0))
	if len(f.calls) != 1 || f.calls[0].req.From == nil {
		t.Fatalf("ApplyFx: %+v, want 1 вызов с From", f.calls)
	}
	if fr := *f.calls[0].req.From; fr != 0 {
		t.Errorf("From = 0: FxRequest.From = %v, want 0 (max(0, From − f))", fr)
	}
}

// --- ТК2: To ≤ 0 — до конца трека ---

func TestRebuildSectionsEngineToZeroIsTrackEnd(t *testing.T) {
	for _, to := range []float64{0, -1} {
		t.Run(fmt.Sprintf("To=%g", to), func(t *testing.T) {
			f, base := engSetup(t)
			out := engRun(t, f, engSpec([]string{"other"}, 2, to, 0))
			if len(f.calls) != 1 || f.calls[0].req.To == nil {
				t.Fatalf("ApplyFx: %+v, want 1 вызов с To", f.calls)
			}
			// длительность трека вниз до сетки 10 мс: 10 с → ровно 10
			if got := *f.calls[0].req.To; !near(got, engDur, 1e-6) {
				t.Errorf("FxRequest.To = %v, want floor(длительность трека / 0,01)·0,01 = %.2f", got, engDur)
			}
			if d := float64(len(out)) / sr; math.Abs(d-engDur) > 0.05 {
				t.Errorf("длина выхода %.3f с, want %.0f ± 0.05 (кусок длиннее остатка трека — не удлиняет)", d, engDur)
			}
			if a := toneAmp(out, 500, 2.5, 9.5); a > 0.01 {
				t.Errorf("500 Гц (other) на 2.5–9.5 с: %.4f, want ≈ 0 (эффект до конца трека)", a)
			}
			if r := relDiffDb(out, base, 0, 1.9); r > -40 {
				t.Errorf("до From разница с исходным %.1f дБ, want ≤ −40", r)
			}
		})
	}
}

// --- ТК3: несколько дорожек; неизменяемые и отсутствующие пропускаются ---

func TestRebuildSectionsEngineTwoStemsSkipsImmutable(t *testing.T) {
	f, _ := engSetup(t)
	// kick есть у родителя, но не в списке изменяемых — пропускается
	out := engRun(t, f, engSpec([]string{"vocals", "other", "kick"}, 2, 6, 0))

	var srcs []string
	for _, c := range f.calls {
		srcs = append(srcs, c.req.Source)
		if c.req.From == nil || !near(*c.req.From, 2-engFade, 1e-9) || c.req.To == nil || !near(*c.req.To, 6, 1e-9) ||
			!near(c.req.Fade, engFade, 1e-9) || c.req.Output != "solo" || !c.req.Preview || !c.req.Pad {
			t.Errorf("ApplyFx(%s): from=%v to=%v fade=%v output=%q preview=%v pad=%v, want 1.95/6/0.05/solo/true/true",
				c.req.Source, deref(c.req.From), deref(c.req.To), c.req.Fade, c.req.Output, c.req.Preview, c.req.Pad)
		}
	}
	sort.Strings(srcs)
	if !reflect.DeepEqual(srcs, []string{"other", "vocals"}) {
		t.Errorf("ApplyFx по дорожкам %v, want [other vocals] (kick неизменяемая)", srcs)
	}
	// обе исходные дорожки вычтены в окне, вне окна на месте
	for _, hz := range []float64{500, 3000} {
		if a := toneAmp(out, hz, 2.5, 5.5); a > 0.01 {
			t.Errorf("%.0f Гц в окне %.4f, want ≈ 0", hz, a)
		}
		if a := toneAmp(out, hz, 0, 1.9); !near(a, ampTone, 0.02) {
			t.Errorf("%.0f Гц до окна %.4f, want ≈ %.2f", hz, a, ampTone)
		}
	}
}

// --- ТК4: ошибка воркера — ошибка пересборки с причиной, ничего не загружено ---

func TestRebuildSectionsEngineWorkerErrorFails(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"движок выключен", &yue.StatusError{Code: 503, Msg: "fx engine disabled"}},
		{"цепочка неверна", &yue.StatusError{Code: 422, Msg: "unknown block type: fuzz"}},
		{"старый воркер", errors.New("404 not found: /jobs/1/fx")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := engSetup(t)
			f.err = tc.err
			_, err := RebuildSections(context.Background(), f, parentID,
				[]SectionSpec{engSpec([]string{"other"}, 2, 6, 0)})
			if err == nil {
				t.Fatal("ApplyFx вернул ошибку: want ошибку пересборки, got nil (молча без эффекта)")
			}
			if want := tc.err.Error(); !strings.Contains(err.Error(), want) {
				t.Errorf("ошибка %q не содержит причину воркера %q", err, want)
			}
			if len(f.uploads) != 0 {
				t.Errorf("при ошибке загружено: %v", keys(f.uploads))
			}
		})
	}
}

// Краевой (контракт): кусок не скачался — ошибка, ничего не загружено.
func TestRebuildSectionsEngineChunkNotFetchedFails(t *testing.T) {
	f, _ := engSetup(t)
	f.lost = true
	_, err := RebuildSections(context.Background(), f, parentID, []SectionSpec{engSpec([]string{"other"}, 2, 6, 0)})
	if err == nil {
		t.Fatal("кусок не скачался: want ошибку, got nil")
	}
	if len(f.uploads) != 0 {
		t.Errorf("при ошибке загружено: %v", keys(f.uploads))
	}
}

// --- ТК5: смешанный список — вклейка рендера + ffmpeg-эффект + движок ---

func TestRebuildSectionsEngineMixedWithInsertAndChain(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()
	sf := newSecFake()
	// трек 12 с: other 440, щелчки drums, vocals 3000
	pf := map[string]string{
		"audio.flac":       lavfi(t, aeval(expr440+"+"+exprClicks+"+"+exprA3000, trackDur), filepath.Join(dir, "m-audio.flac")),
		"stem-drums.flac":  lavfi(t, aeval(exprClicks, trackDur), filepath.Join(dir, "m-drums.flac")),
		"stem-other.flac":  lavfi(t, aeval(expr440, trackDur), filepath.Join(dir, "m-other.flac")),
		"stem-vocals.flac": lavfi(t, aeval(exprA3000, trackDur), filepath.Join(dir, "m-vocals.flac")),
		"stem-bass.flac":   lavfi(t, aeval("0", trackDur), filepath.Join(dir, "m-bass.flac")),
	}
	put(sf, parentID, pf)
	put(sf, 7, childFiles(t, dir, 7, 8, exprClicks))
	f := newEngFake(t, sf)

	insert := spec7([]string{"other"}, 0) // other 440 → 660 в 4–8 с
	chain := SectionSpec{ChildID: 0, From: 1, To: 3, Stems: []string{"vocals"}, Chain: "dewhistle", Params: notch3000()}
	engine := engSpec([]string{"vocals"}, 9, 11, 0)
	out := engRun(t, f, insert, chain, engine)

	// ffmpeg-эффект: 3000 Гц вырезан в 1–3 с
	if d := db(toneAmp(out, 3000, 1.5, 2.5)) - db(ampTone); d > -15 {
		t.Errorf("ffmpeg-эффект: 3000 Гц в 1–3 с изменился на %+.1f дБ, want ≤ −15", d)
	}
	// вклейка: в 4–8 с other ребёнка (660) вместо родителя (440)
	if a := toneAmp(out, 660, 4.5, 7.5); a < 0.15 {
		t.Errorf("вклейка: 660 Гц в окне %.4f, want ≥ 0.15", a)
	}
	if a := toneAmp(out, 440, 4.5, 7.5); a > 0.03 {
		t.Errorf("вклейка: 440 Гц в окне %.4f, want < 0.03", a)
	}
	// движок: vocals вычтен в 9–11 с, обработанный кусок звучит
	if len(f.calls) != 1 || f.calls[0].req.Source != "vocals" {
		t.Fatalf("ApplyFx: %+v, want 1 вызов для vocals", f.calls)
	}
	if a := toneAmp(out, 3000, 9.5, 10.5); a > 0.01 {
		t.Errorf("движок: 3000 Гц (vocals) в 9–11 с %.4f, want ≈ 0", a)
	}
	if a := toneAmp(out, 1000, 9.0, 9.9); !near(a, engAmp, 0.02) {
		t.Errorf("движок: обработанный кусок (1000 Гц) на 9.0–9.9 с %.4f, want ≈ %.2f", a, engAmp)
	}
	// между записями голос на месте
	if a := toneAmp(out, 3000, 4, 8); !near(a, ampTone, 0.02) {
		t.Errorf("3000 Гц на 4–8 с %.4f, want ≈ %.2f (голос вне окон эффектов не тронут)", a, ampTone)
	}
}

// --- ТК6: Db записи — гейн обработанного куска, вычитаемая дорожка −1 ---

func TestRebuildSectionsEngineDbGainsChunkOnly(t *testing.T) {
	level := map[float64]float64{}
	for _, d := range []float64{0, 3, -6} {
		f, _ := engSetup(t)
		out := engRun(t, f, engSpec([]string{"other"}, 2, 6, d))
		level[d] = toneAmp(out, 1500, 3.0, 7.0)
		if a := toneAmp(out, 500, 2.5, 5.5); a > 0.01 {
			t.Errorf("Db=%+g: 500 Гц (other) в окне %.4f, want ≈ 0 (вычитается исходная дорожка ×1, без Db)", d, a)
		}
	}
	if !near(level[0], engAmp, 0.02) {
		t.Fatalf("Db=0: кусок %.4f, want ≈ %.2f (без выравнивания RMS)", level[0], engAmp)
	}
	for _, d := range []float64{3, -6} {
		if got := db(level[d]) - db(level[0]); !near(got, d, 0.3) {
			t.Errorf("Db=%+g: кусок громче на %+.2f дБ, want %+g ± 0.3 (гейн 10^(Db/20))", d, got, d)
		}
	}
}

// --- поля из JSON и подпись варианта ---

// Поле приходит из фронта/MCP по JSON под именем engine — как есть.
func TestSectionSpecEngineJSON(t *testing.T) {
	var sp SectionSpec
	err := json.Unmarshal([]byte(`{"child_id":0,"from":20,"to":35,"stems":["other"],
		"engine":[{"type":"amp","model":"JCM2000.nam","input_db":-6},{"type":"cab","cutoff_hz":7000}]}`), &sp)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sp.Engine, engChain()) {
		t.Errorf("engine → Engine = %v, want %v", sp.Engine, engChain())
	}
}

// Контракт: подпись варианта для записи движка — «Движок: <типы блоков через →> · <дорожки>».
func TestRebuildSectionsEngineLabel(t *testing.T) {
	f, _ := engSetup(t)
	engRun(t, f, engSpec([]string{"other"}, 2, 6, 0))
	if len(f.labels) != 1 {
		t.Fatalf("подписей %d, want 1", len(f.labels))
	}
	for _, l := range f.labels {
		if !strings.Contains(l, "Движок") || !strings.Contains(l, "amp → cab") {
			t.Errorf("подпись %q, want «Движок: amp → cab · …»", l)
		}
	}
}

func deref(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

// --- Регрессия ревью (условие 2 «без сдвига и без щелчков»): некруглые границы окна ---
//
// Воркер с тождественной цепочкой возвращает ровно то, что получил на вход превью
// (контракт воркера): seg[k] = дорожка[a+k]·w(k), a = round(from·sr), b = round(to·sr),
// F = round(fade·sr), длина min(b+F, n) − a, w — линейный рост F отсчётов от a и
// спад F отсчётов от b; from/to/fade — те, что пришли в запросе; sr — частота дорожки.
// Тогда +1 (кусок) и −1 (та же дорожка в том же окне с теми же фейдами) взаимно
// уничтожаются, и пересобранный трек обязан совпасть с исходным — в окне, на краях и
// вне окна. Остаток вместо тишины значит, что кусок и вычитаемая дорожка разъехались
// на отсчёты (сдвиг) — на слух это гребёнка/щелчки на краях.
//
// Сигнал — белый шум 44,1 кГц: на нём сдвиг даже на один отсчёт даёт остаток ≈ 0 дБ,
// а тон с целым числом периодов мог бы его спрятать.

const idSR = hiSR // частота дорожек этого теста (44,1 кГц, как у настоящих стемов)

// idFake — воркер с тождественной цепочкой движка.
type idFake struct {
	*secFake
	t     *testing.T
	dir   string
	calls []yue.FxRequest
}

func (f *idFake) WorkerConfig(context.Context) (map[string]any, error) {
	return map[string]any{"fx_preview": true}, nil
}

func (f *idFake) ApplyFx(_ context.Context, id int64, req yue.FxRequest) (*yue.DspVariant, error) {
	f.t.Helper()
	f.calls = append(f.calls, req)
	if req.From == nil || req.To == nil {
		f.t.Fatalf("ApplyFx без окна: from=%v to=%v", req.From, req.To)
	}
	src, ok := f.files[key(id, "stem-"+req.Source+".flac")]
	if !ok {
		f.t.Fatalf("ApplyFx: у джобы %d нет дорожки %s", id, req.Source)
	}
	part := decodeAt(f.t, src, idSR)
	n := len(part)
	a := int(math.Round(*req.From * idSR))
	b := int(math.Round(*req.To * idSR))
	F := int(math.Round(req.Fade * idSR))
	stop := b + F
	if stop > n {
		stop = n
	}
	if a < 0 || a >= stop {
		f.t.Fatalf("ApplyFx: пустое окно a=%d stop=%d (from=%v to=%v fade=%v)", a, stop, *req.From, *req.To, req.Fade)
	}
	seg := make([]float32, stop-a)
	for k := range seg {
		w := 1.0
		if F > 0 {
			w = math.Min(1, float64(k)/float64(F)) * math.Min(1, float64(b+F-(a+k))/float64(F))
		}
		seg[k] = float32(float64(part[a+k]) * w)
	}
	name := fmt.Sprintf("preview-fx-id-%02d.flac", len(f.calls))
	return workerFile(f.t, f.files, f.dir, id, name, seg, *req.From, req.Pad, idSR), nil
}

// decodeAt — декодировать файл в моно float32 с частотой rate (без пересэмплирования,
// если файл уже в ней).
func decodeAt(t *testing.T, path string, rate int) []float32 {
	t.Helper()
	raw, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error",
		"-i", path, "-f", "f32le", "-ac", "1", "-ar", strconv.Itoa(rate), "-").Output()
	if err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	s := make([]float32, len(raw)/4)
	for i := range s {
		s[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
	}
	return s
}

// writeFlac24 — записать отсчёты FLAC 24 бит (как пишет воркер).
func writeFlac24(t *testing.T, s []float32, rate int, path string) {
	t.Helper()
	raw := make([]byte, 4*len(s))
	for i, v := range s {
		binary.LittleEndian.PutUint32(raw[i*4:], math.Float32bits(v))
	}
	cmd := exec.Command("ffmpeg", "-y", "-hide_banner", "-loglevel", "error",
		"-f", "f32le", "-ar", strconv.Itoa(rate), "-ac", "1", "-i", "-",
		"-c:a", "flac", "-sample_fmt", "s32", "-bits_per_raw_sample", "24", path)
	cmd.Stdin = bytes.NewReader(raw)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("write flac: %v %s", err, out)
	}
}

// idSetup — родитель длиной dur: other = белый шум 0,3 (44,1 кГц), остальные дорожки —
// тишина, трек = other. Возвращает фейк и исходный трек (44,1 кГц).
func idSetup(t *testing.T, dur float64) (*idFake, []float32) {
	t.Helper()
	needFFmpeg(t)
	dir := t.TempDir()
	p := func(n string) string { return filepath.Join(dir, n) }
	noise := fmt.Sprintf("anoisesrc=d=%g:c=white:r=%d:a=0.3:seed=7", dur, idSR)
	other := lavfiHi(t, noise, p("id-other.flac"))
	silent := lavfiHi(t, aevalHi("0", dur), p("id-silent.flac"))
	sf := newSecFake()
	put(sf, parentID, map[string]string{
		"audio.flac":       other,
		"stem-other.flac":  other,
		"stem-drums.flac":  silent,
		"stem-bass.flac":   silent,
		"stem-vocals.flac": silent,
	})
	return &idFake{secFake: sf, t: t, dir: t.TempDir()}, decodeAt(t, other, idSR)
}

// residualDb — остаток (out − base) на [from, to] относительно уровня base, дБ (44,1 кГц).
func residualDb(out, base []float32, from, to float64) float64 {
	a, b := int(from*idSR), int(to*idSR)
	if a < 0 {
		a = 0
	}
	if b > len(base) {
		b = len(base)
	}
	if b > len(out) {
		b = len(out)
	}
	if a >= b {
		return math.Inf(1)
	}
	var e, eb float64
	for i := a; i < b; i++ {
		d := float64(out[i]) - float64(base[i])
		e += d * d
		eb += float64(base[i]) * float64(base[i])
	}
	if e == 0 {
		return -200
	}
	return 10 * math.Log10(e/eb)
}

var idCases = []struct {
	name          string
	dur, from, to float64
}{
	{"круглые 20–35", 40, 20, 35},
	{"некруглые 20.1234567–33.3333333", 40, 20.1234567, 33.3333333},
	{"некруглые 123.45678–139.99", 145, 123.45678, 139.99},
}

// Условие 2: тождественная цепочка движка на некруглом окне — трек после пересборки
// совпадает с исходным (остаток ≤ −60 дБ) целиком, в окне и на обоих краях.
func TestRebuildSectionsEngineIdentityNoShift(t *testing.T) {
	const maxDb = -60.0
	for _, tc := range idCases {
		t.Run(tc.name, func(t *testing.T) {
			f, base := idSetup(t, tc.dur)
			if _, err := RebuildSections(context.Background(), f, parentID,
				[]SectionSpec{engSpec([]string{"other"}, tc.from, tc.to, 0)}); err != nil {
				t.Fatalf("RebuildSections: %v", err)
			}
			if len(f.uploads) != 1 {
				t.Fatalf("загрузок %d, want 1", len(f.uploads))
			}
			var out []float32
			for _, data := range f.uploads {
				out = decodeHi(t, data)
			}
			if d := float64(len(out)-len(base)) / idSR; math.Abs(d) > 0.05 {
				t.Errorf("длина выхода отличается от трека на %.3f с", d)
			}
			for _, w := range []struct {
				what     string
				from, to float64
			}{
				{"весь трек", 0, tc.dur},
				{"окно", tc.from - engFade, tc.to + engFade},
				{"край входа (фейд)", tc.from - engFade - 0.01, tc.from + 0.01},
				{"край выхода (фейд)", tc.to - 0.01, tc.to + engFade + 0.01},
				{"середина окна", tc.from + 0.5, tc.to - 0.5},
			} {
				if r := residualDb(out, base, w.from, w.to); r > maxDb {
					t.Errorf("%s %.4f–%.4f с: остаток %.1f дБ, want ≤ %.0f (тождественная цепочка — трек без изменений; кусок и вычитаемая дорожка разъехались)",
						w.what, w.from, w.to, r, maxDb)
				}
			}
		})
	}
}

// --- Условие 2 (по кросс-ревью r1): окно запроса — на сетке 10 мс ---
//
// При 44,1 и 48 кГц время, кратное 10 мс, — целое число сэмплов. from — округление
// From − f к ближайшим 10 мс (не меньше 0), to — To вниз до 10 мс. Как именно
// округляется ничья (ровно 5 мс), карточкой не задано — тест допускает обе соседние
// точки сетки.

// onGrid10ms — x кратно 0,01 с (с запасом на погрешность float).
func onGrid10ms(x float64) bool {
	c := x * 100
	return math.Abs(c-math.Round(c)) <= 1e-6
}

// checkGrid — from/to/fade запроса: на сетке 10 мс, from — ближайшая к From − f
// точка сетки (≥ 0), to — To вниз (не больше To, меньше чем на 10 мс).
func checkGrid(t *testing.T, r yue.FxRequest, specFrom, specTo float64) {
	t.Helper()
	if r.From == nil || r.To == nil {
		t.Fatalf("FxRequest без окна: from=%v to=%v", deref(r.From), deref(r.To))
	}
	from, to := *r.From, *r.To
	for _, v := range []struct {
		name string
		x    float64
	}{{"From", from}, {"To", to}, {"Fade", r.Fade}} {
		if !onGrid10ms(v.x) {
			t.Errorf("FxRequest.%s = %.9f с — не на сетке 10 мс", v.name, v.x)
		}
	}
	want := math.Max(0, specFrom-engFade)
	if from < 0 || math.Abs(from-want) > 0.005+1e-9 {
		t.Errorf("FxRequest.From = %.9f, want ближайшую к %.6f точку сетки 10 мс (± 5 мс, ≥ 0)", from, want)
	}
	if to > specTo+1e-9 || specTo-to >= 0.01-1e-9 {
		t.Errorf("FxRequest.To = %.9f, want %.6f вниз до 10 мс (to ≤ To < to + 0,01)", to, specTo)
	}
	if !near(r.Fade, engFade, 1e-9) {
		t.Errorf("FxRequest.Fade = %v, want %v", r.Fade, engFade)
	}
}

var gridCases = []struct {
	name          string
	dur, from, to float64
}{
	{"20.1234567–33.3333333", 40, 20.1234567, 33.3333333},
	{"123.45678–139.99", 145, 123.45678, 139.99},
	{"20.007–33.333", 40, 20.007, 33.333},
	{"1.995–7.4567", 10, 1.995, 7.4567},
	{"123.407–139.993", 145, 123.407, 139.993},
	{"0.03–5.009 (from < f → 0)", 10, 0.03, 5.009},
}

func TestRebuildSectionsEngineWindowOn10msGrid(t *testing.T) {
	for _, tc := range gridCases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := idSetup(t, tc.dur)
			if _, err := RebuildSections(context.Background(), f, parentID,
				[]SectionSpec{engSpec([]string{"other"}, tc.from, tc.to, 0)}); err != nil {
				t.Fatalf("RebuildSections: %v", err)
			}
			if len(f.calls) != 1 {
				t.Fatalf("ApplyFx вызван %d раз, want 1", len(f.calls))
			}
			checkGrid(t, f.calls[0], tc.from, tc.to)
		})
	}
}

// --- Условие 2, главный: вычитаемая −1 совпадает с базой по сэмплам ---
//
// Цепочка «заглушить»: воркер возвращает тишину нужной длины (b + F − a сэмплов).
// Тогда +1 ничего не добавляет, и в окне [From, To] (кроме краёв-фейдов) от дорожки
// other в треке не должно остаться ничего: out = base − other. Если −1 стоит хоть на
// сэмпл мимо базы, остаток белого шума ≈ 0 дБ относительно other. Тождественная
// цепочка этого не ловит: кусок и −1 сдвигаются вместе и взаимно гасятся. Место самого
// куска (+1) этот тест не видит — его держит TestRebuildSectionsEngineChunkOnWorkerSamples.
//
// База 44,1 кГц = other (белый шум) + vocals (другой белый шум); остаток считается
// относительно уровня other.

type muteEngFake struct {
	*secFake
	t     *testing.T
	dir   string
	calls []yue.FxRequest
}

func (f *muteEngFake) WorkerConfig(context.Context) (map[string]any, error) {
	return map[string]any{"fx_preview": true}, nil
}

func (f *muteEngFake) ApplyFx(_ context.Context, id int64, req yue.FxRequest) (*yue.DspVariant, error) {
	f.t.Helper()
	f.calls = append(f.calls, req)
	if req.From == nil || req.To == nil {
		f.t.Fatalf("ApplyFx без окна: from=%v to=%v", req.From, req.To)
	}
	src, ok := f.files[key(id, "stem-"+req.Source+".flac")]
	if !ok {
		f.t.Fatalf("ApplyFx: у джобы %d нет дорожки %s", id, req.Source)
	}
	n := len(decodeAt(f.t, src, idSR))
	a := int(math.Round(*req.From * idSR))
	stop := int(math.Round(*req.To*idSR)) + int(math.Round(req.Fade*idSR))
	if stop > n {
		stop = n
	}
	if a < 0 || a >= stop {
		f.t.Fatalf("ApplyFx: пустое окно a=%d stop=%d", a, stop)
	}
	name := fmt.Sprintf("preview-fx-%08x.flac", len(f.calls))
	return workerFile(f.t, f.files, f.dir, id, name, make([]float32, stop-a), *req.From, req.Pad, idSR), nil
}

// muteSetup — родитель длиной dur: other и vocals — разные белые шумы 0,3 (44,1 кГц),
// drums/bass — тишина, трек = other + vocals. Возвращает фейк, трек и дорожку other.
func muteSetup(t *testing.T, dur float64) (*muteEngFake, []float32, []float32) {
	t.Helper()
	needFFmpeg(t)
	dir := t.TempDir()
	p := func(n string) string { return filepath.Join(dir, n) }
	noise := func(seed int) string {
		return fmt.Sprintf("anoisesrc=d=%g:c=white:r=%d:a=0.3:seed=%d", dur, idSR, seed)
	}
	otherP := lavfiHi(t, noise(7), p("m-other.flac"))
	vocalsP := lavfiHi(t, noise(11), p("m-vocals.flac"))
	silent := lavfiHi(t, aevalHi("0", dur), p("m-silent.flac"))
	other, vocals := decodeAt(t, otherP, idSR), decodeAt(t, vocalsP, idSR)
	if len(other) != len(vocals) {
		t.Fatalf("длины дорожек %d/%d", len(other), len(vocals))
	}
	mix := make([]float32, len(other))
	for i := range mix {
		mix[i] = other[i] + vocals[i]
	}
	baseP := p("m-audio.flac")
	writeFlac24(t, mix, idSR, baseP)
	sf := newSecFake()
	put(sf, parentID, map[string]string{
		"audio.flac":       baseP,
		"stem-other.flac":  otherP,
		"stem-vocals.flac": vocalsP,
		"stem-drums.flac":  silent,
		"stem-bass.flac":   silent,
	})
	return &muteEngFake{secFake: sf, t: t, dir: t.TempDir()}, decodeAt(t, baseP, idSR), other
}

// relResidualDb — энергия (out − (base − sub)) на [from, to] относительно энергии ref, дБ.
// sub = nil — остаток out − base.
func relResidualDb(out, base, sub, ref []float32, from, to float64) float64 {
	a, b := int(from*idSR), int(to*idSR)
	if a < 0 {
		a = 0
	}
	for _, s := range [][]float32{out, base, ref} {
		if b > len(s) {
			b = len(s)
		}
	}
	if a >= b {
		return math.Inf(1)
	}
	var e, er float64
	for i := a; i < b; i++ {
		want := float64(base[i])
		if sub != nil {
			want -= float64(sub[i])
		}
		d := float64(out[i]) - want
		e += d * d
		er += float64(ref[i]) * float64(ref[i])
	}
	if e == 0 {
		return -200
	}
	return 10 * math.Log10(e/er)
}

func TestRebuildSectionsEngineMuteChainRemovesStemExactly(t *testing.T) {
	const maxDb = -60.0
	for _, tc := range []struct {
		name          string
		dur, from, to float64
	}{
		{"20.007–33.333", 40, 20.007, 33.333},
		{"1.995–7.4567", 10, 1.995, 7.4567},
		{"123.407–139.993", 145, 123.407, 139.993},
		{"20.1234567–33.3333333", 40, 20.1234567, 33.3333333},
		// условие 10: окна, на которых ошибается adelay ffmpeg (123,2 с при 44,1 кГц →
		// 5 433 119 сэмплов вместо 5 433 120): from = 123,2 / 123,25 / ≈123,41, to = 123,2 / 123,25
		{"123.25–140 (from − f = 123.2)", 145, 123.25, 140},
		{"123.3–140 (from − f = 123.25)", 145, 123.3, 140},
		{"100–123.2", 130, 100, 123.2},
		{"100–123.25", 130, 100, 123.25},
		{"123.457–140 (from − f = 123.407)", 145, 123.457, 140},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, base, other := muteSetup(t, tc.dur)
			if _, err := RebuildSections(context.Background(), f, parentID,
				[]SectionSpec{engSpec([]string{"other"}, tc.from, tc.to, 0)}); err != nil {
				t.Fatalf("RebuildSections: %v", err)
			}
			if len(f.calls) != 1 {
				t.Fatalf("ApplyFx вызван %d раз, want 1", len(f.calls))
			}
			checkGrid(t, f.calls[0], tc.from, tc.to)
			if len(f.uploads) != 1 {
				t.Fatalf("загрузок %d, want 1", len(f.uploads))
			}
			var out []float32
			for _, data := range f.uploads {
				out = decodeHi(t, data)
			}
			if d := float64(len(out)-len(base)) / idSR; math.Abs(d) > 0.05 {
				t.Errorf("длина выхода отличается от трека на %.3f с", d)
			}
			// в окне (без краёв-фейдов: сетка сдвигает их до 10 мс) — base − other
			in := [2]float64{tc.from + 0.01, tc.to - 0.01}
			if r := relResidualDb(out, base, other, other, in[0], in[1]); r > maxDb {
				t.Errorf("окно %.4f–%.4f с: остаток дорожки other %.1f дБ, want ≤ %.0f (вычитаемая −1 стоит мимо базы)",
					in[0], in[1], r, maxDb)
			}
			// вне окна и фейдов — исходный трек
			if r := relResidualDb(out, base, nil, base, 0, tc.from-engFade-0.02); r > maxDb {
				t.Errorf("до окна: отличие от трека %.1f дБ, want ≤ %.0f", r, maxDb)
			}
			if r := relResidualDb(out, base, nil, base, tc.to+engFade+0.02, tc.dur); r > maxDb {
				t.Errorf("после окна: отличие от трека %.1f дБ, want ≤ %.0f", r, maxDb)
			}
		})
	}
}

// --- Условия 2/8: старый воркер — ошибка пересборки, а не молча без эффекта ---

// /config без fx_preview: true (или /config недоступен) → ошибка до вызова ApplyFx,
// текст — про обновление воркера; ничего не загружено.
func TestRebuildSectionsEngineWorkerWithoutPreviewFails(t *testing.T) {
	for _, tc := range []struct {
		name     string
		cfg      map[string]any
		cfgErr   error
		wantText bool // текст про обновление воркера (для ответа /config без fx_preview)
	}{
		{"нет fx_preview", map[string]any{"ollama_url": "http://llm:11434"}, nil, true},
		{"fx_preview false", map[string]any{"fx_preview": false}, nil, true},
		{"пустой /config", map[string]any{}, nil, true},
		{"/config недоступен", nil, errors.New("404 not found: /config"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := engSetup(t)
			f.cfg, f.cfgErr = tc.cfg, tc.cfgErr
			_, err := RebuildSections(context.Background(), f, parentID,
				[]SectionSpec{engSpec([]string{"other"}, 2, 6, 0)})
			if err == nil {
				t.Fatal("воркер без fx_preview: want ошибку пересборки, got nil")
			}
			if tc.wantText {
				low := strings.ToLower(err.Error())
				if !strings.Contains(low, "воркер") || !strings.Contains(low, "обнов") {
					t.Errorf("ошибка %q — want текст про обновление воркера", err)
				}
			}
			if len(f.calls) != 0 {
				t.Errorf("ApplyFx вызван %d раз, want 0 (проверка /config — до вызова)", len(f.calls))
			}
			if len(f.uploads) != 0 {
				t.Errorf("при ошибке загружено: %v", keys(f.uploads))
			}
		})
	}
}

// Ответ ApplyFx — не превью (dsp-fx-*: воркер проигнорировал preview) → ошибка, ничего
// не загружено (файл при этом скачать можно — дело не в скачивании).
func TestRebuildSectionsEngineNonPreviewAnswerFails(t *testing.T) {
	f, _ := engSetup(t)
	f.variant = true
	_, err := RebuildSections(context.Background(), f, parentID, []SectionSpec{engSpec([]string{"other"}, 2, 6, 0)})
	if err == nil {
		t.Fatal("ApplyFx ответил dsp-fx-* вместо preview-fx-*: want ошибку, got nil")
	}
	if len(f.calls) == 0 {
		t.Error("ApplyFx не вызван — тест не дошёл до проверки ответа")
	}
	if len(f.uploads) != 0 {
		t.Errorf("при ошибке загружено: %v", keys(f.uploads))
	}
}

// Пересборка без записей движка у старого воркера не ломается (проверка /config
// нужна только перед движком).
func TestRebuildSectionsNoEngineIgnoresWorkerPreview(t *testing.T) {
	f, _ := engSetup(t)
	f.cfg = map[string]any{}
	if _, err := RebuildSections(context.Background(), f, parentID,
		[]SectionSpec{{ChildID: 0, From: 2, To: 6, Stems: []string{"other"}, Db: -100}}); err != nil {
		t.Fatalf("заглушение без движка у воркера без fx_preview: %v", err)
	}
	if len(f.uploads) != 1 {
		t.Errorf("загрузок %d, want 1", len(f.uploads))
	}
}

// --- Условие 2 / ТК2: To ≤ 0 у импортированного трека (mp3, wav) — длина по декодированию ---

func TestRebuildSectionsEngineToZeroImportedBase(t *testing.T) {
	for _, ext := range []string{"mp3", "wav"} {
		t.Run(ext, func(t *testing.T) {
			f, _ := engSetup(t)
			flac := f.files[key(parentID, "audio.flac")]
			imp := filepath.Join(t.TempDir(), "base."+ext)
			if out, err := exec.Command("ffmpeg", "-y", "-loglevel", "error", "-i", flac, imp).CombinedOutput(); err != nil {
				t.Fatalf("%v %s", err, out)
			}
			delete(f.files, key(parentID, "audio.flac"))
			f.files[key(parentID, "audio."+ext)] = imp
			length := float64(len(decodeFile(t, imp))) / sr // длина импорта по декодированию

			_, err := RebuildSections(context.Background(), f, parentID, []SectionSpec{engSpec([]string{"other"}, 2, 0, 0)})
			if err != nil {
				t.Fatalf("To = 0 у трека audio.%s: %v", ext, err)
			}
			if len(f.calls) != 1 || f.calls[0].req.To == nil {
				t.Fatalf("ApplyFx: %+v, want 1 вызов с To", f.calls)
			}
			to := *f.calls[0].req.To
			if !onGrid10ms(to) || to > length+1e-6 || length-to >= 0.01+1.0/sr {
				t.Errorf("FxRequest.To = %.6f, want floor(%.6f / 0,01)·0,01 (длина audio.%s по декодированию)", to, length, ext)
			}
			if len(f.uploads) != 1 {
				t.Fatalf("загрузок %d, want 1", len(f.uploads))
			}
			// mp3 — с потерями: дорожка FLAC вычитается из mp3-базы не до нуля (у обычного
			// заглушения тот же остаток, 0,015 из 0,3), поэтому порог — 0,03 (−20 дБ)
			out := uploadedOnly(t, f.secFake)
			if a := toneAmp(out, 500, 2.5, 9.5); a > 0.03 {
				t.Errorf("500 Гц (other) на 2.5–9.5 с: %.4f, want < 0.03 (эффект до конца трека)", a)
			}
		})
	}
}

// --- Условие 2: обработанный кусок (+1) стоит в треке на тех же сэмплах, что у воркера ---
//
// Тишина проверяет только −1 против базы; кусок при ней не слышен. Здесь «воркер»
// заменяет дорожку известным сигналом Z, привязанным к своим сэмплам:
// кусок[k] = Z[a + k], a = round(from·sr) (как воркер режет вход). Тогда в окне
// [From, To] (без краёв-фейдов) трек обязан быть base − other + Z. Кусок не на своих
// сэмплах (adelay ставит from не туда, где его взял воркер) → остаток Z ≈ 0 дБ; −1 мимо
// базы → остаток other. В отличие от тождественной цепочки, совместный сдвиг куска
// и −1 здесь не гасится.

type replaceEngFake struct {
	*muteEngFake
	z []float32 // сигнал замены (44,1 кГц, длиной с трек)
}

func (f *replaceEngFake) ApplyFx(_ context.Context, id int64, req yue.FxRequest) (*yue.DspVariant, error) {
	f.t.Helper()
	f.calls = append(f.calls, req)
	if req.From == nil || req.To == nil {
		f.t.Fatalf("ApplyFx без окна: from=%v to=%v", req.From, req.To)
	}
	a := int(math.Round(*req.From * idSR))
	stop := int(math.Round(*req.To*idSR)) + int(math.Round(req.Fade*idSR))
	if stop > len(f.z) {
		stop = len(f.z)
	}
	if a < 0 || a >= stop {
		f.t.Fatalf("ApplyFx: пустое окно a=%d stop=%d", a, stop)
	}
	name := fmt.Sprintf("preview-fx-%08x.flac", len(f.calls))
	return workerFile(f.t, f.files, f.dir, id, name, f.z[a:stop], *req.From, req.Pad, idSR), nil
}

func TestRebuildSectionsEngineChunkOnWorkerSamples(t *testing.T) {
	const maxDb = -60.0
	for _, tc := range []struct {
		name          string
		dur, from, to float64
	}{
		{"20.007–33.333", 40, 20.007, 33.333},
		{"1.995–7.4567", 10, 1.995, 7.4567},
		{"123.407–139.993", 145, 123.407, 139.993},
		{"20.1234567–33.3333333", 40, 20.1234567, 33.3333333},
		{"123.45678–139.99", 145, 123.45678, 139.99},
		// условие 10: окна, на которых ошибается adelay ffmpeg (123,2 с при 44,1 кГц →
		// 5 433 119 сэмплов вместо 5 433 120): from = 123,2 / 123,25 / ≈123,41, to = 123,2 / 123,25
		{"123.25–140 (from − f = 123.2)", 145, 123.25, 140},
		{"123.3–140 (from − f = 123.25)", 145, 123.3, 140},
		{"100–123.2", 130, 100, 123.2},
		{"100–123.25", 130, 100, 123.25},
		{"123.457–140 (from − f = 123.407)", 145, 123.457, 140},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mf, base, other := muteSetup(t, tc.dur)
			zp := lavfiHi(t, fmt.Sprintf("anoisesrc=d=%g:c=white:r=%d:a=0.3:seed=23", tc.dur, idSR),
				filepath.Join(t.TempDir(), "z.flac"))
			z := decodeAt(t, zp, idSR)
			f := &replaceEngFake{muteEngFake: mf, z: z}
			if _, err := RebuildSections(context.Background(), f, parentID,
				[]SectionSpec{engSpec([]string{"other"}, tc.from, tc.to, 0)}); err != nil {
				t.Fatalf("RebuildSections: %v", err)
			}
			if len(f.uploads) != 1 {
				t.Fatalf("загрузок %d, want 1", len(f.uploads))
			}
			var out []float32
			for _, data := range f.uploads {
				out = decodeHi(t, data)
			}
			// ожидание в окне: base − other + z
			want := make([]float32, len(base))
			for i := range want {
				want[i] = base[i] - other[i]
				if i < len(z) {
					want[i] += z[i]
				}
			}
			in := [2]float64{tc.from + 0.01, tc.to - 0.01}
			if r := relResidualDb(out, want, nil, z, in[0], in[1]); r > maxDb {
				t.Errorf("окно %.4f–%.4f с: отличие от base − other + Z %.1f дБ (относительно Z), want ≤ %.0f (кусок не на сэмплах воркера или −1 мимо базы)",
					in[0], in[1], r, maxDb)
			}
		})
	}
}

// --- Условие 8 (регрессия кросс-ревью r3): воркер этапа 3 — fx_preview есть, pad нет ---
//
// Такой воркер на /config отвечает fx_preview: true, но поле pad игнорирует: отдаёт
// кусок с начала окна без тишины спереди, duration_sec = длина куска. При From > 0
// файл короче To — вклеить его «от начала трека» нельзя; пересборка обязана упасть с
// просьбой обновить воркер, а не уложить кусок мимо окна (молча без эффекта).
// При From = 0 кусок и так от начала трека — тот же воркер обязан работать.

// noPadFake — replaceEngFake, который игнорирует pad (воркер этапа 3).
type noPadFake struct{ *replaceEngFake }

func (f *noPadFake) ApplyFx(ctx context.Context, id int64, req yue.FxRequest) (*yue.DspVariant, error) {
	req.Pad = false
	return f.replaceEngFake.ApplyFx(ctx, id, req)
}

func noPadSetup(t *testing.T, dur float64) (*noPadFake, []float32, []float32, []float32) {
	t.Helper()
	mf, base, other := muteSetup(t, dur)
	zp := lavfiHi(t, fmt.Sprintf("anoisesrc=d=%g:c=white:r=%d:a=0.3:seed=29", dur, idSR),
		filepath.Join(t.TempDir(), "z.flac"))
	z := decodeAt(t, zp, idSR)
	return &noPadFake{&replaceEngFake{muteEngFake: mf, z: z}}, base, other, z
}

func TestRebuildSectionsEngineWorkerIgnoresPadFails(t *testing.T) {
	f, _, _, _ := noPadSetup(t, 40)
	_, err := RebuildSections(context.Background(), f, parentID,
		[]SectionSpec{engSpec([]string{"other"}, 20, 35, 0)})
	if err == nil {
		t.Fatal("воркер без pad (кусок без тишины спереди) на окне 20–35: want ошибку пересборки, got nil")
	}
	if len(f.calls) == 0 {
		t.Error("ApplyFx не вызван — тест не дошёл до проверки ответа")
	}
	low := strings.ToLower(err.Error())
	if !strings.Contains(low, "воркер") || !strings.Contains(low, "обнов") {
		t.Errorf("ошибка %q — want текст про обновление воркера", err)
	}
	if len(f.uploads) != 0 {
		t.Errorf("при ошибке загружено: %v", keys(f.uploads))
	}
}

// Контроль: тот же воркер без pad при From = 0 — кусок от начала трека, пересборка
// проходит, в окне звучит обработанный кусок (base − other + Z). Первые f секунд —
// фейд входа −1, его фейк не повторяет (как и в TestRebuildSectionsEngineChunkOnWorkerSamples).
func TestRebuildSectionsEngineWorkerIgnoresPadFromZeroWorks(t *testing.T) {
	f, base, other, z := noPadSetup(t, 20)
	if _, err := RebuildSections(context.Background(), f, parentID,
		[]SectionSpec{engSpec([]string{"other"}, 0, 15, 0)}); err != nil {
		t.Fatalf("воркер без pad при From = 0: %v", err)
	}
	if len(f.uploads) != 1 {
		t.Fatalf("загрузок %d, want 1", len(f.uploads))
	}
	var out []float32
	for _, data := range f.uploads {
		out = decodeHi(t, data)
	}
	want := make([]float32, len(base))
	for i := range want {
		want[i] = base[i] - other[i]
		if i < len(z) {
			want[i] += z[i]
		}
	}
	if r := relResidualDb(out, want, nil, z, engFade, 14.99); r > -60 {
		t.Errorf("окно 0,05–14,99 с (без фейда входа): отличие от base − other + Z %.1f дБ, want ≤ −60 (кусок не на месте)", r)
	}
}

package mcp

import (
	"math"
	"os/exec"
	"strings"
	"testing"

	"yue-studio/internal/yue"
)

// Тесты карточки internal-splice, условие 3 (MCP): splice {job_id, parts, crossfade?,
// title?} — склейка кусков (studio.Splice) → вариант у job_id → версия-трек
// (VariantToTrack: файл варианта, title или название по умолчанию, voice_src как у
// базы); ответ — «версия-трек #N, длина M:SS». Ошибки — без загрузки.
// Воркер — fakeService (server_test.go; FetchAudio отдаёт файл по имени для любой
// джобы); звук — синтетика ffmpeg (fxLavfi/fxDecode/fxTone из stem_fx_test.go).

// spliceServer — база 258 с явным источником голоса 243; audio.flac — 440 Гц 6 с.
func spliceServer(t *testing.T) (*Server, *fakeService) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	s, fake := newTestServer(t)
	fake.jobs = []yue.Job{
		{ID: 243, Status: "done", AudioFile: "audio.flac"},
		{ID: 258, Status: "done", AudioFile: "audio.flac", Role: "variant", ParentID: pid(243), VoiceSrc: pid(243)},
	}
	fake.fetch = map[string]string{"audio.flac": fxLavfi(t, "0.3*sin(2*PI*440*t)", 6)}
	return s, fake
}

func onlyUpload(t *testing.T, fake *fakeService) (string, []float32) {
	t.Helper()
	if len(fake.uploads) != 1 {
		t.Fatalf("загрузок %d, want 1", len(fake.uploads))
	}
	for name, data := range fake.uploads {
		return name, fxDecode(t, data)
	}
	return "", nil
}

// Аргументы из JSON (числа — float64) разобраны: куски, gain_db, crossfade; итог
// загружен к job_id и стал версией-треком с этим файлом, своим title и voice_src
// базы; в ответе — номер версии (#77 у fakeService) и длина 3+3 − 0.7 = 5.3 → 0:05.
func TestSpliceToolBuildsVersion(t *testing.T) {
	s, fake := spliceServer(t)
	out, ok := call(t, s, "splice", jsonArgs(t, `{"job_id":258,"crossfade":0.7,"title":"с проигрышем",
		"parts":[{"job_id":258,"from":0,"to":3},{"job_id":243,"from":0,"to":3,"gain_db":6}]}`))
	if !ok {
		t.Fatalf("splice: %s", out)
	}
	name, res := onlyUpload(t, fake)
	if !strings.HasPrefix(name, "dsp-splice-") || !strings.HasSuffix(name, ".flac") {
		t.Errorf("имя варианта %q, want splice-<n>.flac", name)
	}
	if d := float64(len(res)) / fxSR; math.Abs(d-5.3) > 0.05 {
		t.Errorf("длина %.3f с, want 5.3 ± 0.05 (crossfade 0.7 разобран)", d)
	}
	// кусок 2 (2.3–5.3) громче куска 1 на 6 дБ — gain_db разобран
	if d := 20*math.Log10(fxTone(res, 440, 3.5, 4.5)) - 20*math.Log10(fxTone(res, 440, 0.5, 1.5)); math.Abs(d-6) > 1 {
		t.Errorf("кусок с gain_db 6 громче на %+.2f дБ, want +6 ± 1", d)
	}
	if len(fake.promoted) != 1 {
		t.Fatalf("VariantToTrack вызван %d раз, want 1", len(fake.promoted))
	}
	want := promoteCall{jobID: 258, file: name, title: "с проигрышем", voiceSrc: 243}
	if fake.promoted[0] != want {
		t.Errorf("VariantToTrack %+v, want %+v", fake.promoted[0], want)
	}
	if !strings.Contains(out, "#77") {
		t.Errorf("в ответе нет номера версии #77: %s", out)
	}
	if !strings.Contains(out, "0:05") {
		t.Errorf("в ответе нет длины 0:05: %s", out)
	}
}

// Без title — название по умолчанию (непустое); без crossfade — переход 0.05:
// 3 + 3 − 0.05 = 5.95.
func TestSpliceToolDefaults(t *testing.T) {
	s, fake := spliceServer(t)
	out, ok := call(t, s, "splice", jsonArgs(t, `{"job_id":258,
		"parts":[{"job_id":258,"from":0,"to":3},{"job_id":258,"from":3,"to":0}]}`))
	if !ok {
		t.Fatalf("splice: %s", out)
	}
	_, res := onlyUpload(t, fake)
	if d := float64(len(res)) / fxSR; math.Abs(d-5.95) > 0.05 {
		t.Errorf("длина %.3f с, want 5.95 ± 0.05 (to 0 — до конца, переход 0.05)", d)
	}
	if len(fake.promoted) != 1 || strings.TrimSpace(fake.promoted[0].title) == "" {
		t.Errorf("VariantToTrack %+v, want 1 вызов с непустым названием по умолчанию", fake.promoted)
	}
}

// Ошибки: parts пустой / не массив / кусок to ≤ from / нет аудио у джобы — ошибка,
// ничего не загружено и версия не создана.
func TestSpliceToolErrorsWithoutUpload(t *testing.T) {
	cases := []struct{ name, args string }{
		{"parts пустой", `{"job_id":258,"parts":[]}`},
		{"parts нет", `{"job_id":258}`},
		{"parts не массив", `{"job_id":258,"parts":{"job_id":258,"from":0,"to":3}}`},
		{"to ≤ from", `{"job_id":258,"parts":[{"job_id":258,"from":3,"to":1}]}`},
	}
	for _, tc := range cases {
		s, fake := spliceServer(t)
		out, ok := call(t, s, "splice", jsonArgs(t, tc.args))
		if ok {
			t.Errorf("%s: want ошибку, ответ: %s", tc.name, out)
		}
		if len(fake.uploads) != 0 || len(fake.promoted) != 0 {
			t.Errorf("%s: при ошибке загружено %d, версий %d", tc.name, len(fake.uploads), len(fake.promoted))
		}
	}

	// неизвестная джоба: аудио не скачивается
	s, fake := spliceServer(t)
	fake.fetch = map[string]string{}
	if out, ok := call(t, s, "splice", jsonArgs(t, `{"job_id":258,"parts":[{"job_id":999,"from":0,"to":3}]}`)); ok {
		t.Errorf("неизвестная джоба: want ошибку, ответ: %s", out)
	}
	if len(fake.uploads) != 0 || len(fake.promoted) != 0 {
		t.Errorf("неизвестная джоба: загружено %d, версий %d", len(fake.uploads), len(fake.promoted))
	}
}

package studio

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Тесты RebuildInserts написаны по контракту задачи «вклейки инструментов»:
// база — всегда audio.flac родителя, партии — audio.flac детей, опора ритма —
// stem-drums.flac родителя (нет — один вызов MakeStems, не вышло — по плану),
// результат — overdub-inst-<ChildID последней спеки>.flac.

const parentID int64 = 1

// sr — частота анализа в тестах
const sr = 16000

func key(id int64, file string) string { return fmt.Sprintf("%d/%s", id, file) }

func needFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
}

// lavfi — сгенерировать файл (16 кГц моно) из источника lavfi.
func lavfi(t *testing.T, src, path string) string {
	t.Helper()
	out, err := exec.Command("ffmpeg", "-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", src, "-ac", "1", "-ar", "16000", path).CombinedOutput()
	if err != nil {
		t.Fatalf("lavfi %q: %v %s", src, err, out)
	}
	return path
}

// decodeBytes — декодировать загруженные байты в моно float32 16 кГц.
func decodeBytes(t *testing.T, data []byte) []float32 {
	t.Helper()
	p := filepath.Join(t.TempDir(), "uploaded.flac")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error",
		"-i", p, "-f", "f32le", "-ac", "1", "-ar", "16000", "-").Output()
	if err != nil {
		t.Fatalf("decode upload: %v", err)
	}
	s := make([]float32, len(raw)/4)
	for i := range s {
		s[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
	}
	return s
}

func slice(s []float32, from, to float64) []float32 {
	a, b := int(from*sr), int(to*sr)
	if a < 0 {
		a = 0
	}
	if b > len(s) {
		b = len(s)
	}
	if a >= b {
		return nil
	}
	return s[a:b]
}

func rms(s []float32) float64 {
	if len(s) == 0 {
		return 0
	}
	var e float64
	for _, v := range s {
		e += float64(v) * float64(v)
	}
	return math.Sqrt(e / float64(len(s)))
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// --- тест-кейсы карточки ---

// Кейс 1: база — всегда audio.flac родителя, никогда не прошлый микс overdub-inst-*.
// Кейс 2: имя результата — по ChildID последней спеки.
func keys(m map[string][]byte) []string {
	var r []string
	for k := range m {
		r = append(r, k)
	}
	return r
}

// Кейс 3: без стема партия встаёт по плану — начало на From−Lead; длина выхода = длине базы.
// Кейс 3б: стема нет, MakeStems его сделал → стем скачивается повторно, MakeStems — ровно один раз.
// Кейс 4: звучит только окно [From,To] — часть партии до From в трек не попадает.
// Кейс 5: Db поверх выравнивания по RMS — −6 дБ даёт гейн ×0.501 от 0 дБ.
// Кейс 6: две спеки — обе партии в выходе на своих местах.
// Кейс 7: пустой specs — ошибка, ничего не загружено.
// Кейс 8: у ребёнка нет audio.flac — ошибка, ничего не загружено.
// Кейс 8б: у родителя нет audio.flac — ошибка (база не скачалась).
// Кейс 9: стем уже есть — MakeStems не вызывается, стем скачивается.

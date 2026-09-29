package dsp

import (
	"encoding/binary"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

// Тесты боевого пути подгонки вклейки: MeasureInsert (замер по атакам опорного
// файла — в приложении это стем бочки) и PlaceInsert (перевод замера во вклейку).
// Сигналы синтетические, 16 кГц моно, файлы во временном каталоге.

// writeWav16 — записать моно 16-бит PCM WAV 16 кГц.
func writeWav16(t *testing.T, path string, s []float32) {
	t.Helper()
	data := make([]byte, 44+2*len(s))
	copy(data[0:], "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(36+2*len(s)))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1) // PCM
	binary.LittleEndian.PutUint16(data[22:], 1) // моно
	binary.LittleEndian.PutUint32(data[24:], testRate)
	binary.LittleEndian.PutUint32(data[28:], testRate*2)
	binary.LittleEndian.PutUint16(data[32:], 2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], uint32(2*len(s)))
	for i, v := range s {
		x := math.Max(-1, math.Min(1, float64(v)))
		binary.LittleEndian.PutUint16(data[44+2*i:], uint16(int16(math.Round(x*32767))))
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// kickTrack — «бочка» 20 с: удары каждые 0.5 с (120 BPM), каждый 4-й с акцентом.
func kickTrack(t *testing.T, dir string) string {
	t.Helper()
	buf := make([]float32, 20*testRate)
	for k := 0; float64(k)*0.5 < 20; k++ {
		amp := 0.5
		if k%4 == 0 {
			amp = 0.95
		}
		addClick(buf, float64(k)*0.5, amp)
	}
	p := filepath.Join(dir, "kick.wav")
	writeWav16(t, p, buf)
	return p
}

// gridParty — партия 12 с: нерегулярные ноты-щелчки на сетке восьмых/четвертей
// (шаг 0.25 с в ВРЕМЕНИ ТРЕКА, фиксированный seed). trueStart — истинное начало
// партии в треке: нота, звучащая в треке в момент T, в файле партии стоит на T − trueStart.
func gridParty(t *testing.T, dir string, trueStart float64) string {
	t.Helper()
	const dur = 12.0
	r := rand.New(rand.NewSource(7))
	buf := make([]float32, int(dur*testRate))
	first := math.Ceil(trueStart/0.25) * 0.25
	for T := first; T < trueStart+dur; T += 0.25 {
		onBeat := math.Mod(T+1e-9, 0.5) < 1e-6
		// на долях нот больше, на восьмых между ними — реже: рисунок нерегулярный
		prob := 0.35
		if onBeat {
			prob = 0.7
		}
		if r.Float64() < prob {
			addClick(buf, T-trueStart, 0.4+0.5*r.Float64())
		}
	}
	p := filepath.Join(dir, "party.wav")
	writeWav16(t, p, buf)
	return p
}

const (
	mFrom = 6.0
	mTo   = 14.0
	mLead = 2.0
	mBeat = 0.5
	mPlan = mFrom - mLead // 4.0 — начало партии по плану
)

// Условие 1: партия отстаёт от плана на 80 мс → найдено с точностью ≤ 30 мс.
func TestMeasureInsertLateParty(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()
	want := mPlan + 0.08
	p, err := MeasureInsert(kickTrack(t, dir), gridParty(t, dir, want), mFrom, mTo, mLead, mBeat)
	if err != nil {
		t.Fatalf("MeasureInsert: %v", err)
	}
	if !p.Aligned {
		t.Fatalf("Aligned=false, want true (placement %+v)", p)
	}
	if math.Abs(p.StartSec-want) > 0.03 {
		t.Errorf("StartSec=%.4f, want %.2f ± 0.03", p.StartSec, want)
	}
	if p.Ratio != 1 {
		t.Errorf("Ratio=%v, want 1 (скорость не растягивается)", p.Ratio)
	}
}

// Условие 2: партия раньше плана на 100 мс → найдено ±30 мс.
func TestMeasureInsertEarlyParty(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()
	want := mPlan - 0.1
	p, err := MeasureInsert(kickTrack(t, dir), gridParty(t, dir, want), mFrom, mTo, mLead, mBeat)
	if err != nil {
		t.Fatalf("MeasureInsert: %v", err)
	}
	if !p.Aligned {
		t.Fatalf("Aligned=false, want true (placement %+v)", p)
	}
	if math.Abs(p.StartSec-want) > 0.03 {
		t.Errorf("StartSec=%.4f, want %.2f ± 0.03", p.StartSec, want)
	}
	if p.Ratio != 1 {
		t.Errorf("Ratio=%v, want 1", p.Ratio)
	}
}

// Условие 3: истинный сдвиг 0.3 с — вне радиуса ±¼ доли (0.125). Поиск идёт только
// в радиусе, значит ложного ответа далеко от плана быть не должно: либо Aligned=false
// и начало по плану, либо ответ в пределах радиуса вокруг плана.
func TestMeasureInsertShiftOutsideRadius(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()
	p, err := MeasureInsert(kickTrack(t, dir), gridParty(t, dir, mPlan+0.3), mFrom, mTo, mLead, mBeat)
	if err != nil {
		t.Fatalf("MeasureInsert: %v", err)
	}
	t.Logf("сдвиг вне радиуса: %+v", p)
	if p.Aligned {
		if math.Abs(p.StartSec-mPlan) > 0.125+0.01 {
			t.Errorf("Aligned=true, StartSec=%.4f вне радиуса поиска %.2f ± 0.125", p.StartSec, mPlan)
		}
	} else if math.Abs(p.StartSec-mPlan) > 1e-9 || p.Ratio != 1 {
		t.Errorf("Aligned=false: want StartSec=%.2f Ratio=1, got %+v", mPlan, p)
	}
	if p.Ratio != 1 {
		t.Errorf("Ratio=%v, want 1", p.Ratio)
	}
}

// Условие 4: партия — тишина → совпадение ненадёжно, стоит по плану, без ошибки.
func TestMeasureInsertSilentParty(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()
	party := filepath.Join(dir, "silent.wav")
	writeWav16(t, party, make([]float32, 12*testRate))
	p, err := MeasureInsert(kickTrack(t, dir), party, mFrom, mTo, mLead, mBeat)
	if err != nil {
		t.Fatalf("MeasureInsert on silence: %v, want no error", err)
	}
	if p.Aligned {
		t.Errorf("Aligned=true on silent party, want false (%+v)", p)
	}
	if math.Abs(p.StartSec-mPlan) > 1e-9 {
		t.Errorf("StartSec=%.4f, want plan %.2f", p.StartSec, mPlan)
	}
	if p.Ratio != 1 {
		t.Errorf("Ratio=%v, want 1", p.Ratio)
	}
}

// Условие 5: нет файла (партии или опоры) → ошибка.
func TestMeasureInsertMissingFile(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()
	missing := filepath.Join(dir, "nope.wav")
	if _, err := MeasureInsert(kickTrack(t, dir), missing, mFrom, mTo, mLead, mBeat); err == nil {
		t.Error("missing party: want error, got nil")
	}
	if _, err := MeasureInsert(missing, gridParty(t, dir, mPlan), mFrom, mTo, mLead, mBeat); err == nil {
		t.Error("missing track: want error, got nil")
	}
}

// Условие 6: PlaceInsert — звучит только кусок [from, to].
func TestPlaceInsert(t *testing.T) {
	const eps = 1e-9
	cases := []struct {
		name string
		p    Placement
		want Insert
	}{
		{"начало раньше from — голова отрезается", Placement{StartSec: 4, Ratio: 1},
			Insert{AtSec: 6, SkipSec: 2, DurSec: 8, Tempo: 1, Gain: 0.7}},
		{"начало внутри окна", Placement{StartSec: 7, Ratio: 1},
			Insert{AtSec: 7, SkipSec: 0, DurSec: 7, Tempo: 1, Gain: 0.7}},
		{"начало позже to — длительность 0", Placement{StartSec: 15, Ratio: 1},
			Insert{AtSec: 15, SkipSec: 0, DurSec: 0, Tempo: 1, Gain: 0.7}},
		{"Ratio 1.02 — отрезок в секундах партии", Placement{StartSec: 4, Ratio: 1.02},
			Insert{AtSec: 6, SkipSec: 2 * 1.02, DurSec: 8, Tempo: 1.02, Gain: 0.7}},
		{"Ratio 0 → Tempo 1", Placement{StartSec: 7, Ratio: 0},
			Insert{AtSec: 7, SkipSec: 0, DurSec: 7, Tempo: 1, Gain: 0.7}},
		{"начало ровно на from", Placement{StartSec: 6, Ratio: 1},
			Insert{AtSec: 6, SkipSec: 0, DurSec: 8, Tempo: 1, Gain: 0.7}},
		{"отрицательный StartSec", Placement{StartSec: -1, Ratio: 1},
			Insert{AtSec: 6, SkipSec: 7, DurSec: 8, Tempo: 1, Gain: 0.7}},
	}
	for _, c := range cases {
		got := PlaceInsert(c.p, 6, 14, 0.7)
		if math.Abs(got.AtSec-c.want.AtSec) > eps || math.Abs(got.SkipSec-c.want.SkipSec) > eps ||
			math.Abs(got.DurSec-c.want.DurSec) > eps || math.Abs(got.Tempo-c.want.Tempo) > eps ||
			math.Abs(got.Gain-c.want.Gain) > eps {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

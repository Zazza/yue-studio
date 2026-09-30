package dsp

import (
	"math"
	"testing"
)

// Тесты цепочки «Мастеринг» (id master): карточка реестра и поведение на
// синтетике. Хелперы — из chains_test.go / inserts_test.go.

// maxAbs — пиковая амплитуда (без знака).
func maxAbs(s []float32) float64 {
	m := 0.0
	for _, v := range s {
		if a := math.Abs(float64(v)); a > m {
			m = a
		}
	}
	return m
}

// Карточка: master есть в реестре, не голосовая (применяется на весь микс),
// без своей отметки start — значит получает общий from.
func TestMasterChainCard(t *testing.T) {
	c := ByID("master")
	if c == nil || c.Name == "" {
		t.Fatalf("ByID(master) = %+v, want цепочку с именем", c)
	}
	if c.Voice {
		t.Errorf("master не должна быть голосовой (Voice=true), она на весь микс")
	}
	if hasParam(c, "start") {
		t.Errorf("у master нет своей отметки, но найден параметр start")
	}
	if !hasParam(c, "from") {
		t.Errorf("у master нет общего параметра from — эффект нельзя включить с отметки")
	}
	d := c.Defaults()
	for id, want := range map[string]float64{"drive": 1.5, "grit": 1.3, "breath": 1.5} {
		if v, ok := d[id]; !ok || v != want {
			t.Errorf("Defaults[%s] = %v (есть=%v), want %v", id, v, ok, want)
		}
	}
}

// masterExpr — белый шум громко (0.5) 0–3 с, тихо (0.05) 3–6 с: широкий спектр
// как у музыки (чистый синус эксайтер раздувает до полной шкалы — на реальном
// материале такого нет).
const masterExpr = "if(lt(t\\,3)\\,0.5\\,0.05)*(random(0)*2-1)"

// Карточка: мастеринг поднимает RMS и держит пик (лимитер 0.94).
func TestMasterRaisesRmsAndCapsPeak(t *testing.T) {
	needFFmpeg(t)
	in, src := genIn(t, "0.3*(random(0)*2-1)", 4)
	out := runChain(t, "master", in, nil)
	gain := dbfs(segRMS(out, 0.5, 3.5)) - dbfs(segRMS(src, 0.5, 3.5))
	if gain < 1.5 {
		t.Errorf("RMS вырос на %+.1f дБ, want ≥ +1.5 (гейн → сатурация)", gain)
	}
	if p := maxAbs(out); p > 0.95 {
		t.Errorf("пик %.3f, want ≤ 0.95 (лимитер держит 0.94)", p)
	}
}

// Карточка: разжатие внутри мастеринга — разброс громкое/тихое шире, чем на входе.
func TestMasterWidensDynamics(t *testing.T) {
	needFFmpeg(t)
	in, src := genIn(t, masterExpr, 6)
	out := runChain(t, "master", in, nil)
	gap := func(s []float32) float64 {
		return dbfs(segRMS(s, 0.5, 2.5)) - dbfs(segRMS(s, 3.5, 5.5))
	}
	if d := gap(out) - gap(src); d < 1 {
		t.Errorf("разброс вырос на %+.1f дБ, want ≥ +1 (экспандер до сатурации)", d)
	}
}

// Краевой: breath=0 — экспандер выключен, но громкость от перегруза остаётся.
func TestMasterBreathZeroStillLoud(t *testing.T) {
	needFFmpeg(t)
	in, src := genIn(t, "0.3*(random(0)*2-1)", 4)
	out := runChain(t, "master", in, map[string]float64{"breath": 0})
	gain := dbfs(segRMS(out, 0.5, 3.5)) - dbfs(segRMS(src, 0.5, 3.5))
	if gain < 1.5 {
		t.Errorf("breath=0: RMS вырос на %+.1f дБ, want ≥ +1.5 (перегруз работает)", gain)
	}
}

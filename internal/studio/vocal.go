package studio

import "yue-studio/internal/yue"

// VoiceSource — источник голоса версии: рендер, чей голос в ней звучит
// («перепеть с места» продолжает от него). VoiceSrc версии; сгенерированный
// трек (роль не variant) — он сам; вариант без VoiceSrc — источник родителя.
// 0 — не найден (нет родителя в списке, цикл). Та же семантика, что
// vocalParts.voiceSource во фронте.
func VoiceSource(job yue.Job, byID map[int64]yue.Job) int64 {
	seen := map[int64]bool{}
	cur, ok := job, true
	for ok {
		if seen[cur.ID] {
			return 0
		}
		seen[cur.ID] = true
		if cur.VoiceSrc != nil && *cur.VoiceSrc > 0 {
			return *cur.VoiceSrc
		}
		if cur.Role != "variant" {
			return cur.ID
		}
		if cur.ParentID == nil {
			return 0
		}
		cur, ok = byID[*cur.ParentID]
	}
	return 0
}

// RevoiceSpec — замена голоса в окне части голосом дубля: дубль —
// продолжение источника, его время совпадает со временем трека → Lead = From.
func RevoiceSpec(takeID int64, from, to, beatSec float64) SectionSpec {
	return SectionSpec{ChildID: takeID, From: from, To: to, Lead: from, BeatSec: beatSec,
		Stems: []string{"vocals"}, Revoice: true}
}

package studio

import (
	"reflect"
	"testing"
	"time"

	"yue-studio/internal/yue"
)

// Тесты VoiceSource / RevoiceSpec — по спецификации «голос по частям»:
// источник голоса версии — VoiceSrc, если задан; сгенерированная версия
// (любая роль, кроме variant) поёт своим голосом; вариант эффекта наследует
// голос родителя; нет родителя или цикл — 0.

func i64(v int64) *int64 { return &v }

func jobsByID(jobs ...yue.Job) map[int64]yue.Job {
	m := map[int64]yue.Job{}
	for _, j := range jobs {
		m[j.ID] = j
	}
	return m
}

func TestVoiceSourceExplicitVoiceSrcWins(t *testing.T) {
	// VoiceSrc задан — он, даже у сгенерированной роли и у варианта с родителем
	root := yue.Job{ID: 1}
	for _, role := range []string{"", "continue", "section", "rebuild", "fragment", "variant"} {
		j := yue.Job{ID: 10, Role: role, ParentID: i64(1), VoiceSrc: i64(7)}
		if got := VoiceSource(j, jobsByID(root, j)); got != 7 {
			t.Fatalf("role %q: VoiceSource = %d, want 7 (VoiceSrc)", role, got)
		}
	}
}

func TestVoiceSourceGeneratedRolesAreOwnVoice(t *testing.T) {
	// сгенерированная версия — голос свой, родитель не важен
	root := yue.Job{ID: 1}
	for _, role := range []string{"", "continue", "section", "rebuild", "fragment"} {
		j := yue.Job{ID: 20, Role: role, ParentID: i64(1)}
		if got := VoiceSource(j, jobsByID(root, j)); got != 20 {
			t.Fatalf("role %q: VoiceSource = %d, want 20 (сам)", role, got)
		}
	}
	// корень без родителя
	if got := VoiceSource(root, jobsByID(root)); got != 1 {
		t.Fatalf("root: VoiceSource = %d, want 1", got)
	}
}

func TestVoiceSourceVariantInheritsFromParent(t *testing.T) {
	root := yue.Job{ID: 1}
	v := yue.Job{ID: 11, Role: "variant", ParentID: i64(1)}
	if got := VoiceSource(v, jobsByID(root, v)); got != 1 {
		t.Fatalf("variant of root: %d, want 1", got)
	}
}

func TestVoiceSourceVariantChainRecursesThroughParents(t *testing.T) {
	// variant → variant → rebuild(с VoiceSrc=3): источник — VoiceSrc пересборки
	rb := yue.Job{ID: 30, Role: "rebuild", ParentID: i64(1), VoiceSrc: i64(3)}
	v1 := yue.Job{ID: 31, Role: "variant", ParentID: i64(30)}
	v2 := yue.Job{ID: 32, Role: "variant", ParentID: i64(31)}
	m := jobsByID(yue.Job{ID: 1}, rb, v1, v2)
	if got := VoiceSource(v2, m); got != 3 {
		t.Fatalf("variant chain: %d, want 3", got)
	}
	// variant → variant → section (без VoiceSrc): источник — сама секция
	sec := yue.Job{ID: 40, Role: "section", ParentID: i64(1)}
	w1 := yue.Job{ID: 41, Role: "variant", ParentID: i64(40)}
	w2 := yue.Job{ID: 42, Role: "variant", ParentID: i64(41)}
	m = jobsByID(yue.Job{ID: 1}, sec, w1, w2)
	if got := VoiceSource(w2, m); got != 40 {
		t.Fatalf("variant chain to section: %d, want 40", got)
	}
}

func TestVoiceSourceVariantWithoutParentIsZero(t *testing.T) {
	v := yue.Job{ID: 12, Role: "variant"}
	if got := VoiceSource(v, jobsByID(v)); got != 0 {
		t.Fatalf("variant без ParentID: %d, want 0", got)
	}
	// родитель указан, но в карте его нет
	w := yue.Job{ID: 13, Role: "variant", ParentID: i64(999)}
	if got := VoiceSource(w, jobsByID(w)); got != 0 {
		t.Fatalf("variant с потерянным родителем: %d, want 0", got)
	}
	// пустая / nil карта
	if got := VoiceSource(w, nil); got != 0 {
		t.Fatalf("nil map: %d, want 0", got)
	}
}

func TestVoiceSourceParentCycleIsZeroAndTerminates(t *testing.T) {
	a := yue.Job{ID: 50, Role: "variant", ParentID: i64(51)}
	b := yue.Job{ID: 51, Role: "variant", ParentID: i64(50)}
	self := yue.Job{ID: 52, Role: "variant", ParentID: i64(52)}
	m := jobsByID(a, b, self)

	done := make(chan [2]int64, 1)
	go func() { done <- [2]int64{VoiceSource(a, m), VoiceSource(self, m)} }()
	select {
	case got := <-done:
		if got[0] != 0 || got[1] != 0 {
			t.Fatalf("cycle: got %v, want [0 0]", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("VoiceSource зависла на цикле родителей")
	}
}

func TestRevoiceSpec(t *testing.T) {
	got := RevoiceSpec(77, 12.5, 30, 0.5)
	want := SectionSpec{
		ChildID: 77, From: 12.5, To: 30, Lead: 12.5, BeatSec: 0.5,
		Stems: []string{"vocals"}, Revoice: true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RevoiceSpec = %+v, want %+v", got, want)
	}
	// from = 0: Lead тоже 0, BeatSec 0 передаётся как есть (дефолт решает RebuildSections)
	got = RevoiceSpec(5, 0, 8, 0)
	if got.Lead != 0 || got.From != 0 || got.To != 8 || got.BeatSec != 0 || got.ChildID != 5 ||
		!got.Revoice || !reflect.DeepEqual(got.Stems, []string{"vocals"}) {
		t.Fatalf("RevoiceSpec(from=0) = %+v", got)
	}
	if got.Db != 0 || got.FadeIn != 0 || got.FadeOut != 0 || got.KeepHighHz != 0 {
		t.Fatalf("лишние поля заданы: %+v", got)
	}
}

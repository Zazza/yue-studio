package studio

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// Тесты условия 55 / ТК89 карточки internal-own-track: подпись пересборки
// (rebuildLabel) — не длиннее 280 рун (воркер принимает 300, запас под
// « · мастер»). Части по порядку, пока влезают; не влезли — в конце « + ещё N»
// (N — сколько частей не вошло); первая часть сама длиннее предела —
// обрезается с «…». Короткая подпись — побайтно прежняя.

const labelLimit = 280

// mmssLabel — окно подписи в формате карточки: « M:SS–M:SS».
func mmssLabel(from, to int) string {
	return fmt.Sprintf("%d:%02d–%d:%02d", from/60, from%60, to/60, to%60)
}

// engPart — подпись одной записи движка (amp → cab) на other в окне [from, to).
func engPart(from, to int) string {
	return "Движок: amp → cab · гитары/синты " + mmssLabel(from, to)
}

// fortyEngine — 40 записей движка с разными окнами и их ожидаемые части подписи.
func fortyEngine() ([]SectionSpec, []string) {
	specs := make([]SectionSpec, 0, 40)
	parts := make([]string, 0, 40)
	for i := 0; i < 40; i++ {
		from, to := i*5, i*5+3
		specs = append(specs, SectionSpec{ChildID: 0, From: float64(from), To: float64(to),
			Stems: []string{"other"}, Engine: engChain()})
		parts = append(parts, engPart(from, to))
	}
	return specs, parts
}

// oneBlockSpec — одна запись движка из одного блока с типом typ на other, весь трек:
// подпись «Движок: <typ> · гитары/синты».
func oneBlockSpec(typ string) SectionSpec {
	return SectionSpec{ChildID: 0, Stems: []string{"other"},
		Engine: []map[string]any{{"type": typ}}}
}

// ТК89: одна запись → подпись прежняя (побайтно).
func TestRebuildLabelSingleUnchanged(t *testing.T) {
	sp := SectionSpec{ChildID: 0, From: 80, To: 88, Stems: []string{"other"}, Engine: engChain()}
	want := "Движок: amp → cab · гитары/синты 1:20–1:28"
	if got := rebuildLabel([]SectionSpec{sp}); got != want {
		t.Errorf("подпись %q, want %q", got, want)
	}
}

// Условие 55: несколько коротких записей, вместе короче предела, — прежний формат через « + ».
func TestRebuildLabelFewShortUnchanged(t *testing.T) {
	specs, parts := fortyEngine()
	want := strings.Join(parts[:3], " + ")
	if got := rebuildLabel(specs[:3]); got != want {
		t.Errorf("подпись %q, want %q", got, want)
	}
}

// Граница: подпись ровно 280 рун (кириллица — байтов больше 280) — не меняется.
func TestRebuildLabelExactLimitUnchanged(t *testing.T) {
	base := "Движок:  · гитары/синты"
	typ := strings.Repeat("ж", labelLimit-utf8.RuneCountInString(base))
	want := "Движок: " + typ + " · гитары/синты"
	if n := utf8.RuneCountInString(want); n != labelLimit {
		t.Fatalf("подготовка: подпись %d рун, want %d", n, labelLimit)
	}
	if got := rebuildLabel([]SectionSpec{oneBlockSpec(typ)}); got != want {
		t.Errorf("подпись ровно %d рун изменена:\n got %q\nwant %q", labelLimit, got, want)
	}
}

// ТК89: 40 записей движка с окнами → ≤ 280 рун, начинается с подписи первой
// записи, кончается « + ещё N»; вошедшие части — целые, по порядку, через « + »;
// N = 40 − число вошедших; следующая часть уже не влезла бы.
func TestRebuildLabelFortyEngineTruncated(t *testing.T) {
	specs, parts := fortyEngine()
	got := rebuildLabel(specs)

	if n := utf8.RuneCountInString(got); n > labelLimit {
		t.Fatalf("подпись %d рун > %d: %q", n, labelLimit, got)
	}
	if !strings.HasPrefix(got, parts[0]) {
		t.Errorf("подпись не начинается с первой записи %q: %q", parts[0], got)
	}
	i := strings.LastIndex(got, " + ещё ")
	if i < 0 {
		t.Fatalf("нет « + ещё N» в конце: %q", got)
	}
	n, err := strconv.Atoi(got[i+len(" + ещё "):])
	if err != nil {
		t.Fatalf("после « + ещё » не число: %q", got[i:])
	}
	k := 40 - n // вошедших частей
	if k < 1 || k >= 40 {
		t.Fatalf("N = %d: вошло %d частей из 40", n, k)
	}
	if head, want := got[:i], strings.Join(parts[:k], " + "); head != want {
		t.Errorf("вошедшие части (%d) не целые/не по порядку:\n got %q\nwant %q", k, head, want)
	}
	// «пока влезают»: с ещё одной частью подпись превысила бы предел
	next := strings.Join(parts[:k+1], " + ")
	if rest := 40 - k - 1; rest > 0 {
		next += " + ещё " + strconv.Itoa(rest)
	}
	if utf8.RuneCountInString(next) <= labelLimit {
		t.Errorf("влезла бы ещё часть: %d частей дают %d рун ≤ %d", k+1, utf8.RuneCountInString(next), labelLimit)
	}
}

// Условие 55: две записи, вторая не влезает → «<первая> + ещё 1».
func TestRebuildLabelSecondDoesNotFit(t *testing.T) {
	first := oneBlockSpec(strings.Repeat("a", 200))
	second := oneBlockSpec(strings.Repeat("b", 100))
	wantFirst := "Движок: " + strings.Repeat("a", 200) + " · гитары/синты"
	want := wantFirst + " + ещё 1"
	if got := rebuildLabel([]SectionSpec{first, second}); got != want {
		t.Errorf("подпись %q, want %q", got, want)
	}
}

// ТК89: одна запись с подписью 400 рун (движок из многих блоков) → ≤ 280 рун,
// кончается «…», до «…» — начало исходной подписи.
func TestRebuildLabelLongFirstEllipsis(t *testing.T) {
	var chain []map[string]any
	var names []string
	full := ""
	for i := 0; utf8.RuneCountInString(full) < 400; i++ {
		typ := fmt.Sprintf("блок%d", i)
		chain = append(chain, map[string]any{"type": typ})
		names = append(names, typ)
		full = "Движок: " + strings.Join(names, " → ") + " · гитары/синты 0:02–0:06"
	}
	sp := SectionSpec{ChildID: 0, From: 2, To: 6, Stems: []string{"other"}, Engine: chain}
	got := rebuildLabel([]SectionSpec{sp})

	if n := utf8.RuneCountInString(got); n > labelLimit {
		t.Fatalf("подпись %d рун > %d", n, labelLimit)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("длинная первая часть не кончается «…»: %q", got)
	}
	if head := strings.TrimSuffix(got, "…"); !strings.HasPrefix(full, head) || head == "" {
		t.Errorf("до «…» не начало исходной подписи:\n got %q\nfull %q", head, full)
	}
}

// Граница: подпись 281 руна — уже обрезается (≤ 280, «…»).
func TestRebuildLabelOverLimitByOne(t *testing.T) {
	base := "Движок:  · гитары/синты"
	typ := strings.Repeat("ж", labelLimit+1-utf8.RuneCountInString(base))
	got := rebuildLabel([]SectionSpec{oneBlockSpec(typ)})
	if n := utf8.RuneCountInString(got); n > labelLimit {
		t.Errorf("подпись %d рун > %d", n, labelLimit)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("подпись 281 руна без «…»: %q", got)
	}
}

// ТК89: rebuildSections с 40 записями — UploadDsp получает подпись ≤ 280 рун
// (фейк сервиса secFake; записи — громкость дорожки, без воркера).
func TestRebuildSectionsFortyLabelWithinLimit(t *testing.T) {
	f := labelSetup(t, 12)
	specs := make([]SectionSpec, 0, 40)
	for i := 0; i < 40; i++ {
		from := float64(i) * 0.25
		specs = append(specs, SectionSpec{ChildID: 0, From: from, To: from + 0.2,
			Stems: []string{"other"}, Db: 1})
	}
	got := mixLabel(t, f, specs...)
	if n := utf8.RuneCountInString(got); n > labelLimit {
		t.Errorf("UploadDsp получил подпись %d рун > %d: %q", n, labelLimit, got)
	}
	if !strings.HasPrefix(got, "громкость +1 дБ · гитары/синты") {
		t.Errorf("подпись не начинается с первой записи: %q", got)
	}
}

package studio

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"yue-studio/internal/dsp"
	"yue-studio/internal/yue"
)

// SplicePart — кусок аудио версии [From, To) (To ≤ 0 — до конца) с громкостью GainDb.
type SplicePart struct {
	JobID  int64   `json:"job_id"`
	From   float64 `json:"from"`
	To     float64 `json:"to"`
	GainDb float64 `json:"gain_db"`
}

// spliceXfade — переход между кусками по умолчанию, с: короче доли — ритм не плывёт
const spliceXfade = 0.05

// Splice — новая версия из кусков аудио версий по порядку, соседние сшиты
// переходом crossfade: вернуть вырезанный проигрыш, собрать лучшие куски.
// Резать лучше по границам тактов одного исполнения — тогда шва не слышно.
// Результат — вариант трека baseID (dsp-splice-<n>.flac: такие имена
// принимает загрузка воркера).
func Splice(ctx context.Context, svc yue.Service, baseID int64, parts []SplicePart, crossfade float64) (*yue.DspVariant, error) {
	if len(parts) == 0 {
		return nil, errors.New("нет кусков для склейки")
	}
	for i, p := range parts {
		if p.From < 0 || (p.To > 0 && p.To <= p.From) {
			return nil, fmt.Errorf("кусок %d: конец (%g) должен быть позже начала (%g)", i+1, p.To, p.From)
		}
	}
	if crossfade <= 0 {
		crossfade = spliceXfade
	}
	dir, err := os.MkdirTemp("", fmt.Sprintf("yue-splice-%d-*", baseID))
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	files := map[int64]string{}
	inputs := make([]string, 0, len(parts))
	for _, p := range parts {
		f, ok := files[p.JobID]
		if !ok {
			if f, err = FetchBase(ctx, svc, p.JobID, dir); err != nil {
				return nil, fmt.Errorf("трек #%d: %w", p.JobID, err)
			}
			files[p.JobID] = f
		}
		inputs = append(inputs, f)
	}
	out := dir + "/out.flac"
	if err := dsp.RunInputs(inputs, out, spliceGraph(parts, crossfade)); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(out)
	if err != nil {
		return nil, err
	}
	v, err := svc.UploadDsp(ctx, baseID, fmt.Sprintf("dsp-splice-%d.flac", time.Now().Unix()), data)
	if err != nil {
		return nil, err
	}
	// длина — из самого файла (метрики воркера её могут не нести)
	if d := flacDuration(data); d > 0 {
		if v.Metrics == nil {
			v.Metrics = map[string]any{}
		}
		v.Metrics["duration_sec"] = d
	}
	return v, nil
}

// flacDuration — длина FLAC по блоку STREAMINFO (частота и число сэмплов); 0 — не FLAC.
func flacDuration(b []byte) float64 {
	// "fLaC" + заголовок блока (4) + STREAMINFO: байты 10..17 — частота (20 бит),
	// каналы (3), бит (5), число сэмплов (36)
	if len(b) < 8+18 || string(b[:4]) != "fLaC" {
		return 0
	}
	si := b[8:]
	rate := uint64(si[10])<<12 | uint64(si[11])<<4 | uint64(si[12])>>4
	total := uint64(si[13]&0x0f)<<32 | uint64(si[14])<<24 | uint64(si[15])<<16 | uint64(si[16])<<8 | uint64(si[17])
	if rate == 0 {
		return 0
	}
	return float64(total) / float64(rate)
}

// spliceGraph — куски (atrim + громкость) и цепочка acrossfade между соседними.
func spliceGraph(parts []SplicePart, xf float64) string {
	var b strings.Builder
	for i, p := range parts {
		trim := fmt.Sprintf("atrim=start=%g", p.From)
		if p.To > 0 {
			trim += fmt.Sprintf(":end=%g", p.To)
		}
		fmt.Fprintf(&b, "[%d:a]%s,asetpts=PTS-STARTPTS,volume=%gdB[p%d];", i, trim, p.GainDb, i)
	}
	if len(parts) == 1 {
		b.WriteString("[p0]anull[out]")
		return b.String()
	}
	prev := "p0"
	for i := 1; i < len(parts); i++ {
		next := fmt.Sprintf("x%d", i)
		if i == len(parts)-1 {
			next = "out"
		}
		fmt.Fprintf(&b, "[%s][p%d]acrossfade=d=%g:c1=tri:c2=tri[%s]", prev, i, xf, next)
		if next != "out" {
			b.WriteString(";")
		}
		prev = next
	}
	return b.String()
}

package studio

import (
	"context"
	"fmt"
	"os"
	"slices"

	"yue-studio/internal/dsp"
	"yue-studio/internal/yue"
)

// VolumeEnvelope — линия громкости, нарисованная по волне (точки время → дБ).
// stem "" — весь трек: звук трека через линию, вариант dsp-envelope.flac.
// stem vocals/drums/bass/other — только эта дорожка, через пересборку
// (остальные дорожки не меняются), результат — вариант пересборки.
func VolumeEnvelope(ctx context.Context, svc yue.Service, jobID int64, stem string, pts []dsp.EnvPoint) (*yue.DspVariant, error) {
	norm, err := dsp.NormalizeEnvelope(pts)
	if err != nil {
		return nil, err
	}
	if stem != "" {
		if !slices.Contains(mutable, stem) {
			return nil, fmt.Errorf("неизвестная дорожка %q (vocals/drums/bass/other или пусто — весь трек)", stem)
		}
		res, err := RebuildSections(ctx, svc, jobID, []SectionSpec{{Stems: []string{stem}, Envelope: norm}})
		if err != nil {
			return nil, err
		}
		return res.Variant, nil
	}
	dir, err := os.MkdirTemp("", fmt.Sprintf("yue-envelope-%d-*", jobID))
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	base, err := FetchBase(ctx, svc, jobID, dir)
	if err != nil {
		return nil, fmt.Errorf("звук трека #%d: %w", jobID, err)
	}
	out := dir + "/envelope.flac"
	if err := dsp.Run(base, out, dsp.EnvelopeGraph(norm), nil); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(out)
	if err != nil {
		return nil, err
	}
	return svc.UploadDsp(ctx, jobID, "dsp-envelope.flac", "", data)
}

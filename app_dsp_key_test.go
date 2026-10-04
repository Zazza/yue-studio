package main

import (
	"context"
	"io"
	"strings"
	"testing"

	"yue-studio/internal/dsp"
	"yue-studio/internal/yue"
)

// Карточка internal-dsp-space 6.5 (путь приложения): цепочка с ключом (ducking,
// Key drums) не применяется на весь трек — понятная ошибка «только эффект на
// дорожку» до скачивания звука и ffmpeg.

// fetchCountService — fakeService, считающий запросы звука.
type fetchCountService struct {
	fakeService
	fetched []string
}

func (f *fetchCountService) FetchAudio(ctx context.Context, id int64, file string) (io.ReadCloser, string, error) {
	f.fetched = append(f.fetched, file)
	return f.fakeService.FetchAudio(ctx, id, file)
}

func TestApplyDspKeyChainWholeTrackIsError(t *testing.T) {
	for name, apply := range map[string]func(a *App) error{
		"YueApplyDsp": func(a *App) error {
			_, err := a.YueApplyDsp(1, "ducking", nil)
			return err
		},
		"YueFxPreview": func(a *App) error {
			defer a.fxCleanup()
			_, err := a.YueFxPreview(1, "", []dsp.Step{{Chain: "ducking"}}, 20, 35, "")
			return err
		},
	} {
		s := &fetchCountService{fakeService: fakeService{
			jobs:      []yue.Job{{ID: 1, Status: "done", AudioFile: "audio.flac"}},
			fetchData: map[string][]byte{"audio.flac": []byte("not-audio")},
		}}
		a := newTestApp(s, &fakePlayer{})
		err := apply(a)
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "дорожк") {
			t.Errorf("%s ducking на весь трек: want ошибку про дорожку, got %v", name, err)
		}
		if len(s.fetched) != 0 {
			t.Errorf("%s: до ошибки скачано %v — проверка должна быть до звука и ffmpeg", name, s.fetched)
		}
	}
}

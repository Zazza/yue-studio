// Package studio — конвейеры экрана студии на стороне ПК (ffmpeg + воркер).
package studio

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"yue-studio/internal/yue"
)

// baseAudioFiles — где лежит звук трека: у сгенерированных audio.flac, у
// импортированных — как загрузили (audio.mp3 / audio.wav: #257)
var baseAudioFiles = []string{"audio.flac", "audio.mp3", "audio.wav"}

// FetchBase — звук трека во временный файл: первое имя из baseAudioFiles,
// которое есть у джобы (404 — пробуем следующее, иная ошибка — сразу наружу).
func FetchBase(ctx context.Context, svc yue.Service, id int64, dir string) (string, error) {
	var last error
	for _, name := range baseAudioFiles {
		p, err := FetchTemp(ctx, svc, id, name, dir, "*"+filepath.Ext(name))
		if err == nil {
			return p, nil
		}
		var se *yue.StatusError
		if !errors.As(err, &se) || se.Code != http.StatusNotFound {
			return "", err
		}
		last = err
	}
	return "", last
}

// FetchTemp скачивает артефакт джобы во временный файл каталога dir
// ("" — системный temp) по шаблону имени pattern; удаление — на вызывающем.
func FetchTemp(ctx context.Context, svc yue.Service, id int64, file, dir, pattern string) (string, error) {
	body, _, err := svc.FetchAudio(ctx, id, file)
	if err != nil {
		return "", err
	}
	defer func() { _ = body.Close() }()
	tmp, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	_, cpErr := io.Copy(tmp, body)
	tmp.Close()
	if cpErr != nil {
		os.Remove(tmp.Name())
		return "", cpErr
	}
	return tmp.Name(), nil
}

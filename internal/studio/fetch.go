// Package studio — конвейеры экрана студии на стороне ПК (ffmpeg + воркер).
package studio

import (
	"context"
	"io"
	"os"

	"yue-studio/internal/yue"
)

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

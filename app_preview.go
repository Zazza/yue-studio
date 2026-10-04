package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"yue-studio/internal/dsp"
	"yue-studio/internal/studio"
)

// fxCacheDirPattern — каталог кэша превью эффектов: свой у каждого запуска
// приложения (второй экземпляр или тест не стирает кэш работающего)
const fxCacheDirPattern = "yue-studio-fx-*"

// previewSlots — куски превью: "" — обычное превью, A–D — «сравнить наборы».
var previewSlots = []string{"", "A", "B", "C", "D"}

// fxCache — кэш трека и стемов для превью. Создаётся при первом обращении во
// временном каталоге, удаляется при закрытии окна (fxCleanup).
func (a *App) fxCache() *studio.Cache {
	a.fxOnce.Do(func() {
		dir, err := os.MkdirTemp("", fxCacheDirPattern)
		if err != nil {
			dir = filepath.Join(os.TempDir(), fmt.Sprintf("yue-studio-fx-%d", os.Getpid()))
		}
		a.fxDir = dir
		a.fx = studio.NewCache(filepath.Join(a.fxDir, "files"))
		a.fxDur = map[string]float64{}
	})
	return a.fx
}

// fxCleanup — удалить кэш превью (окно закрыто).
func (a *App) fxCleanup() {
	if a.fxDir != "" {
		_ = os.RemoveAll(a.fxDir) // временный каталог; не удалился — уберёт ОС
	}
}

// previewDir — где лежат куски превью трека для слота (A–D или обычный).
func (a *App) previewDir(jobID int64, slot string) (string, error) {
	if !slices.Contains(previewSlots, slot) {
		return "", fmt.Errorf("неизвестный слот превью %q", slot)
	}
	a.fxCache()
	if slot == "" {
		slot = "main"
	}
	return filepath.Join(a.fxDir, "previews", fmt.Sprint(jobID), slot), nil
}

// YueFxPreview — быстрое превью эффектов на куске трека [from, to): steps — цепочка
// эффектов по порядку (педали), stem "" — весь трек, иначе дорожка. Куски «было»
// и «стало» остаются на ПК (на воркер не грузятся); играет их YuePlayPreview.
func (a *App) YueFxPreview(jobID int64, stem string, steps []dsp.Step, from, to float64, slot string) (*studio.PreviewResult, error) {
	dir, err := a.previewDir(jobID, slot)
	if err != nil {
		return nil, err
	}
	res, err := studio.Preview(a.ctx, a.yue, a.fxCache(), studio.PreviewSpec{
		JobID: jobID, Stem: stem, Steps: steps, From: from, To: to}, dir)
	if err != nil {
		return nil, err
	}
	a.fxMu.Lock()
	a.fxDur[dir] = res.DurSec
	a.fxMu.Unlock()
	return res, nil
}

// YuePlayPreview — сыграть кусок превью: which "wet" (стало) или "dry" (было),
// с секунды startSec куска — переключение было↔стало на той же позиции. Длину
// куска (без неё плеер не перематывает) App запомнил при превью. Файл
// выбирается по имени слота, а не по пути: фронт не может попросить сыграть
// произвольный файл ПК.
func (a *App) YuePlayPreview(jobID int64, slot, which string, startSec float64) error {
	dir, err := a.previewDir(jobID, slot)
	if err != nil {
		return err
	}
	if which != "wet" && which != "dry" {
		return fmt.Errorf("неизвестный кусок превью %q", which)
	}
	a.fxMu.Lock()
	durSec := a.fxDur[dir]
	a.fxMu.Unlock()
	data, err := os.ReadFile(filepath.Join(dir, which+".flac"))
	if err != nil {
		return fmt.Errorf("превью ещё не сделано: %w", err)
	}
	if err := a.player.Load(jobID, data, time.Duration(durSec*float64(time.Second))); err != nil {
		return err
	}
	if startSec > 0 && startSec < durSec {
		// перемотка сама запускает плеер с позиции — без лишнего старта с нуля
		return a.player.Seek(time.Duration(startSec * float64(time.Second)))
	}
	return a.player.Play()
}

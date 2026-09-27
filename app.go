package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
	"path/filepath"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"yue-studio/internal/dsp"
	"yue-studio/internal/yue"
)

type App struct {
	ctx context.Context
	yue yue.Service
}

func NewApp(client *yue.Client) *App {
	return &App{yue: client}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// shutdown — окно закрыто: глушим pw-play, иначе он играет сиротой.
func (a *App) shutdown(ctx context.Context) {
	pl.stop()
}

func (a *App) YueStatus() (*yue.HealthInfo, error) {
	return a.yue.Health(a.ctx)
}

func (a *App) YueJobs() ([]yue.Job, error) {
	return a.yue.Jobs(a.ctx)
}

func (a *App) YueSubmit(params yue.SubmitParams) (int64, error) {
	return a.yue.Submit(a.ctx, params)
}

// YueSubmitFan — веер best-of-N: n джоб подряд, сиды base+0..n-1
// (base из поля seed или случайный).
func (a *App) YueSubmitFan(params yue.SubmitParams, n int) ([]int64, error) {
	if n < 1 {
		n = 1
	}
	if n > 10 {
		n = 10
	}
	base := params.Seed
	if base <= 0 {
		base = time.Now().UnixNano() % 1_000_000_000
	}
	ids := make([]int64, 0, n)
	for i := 0; i < n; i++ {
		p := params
		p.Seed = base + int64(i)
		if p.Title != "" {
			p.Title = fmt.Sprintf("%s [%d/%d]", params.Title, i+1, n)
		}
		id, err := a.yue.Submit(a.ctx, p)
		if err != nil {
			return ids, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (a *App) YuePlan(params yue.PlanParams) (*yue.PlanResult, error) {
	return a.yue.Plan(a.ctx, params)
}

func (a *App) YueCopilot(params yue.CopilotParams) (*yue.CopilotResult, error) {
	return a.yue.Copilot(a.ctx, params)
}

func (a *App) YueTranslate(text string) (*yue.TranslateResult, error) {
	return a.yue.Translate(a.ctx, text)
}

func (a *App) YueAnalyzeJob(id int64) (map[string]any, error) {
	return a.yue.AnalyzeJob(a.ctx, id)
}

func (a *App) YueReferences() ([]yue.Reference, error) {
	return a.yue.References(a.ctx)
}

// YueAddReference — диалог выбора аудио-файла, заливка на воркер и замер.
func (a *App) YueAddReference() (*yue.Reference, error) {
	src, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Референс для сравнения метрик",
		Filters: []runtime.FileFilter{
			{DisplayName: "Аудио (*.flac; *.mp3; *.wav; *.ogg; *.m4a)", Pattern: "*.flac;*.mp3;*.wav;*.ogg;*.m4a"},
		},
	})
	if err != nil {
		return nil, err
	}
	if src == "" {
		return nil, nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return nil, err
	}
	if len(data) > 200<<20 {
		return nil, fmt.Errorf("файл слишком большой (>200 МБ)")
	}
	name := filepath.Base(src)
	return a.yue.AddReference(a.ctx, name, data)
}

// YueDspChains — пресеты эффектов (крутилки с дефолтами).
func (a *App) YueDspChains() []dsp.Chain {
	return dsp.All()
}

// YueApplyDsp — скачать flac джобы, прогнать цепочку локальным ffmpeg,
// залить вариант обратно на воркер (там замер) и вернуть метрики.
func (a *App) YueApplyDsp(jobID int64, chainID string, params map[string]float64) (*yue.DspVariant, error) {
	chain := dsp.ByID(chainID)
	if chain == nil {
		return nil, fmt.Errorf("unknown chain %q", chainID)
	}
	jobs, err := a.yue.Jobs(a.ctx)
	if err != nil {
		return nil, err
	}
	var audio string
	for _, j := range jobs {
		if j.ID == jobID {
			audio = j.AudioFile
			break
		}
	}
	if audio == "" {
		return nil, fmt.Errorf("job %d has no audio", jobID)
	}
	body, _, err := a.yue.FetchAudio(a.ctx, jobID, audio)
	if err != nil {
		return nil, err
	}
	tmpIn, err := os.CreateTemp("", fmt.Sprintf("yue-dsp-%d-in-*.flac", jobID))
	if err != nil {
		body.Close()
		return nil, err
	}
	_, cpErr := io.Copy(tmpIn, body)
	body.Close()
	tmpIn.Close()
	if cpErr != nil {
		os.Remove(tmpIn.Name())
		return nil, cpErr
	}
	defer os.Remove(tmpIn.Name())

	tmpOut, err := os.CreateTemp("", fmt.Sprintf("yue-dsp-%d-out-*.flac", jobID))
	if err != nil {
		return nil, err
	}
	tmpOut.Close()
	defer os.Remove(tmpOut.Name())

	if err := dsp.Run(tmpIn.Name(), tmpOut.Name(), chain.FilterGraph(params)); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(tmpOut.Name())
	if err != nil {
		return nil, err
	}
	fname := fmt.Sprintf("dsp-%s.flac", chainID)
	return a.yue.UploadDsp(a.ctx, jobID, fname, data)
}

func (a *App) YueDspVariants(jobID int64) ([]yue.DspVariant, error) {
	return a.yue.JobDspVariants(a.ctx, jobID)
}

func (a *App) YueCancelJob(id int64) (bool, error) {
	return a.yue.Cancel(a.ctx, id)
}

func (a *App) YueDeleteJob(id int64) (bool, error) {
	return a.yue.DeleteJob(a.ctx, id)
}

func (a *App) YueAudioURL(id int64, file string) string {
	return a.yue.AudioURL(id, file)
}

func (a *App) YueOpenExternal(id int64, file string) {
	body, _, err := a.yue.FetchAudio(a.ctx, id, file)
	if err != nil {
		return
	}
	defer body.Close()
	tmp, err := os.CreateTemp("", fmt.Sprintf("yue-%d-*%s", id, filepath.Ext(file)))
	if err != nil {
		return
	}
	if _, err := io.Copy(tmp, body); err != nil {
		tmp.Close()
		return
	}
	tmp.Close()
	exec.Command("xdg-open", tmp.Name()).Start()
}

func (a *App) YueSaveAudio(id int64, file string) (string, error) {
	body, _, err := a.yue.FetchAudio(a.ctx, id, file)
	if err != nil {
		return "", err
	}
	defer body.Close()

	target, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		DefaultFilename: fmt.Sprintf("yue-%d-%s", id, file),
	})
	if err != nil {
		return "", err
	}
	if target == "" {
		return "", nil
	}
	f, err := os.Create(filepath.Clean(target))
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, body); err != nil {
		return "", err
	}
	return target, nil
}

func (a *App) YuePlayAudio(id int64) error {
	jobs, err := a.yue.Jobs(a.ctx)
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if j.ID == id {
			// flac: меньше wav в ~5–7 раз → быстрее скачивается с воркера;
			// pw-play (libsndfile) играет flac напрямую
			f := j.AudioFile
			if f == "" {
				f = j.WavFile
			}
			if f == "" {
				f = j.Mp3File
			}
			if f == "" {
				return fmt.Errorf("no audio for job %d", id)
			}
			body, _, err := a.yue.FetchAudio(a.ctx, id, f)
			if err != nil {
				return err
			}
			data, err := io.ReadAll(body)
			body.Close()
			if err != nil {
				return err
			}
			dur := time.Duration(j.DurationSec * float64(time.Second))
			if err := pl.load(id, data, dur); err != nil {
				return err
			}
			return pl.play()
		}
	}
	return fmt.Errorf("job %d not found", id)
}

// YuePlayFile — воспроизведение любого аудио-артефакта джобы (стем, превью,
// овердаб, DSP-вариант, основной трек) через встроенный pw-play плеер.
// durSec — подсказка длительности для полосы позиции (0 = неизвестно).
func (a *App) YuePlayFile(id int64, file string, durSec float64) error {
	if file == "" {
		return fmt.Errorf("empty file")
	}
	body, _, err := a.yue.FetchAudio(a.ctx, id, file)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(body)
	body.Close()
	if err != nil {
		return err
	}
	if err := pl.load(id, data, time.Duration(durSec*float64(time.Second))); err != nil {
		return err
	}
	return pl.play()
}

func (a *App) YueToggleAudio() {
	pl.toggle()
}

func (a *App) YueStopAudio() {
	pl.stop()
}

type YuePlayerState struct {
	Playing     bool    `json:"playing"`
	PositionSec float64 `json:"position_sec"`
	DurationSec float64 `json:"duration_sec"`
	JobID       int64   `json:"job_id"`
	Error       string  `json:"error"`
}

func (a *App) YueAudioState() YuePlayerState {
	playing, pos, dur, id := pl.state()
	return YuePlayerState{Playing: playing, PositionSec: pos.Seconds(), DurationSec: dur.Seconds(),
		JobID: id, Error: pl.lastError()}
}

func (a *App) YueOpenURL(url string) {
	runtime.BrowserOpenURL(a.ctx, url)
}

func (a *App) YueGetServerURL() string {
	return a.yue.GetURL()
}

func (a *App) YueSetServerURL(url string) {
	a.yue.SetURL(url)
}

// ---------- v2/v3: транскрипция, ролл, превью, овердаб, стемы, корпус ----------

// YueTranscribeFile — диалог выбора трека → SheetSage2 → ABC (для каверов).
func (a *App) YueTranscribeFile() (*yue.TranscribeResult, error) {
	src, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Трек для транскрипции (кавер)",
		Filters: []runtime.FileFilter{
			{DisplayName: "Аудио (*.flac; *.mp3; *.wav; *.ogg; *.m4a)", Pattern: "*.flac;*.mp3;*.wav;*.ogg;*.m4a"},
		},
	})
	if err != nil {
		return nil, err
	}
	if src == "" {
		return nil, nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return nil, err
	}
	return a.yue.Transcribe(a.ctx, filepath.Base(src), data)
}

func (a *App) YueJobScore(id int64) (map[string]any, error) {
	return a.yue.JobScore(a.ctx, id)
}

func (a *App) YueJobPreview(id int64, fromSec, toSec float64) (map[string]any, error) {
	return a.yue.JobPreview(a.ctx, id, fromSec, toSec)
}

func (a *App) YueSubmitOverdub(id int64, style string, gain float64) (int64, error) {
	return a.yue.SubmitOverdub(a.ctx, id, style, gain)
}

func (a *App) YueMakeStems(id int64) (map[string]any, error) {
	return a.yue.MakeStems(a.ctx, id)
}

func (a *App) YueJobStems(id int64) ([]map[string]any, error) {
	return a.yue.JobStems(a.ctx, id)
}

func (a *App) YueCorpusCreate(name string) (int64, error) {
	return a.yue.CorpusCreate(a.ctx, name)
}

// YueCorpusAddTracks — диалог выбора треков корпуса (по одному обрабатываются
// на воркере: DSP + SheetSage2 + Whisper). Возвращает число загруженных.
func (a *App) YueCorpusAddTracks(id int64) (int, error) {
	srcs, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Треки корпуса (3–10 одного исполнителя/периода)",
		Filters: []runtime.FileFilter{
			{DisplayName: "Аудио (*.flac; *.mp3; *.wav; *.ogg; *.m4a)", Pattern: "*.flac;*.mp3;*.wav;*.ogg;*.m4a"},
		},
	})
	if err != nil {
		return 0, err
	}
	loaded := 0
	var lastErr error
	for _, src := range srcs {
		data, err := os.ReadFile(src)
		if err != nil {
			lastErr = err
			continue
		}
		if _, err := a.yue.CorpusAddTrack(a.ctx, id, filepath.Base(src), data); err != nil {
			lastErr = err
			continue
		}
		loaded++
	}
	if loaded == 0 && lastErr != nil {
		return 0, lastErr
	}
	return loaded, nil
}

func (a *App) YueCorpusBuild(id int64) (map[string]any, error) {
	return a.yue.CorpusBuild(a.ctx, id)
}

func (a *App) YueCorpusList() ([]yue.Corpus, error) {
	return a.yue.CorpusList(a.ctx)
}

func (a *App) YueCorpusGet(id int64) (map[string]any, error) {
	return a.yue.CorpusGet(a.ctx, id)
}

func (a *App) YueCorpusTracks(id int64) ([]map[string]any, error) {
	return a.yue.CorpusTracks(a.ctx, id)
}

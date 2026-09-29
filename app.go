package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"yue-studio/internal/config"
	"yue-studio/internal/dsp"
	"yue-studio/internal/studio"
	"yue-studio/internal/yue"
)

const (
	// maxUploadBytes — потолок загрузки своего трека на воркер.
	maxUploadBytes = 200 << 20
	// fanLimits — границы веера best-of-N.
	fanMin, fanMax = 1, 10
	// previewSpan — превью DSP-цепочки: кусок трека с 20-й секунды.
	previewStartSec, previewDurSec = 20, 15
)

var audioFileFilter = []runtime.FileFilter{
	{DisplayName: "Аудио (*.flac; *.mp3; *.wav; *.ogg; *.m4a)", Pattern: "*.flac;*.mp3;*.wav;*.ogg;*.m4a"},
}

// App — биндинги Wails для фронтенда: тонкие обёртки над клиентом воркера
// плюс локальные операции (диалоги, ffmpeg-DSP, встроенный плеер).
type App struct {
	ctx    context.Context
	yue    yue.Service
	player Player
}

func NewApp(client *yue.Client, player Player) *App {
	return &App{yue: client, player: player}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// shutdown — окно закрыто: глушим плеер, иначе он играет сиротой.
func (a *App) shutdown(ctx context.Context) {
	a.player.Stop()
}

// pickAudioFile — диалог выбора аудио-файла; "" = отмена.
func (a *App) pickAudioFile(title string) (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   title,
		Filters: audioFileFilter,
	})
}

// readAudioFile — диалог + чтение выбранного файла с проверкой размера;
// (nil, nil) = отмена диалога.
func (a *App) readAudioFile(title string) ([]byte, string, error) {
	src, err := a.pickAudioFile(title)
	if err != nil || src == "" {
		return nil, "", err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return nil, "", err
	}
	if len(data) > maxUploadBytes {
		return nil, "", fmt.Errorf("файл слишком большой (>200 МБ)")
	}
	return data, filepath.Base(src), nil
}

// fetchTempFile скачивает артефакт джобы в новый temp-файл; удаление — на вызывающем (defer).
func (a *App) fetchTempFile(id int64, file, pattern string) (string, error) {
	return studio.FetchTemp(a.ctx, a.yue, id, file, "", pattern)
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
	if n < fanMin {
		n = fanMin
	}
	if n > fanMax {
		n = fanMax
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
	data, name, err := a.readAudioFile("Референс для сравнения метрик")
	if err != nil || data == nil {
		return nil, err
	}
	return a.yue.AddReference(a.ctx, name, data)
}

// YueDspChains — пресеты эффектов (крутилки с дефолтами).
func (a *App) YueDspChains() []dsp.Chain {
	return dsp.All()
}

// runDsp — общий конвейер DSP-варианта: скачать flac джобы, прогнать цепочку
// локальным ffmpeg (весь трек или кусок-превью), залить обратно на воркер.
func (a *App) runDsp(jobID int64, chainID string, params map[string]float64, preview bool) (*yue.DspVariant, error) {
	chain := dsp.ByID(chainID)
	if chain == nil {
		return nil, fmt.Errorf("unknown chain %q", chainID)
	}
	jobs, err := a.yue.Jobs(a.ctx)
	if err != nil {
		return nil, err
	}
	audio := ""
	for _, j := range jobs {
		if j.ID == jobID {
			audio = j.AudioFile
			break
		}
	}
	if audio == "" {
		return nil, fmt.Errorf("job %d has no audio", jobID)
	}
	tmpIn, err := a.fetchTempFile(jobID, audio, fmt.Sprintf("yue-dsp-%d-in-*.flac", jobID))
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmpIn)
	tmpOut, err := os.CreateTemp("", fmt.Sprintf("yue-dsp-%d-out-*.flac", jobID))
	if err != nil {
		return nil, err
	}
	tmpOut.Close()
	defer os.Remove(tmpOut.Name())

	var span *dsp.Span
	if preview {
		span = &dsp.Span{StartSec: previewStartSec, DurSec: previewDurSec}
	}
	if err := dsp.Run(tmpIn, tmpOut.Name(), chain.FilterGraph(params), span); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(tmpOut.Name())
	if err != nil {
		return nil, err
	}
	fname := fmt.Sprintf("dsp-%s.flac", chainID)
	if preview {
		fname = fmt.Sprintf("dsp-preview-%s.flac", chainID)
	}
	return a.yue.UploadDsp(a.ctx, jobID, fname, data)
}

// YueRebuildInserts — пересобрать трек джобы со всеми вклейками инструментов
// с чистого оригинала: партии встают в ритм по бочке трека (или по плану,
// если подгонка не уверена), громкость — дБ относительно оригинала.
// Результат — overdub-inst-<последняя партия>.flac + отчёт по вклейкам.
func (a *App) YueRebuildInserts(parentID int64, specs []studio.InsertSpec) (*studio.RebuildResult, error) {
	return studio.RebuildInserts(a.ctx, a.yue, parentID, specs)
}

// YueMixVocalsOver — родной вокал джобы (stem-vocals) поверх нового
// инструментального рендера (backingID): перелепка аранжировки вокального
// трека без удвоения голоса и без микса двух исполнений. Результат —
// dsp-with-vocal.flac у нового рендера.
func (a *App) YueMixVocalsOver(backingID, vocalJobID int64) (*yue.DspVariant, error) {
	backing, err := a.fetchTempFile(backingID, "audio.flac", fmt.Sprintf("yue-voc-%d-back-*.flac", backingID))
	if err != nil {
		return nil, err
	}
	defer os.Remove(backing)
	vocals, err := a.fetchTempFile(vocalJobID, "stem-vocals.flac", fmt.Sprintf("yue-voc-%d-stem-*.flac", vocalJobID))
	if err != nil {
		return nil, err
	}
	defer os.Remove(vocals)
	out, err := os.CreateTemp("", fmt.Sprintf("yue-voc-%d-out-*.flac", backingID))
	if err != nil {
		return nil, err
	}
	out.Close()
	defer os.Remove(out.Name())
	if err := dsp.RunTwoInputs(backing, vocals, out.Name(), dsp.VocalsOverGraph()); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(out.Name())
	if err != nil {
		return nil, err
	}
	return a.yue.UploadDsp(a.ctx, backingID, "dsp-with-vocal.flac", data)
}

// YueApplyDsp — применить цепочку к треку джобы и вернуть метрики варианта.
func (a *App) YueApplyDsp(jobID int64, chainID string, params map[string]float64) (*yue.DspVariant, error) {
	return a.runDsp(jobID, chainID, params, false)
}

// YueDspPreview — превью цепочки: кусок трека через те же эффекты.
// Файл кладётся как вариант dsp-preview-<chain>.flac и сразу проигрывается.
func (a *App) YueDspPreview(jobID int64, chainID string, params map[string]float64) (*yue.DspVariant, error) {
	return a.runDsp(jobID, chainID, params, true)
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

// YueOpenExternal — скачать артефакт во temp-файл и открыть приложением ОС.
func (a *App) YueOpenExternal(id int64, file string) error {
	tmp, err := a.fetchTempFile(id, file, fmt.Sprintf("yue-%d-*%s", id, filepath.Ext(file)))
	if err != nil {
		return err
	}
	// temp не удаляем: файл должен жить, пока пользователь его слушает
	return openExternal(tmp)
}

func (a *App) YueSaveAudio(id int64, file string) (string, error) {
	body, _, err := a.yue.FetchAudio(a.ctx, id, file)
	if err != nil {
		return "", err
	}
	defer func() { _ = body.Close() }()

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
		if j.ID != id {
			continue
		}
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
		return a.playFile(id, f, j.DurationSec)
	}
	return fmt.Errorf("job %d not found", id)
}

// YuePlayFile — воспроизведение любого аудио-артефакта джобы (стем, превью,
// овердаб, DSP-вариант, основной трек) через встроенный плеер.
// durSec — подсказка длительности для полосы позиции (0 = неизвестно).
func (a *App) YuePlayFile(id int64, file string, durSec float64) error {
	if file == "" {
		return fmt.Errorf("empty file")
	}
	return a.playFile(id, file, durSec)
}

func (a *App) playFile(id int64, file string, durSec float64) error {
	body, _, err := a.yue.FetchAudio(a.ctx, id, file)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(body)
	_ = body.Close()
	if err != nil {
		return err
	}
	if err := a.player.Load(id, data, time.Duration(durSec*float64(time.Second))); err != nil {
		return err
	}
	return a.player.Play()
}

func (a *App) YueToggleAudio() {
	a.player.Toggle()
}

func (a *App) YueStopAudio() {
	a.player.Stop()
}

type YuePlayerState struct {
	Playing     bool    `json:"playing"`
	PositionSec float64 `json:"position_sec"`
	DurationSec float64 `json:"duration_sec"`
	JobID       int64   `json:"job_id"`
	Error       string  `json:"error"`
}

func (a *App) YueAudioState() YuePlayerState {
	playing, pos, dur, id := a.player.State()
	return YuePlayerState{Playing: playing, PositionSec: pos.Seconds(), DurationSec: dur.Seconds(),
		JobID: id, Error: a.player.LastError()}
}

// YueSeekAudio — перемотка текущего трека к позиции (секунды).
func (a *App) YueSeekAudio(posSec float64) error {
	return a.player.Seek(time.Duration(posSec * float64(time.Second)))
}

// YueSetVolume — громкость встроенного плеера (0..1).
func (a *App) YueSetVolume(v float64) {
	a.player.SetVolume(v)
}

func (a *App) YueOpenURL(url string) {
	runtime.BrowserOpenURL(a.ctx, url)
}

func (a *App) YueGetServerURL() string {
	return a.yue.GetURL()
}

func (a *App) YueSetServerURL(url string) {
	a.yue.SetURL(url)
	config.SaveSettings(config.Settings{ServerURL: url})
}

// YueWorkerConfig — настройки воркера (Ollama и пр.) для попапа настроек.
func (a *App) YueWorkerConfig() (map[string]any, error) {
	return a.yue.WorkerConfig(a.ctx)
}

func (a *App) YueSetWorkerConfig(cfg map[string]any) error {
	return a.yue.SetWorkerConfig(a.ctx, cfg)
}

// YueOllamaModels — модели Ollama для выпадающего списка (url — кандидат).
func (a *App) YueOllamaModels(url string) (map[string]any, error) {
	return a.yue.OllamaModels(a.ctx, url)
}

// ---------- транскрипция, ролл, превью, овердаб, стемы, корпус ----------

// YueImportTrack — диалог выбора своего трека → импорт как джобы (статус done):
// в студии работают стемы, минус, эффекты, ролл (по транскрипции), овердаб.
func (a *App) YueImportTrack() (map[string]any, error) {
	data, title, err := a.readAudioFile("Импорт трека в студию")
	if err != nil || data == nil {
		return nil, err
	}
	return a.yue.ImportTrack(a.ctx, title, data, true)
}

// YueTranscribeFile — диалог выбора трека → SheetSage2 → ABC (для каверов).
func (a *App) YueTranscribeFile() (*yue.TranscribeResult, error) {
	data, name, err := a.readAudioFile("Трек для транскрипции (кавер)")
	if err != nil || data == nil {
		return nil, err
	}
	return a.yue.Transcribe(a.ctx, name, data)
}

// YueJobAbcText — текст ABC-артефакта джобы (score.abc / request.abc).
// Через Go, а не fetch из webview: у окна Wails нет origin воркера,
// относительный /audio/... туда не долетает.
func (a *App) YueJobAbcText(id int64, file string) (string, error) {
	body, _, err := a.yue.FetchAudio(a.ctx, id, file)
	if err != nil {
		return "", err
	}
	defer func() { _ = body.Close() }()
	b, err := io.ReadAll(io.LimitReader(body, 8<<20))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (a *App) YueJobScore(id int64) (map[string]any, error) {
	return a.yue.JobScore(a.ctx, id)
}

func (a *App) YueJobPreview(id int64, fromSec, toSec float64) (map[string]any, error) {
	return a.yue.JobPreview(a.ctx, id, fromSec, toSec)
}

// YueRecognizeLyricsFile — диалог выбора трека → faster-whisper → текст
// (оригинал для кавера: дальше адаптация-перевод или правка руками).
func (a *App) YueRecognizeLyricsFile() (*yue.LyricsResult, error) {
	data, name, err := a.readAudioFile("Трек для распознавания текста")
	if err != nil || data == nil {
		return nil, err
	}
	return a.yue.RecognizeLyrics(a.ctx, name, data)
}

// YueJobLyrics — текст из готового аудио джобы (whisper на воркере,
// без повторной загрузки файла).
func (a *App) YueJobLyrics(id int64) (*yue.LyricsResult, error) {
	return a.yue.JobLyrics(a.ctx, id)
}

// YueAdaptLyrics — адаптация-перевод лирики под пение (сохранение слогов).
func (a *App) YueAdaptLyrics(text, to string) (*yue.LyricsResult, error) {
	return a.yue.AdaptLyrics(a.ctx, text, to)
}

func (a *App) YueSubmitOverdub(id int64, style, lyrics string, gain float64, abc string, seed int64) (int64, error) {
	return a.yue.SubmitOverdub(a.ctx, id, style, lyrics, gain, abc, seed)
}

func (a *App) YueMakeStems(id int64) (map[string]any, error) {
	return a.yue.MakeStems(a.ctx, id)
}

// YueEnsureMp3 — конвертировать джобу в mp3 320, если ещё нет (импортные треки).
func (a *App) YueEnsureMp3(id int64) (map[string]any, error) {
	return a.yue.EnsureMp3(a.ctx, id)
}

// YueMakeMinus — минус-трек: микс стемов без выбранных групп.
func (a *App) YueMakeMinus(id int64, exclude []string) (map[string]any, error) {
	return a.yue.MakeMinus(a.ctx, id, exclude)
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
		Title:   "Треки корпуса (3–10 одного исполнителя/периода)",
		Filters: audioFileFilter,
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

// YueVoiceCreate — сохранить карточку голоса из джобы-прослушивания примерочной.
func (a *App) YueVoiceCreate(name string, jobID int64, params string, seed int64) (int64, error) {
	return a.yue.VoiceCreate(a.ctx, name, jobID, params, seed)
}

func (a *App) YueVoices() ([]yue.Voice, error) {
	return a.yue.Voices(a.ctx)
}

func (a *App) YueVoiceDelete(id int64) (bool, error) {
	return a.yue.VoiceDelete(a.ctx, id)
}

// YueDspVariantDelete — удалить вариант эффекта/вклейки.
func (a *App) YueDspVariantDelete(id int64, fname string) (bool, error) {
	return a.yue.DspVariantDelete(a.ctx, id, fname)
}

// YueVariantToTrack — вариант DSP-эффекта отдельным треком с подписью эффекта.
func (a *App) YueVariantToTrack(jobID int64, file, title string) (int64, error) {
	return a.yue.VariantToTrack(a.ctx, jobID, file, title)
}

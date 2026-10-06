package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
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

	// fx — кэш трека/стемов на ПК для быстрого превью эффектов (app_preview.go)
	fxOnce sync.Once
	fx     *studio.Cache
	fxDir  string
	fxMu   sync.Mutex
	fxDur  map[string]float64 // каталог превью → длина кусков, с
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
	a.fxCleanup()
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

// YueStats — сводка для футер-статусбара (VRAM, очередь, текущая джоба).
func (a *App) YueStats() (*yue.StatsInfo, error) {
	return a.yue.Stats(a.ctx)
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
// локальным ffmpeg, залить обратно на воркер. Превью — YueFxPreview (app_preview.go).
func (a *App) runDsp(jobID int64, chainID string, params map[string]float64) (*yue.DspVariant, error) {
	chain := dsp.ByID(chainID)
	if chain == nil {
		return nil, fmt.Errorf("unknown chain %q", chainID)
	}
	if chain.Key != "" {
		return nil, fmt.Errorf("эффект %q — только на дорожку (ключ — дорожка %s)", chain.Name, chain.Key)
	}
	return a.runGraph(jobID, chain.FilterGraph(params), fmt.Sprintf("dsp-%s.flac", chainID), "")
}

// runGraph — граф ffmpeg на весь трек джобы: скачать звук, прогнать локально,
// залить вариантом fname с подписью label ("" — по имени файла).
func (a *App) runGraph(jobID int64, graph, fname, label string) (*yue.DspVariant, error) {
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

	if err := dsp.Run(tmpIn, tmpOut.Name(), graph, nil); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(tmpOut.Name())
	if err != nil {
		return nil, err
	}
	return a.yue.UploadDsp(a.ctx, jobID, fname, label, data)
}

// YueRebuildSections — пересобрать трек джобы со всеми заменами дорожек:
// кусок перерендерен моделью целой группой, в треке меняются только нужные
// стемы (голос всегда родной), в ритм по бочке, громкость — дБ к старой дорожке.
// Результат — overdub-inst-<последний рендер>.flac + отчёт.
func (a *App) YueRebuildSections(parentID int64, specs []studio.SectionSpec) (*studio.RebuildResult, error) {
	return studio.RebuildSections(a.ctx, a.yue, parentID, specs)
}

// YueApplyDsp — применить цепочку к треку джобы и вернуть метрики варианта.
func (a *App) YueApplyDsp(jobID int64, chainID string, params map[string]float64) (*yue.DspVariant, error) {
	return a.runDsp(jobID, chainID, params)
}

// YueVolumeEnvelope — линия громкости по волне (точки время → дБ): stem "" —
// весь трек (вариант dsp-envelope.flac), иначе только дорожка через пересборку.
func (a *App) YueVolumeEnvelope(jobID int64, stem string, points []dsp.EnvPoint) (*yue.DspVariant, error) {
	return studio.VolumeEnvelope(a.ctx, a.yue, jobID, stem, points)
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

// YueTranscribeJob — повторить создание партитуры для трека без неё.
func (a *App) YueTranscribeJob(id int64) (map[string]any, error) {
	return a.yue.TranscribeJob(a.ctx, id)
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

// YueJobPeaks — огибающая громкости артефакта (волна в студии): file "" —
// основной трек, bins 0 — каноническое разрешение воркера.
func (a *App) YueJobPeaks(id int64, file string, bins int) (map[string]any, error) {
	return a.yue.JobPeaks(a.ctx, id, file, bins)
}

// YueJobSpectrumPNG — спектрограмма артефакта PNG; []byte уходит в JS
// как base64 (картинка для <img> с той же шкалой времени, что волна).
func (a *App) YueJobSpectrumPNG(id int64, file string) ([]byte, error) {
	return a.yue.JobSpectrum(a.ctx, id, file)
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
	return a.yue.JobLyrics(a.ctx, id, "") // язык — по стилю трека
}

// YueAdaptLyrics — адаптация-перевод лирики под пение (сохранение слогов).
func (a *App) YueAdaptLyrics(text, to string) (*yue.LyricsResult, error) {
	return a.yue.AdaptLyrics(a.ctx, text, to)
}

func (a *App) YueSubmitOverdub(id int64, style, lyrics string, gain float64, abc string, seed int64) (int64, error) {
	return a.yue.SubmitOverdub(a.ctx, id, style, lyrics, gain, abc, seed)
}

func (a *App) YueMakeStems(id int64) (map[string]any, error) {
	defer a.fxCache().Invalidate(id) // стемы на воркере переписаны — кэш превью устарел
	return a.yue.MakeStems(a.ctx, id)
}

// YueApplyFx — звуковой движок воркера: цепочка блоков на трек/дорожку → вариант dsp-fx-*.flac.
func (a *App) YueApplyFx(id int64, req yue.FxRequest) (*yue.DspVariant, error) {
	return a.yue.ApplyFx(a.ctx, id, req)
}

// YueFxAssets — захваты NAM и IR, загруженные на воркер.
func (a *App) YueFxAssets() (map[string]any, error) {
	return a.yue.FxAssets(a.ctx)
}

// YueUploadFxAsset — выбрать на ПК захват NAM (kind=amp, .nam) или IR (kind=ir, .wav) и загрузить
// на воркер; (nil, nil) — диалог отменён.
func (a *App) YueUploadFxAsset(kind string) (map[string]any, error) {
	ext := map[string]string{"amp": "*.nam", "ir": "*.wav"}[kind]
	if ext == "" {
		return nil, fmt.Errorf("kind: amp | ir")
	}
	src, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Загрузить на воркер",
		Filters: []runtime.FileFilter{{DisplayName: ext, Pattern: ext}},
	})
	if err != nil || src == "" {
		return nil, err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return nil, err
	}
	return a.yue.UploadFxAsset(a.ctx, kind, filepath.Base(src), data)
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

// YueContinueJob — «продолжение с места»: трек до fromSec + продолжение моделью.
func (a *App) YueContinueJob(jobID int64, fromSec float64, seed int64, abc, styleAdd string) (int64, error) {
	return a.yue.ContinueJob(a.ctx, jobID, fromSec, seed, abc, styleAdd)
}

// YueRenameJob — своё название трека вместо номера (номер при переносе меняется).
func (a *App) YueRenameJob(jobID int64, title string) (*yue.Job, error) {
	return a.yue.UpdateJob(a.ctx, jobID, &title, nil)
}

// YueRetryJob — «повторить» упавшую или отменённую джобу.
func (a *App) YueRetryJob(jobID int64) error {
	return a.yue.RetryJob(a.ctx, jobID)
}

// YueVoiceConvert — «голос альбома»: голос трека тембром образца (Seed-VC на
// воркере, отдельная установка); новая версия появится в очереди.
func (a *App) YueVoiceConvert(jobID int64, p yue.VoiceParams) (int64, error) {
	return a.yue.VoiceConvert(a.ctx, jobID, p)
}

// YueSetJobFolder — папка песни («Альбом», «Основы», …); "" — без папки.
func (a *App) YueSetJobFolder(jobID int64, folder string) (*yue.Job, error) {
	return a.yue.UpdateJob(a.ctx, jobID, nil, &folder)
}

// YueSetHead — основная версия песни (0 — сам трек).
func (a *App) YueSetHead(jobID, headID int64) error {
	return a.yue.SetHead(a.ctx, jobID, headID)
}

// YueVocalContour — высота голоса по тактам плана (сверка «спето ли по плану»).
func (a *App) YueVocalContour(jobID int64, from, to float64) (*yue.VocalContour, error) {
	return a.yue.VocalContour(a.ctx, jobID, from, to)
}

// YueSplice — склеить куски версий в вариант трека baseID (потом «→ в треки»).
func (a *App) YueSplice(baseID int64, parts []studio.SplicePart, crossfade float64) (*yue.DspVariant, error) {
	return studio.Splice(a.ctx, a.yue, baseID, parts, crossfade)
}

// YuePlanCheck — что изменилось в плане и где проблемы (потолок голоса и т.п.).
func (a *App) YuePlanCheck(jobID int64, abc string, fromSec float64) (*yue.PlanCheck, error) {
	return a.yue.PlanCheck(a.ctx, jobID, abc, fromSec)
}

// YueJobGrid — сетка долей (темп и сильная доля) для «Ритм-гейта».
func (a *App) YueJobGrid(jobID int64, from, to float64) (*yue.BeatGrid, error) {
	return a.yue.JobGrid(a.ctx, jobID, from, to)
}

// YueJobTones — узкие тона («свист») в окне трека — частота для «Убрать свист».
// stem — дорожка (vocals/drums/bass/other/guitar/piano), пусто — весь микс.
func (a *App) YueJobTones(jobID int64, from, to float64, stem string) ([]yue.Tone, error) {
	return a.yue.JobTones(a.ctx, jobID, from, to, stem)
}

// YueVariantToTrack — вариант DSP-эффекта отдельным треком с подписью эффекта.
// voiceSrc > 0 — версия с подставленным голосом этого рендера.
func (a *App) YueVariantToTrack(jobID int64, file, title string, voiceSrc int64) (int64, error) {
	return a.yue.VariantToTrack(a.ctx, jobID, file, title, voiceSrc)
}

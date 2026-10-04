package yue

import (
	"context"
	"io"
)

// Service — API воркера Yue Studio. Интерфейс нужен app-слою для тестирования.
type Service interface {
	Health(ctx context.Context) (*HealthInfo, error)
	Stats(ctx context.Context) (*StatsInfo, error)
	Jobs(ctx context.Context) ([]Job, error)
	Submit(ctx context.Context, p SubmitParams) (int64, error)
	Cancel(ctx context.Context, id int64) (bool, error)
	DeleteJob(ctx context.Context, id int64) (bool, error)
	Plan(ctx context.Context, p PlanParams) (*PlanResult, error)
	Copilot(ctx context.Context, p CopilotParams) (*CopilotResult, error)
	Translate(ctx context.Context, text string) (*TranslateResult, error)
	RecognizeLyrics(ctx context.Context, name string, data []byte) (*LyricsResult, error)
	JobLyrics(ctx context.Context, id int64, language string) (*LyricsResult, error)
	AdaptLyrics(ctx context.Context, text, to string) (*LyricsResult, error)
	AnalyzeJob(ctx context.Context, id int64) (map[string]any, error)
	References(ctx context.Context) ([]Reference, error)
	AddReference(ctx context.Context, name string, data []byte) (*Reference, error)
	JobDspVariants(ctx context.Context, id int64) ([]DspVariant, error)
	// UploadDsp — label: что сделано, для подписи в списках («Перегруз голоса · голос»); "" — по имени файла
	UploadDsp(ctx context.Context, id int64, fname, label string, data []byte) (*DspVariant, error)
	Transcribe(ctx context.Context, name string, data []byte) (*TranscribeResult, error)
	JobScore(ctx context.Context, id int64) (map[string]any, error)
	// JobPeaks — огибающая громкости артефакта (волна в студии): file "" —
	// основной трек, bins 0 — каноническое разрешение воркера (кэшируемое)
	JobPeaks(ctx context.Context, id int64, file string, bins int) (map[string]any, error)
	// JobSpectrum — спектрограмма артефакта PNG; ось X — 0..длительность,
	// та же шкала времени, что у волны
	JobSpectrum(ctx context.Context, id int64, file string) ([]byte, error)
	JobPreview(ctx context.Context, id int64, fromSec, toSec float64) (map[string]any, error)
	SubmitOverdub(ctx context.Context, id int64, style, lyrics string, gain float64, abc string, seed int64) (int64, error)
	MakeStems(ctx context.Context, id int64) (map[string]any, error)
	MakeMinus(ctx context.Context, id int64, exclude []string) (map[string]any, error)
	ImportTrack(ctx context.Context, name string, data []byte, transcribe bool) (map[string]any, error)
	EnsureMp3(ctx context.Context, id int64) (map[string]any, error)
	JobStems(ctx context.Context, id int64) ([]map[string]any, error)
	CorpusCreate(ctx context.Context, name string) (int64, error)
	CorpusAddTrack(ctx context.Context, id int64, name string, data []byte) (map[string]any, error)
	CorpusBuild(ctx context.Context, id int64) (map[string]any, error)
	CorpusList(ctx context.Context) ([]Corpus, error)
	CorpusGet(ctx context.Context, id int64) (map[string]any, error)
	CorpusTracks(ctx context.Context, id int64) ([]map[string]any, error)
	VoiceCreate(ctx context.Context, name string, jobID int64, params string, seed int64) (int64, error)
	Voices(ctx context.Context) ([]Voice, error)
	VoiceDelete(ctx context.Context, id int64) (bool, error)
	VariantToTrack(ctx context.Context, jobID int64, file, title string, voiceSrc int64) (int64, error)
	VocalContour(ctx context.Context, id int64, from, to float64) (*VocalContour, error)
	JobTones(ctx context.Context, id int64, from, to float64, stem string) ([]Tone, error)
	// JobGrid — сетка долей (темп и сильная доля) в окне [from, to) (to 0 — до конца).
	JobGrid(ctx context.Context, id int64, from, to float64) (*BeatGrid, error)
	PlanCheck(ctx context.Context, id int64, abc string, fromSec float64) (*PlanCheck, error)
	// ContinueJob — «продолжение с места»: новый трек-вложение = джоба до
	// fromSec + продолжение моделью (seed 0 — случайный, abc — изменённый план,
	// styleAdd — что изменить в звучании с этого места)
	ContinueJob(ctx context.Context, jobID int64, fromSec float64, seed int64, abc, styleAdd string) (int64, error)
	// SetHead — основная версия песни jobID: сам трек (headID 0) или потомок
	SetHead(ctx context.Context, jobID, headID int64) error
	// UpdateJob — подпись и папка трека (nil — поле не менять, folder "" —
	// убрать из папки); ответ — трек после правки
	UpdateJob(ctx context.Context, jobID int64, title, folder *string) (*Job, error)
	// RetryJob — упавшая или отменённая джоба снова в очередь с теми же параметрами.
	RetryJob(ctx context.Context, jobID int64) error
	// VoiceConvert — «голос альбома»: новая версия трека, голос спет тембром
	// образца (Seed-VC на воркере), музыка прежняя; ответ — id джобы в очереди
	VoiceConvert(ctx context.Context, jobID int64, p VoiceParams) (int64, error)
	DspVariantDelete(ctx context.Context, id int64, fname string) (bool, error)
	AudioURL(id int64, file string) string
	FetchAudio(ctx context.Context, id int64, file string) (io.ReadCloser, string, error)
	SetURL(baseURL string)
	GetURL() string
	WorkerConfig(ctx context.Context) (map[string]any, error)
	SetWorkerConfig(ctx context.Context, cfg map[string]any) error
	OllamaModels(ctx context.Context, url string) (map[string]any, error)
}

type Job struct {
	ID          int64   `json:"id"`
	Title       string  `json:"title"`
	Status      string  `json:"status"`
	Style       string  `json:"style"`
	Lyrics      string  `json:"lyrics"`
	Seed        int64   `json:"seed"`
	Cot         string  `json:"cot"`
	ReqAbc      string  `json:"req_abc"`
	Error       string  `json:"error"`
	DurationSec float64 `json:"duration_sec"`
	AudioFile   string  `json:"audio_file"`
	Mp3File     string  `json:"mp3_file"`
	WavFile     string  `json:"wav_file"`
	AbcFile     string  `json:"abc_file"`
	CreatedAt   string  `json:"created_at"`
	FinishedAt  string  `json:"finished_at"`
	// черновик (~18 с): пробы стилей и прослушивания голосов
	Draft bool `json:"draft,omitempty"`
	// производный трек: от какого трека (ParentID) и зачем (Role: section —
	// кусок для вклейки, rebuild — пересборка с приёмами, fragment — «проверить
	// кусок», variant — вариант эффекта треком); в списке прячется под родителем
	ParentID *int64 `json:"parent_id,omitempty"`
	Role     string `json:"role,omitempty"`
	// овердаб-партия: поверх какого трека (воркер микширует её в его вариант)
	OverdubOf *int64 `json:"overdub_of,omitempty"`
	// основная версия песни (у корня): её играет карточка и открывает студия
	HeadID *int64 `json:"head_id,omitempty"`
	// папка песни («Альбом», «Основы», …) — у корня; версии следуют за ним
	Folder string `json:"folder,omitempty"`
	// VocalLeak — где в треке «без голоса» звучит дорожка голоса: секунды начала
	// через запятую («25.3,40.1»); пусто — не проверяли или голоса нет.
	VocalLeak string `json:"vocal_leak,omitempty"`
	// Mixes — сколько у трека готовых миксов (overdub-inst-*: вклейки, эффекты на дорожки)
	Mixes int `json:"mixes,omitempty"`
	// источник голоса: рендер, чей голос звучит в этой версии (у версии,
	// созданной подстановкой голоса); пусто — голос свой или от родителя
	VoiceSrc *int64 `json:"voice_src,omitempty"`
	// характер исполнения: температура (смелость игры) и cfg (точность по
	// стилю/нотам); 0 — по умолчанию воркера
	Temperature float64 `json:"temperature,omitempty"`
	Cfg         float64 `json:"cfg,omitempty"`

	// живой прогресс (только у running-джоб; дополняется воркером поверх строки БД)
	Stage       string   `json:"stage,omitempty"`
	Tokens      int      `json:"tokens,omitempty"`
	TokPerSec   *float64 `json:"tok_per_s,omitempty"`
	ElapsedSec  float64  `json:"elapsed_s,omitempty"`
	ProgressPct *int     `json:"progress_pct,omitempty"`
}

type HealthInfo struct {
	Status      string `json:"status"`
	ModelLoaded bool   `json:"model_loaded"`
	LoadError   string `json:"load_error"`
}

// StatsRunning — идущая джоба в сводке /stats (нет рендера — nil).
type StatsRunning struct {
	JobID       int64   `json:"job_id"`
	Title       string  `json:"title"`
	Stage       string  `json:"stage"`
	ProgressPct *int    `json:"progress_pct"`
	TokPerS     float64 `json:"tok_per_s"`
}

// StatsInfo — сводка для статус-бара приложения (лёгкий опрос раз в пару секунд).
type StatsInfo struct {
	ModelLoaded bool          `json:"model_loaded"`
	VramTotal   *int          `json:"vram_total"` // МБ; nil — CPU-воркер/CUDA недоступна
	VramUsed    *int          `json:"vram_used"`
	Queue       map[string]int `json:"queue"`
	Running     *StatsRunning `json:"running"`
	GpuWaiting  int           `json:"gpu_waiting"`
}

type SubmitParams struct {
	Title  string `json:"title"`
	Style  string `json:"style"`
	Lyrics string `json:"lyrics"`
	Seed   int64  `json:"seed"`
	Cot    string `json:"cot"`
	Abc    string `json:"abc,omitempty"`
	// черновик ~40 с вместо полного трека (быстрое предпрослушивание стиля)
	Draft bool `json:"draft,omitempty"`
	// драматургия поверх плана: "" | build (нарастание) | wave (волна) |
	// burst (взрыв: пол-время на входе, breakdown, финал — голос на октаву выше)
	Arc string `json:"arc,omitempty"`
	// жёсткий потолок семантических токенов (селектор длительности); 0 = бюджет воркера
	MaxTokens int64 `json:"max_tokens,omitempty"`
	// производный трек: родитель и роль (см. Job.ParentID/Role)
	ParentID int64  `json:"parent_id,omitempty"`
	Role     string `json:"role,omitempty"`
	// характер исполнения: 0 — по умолчанию (у производного трека — как у родителя);
	// температура 0.5–1.5 (выше — смелее игра), cfg 1–4 (выше — точнее по стилю и нотам)
	Temperature float64 `json:"temperature,omitempty"`
	Cfg         float64 `json:"cfg,omitempty"`
}

type PlanParams struct {
	Style  string `json:"style"`
	Lyrics string `json:"lyrics"`
	Seed   int64  `json:"seed"`
	Cot    string `json:"cot"`
}

type PlanResult struct {
	Abc       string  `json:"abc"`
	Truncated bool    `json:"truncated"`
	Seconds   float64 `json:"seconds"`
	// сид плана (пустой в запросе — случайный): рендер с ним споёт так же
	Seed int64 `json:"seed,omitempty"`
}

type CopilotParams struct {
	Theme       string `json:"theme"`
	Style       string `json:"style"`
	Example     string `json:"example"`
	Lang        string `json:"lang"`
	Instruction string `json:"instruction,omitempty"`
}

type CopilotResult struct {
	Text    string  `json:"text"`
	Seconds float64 `json:"seconds"`
}

type TranslateResult struct {
	Text    string  `json:"text"`
	Seconds float64 `json:"seconds"`
}

// LyricsResult — текст лирики: whisper-распознавание или адаптация-перевод.
type LyricsResult struct {
	Text    string  `json:"text"`
	Seconds float64 `json:"seconds"`
}

type Reference struct {
	ID        string         `json:"id"`
	CreatedAt string         `json:"created_at"`
	Size      int64          `json:"size"`
	Metrics   map[string]any `json:"metrics"`
}

type DspVariant struct {
	File      string         `json:"file"`
	CreatedAt string         `json:"created_at"`
	Metrics   map[string]any `json:"metrics"`
	Label     string         `json:"label,omitempty"` // что сделано; пусто — понятно по имени файла
}

type TranscribeResult struct {
	ID       string         `json:"id"`
	Abc      string         `json:"abc"`
	Seconds  float64        `json:"seconds"`
	Warnings []string       `json:"warnings"`
	Stats    map[string]any `json:"stats"`
}

type Corpus struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	CreatedAt  string `json:"created_at"`
	Tracks     int    `json:"tracks"`
	HasProfile bool   `json:"has_profile"`
}

// Voice — карточка голоса примерочной: ручки (params, JSON-строка) + seed
// прослушивания; JobAlive — жива ли исходная джоба (иначе «переспросить»),
// HasAudio — есть ли копия аудио в voices/<id>/.
type Voice struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	JobID     int64  `json:"job_id"`
	Params    string `json:"params"`
	Seed      int64  `json:"seed"`
	CreatedAt string `json:"created_at"`
	JobAlive  bool   `json:"job_alive"`
	HasAudio  bool   `json:"has_audio"`
}

// VocalContour — высота голоса по тактам плана (стем vocals): сверка «спето ли
// по плану» и не ушёл ли голос выше потолка.
type VocalContour struct {
	Bars     []ContourBar `json:"bars"`
	MedianHz float64      `json:"median_hz"`
	LowHz    float64      `json:"low_hz"`  // 5-й перцентиль
	HighHz   float64      `json:"high_hz"` // 95-й перцентиль
}

// ContourBar — такт голоса Vocal: ноты по четвертям («A3», «·» — нет голоса).
type ContourBar struct {
	Index int      `json:"index"`
	Start float64  `json:"start"`
	End   float64  `json:"end"`
	Notes []string `json:"notes"`
}

// BeatGrid — сетка долей трека (POST /jobs/{id}/grid) для эффектов в такт:
// темп, время сильной доли (с, внутри запрошенного окна), насколько удары
// ложатся на сетку (0…1) и источник — дорожка барабанов или микс.
type BeatGrid struct {
	BPM      float64 `json:"bpm"`
	Offset   float64 `json:"offset"`
	Strength float64 `json:"strength"`
	Source   string  `json:"source"`
}

// Tone — узкий устойчивый пик спектра («свист»): частота и насколько он
// выше окрестности, дБ. Для эффекта «Убрать свист».
type Tone struct {
	Hz           float64 `json:"hz"`
	ProminenceDb float64 `json:"prominence_db"`
}

// PlanCheck — сравнение изменённого плана с планом джобы (POST /jobs/{id}/plan_check):
// что изменилось и где проблемы (потолок голоса, правки до отметки, сдвиг тактов).
type PlanCheck struct {
	Bars         map[string][2]int `json:"bars"`     // голос → [тактов было, стало]
	Duration     [2]float64        `json:"duration"` // длина плана было/стало, с
	Changed      []PlanChange      `json:"changed"`
	ChangedTotal int               `json:"changed_total"`
	Ceiling      PlanCeiling       `json:"ceiling"`
	Warnings     []string          `json:"warnings"`
}

// PlanChange — изменённый такт: время по новому плану, текст до/после.
type PlanChange struct {
	Voice  string  `json:"voice"`
	Bar    int     `json:"bar"`
	Start  float64 `json:"start"`
	End    float64 `json:"end"`
	Before string  `json:"before"`
	After  string  `json:"after"`
}

// PlanCeiling — верх мелодии голоса было/стало и потолок (верх + 2 ступени).
type PlanCeiling struct {
	Top     string `json:"top"`
	Ceiling string `json:"ceiling"`
	NewTop  string `json:"new_top"`
}

// VoiceParams — образец голоса для VoiceConvert: трек RefJobID, окно его дорожки
// голоса [RefFrom, RefFrom+RefDur) (3–30 с), шаги диффузии Steps (10–100).
// Нули — значения воркера по умолчанию: окно 25 с там, где голос звучит
// плотнее всего (RefFrom 0 = подобрать само), 50 шагов.
type VoiceParams struct {
	RefJobID int64   `json:"ref_job_id"`
	RefFrom  float64 `json:"ref_from,omitempty"`
	RefDur   float64 `json:"ref_dur,omitempty"`
	Steps    int     `json:"steps,omitempty"`
	Title    string  `json:"title,omitempty"`
}

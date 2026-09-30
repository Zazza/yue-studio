package yue

import (
	"context"
	"io"
)

// Service — API воркера Yue Studio. Интерфейс нужен app-слою для тестирования.
type Service interface {
	Health(ctx context.Context) (*HealthInfo, error)
	Jobs(ctx context.Context) ([]Job, error)
	Submit(ctx context.Context, p SubmitParams) (int64, error)
	Cancel(ctx context.Context, id int64) (bool, error)
	DeleteJob(ctx context.Context, id int64) (bool, error)
	Plan(ctx context.Context, p PlanParams) (*PlanResult, error)
	Copilot(ctx context.Context, p CopilotParams) (*CopilotResult, error)
	Translate(ctx context.Context, text string) (*TranslateResult, error)
	RecognizeLyrics(ctx context.Context, name string, data []byte) (*LyricsResult, error)
	JobLyrics(ctx context.Context, id int64) (*LyricsResult, error)
	AdaptLyrics(ctx context.Context, text, to string) (*LyricsResult, error)
	AnalyzeJob(ctx context.Context, id int64) (map[string]any, error)
	References(ctx context.Context) ([]Reference, error)
	AddReference(ctx context.Context, name string, data []byte) (*Reference, error)
	JobDspVariants(ctx context.Context, id int64) ([]DspVariant, error)
	UploadDsp(ctx context.Context, id int64, fname string, data []byte) (*DspVariant, error)
	Transcribe(ctx context.Context, name string, data []byte) (*TranscribeResult, error)
	JobScore(ctx context.Context, id int64) (map[string]any, error)
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
	// ContinueJob — «продолжение с места»: новый трек-вложение = джоба до
	// fromSec + продолжение моделью (seed 0 — случайный, abc — изменённый план,
	// styleAdd — что изменить в звучании с этого места)
	ContinueJob(ctx context.Context, jobID int64, fromSec float64, seed int64, abc, styleAdd string) (int64, error)
	// SetHead — основная версия песни jobID: сам трек (headID 0) или потомок
	SetHead(ctx context.Context, jobID, headID int64) error
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
	// основная версия песни (у корня): её играет карточка и открывает студия
	HeadID *int64 `json:"head_id,omitempty"`
	// источник голоса: рендер, чей голос звучит в этой версии (у версии,
	// созданной подстановкой голоса); пусто — голос свой или от родителя
	VoiceSrc *int64 `json:"voice_src,omitempty"`

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

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
	AnalyzeJob(ctx context.Context, id int64) (map[string]any, error)
	References(ctx context.Context) ([]Reference, error)
	AddReference(ctx context.Context, name string, data []byte) (*Reference, error)
	JobDspVariants(ctx context.Context, id int64) ([]DspVariant, error)
	UploadDsp(ctx context.Context, id int64, fname string, data []byte) (*DspVariant, error)
	Transcribe(ctx context.Context, name string, data []byte) (*TranscribeResult, error)
	JobScore(ctx context.Context, id int64) (map[string]any, error)
	JobPreview(ctx context.Context, id int64, fromSec, toSec float64) (map[string]any, error)
	SubmitOverdub(ctx context.Context, id int64, style string, gain float64) (int64, error)
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

	// живой прогресс (только у running-джоб; дополняется воркером поверх строки БД)
	Stage      string   `json:"stage,omitempty"`
	Tokens     int      `json:"tokens,omitempty"`
	TokPerSec  *float64 `json:"tok_per_s,omitempty"`
	ElapsedSec float64  `json:"elapsed_s,omitempty"`
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

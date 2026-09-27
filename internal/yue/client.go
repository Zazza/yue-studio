package yue

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	requestTimeout = 30 * time.Second
	// Заголовки тяжёлых запросов (corpus/track: DSP+SheetSage2+Whisper,
	// стриминг аудио) могут идти минутами — транспорт ждёт дольше,
	// короткие GET всё равно ограничены контекстом requestTimeout.
	headerTimeout = 15 * time.Minute
	// План грузит модель при холодном старте и генерирует ABC на GPU — минуты.
	planTimeout = 10 * time.Minute
	// Копайтер: Ollama грузит 9 ГБ модель с диска + генерация текста.
	copilotTimeout = 6 * time.Minute
)

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
	Theme   string `json:"theme"`
	Style   string `json:"style"`
	Example string `json:"example"`
	Lang    string `json:"lang"`
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
}

type Client struct {
	mu      sync.RWMutex
	baseURL string
	hc      *http.Client
}

var _ Service = (*Client)(nil)

func New(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		hc: &http.Client{
			// Без глобального таймаута: долгие вызовы (plan, стриминг аудио)
			// получают дедлайн per-request через контекст.
			Transport: &http.Transport{
				DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
				MaxIdleConns:          10,
				MaxIdleConnsPerHost:   10,
				IdleConnTimeout:       120 * time.Second,
				ResponseHeaderTimeout: headerTimeout,
				Proxy:                 nil,
			},
		},
	}
}

func (c *Client) SetURL(baseURL string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.baseURL = baseURL
}

func (c *Client) GetURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baseURL
}

func (c *Client) AudioURL(id int64, file string) string {
	return fmt.Sprintf("%s/audio/%d/%s", c.GetURL(), id, file)
}

func (c *Client) FetchAudio(ctx context.Context, id int64, file string) (io.ReadCloser, string, error) {
	if !validFile(file) {
		return nil, "", fmt.Errorf("bad filename")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.AudioURL(id, file), nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		resp.Body.Close()
		return nil, "", fmt.Errorf("yue audio %d/%s: %s: %s", id, file, resp.Status, string(b))
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" || ct == "application/octet-stream" {
		ct = contentTypeByExt(file)
	}
	resp.Header.Set("Content-Type", ct)
	return resp.Body, ct, nil
}

// FetchAudioReq выполняет запрос к артефакту как есть и возвращает сырой ответ
// (с Range-заголовком из r, статусом 200/206 и заголовками Content-Range/Length).
func (c *Client) FetchAudioReq(ctx context.Context, id int64, file, rangeHeader string) (*http.Response, error) {
	if !validFile(file) {
		return nil, fmt.Errorf("bad filename")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.AudioURL(id, file), nil)
	if err != nil {
		return nil, err
	}
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		resp.Body.Close()
		return nil, fmt.Errorf("yue audio %d/%s: %s: %s", id, file, resp.Status, string(b))
	}
	if ct := resp.Header.Get("Content-Type"); ct == "" || ct == "application/octet-stream" {
		resp.Header.Set("Content-Type", contentTypeByExt(file))
	}
	return resp, nil
}

func validFile(file string) bool {
	if file == "" || strings.Contains(file, "/") || strings.Contains(file, "..") {
		return false
	}
	switch filepath.Ext(file) {
	case ".flac", ".mp3", ".wav", ".abc", ".json", ".npy":
		return true
	}
	return false
}

func contentTypeByExt(file string) string {
	switch filepath.Ext(file) {
	case ".flac":
		return "audio/flac"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".abc":
		return "text/plain; charset=utf-8"
	case ".json":
		return "application/json"
	}
	return "application/octet-stream"
}

func (c *Client) Health(ctx context.Context) (*HealthInfo, error) {
	var info HealthInfo
	if err := c.get(ctx, "/health", &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (c *Client) Jobs(ctx context.Context) ([]Job, error) {
	var jobs []Job
	if err := c.get(ctx, "/jobs", &jobs); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (c *Client) Submit(ctx context.Context, p SubmitParams) (int64, error) {
	body, err := json.Marshal(p)
	if err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.GetURL()+"/jobs", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return 0, fmt.Errorf("yue submit: %s: %s", resp.Status, string(b))
	}
	var out struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, err
	}
	return out.ID, nil
}

func (c *Client) Plan(ctx context.Context, p PlanParams) (*PlanResult, error) {
	body, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, planTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.GetURL()+"/plan", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("yue plan: %s: %s", resp.Status, string(b))
	}
	var out PlanResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) AnalyzeJob(ctx context.Context, id int64) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, planTimeout) // librosa-анализ может занять десятки секунд
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/jobs/%d/analyze", c.GetURL(), id), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("yue analyze %d: %s: %s", id, resp.Status, string(b))
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) References(ctx context.Context) ([]Reference, error) {
	var refs []Reference
	if err := c.get(ctx, "/references", &refs); err != nil {
		return nil, err
	}
	return refs, nil
}

func (c *Client) JobDspVariants(ctx context.Context, id int64) ([]DspVariant, error) {
	var vs []DspVariant
	if err := c.get(ctx, fmt.Sprintf("/jobs/%d/dsp", id), &vs); err != nil {
		return nil, err
	}
	return vs, nil
}

func (c *Client) UploadDsp(ctx context.Context, id int64, fname string, data []byte) (*DspVariant, error) {
	ctx, cancel := context.WithTimeout(ctx, planTimeout) // включает librosa-замер на воркере
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/jobs/%d/dsp", c.GetURL(), id), bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Filename", fname)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("yue upload dsp %d/%s: %s: %s", id, fname, resp.Status, string(b))
	}
	var out DspVariant
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) AddReference(ctx context.Context, name string, data []byte) (*Reference, error) {
	ctx, cancel := context.WithTimeout(ctx, planTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.GetURL()+"/references", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Filename", name)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("yue add reference: %s: %s", resp.Status, string(b))
	}
	var out Reference
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Copilot(ctx context.Context, p CopilotParams) (*CopilotResult, error) {
	body, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, copilotTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.GetURL()+"/copilot", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("yue copilot: %s: %s", resp.Status, string(b))
	}
	var out CopilotResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Transcribe(ctx context.Context, name string, data []byte) (*TranscribeResult, error) {
	// SheetSage2 транскрибирует трек за секунды, но первый вызов грузит модель
	ctx, cancel := context.WithTimeout(ctx, copilotTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.GetURL()+"/transcribe", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Filename", name)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("yue transcribe: %s: %s", resp.Status, string(b))
	}
	var out TranscribeResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) JobScore(ctx context.Context, id int64) (map[string]any, error) {
	var out map[string]any
	if err := c.get(ctx, fmt.Sprintf("/jobs/%d/score", id), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) JobPreview(ctx context.Context, id int64, fromSec, toSec float64) (map[string]any, error) {
	body, _ := json.Marshal(map[string]float64{"from_sec": fromSec, "to_sec": toSec})
	ctx, cancel := context.WithTimeout(ctx, planTimeout) // VAE-decode куска — десятки секунд
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/jobs/%d/preview", c.GetURL(), id), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("yue preview %d: %s: %s", id, resp.Status, string(b))
	}
	var out map[string]any
	return out, json.NewDecoder(resp.Body).Decode(&out)
}

func (c *Client) SubmitOverdub(ctx context.Context, id int64, style string, gain float64) (int64, error) {
	body, _ := json.Marshal(map[string]any{"style": style, "gain": gain})
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/jobs/%d/overdub", c.GetURL(), id), bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return 0, fmt.Errorf("yue overdub %d: %s: %s", id, resp.Status, string(b))
	}
	var out struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, err
	}
	return out.ID, nil
}

func (c *Client) MakeStems(ctx context.Context, id int64) (map[string]any, error) {
	// demucs ~2 с на минуту аудио, но грузит модель при первом вызове
	ctx, cancel := context.WithTimeout(ctx, planTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/jobs/%d/stems", c.GetURL(), id), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("yue stems %d: %s: %s", id, resp.Status, string(b))
	}
	var out map[string]any
	return out, json.NewDecoder(resp.Body).Decode(&out)
}

func (c *Client) JobStems(ctx context.Context, id int64) ([]map[string]any, error) {
	var out []map[string]any
	if err := c.get(ctx, fmt.Sprintf("/jobs/%d/stems", id), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) CorpusCreate(ctx context.Context, name string) (int64, error) {
	body, _ := json.Marshal(map[string]string{"name": name})
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.GetURL()+"/corpus", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return 0, fmt.Errorf("yue corpus create: %s: %s", resp.Status, string(b))
	}
	var out struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, err
	}
	return out.ID, nil
}

// CorpusAddTrack — трек корпуса: DSP-паспорт + транскрипция + Whisper (минуты).
func (c *Client) CorpusAddTrack(ctx context.Context, id int64, name string, data []byte) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/corpus/%d/track", c.GetURL(), id), bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Filename", name)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("yue corpus track %d: %s: %s", id, resp.Status, string(b))
	}
	var out map[string]any
	return out, json.NewDecoder(resp.Body).Decode(&out)
}

func (c *Client) CorpusBuild(ctx context.Context, id int64) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, copilotTimeout) // агрегация + Ollama-строка
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/corpus/%d/build", c.GetURL(), id), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("yue corpus build %d: %s: %s", id, resp.Status, string(b))
	}
	var out map[string]any
	return out, json.NewDecoder(resp.Body).Decode(&out)
}

func (c *Client) CorpusList(ctx context.Context) ([]Corpus, error) {
	var out []Corpus
	if err := c.get(ctx, "/corpus", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) CorpusGet(ctx context.Context, id int64) (map[string]any, error) {
	var out map[string]any
	if err := c.get(ctx, fmt.Sprintf("/corpus/%d", id), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) Translate(ctx context.Context, text string) (*TranslateResult, error) {
	body, err := json.Marshal(map[string]string{"text": text, "to": "English"})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, copilotTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.GetURL()+"/translate", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("yue translate: %s: %s", resp.Status, string(b))
	}
	var out TranslateResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CorpusTracks(ctx context.Context, id int64) ([]map[string]any, error) {
	var out []map[string]any
	if err := c.get(ctx, fmt.Sprintf("/corpus/%d/tracks", id), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) Cancel(ctx context.Context, id int64) (bool, error) {	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/jobs/%d/cancel", c.GetURL(), id), nil)
	if err != nil {
		return false, err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return false, fmt.Errorf("yue cancel: %s: %s", resp.Status, string(b))
	}
	var out struct {
		Canceled bool `json:"canceled"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return false, err
	}
	return out.Canceled, nil
}

func (c *Client) DeleteJob(ctx context.Context, id int64) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		fmt.Sprintf("%s/jobs/%d", c.GetURL(), id), nil)
	if err != nil {
		return false, err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return false, fmt.Errorf("yue delete %d: %s: %s", id, resp.Status, string(b))
	}
	var out struct {
		Deleted bool `json:"deleted"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return false, err
	}
	return out.Deleted, nil
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.GetURL()+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("yue %s: %s: %s", path, resp.Status, string(b))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

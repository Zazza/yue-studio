package yue

import (
	"context"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strconv"
)

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
	var out struct {
		ID int64 `json:"id"`
	}
	if err := c.postJSON(ctx, "/jobs", p, requestTimeout, &out); err != nil {
		return 0, err
	}
	return out.ID, nil
}

func (c *Client) Cancel(ctx context.Context, id int64) (bool, error) {
	var out struct {
		Canceled bool `json:"canceled"`
	}
	if err := c.post(ctx, fmt.Sprintf("/jobs/%d/cancel", id), requestTimeout, &out); err != nil {
		return false, err
	}
	return out.Canceled, nil
}

func (c *Client) DeleteJob(ctx context.Context, id int64) (bool, error) {
	var out struct {
		Deleted bool `json:"deleted"`
	}
	if err := c.del(ctx, fmt.Sprintf("/jobs/%d", id), &out); err != nil {
		return false, err
	}
	return out.Deleted, nil
}

// AnalyzeJob — метрики трека джобы (librosa-анализ может занять десятки секунд).
func (c *Client) AnalyzeJob(ctx context.Context, id int64) (map[string]any, error) {
	var out map[string]any
	if err := c.post(ctx, fmt.Sprintf("/jobs/%d/analyze", id), planTimeout, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) JobScore(ctx context.Context, id int64) (map[string]any, error) {
	var out map[string]any
	if err := c.get(ctx, fmt.Sprintf("/jobs/%d/score", id), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// JobPeaks — огибающая громкости артефакта для волны в студии: [min, max] по
// окнам. file "" — основной трек джобы, bins 0 — каноническое разрешение
// воркера (только оно кэшируется на его стороне).
func (c *Client) JobPeaks(ctx context.Context, id int64, file string, bins int) (map[string]any, error) {
	if file != "" && !validFile(file) {
		return nil, fmt.Errorf("yue: bad artifact name %q", file)
	}
	path := fmt.Sprintf("/jobs/%d/peaks", id)
	q := neturl.Values{}
	if file != "" {
		q.Set("file", file)
	}
	if bins > 0 {
		q.Set("bins", strconv.Itoa(bins))
	}
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	var out map[string]any
	if err := c.get(ctx, path, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// JobSpectrum — спектрограмма артефакта готовой картинкой PNG с воркера.
func (c *Client) JobSpectrum(ctx context.Context, id int64, file string) ([]byte, error) {
	if file != "" && !validFile(file) {
		return nil, fmt.Errorf("yue: bad artifact name %q", file)
	}
	path := fmt.Sprintf("/jobs/%d/spectrum.png", id)
	if file != "" {
		path += "?file=" + neturl.QueryEscape(file)
	}
	return c.getBytes(ctx, path)
}

// JobPreview — превью фрагмента: VAE-decode куска латентов, десятки секунд.
func (c *Client) JobPreview(ctx context.Context, id int64, fromSec, toSec float64) (map[string]any, error) {
	var out map[string]any
	body := map[string]float64{"from_sec": fromSec, "to_sec": toSec}
	if err := c.postJSON(ctx, fmt.Sprintf("/jobs/%d/preview", id), body, planTimeout, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SubmitOverdub — рендер партии по партитуре джобы с новым стилем + микс.
// lyrics — необязательный текст голосовой партии (без него модель поёт
// импровизированный вокализ, часто несуразный). abc — свой план партии
// (пусто = партитура джобы): так «+ инструмент» локализует звук куском плана.
func (c *Client) SubmitOverdub(ctx context.Context, id int64, style, lyrics string, gain float64, abc string, seed int64) (int64, error) {
	var out struct {
		ID int64 `json:"id"`
	}
	body := map[string]any{"style": style, "lyrics": lyrics, "gain": gain}
	if seed != 0 {
		body["seed"] = seed // сид родителя = «та же интерпретация»
	}
	if abc != "" {
		body["abc"] = abc
	}
	if err := c.postJSON(ctx, fmt.Sprintf("/jobs/%d/overdub", id), body, requestTimeout, &out); err != nil {
		return 0, err
	}
	return out.ID, nil
}

// ImportTrack — импорт внешнего трека как джобы: дальше работают стемы/минус/
// эффекты/ролл/овердаб. Транскрипция (для ролла) — опционально.
func (c *Client) ImportTrack(ctx context.Context, name string, data []byte, transcribe bool) (map[string]any, error) {
	var out map[string]any
	path := "/tracks/import?transcribe=" + strconv.FormatBool(transcribe)
	// транскрипция может идти минутами
	if err := c.postRaw(ctx, path, name, data, gpuTimeout, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// EnsureMp3 — ленивая конвертация джобы в mp3 320 (для импортированных треков).
func (c *Client) EnsureMp3(ctx context.Context, id int64) (map[string]any, error) {
	var out map[string]any
	if err := c.post(ctx, fmt.Sprintf("/jobs/%d/mp3", id), requestTimeout, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// FetchAudio скачивает артефакт джобы; возвращает тело и Content-Type.
func (c *Client) FetchAudio(ctx context.Context, id int64, file string) (io.ReadCloser, string, error) {
	resp, err := c.fetchAudioResp(ctx, id, file, "")
	if err != nil {
		return nil, "", err
	}
	return resp.Body, resp.Header.Get("Content-Type"), nil
}

// FetchAudioReq выполняет запрос к артефакту как есть и возвращает сырой ответ
// (с Range-заголовком из r, статусом 200/206 и заголовками Content-Range/Length).
func (c *Client) FetchAudioReq(ctx context.Context, id int64, file, rangeHeader string) (*http.Response, error) {
	return c.fetchAudioResp(ctx, id, file, rangeHeader)
}

func (c *Client) fetchAudioResp(ctx context.Context, id int64, file, rangeHeader string) (*http.Response, error) {
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
		_ = resp.Body.Close()
		return nil, &StatusError{Code: resp.StatusCode, Msg: fmt.Sprintf("yue audio %d/%s: %s: %s", id, file, resp.Status, string(b))}
	}
	if ct := resp.Header.Get("Content-Type"); ct == "" || ct == "application/octet-stream" {
		resp.Header.Set("Content-Type", contentTypeByExt(file))
	}
	return resp, nil
}

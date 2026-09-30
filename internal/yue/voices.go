package yue

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// VoiceCreate — карточка голоса из джобы-прослушивания примерочной:
// воркер копирует аудио в voices/<id>/, так что карточка переживает удаление джоб.
func (c *Client) VoiceCreate(ctx context.Context, name string, jobID int64, params string, seed int64) (int64, error) {
	var out struct {
		ID int64 `json:"id"`
	}
	body := map[string]any{"name": name, "job_id": jobID, "params": params, "seed": seed}
	if err := c.postJSON(ctx, "/voices", body, requestTimeout, &out); err != nil {
		return 0, err
	}
	return out.ID, nil
}

func (c *Client) Voices(ctx context.Context) ([]Voice, error) {
	var out []Voice
	if err := c.get(ctx, "/voices", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) VoiceDelete(ctx context.Context, id int64) (bool, error) {
	var out struct {
		Deleted bool `json:"deleted"`
	}
	if err := c.del(ctx, fmt.Sprintf("/voices/%d", id), &out); err != nil {
		return false, err
	}
	return out.Deleted, nil
}

// VariantToTrack — вариант DSP-эффекта отдельным треком (копия с подписью).
// voiceSrc > 0 — у версии подставлен голос этого рендера (источник голоса
// для следующих «перепеть с места»); 0 — не менялся.
func (c *Client) VariantToTrack(ctx context.Context, jobID int64, file, title string, voiceSrc int64) (int64, error) {
	var out struct {
		ID int64 `json:"id"`
	}
	body := map[string]any{"file": file, "title": title}
	if voiceSrc > 0 {
		body["voice_src"] = voiceSrc
	}
	if err := c.postJSON(ctx, fmt.Sprintf("/jobs/%d/variant_track", jobID), body, requestTimeout, &out); err != nil {
		return 0, err
	}
	return out.ID, nil
}

// VocalContour — высота голоса по тактам (GET /jobs/{id}/vocal_contour);
// to ≤ 0 — до конца трека. pyin на всём треке — десятки секунд.
func (c *Client) VocalContour(ctx context.Context, id int64, from, to float64) (*VocalContour, error) {
	var out VocalContour
	path := fmt.Sprintf("/jobs/%d/vocal_contour?from=%g&to=%g", id, from, to)
	if err := c.call(ctx, http.MethodGet, path, planTimeout, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// JobTones — узкие тона («свист») трека (GET /jobs/{id}/tones); to ≤ 0 — до
// конца; stem — дорожка (vocals/drums/bass/other), пусто — весь микс.
func (c *Client) JobTones(ctx context.Context, id int64, from, to float64, stem string) ([]Tone, error) {
	var out []Tone
	path := fmt.Sprintf("/jobs/%d/tones?from=%g&to=%g", id, from, to)
	if stem != "" {
		path += "&stem=" + url.QueryEscape(stem)
	}
	if err := c.call(ctx, http.MethodGet, path, planTimeout, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ContinueJob — «продолжение с места» (POST /jobs/{id}/continue).
func (c *Client) ContinueJob(ctx context.Context, jobID int64, fromSec float64, seed int64, abc, styleAdd string) (int64, error) {
	var out struct {
		ID int64 `json:"id"`
	}
	body := map[string]any{"from_sec": fromSec}
	if seed > 0 {
		body["seed"] = seed
	}
	if abc != "" {
		body["abc"] = abc
	}
	if styleAdd != "" {
		body["style_add"] = styleAdd
	}
	if err := c.postJSON(ctx, fmt.Sprintf("/jobs/%d/continue", jobID), body, requestTimeout, &out); err != nil {
		return 0, err
	}
	return out.ID, nil
}

// SetHead — основная версия песни (POST /jobs/{id}/head).
func (c *Client) SetHead(ctx context.Context, jobID, headID int64) error {
	var out map[string]any
	return c.postJSON(ctx, fmt.Sprintf("/jobs/%d/head", jobID), map[string]any{"head_id": headID}, requestTimeout, &out)
}

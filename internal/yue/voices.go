package yue

import (
	"context"
	"fmt"
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
func (c *Client) VariantToTrack(ctx context.Context, jobID int64, file, title string) (int64, error) {
	var out struct {
		ID int64 `json:"id"`
	}
	body := map[string]any{"file": file, "title": title}
	if err := c.postJSON(ctx, fmt.Sprintf("/jobs/%d/variant_track", jobID), body, requestTimeout, &out); err != nil {
		return 0, err
	}
	return out.ID, nil
}

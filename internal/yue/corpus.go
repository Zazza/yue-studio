package yue

import (
	"context"
	"fmt"
)

func (c *Client) CorpusCreate(ctx context.Context, name string) (int64, error) {
	var out struct {
		ID int64 `json:"id"`
	}
	if err := c.postJSON(ctx, "/corpus", map[string]string{"name": name}, requestTimeout, &out); err != nil {
		return 0, err
	}
	return out.ID, nil
}

// CorpusAddTrack — трек корпуса: DSP-паспорт + транскрипция + Whisper (минуты).
func (c *Client) CorpusAddTrack(ctx context.Context, id int64, name string, data []byte) (map[string]any, error) {
	var out map[string]any
	if err := c.postRaw(ctx, fmt.Sprintf("/corpus/%d/track", id), name, data, gpuTimeout, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CorpusBuild — агрегация профиля + строка стиля через Ollama.
func (c *Client) CorpusBuild(ctx context.Context, id int64) (map[string]any, error) {
	var out map[string]any
	if err := c.post(ctx, fmt.Sprintf("/corpus/%d/build", id), copilotTimeout, &out); err != nil {
		return nil, err
	}
	return out, nil
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

func (c *Client) CorpusTracks(ctx context.Context, id int64) ([]map[string]any, error) {
	var out []map[string]any
	if err := c.get(ctx, fmt.Sprintf("/corpus/%d/tracks", id), &out); err != nil {
		return nil, err
	}
	return out, nil
}

package yue

import (
	"context"
	"fmt"
	neturl "net/url"
)

// Plan — стадия плана: ABC-партитура без рендера (грузит модель, минуты).
func (c *Client) Plan(ctx context.Context, p PlanParams) (*PlanResult, error) {
	var out PlanResult
	if err := c.postJSON(ctx, "/plan", p, planTimeout, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Copilot — копайтер стихов через Ollama на воркере.
func (c *Client) Copilot(ctx context.Context, p CopilotParams) (*CopilotResult, error) {
	var out CopilotResult
	if err := c.postJSON(ctx, "/copilot", p, copilotTimeout, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Translate — перевод текста (слотов стиля) в английский через Ollama.
func (c *Client) Translate(ctx context.Context, text string) (*TranslateResult, error) {
	var out TranslateResult
	body := map[string]string{"text": text, "to": "English"}
	if err := c.postJSON(ctx, "/translate", body, copilotTimeout, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RecognizeLyrics — текст трека через faster-whisper (для каверов: оригинал
// → адаптация → поле лирики). Минуты на длинных треках.
func (c *Client) RecognizeLyrics(ctx context.Context, name string, data []byte) (*LyricsResult, error) {
	var out LyricsResult
	if err := c.postRaw(ctx, "/lyrics", name, data, headerTimeout, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// JobLyrics — текст из готового аудио джобы (whisper на стороне воркера,
// без повторной загрузки файла; есть дорожка голоса — по ней). language —
// код языка пения (en, ru, …; "auto" — определить); пусто — по стилю трека.
func (c *Client) JobLyrics(ctx context.Context, id int64, language string) (*LyricsResult, error) {
	var out LyricsResult
	path := fmt.Sprintf("/jobs/%d/lyrics", id)
	if language != "" {
		path += "?language=" + neturl.QueryEscape(language)
	}
	if err := c.post(ctx, path, headerTimeout, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AdaptLyrics — адаптация-перевод лирики под пение (сохранение слогов).
func (c *Client) AdaptLyrics(ctx context.Context, text, to string) (*LyricsResult, error) {
	var out LyricsResult
	body := map[string]string{"text": text, "to": to}
	if err := c.postJSON(ctx, "/lyrics/adapt", body, copilotTimeout, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// WorkerConfig — настройки воркера: Ollama URL/модель, пути данных.
func (c *Client) WorkerConfig(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	if err := c.get(ctx, "/config", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SetWorkerConfig — смена настроек воркера на лету (ollama_url, ollama_model).
func (c *Client) SetWorkerConfig(ctx context.Context, cfg map[string]any) error {
	return c.postJSON(ctx, "/config", cfg, requestTimeout, nil)
}

// OllamaModels — список моделей Ollama (ollamaURL — кандидат или "" для текущего).
func (c *Client) OllamaModels(ctx context.Context, ollamaURL string) (map[string]any, error) {
	path := "/ollama_models"
	if ollamaURL != "" {
		path += "?url=" + neturl.QueryEscape(ollamaURL)
	}
	var out map[string]any
	if err := c.get(ctx, path, &out); err != nil {
		return nil, err
	}
	return out, nil
}

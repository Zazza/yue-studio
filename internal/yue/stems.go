package yue

import (
	"context"
	"fmt"
)

// References — загруженные референсы для сравнения метрик.
func (c *Client) References(ctx context.Context) ([]Reference, error) {
	var refs []Reference
	if err := c.get(ctx, "/references", &refs); err != nil {
		return nil, err
	}
	return refs, nil
}

// AddReference — залить референс на воркер (там замер метрик).
func (c *Client) AddReference(ctx context.Context, name string, data []byte) (*Reference, error) {
	var out Reference
	if err := c.postRaw(ctx, "/references", name, data, planTimeout, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// JobDspVariants — список применённых DSP-вариантов джобы.
func (c *Client) JobDspVariants(ctx context.Context, id int64) ([]DspVariant, error) {
	var vs []DspVariant
	if err := c.get(ctx, fmt.Sprintf("/jobs/%d/dsp", id), &vs); err != nil {
		return nil, err
	}
	return vs, nil
}

// UploadDsp — залить DSP-вариант джобы (включает librosa-замер на воркере).
func (c *Client) UploadDsp(ctx context.Context, id int64, fname string, data []byte) (*DspVariant, error) {
	var out DspVariant
	if err := c.postRaw(ctx, fmt.Sprintf("/jobs/%d/dsp", id), fname, data, planTimeout, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Transcribe — трек → ABC (SheetSage2; первый вызов грузит модель).
func (c *Client) Transcribe(ctx context.Context, name string, data []byte) (*TranscribeResult, error) {
	var out TranscribeResult
	if err := c.postRaw(ctx, "/transcribe", name, data, copilotTimeout, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// MakeStems — demucs: drums/bass/other/vocals (грузит модель при первом вызове).
func (c *Client) MakeStems(ctx context.Context, id int64) (map[string]any, error) {
	var out map[string]any
	if err := c.post(ctx, fmt.Sprintf("/jobs/%d/stems", id), planTimeout, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DspVariantDelete — удалить вариант эффекта/вклейки (файл + метрики).
func (c *Client) DspVariantDelete(ctx context.Context, id int64, fname string) (bool, error) {
	var out struct {
		Deleted bool `json:"deleted"`
	}
	if err := c.del(ctx, fmt.Sprintf("/jobs/%d/dsp/%s", id, fname), &out); err != nil {
		return false, err
	}
	return out.Deleted, nil
}

func (c *Client) JobStems(ctx context.Context, id int64) ([]map[string]any, error) {
	var out []map[string]any
	if err := c.get(ctx, fmt.Sprintf("/jobs/%d/stems", id), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// MakeMinus — минус-трек: микс стемов без исключённых групп.
func (c *Client) MakeMinus(ctx context.Context, id int64, exclude []string) (map[string]any, error) {
	var out map[string]any
	body := map[string]any{"exclude": exclude}
	if err := c.postJSON(ctx, fmt.Sprintf("/jobs/%d/minus", id), body, planTimeout, &out); err != nil {
		return nil, err
	}
	return out, nil
}

package yue

import (
	"context"
	"fmt"
	"net/http"
)

// SoundPresets — пресеты звука воркера: встроенные первыми, затем свои.
func (c *Client) SoundPresets(ctx context.Context) ([]SoundPreset, error) {
	var out []SoundPreset
	if err := c.get(ctx, "/sound-presets", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SoundPresetCreate — сохранить свой пресет (воркер проверяет рецепт, 422 — причина).
func (c *Client) SoundPresetCreate(ctx context.Context, p SoundPreset) (*SoundPreset, error) {
	var out SoundPreset
	if err := c.postJSON(ctx, "/sound-presets", presetBody(p), requestTimeout, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SoundPresetUpdate — заменить свой пресет целиком; встроенный — 409.
func (c *Client) SoundPresetUpdate(ctx context.Context, id int64, p SoundPreset) (*SoundPreset, error) {
	var out SoundPreset
	if err := c.call(ctx, http.MethodPut, fmt.Sprintf("/sound-presets/%d", id), requestTimeout, jsonReq(presetBody(p)), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SoundPresetDelete — удалить свой пресет; встроенный — 409.
func (c *Client) SoundPresetDelete(ctx context.Context, id int64) error {
	return c.del(ctx, fmt.Sprintf("/sound-presets/%d", id), nil)
}

// SoundPresetState — статус пресета у трека (захват pending→running, итог done/error, повтор → pending);
// уже захвачен или переход запрещён — *StatusError{Code: 409}.
func (c *Client) SoundPresetState(ctx context.Context, jobID, presetID int64, st JobPreset) ([]JobPreset, error) {
	body := map[string]any{"status": st.Status}
	if st.ChildID > 0 {
		body["child_id"] = st.ChildID
	}
	if st.Error != "" {
		body["error"] = st.Error
	}
	var out []JobPreset
	if err := c.postJSON(ctx, fmt.Sprintf("/jobs/%d/sound-presets/%d/state", jobID, presetID), body, requestTimeout, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// presetBody — то, что воркер принимает при создании/замене: без id, slug и builtin
func presetBody(p SoundPreset) map[string]any {
	specs, final := p.Specs, p.Final
	if specs == nil {
		specs = []PresetSpec{}
	}
	if final == nil {
		final = []PresetStep{}
	}
	body := map[string]any{"name": p.Name, "note": p.Note, "specs": specs, "final": final, "target_lufs": p.TargetLUFS}
	if p.ReferenceJobID > 0 {
		body["reference_job_id"] = p.ReferenceJobID
	}
	return body
}

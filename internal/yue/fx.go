package yue

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
)

// FxRequest — обработка звуковым движком воркера (POST /jobs/{id}/fx): цепочка блоков
// gate/eq/comp/drive/amp/cab/reverb/delay по порядку на весь трек (source=mix) или дорожку.
type FxRequest struct {
	Source string           `json:"source"`
	Chain  []map[string]any `json:"chain"`
	From   *float64         `json:"from,omitempty"`   // окно, секунды; nil — с начала
	To     *float64         `json:"to,omitempty"`     // nil — до конца
	Output string           `json:"output,omitempty"` // mix (по умолчанию) | solo — только дорожка
	Label  string           `json:"label,omitempty"`
	// Preview — прослушать кусок окна (обязательны From/To): preview-fx-*.flac, не вариант
	Preview bool `json:"preview,omitempty"`
	// Fade — у превью: вход окна с линейными краями (нарастание с From, спад после To), с;
	// пересборка студии берёт ту же форму, что у вычитаемой исходной дорожки
	Fade float64 `json:"fade,omitempty"`
	// Pad — у превью: файл от начала трека (до From — тишина), чтобы вставить его без задержки
	Pad bool `json:"pad,omitempty"`
	// Add — добавление (синт-партия): превью «в миксе» = трек + обработанное, без замены дорожки
	Add bool `json:"add,omitempty"`
	// File — вход не исходный звук трека, а его вариант (микс студии, файл пресета); только с Source mix
	File string `json:"file,omitempty"`
	// InPlace — результат записать в тот же вариант File (мастер на миксе; не превью)
	InPlace bool `json:"in_place,omitempty"`
}

// ApplyFx — цепочка движка на трек/дорожку → вариант dsp-fx-*.flac (список вариантов).
func (c *Client) ApplyFx(ctx context.Context, id int64, req FxRequest) (*DspVariant, error) {
	var out DspVariant
	if err := c.postJSON(ctx, fmt.Sprintf("/jobs/%d/fx", id), req, gpuTimeout, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// FxAssets — загруженные на воркер захваты NAM и IR: {"amps": [...], "irs": [...]}.
func (c *Client) FxAssets(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	if err := c.get(ctx, "/fx/assets", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// FxKitProgress — идущая установка набора на воркере {name, part, done, total, bytes}; нет — пустой объект.
func (c *Client) FxKitProgress(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	if err := c.get(ctx, "/fx/kits/progress", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// InstallFxKit — воркер скачивает набор сэмплов барабанов из своего каталога (повтор — без сети).
func (c *Client) InstallFxKit(ctx context.Context, name string) (map[string]any, error) {
	var out map[string]any
	if err := c.post(ctx, "/fx/kits/install?name="+url.QueryEscape(name), planTimeout, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UploadFxAsset — загрузить захват NAM (kind=amp, .nam) или IR (kind=ir, .wav).
func (c *Client) UploadFxAsset(ctx context.Context, kind, name string, data []byte) (map[string]any, error) {
	q := url.Values{"kind": {kind}, "name": {name}}
	var out map[string]any
	if err := c.postRaw(ctx, "/fx/assets?"+q.Encode(), name, data, planTimeout, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// FxPhrase — фраза страницы «Инструменты» (GET /fx/phrases): круг, который играет по кругу.
type FxPhrase struct {
	ID       string            `json:"id"`
	Family   string            `json:"family"` // guitar | bass | drums | synth
	Name     map[string]string `json:"name"`
	BPM      float64           `json:"bpm"`
	Beats    float64           `json:"beats,omitempty"`  // долей в круге (такт — 4 доли)
	Chords   []string          `json:"chords,omitempty"` // аккорд на такт по кругу — ноты синта кладёт страница
	Style    string            `json:"style,omitempty"`  // у синт-фразы: pad | arp | pulse | drone
	Parts    []string          `json:"parts,omitempty"`  // части фразы под цепочку (барабаны — по частям)
	CycleSec float64           `json:"cycle_sec"`        // длина круга при темпе 1
}

// FxPhraseReq — круг фразы через цепочку движка (POST /fx/phrase).
type FxPhraseReq struct {
	Phrase string           `json:"phrase"`
	Tempo  float64          `json:"tempo"` // множитель темпа 0,5…1,5
	Chain  []map[string]any `json:"chain"`
	Stems  []string         `json:"stems"`  // части под цепочку; пусто — все
	Bypass bool             `json:"bypass"` // сухая фраза тем же путём (сравнение «обработка вкл/выкл»)
}

// FxPhraseResult — файл круга в кэше фраз воркера.
type FxPhraseResult struct {
	File     string  `json:"file"`
	CycleSec float64 `json:"cycle_sec"`
	Clipped  bool    `json:"clipped"`
}

// FxPhrases — каталог фраз воркера.
func (c *Client) FxPhrases(ctx context.Context) ([]FxPhrase, error) {
	var out []FxPhrase
	if err := c.get(ctx, "/fx/phrases", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// FxPhrase — круг фразы через цепочку (с усилителем — в очереди GPU воркера).
func (c *Client) FxPhrase(ctx context.Context, req FxPhraseReq) (*FxPhraseResult, error) {
	if req.Chain == nil {
		req.Chain = []map[string]any{}
	}
	if req.Stems == nil {
		req.Stems = []string{}
	}
	var out FxPhraseResult
	if err := c.postJSON(ctx, "/fx/phrase", req, gpuTimeout, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// FetchPhraseAudio — WAV круга из кэша фраз (имя — из ответа FxPhrase).
func (c *Client) FetchPhraseAudio(ctx context.Context, file string) (io.ReadCloser, error) {
	if !validFile(file) {
		return nil, fmt.Errorf("bad filename")
	}
	b, err := c.getBytes(ctx, "/fx/phrase/files/"+url.PathEscape(file))
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

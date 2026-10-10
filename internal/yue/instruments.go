package yue

import (
	"context"
	"fmt"
	"net/http"
)

// FxInstrument — свой инструмент страницы «Инструменты» (GET/POST /fx/instruments): цепочка движка под своим
// именем на основе готовой (Base — id готовой); в студии выбирается вместе с готовыми.
type FxInstrument struct {
	ID    int64            `json:"id,omitempty"`
	Name  string           `json:"name"`
	Base  string           `json:"base"`
	Group string           `json:"group"`
	Stems []string         `json:"stems"`
	Chain []map[string]any `json:"chain"`
	// Extra — поля готовой, которые нужны выбору (style, octave, pattern, swing, accent, amp_hint, place)
	Extra map[string]any `json:"extra"`
}

// FxInstruments — свои инструменты по порядку создания.
func (c *Client) FxInstruments(ctx context.Context) ([]FxInstrument, error) {
	var out []FxInstrument
	if err := c.get(ctx, "/fx/instruments", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// FxInstrumentCreate — сохранить свой инструмент; ответ — с id.
func (c *Client) FxInstrumentCreate(ctx context.Context, in FxInstrument) (*FxInstrument, error) {
	var out FxInstrument
	if err := c.postJSON(ctx, "/fx/instruments", instrumentBody(in), requestTimeout, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// FxInstrumentUpdate — заменить поля своего инструмента (имя, цепочку…).
func (c *Client) FxInstrumentUpdate(ctx context.Context, id int64, in FxInstrument) (*FxInstrument, error) {
	var out FxInstrument
	if err := c.call(ctx, http.MethodPut, fmt.Sprintf("/fx/instruments/%d", id), requestTimeout,
		jsonReq(instrumentBody(in)), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// FxInstrumentDelete — удалить свой инструмент.
func (c *Client) FxInstrumentDelete(ctx context.Context, id int64) error {
	return c.del(ctx, fmt.Sprintf("/fx/instruments/%d", id), nil)
}

// instrumentBody — тело без id (id — в пути), пустые списки — списками, а не null.
func instrumentBody(in FxInstrument) map[string]any {
	stems, chain, extra := in.Stems, in.Chain, in.Extra
	if stems == nil {
		stems = []string{}
	}
	if chain == nil {
		chain = []map[string]any{}
	}
	if extra == nil {
		extra = map[string]any{}
	}
	return map[string]any{"name": in.Name, "base": in.Base, "group": in.Group, "stems": stems, "chain": chain, "extra": extra}
}

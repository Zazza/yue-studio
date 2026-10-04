package dsp

// Preset — готовый набор педалей: стартовая точка для подбора звука гитары.
type Preset struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Note  string `json:"note"`
	Steps []Step `json:"steps"`
}

type pp = map[string]float64

// presets — наборы «как у кого-то»: не копия чужого звука (гитару уже сыграла
// модель), а цепочка тех же типов педалей в том же порядке. Темп дилея — 120
// BPM по умолчанию, под трек — «найти сетку».
var presets = []Preset{
	{
		ID: "hendrix", Name: "Хендрикс",
		Note: "Октавный фузз → Uni-Vibe → кабинет → немного комнаты: вязкий психоделический рок.",
		Steps: []Step{
			{Chain: "fuzz-octave", Params: pp{"drive": 22, "octave": 0.4}},
			{Chain: "univibe", Params: pp{"rate": 1.5, "depth": 0.6, "mix": 0.6}},
			{Chain: "cab"},
			{Chain: "reverb-room", Params: pp{"wet": 0.2}},
		},
	},
	{
		ID: "shoegaze", Name: "Шугейз",
		Note: "Big Muff → хорус → длинный дилей → огромный зал: стена гитар, в которой тонет голос.",
		Steps: []Step{
			{Chain: "fuzz-muff", Params: pp{"drive": 36, "tone": 0.4}},
			{Chain: "chorus", Params: pp{"depth": 6, "rate": 0.3, "mix": 0.5}},
			{Chain: "delay", Params: pp{"feedback": 0.55, "wet": 0.35}},
			{Chain: "reverb-hall", Params: pp{"size": 4, "wet": 0.5}},
		},
	},
	{
		ID: "surf", Name: "Сёрф",
		Note: "Тремоло → много пружины: Дик Дэйл, саундтреки Тарантино.",
		Steps: []Step{
			{Chain: "tremolo", Params: pp{"rate": 6, "depth": 0.4}},
			{Chain: "reverb-spring", Params: pp{"size": 2, "wet": 0.5}},
		},
	},
	{
		ID: "grunge", Name: "Гранж",
		Note: "RAT → лёгкий хорус → кабинет: грязно, тяжело, начало 90-х.",
		Steps: []Step{
			{Chain: "dist-rat", Params: pp{"drive": 30, "tone": 3.5}},
			{Chain: "chorus", Params: pp{"mix": 0.3}},
			{Chain: "cab"},
		},
	},
	{
		ID: "dub", Name: "Даб",
		Note: "Густой низ → дилей с длинной тёмной обратной связью → пружина: ямайский даб.",
		Steps: []Step{
			{Chain: "eq", Params: pp{"low": 3}},
			{Chain: "delay", Params: pp{"feedback": 0.65, "cut": 2.5, "wet": 0.55}},
			{Chain: "reverb-spring", Params: pp{"wet": 0.35}},
		},
	},
	{
		ID: "postpunk", Name: "Пост-панк",
		Note: "Компрессор → хорус → короткий дилей → плейт: холодные звенящие гитары 80-х.",
		Steps: []Step{
			{Chain: "comp-pedal", Params: pp{"sustain": 0.4}},
			{Chain: "chorus", Params: pp{"depth": 4, "rate": 0.8, "mix": 0.6}},
			{Chain: "delay", Params: pp{"div": 4, "feedback": 0.3, "wet": 0.25}},
			{Chain: "reverb-plate", Params: pp{"wet": 0.3}},
		},
	},
	{
		ID: "clean-space", Name: "Чисто + простор",
		Note: "Компрессор → дилей → зал: чистая гитара в большом пространстве — эмбиент, баллады.",
		Steps: []Step{
			{Chain: "comp-pedal", Params: pp{"sustain": 0.3}},
			{Chain: "delay", Params: pp{"feedback": 0.35, "wet": 0.25}},
			{Chain: "reverb-hall", Params: pp{"wet": 0.3}},
		},
	},
}

// Presets — готовые наборы педалей (для UI и MCP).
func Presets() []Preset {
	out := make([]Preset, len(presets))
	copy(out, presets)
	return out
}

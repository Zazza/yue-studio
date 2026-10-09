// Редактируемая цепочка звукового движка (страница «Инструменты»): чистая логика без DOM.
// Блок — {type, on, params}; описание блоков (умолчания, границы) — fxBlocks.json, копия
// worker/fx_blocks.json (make mcp-data). Все функции возвращают новый массив.

function spec(type, blocks) {
  const s = blocks && blocks[type]
  if (!s) throw new Error(`неизвестный блок: ${type}`)
  return s
}

function numSpec(s, key) {
  return (s.params || []).find((p) => p.id === key)
}

function strSpec(s, key) {
  return (s.strings || []).find((p) => p.id === key)
}

// число в границы описания; zero_off — 0 значит «выкл» и остаётся 0: у положительных границ (срез, Гц) —
// всё ≤ 0, у отрицательных (цель LUFS −24…−6) — всё выше верхней границы, ползунок тянется до 0
export function clampParam(p, value) {
  let v = Number(value)
  if (!Number.isFinite(v)) v = p.default
  if (p.zero_off && (p.max < 0 ? v > p.max : v <= 0)) return 0
  return Math.min(p.max, Math.max(p.min, v))
}

function clampBand(bandSpec, band) {
  const out = {}
  for (const [k, p] of Object.entries(bandSpec)) out[k] = clampParam(p, band && band[k] !== undefined ? band[k] : p.default)
  return out
}

export function newBlock(type, blocks) {
  const s = spec(type, blocks)
  const params = {}
  for (const p of s.params || []) params[p.id] = p.default
  for (const p of s.strings || []) params[p.id] = p.default ?? ''
  if (s.bands) params.bands = []
  return { type, on: true, params }
}

export function addBlock(chain, type, blocks) {
  return [...chain, newBlock(type, blocks)]
}

export function removeBlock(chain, i) {
  return chain.filter((_, j) => j !== i)
}

export function moveBlock(chain, i, dir) {
  const j = i + dir
  if (i < 0 || i >= chain.length || j < 0 || j >= chain.length) return [...chain]
  const out = [...chain]
  ;[out[i], out[j]] = [out[j], out[i]]
  return out
}

export function toggleBlock(chain, i) {
  return chain.map((b, j) => (j === i ? { ...b, on: !b.on } : b))
}

export function setParam(chain, i, key, value, blocks) {
  return chain.map((b, j) => {
    if (j !== i) return b
    const s = spec(b.type, blocks)
    const p = numSpec(s, key)
    let v = value
    if (p) v = clampParam(p, value)
    else if (key === 'bands' && s.bands) v = (value || []).map((band) => clampBand(s.bands, band))
    else if (!strSpec(s, key)) return b
    return { ...b, params: { ...b.params, [key]: v } }
  })
}

// полосы эквалайзера: добавить с умолчаниями / убрать / поменять поле
export function addBand(chain, i, blocks) {
  const b = chain[i]
  const s = spec(b.type, blocks)
  if (!s.bands) return [...chain]
  return setParam(chain, i, 'bands', [...(b.params.bands || []), clampBand(s.bands, {})], blocks)
}

export function removeBand(chain, i, k, blocks) {
  return setParam(chain, i, 'bands', (chain[i].params.bands || []).filter((_, j) => j !== k), blocks)
}

export function setBand(chain, i, k, key, value, blocks) {
  const bands = (chain[i].params.bands || []).map((band, j) => (j === k ? { ...band, [key]: value } : band))
  return setParam(chain, i, 'bands', bands, blocks)
}

// → JSON для воркера (POST /jobs/{id}/fx): выключенные блоки не уходят
export function toWorkerChain(chain) {
  return chain.filter((b) => b.on).map((b) => ({ type: b.type, ...b.params }))
}

// JSON воркера/пресета → редактируемая цепочка; недостающее — умолчания
export function fromWorkerChain(json, blocks) {
  return (json || []).map((raw) => {
    let b = newBlock(raw.type, blocks)
    const s = spec(raw.type, blocks)
    const params = { ...b.params }
    for (const [k, v] of Object.entries(raw)) {
      if (k === 'type') continue
      const p = numSpec(s, k)
      if (p) params[k] = clampParam(p, v)
      else if (k === 'bands' && s.bands) params.bands = (v || []).map((band) => clampBand(s.bands, band))
      else if (strSpec(s, k)) params[k] = String(v)
    }
    b = { ...b, params }
    return b
  })
}

// блоки с обязательной строкой без значения (усилитель без захвата) — индексы
export function missingRequired(chain, blocks) {
  const out = []
  chain.forEach((b, i) => {
    if (!b.on) return
    const s = spec(b.type, blocks)
    if ((s.strings || []).some((p) => p.required && !b.params[p.id])) out.push(i)
  })
  return out
}

// наборы сэмплов (sampler, bass), которых цепочке не хватает на воркере: имена наборов (до «/»),
// без повторов — их воркер скачает по требованию (installFxKit)
const KIT_BLOCKS = ['sampler', 'bass', 'perc']

export function missingKits(workerChain, kits) {
  const have = new Set((kits || []).map((k) => k.name))
  const out = []
  for (const b of workerChain || []) {
    if (!KIT_BLOCKS.includes(b.type)) continue
    for (const name of [b.kit, b.kit_open, b.kit_mid, b.kit_low]) {   // открытые удары (хэт) и тамы по высоте — тоже наборы
      if (!name || have.has(name)) continue
      const kit = String(name).split('/')[0]
      if (kit && !out.includes(kit)) out.push(kit)
    }
  }
  return out
}

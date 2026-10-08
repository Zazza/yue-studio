// Тесты карточки internal-studio-engine, условие 36i (ТК43): engineRun.js — логика
// «применить цепочку движка» (EngineBox) и «докачать наборы» (EngineBox, InstrumentsPage)
// вне компонентов. Написаны по карточке, без чтения реализации.
//
// Контракт (карточка + допущения теста):
// - ensureKits(api, chain, kits): chain — цепочка для воркера ({type, ...параметры}, как у
//   missingKits), kits — список наборов воркера ([{name: 'набор/часть', samples}], как в
//   fxAssets().kits). Недостающие наборы (по kit и kit_open блоков sampler/bass) ставятся
//   api.installFxKit(имя набора) по одному (следующий — после окончания предыдущего), в
//   порядке цепочки, каждый один раз; уже стоящие не трогаются. Ошибка установки →
//   отклонённый промис с текстом ошибки.
// - applyEngine(api, snap): snap — снимок EngineBox (допущение о форме):
//     { jobId, dur, src, from, to, chain, label }
//     jobId — id трека, dur — длина трека (с), src — дорожка (stem), from/to — окно (с;
//     to ≤ from — «до конца», как to ≤ 0 у записи реестра), chain — цепочка для воркера,
//     label — подпись записи.
//   Порядок: api.workerConfig() → нет fx_preview — ошибка «обнов…»; наборы цепочки —
//   по api.fxAssets().kits, недостающие — api.installFxKit; затем api.applyFx(jobId, req)
//   с req ⊇ {source: src, output: 'solo', preview: true, from, to: окно превью, chain};
//   окно превью — до 10 с от from: to > from → min(to, from + 10); to ≤ from →
//   min(from + 10, dur). Ответ не preview-fx-* — ошибка «обнов…».
//   Возврат — то, что нужно записи реестра useInserts.addStemEngine:
//     ⊇ { stem: src, chain, from, to, label } (to — исходное окно записи, не окно превью).
//   Снимок берётся до первого await: изменения исходных объектов во время ожидания
//   workerConfig / fxAssets / installFxKit не меняют ни запрос превью, ни запись.
// - Текст ошибки «старый воркер» — из i18n; в тестах принимается ru («обнов») или en
//   («update»), т.к. язык окружения тестов не задан карточкой.
import { describe, it, expect } from 'vitest'
import { ensureKits, applyEngine } from './engineRun.js'

const OLD_WORKER = /обнов|update/i

// Фейк api: асинхронные методы, журнал вызовов, хуки «во время ожидания».
// hooks[method] вызывается ПОСЛЕ того, как applyEngine/ensureKits вызвали метод, но до
// того, как его промис разрешился, — это и есть «изменение во время await».
function fakeApi({
  config = { fx_preview: true },
  kits = [],
  applyResp = { file: 'preview-fx-0a1b2c3d.flac', duration_sec: 10, clipped: false },
  installErr = {},
  hooks = {},
} = {}) {
  const log = []
  const tick = () => new Promise((r) => setTimeout(r, 0))
  const api = {
    async workerConfig() {
      log.push(['workerConfig'])
      hooks.workerConfig?.()
      await tick()
      return config
    },
    async fxAssets() {
      log.push(['fxAssets'])
      hooks.fxAssets?.()
      await tick()
      return { amps: [], irs: [], kits: kits.map((k) => ({ ...k })) }
    },
    async installFxKit(name) {
      log.push(['installFxKit:start', name])
      hooks.installFxKit?.(name)
      await tick()
      if (installErr[name]) {
        log.push(['installFxKit:fail', name])
        throw new Error(installErr[name])
      }
      log.push(['installFxKit:end', name])
      return { name, downloaded: true }
    },
    async applyFx(id, req) {
      // глубокая копия: позднее изменение объектов не должно «переписать» журнал
      log.push(['applyFx', id, JSON.parse(JSON.stringify(req))])
      await tick()
      return applyResp
    },
  }
  const calls = (m) => log.filter((e) => e[0] === m)
  const installs = () => log.filter((e) => e[0] === 'installFxKit:start').map((e) => e[1])
  return { api, log, calls, installs }
}

const kit = (name) => ({ name, samples: 3 })

// ---------- ensureKits ----------

describe('ensureKits: недостающие наборы', () => {
  it('всё стоит → ничего не ставит', async () => {
    const f = fakeApi()
    const chain = [{ type: 'sampler', kit: 'osdk/kick' }, { type: 'gain', gain_db: 0 }]
    await ensureKits(f.api, chain, [kit('osdk/kick')])
    expect(f.installs()).toEqual([])
  })

  it('цепочка без наборов → ничего не ставит', async () => {
    const f = fakeApi()
    await ensureKits(f.api, [{ type: 'eq' }, { type: 'reverb' }], [])
    expect(f.installs()).toEqual([])
  })

  it('kit есть, kit_open нет → ставится набор kit_open', async () => {
    const f = fakeApi()
    const chain = [{ type: 'sampler', kit: 'osdk/hh-closed', kit_open: 'osdk/hh-half', choke: 1 }]
    await ensureKits(f.api, chain, [kit('osdk/hh-closed')])
    expect(f.installs()).toEqual(['osdk'])
  })

  it('kit нет → ставится набор kit (и для bass)', async () => {
    const f = fakeApi()
    await ensureKits(f.api, [{ type: 'bass', kit: 'growlybass/bass' }], [kit('osdk/kick')])
    expect(f.installs()).toEqual(['growlybass'])
  })

  it('порядок как в цепочке, каждый набор один раз, стоящие не трогает', async () => {
    const f = fakeApi()
    const chain = [
      { type: 'bass', kit: 'growlybass/bass' },
      { type: 'sampler', kit: 'osdk/kick' },                                // стоит
      { type: 'sampler', kit: 'mykit/hh', kit_open: 'other/open' },
      { type: 'sampler', kit: 'osdk/snare', kit_open: 'growlybass/bass' },  // osdk/snare нет, growlybass — уже
    ]
    await ensureKits(f.api, chain, [kit('osdk/kick')])
    expect(f.installs()).toEqual(['growlybass', 'mykit', 'other', 'osdk'])
  })

  it('пустой список наборов воркера → ставит все наборы цепочки', async () => {
    const f = fakeApi()
    const chain = [{ type: 'sampler', kit: 'osdk/kick' }, { type: 'bass', kit: 'growlybass/bass' }]
    await ensureKits(f.api, chain, [])
    expect(f.installs()).toEqual(['osdk', 'growlybass'])
  })

  it('по одному: следующая установка начинается после окончания предыдущей', async () => {
    const f = fakeApi()
    const chain = [{ type: 'sampler', kit: 'a/x' }, { type: 'sampler', kit: 'b/y' }, { type: 'bass', kit: 'c/z' }]
    await ensureKits(f.api, chain, [])
    const seq = f.log.filter((e) => e[0].startsWith('installFxKit')).map((e) => `${e[0]}:${e[1]}`)
    expect(seq).toEqual([
      'installFxKit:start:a', 'installFxKit:end:a',
      'installFxKit:start:b', 'installFxKit:end:b',
      'installFxKit:start:c', 'installFxKit:end:c',
    ])
  })

  it('ошибка установки → отклонённый промис с текстом, дальше не ставит', async () => {
    const f = fakeApi({ installErr: { b: 'набор b: сеть недоступна' } })
    const chain = [{ type: 'sampler', kit: 'a/x' }, { type: 'sampler', kit: 'b/y' }, { type: 'sampler', kit: 'c/z' }]
    await expect(ensureKits(f.api, chain, [])).rejects.toThrow(/сеть недоступна/)
    expect(f.installs()).toEqual(['a', 'b'])
  })

  it('вход не меняет', async () => {
    const f = fakeApi()
    const chain = Object.freeze([Object.freeze({ type: 'sampler', kit: 'osdk/kick', kit_open: 'osdk/hh-half' })])
    const kits = Object.freeze([Object.freeze(kit('osdk/kick'))])
    await ensureKits(f.api, chain, kits)
    expect(f.installs()).toEqual(['osdk'])
  })
})

// ---------- applyEngine ----------

const CHAIN = () => [
  { type: 'amp', model: 'JCM2000.nam', input_db: 0, output_db: -3 },
  { type: 'eq', highpass_hz: 80, lowpass_hz: 0, bands: [{ freq_hz: 1000, gain_db: -3, q: 1 }] },
]
const SNAP = (over = {}) => ({
  jobId: 466, dur: 180, src: 'other', from: 20, to: 35, chain: CHAIN(), label: 'Гитара: JCM2000', ...over,
})

describe('applyEngine: запрос превью и запись', () => {
  it('один запрос превью solo по дорожке, окно до 10 с от from; запись — исходное окно', async () => {
    const f = fakeApi()
    const rec = await applyEngine(f.api, SNAP())
    const fx = f.calls('applyFx')
    expect(fx).toHaveLength(1)
    const [, id, req] = fx[0]
    expect(id).toBe(466)
    expect(req).toMatchObject({ source: 'other', output: 'solo', preview: true, from: 20, to: 30 })
    expect(req.chain).toEqual(CHAIN())
    expect(rec).toMatchObject({ stem: 'other', from: 20, to: 35, label: 'Гитара: JCM2000' })
    expect(rec.chain).toEqual(CHAIN())
  })

  it('окно короче 10 с → превью ровно по окну', async () => {
    const f = fakeApi()
    await applyEngine(f.api, SNAP({ from: 20, to: 25.5 }))
    expect(f.calls('applyFx')[0][2]).toMatchObject({ from: 20, to: 25.5 })
  })

  it('окно ровно 10 с → превью по окну', async () => {
    const f = fakeApi()
    await applyEngine(f.api, SNAP({ from: 40, to: 50 }))
    expect(f.calls('applyFx')[0][2]).toMatchObject({ from: 40, to: 50 })
  })

  it('to ≤ from («до конца») → превью 10 с от from; запись хранит to как есть', async () => {
    for (const to of [0, -1, 20, 10]) {
      const f = fakeApi()
      const rec = await applyEngine(f.api, SNAP({ from: 20, to }))
      expect(f.calls('applyFx')[0][2]).toMatchObject({ from: 20, to: 30 })
      expect(rec).toMatchObject({ from: 20, to })
    }
  })

  it('to ≤ from у конца трека → превью не дальше длины трека', async () => {
    const f = fakeApi()
    await applyEngine(f.api, SNAP({ from: 175, to: 0, dur: 180 }))
    expect(f.calls('applyFx')[0][2]).toMatchObject({ from: 175, to: 180 })
  })

  it('from = 0 — допустимое начало окна', async () => {
    const f = fakeApi()
    await applyEngine(f.api, SNAP({ from: 0, to: 0, dur: 6 }))
    expect(f.calls('applyFx')[0][2]).toMatchObject({ from: 0, to: 6 })
  })
})

describe('applyEngine: старый воркер', () => {
  for (const [name, config] of [
    ['нет fx_preview', { fx_engine: true }],
    ['fx_preview: false', { fx_preview: false }],
    ['config пустой', null],
  ]) {
    it(`${name} → ошибка «обновите», превью не запрашивается`, async () => {
      const f = fakeApi({ config })
      await expect(applyEngine(f.api, SNAP())).rejects.toThrow(OLD_WORKER)
      expect(f.calls('applyFx')).toHaveLength(0)
      expect(f.installs()).toEqual([])
    })
  }

  for (const [name, resp] of [
    ['вариант dsp-fx-* вместо превью', { file: 'dsp-fx-other-0a1b2c3d.flac' }],
    ['пустой ответ', null],
    ['без имени файла', { duration_sec: 10 }],
  ]) {
    it(`ответ ${name} → ошибка «обновите»`, async () => {
      const f = fakeApi({ applyResp: resp })
      await expect(applyEngine(f.api, SNAP())).rejects.toThrow(OLD_WORKER)
    })
  }
})

describe('applyEngine: наборы', () => {
  const SAMPLER_CHAIN = () => [{ type: 'sampler', kit: 'osdk/hh-closed', kit_open: 'osdk/hh-half', choke: 1 }]

  it('недостающий набор ставится до запроса превью', async () => {
    const f = fakeApi({ kits: [kit('osdk/kick')] })
    await applyEngine(f.api, SNAP({ src: 'drums', chain: SAMPLER_CHAIN() }))
    expect(f.installs()).toEqual(['osdk'])
    const order = f.log.map((e) => e[0])
    expect(order.indexOf('installFxKit:end')).toBeLessThan(order.indexOf('applyFx'))
  })

  it('наборы стоят → ничего не ставит', async () => {
    const f = fakeApi({ kits: [kit('osdk/hh-closed'), kit('osdk/hh-half')] })
    await applyEngine(f.api, SNAP({ src: 'drums', chain: SAMPLER_CHAIN() }))
    expect(f.installs()).toEqual([])
    expect(f.calls('applyFx')).toHaveLength(1)
  })

  it('ошибка установки → отклонённый промис с текстом, превью не запрашивается', async () => {
    const f = fakeApi({ installErr: { osdk: 'osdk: GitHub недоступен' } })
    await expect(applyEngine(f.api, SNAP({ src: 'drums', chain: SAMPLER_CHAIN() }))).rejects.toThrow(/GitHub недоступен/)
    expect(f.calls('applyFx')).toHaveLength(0)
  })
})

describe('applyEngine: снимок до первого await', () => {
  // меняет всё, что можно поменять в исходных объектах: поля снимка и цепочку вглубь
  function mutate(snap) {
    snap.jobId = 999
    snap.src = 'vocals'
    snap.from = 100
    snap.to = 105
    snap.dur = 101
    snap.label = 'чужая подпись'
    snap.chain[0].kit = 'evil/kit'
    snap.chain[0].floor_db = -10
    snap.chain.push({ type: 'gain', gain_db: 12 })
  }

  const CHAIN_K = () => [{ type: 'sampler', kit: 'osdk/kick', floor_db: -40 }, { type: 'eq', bands: [{ freq_hz: 200, gain_db: 2, q: 1 }] }]

  for (const when of ['workerConfig', 'fxAssets', 'installFxKit']) {
    it(`изменение исходных объектов во время ${when} не меняет запрос и запись`, async () => {
      const snap = SNAP({ src: 'drums', from: 20, to: 35, chain: CHAIN_K(), label: 'Бочка' })
      const f = fakeApi({ kits: [], hooks: { [when]: () => mutate(snap) } })
      const rec = await applyEngine(f.api, snap)
      if (when === 'installFxKit') expect(f.installs()).toEqual(['osdk'])
      const fx = f.calls('applyFx')
      expect(fx).toHaveLength(1)
      const [, id, req] = fx[0]
      expect(id).toBe(466)
      expect(req).toMatchObject({ source: 'drums', output: 'solo', preview: true, from: 20, to: 30 })
      expect(req.chain).toEqual(CHAIN_K())
      expect(rec).toMatchObject({ stem: 'drums', from: 20, to: 35, label: 'Бочка' })
      expect(rec.chain).toEqual(CHAIN_K())
    })
  }

  it('устанавливает наборы из снимка, а не из изменённой цепочки', async () => {
    const snap = SNAP({ src: 'drums', chain: CHAIN_K() })
    const f = fakeApi({ kits: [], hooks: { workerConfig: () => mutate(snap) } })
    await applyEngine(f.api, snap)
    expect(f.installs()).toEqual(['osdk'])
  })

  it('запись не делит объекты с исходной цепочкой', async () => {
    const snap = SNAP()
    const f = fakeApi()
    const rec = await applyEngine(f.api, snap)
    snap.chain[0].output_db = 12
    expect(rec.chain).toEqual(CHAIN())
  })
})

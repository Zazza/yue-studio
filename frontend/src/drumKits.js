// Наборы барабанов целиком (пульт «Дорожки», «ритм-секция набором»): живой osdk и драм-машины, которые воркер
// синтезирует сам (worker/drumsynth.py). Части — блок sampler на дорожках-частях барабанов RoFormer.
// Обработки ритм-секции — цепочки, которые дописываются к каждой части барабанов (к басу — нет).

const machine = (id) => ({
  kick: { kit: `${id}/kick` },
  snare: { kit: `${id}/snare` },
  toms: { kit: `${id}/tom-small`, kit_mid: `${id}/tom-medium`, kit_low: `${id}/tom-large` },
  hh: { kit: `${id}/hh-closed`, kit_open: `${id}/hh-open`, choke: 1 },
  ride: { kit: `${id}/ride` },
  crash: { kit: `${id}/crash` },
})

export const DRUM_KITS = [
  {
    id: 'osdk',
    name: { ru: 'Живой акустический', en: 'Live acoustic' },
    note: { ru: 'Настоящая ударная установка (The Open Source Drum Kit): тот же рисунок, но живые барабаны.', en: 'A real drum kit (The Open Source Drum Kit): the same groove, real drums.' },
    parts: {
      kick: { kit: 'osdk/kick' },
      snare: { kit: 'osdk/snare' },
      toms: { kit: 'osdk/tom-small', kit_mid: 'osdk/tom-medium', kit_low: 'osdk/tom-large' },
      hh: { kit: 'osdk/hh-closed', kit_open: 'osdk/hh-half', choke: 1 },
      ride: { kit: 'osdk/ride' },
      crash: { kit: 'osdk/crash' },
    },
  },
  {
    id: 'tr808',
    name: { ru: 'Драм-машина 808', en: '808 drum machine' },
    note: { ru: 'Гулкая низкая бочка, щёлкающий малый, металлический хэт — электро, хип-хоп, синти-поп 80-х.', en: 'A booming low kick, a snappy snare, a metallic hat — electro, hip-hop, 80s synth-pop.' },
    parts: machine('tr808'),
  },
  {
    id: 'tr909',
    name: { ru: 'Драм-машина 909', en: '909 drum machine' },
    note: { ru: 'Плотная бочка с ударом, шумный яркий малый — хаус, техно, индастриал.', en: 'A punchy kick, a noisy bright snare — house, techno, industrial.' },
    parts: machine('tr909'),
  },
  {
    id: 'linn',
    name: { ru: 'Сэмплерная машина 80-х (Linn)', en: '80s sample machine (Linn)' },
    note: { ru: 'Сухие короткие удары с зерном старого 8-битного сэмплера — нью-вейв и поп 80-х.', en: 'Dry short hits with the grain of an old 8-bit sampler — 80s new wave and pop.' },
    parts: machine('linn'),
  },
  {
    id: 'cr78',
    name: { ru: 'Ранняя ритм-машина (CR-78)', en: 'Early rhythm box (CR-78)' },
    note: { ru: 'Мягкие тихие «тук-тс» ранней ритм-машины — холодный минимализм, пост-панк, ранний синти-поп.', en: 'Soft quiet ticks of an early rhythm box — cold minimalism, post-punk, early synth-pop.' },
    parts: machine('cr78'),
  },
  {
    id: 'simmons',
    name: { ru: 'Электронные барабаны 80-х (Simmons)', en: '80s electronic drums (Simmons)' },
    note: { ru: 'Тамы с «пью» — высота падает на ударе, синтетический малый — 80-е, готика, синти-рок.', en: 'Toms that go "pew" — pitch drops on the hit, a synthetic snare — 80s, goth, synth rock.' },
    parts: machine('simmons'),
  },
]

// «комната» — короткое помещение вокруг сухих сэмплов (то же, что у пресета «Живая ритм-секция»)
export const ROOM = { type: 'reverb', decay_s: 0.5, predelay_ms: 5, lowpass_hz: 7000, wet: 0.12 }

const KIT_FIELDS = ['kit', 'kit_open', 'kit_mid', 'kit_low', 'choke']

// параметры удара машин, которые расходятся с живым набором: у живого хэта громкость 0 — его поднимает эквалайзер
// (этап 10, сэмплы osdk тусклые), у машин хэт и так яркий, без eq — прежние +4 (усл. 84: машины не меняются)
const MACHINE_STRIKE = { hh: { output_db: 4 } }

/** Блок sampler части набора: параметры удара (floor_db, dynamics, output_db…) — от блока живого набора той же
 *  части (кроме MACHINE_STRIKE у машин), наборы сэмплов (kit, kit_open, kit_mid, kit_low, choke) — от части набора. */
export function kitSampler(live, part, stem = '') {
  const base = Object.fromEntries(Object.entries(live).filter(([k]) => !KIT_FIELDS.includes(k)))
  return { ...base, ...(MACHINE_STRIKE[stem] || {}), ...part }
}

// части пульта по-человечески — для подписей записей «набором»
export const PART_NAMES = {
  kick: { ru: 'бочка', en: 'kick' },
  snare: { ru: 'малый', en: 'snare' },
  toms: { ru: 'тамы', en: 'toms' },
  hh: { ru: 'хэт', en: 'hi-hat' },
  ride: { ru: 'райд', en: 'ride' },
  crash: { ru: 'крэш', en: 'crash' },
}

// обработки ритм-секции: первая — «сухо» (неизвестная обработка — тоже она)
export const DRUM_TREATMENTS = [
  { id: 'dry', name: { ru: 'сухо', en: 'dry' }, chain: [] },
  { id: 'room', name: { ru: 'комната', en: 'room' }, chain: [ROOM] },
  {
    id: 'gated', name: { ru: 'гейт-реверб 80-х', en: '80s gated reverb' },
    // яркий густой зал, обрезанный через 280 мс (как нелинейные ревербераторы 80-х), — не гейт по уровню:
    // порог в дБFS глушил бы тихие части целиком
    chain: [{ type: 'reverb', decay_s: 2.0, predelay_ms: 0, lowpass_hz: 9000, wet: 0.5, gate_ms: 280 }],
  },
  {
    id: 'tight', name: { ru: 'плотно', en: 'tight' },
    chain: [
      { type: 'comp', threshold_db: -24, ratio: 4, attack_ms: 5, release_ms: 80, makeup_db: 3 },
      { type: 'drive', gain_db: 12, mix: 0.3 },
    ],
  },
  {
    id: 'lofi', name: { ru: 'лоуфай', en: 'lo-fi' },
    chain: [
      { type: 'tape', wow: 0.15, flutter: 0.2, saturation: 0.4, lowpass_hz: 7000, hiss: 0.1 },
      { type: 'eq', highpass_hz: 60, lowpass_hz: 8000 },
    ],
  },
]

"""Фразы страницы «Инструменты»: короткий круг, который играет по кругу, пока крутятся регуляторы цепочки.

Чистый модуль: numpy/scipy/soundfile (+ librosa для растяжения записей). Гитара — записи чистого звукоснимателя
(нарезка GuitarSet, CC BY 4.0, см. phrases/README.md), бас и барабаны — нотами через drumsynth. render_dry даёт части
фразы (моно, одной длины) и длину круга; loop_render прогоняет цепочку движка по выбранным частям так, что круг
склеивается сам с собой (хвосты реверба и дилея конца круга звучат в его начале).
"""
from __future__ import annotations

from functools import lru_cache
from pathlib import Path

import numpy as np

import drumsynth

SR = 48000
TEMPO_MIN, TEMPO_MAX = 0.5, 1.5
PEAK_MAX = 0.99
DIR = Path(__file__).with_name("phrases")
DRUM_KIT = "linn"   # сэмплы настоящих барабанов 80-х — ближе к живым, чем 808/909
DRUM_PARTS = ("kick", "snare", "hh", "toms", "ride", "crash")
PERC_PART = "perc"   # тишина под блок perc: перкуссия по сетке ложится поверх бита
NOTE_BLOCKS = ("synth", "perc")   # блоки, которые сами играют ноты (notes от начала круга)
FADE_S = 0.004      # края записи: без щелчка на стыке круга
DRY_RMS = 0.1       # −20 dBFS: гитарные записи выровнены к нему же (phrases/README.md)
DRY_PEAK = 0.7      # запас для цепочек с усилением (перегруз, компрессор с подъёмом)
SILENT_RMS = 1e-6   # тише — тишина (синт-фраза без нот)


def _n(ru: str, en: str) -> dict:
    return {"ru": ru, "en": en}


# Гитарные записи: файл phrases/<id>.flac — ровно круг (такты целиком), bpm записи.
GUITAR = {
    "gtr-arpeggio": {"name": _n("Перебор", "Arpeggio"), "bpm": 98},
    "gtr-strum": {"name": _n("Бой аккордами", "Strummed chords"), "bpm": 148},
    "gtr-sustain": {"name": _n("Протяжные ноты", "Sustained notes"), "bpm": 119},
    "gtr-shred": {"name": _n("Запил (быстрое соло)", "Shred (fast solo)"), "bpm": 90},
    "gtr-funk": {"name": _n("Фанк, глушёные", "Funk, muted"), "bpm": 119},
    "gtr-jazz": {"name": _n("Джазовые аккорды", "Jazz chords"), "bpm": 137},
}

# Бас нотами: (MIDI, начало в долях, длина в долях); круг — beats долей.
_PROG = (40, 36, 43, 38)   # E1 C1 G1 D1 — Em C G D
BASS = {
    "bass-eighths": {"name": _n("Восьмые по аккордам", "Root eighths"), "bpm": 110, "beats": 16, "kit": "moog",
                     "notes": [(m, bar * 4 + k * 0.5, 0.45) for bar, m in enumerate(_PROG) for k in range(8)]},
    "bass-long": {"name": _n("Протяжные ноты", "Long notes"), "bpm": 80, "beats": 16, "kit": "moog",
                  "notes": [(m, bar * 4, 3.8) for bar, m in enumerate(_PROG)]},
}

# Барабаны: часть → [(доля, сила 0…7)]; части тамов/тарелок — свои удары (drumsynth-имена — в _DRUM_SRC).
_DRUM_SRC = {"kick": ("kick",), "snare": ("snare",), "hh": ("hh-closed",), "ride": ("ride",), "crash": ("crash",),
             "toms": ("tom-small", "tom-medium", "tom-large")}


def _rock():
    hits = {p: [] for p in DRUM_PARTS}
    for bar in range(2):
        o = bar * 4
        hits["kick"] += [(o, 7), (o + 2, 7), (o + 2.5, 5)]
        hits["snare"] += [(o + 1, 7), (o + 3, 7)]
        hits["hh"] += [(o + k * 0.5, 6 if k % 2 == 0 else 4) for k in range(8 if bar == 0 else 6)]
    hits["crash"] = [(0, 7)]
    hits["toms"] = [(7, 6, 0), (7.25, 6, 0), (7.5, 6, 1), (7.75, 7, 2)]   # сбивка: (доля, сила, номер тама)
    return hits


def _halftime():
    hits = {p: [] for p in DRUM_PARTS}
    for bar in range(2):
        o = bar * 4
        hits["kick"] += [(o, 7), (o + 1.5, 5), (o + 3.5, 4)]
        hits["snare"] += [(o + 2, 7)]
        hits["ride"] += [(o + k * 0.5, 6 if k % 2 == 0 else 4) for k in range(8)]
        hits["hh"] += [(o + 1, 5), (o + 3, 5)]
    hits["crash"] = [(0, 7)]
    hits["toms"] = [(6.5, 6, 2), (7.5, 6, 1)]
    return hits


DRUMS = {
    "drums-rock": {"name": _n("Рок-бит со сбивкой", "Rock beat with fill"), "bpm": 112, "beats": 8, "hits": _rock()},
    "drums-halftime": {"name": _n("Медленный, райд", "Half-time, ride"), "bpm": 76, "beats": 8, "hits": _halftime()},
}

# Синт: тишина, ноты кладёт страница (стиль игры по аккордам — как «Синт по аккордам» студии)
_CHORDS = ["Em", "C", "G", "D"]
SYNTH = {
    "synth-pad": {"name": _n("Пэд: аккорды", "Pad: chords"), "style": "pad"},
    "synth-arp": {"name": _n("Арпеджио", "Arpeggio"), "style": "arp"},
    "synth-pulse": {"name": _n("Пульс восьмыми", "Eighth pulse"), "style": "pulse"},
    "synth-drone": {"name": _n("Бурдон", "Drone"), "style": "drone"},
}
SYNTH_BPM, SYNTH_BEATS = 100, 16

PHRASES: dict[str, dict] = {}
for _id, _p in GUITAR.items():
    PHRASES[_id] = {"family": "guitar", "name": _p["name"], "bpm": _p["bpm"], "beats": 16, "chords": []}
for _id, _p in BASS.items():
    PHRASES[_id] = {"family": "bass", "name": _p["name"], "bpm": _p["bpm"], "beats": _p["beats"], "chords": _CHORDS}
for _id, _p in DRUMS.items():
    PHRASES[_id] = {"family": "drums", "name": _p["name"], "bpm": _p["bpm"], "beats": _p["beats"], "chords": []}
for _id, _p in SYNTH.items():
    PHRASES[_id] = {"family": "synth", "name": _p["name"], "bpm": SYNTH_BPM, "beats": SYNTH_BEATS,
                    "chords": _CHORDS, "style": _p["style"]}

PARTS = {"guitar": ("guitar",), "bass": ("bass",), "drums": DRUM_PARTS + (PERC_PART,), "synth": ("synth",)}


def parts_of(phrase_id: str) -> tuple:
    return PARTS[PHRASES[phrase_id]["family"]]


def _check_tempo(tempo: float) -> float:
    t = float(tempo)
    if not (TEMPO_MIN <= t <= TEMPO_MAX) or not np.isfinite(t):
        raise ValueError(f"темп {TEMPO_MIN}…{TEMPO_MAX}")
    return t


def _edges(y: np.ndarray, sr: int) -> np.ndarray:
    f = min(len(y) // 2, int(FADE_S * sr))
    if f:
        y = y.copy()
        y[:f] *= np.linspace(0.0, 1.0, f)
        y[-f:] *= np.linspace(1.0, 0.0, f)
    return y


@lru_cache(maxsize=8)
def _guitar_file(phrase_id: str, sr: int) -> np.ndarray:
    import soundfile as sf
    data, fsr = sf.read(str(DIR / f"{phrase_id}.flac"), dtype="float64", always_2d=True)
    y = data.mean(axis=1)
    if fsr != sr:
        from scipy import signal
        from math import gcd
        g = gcd(int(fsr), int(sr))
        y = signal.resample_poly(y, sr // g, int(fsr) // g)
    return y


def _guitar(phrase_id: str, tempo: float, sr: int) -> np.ndarray:
    y = _guitar_file(phrase_id, sr)
    if tempo != 1.0:
        import librosa
        target = int(round(len(y) / tempo))
        y = librosa.effects.time_stretch(y.astype(np.float32), rate=tempo).astype(np.float64)
        y = np.pad(y, (0, max(0, target - len(y))))[:target]
    return _edges(y, sr)


@lru_cache(maxsize=64)
def _bass_note(kit: str, midi: int, sr: int) -> np.ndarray:
    return drumsynth.render_bass(kit, midi, sr)


@lru_cache(maxsize=128)
def _drum_hit(part: str, layer: int, sr: int) -> np.ndarray:
    return drumsynth.render(DRUM_KIT, part, layer, sr)


def _place(out: np.ndarray, x: np.ndarray, at: int) -> None:
    """Звук с отсчёта at; то, что за концом круга, — в его начало (круг играет по кругу)."""
    n = len(out)
    at %= n
    k = min(len(x), n - at)
    out[at:at + k] += x[:k]
    rest = x[k:]
    while len(rest):
        m = min(len(rest), n)
        out[:m] += rest[:m]
        rest = rest[m:]


BASS_KIT = "growlybass/bass"   # настоящая бас-гитара (Squier Jazz, CC0) — если набор на воркере есть
_kit_cache: dict = {}


def _kit_bank(res, sr: int):
    """Сэмплы набора баса [(высота MIDI, моно-сэмпл с атаки, частота сэмпла)] (на набор — один раз); нет — None."""
    if res is None:
        return None
    key = (BASS_KIT, sr)
    if key not in _kit_cache:
        try:
            import fx_engine
            raw, ksr = res.kit(BASS_KIT)
            names = res.kit_names(BASS_KIT) if hasattr(res, "kit_names") else None
            notes = fx_engine._kit_notes(raw, ksr, names)
            # слои силы одной ноты — берём громкий (средний по пику из трёх ближайших не нужен: фраза ровная)
            best: dict = {}
            for midi, peak, a, s, _named in notes:
                k = int(round(midi))
                if k not in best or peak > best[k][0]:
                    best[k] = (peak, midi, np.asarray(s[a:], dtype=np.float64), ksr)
            bank = [(v[1], v[2], v[3]) for v in best.values()]
        except (KeyError, ValueError, OSError):
            bank = []                # набора нет — бас нотами синта
        if not bank:
            return None              # отсутствие не запоминается: набор, поставленный позже, подхватится сразу
        _kit_cache[key] = bank
    return _kit_cache[key]


def _kit_note(bank, midi: int, sr: int) -> np.ndarray:
    """Нота из ближайшего по высоте сэмпла: сдвиг высоты пересэмплированием (на полутон-два — без призвуков)."""
    from scipy import signal
    pitch, s, ksr = min(bank, key=lambda b: abs(b[0] - midi))
    ratio = 2 ** ((midi - pitch) / 12) * ksr / sr   # >1 — выше: сэмпл проигрывается быстрее
    n = max(1, int(len(s) / ratio))
    return signal.resample(s, n) if abs(ratio - 1) > 1e-6 else s


def _bass(p: dict, tempo: float, sr: int, res=None) -> np.ndarray:
    beat = 60.0 / (p["bpm"] * tempo)
    n = int(round(p["beats"] * beat * sr))
    out = np.zeros(n)
    bank = _kit_bank(res, sr)
    for midi, at, dur in p["notes"]:
        x = _kit_note(bank, midi, sr) if bank else _bass_note(p["kit"], midi, sr)
        k = min(len(x), int(round(dur * beat * sr)))
        x = x[:k].copy()
        r = min(k, int(0.03 * sr))   # отпускание ноты
        x[-r:] *= np.linspace(1.0, 0.0, r)
        _place(out, 0.7 * x, int(round(at * beat * sr)))
    return out


def _drums(p: dict, tempo: float, sr: int) -> dict:
    beat = 60.0 / (p["bpm"] * tempo)
    n = int(round(p["beats"] * beat * sr))
    out = {}
    for part in DRUM_PARTS:
        y = np.zeros(n)
        for h in p["hits"].get(part, []):
            src = _DRUM_SRC[part][h[2] if len(h) > 2 else 0]
            _place(y, 0.6 * _drum_hit(src, int(h[1]), sr), int(round(h[0] * beat * sr)))
        out[part] = y
    out[PERC_PART] = np.zeros(n)
    return out


def render_dry(phrase_id: str, tempo: float = 1.0, sr: int = SR, res=None) -> tuple[dict, float]:
    """Части фразы ({часть: моно}, одной длины) и длина круга в секундах. res — хранилище воркера: бас играет
    набором BASS_KIT (настоящая бас-гитара), без него — синт-басом нотами."""
    if phrase_id not in PHRASES:
        raise KeyError(phrase_id)
    tempo = _check_tempo(tempo)
    fam = PHRASES[phrase_id]["family"]
    if fam == "guitar":
        parts = {"guitar": _guitar(phrase_id, tempo, sr)}
    elif fam == "bass":
        parts = {"bass": _bass(BASS[phrase_id], tempo, sr, res)}
    elif fam == "synth":
        parts = {"synth": np.zeros(int(round(SYNTH_BEATS * 60.0 / (SYNTH_BPM * tempo) * sr)))}
    else:
        parts = _drums(DRUMS[phrase_id], tempo, sr)
    if fam != "guitar":
        # нотные — к громкости записей (−20 dBFS RMS сведения) с запасом по пику: части одним множителем
        mix = np.sum(list(parts.values()), axis=0)
        rms, peak = float(np.sqrt(np.mean(mix ** 2))), float(np.max(np.abs(mix)))
        if rms > 0:
            g = min(DRY_RMS / rms, DRY_PEAK / peak)
            parts = {k: v * g for k, v in parts.items()}
    n = len(next(iter(parts.values())))
    return parts, n / sr


def a_rms(x: np.ndarray, sr: int) -> float:
    import fx_engine
    y = fx_engine.a_weight(np.asarray(x, dtype=np.float64), sr)
    return float(np.sqrt(np.mean(np.square(y)))) if y.size else 0.0


def _twice(chain, cycle_s: float) -> list:
    """Ноты блоков synth/perc (от начала круга) — и во втором круге входа: вход из двух кругов играет их дважды."""
    out = []
    for b in chain:
        if b.get("type") in NOTE_BLOCKS and isinstance(b.get("notes"), list):
            b = dict(b, notes=list(b["notes"]) + [dict(nt, t=float(nt["t"]) + cycle_s) for nt in b["notes"]
                                                   if isinstance(nt, dict) and "t" in nt])
        out.append(b)
    return out


def loop_render(dry: dict, stems, chain, sr: int = SR, res=None, bypass: bool = False) -> tuple[np.ndarray, bool]:
    """Круг (n, 2): цепочка на части stems (пусто — все) по входу из двух кругов подряд, берётся второй — хвосты
    конца круга ложатся в его начало. Громкость — по сухому сведению (A-RMS; тихое — синт сам по себе — к −20 dBFS),
    пик не выше PEAK_MAX. bypass — без эффектов: остаются только блоки, которые сами играют ноты (synth, perc)."""
    chain = list(chain or [])
    if bypass:
        chain = [b for b in chain if b.get("type") in NOTE_BLOCKS]
        bypass = not chain
    names = list(dry)
    stems = list(stems) or names
    bad = [s for s in stems if s not in dry]
    if bad:
        raise ValueError(f"нет части: {', '.join(bad)}")
    n = len(dry[names[0]])
    drysum = np.sum([dry[k] for k in names], axis=0)
    out = np.repeat(drysum[:, None], 2, axis=1)
    if not bypass and chain:
        import fx_engine
        out = np.repeat(np.sum([dry[k] for k in names if k not in stems] or [np.zeros(n)], axis=0)[:, None], 2, axis=1)
        for s in stems:
            x2 = np.tile(np.asarray(dry[s], dtype=np.float64), 2)
            y = fx_engine.process(np.repeat(x2[:, None], 2, axis=1), sr, _twice(chain, n / sr), res).astype(np.float64)
            out = out + y[n:2 * n]
        ref, got = a_rms(drysum, sr), a_rms(out.mean(axis=1), sr)
        if ref <= SILENT_RMS:
            ref = DRY_RMS
        if got > SILENT_RMS:
            out *= ref / got
    peak = float(np.max(np.abs(out))) if out.size else 0.0
    clipped = peak > PEAK_MAX
    if clipped:
        out *= PEAK_MAX / peak
    return out, clipped

"""Сетка аккордов трека: аккорды и секции плана (score.abc) → такты звука. Чистый модуль (numpy + abcparse):
подбор сдвига плана относительно тактов звука по хроме гармонии. Эндпоинт — GET /jobs/{id}/chord_grid."""
from __future__ import annotations

import re

_ROOT = {"C": 0, "D": 2, "E": 4, "F": 5, "G": 7, "A": 9, "B": 11}
# интервалы от основного тона: терция, квинта, (септима)
_QUAL = {"": (4, 7), "maj": (4, 7), "m": (3, 7), "min": (3, 7), "dim": (3, 6), "aug": (4, 8),
         "7": (4, 7, 10), "maj7": (4, 7, 11), "m7": (3, 7, 10), "sus2": (2, 7), "sus4": (5, 7), "5": (7,)}
TIE = 0.01   # best_shift: сдвиги с мерой не хуже лучшей на 1 % — «почти равные»
_CHORD_RE = re.compile(r"^([A-G])([#b]?)(maj7|maj|min|m7|m|dim|aug|sus2|sus4|7|5)?(?:/[A-G][#b]?)?$")


def chord_pcs(name: str | None) -> list[int] | None:
    """«Dm» → [2, 5, 9] (основной тон, терция, квинта, …); нераспознанный — None. Бас через «/» не меняет состав."""
    m = _CHORD_RE.match((name or "").strip())
    if not m:
        return None
    root = (_ROOT[m.group(1)] + {"#": 1, "b": -1, "": 0}[m.group(2)]) % 12
    return [root] + [(root + i) % 12 for i in _QUAL[m.group(3) or ""]]


def plan_bars(abc_text: str) -> list[dict]:
    """Такты плана по порядку: [{chord, section}]. parse_abc отдаёт такты каждого голоса отдельно — такт плана =
    позиция по времени; аккорд — первый аккорд позиции у любого голоса; без аккорда — прежний."""
    from abcparse import parse_abc
    bars = parse_abc(abc_text)["bars"]
    pos: dict[float, dict] = {}
    order: list[float] = []
    for b in bars:
        k = round(float(b["start_sec"]), 3)
        if k not in pos:
            pos[k] = {"chord": None, "section": b.get("section") or ""}
            order.append(k)
        if pos[k]["chord"] is None and b.get("chords"):
            pos[k]["chord"] = b["chords"][0]
    out, prev = [], None
    for k in sorted(order):
        c = pos[k]["chord"] or prev
        out.append({"chord": c, "section": pos[k]["section"]})
        prev = c
    return out


def best_shift(plan_chords: list[str | None], beat_chroma, beats_per_bar: int = 4,
               shifts=range(-8, 17)) -> tuple[int, int]:
    """(сдвиг в тактах, фаза доли): такт плана k = такт звука с доли phase + (k + shift)·beats_per_bar. Мера —
    сумма по тактам (где есть и аккорд, и звук) доли хромы такта в нотах аккорда плана. Сумма, не среднее:
    при частичном наложении у края плана среднее выбирало ложный сдвиг с парой удачных тактов."""
    import numpy as np   # разбор аккордов и плана без numpy (тесты в окружении без него)
    beat_chroma = np.asarray(beat_chroma)
    nb = len(beat_chroma)
    pcs = [chord_pcs(c) for c in plan_chords]
    scores: dict[tuple[int, int], float] = {}
    for phase in range(beats_per_bar):
        for sh in shifts:
            tot, cnt = 0.0, 0
            for k, pc in enumerate(pcs):
                if pc is None:
                    continue
                a = phase + (k + sh) * beats_per_bar
                if a < 0 or a + beats_per_bar > nb:
                    continue
                c = beat_chroma[a:a + beats_per_bar].sum(axis=0)
                s = c.sum()
                if s <= 0:
                    continue
                tot += float(c[pc].sum() / s)
                cnt += 1
            if cnt >= 2:
                scores[(sh, phase)] = tot
    if not scores:
        return 0, 0
    # круг аккордов повторяется (4 такта) — сдвиги на период почти равны по мере; аккорды от этого не меняются,
    # а секции съехали бы: из почти равных (TIE от лучшего) — с наименьшим |сдвигом|, затем с меньшей фазой
    best = max(scores.values())
    near = [k for k, v in scores.items() if v >= best * (1 - TIE)]
    return min(near, key=lambda k: (abs(k[0]), k[0] < 0, k[1]))

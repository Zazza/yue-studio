"""Проверка изменённого плана (ABC) перед генерацией: что изменилось
относительно плана трека и где проблемы. Для MCP (continue_job / revoice_start /
plan_check): ИИ правит план сам — воркер говорит, что вышло.

Чистый модуль (без fastapi/numpy): тесты идут в системном python3.
Замер 2026-09-30: модель поёт мелодию голоса плана нота в ноту; выше потолка
(верх мелодии плана + 2 ступени) — писк/фальцет.
"""
import re

from abcparse import parse_abc

_LETTERS = "CDEFGAB"
# знаков у ноты не больше двух (^^, __): неограниченный повтор перед буквой
# разбирался квадратично (40 000 «=» — 18 с, ревью безопасности)
_NOTE_RE = re.compile(r"[=_^]{0,2}([A-Ga-g])([,']{0,4})")
_CHORD_RE = re.compile(r'"[^"]*"')
_MULTI_REST_RE = re.compile(r"^\s*Z(\d*)\s*$")
_VOICE_RE = re.compile(r"^V:\s*(\S+)")
_HEAD_RE = re.compile(r"^[A-Za-z]:")
CHANGED_LIMIT = 64
ABC_MAX_CHARS = 200_000   # реальные планы — единицы-десятки КБ
MULTI_REST_MAX = 4096     # тактов мультипауз на весь план (Z<n> в сумме): больше — не план,
                          # а попытка съесть память воркера (повтор Z512| обходил лимит «на каждую»)
_ANY_MULTI_REST_RE = re.compile(r"Z(\d+)")
CEILING_STEPS = 2


def note_step(token: str) -> int:
    """Ступень ноты по букве и октаве (C=0, c=7, c'=14, C,=-7); знаки не влияют."""
    m = _NOTE_RE.match(token.strip())
    if not m:
        raise ValueError(f"not a note: {token!r}")
    letter, octave = m.groups()
    step = _LETTERS.index(letter.upper()) + (7 if letter.islower() else 0)
    return step + 7 * octave.count("'") - 7 * octave.count(",")


def step_note(step: int) -> str:
    """Обратно: 9 → «e», 14 → «c'», -7 → «C,»."""
    if step >= 7:
        return _LETTERS[(step - 7) % 7].lower() + "'" * ((step - 7) // 7)
    return _LETTERS[step % 7] + "," * ((6 - step) // 7)


def voice_bars(abc: str) -> dict[str, list[str]]:
    """Тексты тактов по голосам в порядке плана; Z<n> — n тактов паузы."""
    out: dict[str, list[str]] = {}
    voice = None
    for raw in str(abc).splitlines():
        line = raw.strip()
        if not line or line.startswith("%"):
            continue
        vm = _VOICE_RE.match(line)
        if vm:
            voice = vm.group(1)
            rest = line[vm.end():].strip()
            # объявление голоса в заголовке (с clef=…) — не тело
            if not rest or "=" in rest:
                continue
            line = rest
        elif _HEAD_RE.match(line) or voice is None:
            continue
        chunks = line.split("|")
        if chunks and not chunks[-1].strip():
            chunks = chunks[:-1]   # финальная черта — не такт
        bars = out.setdefault(voice, [])
        for c in chunks:
            m = _MULTI_REST_RE.match(c)
            if m:
                bars.extend(["z"] * int(m.group(1) or 1))
            else:
                bars.append(c.strip())
    return out


def _notes(bar: str) -> list[int]:
    return [note_step(m.group(0)) for m in _NOTE_RE.finditer(_CHORD_RE.sub("", bar))]


def vocal_top(abc: str):
    """Верхняя ступень нот голоса(ов) Vocal; нот нет — None."""
    steps = [s for v, bars in voice_bars(abc).items() if "vocal" in v.lower()
             for b in bars for s in _notes(b)]
    return max(steps) if steps else None


def _timeline(abc: str) -> dict[str, list[tuple[float, float]]]:
    """Время тактов по голосам (как у ролла: abcparse)."""
    out: dict[str, list[tuple[float, float]]] = {}
    for b in parse_abc(abc)["bars"]:
        names = list((b.get("voices") or {}).keys()) or list((b.get("rests") or {}).keys())
        for v in names[:1]:
            out.setdefault(v, []).append((round(b["start_sec"], 3), round(b["end_sec"], 3)))
    return out


def plan_diff(old_abc: str, new_abc: str, from_sec=None) -> dict:
    """Сравнение нового плана с планом трека: такты, длина, изменённые такты
    (время по новому плану), потолок голоса, предупреждения."""
    if len(new_abc) > ABC_MAX_CHARS:
        raise ValueError(f"план длиннее {ABC_MAX_CHARS} символов")
    if sum(int(n) for n in _ANY_MULTI_REST_RE.findall(new_abc)) > MULTI_REST_MAX:
        raise ValueError(f"мультипауз больше {MULTI_REST_MAX} тактов на план")
    old_b, new_b = voice_bars(old_abc), voice_bars(new_abc)
    if not any(new_b.values()):
        raise ValueError("в плане нет тактов голосов (V: … и такты через |)")
    try:
        old_t, new_t = _timeline(old_abc), _timeline(new_abc)
        old_d, new_d = parse_abc(old_abc)["duration_sec"], parse_abc(new_abc)["duration_sec"]
    except (ZeroDivisionError, OverflowError, KeyError, IndexError) as e:
        raise ValueError(f"не разобрать размер/длительности плана (M:, L:, Q:): {e}") from None
    voices = list(dict.fromkeys([*old_b, *new_b]))
    changed, warnings = [], []
    for v in voices:
        ob, nb = old_b.get(v, []), new_b.get(v, [])
        if len(ob) != len(nb):
            warnings.append(f"{v}: тактов было {len(ob)}, стало {len(nb)} — всё после правки сдвинется")
        for i in range(max(len(ob), len(nb))):
            before = ob[i] if i < len(ob) else ""
            after = nb[i] if i < len(nb) else ""
            if before == after:
                continue
            times = new_t.get(v, []) if i < len(nb) else old_t.get(v, [])
            start, end = times[i] if i < len(times) else (None, None)
            changed.append({"voice": v, "bar": i + 1, "start": start, "end": end,
                            "before": before, "after": after})
    top, new_top = vocal_top(old_abc), vocal_top(new_abc)
    ceiling = top + CEILING_STEPS if top is not None else None
    if ceiling is not None:
        over = [c for c in changed if "vocal" in c["voice"].lower()
                and any(s > ceiling for s in _notes(c["after"]))]
        if over:
            where = ", ".join(f"такт {c['bar']} ({c['start']} с)" for c in over[:8])
            warnings.append(f"голос выше, чем потолок {step_note(ceiling)} (писк/фальцет): {where}")
    if from_sec:
        early = [c for c in changed if c["end"] is not None and c["end"] <= from_sec]
        if early:
            where = ", ".join(f"{c['voice']} такт {c['bar']}" for c in early[:8])
            warnings.append(f"правки до отметки {from_sec} с — продолжение их не сыграет: {where}")
    return {
        "bars": {v: [len(old_b.get(v, [])), len(new_b.get(v, []))] for v in voices},
        "duration": [round(float(old_d or 0), 1), round(float(new_d or 0), 1)],
        "changed": changed[:CHANGED_LIMIT],
        "changed_total": len(changed),
        "ceiling": {"top": step_note(top) if top is not None else None,
                    "ceiling": step_note(ceiling) if ceiling is not None else None,
                    "new_top": step_note(new_top) if new_top is not None else None},
        "warnings": warnings,
    }

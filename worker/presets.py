"""Пресеты звука: проверка рецепта, встроенные пресеты, переходы статуса пресета у трека.

Пресет — сохранённый рецепт обработки готового трека: правки по дорожкам на весь трек
(цепочка движка / эффект ffmpeg / педали) и финальная цепочка ffmpeg на весь микс. Применяет
его приложение (пересборка и ffmpeg — на ПК), воркер хранит рецепты и статусы у джоб.
Модуль без FastAPI: ошибки — PresetError (эндпоинт отдаёт 422 с причиной).
"""
from __future__ import annotations

import math

NAME_MAX = 80
NOTE_MAX = 500
SPECS_MAX = 16
STEPS_MAX = 12
FINAL_MAX = 12
CHAIN_ID_MAX = 40
DB_RANGE = (-24.0, 24.0)
ERROR_MAX = 500
TARGET_LUFS_RANGE = (-24.0, -6.0)
JOB_PRESETS_MAX = 3
KNOWN_STEMS = ("vocals", "drums", "bass", "other", "guitar", "piano",
               "kick", "snare", "toms", "hh", "ride", "crash")
# части барабанов пересборка обрабатывает только движком: эффект ffmpeg/педали на них молча пропали бы
DRUM_PARTS = ("kick", "snare", "toms", "hh", "ride", "crash")
KINDS = ("engine", "chain", "steps")


class PresetError(ValueError):
    """Неверный рецепт пресета — причина для 422."""


def _num(v) -> bool:
    # NaN/Infinity из JSON — не число для рецепта: ломали бы JSON ответа и графы ffmpeg
    return isinstance(v, (int, float)) and not isinstance(v, bool) and math.isfinite(v)


def _chain_id(where: str, v) -> str:
    if not isinstance(v, str) or not v.strip() or len(v) > CHAIN_ID_MAX:
        raise PresetError(f"{where}: chain — строка 1–{CHAIN_ID_MAX} символов")
    return v


def _params(where: str, v) -> dict:
    if v is None:
        return {}
    if not isinstance(v, dict) or not all(isinstance(k, str) and _num(x) for k, x in v.items()):
        raise PresetError(f"{where}: params — объект {{имя: число}}")
    return {k: float(x) for k, x in v.items()}


def _step(where: str, s) -> dict:
    if not isinstance(s, dict):
        raise PresetError(f"{where}: шаг — объект {{chain, params, off}}")
    off = s.get("off", False)
    if not isinstance(off, bool):
        raise PresetError(f"{where}: off — true/false")
    return {"chain": _chain_id(where, s.get("chain")), "params": _params(where, s.get("params")), "off": off}


def _steps(where: str, v, limit: int, allow_empty: bool) -> list:
    if not isinstance(v, list):
        raise PresetError(f"{where}: список шагов")
    if not v and not allow_empty:
        raise PresetError(f"{where}: пустой список шагов")
    if len(v) > limit:
        raise PresetError(f"{where}: не больше {limit} шагов (сейчас {len(v)})")
    return [_step(f"{where}, шаг {i + 1}", s) for i, s in enumerate(v)]


def _spec(where: str, s, parse_engine) -> dict:
    if not isinstance(s, dict):
        raise PresetError(f"{where}: запись — объект {{stems, engine | chain | steps, db}}")
    stems = s.get("stems")
    if not isinstance(stems, list) or not stems:
        raise PresetError(f"{where}: stems — непустой список дорожек")
    for st in stems:
        if st not in KNOWN_STEMS:
            raise PresetError(f"{where}: неизвестная дорожка {st!r} (есть: {', '.join(KNOWN_STEMS)})")
    kinds = [k for k in KINDS if s.get(k) not in (None, "", [])]
    if len(kinds) != 1:
        raise PresetError(f"{where}: нужно ровно одно из engine, chain, steps")
    db = s.get("db", 0)
    if not _num(db) or not DB_RANGE[0] <= db <= DB_RANGE[1]:
        raise PresetError(f"{where}: db — число {DB_RANGE[0]:g}…{DB_RANGE[1]:g}")
    out = {"stems": list(dict.fromkeys(stems)), "db": float(db)}
    kind = kinds[0]
    parts = [st for st in stems if st in DRUM_PARTS]
    if kind != "engine" and parts:
        raise PresetError(f"{where}: на части барабанов ({', '.join(parts)}) — только цепочка движка (engine)")
    if kind == "engine":
        try:
            parse_engine(s["engine"])
        except ValueError as e:     # ChainError движка — причина как есть
            raise PresetError(f"{where}: {e}") from e
        # хранится как задано: умолчания блоков дописывает движок при расчёте (новые умолчания —
        # и у старых пресетов), а не застывают в рецепте
        out["engine"] = [dict(b) for b in s["engine"]]
    elif kind == "chain":
        out["chain"] = _chain_id(where, s["chain"])
        out["params"] = _params(where, s.get("params"))
    else:
        out["steps"] = _steps(where, s["steps"], STEPS_MAX, allow_empty=False)
    return out


def validate(p: dict, parse_engine) -> dict:
    """Рецепт пресета → нормализованный {name, note, specs, final, reference_job_id}.
    parse_engine — проверка цепочки движка (fx_engine.parse_chain), ошибки — ValueError."""
    name = p.get("name")
    if not isinstance(name, str) or not name.strip() or len(name.strip()) > NAME_MAX:
        raise PresetError(f"name — 1–{NAME_MAX} символов")
    note = p.get("note") or ""
    if not isinstance(note, str) or len(note) > NOTE_MAX:
        raise PresetError(f"note — не длиннее {NOTE_MAX} символов")
    specs = p.get("specs", [])     # нет поля — пусто; null/false/{}/"" — ошибка типа, не «пусто»
    if not isinstance(specs, list):
        raise PresetError("specs — список записей")
    if len(specs) > SPECS_MAX:
        raise PresetError(f"specs: не больше {SPECS_MAX} записей (сейчас {len(specs)})")
    final = p.get("final", [])
    target = p.get("target_lufs")
    if target is not None and (not _num(target) or not TARGET_LUFS_RANGE[0] <= target <= TARGET_LUFS_RANGE[1]):
        raise PresetError(f"target_lufs — число {TARGET_LUFS_RANGE[0]:g}…{TARGET_LUFS_RANGE[1]:g} или null")
    ref = p.get("reference_job_id")
    if ref is not None and (not isinstance(ref, int) or isinstance(ref, bool)):
        raise PresetError("reference_job_id — номер трека или null")
    out = {
        "name": name.strip(), "note": note,
        "specs": [_spec(f"запись {i + 1}", s, parse_engine) for i, s in enumerate(specs)],
        "final": _steps("финал", final, FINAL_MAX, allow_empty=True),
        "reference_job_id": ref,
        "target_lufs": None if target is None else float(target),
    }
    # финал из одних выключенных шагов ничего не делает — как пустой (иначе версия = исходный звук);
    # одна целевая громкость — уже обработка (выравнивание)
    if not out["specs"] and not any(not st["off"] for st in out["final"]) and target is None:
        raise PresetError("пустой пресет: нет ни правок дорожек, ни включённого финала, ни целевой громкости")
    return out


def check_job_ids(ids, existing: set, draft: bool) -> list:
    """sound_preset_ids при создании трека: ≤ 3 разных существующих, у черновика — нельзя."""
    ids = list(ids or [])
    if not ids:
        return []
    if draft:
        raise PresetError("у черновика пресеты звука не применяются")
    if len(ids) > JOB_PRESETS_MAX:
        raise PresetError(f"не больше {JOB_PRESETS_MAX} пресетов на трек")
    if len(set(ids)) != len(ids):
        raise PresetError("пресет указан дважды")
    missing = [i for i in ids if i not in existing]
    if missing:
        raise PresetError(f"нет пресета {missing[0]}")
    return ids


# переходы статуса пресета у трека: откуда → куда разрешено
TRANSITIONS = {
    ("pending", "running"),           # захват приложением
    ("running", "done"), ("running", "error"),
    ("error", "pending"), ("running", "pending"),   # повтор
}


class TransitionError(Exception):
    """Переход не разрешён (уже захвачен, повтор готового…) — 409."""


def apply_state(items: list, pid: int, status: str, child_id=None, error=None) -> list:
    """Новый список пресетов трека после перехода; KeyError — пресета нет у трека,
    PresetError — не хватает поля, TransitionError — переход запрещён."""
    out = [dict(x) for x in items]
    it = next((x for x in out if x.get("id") == pid), None)
    if it is None:
        raise KeyError(pid)
    if (it.get("status"), status) not in TRANSITIONS:
        raise TransitionError(f"{it.get('status')} → {status} нельзя")
    if status == "done":
        if not isinstance(child_id, int) or isinstance(child_id, bool) or child_id <= 0:
            raise PresetError("done: нужен child_id — номер версии")
        it.update(status="done", child_id=child_id, error="")
    elif status == "error":
        if not isinstance(error, str) or not error.strip() or len(error) > ERROR_MAX:
            raise PresetError(f"error: причина 1–{ERROR_MAX} символов")
        it.update(status="error", error=error, child_id=0)
    else:
        it.update(status=status, error="", child_id=0)
    return out


def _eq(*bands, highpass=0):
    return {"type": "eq", "highpass_hz": highpass,
            "bands": [{"freq_hz": f, "gain_db": g, "q": 1.0} for f, g in bands]}


# встроенные пресеты: рецепты человека, собраны вручную через MCP и проверены на #376 и #383
BUILTIN = [
    {
        "slug": "transmission", "name": "Пост-панк · Transmission", "reference_job_id": 376,
        "note": "Живые бочка и малый, бас-гитара с серединой, гитары с провалом на 2,5 кГц, голос вперёд; "
                "шире и громче. Без песка верхов и мастера: песок давал «свист».",
        "specs": [
            {"stems": ["kick"], "engine": [{"type": "sampler", "kit": "osdk/kick", "output_db": 0}]},
            {"stems": ["snare"], "engine": [{"type": "sampler", "kit": "osdk/snare", "output_db": 2.5}]},
            # бас ведёт атмосферу пост-панка: громче (прослушивание 2026-10-08: было −6 и −3 на 150 Гц — тихо)
            {"stems": ["bass"], "engine": [{"type": "bass", "kit": "growlybass/bass", "division": 2, "output_db": 0},
                                           _eq((800, 4), highpass=80), {"type": "comp"}]},
            {"stems": ["other"], "engine": [_eq((2500, -3), (1200, 3), (4000, 2)), {"type": "comp"}]},
            {"stems": ["vocals"], "engine": [_eq((650, 3), (8000, 2)), {"type": "comp", "makeup_db": 6}]},
        ],
        # громкость — целью, а не «+5,5 дБ в ограничитель»: на громком треке это был перегруз (#666)
        "final": [{"chain": "width", "params": {"width": 1.1, "bass": 120}}], "target_lufs": -13.0,
    },
    {
        "slug": "sex-on-fire", "name": "Инди-рок · Sex on Fire", "reference_job_id": 383,
        "note": "Живые бочка и малый, плотный бас, гитары без резкого верха, голос с присутствием; "
                "закрытый верх, широко, громко. Многополосный компрессор не класть — раздувает верх и давит бас.",
        "specs": [
            {"stems": ["kick"], "engine": [{"type": "sampler", "kit": "osdk/kick", "output_db": 1}]},
            {"stems": ["snare"], "engine": [{"type": "sampler", "kit": "osdk/snare", "output_db": 1}]},
            {"stems": ["bass"], "engine": [_eq((150, 2), (700, 2), highpass=40), {"type": "comp"}]},
            {"stems": ["other"], "engine": [_eq((8000, -4), (2500, 3), (1000, 2)), {"type": "comp"}]},
            {"stems": ["vocals"], "engine": [_eq((2500, 2), (200, 2)), {"type": "comp", "makeup_db": 4}]},
        ],
        "final": [{"chain": "eq", "params": {"high": -3.5, "highf": 6500}},
                  {"chain": "width", "params": {"width": 1.2, "bass": 150}}], "target_lufs": -12.0,
    },
]

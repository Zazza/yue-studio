"""Свои инструменты страницы «Инструменты»: цепочка движка под своим именем (на основе готовой). Чистый модуль:
проверка тела запроса; хранение — таблица fx_instruments воркера, выбор в студии — вместе с готовыми."""
from __future__ import annotations

NAME_MAX = 80
GROUP_MAX = 40
STEMS = ("vocals", "drums", "bass", "other", "guitar", "piano", "kick", "snare", "toms", "hh", "ride", "crash",
         "synth", "perc")
EXTRA_KEYS = ("style", "octave", "pattern", "swing", "accent", "amp_hint", "place")


class InstrumentError(ValueError):
    pass


def validate(body: dict, parse_chain, partial: bool = False) -> dict:
    """Поля инструмента после проверки: name, base, group, stems, chain, extra. partial — правка: только пришедшие
    поля. parse_chain — fx_engine.parse_chain (ошибка цепочки → InstrumentError). Цепочка хранится как пришла."""
    if not isinstance(body, dict):
        raise InstrumentError("тело — объект")
    out: dict = {}
    if "name" in body or not partial:
        name = body.get("name")
        name = name.strip() if isinstance(name, str) else ""
        if not 1 <= len(name) <= NAME_MAX:
            raise InstrumentError(f"name — 1…{NAME_MAX} символов")
        out["name"] = name
    for key, limit in (("base", NAME_MAX), ("group", GROUP_MAX)):
        if key in body or not partial:
            v = body.get(key) or ""
            if not isinstance(v, str) or len(v) > limit:
                raise InstrumentError(f"{key} — строка до {limit} символов")
            out[key] = v
    if "stems" in body or not partial:
        stems = body.get("stems")
        if not isinstance(stems, list) or not stems or not all(isinstance(s, str) and s in STEMS for s in stems):
            raise InstrumentError(f"stems — непустой список из: {', '.join(STEMS)}")
        out["stems"] = stems
    if "chain" in body or not partial:
        chain = body.get("chain")
        if not isinstance(chain, list):
            raise InstrumentError("chain — список блоков")
        try:
            parse_chain(chain)
        except ValueError as e:
            raise InstrumentError(str(e)) from e
        out["chain"] = chain
    if "extra" in body or not partial:
        extra = body.get("extra") or {}
        if not isinstance(extra, dict):
            raise InstrumentError("extra — объект")
        out["extra"] = {k: v for k, v in extra.items() if k in EXTRA_KEYS}
    return out

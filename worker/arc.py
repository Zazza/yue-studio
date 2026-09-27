"""Драматургия трека: дуги темпа/вокала поверх плана YuE2.

Пользователь просит «нарастание» или «взрыв» — здесь это превращается в
правки ABC: смена темпа по секциям (Q: перед секцией) и, для «взрыва»,
подъём вокальной партии на октаву в финале (голос, а не громкость:
громкостную дугу YuE2 не строит, а вот октаву в партитуре поёт).
"""
import re

ARCS = ("", "build", "wave", "burst")

# множители темпа по секциям (от базового Q заголовка); last_* — финальные
_TEMPO_MAP = {
    "build": {"intro": 0.85, "verse": 1.0, "chorus": 1.0, "interlude": 1.0,
              "last_chorus": 1.12, "last_*": 1.12},
    "wave": {"intro": 0.95, "verse": 0.96, "chorus": 1.08, "interlude": 1.0,
             "last_chorus": 1.1, "last_*": 1.08},
    "burst": {"intro": 0.55, "verse": 1.0, "chorus": 1.0, "interlude": 0.72,
              "last_chorus": 1.17, "last_*": 1.17},
}

# контраст доставки в строке стиля (голос в финале — экспрессия исполнителем)
_STYLE_SUFFIX = {
    "build": ("dynamic arc, starts restrained and sparse, gradually adds energy, "
              "final chorus is the emotional peak"),
    "wave": ("dynamic contrast: verses restrained and calm, choruses energetic and loud, "
             "alternating waves of intensity"),
    "burst": ("dramatic arc: verses sung restrained deadpan, quiet heavy intro, stark "
              "breakdown before the final chorus, FINAL CHORUS sung with desperate soaring "
              "intensity, voice rises to an emotional climax, high register"),
}

_NOTE = re.compile(r"(?<![A-Za-z])([=_^]?)([A-Ga-g])([,\"\']*)")
_SEC = re.compile(r"^%\s*(\w+)")


def _octave_up(m: re.Match) -> str:
    acc, note, octv = m.groups()
    if note.isupper():
        return acc + note.lower() + octv.replace(",", "")
    return acc + note + octv.replace(",", "") + "'"


def _base_bpm(abc: str) -> float:
    m = re.search(r"^Q:1/4=(\d+(?:\.\d+)?)", abc, re.M)
    return float(m.group(1)) if m else 0.0


def style_with_arc(style: str, arc: str) -> str:
    """Строка стиля + дескрипторы дуги (контраст доставки голоса)."""
    sfx = _STYLE_SUFFIX.get(arc, "")
    return f"{style}, {sfx}" if sfx and sfx not in style else style


def apply_arc(abc: str, arc: str) -> str:
    """ABC плана + дуга: Q перед секциями по темповой карте; для burst —
    вокальная партия финальной секции поднимается на октаву."""
    if arc not in _TEMPO_MAP or not abc.strip():
        return abc
    tmap = _TEMPO_MAP[arc]
    base = _base_bpm(abc)
    if base <= 0:
        return abc
    lines = abc.split("\n")
    # имена секций по порядку; финальная chorus/интерлюдия после последней verse
    sec_idx = [i for i, ln in enumerate(lines) if _SEC.match(ln)]
    names = [_SEC.match(lines[i]).group(1).lower() for i in sec_idx]
    last_chorus_i = max((i for i, nm in enumerate(names) if "chorus" in nm), default=-1)
    last_i = len(names) - 1
    out, cur_voice, final_zone = [], None, False
    for i, ln in enumerate(lines):
        if ln.startswith("V:"):
            cur_voice = ln.split(":", 1)[1].split()[0] if len(ln.split(":", 1)) > 1 else None
        si = sec_idx.index(i) if i in sec_idx else None
        if si is not None:
            nm = names[si]
            is_last_chorus = si == last_chorus_i
            is_last = si == last_i and arc == "burst"
            final_zone = is_last_chorus or is_last
            key = "last_chorus" if is_last_chorus else nm
            mult = tmap.get(key, tmap.get("last_*") if is_last else 1.0)
            if mult and mult != 1.0:
                out.append(f"Q:1/4={round(base * mult)}")
        if final_zone and arc == "burst" and cur_voice == "Vocal" \
                and ln and not ln.startswith(("V:", "%", "Q:", "X:", "T:", "M:", "L:", "K:", "w:")):
            ln = _NOTE.sub(_octave_up, ln)
        out.append(ln)
    return "\n".join(out)

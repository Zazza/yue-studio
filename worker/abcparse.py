"""Парсер score.abc ( формат YuE/SheetSage2 ) → таймлайн для пиано-ролла.

ABC-диалект YuE: заголовок (X/T/M/L/Q/V/K), затем такты с | , голоса
переключаются строками «V: Name», аккорды в "кавычках", паузы z<dur>,
комментарии % — секции. Длительности — в единицах L (по умолчанию 1).
"""
import re

NOTE_RE = re.compile(r"[=_^]?[A-Ga-g][,']*(\d+)?")
REST_RE = re.compile(r"z(\d+)?")
CHORD_RE = re.compile(r'"([^"]*)"')
MULTI_REST_RE = re.compile(r"\s*Z(\d*)\s*")


def _header_field(line: str, prefix: str) -> str | None:
    if line.startswith(prefix):
        return line[len(prefix):].strip()
    return None


def parse_abc(text: str) -> dict:
    """→ {tempo_bpm, key, meter, unit, voices, bars: [...], sections}.

    bars[i] = {idx, section, voices: {name: notes}, chords: [..],
               dur_units, start_sec, end_sec, density}
    """
    tempo = 120.0
    # время — по темпу ЗАГОЛОВКА (первый Q:): YuE смену темпа посреди плана не
    # исполняет (замер: приём «темп +10%» — бочка осталась 120 BPM); раньше
    # бралось последнее Q: файла, и после приёма весь ролл сжимался
    head_tempo: float | None = None
    key, meter, unit = "", "4/4", 1
    voice_order: list[str] = []
    voices: dict[str, str] = {}
    raw_bars: list[dict] = []
    cur_voice = voice_order[0] if voice_order else "Main"
    section = "intro"
    buf = ""

    def flush_bar():
        nonlocal buf
        if not buf.strip():
            buf = ""
            return
        # аккорды в "кавычках" — аннотации: не считаются ни нотами, ни длительностью
        body = CHORD_RE.sub(" ", buf)
        # мультипауза Z<n> — n целых тактов тишины: без разворота голос отставал
        # на n−1 тактов (таймлайн ролла Ins кончался на 152 с вместо 210)
        if (mr := MULTI_REST_RE.fullmatch(body)):
            for _ in range(int(mr.group(1) or 1)):
                raw_bars.append({"section": section, "voices": {cur_voice: 0},
                                 "rests": {cur_voice: 0}, "chords": [], "dur_units": 0})
            buf = ""
            return
        notes = len(NOTE_RE.findall(body))
        rests = sum(int(m or 1) for m in REST_RE.findall(body))
        # длительность: каждая нота/пауза без цифры = 1 unit, с цифрой = dur
        dur = 0
        for m in re.finditer(r"(?:[=_^]?[A-Ga-g][,']*|z)(\d+)?", body):
            d = m.group(1)
            dur += int(d) if d else 1
        raw_bars.append({
            "section": section,
            "voices": {cur_voice: notes},
            "rests": {cur_voice: rests},
            "chords": [c for c in CHORD_RE.findall(buf) if c],
            "dur_units": dur,
        })
        buf = ""

    for line in text.splitlines():
        s = line.strip()
        if not s:
            continue
        if s.startswith("%"):
            section = s.lstrip("%").strip() or section
            continue
        if (v := _header_field(s, "V:")):
            flush_bar()
            name = v.split()[0]
            cur_voice = name
            if name not in voices:
                voice_order.append(name)
                voices[name] = " ".join(v.split()[1:])
            continue
        if (q := _header_field(s, "Q:")):
            m = re.search(r"=\s*(\d+)", q)
            if m:
                tempo = float(m.group(1))
                if head_tempo is None:
                    head_tempo = tempo
            continue
        if (k := _header_field(s, "K:")):
            key = k.split()[0] if k.split() else k
            continue
        if (m := _header_field(s, "M:")):
            meter = m
            continue
        if (lf := _header_field(s, "L:")):
            mm = re.match(r"1/(\d+)", lf)
            if mm:
                unit = 1.0 / float(mm.group(1))
            continue
        if s[0].isalpha() and s[1:2] == ":":
            continue  # прочие поля заголовка
        for ch in s:
            if ch == "|":
                flush_bar()
            else:
                buf += ch
    flush_bar()

    # длительности: unit = длительность одной ABC-единицы в четвертях.
    # L:1/32 → единица = 1/8 четверти. tempo — четвертей в минуту.
    if head_tempo is None:
        head_tempo = tempo
    n, d = meter.split("/")
    beats_per_bar = float(n) * 4.0 / float(d)          # четвертей в такте
    unit_quarters = unit * 4.0                          # единица в четвертях

    # Время — у каждого голоса свой ход часов: голоса звучат одновременно, но
    # в диалекте YuE их такты идут последовательными блоками (секция Vocal,
    # затем секция Ins). Сквозной накопитель раздувал таймлайн почти вдвое
    # от реального трека и сдвигал «тот же момент» у второго голоса.
    vt: dict[str, float] = {}
    bars = []
    for i, b in enumerate(raw_bars):
        dur_sec = (b["dur_units"] * 60.0 / head_tempo * unit_quarters if b["dur_units"]
                   else beats_per_bar * 60.0 / head_tempo)
        v = next(iter(b["voices"]), "")
        t = vt.get(v, 0.0)
        bars.append({
            "idx": i,
            "section": b["section"],
            "voices": b["voices"],
            "rests": b["rests"],
            "chords": b["chords"],
            "start_sec": round(t, 2),
            "end_sec": round(t + dur_sec, 2),
        })
        vt[v] = t + dur_sec
    return {
        "tempo_bpm": head_tempo,
        "key": key,
        "meter": meter,
        "unit": unit,
        "voices": voices,
        "voice_order": voice_order,
        "bars": bars,
        "duration_sec": round(max(vt.values(), default=0.0), 2),
    }

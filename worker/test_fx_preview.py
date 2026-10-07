"""Тесты карточки internal-instruments-page (этап 3 звукового движка): режим превью
POST /jobs/{id}/fx (тест-кейсы 4–6) и единый источник описания блоков
worker/fx_blocks.json (тест-кейс 7).

Контракт (из карточки и контракта задачи):
- поле запроса preview (по умолчанию false); при true обязательны from и to, иначе 422;
- хвост tail = 3 с, если в цепочке есть reverb или delay, иначе 0;
  end = min(to + tail, длина трека);
- seg = дорожка-источник [from, to), дополненная тишиной до end − from;
  wet = fx_engine.process(seg); output=solo → файл = wet;
  output=mix (или source=mix) → файл = трек[from, end) + (wet − seg);
- файл preview-fx-<8 hex>.flac; уже есть — возвращается без пересчёта; без метрик,
  в GET /jobs/{id}/dsp не попадает; ответ {file, duration_sec, clipped};
- на джобу не больше 8 файлов preview-fx-*.flac (старейшие по mtime удаляются),
  dsp-* и preview-* не от движка не трогаются;
- движок выключен → 503;
- fx_engine.SPEC / STR_SPEC / BAND_SPEC строятся из worker/fx_blocks.json, умолчания
  и границы — те же, что на этапе 2; копии frontend/src/fxBlocks.json и
  internal/mcp/fx_blocks.json побайтно равны источнику.

Внешние границы подменяются так же, как в test_fx_api: fx_resources — фейк,
NAM не нужен (в цепочках превью нет amp). torch, GPU и сеть не нужны.

Запуск: cd worker && python3 -m unittest test_fx_preview -v
"""
import json
import os
import re
import time
import unittest
from pathlib import Path
from unittest import mock

import test_fx_api as fa
from test_fx_api import CUT_1K, IDENTITY, SR, _read, _residual_db

try:
    import numpy  # noqa: F401
    import scipy  # noqa: F401
    _HAS_NP = True
except ImportError:
    _HAS_NP = False

ROOT = Path(__file__).resolve().parent.parent
BLOCKS_JSON = Path(__file__).resolve().with_name("fx_blocks.json")
TYPES = ("gate", "eq", "comp", "drive", "amp", "cab", "reverb", "delay", "gain", "sampler")
PREVIEW_RE = re.compile(r"^preview-fx-[0-9a-f]{8}\.flac$")
TAIL = 3.0
MAX_PREVIEWS = 8
# цепочка с хвостом: эхо через 0,5 с
ECHO = [{"type": "delay", "time_ms": 500, "feedback": 0.3, "wet": 0.5}]


def _preview_files(d):
    return sorted(p.name for p in d.glob("preview-fx-*.flac"))


# ---------- ТК4–6: режим превью ----------

@unittest.skipUnless(fa._OK, fa._SKIP)
class _PreviewCase(fa._FxApiCase):

    def _preview(self, jid, chain, fr, to, **body):
        return self._fx(jid, chain=chain, preview=True, **{"from": fr, "to": to}, **body)

    def _ok_preview(self, jid, chain, fr, to, **body):
        r = self._preview(jid, chain, fr, to, **body)
        self.assertEqual(r.status_code, 200, r.text)
        return r.json()

    @staticmethod
    def _cut(x, fr, end):
        """Кусок [fr, end) в сэмплах; за концом сигнала — тишина (дополнение)."""
        import numpy as np
        a, b = int(round(fr * SR)), int(round(end * SR))
        out = np.zeros((b - a, x.shape[1]), dtype=x.dtype)
        part = x[a:min(b, len(x))]
        out[:len(part)] = part
        return out

    def _expected(self, d, source, chain, fr, to, output):
        """Ожидаемое содержимое превью по формуле контракта."""
        import fx_engine
        track, _ = _read(d / "audio.flac", dtype="float32")
        src = track if source == "mix" else _read(d / f"stem-{source}.flac", dtype="float32")[0]
        has_tail = any(b["type"] in ("reverb", "delay") for b in chain)
        end = min(to + (TAIL if has_tail else 0.0), len(track) / SR)
        seg = self._cut(src, fr, end)
        seg[int(round(to * SR)) - int(round(fr * SR)):] = 0  # после окна — тишина
        wet = fx_engine.process(seg, SR, chain).astype("float64")
        if output == "solo" and source != "mix":
            return wet, end
        return self._cut(track, fr, end).astype("float64") + (wet - seg.astype("float64")), end


class TestPreviewFile(_PreviewCase):
    """ТК4: файл превью — кусок окна (+ хвост), содержимое — обработка этого куска."""

    def test_tc4_response_name_and_no_metrics(self):
        jid, d = self._audio_job()
        out = self._ok_preview(jid, CUT_1K, 1.0, 2.0, source="vocals", output="solo")
        self.assertTrue(PREVIEW_RE.match(out.get("file", "")), out)
        for k in ("file", "duration_sec", "clipped"):
            self.assertIn(k, out)
        self.assertNotIn("metrics", out, "превью без метрик")
        self.assertIsInstance(out["clipped"], bool)
        self.assertTrue((d / out["file"]).is_file())

    def test_tc4_no_tail_without_reverb_delay(self):
        jid, d = self._audio_job()
        out = self._ok_preview(jid, CUT_1K, 1.0, 2.0, source="vocals", output="solo")
        y, sr = _read(d / out["file"])
        self.assertEqual(sr, SR)
        self.assertAlmostEqual(len(y) / SR, 1.0, delta=2 / SR)
        self.assertAlmostEqual(out["duration_sec"], 1.0, delta=0.01)

    def test_tc4_tail_3s_with_delay(self):
        # окно 0,2–0,5 с, трек 4 с: хвост целиком влезает → 0,3 + 3 = 3,3 с
        jid, d = self._audio_job()
        out = self._ok_preview(jid, ECHO, 0.2, 0.5, source="vocals", output="solo")
        y, _ = _read(d / out["file"])
        self.assertAlmostEqual(len(y) / SR, 0.3 + TAIL, delta=2 / SR)
        self.assertAlmostEqual(out["duration_sec"], 0.3 + TAIL, delta=0.01)

    def test_tc4_tail_with_reverb_too(self):
        jid, d = self._audio_job()
        out = self._ok_preview(jid, [{"type": "reverb"}], 0.2, 0.5, source="vocals", output="solo")
        y, _ = _read(d / out["file"])
        self.assertAlmostEqual(len(y) / SR, 0.3 + TAIL, delta=2 / SR)

    def test_tc4_tail_not_past_track_end(self):
        # окно 2–3 с, трек 4 с: хвост обрезан концом трека → 2 с, не 4
        jid, d = self._audio_job()
        out = self._ok_preview(jid, ECHO, 2.0, 3.0, source="vocals", output="solo")
        y, _ = _read(d / out["file"])
        self.assertAlmostEqual(len(y) / SR, fa.DUR - 2.0, delta=2 / SR)

    def test_tc4_solo_content_is_processed_window(self):
        jid, d = self._audio_job()
        for chain in (CUT_1K, ECHO):
            with self.subTest(chain=chain):
                out = self._ok_preview(jid, chain, 0.5, 1.5, source="vocals", output="solo")
                y, _ = _read(d / out["file"])
                want, _ = self._expected(d, "vocals", chain, 0.5, 1.5, "solo")
                self.assertEqual(y.shape, want.shape)
                self.assertLessEqual(_residual_db(y, want), -60)

    def test_tc4_mix_content_is_track_window_with_processed_stem(self):
        jid, d = self._audio_job()
        for chain in (CUT_1K, ECHO):
            with self.subTest(chain=chain):
                out = self._ok_preview(jid, chain, 0.5, 1.5, source="vocals", output="mix")
                y, _ = _read(d / out["file"])
                want, _ = self._expected(d, "vocals", chain, 0.5, 1.5, "mix")
                self.assertEqual(y.shape, want.shape)
                self.assertLessEqual(_residual_db(y, want), -60)
        # смысл: голос (1 кГц) приглушён, остальное на месте
        out = self._ok_preview(jid, CUT_1K, 1.0, 3.0, source="vocals", output="mix")
        y, _ = _read(d / out["file"])
        self.assertLess(fa._db(fa._amp(y, 1000, 0.0, 2.0) / fa.AMP), -18)
        self.assertAlmostEqual(fa._db(fa._amp(y, fa.HZ["drums"], 0.0, 2.0) / fa.AMP), 0, delta=1)

    def test_tc4_output_defaults_to_mix(self):
        jid, d = self._audio_job()
        out = self._ok_preview(jid, CUT_1K, 1.0, 2.0, source="vocals")
        y, _ = _read(d / out["file"])
        want, _ = self._expected(d, "vocals", CUT_1K, 1.0, 2.0, "mix")
        self.assertLessEqual(_residual_db(y, want), -60)

    def test_tc4_source_mix(self):
        jid, d = self._audio_job()
        for chain in (CUT_1K, ECHO):
            with self.subTest(chain=chain):
                out = self._ok_preview(jid, chain, 0.5, 1.5, source="mix")
                y, _ = _read(d / out["file"])
                want, _ = self._expected(d, "mix", chain, 0.5, 1.5, "mix")
                self.assertEqual(y.shape, want.shape)
                self.assertLessEqual(_residual_db(y, want), -60)

    def test_tc4_not_in_dsp_list(self):
        jid, d = self._audio_job()
        variant = self._ok(jid, source="vocals", chain=CUT_1K)["file"]
        out = self._ok_preview(jid, CUT_1K, 1.0, 2.0, source="vocals")
        r = self.client.get(f"/jobs/{jid}/dsp")
        self.assertEqual(r.status_code, 200, r.text)
        listed = [v["file"] for v in r.json()]
        self.assertNotIn(out["file"], listed)
        self.assertIn(variant, listed, "обычный вариант пропал из списка")
        # превью — не вариант: новых dsp-fx-* не появилось
        self.assertEqual(self._fx_files(d), [variant])

    def test_repeat_same_file_without_recompute(self):
        import fx_engine
        jid, d = self._audio_job()
        with mock.patch.object(fx_engine, "process", wraps=fx_engine.process) as proc:
            a = self._ok_preview(jid, ECHO, 1.0, 2.0, source="vocals", output="solo")
            calls = proc.call_count
            self.assertGreaterEqual(calls, 1, "превью посчитано мимо fx_engine.process")
            # время изменения при попадании в кэш обновляется (вытеснение — давно не слушанных,
            # решение человека 2026-10-07), поэтому «не перезаписан» — по содержимому
            before = (d / a["file"]).read_bytes()
            b = self._ok_preview(jid, ECHO, 1.0, 2.0, source="vocals", output="solo")
            self.assertEqual(proc.call_count, calls, "повтор пересчитан")
        self.assertEqual(a["file"], b["file"])
        self.assertEqual((d / b["file"]).read_bytes(), before, "файл перезаписан")
        self.assertEqual(_preview_files(d), [a["file"]])
        self.assertAlmostEqual(a["duration_sec"], b["duration_sec"], delta=1e-6)

    def test_different_settings_different_file(self):
        jid, d = self._audio_job()
        files = {
            self._ok_preview(jid, CUT_1K, 1.0, 2.0, source="vocals", output="solo")["file"],
            self._ok_preview(jid, IDENTITY, 1.0, 2.0, source="vocals", output="solo")["file"],
            self._ok_preview(jid, CUT_1K, 1.5, 2.0, source="vocals", output="solo")["file"],
            self._ok_preview(jid, CUT_1K, 1.0, 2.5, source="vocals", output="solo")["file"],
            self._ok_preview(jid, CUT_1K, 1.0, 2.0, source="vocals", output="mix")["file"],
            self._ok_preview(jid, CUT_1K, 1.0, 2.0, source="drums", output="solo")["file"],
        }
        self.assertEqual(len(files), 6)
        self.assertEqual(len(_preview_files(d)), 6)

    def test_equivalent_chain_same_file(self):
        # хэш — от цепочки после parse_chain: явные умолчания = пропущенные
        import fx_engine
        jid, _ = self._audio_job()
        explicit = fx_engine.parse_chain(ECHO)
        a = self._ok_preview(jid, ECHO, 1.0, 2.0, source="vocals", output="solo")["file"]
        b = self._ok_preview(jid, explicit, 1.0, 2.0, source="vocals", output="solo")["file"]
        self.assertTrue(PREVIEW_RE.match(a), a)
        self.assertEqual(a, b)


class TestPreviewErrors(_PreviewCase):
    """ТК5 + выключенный движок."""

    def test_tc5_preview_without_window_422(self):
        jid, d = self._audio_job()
        for extra in ({}, {"from": 1.0}, {"to": 2.0}):
            with self.subTest(window=extra):
                r = self._fx(jid, source="vocals", chain=CUT_1K, preview=True, **extra)
                self.assertEqual(r.status_code, 422, r.text)
        self.assertEqual(_preview_files(d), [])
        self.assertEqual(self._fx_files(d), [], "без окна превью не должно стать вариантом")

    def test_bad_window_422(self):
        jid, d = self._audio_job()
        for fr, to in ((2.0, 1.0), (1.0, 1.0), (-1.0, 1.0), (1.0, fa.DUR + 5)):
            with self.subTest(window=(fr, to)):
                self.assertEqual(self._preview(jid, CUT_1K, fr, to, source="vocals").status_code, 422)
        self.assertEqual(_preview_files(d), [])

    def test_bad_chain_422(self):
        jid, d = self._audio_job()
        r = self._preview(jid, [{"type": "fuzz"}], 1.0, 2.0, source="vocals")
        self.assertEqual(r.status_code, 422, r.text)
        self.assertEqual(_preview_files(d), [])

    def test_job_not_found_404(self):
        self.assertEqual(self._preview(9999, CUT_1K, 1.0, 2.0, source="mix").status_code, 404)

    def test_engine_disabled_503(self):
        jid, d = self._audio_job()
        self.assertLess(self.client.post("/config", json={"fx_engine": False}).status_code, 300)
        r = self._preview(jid, CUT_1K, 1.0, 2.0, source="vocals")
        self.assertEqual(r.status_code, 503, r.text)
        self.assertEqual(_preview_files(d), [])

    def test_preview_false_is_variant(self):
        # preview=false — обычный путь этапа 2: вариант dsp-fx-*, превью нет
        jid, d = self._audio_job()
        out = self._ok(jid, source="vocals", chain=CUT_1K, preview=False)
        self.assertTrue(fa.NAME_RE.match(out["file"]), out)
        self.assertEqual(_preview_files(d), [])


class TestPreviewLimit(_PreviewCase):
    """ТК6: не больше 8 превью движка на джобу, чужие файлы не трогаются."""

    def test_tc6_ninth_evicts_oldest(self):
        import numpy as np
        import soundfile as sf
        jid, d = self._audio_job()
        variant = self._ok(jid, source="vocals", chain=CUT_1K)["file"]
        # «чужое» превью (не от движка) и временный файл другого вида
        other = d / "preview-0123abcd.flac"
        sf.write(str(other), np.zeros((SR // 10, 2), dtype=np.float32), SR)
        base = time.time() - 1000
        os.utime(other, (base - 100, base - 100))  # старее всех — всё равно не трогается
        files = []
        for k in range(MAX_PREVIEWS + 1):
            fr = 0.1 * k
            name = self._ok_preview(jid, CUT_1K, fr, fr + 0.5, source="vocals")["file"]
            files.append(name)
            if k < MAX_PREVIEWS:
                # порядок по mtime — явно, чтобы не зависеть от разрешения часов ФС
                os.utime(d / name, (base + k, base + k))
        self.assertEqual(len(set(files)), MAX_PREVIEWS + 1)
        left = _preview_files(d)
        self.assertEqual(len(left), MAX_PREVIEWS, left)
        self.assertNotIn(files[0], left, "самое старое превью не удалено")
        self.assertEqual(sorted(files[1:]), left)
        self.assertTrue(other.is_file(), "удалено превью не от движка")
        self.assertTrue((d / variant).is_file(), "удалён вариант dsp-*")
        self.assertTrue((d / "audio.flac").is_file())
        self.assertTrue((d / "stem-vocals.flac").is_file())

    def test_cache_hit_refreshes_mtime(self):
        # попадание в кэш обновляет время изменения (os.utime, решение человека 2026-10-07):
        # вытесняется давно не слушанное, а не давно посчитанное
        jid, d = self._audio_job()
        base = time.time() - 1000
        a = self._ok_preview(jid, CUT_1K, 0.0, 0.5, source="vocals")["file"]
        os.utime(d / a, (base, base))  # A — самое старое
        others = []
        for k in range(1, MAX_PREVIEWS):
            fr = 0.1 * k
            name = self._ok_preview(jid, CUT_1K, fr, fr + 0.5, source="vocals")["file"]
            os.utime(d / name, (base + k, base + k))
            others.append(name)
        self.assertEqual(len(_preview_files(d)), MAX_PREVIEWS)
        again = self._ok_preview(jid, CUT_1K, 0.0, 0.5, source="vocals")["file"]  # повтор A
        self.assertEqual(again, a)
        self._ok_preview(jid, CUT_1K, 1.0, 1.5, source="vocals")  # новое — девятое
        left = _preview_files(d)
        self.assertEqual(len(left), MAX_PREVIEWS, left)
        self.assertIn(a, left, "вытеснено превью, которое только что слушали (кэш не обновил mtime)")
        self.assertNotIn(others[0], left, "вытеснено не самое давно слушанное")

    def test_tc6_eight_kept(self):
        jid, d = self._audio_job()
        for k in range(MAX_PREVIEWS):
            self._ok_preview(jid, CUT_1K, 0.1 * k, 0.1 * k + 0.5, source="vocals")
        self.assertEqual(len(_preview_files(d)), MAX_PREVIEWS)

    def test_tc6_limit_per_job(self):
        # лимит считается на джобу: превью другой джобы не вытесняют
        jid1, d1 = self._audio_job()
        jid2, d2 = self._audio_job()
        first = self._ok_preview(jid1, CUT_1K, 0.0, 0.5, source="vocals")["file"]
        for k in range(MAX_PREVIEWS):
            self._ok_preview(jid2, CUT_1K, 0.1 * k, 0.1 * k + 0.5, source="vocals")
        self.assertEqual(_preview_files(d1), [first])


class TestPreviewCacheVersion(_PreviewCase):
    """Регрессия ревью: кэш превью зависит от версии файлов-входов (дорожка, IR)."""

    @staticmethod
    def _rewrite(path, x):
        import soundfile as sf
        sf.write(str(path), x, SR)
        st = path.stat()
        # mtime явно вперёд — не зависеть от разрешения часов ФС
        os.utime(path, ns=(st.st_atime_ns, st.st_mtime_ns + 10 * 10**9))

    def test_rewritten_stem_recomputed(self):
        import fx_engine
        jid, d = self._audio_job()
        with mock.patch.object(fx_engine, "process", wraps=fx_engine.process) as proc:
            a = self._ok_preview(jid, IDENTITY, 1.0, 2.0, source="vocals", output="solo")
            calls = proc.call_count
            y1, _ = _read(d / a["file"])
            # дорожку перезаписали: тот же голос, но вдвое громче и на другой ноте
            self._rewrite(d / "stem-vocals.flac", fa._tone(2000, amp=2 * fa.AMP))
            b = self._ok_preview(jid, IDENTITY, 1.0, 2.0, source="vocals", output="solo")
            self.assertGreater(proc.call_count, calls, "дорожка сменилась, а превью взято из кэша")
        y2, _ = _read(d / b["file"])
        self.assertAlmostEqual(fa._db(fa._amp(y2, 2000, 0.0, 1.0) / fa.AMP), 6.0, delta=1)
        self.assertLess(fa._db(fa._amp(y2, 1000, 0.0, 1.0) / fa.AMP), -40, "в превью старая дорожка")
        self.assertGreater(_residual_db(y1, y2), -20)

    def test_rewritten_ir_recomputed(self):
        import fx_engine
        import numpy as np
        from test_fx_api import _wav_bytes
        # настоящее хранилище IR, не фейк ресурсов
        p = mock.patch.object(self.w, "fx_resources", self.real_fx_resources)
        p.start()
        self.addCleanup(p.stop)

        def upload(data):
            r = self.client.post("/fx/assets", params={"kind": "ir", "name": "room.wav"}, content=data)
            self.assertEqual(r.status_code, 200, r.text)

        upload(_wav_bytes(0.5, 48000))  # импульс на 10-м сэмпле
        jid, d = self._audio_job()
        for block in ("cab", "reverb"):
            with self.subTest(block=block):
                chain = [{"type": block, "ir": "room.wav"}]
                with mock.patch.object(fx_engine, "process", wraps=fx_engine.process) as proc:
                    a = self._ok_preview(jid, chain, 1.0, 2.0, source="vocals", output="solo")
                    calls = proc.call_count
                    y1, _ = _read(d / a["file"])
                    # перезалили IR под тем же именем: другая форма — второй импульс
                    # через 0,1 с (громкость не годится: IR может нормироваться)
                    import io

                    import soundfile as sf
                    buf = io.BytesIO()
                    ir = np.zeros(int(0.5 * 48000), dtype=np.float32)
                    ir[10] = 1.0
                    ir[10 + 4800] = 0.8
                    sf.write(buf, ir, 48000, format="WAV")
                    upload(buf.getvalue())
                    path = self.data / "fx" / "irs" / "room.wav"
                    if path.is_file():
                        st = path.stat()
                        os.utime(path, ns=(st.st_atime_ns, st.st_mtime_ns + 10 * 10**9))
                    b = self._ok_preview(jid, chain, 1.0, 2.0, source="vocals", output="solo")
                    self.assertGreater(proc.call_count, calls, "IR сменился, а превью взято из кэша")
                y2, _ = _read(d / b["file"])
                self.assertGreater(_residual_db(y1, y2), -20, "превью посчитано со старым IR")
                upload(_wav_bytes(0.5, 48000))  # вернуть исходный IR для следующего блока


class TestPreviewConcurrent(_PreviewCase):
    """Регрессия ревью: два одинаковых превью одновременно."""

    def test_two_same_previews_in_parallel(self):
        import threading

        import fx_engine
        jid, d = self._audio_job()
        before = {p.name for p in d.iterdir()}
        real = fx_engine.process
        barrier = threading.Barrier(2)

        def process(*a, **kw):
            out = real(*a, **kw)
            try:
                # оба запроса доходят до записи одновременно
                barrier.wait(timeout=3)
            except threading.BrokenBarrierError:
                pass  # реализация сериализует расчёт — тоже допустимо, ждать дальше незачем
            return out

        results = [None, None]

        def run(i):
            results[i] = self._preview(jid, ECHO, 1.0, 2.0, source="vocals", output="solo")

        with mock.patch.object(fx_engine, "process", process):
            ts = [threading.Thread(target=run, args=(i,)) for i in range(2)]
            for t in ts:
                t.start()
            for t in ts:
                t.join(timeout=60)
        for i, r in enumerate(results):
            self.assertIsNotNone(r, f"запрос {i} не завершился")
            self.assertEqual(r.status_code, 200, r.text)
        a, b = results[0].json(), results[1].json()
        self.assertEqual(a["file"], b["file"])
        y, _ = _read(d / a["file"])
        want, _ = self._expected(d, "vocals", ECHO, 1.0, 2.0, "solo")
        self.assertEqual(y.shape, want.shape)
        self.assertLessEqual(_residual_db(y, want), -60)
        self.assertEqual(list(d.glob("*.part")), [], "остались временные файлы")
        after = {p.name for p in d.iterdir()}
        self.assertEqual(after - before, {a["file"]}, "лишние файлы в каталоге джобы")


class TestPreviewClipped(_PreviewCase):
    """Регрессия ревью: clipped считается и для превью из кэша."""

    BOOST = [{"type": "eq", "bands": [{"freq_hz": 1000, "gain_db": 24, "q": 1}]}]

    def test_overload_clipped_true_also_from_cache(self):
        jid, d = self._audio_job()
        a = self._ok_preview(jid, self.BOOST, 1.0, 2.0, source="vocals", output="solo")
        self.assertIs(a["clipped"], True, "голос 0,1 + 24 дБ ≈ 1,6 — перегруз")
        b = self._ok_preview(jid, self.BOOST, 1.0, 2.0, source="vocals", output="solo")
        self.assertEqual(a["file"], b["file"])
        self.assertIs(b["clipped"], True, "из кэша перегруз потерян")

    def test_no_overload_clipped_false_also_from_cache(self):
        jid, d = self._audio_job()
        a = self._ok_preview(jid, CUT_1K, 1.0, 2.0, source="vocals", output="solo")
        self.assertIs(a["clipped"], False)
        b = self._ok_preview(jid, CUT_1K, 1.0, 2.0, source="vocals", output="solo")
        self.assertEqual(a["file"], b["file"])
        self.assertIs(b["clipped"], False)


class TestPreviewEvictRace(_PreviewCase):
    """Регрессия ревью: файл превью удалён между перечислением каталога и stat."""

    def test_evict_survives_vanished_file(self):
        import glob as globmod
        import pathlib
        jid, d = self._audio_job()
        base = time.time() - 1000
        for k in range(MAX_PREVIEWS):
            name = self._ok_preview(jid, CUT_1K, 0.1 * k, 0.1 * k + 0.5, source="vocals")["file"]
            os.utime(d / name, (base + k, base + k))
        ghost = "preview-fx-00000000.flac"  # «был в листинге, но уже удалён»
        self.assertFalse((d / ghost).exists())
        real_glob, real_iter = pathlib.Path.glob, pathlib.Path.iterdir
        real_gg, real_ld = globmod.glob, os.listdir

        def in_job(p):
            return Path(p).resolve() == d.resolve()

        def p_glob(self_, pattern, *a, **kw):
            res = list(real_glob(self_, pattern, *a, **kw))
            if in_job(self_) and Path(ghost).match(pattern):
                res.append(self_ / ghost)
            return iter(res)

        def p_iter(self_):
            res = list(real_iter(self_))
            if in_job(self_):
                res.append(self_ / ghost)
            return iter(res)

        def g_glob(pattern, *a, **kw):
            res = real_gg(pattern, *a, **kw)
            pat = Path(pattern)
            if in_job(pat.parent) and Path(ghost).match(pat.name):
                res.append(str(pat.parent / ghost))
            return res

        def listdir(path="."):
            res = real_ld(path)
            if in_job(path):
                res.append(ghost)
            return res

        with mock.patch.object(pathlib.Path, "glob", p_glob), \
                mock.patch.object(pathlib.Path, "iterdir", p_iter), \
                mock.patch.object(globmod, "glob", g_glob), \
                mock.patch.object(os, "listdir", listdir):
            r = self._preview(jid, CUT_1K, 2.0, 2.5, source="vocals")
        self.assertEqual(r.status_code, 200, r.text)
        self.assertTrue((d / r.json()["file"]).is_file())
        self.assertLessEqual(len(_preview_files(d)), MAX_PREVIEWS)


# ---------- Признак превью в /config (решение кросс-ревью) ----------

class TestConfigAdvertisesPreview(_PreviewCase):
    """GET /config отдаёт fx_preview: true — по нему страница «Инструменты»
    отличает воркер с режимом превью от воркера этапа 2."""

    def test_config_has_fx_preview_true(self):
        r = self.client.get("/config")
        self.assertEqual(r.status_code, 200, r.text)
        self.assertIs(r.json().get("fx_preview"), True, r.json())


# ---------- Этап 4 (internal-studio-engine), ТК7: поле fade превью ----------
#
# Контракт: fade — секунды, 0 ≤ fade ≤ 0,5 (иначе 422), по умолчанию 0, только при
# preview. a = round(from·sr), b = round(to·sr), F = round(fade·sr);
# end = min(b + F + tail, n); seg[k] = part[a+k]·w(k) при a+k < min(b+F, n), иначе 0;
# w(k) = min(1, k/F)·min(1, (b+F−(a+k))/F) (линейно, как dsp.WindowGraph); F = 0 —
# поведение этапа 3. fade входит в ключ кэша.

FADE = 0.05


class _FadeCase(_PreviewCase):

    def _expected_fade(self, d, source, chain, fr, to, fade):
        """Ожидаемый файл solo по формуле контракта: обработанный вход с фейдами."""
        import fx_engine
        import numpy as np
        src, _ = _read(d / f"stem-{source}.flac", dtype="float32")
        n = len(src)
        a, b, F = int(round(fr * SR)), int(round(to * SR)), int(round(fade * SR))
        has_tail = any(blk["type"] in ("reverb", "delay") for blk in chain)
        tail = int(round(TAIL * SR)) if has_tail else 0
        end = min(b + F + tail, n)
        seg = np.zeros((end - a, src.shape[1]), dtype=np.float64)
        stop = min(b + F, n)
        seg[:stop - a] = src[a:stop]
        if F > 0:
            k = np.arange(end - a, dtype=np.float64)
            w = np.minimum(1.0, k / F) * np.minimum(1.0, (b + F - (a + k)) / F)
            seg *= np.clip(w, 0.0, 1.0)[:, None]
        wet = fx_engine.process(seg.astype(np.float32), SR, chain).astype("float64")
        return wet, seg, end - a


class TestPreviewFade(_FadeCase):
    """ТК7: вход превью с линейными краями fade, длина, ключ кэша, ошибки."""

    def test_tc7_identity_input_has_linear_edges(self):
        # тождественная цепочка: файл = вход превью; голос — тон 1 кГц
        import numpy as np
        jid, d = self._audio_job()
        out = self._ok_preview(jid, IDENTITY, 1.0, 2.0, source="vocals", output="solo", fade=FADE)
        y, _ = _read(d / out["file"])
        src, _ = _read(d / "stem-vocals.flac")
        F = int(round(FADE * SR))
        a, b = SR, 2 * SR
        rms = lambda x: float(np.sqrt(np.mean(np.asarray(x) ** 2)))  # noqa: E731
        # первые fade — линейный рост 0→1: RMS = 1/√3 от исходного, первая половина тише второй
        self.assertAlmostEqual(rms(y[:F]) / rms(src[a:a + F]), 1 / np.sqrt(3), delta=0.03)
        self.assertLess(rms(y[:F // 2]), rms(y[F // 2:F]) * 0.5)
        self.assertLess(abs(y[0]).max(), 1e-3, "вход начинается не с нуля")
        # середина окна — как исходная дорожка
        self.assertLessEqual(_residual_db(y[F:b - a], src[a + F:b]), -60)
        # после to — линейный спад 1→0 за fade, дальше тишина (+ хвост тождественной цепочки)
        self.assertAlmostEqual(rms(y[b - a:b - a + F]) / rms(src[b:b + F]), 1 / np.sqrt(3), delta=0.03)
        self.assertGreater(rms(y[b - a:b - a + F // 2]), rms(y[b - a + F // 2:b - a + F]) * 2)
        self.assertLess(abs(y[b - a + F:]).max(), 1e-3, "после to + fade вход не тишина")

    def test_tc7_content_matches_contract(self):
        jid, d = self._audio_job()
        for chain in (CUT_1K, ECHO, IDENTITY):
            with self.subTest(chain=chain):
                out = self._ok_preview(jid, chain, 0.5, 1.5, source="vocals", output="solo", fade=FADE)
                y, _ = _read(d / out["file"])
                want, _, _ = self._expected_fade(d, "vocals", chain, 0.5, 1.5, FADE)
                self.assertEqual(y.shape, want.shape)
                self.assertLessEqual(_residual_db(y, want), -60)

    def test_tc7_length_without_tail(self):
        # без reverb/delay: to + fade − from
        jid, d = self._audio_job()
        out = self._ok_preview(jid, CUT_1K, 1.0, 2.0, source="vocals", output="solo", fade=FADE)
        y, _ = _read(d / out["file"])
        self.assertAlmostEqual(len(y) / SR, 1.0 + FADE, delta=2 / SR)
        self.assertAlmostEqual(out["duration_sec"], 1.0 + FADE, delta=0.01)

    def test_tc7_length_with_tail(self):
        # с delay: to + fade + хвост − from (влезает в трек 4 с)
        jid, d = self._audio_job()
        out = self._ok_preview(jid, ECHO, 0.2, 0.5, source="vocals", output="solo", fade=FADE)
        y, _ = _read(d / out["file"])
        self.assertAlmostEqual(len(y) / SR, 0.3 + FADE + TAIL, delta=2 / SR)

    def test_tc7_length_capped_by_track_end(self):
        # to + fade за концом трека: файл до конца трека, не длиннее
        jid, d = self._audio_job()
        out = self._ok_preview(jid, CUT_1K, 3.0, 3.98, source="vocals", output="solo", fade=FADE)
        y, _ = _read(d / out["file"])
        self.assertAlmostEqual(len(y) / SR, fa.DUR - 3.0, delta=2 / SR)
        want, _, _ = self._expected_fade(d, "vocals", CUT_1K, 3.0, 3.98, FADE)
        self.assertEqual(y.shape, want.shape)
        self.assertLessEqual(_residual_db(y, want), -60)

    def test_tc7_fade_zero_is_stage3(self):
        # fade = 0 — как без поля (этап 3): то же содержимое и длина
        jid, d = self._audio_job()
        for chain in (CUT_1K, ECHO):
            with self.subTest(chain=chain):
                a = self._ok_preview(jid, chain, 0.5, 1.5, source="vocals", output="solo", fade=0)
                b = self._ok_preview(jid, chain, 0.5, 1.5, source="vocals", output="solo")
                ya, _ = _read(d / a["file"])
                yb, _ = _read(d / b["file"])
                self.assertEqual(ya.shape, yb.shape)
                self.assertLessEqual(_residual_db(ya, yb), -60)
                want, _ = self._expected(d, "vocals", chain, 0.5, 1.5, "solo")
                self.assertEqual(ya.shape, want.shape)
                self.assertLessEqual(_residual_db(ya, want), -60)

    def test_tc7_max_fade_accepted(self):
        jid, d = self._audio_job()
        out = self._ok_preview(jid, CUT_1K, 1.0, 2.0, source="vocals", output="solo", fade=0.5)
        y, _ = _read(d / out["file"])
        self.assertAlmostEqual(len(y) / SR, 1.5, delta=2 / SR)

    def test_tc7_different_fade_different_file(self):
        jid, d = self._audio_job()
        files = {
            self._ok_preview(jid, CUT_1K, 1.0, 2.0, source="vocals", output="solo", fade=f)["file"]
            for f in (0, 0.05, 0.1)
        }
        self.assertEqual(len(files), 3, "fade не входит в ключ кэша")
        self.assertEqual(len(_preview_files(d)), 3)

    def test_tc7_same_fade_same_file(self):
        jid, d = self._audio_job()
        a = self._ok_preview(jid, CUT_1K, 1.0, 2.0, source="vocals", output="solo", fade=FADE)
        b = self._ok_preview(jid, CUT_1K, 1.0, 2.0, source="vocals", output="solo", fade=FADE)
        self.assertEqual(a["file"], b["file"])
        self.assertEqual(len(_preview_files(d)), 1)

    def test_tc7_bad_fade_422(self):
        jid, d = self._audio_job()
        for bad in (0.6, -1, -0.01, 0.51, "abc"):
            with self.subTest(fade=bad):
                r = self._preview(jid, CUT_1K, 1.0, 2.0, source="vocals", output="solo", fade=bad)
                self.assertEqual(r.status_code, 422, r.text)
        self.assertEqual(_preview_files(d), [])


# ---------- Этап 4 (internal-studio-engine), условие 10: поле pad превью ----------
#
# Контракт: pad (по умолчанию false, только превью) — файл от начала трека: тишина
# a = round(from·sr) сэмплов, дальше тот же кусок, что без pad; длина файла = end
# (отсчёт от 0); duration_sec — длина файла; pad входит в ключ кэша.


class TestPreviewPad(_FadeCase):
    """Условие 10: pad=true — кусок превью от начала трека (для вставки без задержки)."""

    # (цепочка, from, to, fade): некруглый from, хвост delay, хвост до конца трека, fade=0
    CASES = (
        (CUT_1K, 1.0, 2.0, FADE),
        (CUT_1K, 1.2345, 2.3456, FADE),
        (ECHO, 0.2, 0.5, FADE),
        (ECHO, 2.0, 3.0, FADE),
        (CUT_1K, 0.5, 1.5, 0),
    )

    def _pair(self, jid, d, chain, fr, to, fade):
        a = self._ok_preview(jid, chain, fr, to, source="vocals", output="solo", fade=fade, pad=True)
        b = self._ok_preview(jid, chain, fr, to, source="vocals", output="solo", fade=fade, pad=False)
        ya, sra = _read(d / a["file"])
        yb, _ = _read(d / b["file"])
        return a, ya, sra, yb

    def test_pad_silence_then_same_chunk(self):
        import numpy as np
        jid, d = self._audio_job()
        for chain, fr, to, fade in self.CASES:
            with self.subTest(chain=chain, fr=fr, to=to, fade=fade):
                _, ya, sr, yb = self._pair(jid, d, chain, fr, to, fade)
                self.assertEqual(sr, SR)
                a = int(round(fr * SR))
                self.assertEqual(len(ya), a + len(yb), "pad: длина ≠ round(from·sr) + длина куска без pad")
                self.assertEqual(float(np.abs(ya[:a]).max()), 0.0, "до round(from·sr) не тишина")
                self.assertLessEqual(_residual_db(ya[a:], yb), -60, "после тишины — не тот же кусок, что без pad")

    def test_pad_length_is_end_from_zero(self):
        jid, d = self._audio_job()
        n = int(round(fa.DUR * SR))
        for chain, fr, to, fade in self.CASES:
            with self.subTest(chain=chain, fr=fr, to=to, fade=fade):
                out, ya, _, _ = self._pair(jid, d, chain, fr, to, fade)
                b, F = int(round(to * SR)), int(round(fade * SR))
                has_tail = any(blk["type"] in ("reverb", "delay") for blk in chain)
                end = min(b + F + (int(round(TAIL * SR)) if has_tail else 0), n)
                self.assertEqual(len(ya), end, "pad: длина файла ≠ end (отсчёт от начала трека)")
                self.assertAlmostEqual(out["duration_sec"], len(ya) / SR, delta=0.001,
                                       msg="duration_sec ≠ длина файла (с точностью до мс)")

    def test_pad_chunk_matches_contract(self):
        # содержимое после тишины — формула контракта (вход с фейдами → цепочка)
        jid, d = self._audio_job()
        out = self._ok_preview(jid, CUT_1K, 1.2345, 2.3456, source="vocals", output="solo", fade=FADE, pad=True)
        y, _ = _read(d / out["file"])
        want, _, _ = self._expected_fade(d, "vocals", CUT_1K, 1.2345, 2.3456, FADE)
        a = int(round(1.2345 * SR))
        self.assertEqual(y[a:].shape, want.shape)
        self.assertLessEqual(_residual_db(y[a:], want), -60)

    def test_pad_from_zero_no_silence(self):
        # from = 0: тишины спереди нет, файл = кусок без pad
        jid, d = self._audio_job()
        _, ya, _, yb = self._pair(jid, d, CUT_1K, 0.0, 1.0, FADE)
        self.assertEqual(ya.shape, yb.shape)
        self.assertLessEqual(_residual_db(ya, yb), -60)

    def test_pad_in_cache_key(self):
        jid, d = self._audio_job()
        a = self._ok_preview(jid, CUT_1K, 1.0, 2.0, source="vocals", output="solo", fade=FADE, pad=True)
        b = self._ok_preview(jid, CUT_1K, 1.0, 2.0, source="vocals", output="solo", fade=FADE)
        a2 = self._ok_preview(jid, CUT_1K, 1.0, 2.0, source="vocals", output="solo", fade=FADE, pad=True)
        self.assertNotEqual(a["file"], b["file"], "pad не входит в ключ кэша")
        self.assertEqual(a["file"], a2["file"], "тот же запрос с pad — другой файл")
        self.assertEqual(len(_preview_files(d)), 2)
        ya, _ = _read(d / a["file"])
        self.assertEqual(len(ya), int(round(2.0 * SR)) + int(round(FADE * SR)), "из кэша отдан файл без pad")

    def test_pad_default_false(self):
        # без поля — как pad=false (этап 3/4 без изменений): тот же файл
        jid, d = self._audio_job()
        a = self._ok_preview(jid, CUT_1K, 1.0, 2.0, source="vocals", output="solo", fade=FADE)
        b = self._ok_preview(jid, CUT_1K, 1.0, 2.0, source="vocals", output="solo", fade=FADE, pad=False)
        self.assertEqual(a["file"], b["file"])
        y, _ = _read(d / a["file"])
        self.assertAlmostEqual(len(y) / SR, 1.0 + FADE, delta=2 / SR)


# ---------- Условие 14 (internal-studio-engine, этап 5а): превью с sampler ----------
#
# Регрессия кросс-ревью. Контракт: превью с sampler — хвост FX_SAMPLER_TAIL_S = 1 с
# (удар у конца окна не обрывается): end = min(b + F + 1 с, n); кэш превью учитывает
# файлы набора (<data>/fx/kits/<набор>/<часть>/*.wav): заменили wav — пересчёт.
# Набор — настоящее хранилище воркера (файлы), дорожка-часть kick — щелчки.

SAMPLER_TAIL = 1.0
KIT = "mykit/kick"
SAMPLER = [{"type": "sampler", "kit": KIT, "floor_db": -40}]
HITS_S = (0.7, 1.0, 1.4)


def _kit_wav(hz, dur=0.5, peak=0.8, sr=SR):
    """Сэмпл набора: тон hz с атакой 2 мс и медленным спадом (длиннее хвоста окна)."""
    import io

    import numpy as np
    import soundfile as sf
    k = np.arange(int(dur * sr))
    att = int(0.002 * sr)
    env = np.where(k <= att, k / att, np.exp(-(k - att) / (0.15 * sr)))
    x = (peak * env * np.sin(2 * np.pi * hz * k / sr)).astype(np.float32)
    buf = io.BytesIO()
    sf.write(buf, x, sr, format="WAV", subtype="PCM_24")
    return buf.getvalue()


@unittest.skipUnless(fa._OK and _HAS_NP, fa._SKIP)
class TestPreviewSampler(_PreviewCase):

    def setUp(self):
        super().setUp()
        p = mock.patch.object(self.w, "fx_resources", self.real_fx_resources)
        p.start()
        self.addCleanup(p.stop)
        self.kit_dir = self.data / "fx" / "kits" / KIT
        self.kit_dir.mkdir(parents=True)

    def _put(self, name, hz):
        path = self.kit_dir / name
        existed = path.exists()
        old = path.stat().st_mtime_ns if existed else 0
        path.write_bytes(_kit_wav(hz))
        if existed:
            st = path.stat()
            # mtime явно вперёд — не зависеть от разрешения часов ФС
            os.utime(path, ns=(st.st_atime_ns, max(st.st_mtime_ns, old) + 10 * 10**9))
        return path

    def _kick_job(self):
        from test_fx_engine import _hits
        n = int(fa.DUR * SR)
        hits = _hits([int(t * SR) for t in HITS_S], [0.8] * len(HITS_S), n, sr=SR)
        return self._audio_job(extra={"kick": hits})

    def test_tail_1s(self):
        self._put("a.wav", 300)
        jid, d = self._kick_job()
        for fr, to, fade, want in ((0.5, 1.5, 0.0, 1.0 + SAMPLER_TAIL),
                                   (0.5, 1.5, 0.05, 1.0 + 0.05 + SAMPLER_TAIL),
                                   (1.2, 1.7, 0.05, 0.5 + 0.05 + SAMPLER_TAIL)):
            with self.subTest(fr=fr, to=to, fade=fade):
                out = self._ok_preview(jid, SAMPLER, fr, to, source="kick", output="solo", fade=fade)
                y, _ = _read(d / out["file"])
                self.assertAlmostEqual(len(y) / SR, want, delta=2 / SR)
                self.assertAlmostEqual(out["duration_sec"], want, delta=0.01)

    def test_tail_keeps_hit_near_window_end(self):
        # удар на 1,4 с, окно до 1,5 с: сэмпл (0,5 с) звучит и после to + fade
        import numpy as np
        self._put("a.wav", 300)
        jid, d = self._kick_job()
        out = self._ok_preview(jid, SAMPLER, 0.5, 1.5, source="kick", output="solo", fade=0.05)
        y, _ = _read(d / out["file"])
        lo, hi = int((1.6 - 0.5) * SR), int((1.8 - 0.5) * SR)
        self.assertGreater(len(y), hi, "превью без хвоста")
        tail = float(np.abs(y[lo:hi]).max())
        self.assertGreater(tail, 0.01 * float(np.abs(y).max()), "удар у конца окна оборван")

    def test_tail_not_past_track_end(self):
        # трек 4 с, окно 2,5–3,5 с + fade 0,05 + 1 с → обрезано концом трека: 1,5 с
        self._put("a.wav", 300)
        jid, d = self._kick_job()
        for fade in (0.0, 0.05):
            with self.subTest(fade=fade):
                out = self._ok_preview(jid, SAMPLER, 2.5, 3.5, source="kick", output="solo", fade=fade)
                y, _ = _read(d / out["file"])
                self.assertAlmostEqual(len(y) / SR, fa.DUR - 2.5, delta=2 / SR)

    def test_replaced_kit_wav_recomputed(self):
        import fx_engine
        self._put("a.wav", 300)
        jid, d = self._kick_job()
        with mock.patch.object(fx_engine, "process", wraps=fx_engine.process) as proc:
            a = self._ok_preview(jid, SAMPLER, 0.5, 1.5, source="kick", output="solo")
            calls = proc.call_count
            y1, _ = _read(d / a["file"])
            # тот же файл набора, новое содержимое (другой тон) и mtime
            self._put("a.wav", 2000)
            b = self._ok_preview(jid, SAMPLER, 0.5, 1.5, source="kick", output="solo")
            self.assertGreater(proc.call_count, calls, "файл набора сменился, а превью взято из кэша")
        y2, _ = _read(d / b["file"])
        self.assertGreater(_residual_db(y1, y2), -20, "превью посчитано со старым сэмплом")
        self.assertGreater(fa._amp(y2, 2000, 0.2, 0.4), 3 * fa._amp(y2, 300, 0.2, 0.4),
                           "в превью старый сэмпл")

    def test_added_kit_wav_recomputed(self):
        import fx_engine
        self._put("a.wav", 300)
        jid, d = self._kick_job()
        with mock.patch.object(fx_engine, "process", wraps=fx_engine.process) as proc:
            self._ok_preview(jid, SAMPLER, 0.5, 1.5, source="kick", output="solo")
            calls = proc.call_count
            self._put("b.wav", 2000)
            self._ok_preview(jid, SAMPLER, 0.5, 1.5, source="kick", output="solo")
            self.assertGreater(proc.call_count, calls, "в наборе новый файл, а превью взято из кэша")

    def test_same_kit_same_file_from_cache(self):
        # контроль: набор не менялся — второй раз из кэша, без пересчёта
        import fx_engine
        self._put("a.wav", 300)
        jid, _ = self._kick_job()
        with mock.patch.object(fx_engine, "process", wraps=fx_engine.process) as proc:
            a = self._ok_preview(jid, SAMPLER, 0.5, 1.5, source="kick", output="solo")
            calls = proc.call_count
            b = self._ok_preview(jid, SAMPLER, 0.5, 1.5, source="kick", output="solo")
            self.assertEqual(proc.call_count, calls)
        self.assertEqual(a["file"], b["file"])


# ---------- ТК7: один источник описания блоков ----------

def _blocks():
    return json.loads(BLOCKS_JSON.read_text(encoding="utf-8"))


class TestBlocksCopies(unittest.TestCase):
    """Копии для фронта и MCP побайтно равны источнику (генерирует make mcp-data)."""

    def test_tc7_source_exists_and_valid(self):
        self.assertTrue(BLOCKS_JSON.is_file(), BLOCKS_JSON)
        b = _blocks()
        self.assertEqual(tuple(b), TYPES, "типы блоков и их порядок показа")

    def test_gain_block_described(self):
        # условие 12: блок «громкость» — один параметр gain_db, −24…+24, по умолчанию 0
        g = _blocks()["gain"]
        self.assertEqual([p["id"] for p in g["params"]], ["gain_db"])
        p = g["params"][0]
        self.assertEqual((p["default"], p["min"], p["max"]), (0, -24, 24))
        self.assertFalse(p.get("zero_off"))
        self.assertFalse(g.get("strings"))

    def test_tc7_copies_identical(self):
        src = BLOCKS_JSON.read_bytes()
        for rel in ("frontend/src/fxBlocks.json", "internal/mcp/fx_blocks.json"):
            with self.subTest(copy=rel):
                p = ROOT / rel
                self.assertTrue(p.is_file(), f"нет копии {rel} (make mcp-data)")
                self.assertEqual(p.read_bytes(), src, f"{rel} отличается от worker/fx_blocks.json")

    def test_tc7_labels_ru_en(self):
        for t, blk in _blocks().items():
            items = [("блок", blk)] + [(p["id"], p) for p in blk.get("params", [])] + \
                [(s["id"], s) for s in blk.get("strings", [])] + \
                [(k, v) for k, v in blk.get("bands", {}).items()]
            for name, item in items:
                with self.subTest(type=t, item=name):
                    lab = item.get("label") or {}
                    for lang in ("ru", "en"):
                        self.assertIsInstance(lab.get(lang), str)
                        self.assertTrue(lab[lang].strip())

    def test_tc7_params_well_formed(self):
        for t, blk in _blocks().items():
            for p in blk.get("params", []):
                with self.subTest(type=t, param=p.get("id")):
                    for k in ("default", "min", "max"):
                        self.assertIsInstance(p.get(k), (int, float))
                    self.assertLessEqual(p["min"], p["max"])
                    ok = p["min"] <= p["default"] <= p["max"] or (p.get("zero_off") and p["default"] == 0)
                    self.assertTrue(ok, f"умолчание {p['default']} вне [{p['min']}, {p['max']}]")
        self.assertIn("bands", _blocks()["eq"])
        for t, blk in _blocks().items():
            if t != "eq":
                self.assertNotIn("bands", blk, t)


# Значения этапа 2 (поведение воркера не меняется): параметр → (умолчание, мин, макс).
# У eq.lowpass_hz допустимо 0 (выкл) или [1000, 22000].
STAGE2 = {
    "gate": {"threshold_db": (-50, -90, 0), "range_db": (-40, -90, 0),
             "attack_ms": (1, 0.1, 50), "release_ms": (100, 5, 1000)},
    "eq": {"highpass_hz": (0, 0, 1000), "lowpass_hz": (0, 1000, 22000)},
    "comp": {"threshold_db": (-20, -60, 0), "ratio": (4, 1, 20), "attack_ms": (10, 0.1, 200),
             "release_ms": (100, 5, 2000), "makeup_db": (0, -12, 24)},
    "drive": {"gain_db": (12, 0, 48), "mix": (1, 0, 1), "output_db": (0, -24, 12)},
    "amp": {"input_db": (0, -24, 24), "output_db": (0, -24, 24)},
    "cab": {"cutoff_hz": (7000, 2000, 12000), "mix": (1, 0, 1)},
    "reverb": {"decay_s": (1.5, 0.1, 10), "predelay_ms": (10, 0, 200),
               "lowpass_hz": (8000, 1000, 20000), "wet": (0.3, 0, 1)},
    "delay": {"time_ms": (375, 1, 2000), "feedback": (0.35, 0, 0.95),
              "lowpass_hz": (6000, 1000, 20000), "wet": (0.3, 0, 1)},
}


def _need(t):
    """Обязательные строковые поля блока для проверки parse_chain."""
    return {"amp": {"model": "fake"}, "sampler": {"kit": "fake/kick"}}.get(t, {})


@unittest.skipUnless(_HAS_NP, "нужны numpy/scipy (окружение воркера)")
class _ParseCase(unittest.TestCase):
    """Проверки через parse_chain: принят / отвергнут."""

    def setUp(self):
        import fx_engine
        self.fx = fx_engine

    def _ok(self, t, **kw):
        return self.fx.parse_chain([{"type": t, **_need(t), **kw}])[0]

    def _bad(self, t, **kw):
        with self.assertRaises(self.fx.ChainError, msg=f"{t} {kw} принят"):
            self.fx.parse_chain([{"type": t, **_need(t), **kw}])


@unittest.skipUnless(_HAS_NP, "нужны numpy/scipy (окружение воркера)")
class TestSpecFromBlocks(_ParseCase):
    """SPEC проверки цепочки построен из worker/fx_blocks.json."""

    def setUp(self):
        super().setUp()
        self.blocks = _blocks()

    def test_tc7_same_types_and_params(self):
        self.assertEqual(set(self.fx.SPEC), set(self.blocks))
        for t, blk in self.blocks.items():
            with self.subTest(type=t):
                self.assertEqual(set(self.fx.SPEC[t]), {p["id"] for p in blk.get("params", [])})
                self.assertEqual(set(self.fx.STR_SPEC.get(t, {})), {s["id"] for s in blk.get("strings", [])})

    def test_tc7_defaults_and_max_match(self):
        for t, blk in self.blocks.items():
            for p in blk["params"]:
                with self.subTest(type=t, param=p["id"]):
                    spec = self.fx.SPEC[t][p["id"]]
                    self.assertEqual(spec[0], p["default"])
                    self.assertEqual(spec[2], p["max"])
                    # умолчание дописывается parse_chain
                    self.assertEqual(self._ok(t)[p["id"]], p["default"])

    def test_tc7_bounds_enforced_as_in_file(self):
        for t, blk in self.blocks.items():
            for p in blk["params"]:
                k, lo, hi = p["id"], p["min"], p["max"]
                span = max(hi - lo, 1e-3)
                with self.subTest(type=t, param=k):
                    self.assertEqual(self._ok(t, **{k: lo})[k], lo)
                    self.assertEqual(self._ok(t, **{k: hi})[k], hi)
                    self._bad(t, **{k: hi + span * 0.01})
                    below = lo - span * 0.01
                    if p.get("zero_off"):
                        self.assertEqual(self._ok(t, **{k: 0})[k], 0)
                        if lo > 0:
                            self._bad(t, **{k: lo / 2})
                    else:
                        self._bad(t, **{k: below})

    def test_tc7_strings_as_in_file(self):
        for t, blk in self.blocks.items():
            for s in blk.get("strings", []):
                with self.subTest(type=t, string=s["id"]):
                    self.assertIn(s.get("asset"), ("amp", "ir", "kit"))
                    if s.get("required"):
                        self.assertIsNone(self.fx.STR_SPEC[t][s["id"]])
                        with self.assertRaises(self.fx.ChainError):
                            self.fx.parse_chain([{"type": t}])
                    else:
                        self.assertEqual(self.fx.STR_SPEC[t][s["id"]], s.get("default", ""))
                        self.assertEqual(self.fx.parse_chain([{"type": t}])[0][s["id"]], s.get("default", ""))

    def test_tc7_bands_as_in_file(self):
        want = {k: (v["default"], v["min"], v["max"]) for k, v in self.blocks["eq"]["bands"].items()}
        self.assertEqual(dict(self.fx.BAND_SPEC), want)


@unittest.skipUnless(_HAS_NP, "нужны numpy/scipy (окружение воркера)")
class TestStage2Unchanged(_ParseCase):
    """Перенос описания в JSON не меняет поведение проверки цепочки этапа 2."""

    def test_stage2_behaviour_unchanged(self):
        # поведение этапа 2: умолчания и допустимые значения те же
        for t, params in STAGE2.items():
            for k, (default, lo, hi) in params.items():
                with self.subTest(type=t, param=k):
                    self.assertEqual(self._ok(t)[k], default)
                    self.assertEqual(self._ok(t, **{k: lo})[k], lo)
                    self.assertEqual(self._ok(t, **{k: hi})[k], hi)
                    self._bad(t, **{k: hi + 1})
                    if (t, k) != ("eq", "lowpass_hz"):
                        self._bad(t, **{k: lo - 1})
        self.assertEqual(self._ok("eq", lowpass_hz=0)["lowpass_hz"], 0)
        self._bad("eq", lowpass_hz=500)
        self.assertIsNone(self.fx.STR_SPEC["amp"]["model"])
        self.assertEqual(self._ok("cab")["ir"], "")
        self.assertEqual(self._ok("reverb")["ir"], "")
        self.assertEqual(dict(self.fx.BAND_SPEC), {"freq_hz": (1000, 20, 20000), "gain_db": (0, -24, 24),
                                                    "q": (1, 0.1, 10)})


if __name__ == "__main__":
    unittest.main()

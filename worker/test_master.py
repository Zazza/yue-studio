"""Тесты карточки internal-own-track, этап 6 «Сведение и мастер»: блоки движка glue и
limiter (условия 41, 42; ТК69, ТК70, ТК78б), /fx на варианте и «на месте» (условие 43; ТК71,
ТК78в), поле master и place у записей пресетов звука (условие 45; ТК74).

Контракт (из карточки, без чтения реализации):
- glue — склейка шины: стерео-связанный детектор RMS (окно = attack), мягкое колено 6 дБ,
  threshold_db −40…0 (−18), ratio 1…10 (2), attack_ms 0,1…100 (10), release_ms 20…2000 (250),
  makeup_db 0…12 (0), mix 0…1 (1, параллельная компрессия); ниже порога (за коленом) и mix 0 —
  выход = вход; длина = входу;
- limiter — по истинному пику: ceiling_db −3…−0,1 (−1), release_ms 10…1000 (100), target_lufs
  −24…−6 или 0 = выкл; с target — усиление по LUFS всего входа (±12 дБ), ограничитель
  (истинный пик ×4, упреждение без сдвига), подстройка до |LUFS − target| ≤ 0,3; итог —
  true-peak ≤ ceiling + 0,1 (dsp.loudness); без target громкость не трогается; тишина — тишина;
- POST /jobs/{id}/fx: file — вход = вариант трека (имя по DSP_NAME_RE, файл в каталоге
  джобы; иначе 422/404; только source mix); in_place (только с file, без preview) —
  результат в тот же файл, метрики пересчитаны, к подписи « · мастер», если её нет, ответ —
  тот же вариант; превью с limiter считается по всему входу и режется окном; ключ кэша
  превью учитывает file и его версию;
- пресеты: master — цепочка движка ≤ 16 блоков, проверка как у /fx, пусто — нет; place у
  записи specs ({stems, place, db}) — pan −1…1, width 0…2; встроенные — master [] и
  target_lufs как раньше (transmission −13, sex-on-fire −12, live-rhythm null).

Допущение (карточка не уточняет): уровень тона «−6 дБFS» — по пику синуса (амплитуда 0,5),
что совпадает с RMS-уровнем по AES17 (синус полной шкалы = 0 дБFS).

Внешние границы подменяются как в test_fx_api (fx_resources — фейк). GPU и сеть не нужны.

Запуск: cd worker && python3 -m unittest test_master -v
"""
import copy
import os
import unittest

import test_fx_api as fa
import test_pure as tp

try:
    import numpy as np
    import scipy  # noqa: F401
    _HAS_NP = True
except ImportError:
    _HAS_NP = False

_SKIP_NP = "нужны numpy/scipy (окружение воркера)"
SR = 44100


def _fx():
    import fx_engine
    return fx_engine


def _loud(x, sr=SR):
    import dsp
    return dsp.loudness(x, sr)


def _rms_db(x):
    x = np.asarray(x, dtype=np.float64)
    return 20 * np.log10(max(float(np.sqrt(np.mean(x ** 2))), 1e-12))


def _sine(hz, dbfs, dur, sr=SR):
    """Синус уровня dbfs (по пику: амплитуда 10^(dbfs/20))."""
    t = np.arange(int(dur * sr)) / sr
    return 10 ** (dbfs / 20) * np.sin(2 * np.pi * hz * t)


def _stereo(left, right=None):
    return np.stack([left, left if right is None else right], axis=1).astype(np.float32)


def _music(dur=6.0, sr=SR, seed=1):
    """Синтетическая «музыка»: аккорд + шум + редкие удары; стерео, каналы различаются."""
    rng = np.random.default_rng(seed)
    n = int(dur * sr)
    t = np.arange(n) / sr
    body = sum(0.12 * np.sin(2 * np.pi * f * t + p) for f, p in ((110, 0), (220, 1), (330, 2), (440, 3)))
    noise = 0.05 * rng.standard_normal(n)
    hits = np.zeros(n)
    for at in np.arange(0.25, dur, 0.5):
        a = int(at * sr)
        k = np.arange(min(2000, n - a))
        hits[a:a + len(k)] += 0.8 * np.exp(-k / 300) * np.sin(2 * np.pi * 90 * k / sr)
    left = body + noise + hits
    r = 0.9 * body + 0.05 * rng.standard_normal(n) + hits
    return np.stack([left, r], axis=1)


def _peaky(dur=8.0, lufs=-20.0, every=0.5, seed=5):
    """ТК78б: плотное тело (шум) с громкостью lufs и частыми короткими пиками +6 дБFS
    (пара отсчётов ±2 каждые every с). Пики почти не добавляют громкости, но ограничитель
    держит их весь release — один проход после усиления заметно теряет LUFS."""
    rng = np.random.default_rng(seed)
    n = int(dur * SR)
    body = rng.standard_normal((n, 2))
    spikes = np.zeros(n)
    for a in range(int(0.01 * SR), n - 2, int(every * SR)):
        spikes[a], spikes[a + 1] = 1.0, -1.0

    def mk(g):
        x = body * g
        x += 2.0 * spikes[:, None]
        return x
    g = 0.05
    for _ in range(8):
        g *= 10 ** ((lufs - _loud(mk(g))["lufs"]) / 20)
    return mk(g)


def _scale_peak_db(x, peak_db):
    return x * (10 ** (peak_db / 20) / np.max(np.abs(x)))


def _scale_lufs(x, lufs):
    for _ in range(3):
        x = x * 10 ** ((lufs - _loud(x)["lufs"]) / 20)
    return x


# ---------- ТК69: glue ----------

@unittest.skipUnless(_HAS_NP, _SKIP_NP)
class TestGlue(unittest.TestCase):

    def _run(self, x, **p):
        return _fx().process(x, SR, [dict(type="glue", **p)])

    def test_defaults_and_limits(self):
        fx = _fx()
        b = fx.parse_chain([{"type": "glue"}])[0]
        want = {"threshold_db": -18, "ratio": 2, "attack_ms": 10, "release_ms": 250, "makeup_db": 0, "mix": 1}
        for k, v in want.items():
            self.assertAlmostEqual(float(b[k]), v, msg=k)
        for k, v in (("threshold_db", -41), ("threshold_db", 1), ("ratio", 0.5), ("ratio", 11),
                     ("attack_ms", 0.05), ("attack_ms", 101), ("release_ms", 10), ("release_ms", 2001),
                     ("makeup_db", -1), ("makeup_db", 13), ("mix", -0.1), ("mix", 1.1)):
            with self.subTest(k=k, v=v), self.assertRaises(fx.ChainError):
                fx.parse_chain([{"type": "glue", k: v}])

    def test_tc69_reduction_above_threshold(self):
        # тон −6 дБFS, порог −18, ratio 2, колено далеко позади: (−6 − (−18))·(1 − 1/2) = 6 дБ
        x = _stereo(_sine(1000, -6, 3.0))
        y = self._run(x, threshold_db=-18, ratio=2)
        a, b = int(1.0 * SR), int(2.5 * SR)
        red = _rms_db(x[a:b]) - _rms_db(y[a:b])
        self.assertAlmostEqual(red, 6.0, delta=0.5)

    def test_tc69_below_threshold_unchanged(self):
        x = _stereo(_sine(1000, -30, 3.0))
        y = self._run(x, threshold_db=-18, ratio=2)
        a, b = int(1.0 * SR), int(2.5 * SR)
        self.assertAlmostEqual(_rms_db(y[a:b]) - _rms_db(x[a:b]), 0.0, delta=0.1)

    def test_tc69_mix0_is_input(self):
        x = _stereo(_sine(1000, -6, 2.0))
        y = self._run(x, threshold_db=-30, ratio=8, mix=0)
        np.testing.assert_allclose(y, x, atol=1e-6)

    def test_tc69_stereo_link(self):
        # громкий только L: R (тихий тон другой частоты) приглушается так же
        left, right = _sine(1000, -6, 3.0), _sine(700, -30, 3.0)
        x = _stereo(left, right)
        y = self._run(x, threshold_db=-18, ratio=2)
        a, b = int(1.0 * SR), int(2.5 * SR)
        red_l = _rms_db(x[a:b, 0]) - _rms_db(y[a:b, 0])
        red_r = _rms_db(x[a:b, 1]) - _rms_db(y[a:b, 1])
        self.assertGreater(red_l, 3.0, "громкий канал не сжат")
        self.assertAlmostEqual(red_r, red_l, delta=0.5)

    def test_tc69_length_same(self):
        for x in (_stereo(_sine(1000, -6, 1.234)), _sine(1000, -6, 0.777).astype(np.float32)):
            with self.subTest(shape=x.shape):
                y = self._run(x)
                self.assertEqual(y.shape, x.shape)

    def test_makeup_adds_gain(self):
        x = _stereo(_sine(1000, -30, 2.0))   # ниже порога: только makeup
        y = self._run(x, makeup_db=6)
        a, b = int(0.5 * SR), int(1.5 * SR)
        self.assertAlmostEqual(_rms_db(y[a:b]) - _rms_db(x[a:b]), 6.0, delta=0.2)


# ---------- ТК70: limiter ----------

@unittest.skipUnless(_HAS_NP, _SKIP_NP)
class TestLimiter(unittest.TestCase):

    def _run(self, x, **p):
        return _fx().process(x.astype(np.float32), SR, [dict(type="limiter", **p)])

    def test_defaults_and_limits(self):
        fx = _fx()
        b = fx.parse_chain([{"type": "limiter"}])[0]
        self.assertAlmostEqual(float(b["ceiling_db"]), -1)
        self.assertAlmostEqual(float(b["release_ms"]), 100)
        self.assertAlmostEqual(float(b["target_lufs"]), 0)
        for v in (-24, -6, 0, -14.5):
            fx.parse_chain([{"type": "limiter", "target_lufs": v}])
        for k, v in (("ceiling_db", -3.5), ("ceiling_db", -0.05), ("ceiling_db", 0),
                     ("release_ms", 5), ("release_ms", 1001),
                     ("target_lufs", -30), ("target_lufs", -5), ("target_lufs", -25)):
            with self.subTest(k=k, v=v), self.assertRaises(fx.ChainError):
                fx.parse_chain([{"type": "limiter", k: v}])

    def test_tc70_target_minus30_chain_error(self):
        with self.assertRaises(_fx().ChainError):
            _fx().process(np.zeros((1000, 2), np.float32), SR, [{"type": "limiter", "target_lufs": -30}])

    def test_tc70_true_peak_below_ceiling_no_shift(self):
        x = _scale_peak_db(_music(), 3.0)            # пики +3 дБFS
        y = self._run(x, ceiling_db=-1)
        self.assertEqual(y.shape, x.shape)
        self.assertLessEqual(_loud(y)["true_peak_db"], -0.9)
        # сдвиг 0: корреляция максимальна на нулевом лаге
        a, b = x.mean(axis=1), y.astype(np.float64).mean(axis=1)
        lags = range(-200, 201)
        n = len(a)
        corr = [float(np.dot(a[max(0, -k):n - max(0, k)], b[max(0, k):n - max(0, -k)])) for k in lags]
        self.assertEqual(list(lags)[int(np.argmax(corr))], 0)

    def test_tc70_target_lufs(self):
        x = _scale_lufs(_music(), -20.0)
        self.assertAlmostEqual(_loud(x)["lufs"], -20.0, delta=0.15)
        y = self._run(x, target_lufs=-14)
        m = _loud(y)
        self.assertAlmostEqual(m["lufs"], -14.0, delta=0.3)
        self.assertLessEqual(m["true_peak_db"], -0.9)

    def test_tc78b_target_reached_when_one_pass_falls_short(self):
        # частые пики +6 дБFS над плотным телом, LUFS входа −20, цель −10: усиление +10 дБ
        # загоняет пики на +16, ограничитель снимает громкость — нужна подстройка (повтор прохода)
        x = _peaky()
        self.assertAlmostEqual(_loud(x)["lufs"], -20.0, delta=0.3)
        self.assertGreaterEqual(20 * np.log10(np.max(np.abs(x))), 5.9, "пики входа ниже +6 дБFS")
        y = self._run(x, target_lufs=-10)
        self.assertEqual(y.shape, x.shape)
        m = _loud(y)
        self.assertAlmostEqual(m["lufs"], -10.0, delta=0.3)
        self.assertLessEqual(m["true_peak_db"], -0.9)

    def test_tc70_no_target_quiet_signal_unchanged(self):
        x = _scale_peak_db(_music(), -8.0)           # истинный пик заведомо ниже −1
        self.assertLess(_loud(x)["true_peak_db"], -2)
        y = self._run(x, target_lufs=0)
        np.testing.assert_allclose(y, x.astype(np.float32), atol=1e-6)

    def test_tc70_silence_stays_silence(self):
        x = np.zeros((SR * 2, 2), np.float32)
        for p in ({}, {"target_lufs": -14}):
            with self.subTest(**p):
                y = self._run(x, **p)
                self.assertEqual(y.shape, x.shape)
                self.assertTrue(np.all(np.isfinite(y)))
                self.assertEqual(float(np.max(np.abs(y))), 0.0)


# ---------- ТК71: /fx на варианте и «на месте» ----------

VAR = "overdub-inst-0.flac"
VAR_HZ = 5000


@unittest.skipUnless(fa._OK, fa._SKIP)
class TestFxFileInPlace(fa._FxApiCase):

    def _variant(self, d, jid, name=VAR, hz=VAR_HZ, amp=fa.AMP, label="Микс студии"):
        """Вариант трека через POST /jobs/{id}/dsp (как его загружает приложение)."""
        import io
        import soundfile as sf
        buf = io.BytesIO()
        sf.write(buf, fa._tone(hz, amp=amp), fa.SR, format="FLAC")
        r = self.client.post(f"/jobs/{jid}/dsp", params={"label": label}, content=buf.getvalue(),
                             headers={"x-filename": name})
        self.assertEqual(r.status_code, 200, r.text)
        return d / name

    def _listed(self, jid):
        return {v["file"]: v for v in self.client.get(f"/jobs/{jid}/dsp").json()}

    def test_tc71_file_processes_variant_not_track(self):
        jid, d = self._audio_job()
        self._variant(d, jid)
        out = self._ok(jid, source="mix", file=VAR, chain=fa.IDENTITY)
        self.assertNotEqual(out["file"], VAR)
        y, _ = fa._read(d / out["file"])
        self.assertAlmostEqual(fa._db(fa._amp(y, VAR_HZ) / fa.AMP), 0, delta=0.5)
        for k, hz in fa.HZ.items():
            self.assertLess(fa._db(fa._amp(y, hz) / fa.AMP), -40, f"в результате звук трека ({k})")

    def test_file_and_track_same_chain_different_variants(self):
        # дубль: та же цепочка на треке и на варианте — разные входы, один файл перезаписал бы другой
        jid, d = self._audio_job()
        self._variant(d, jid)
        a = self._ok(jid, source="mix", chain=fa.IDENTITY)["file"]
        b = self._ok(jid, source="mix", file=VAR, chain=fa.IDENTITY)["file"]
        self.assertNotEqual(a, b, "результат по варианту перезаписал результат по треку")
        self.assertTrue((d / a).is_file() and (d / b).is_file())

    def test_tc71_bad_names_422(self):
        jid, d = self._audio_job()
        self._variant(d, jid)
        for bad in ("../" + VAR, "../1/" + VAR, "audio.flac", "stem-vocals.wav", "x/" + VAR):
            with self.subTest(file=bad):
                r = self._fx(jid, source="mix", file=bad, chain=fa.IDENTITY)
                self.assertEqual(r.status_code, 422, r.text)

    def test_tc71_missing_file_404(self):
        jid, _ = self._audio_job()
        r = self._fx(jid, source="mix", file="dsp-fx-mix-00000000.flac", chain=fa.IDENTITY)
        self.assertEqual(r.status_code, 404, r.text)

    def test_tc71_file_with_stem_source_422(self):
        jid, d = self._audio_job()
        self._variant(d, jid)
        r = self._fx(jid, source="vocals", file=VAR, chain=fa.IDENTITY)
        self.assertEqual(r.status_code, 422, r.text)

    def test_tc71_in_place_same_file_metrics_label(self):
        jid, d = self._audio_job()
        path = self._variant(d, jid)
        before = self._listed(jid)[VAR]
        x0, _ = fa._read(path)
        files_before = sorted(p.name for p in d.glob("*.flac"))
        out = self._ok(jid, source="mix", file=VAR, in_place=True,
                       chain=[{"type": "gain", "gain_db": -6}])
        self.assertEqual(out["file"], VAR)
        self.assertEqual(sorted(p.name for p in d.glob("*.flac")), files_before, "появился новый файл")
        x1, _ = fa._read(path)
        self.assertEqual(x1.shape, x0.shape)
        self.assertAlmostEqual(fa._db(fa._amp(x1, VAR_HZ) / fa._amp(x0, VAR_HZ)), -6, delta=0.2)
        after = self._listed(jid)[VAR]
        self.assertAlmostEqual(after["metrics"]["lufs"] - before["metrics"]["lufs"], -6, delta=0.5,
                               msg="метрики не пересчитаны")
        self.assertEqual(out["metrics"]["lufs"], after["metrics"]["lufs"])
        self.assertEqual(after["label"], "Микс студии · мастер")
        self.assertEqual(out.get("label"), "Микс студии · мастер")
        # повтор — « · мастер» не дублируется
        self._ok(jid, source="mix", file=VAR, in_place=True, chain=[{"type": "gain", "gain_db": 0}])
        self.assertEqual(self._listed(jid)[VAR]["label"], "Микс студии · мастер")

    def test_tc71_in_place_without_file_422(self):
        jid, d = self._audio_job()
        track0 = (d / "audio.flac").read_bytes()
        r = self._fx(jid, source="mix", in_place=True, chain=fa.IDENTITY)
        self.assertEqual(r.status_code, 422, r.text)
        self.assertEqual((d / "audio.flac").read_bytes(), track0, "звук трека перезаписан")

    def test_tc71_in_place_with_preview_422(self):
        jid, d = self._audio_job()
        path = self._variant(d, jid)
        data0 = path.read_bytes()
        r = self._fx(jid, source="mix", file=VAR, in_place=True, preview=True,
                     chain=fa.IDENTITY, **{"from": 1.0, "to": 2.0})
        self.assertEqual(r.status_code, 422, r.text)
        self.assertEqual(path.read_bytes(), data0)


@unittest.skipUnless(fa._OK, fa._SKIP)
class TestLimiterPreview(fa._FxApiCase):
    """Превью с limiter: считается по всему входу, режется окном."""

    LONG = 30.0

    def _write_job(self, x):
        import soundfile as sf
        jid = self._job(duration=len(x) / SR, semantic=False)
        d = self.jobs_dir / str(jid)
        d.mkdir(parents=True, exist_ok=True)
        sf.write(str(d / "audio.flac"), x, SR, subtype="PCM_24")
        with self._conn() as c:
            c.execute("UPDATE jobs SET audio_file='audio.flac' WHERE id=?", (jid,))
        return jid, d

    def _long_job(self):
        """Трек 30 с: первые 10 с тихо, дальше громко — LUFS окна ≠ LUFS всего трека."""
        x = _music(self.LONG, SR, seed=3)
        x[:int(10 * SR)] *= 0.05
        return self._write_job(_scale_peak_db(x, -3.0))

    def _check_preview_matches(self, jid, d, chain, fr, to):
        full = self._ok(jid, source="mix", chain=chain)
        prev = self._ok(jid, source="mix", chain=chain, preview=True, **{"from": fr, "to": to})
        y_full, _ = fa._read(d / full["file"])
        y_prev, _ = fa._read(d / prev["file"])
        seg = y_full[int(fr * SR):int(to * SR)]
        n = min(len(seg), len(y_prev))
        self.assertGreater(n, int((to - fr - 0.1) * SR))
        self.assertAlmostEqual(_rms_db(y_prev[:n]), _rms_db(seg[:n]), delta=0.5)

    def test_tc71_preview_limiter_matches_full_render(self):
        jid, d = self._long_job()
        self._check_preview_matches(jid, d, [{"type": "limiter", "target_lufs": -14}], 10.0, 20.0)

    def test_tc78c_preview_quiet_part_matches_full_render(self):
        # тихие 20 с (−40 LUFS) + громкие 20 с (−12 LUFS), окно [2, 12) — тихая часть: по всему
        # треку усиление ≈ −2 дБ, окно отдельно подняло бы тихую часть на +12 дБ (зажим)
        quiet = _scale_lufs(_music(20.0, SR, seed=5), -40.0)
        loud = _scale_lufs(_music(20.0, SR, seed=4), -12.0)
        self.assertLess(np.max(np.abs(loud)), 0.99, "громкая часть клиппует в PCM_24")
        jid, d = self._write_job(np.concatenate([quiet, loud]))
        self._check_preview_matches(jid, d, [{"type": "limiter", "target_lufs": -14}], 2.0, 12.0)

    def test_preview_cache_depends_on_file_and_version(self):
        import soundfile as sf
        jid, d = self._long_job()
        sf.write(str(d / VAR), fa._tone(VAR_HZ, amp=0.1, dur=self.LONG), SR)
        chain = [{"type": "limiter", "target_lufs": -14}]
        win = {"from": 10.0, "to": 12.0}
        a = self._ok(jid, source="mix", chain=chain, preview=True, **win)["file"]
        b = self._ok(jid, source="mix", file=VAR, chain=chain, preview=True, **win)["file"]
        self.assertNotEqual(a, b, "превью трека и варианта — один файл кэша")
        yb, _ = fa._read(d / b)
        self.assertGreater(fa._amp(yb, VAR_HZ, 0.2, 1.8), 0.01, "превью варианта — не из варианта")
        # вариант перезаписан (другой тон) — превью пересчитано
        sf.write(str(d / VAR), fa._tone(2000, amp=0.1, dur=self.LONG), SR)
        st = (d / VAR).stat()
        os.utime(d / VAR, ns=(st.st_atime_ns, st.st_mtime_ns + 10 * 10**9))
        c = self._ok(jid, source="mix", file=VAR, chain=chain, preview=True, **win)["file"]
        yc, _ = fa._read(d / c)
        self.assertGreater(fa._amp(yc, 2000, 0.2, 1.8), 0.01, "превью взято из кэша старой версии")
        self.assertLess(fa._amp(yc, VAR_HZ, 0.2, 1.8), 0.001)


# ---------- ТК74: master и place у пресетов звука ----------

ENGINE = [{"type": "eq", "highpass_hz": 80}]
FINAL = [{"chain": "width", "params": {"width": 1.1, "bass": 120}}]
MASTER = [{"type": "glue", "threshold_db": -20, "ratio": 2}, {"type": "limiter", "ceiling_db": -1}]


def _preset(**over):
    body = {"name": "Мастер", "note": "", "specs": [{"stems": ["bass"], "engine": copy.deepcopy(ENGINE), "db": 0}],
            "final": copy.deepcopy(FINAL), "reference_job_id": None, "target_lufs": None}
    body.update(over)
    return body


@unittest.skipUnless(tp._HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestPresetMasterPlace(tp._WorkerApiCase):

    def _list(self):
        r = self.client.get("/sound-presets")
        self.assertEqual(r.status_code, 200, r.text)
        return r.json()

    def _by_id(self, pid):
        return next((p for p in self._list() if p.get("id") == pid), None)

    def _create(self, **over):
        r = self.client.post("/sound-presets", json=_preset(**over))
        self.assertEqual(r.status_code, 200, r.text)
        return r.json()["id"]

    def _bad(self, **over):
        n = len(self._list())
        r = self.client.post("/sound-presets", json=_preset(**over))
        self.assertEqual(r.status_code, 422, r.text)
        self.assertTrue(r.json().get("detail"), r.text)
        self.assertEqual(len(self._list()), n, "невалидный пресет сохранился")

    def test_tc74_master_saved_and_returned(self):
        pid = self._create(master=copy.deepcopy(MASTER))
        got = self._by_id(pid)
        self.assertEqual(got.get("master"), MASTER)

    def test_master_absent_is_empty(self):
        pid = self._create()
        self.assertEqual(self._by_id(pid).get("master"), [])

    def test_master_put_replaces(self):
        pid = self._create(master=copy.deepcopy(MASTER))
        r = self.client.put(f"/sound-presets/{pid}", json=_preset(master=[{"type": "limiter"}]))
        self.assertLess(r.status_code, 300, r.text)
        self.assertEqual(self._by_id(pid)["master"], [{"type": "limiter"}])

    def test_tc74_master_unknown_block_422(self):
        self._bad(master=[{"type": "nope"}])

    def test_tc74_master_17_blocks_422(self):
        self._bad(master=[{"type": "gain", "gain_db": 0}] * 17)

    def test_master_bad_param_422(self):
        self._bad(master=[{"type": "limiter", "target_lufs": -30}])

    def test_master_16_blocks_ok(self):
        pid = self._create(master=[{"type": "gain", "gain_db": 0}] * 16)
        self.assertEqual(len(self._by_id(pid)["master"]), 16)

    def test_place_record_saved(self):
        spec = {"stems": ["other"], "place": {"pan": 0.3, "width": 1.4}, "db": 0}
        pid = self._create(specs=[spec])
        got = self._by_id(pid)["specs"][0]
        self.assertEqual(got["stems"], ["other"])
        self.assertEqual(got["place"], {"pan": 0.3, "width": 1.4})

    def test_tc74_place_out_of_range_422(self):
        for place in ({"pan": 2, "width": 1}, {"pan": 0, "width": 3}, {"pan": -1.5, "width": 1},
                      {"pan": 0, "width": -0.1}, {"pan": "0", "width": 1}):
            with self.subTest(place=place):
                self._bad(specs=[{"stems": ["other"], "place": place, "db": 0}])

    def test_tc74_builtins_master_empty_target_unchanged(self):
        want = {"transmission": -13.0, "sex-on-fire": -12.0, "live-rhythm": None}
        got = {p["slug"]: p for p in self._list() if p.get("builtin")}
        for slug, target in want.items():
            with self.subTest(slug=slug):
                self.assertIn(slug, got)
                self.assertEqual(got[slug].get("master"), [])
                self.assertEqual(got[slug].get("target_lufs"), target)


@unittest.skipUnless(tp._HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestPresetMasterLimiterRoom(tp._WorkerApiCase):
    """ТК79г / усл. 45б: при target_lufs и мастере без limiter limiter допишется 17-м блоком —
    16 блоков без limiter → 422; 15 → 200; 16 с limiter (дописывать нечего) → 200."""

    # помощники — те же, что у TestPresetMasterPlace (без повторного прогона его тестов)
    _list = TestPresetMasterPlace._list
    _by_id = TestPresetMasterPlace._by_id
    _create = TestPresetMasterPlace._create
    _bad = TestPresetMasterPlace._bad

    GAIN = {"type": "gain", "gain_db": 0}

    def test_tc79g_target_and_16_blocks_without_limiter_422(self):
        self._bad(target_lufs=-14, master=[dict(self.GAIN) for _ in range(16)])

    def test_tc79g_target_and_15_blocks_ok(self):
        pid = self._create(target_lufs=-14, master=[dict(self.GAIN) for _ in range(15)])
        self.assertEqual(len(self._by_id(pid)["master"]), 15)

    def test_tc79g_target_and_16_blocks_with_limiter_ok(self):
        master = [dict(self.GAIN) for _ in range(15)] + [{"type": "limiter"}]
        pid = self._create(target_lufs=-14, master=master)
        self.assertEqual(len(self._by_id(pid)["master"]), 16)


if __name__ == "__main__":
    unittest.main()

"""Тесты карточки «дорожки гитары и клавиш» (воркер).

Стемы трека — 4 основных (drums, bass, other, vocals); дополнительно при
разделении кладутся две «подробные» дорожки из 6-стемной модели:
stem-guitar.flac и stem-piano.flac. Они уточняют «прочее» и в сумму трека
не входят.

Запуск: cd worker && python3 -m unittest test_detail_stems -v
Без зависимостей воркера (fastapi/httpx/numpy/soundfile, для stems.separate —
ещё torch) классы пропускаются, как соседние в test_pure.py.
"""
import sys
import unittest
from pathlib import Path
from unittest import mock

import test_pure as tp

try:
    import soundfile  # noqa: F401
    _HAS_SF = True
except ImportError:
    _HAS_SF = False

try:
    import torch  # noqa: F401
    _HAS_TORCH = True
except ImportError:
    _HAS_TORCH = False

_WORKER_OK = tp._HAS_WORKER_DEPS and _HAS_SF
_WORKER_SKIP = "нужны fastapi/httpx/numpy/soundfile (окружение воркера)"

MAIN = ("drums", "bass", "other", "vocals")
DETAIL = ("guitar", "piano")
SR = 44100


def _tone(hz, amp=0.1, dur=2.0, sr=SR):
    import numpy as np
    t = np.arange(int(dur * sr)) / sr
    x = (amp * np.sin(2 * np.pi * hz * t)).astype(np.float32)
    return np.stack([x, x], axis=1)  # стерео, как у demucs


def _tone_amp(x, hz, sr=SR):
    """Амплитуда синуса частоты hz (одна точка ДПФ по целым секундам)."""
    import numpy as np
    if x.ndim > 1:
        x = x.mean(axis=1)
    n = (len(x) // sr) * sr
    x = x[:n].astype(np.float64)
    t = np.arange(n) / sr
    return 2 * abs(np.sum(x * np.exp(-2j * np.pi * hz * t))) / n


@unittest.skipUnless(_WORKER_OK, _WORKER_SKIP)
class TestMinusIgnoresDetailStems(tp._WorkerApiCase):
    """W1: POST /jobs/{id}/minus при всех шести stem-*.flac и exclude=["vocals"]:
    minus.flac = drums+bass+other (guitar/piano не добавляются — иначе гитара
    удвоится), kept — только основные имена."""

    # у каждого стема свой тон: по спектру минуса видно, какие дорожки в нём
    HZ = {"drums": 110, "bass": 220, "other": 330, "vocals": 440, "guitar": 550, "piano": 660}

    def _six_stems_job(self):
        import numpy as np
        import soundfile as sf
        jid = self._job(duration=2.0, semantic=False)
        d = self.jobs_dir / str(jid)
        d.mkdir(parents=True, exist_ok=True)
        for name, hz in self.HZ.items():
            sf.write(str(d / f"stem-{name}.flac"), _tone(hz), SR)
        mix = np.sum([_tone(self.HZ[n]) for n in MAIN], axis=0)
        sf.write(str(d / "audio.flac"), mix, SR)
        with self._conn() as c:
            c.execute("UPDATE jobs SET audio_file='audio.flac' WHERE id=?", (jid,))
        return jid, d

    def test_minus_is_sum_of_main_stems_only(self):
        import soundfile as sf
        jid, d = self._six_stems_job()
        r = self.client.post(f"/jobs/{jid}/minus", json={"exclude": ["vocals"]})
        self.assertLess(r.status_code, 300, r.text)
        self.assertTrue((d / "minus.flac").exists(), f"нет minus.flac: {sorted(p.name for p in d.iterdir())}")
        x, sr = sf.read(str(d / "minus.flac"), dtype="float32")
        ref = _tone_amp(x, self.HZ["drums"], sr)
        self.assertGreater(ref, 0.01, "в минусе нет барабанов")
        for name in ("bass", "other"):
            a = _tone_amp(x, self.HZ[name], sr)
            self.assertAlmostEqual(a / ref, 1.0, delta=0.05,
                                   msg=f"{name}: {a:.4f} vs drums {ref:.4f} — основная дорожка не на своём уровне")
        for name in ("vocals", "guitar", "piano"):
            a = _tone_amp(x, self.HZ[name], sr)
            self.assertLess(a / ref, 0.02, f"{name} попал в минус ({a:.4f} при drums {ref:.4f})")

    def test_kept_lists_main_names_only(self):
        jid, _ = self._six_stems_job()
        r = self.client.post(f"/jobs/{jid}/minus", json={"exclude": ["vocals"]})
        self.assertLess(r.status_code, 300, r.text)
        kept = r.json().get("kept")
        self.assertIsNotNone(kept, r.text)
        self.assertEqual(sorted(kept), ["bass", "drums", "other"])


@unittest.skipUnless(_WORKER_OK, _WORKER_SKIP)
class TestTonesDetailStems(tp._WorkerApiCase):
    """W2: GET /jobs/{id}/tones?stem=guitar|piano без файла → 409 с подсказкой
    make_stems (как у основных); неизвестный stem → 422."""

    def _audio_job(self):
        jid = self._job(duration=60.0, semantic=False)
        d = self.jobs_dir / str(jid)
        d.mkdir(parents=True, exist_ok=True)
        (d / "audio.flac").write_bytes(b"x")
        with self._conn() as c:
            c.execute("UPDATE jobs SET audio_file='audio.flac' WHERE id=?", (jid,))
        return jid

    def test_detail_stem_without_file_409_make_stems(self):
        jid = self._audio_job()
        for stem in DETAIL:
            with self.subTest(stem=stem):
                r = self.client.get(f"/jobs/{jid}/tones", params={"stem": stem})
                self.assertEqual(r.status_code, 409, r.text)
                self.assertIn("make_stems", r.json().get("detail", ""))

    def test_detail_stem_missing_even_if_main_stems_present(self):
        # основные стемы есть, подробной дорожки нет → всё равно 409
        jid = self._audio_job()
        d = self.jobs_dir / str(jid)
        for name in MAIN:
            (d / f"stem-{name}.flac").write_bytes(b"x")
        r = self.client.get(f"/jobs/{jid}/tones", params={"stem": "piano"})
        self.assertEqual(r.status_code, 409, r.text)
        self.assertIn("make_stems", r.json().get("detail", ""))

    def test_unknown_stem_422(self):
        jid = self._audio_job()
        for stem in ("flute", "../x", "GUITAR", "guitar/../x"):
            with self.subTest(stem=stem):
                r = self.client.get(f"/jobs/{jid}/tones", params={"stem": stem})
                self.assertEqual(r.status_code, 422, f"{stem}: {r.text}")


# --- W3: stems.separate ------------------------------------------------------

# Фейковые модели: источник = вход × k. У 4-стемной все k > 0, у 6-стемной
# основные k < 0 — по знаку корреляции с входом видно, из какой модели взят файл.
_K4 = {"drums": 0.5, "bass": 0.4, "other": 0.3, "vocals": 0.2}
_K6 = {"drums": -0.5, "bass": -0.4, "other": -0.3, "vocals": -0.2, "guitar": 0.6, "piano": 0.45}
_SOURCES = {"htdemucs": list(MAIN), "htdemucs_6s": list(MAIN) + list(DETAIL)}


def _fake_model(name):
    m = mock.MagicMock(name=f"model-{name}")
    m.sources = list(_SOURCES[name])
    m.samplerate = SR
    m.audio_channels = 2
    for meth in ("to", "eval", "cpu", "cuda", "float"):
        getattr(m, meth).return_value = m
    m._fake_name = name
    return m


def _fake_get_model(fail_6s=False):
    def get_model(*args, **kw):
        name = kw.get("name", args[0] if args else None)
        if name not in _SOURCES:
            raise AssertionError(f"get_model({name!r}) — ждали htdemucs или htdemucs_6s")
        if fail_6s and name == "htdemucs_6s":
            raise RuntimeError("download failed: htdemucs_6s")
        return _fake_model(name)
    return get_model


def _fake_apply_model(model, mix, *args, **kw):
    import torch
    ks = _K4 if model._fake_name == "htdemucs" else _K6
    # mix: (batch, channels, time) → (batch, sources, channels, time)
    return torch.stack([mix * ks[s] for s in model.sources], dim=1)


@unittest.skipUnless(_HAS_TORCH and tp._HAS_NUMPY and _HAS_SF, "нужны torch/numpy/soundfile")
class TestSeparateDetailStems(unittest.TestCase):
    """W3: stems.separate(audio_path, out_dir) пишет 4 основных стема из
    htdemucs и stem-guitar/stem-piano из htdemucs_6s; сбой загрузки 6-стемной
    модели не мешает основным и не вылетает наружу. demucs подменяется на
    внешней границе (get_model/apply_model), GPU не нужен."""

    def setUp(self):
        import tempfile
        import soundfile as sf
        self._td = tempfile.TemporaryDirectory()
        self.addCleanup(self._td.cleanup)
        tmp = Path(self._td.name)
        self.audio = tmp / "audio.flac"
        sf.write(str(self.audio), _tone(440, amp=0.5, dur=3.0), SR)
        self.out = tmp / "out"
        self.out.mkdir()
        p = mock.patch("torch.cuda.is_available", return_value=False)
        p.start()
        self.addCleanup(p.stop)

    def _patch_demucs(self, fail_6s=False):
        """Подменить demucs.pretrained.get_model и demucs.apply.apply_model.
        Если demucs не установлен — подставить модули-заглушки в sys.modules."""
        get_model = _fake_get_model(fail_6s)
        try:
            import demucs.apply  # noqa: F401
            import demucs.pretrained  # noqa: F401
        except ImportError:
            import types
            pkg = types.ModuleType("demucs")
            pre = types.ModuleType("demucs.pretrained")
            app = types.ModuleType("demucs.apply")
            pre.get_model = get_model
            app.apply_model = _fake_apply_model
            pkg.pretrained, pkg.apply = pre, app
            p = mock.patch.dict(sys.modules, {"demucs": pkg, "demucs.pretrained": pre, "demucs.apply": app})
            p.start()
            self.addCleanup(p.stop)
            return
        for target, fake in (("demucs.pretrained.get_model", get_model),
                             ("demucs.apply.apply_model", _fake_apply_model)):
            p = mock.patch(target, side_effect=fake)
            p.start()
            self.addCleanup(p.stop)

    def _separate(self):
        import stems
        return stems.separate(str(self.audio), str(self.out))

    def _corr(self, name):
        """Знак и величина связи файла стема с входом: >0 — источник с k>0."""
        import numpy as np
        import soundfile as sf
        x, _ = sf.read(str(self.out / f"stem-{name}.flac"), dtype="float32")
        ref, _ = sf.read(str(self.audio), dtype="float32")
        x = x.mean(axis=1) if x.ndim > 1 else x
        ref = ref.mean(axis=1) if ref.ndim > 1 else ref
        n = min(len(x), len(ref))
        return float(np.dot(x[:n], ref[:n]) / np.dot(ref[:n], ref[:n]))

    @staticmethod
    def _names(res):
        return {Path(s).name for s in res.get("stems", [])}

    def test_writes_six_files_and_reports_them(self):
        self._patch_demucs()
        res = self._separate()
        want = {f"stem-{n}.flac" for n in MAIN + DETAIL}
        for f in want:
            self.assertTrue((self.out / f).exists(), f"нет {f}: {sorted(p.name for p in self.out.iterdir())}")
        self.assertEqual(self._names(res), want)

    def test_main_stems_from_4_stem_model(self):
        self._patch_demucs()
        self._separate()
        for name in MAIN:
            with self.subTest(stem=name):
                c = self._corr(name)
                self.assertGreater(c, 0.05, f"stem-{name}.flac (k={c:+.3f}) взят не из htdemucs")

    def test_drums_value_matches_4_stem_model(self):
        # нормализация входа (вычесть среднее/разделить на σ и вернуть) не меняет
        # коэффициент: синус без постоянной составляющей
        self._patch_demucs()
        self._separate()
        self.assertAlmostEqual(self._corr("drums"), _K4["drums"], delta=0.05)

    def test_detail_stems_written_and_nonsilent(self):
        self._patch_demucs()
        self._separate()
        for name in DETAIL:
            with self.subTest(stem=name):
                self.assertGreater(self._corr(name), 0.05, f"stem-{name}.flac пустой или не из htdemucs_6s")

    def test_6s_load_failure_keeps_main_stems(self):
        self._patch_demucs(fail_6s=True)
        res = self._separate()  # исключение наружу не выходит
        names = self._names(res)
        for name in MAIN:
            self.assertTrue((self.out / f"stem-{name}.flac").exists(), f"нет stem-{name}.flac")
            self.assertIn(f"stem-{name}.flac", names)
        for name in DETAIL:
            self.assertNotIn(f"stem-{name}.flac", names)
            self.assertFalse((self.out / f"stem-{name}.flac").exists(),
                             f"stem-{name}.flac записан, хотя 6-стемная модель не загрузилась")
        self.assertGreater(self._corr("drums"), 0.05)


if __name__ == "__main__":
    unittest.main()

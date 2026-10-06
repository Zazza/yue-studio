"""Тесты карточки internal-roformer-stems: разделение BS-Roformer-SW вместо
htdemucs + барабаны по частям (DrumSep).

Контракт (из карточки):
- основные stem-{drums,bass,other,vocals}.flac в сумме дают трек;
  stem-other = other + guitar + piano модели;
- stem-guitar/stem-piano — подробные внутри «прочего», в сумму не входят;
- stem-{kick,snare,toms,hh,ride,crash}.flac — части внутри «барабанов»,
  в сумму не входят, минус их не видит;
- модель выбирается: по умолчанию htdemucs; RoFormer — если выбран
  (stems.separate(..., prefer="roformer"), настройка воркера stems_model,
  разово POST /jobs/{id}/stems?model=roformer|htdemucs);
- RoFormer выбран, но окружения нет / он упал / YUE_STEMS_MODEL=demucs →
  demucs, model = htdemucs, частей нет, это не ошибка;
- ответ разделения содержит model (bs-roformer-sw | htdemucs); неизвестная
  модель → 422; /config отдаёт stems_pref, stems_model, roformer_available.

Внешние границы подменяются: подпроцесс RoFormer (subprocess.run на python
из YUE_SEP_PY со скриптом sep_run.py) — фейком, который пишет известные
сигналы и печатает JSON по контракту sep_run.py; demucs — stems._run_model.
torch и веса моделей не нужны.

Запуск: cd worker && python3 -m unittest test_roformer_stems -v
Без numpy/soundfile классы разделения пропускаются; API-классы ещё и без
fastapi/httpx (как соседние в test_pure.py).
"""
import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import test_pure as tp

try:
    import soundfile  # noqa: F401
    _HAS_SF = True
except ImportError:
    _HAS_SF = False

# librosa — зависимость воркера (requirements.txt): без неё путь RoFormer
# в stems.py падает и уходит на demucs, кейсы «сработал RoFormer» не проверить
try:
    import librosa  # noqa: F401
    _HAS_LIBROSA = True
except ImportError:
    _HAS_LIBROSA = False

_SEP_OK = tp._HAS_NUMPY and _HAS_SF and _HAS_LIBROSA
_SEP_SKIP = "нужны numpy/soundfile/librosa"
_WORKER_OK = tp._HAS_WORKER_DEPS and _HAS_SF and _HAS_LIBROSA
_WORKER_SKIP = "нужны fastapi/httpx/numpy/soundfile/librosa (окружение воркера)"

SR = 44100
DUR = 2.0
MAIN = ("drums", "bass", "other", "vocals")
DETAIL = ("guitar", "piano")
PARTS = ("kick", "snare", "toms", "hh", "ride", "crash")

# у каждого источника свой тон — по спектру видно, что попало в файл
HZ = {"drums": 110, "bass": 220, "other": 330, "vocals": 440, "guitar": 550, "piano": 660}
PART_HZ = {"kick": 770, "snare": 880, "toms": 990, "hh": 1100, "ride": 1210, "crash": 1320}
AMP = 0.1
PART_AMP = 0.05


def _tone(hz, amp=AMP, dur=DUR, sr=SR):
    import numpy as np
    t = np.arange(int(dur * sr)) / sr
    x = (amp * np.sin(2 * np.pi * hz * t)).astype(np.float32)
    return np.stack([x, x], axis=1)


def _tone_amp(x, hz, sr=SR):
    """Амплитуда синуса частоты hz (одна точка ДПФ по целым секундам)."""
    import numpy as np
    if x.ndim > 1:
        x = x.mean(axis=1)
    n = (len(x) // sr) * sr
    x = x[:n].astype(np.float64)
    t = np.arange(n) / sr
    return 2 * abs(np.sum(x * np.exp(-2j * np.pi * hz * t))) / n


def _mix():
    """Трек = сумма шести источников RoFormer (модель раскладывает его без остатка)."""
    import numpy as np
    return np.sum([_tone(h) for h in HZ.values()], axis=0).astype(np.float32)


def _read(path):
    import soundfile as sf
    x, _ = sf.read(str(path), dtype="float32", always_2d=True)
    return x


class FakeSep:
    """Фейк subprocess.run для `python sep_run.py <аудио> <выход> <веса>`.
    Пишет в каталог выхода сырые дорожки <имя>.wav и drums-<часть>.wav,
    печатает последней строкой JSON по контракту sep_run.py. Чужие вызовы
    subprocess.run уходят в настоящий."""

    _real = staticmethod(subprocess.run)

    def __init__(self, mode="ok"):
        # ok | fail (код выхода 1) | drum_fail (DrumSep упал) | raise (таймаут)
        self.mode = mode
        self.calls = []

    def __call__(self, cmd, *args, **kw):
        argv = [str(a) for a in (cmd if isinstance(cmd, (list, tuple)) else [cmd])]
        idx = next((i for i, a in enumerate(argv) if Path(a).name == "sep_run.py"), None)
        if idx is None:
            return self._real(cmd, *args, **kw)
        self.calls.append(argv)
        src, out = Path(argv[idx + 1]), Path(argv[idx + 2])
        if self.mode == "raise":
            raise subprocess.TimeoutExpired(argv, 1)
        if self.mode == "fail":
            return self._done(argv, 1, "", "Traceback ...\nRuntimeError: CUDA out of memory")
        import soundfile as sf
        try:
            sf.read(str(src))
        except Exception as e:  # noqa: BLE001 — как настоящий sep_run на битом файле
            return self._done(argv, 1, "", f"Traceback ...\n{type(e).__name__}: {e}")
        out.mkdir(parents=True, exist_ok=True)
        stems = {}
        for name, hz in HZ.items():
            p = out / f"{name}.wav"
            sf.write(str(p), _tone(hz), SR)
            stems[name] = str(p)
        parts, err = {}, ""
        if self.mode == "drum_fail":
            err = "RuntimeError: DrumSep weights download failed"
        else:
            for name, hz in PART_HZ.items():
                p = out / f"drums-{name}.wav"
                sf.write(str(p), _tone(hz, amp=PART_AMP), SR)
                parts[name] = str(p)
        stdout = "loading model...\n" + json.dumps({"stems": stems, "drum_parts": parts, "drum_error": err}) + "\n"
        return self._done(argv, 0, stdout, "")

    @staticmethod
    def _done(argv, code, stdout, stderr):
        return subprocess.CompletedProcess(argv, code, stdout=stdout, stderr=stderr)


# demucs-фейк: источник = вход × k; у htdemucs сумма k = 1 (дорожки = трек)
_K4 = {"drums": 0.4, "bass": 0.3, "other": 0.2, "vocals": 0.1}
_K6 = {"drums": 0.4, "bass": 0.3, "other": 0.1, "vocals": 0.1, "guitar": 0.06, "piano": 0.04}


class FakeDemucs:
    def __init__(self):
        self.calls = []

    def __call__(self, name, data, sr):
        self.calls.append(name)
        ks = {"htdemucs": _K4, "htdemucs_6s": _K6}[name]
        return {s: (data * k).astype("float32") for s, k in ks.items()}


RF = "roformer"  # выбор RoFormer (значение настройки/параметра по карточке)


class _SepEnv:
    """Окружение разделения на тест: временный файл YUE_SEP_PY (по умолчанию
    существует — окружение RoFormer «установлено»), каталог весов
    YUE_SEP_MODELS, откат YUE_STEMS_MODEL — только если задан; фейки на
    подпроцессе и demucs."""

    def _setup_sep(self, tmp: Path, sep_mode="ok", sep_py_exists=True, rollback_env=None):
        import stems
        sep_py = tmp / "sep-python"
        if sep_py_exists:
            sep_py.write_text("")
        elif sep_py.exists():
            sep_py.unlink()
        env = {"YUE_SEP_PY": str(sep_py), "YUE_SEP_MODELS": str(tmp / "sep-models")}
        p = mock.patch.dict(os.environ, env)
        p.start()
        self.addCleanup(p.stop)
        if rollback_env is None:
            os.environ.pop("YUE_STEMS_MODEL", None)
        else:
            os.environ["YUE_STEMS_MODEL"] = rollback_env
        self.sep = FakeSep(sep_mode)
        self.demucs = FakeDemucs()
        for p in (mock.patch.object(stems.subprocess, "run", self.sep),
                  mock.patch.object(stems, "_run_model", self.demucs)):
            p.start()
            self.addCleanup(p.stop)


@unittest.skipUnless(_SEP_OK, _SEP_SKIP)
class TestSeparateRoformer(_SepEnv, unittest.TestCase):
    """stems.separate(audio_path, out_dir, prefer=...) с установленным окружением RoFormer."""

    def setUp(self):
        import soundfile as sf
        self._td = tempfile.TemporaryDirectory()
        self.addCleanup(self._td.cleanup)
        self.tmp = Path(self._td.name)
        self.audio = self.tmp / "audio.flac"
        sf.write(str(self.audio), _mix(), SR)
        self.out = self.tmp / "job"
        self.out.mkdir()

    def _separate(self, prefer=RF, **kw):
        """prefer=None — вызов без выбора модели (по умолчанию)."""
        import stems
        self._setup_sep(self.tmp, **kw)
        if prefer is None:
            return stems.separate(self.audio, self.out)
        return stems.separate(self.audio, self.out, prefer=prefer)

    def _files(self):
        return sorted(p.name for p in self.out.iterdir())

    # --- ТК1 ---------------------------------------------------------------
    def test_tc1_main_and_detail_files_model_roformer(self):
        res = self._separate()
        self.assertEqual(len(self.sep.calls), 1, "RoFormer не вызван")
        for n in MAIN + DETAIL:
            self.assertTrue((self.out / f"stem-{n}.flac").is_file(), f"нет stem-{n}.flac: {self._files()}")
        self.assertEqual(res.get("model"), "bs-roformer-sw")
        self.assertEqual(self.demucs.calls, [], "при рабочем RoFormer demucs не нужен")

    def test_tc1_other_is_other_plus_guitar_plus_piano(self):
        self._separate()
        x = _read(self.out / "stem-other.flac")
        for n in ("other", "guitar", "piano"):
            self.assertAlmostEqual(_tone_amp(x, HZ[n]), AMP, delta=0.005,
                                   msg=f"в stem-other нет {n} на своём уровне")
        for n in ("drums", "bass", "vocals"):
            self.assertLess(_tone_amp(x, HZ[n]), 0.002, f"{n} попал в stem-other")

    def test_tc1_main_stems_are_model_stems(self):
        self._separate()
        for n in ("drums", "bass", "vocals"):
            with self.subTest(stem=n):
                x = _read(self.out / f"stem-{n}.flac")
                self.assertAlmostEqual(_tone_amp(x, HZ[n]), AMP, delta=0.005)
                for other in set(HZ) - {n}:
                    self.assertLess(_tone_amp(x, HZ[other]), 0.002, f"{other} в stem-{n}")

    def test_tc1_detail_stems_are_model_guitar_piano(self):
        self._separate()
        for n in DETAIL:
            x = _read(self.out / f"stem-{n}.flac")
            self.assertAlmostEqual(_tone_amp(x, HZ[n]), AMP, delta=0.005, msg=f"stem-{n}")

    # --- ТК2 ---------------------------------------------------------------
    def _assert_main_sum_is_input(self):
        import numpy as np
        ref = _read(self.audio)
        s = np.sum([_read(self.out / f"stem-{n}.flac") for n in MAIN], axis=0)
        n = min(len(s), len(ref))
        self.assertGreater(n, len(ref) - SR // 100, "дорожки короче трека")
        resid = float(np.sqrt(np.mean((s[:n] - ref[:n]) ** 2)))
        level = float(np.sqrt(np.mean(ref[:n] ** 2)))
        self.assertLess(20 * np.log10(resid / level + 1e-12), -40.0,
                        "сумма основных дорожек не совпадает с треком")

    def test_tc2_main_stems_sum_to_input(self):
        self._separate()
        self._assert_main_sum_is_input()

    # --- ТК3 ---------------------------------------------------------------
    def test_tc3_detail_not_in_main_sum(self):
        import numpy as np
        self._separate()
        without_other = np.sum([_read(self.out / f"stem-{n}.flac") for n in ("drums", "bass", "vocals")], axis=0)
        for n in DETAIL:
            self.assertLess(_tone_amp(without_other, HZ[n]), 0.002, f"{n} вне «прочего»")
        full = np.sum([_read(self.out / f"stem-{n}.flac") for n in MAIN], axis=0)
        for n in DETAIL:
            self.assertAlmostEqual(_tone_amp(full, HZ[n]), AMP, delta=0.005,
                                   msg=f"{n} в сумме основных не на уровне трека (удвоен/потерян)")

    # --- ТК4 ---------------------------------------------------------------
    def test_tc4_six_drum_part_files(self):
        self._separate()
        for n in PARTS:
            with self.subTest(part=n):
                f = self.out / f"stem-{n}.flac"
                self.assertTrue(f.is_file(), f"нет {f.name}: {self._files()}")
                self.assertAlmostEqual(_tone_amp(_read(f), PART_HZ[n]), PART_AMP, delta=0.003)

    def test_tc4_drum_parts_not_in_main_stems(self):
        import numpy as np
        self._separate()
        full = np.sum([_read(self.out / f"stem-{n}.flac") for n in MAIN], axis=0)
        for n in PARTS:
            self.assertLess(_tone_amp(full, PART_HZ[n]), 0.002, f"часть {n} попала в основные дорожки")

    # --- ТК5 ---------------------------------------------------------------
    def test_tc5_no_sep_python_demucs_path(self):
        res = self._separate(sep_py_exists=False)  # RoFormer выбран, окружения нет; исключения нет
        self.assertEqual(self.sep.calls, [], "RoFormer вызван без окружения")
        self.assertEqual(res.get("model"), "htdemucs")
        self.assertIn("htdemucs", self.demucs.calls)
        for n in MAIN:
            self.assertTrue((self.out / f"stem-{n}.flac").is_file(), f"нет stem-{n}.flac")
        for n in PARTS:
            self.assertFalse((self.out / f"stem-{n}.flac").exists(), f"stem-{n}.flac без DrumSep")

    # --- ТК6 ---------------------------------------------------------------
    def test_tc6_subprocess_error_falls_back_with_log(self):
        with self.assertLogs("yue-worker", level="WARNING") as cm:
            res = self._separate(sep_mode="fail")
        self.assertEqual(len(self.sep.calls), 1)
        self.assertEqual(res.get("model"), "htdemucs")
        for n in MAIN:
            self.assertTrue((self.out / f"stem-{n}.flac").is_file(), f"нет stem-{n}.flac")
        for n in PARTS:
            self.assertFalse((self.out / f"stem-{n}.flac").exists())
        self.assertTrue(cm.records, "сбой RoFormer не записан в лог")

    def test_tc6_subprocess_timeout_falls_back(self):
        with self.assertLogs("yue-worker", level="WARNING"):
            res = self._separate(sep_mode="raise")
        self.assertEqual(res.get("model"), "htdemucs")
        self.assertTrue((self.out / "stem-vocals.flac").is_file())

    def test_tc6_fallback_main_stems_sum_to_input(self):
        self._separate(sep_mode="fail")
        self._assert_main_sum_is_input()

    # --- ТК7 ---------------------------------------------------------------
    def test_tc7_rollback_env_never_calls_roformer(self):
        res = self._separate(rollback_env="demucs")  # RoFormer выбран и окружение есть
        self.assertEqual(self.sep.calls, [], "YUE_STEMS_MODEL=demucs, а RoFormer вызван")
        self.assertEqual(res.get("model"), "htdemucs")
        self.assertTrue((self.out / "stem-drums.flac").is_file())

    # --- ТК8 ---------------------------------------------------------------
    def test_tc8_drumsep_failure_keeps_main(self):
        res = self._separate(sep_mode="drum_fail")
        self.assertEqual(res.get("model"), "bs-roformer-sw")
        self.assertEqual(self.demucs.calls, [], "сбой DrumSep не повод уходить в demucs")
        for n in MAIN + DETAIL:
            self.assertTrue((self.out / f"stem-{n}.flac").is_file(), f"нет stem-{n}.flac")
        for n in PARTS:
            self.assertFalse((self.out / f"stem-{n}.flac").exists(), f"stem-{n}.flac при упавшем DrumSep")

    # --- ТК9 ---------------------------------------------------------------
    def _broken(self, content: bytes, prefer):
        self.audio.write_bytes(content)
        with self.assertRaises(Exception):  # noqa: B017 — тип ошибки карточкой не задан, важно «упало»
            self._separate(prefer=prefer)
        stems_left = [f for f in self._files() if f.startswith("stem-")]
        self.assertEqual(stems_left, [], f"частичные дорожки после сбоя: {stems_left}")

    def test_tc9_empty_file_raises_without_stems(self):
        for prefer in (RF, None):
            with self.subTest(prefer=prefer):
                self._broken(b"", prefer)

    def test_tc9_garbage_file_raises_without_stems(self):
        for prefer in (RF, None):
            with self.subTest(prefer=prefer):
                self._broken(b"not an audio file at all" * 100, prefer)

    # --- ТК11 --------------------------------------------------------------
    def test_tc11_default_is_demucs_even_with_roformer_env(self):
        res = self._separate(prefer=None)
        self.assertEqual(self.sep.calls, [], "без выбора RoFormer вызван")
        self.assertEqual(res.get("model"), "htdemucs")
        for n in MAIN:
            self.assertTrue((self.out / f"stem-{n}.flac").is_file(), f"нет stem-{n}.flac")
        for n in PARTS:
            self.assertFalse((self.out / f"stem-{n}.flac").exists())

    def test_tc11_explicit_htdemucs(self):
        res = self._separate(prefer="htdemucs")
        self.assertEqual(self.sep.calls, [])
        self.assertEqual(res.get("model"), "htdemucs")


class TestRoformerAvailability(unittest.TestCase):
    """Без аудио-зависимостей: roformer_available отражает наличие файла
    YUE_SEP_PY (ТК14); выбор RoFormer действует, только если он выбран, окружение
    есть и нет отката YUE_STEMS_MODEL=demucs (ТК5, ТК7, ТК11)."""

    def _check(self, exists, prefer=None, rollback=None):
        import stems
        with tempfile.TemporaryDirectory() as td:
            py = Path(td) / "python"
            if exists:
                py.write_text("")
            with mock.patch.dict(os.environ, {"YUE_SEP_PY": str(py)}):
                os.environ.pop("YUE_STEMS_MODEL", None)
                if rollback is not None:
                    os.environ["YUE_STEMS_MODEL"] = rollback
                avail = stems.roformer_available()
                enabled = stems.roformer_enabled() if prefer is None else stems.roformer_enabled(prefer)
                return avail, enabled

    def test_available_reflects_file(self):
        self.assertTrue(self._check(True)[0])
        self.assertFalse(self._check(False)[0])

    def test_not_enabled_by_default(self):
        self.assertFalse(self._check(True)[1])

    def test_enabled_when_chosen_and_installed(self):
        self.assertTrue(self._check(True, RF)[1])

    def test_not_enabled_when_missing(self):
        self.assertFalse(self._check(False, RF)[1])

    def test_not_enabled_by_rollback(self):
        self.assertFalse(self._check(True, RF, "demucs")[1])


@unittest.skipUnless(_WORKER_OK, _WORKER_SKIP)
class TestStemsApiRoformer(_SepEnv, tp._WorkerApiCase):
    """POST /jobs/{id}/stems, /minus, GET/POST /config поверх выбора модели."""

    def setUp(self):
        super().setUp()
        # настройки воркера — свои на тест (как TestAutoStemsInstrumentalConfig)
        data = Path(self._td.name) / "cfg"
        data.mkdir()
        self.settings = data / "settings.json"
        for name, val in (("DATA_DIR", data), ("SETTINGS_PATH", self.settings)):
            p = mock.patch.object(self.w, name, val)
            p.start()
            self.addCleanup(p.stop)

    def _audio_job(self, content=None):
        import soundfile as sf
        jid = self._job(duration=DUR, semantic=False)
        d = self.jobs_dir / str(jid)
        d.mkdir(parents=True, exist_ok=True)
        if content is None:
            sf.write(str(d / "audio.flac"), _mix(), SR)
        else:
            (d / "audio.flac").write_bytes(content)
        with self._conn() as c:
            c.execute("UPDATE jobs SET audio_file='audio.flac' WHERE id=?", (jid,))
        return jid, d

    def _stems(self, jid, model=None):
        params = {} if model is None else {"model": model}
        return self.client.post(f"/jobs/{jid}/stems", params=params)

    def _minus(self, jid, exclude):
        r = self.client.post(f"/jobs/{jid}/minus", json={"exclude": exclude})
        self.assertLess(r.status_code, 300, r.text)
        return r

    def _config(self):
        r = self.client.get("/config")
        self.assertEqual(r.status_code, 200, r.text)
        return r.json()

    # ТК1 + условие 5
    def test_tc1_stems_response_model_roformer(self):
        self._setup_sep(Path(self._td.name))
        jid, _ = self._audio_job()
        r = self._stems(jid, RF)
        self.assertEqual(r.status_code, 200, r.text)
        self.assertEqual(r.json().get("model"), "bs-roformer-sw")

    # ТК3
    def test_tc3_minus_without_other_has_no_guitar(self):
        self._setup_sep(Path(self._td.name))
        jid, d = self._audio_job()
        self.assertEqual(self._stems(jid, RF).status_code, 200)
        self._minus(jid, ["other"])
        x = _read(d / "minus.flac")
        ref = _tone_amp(x, HZ["drums"])
        self.assertGreater(ref, 0.01, "в минусе нет барабанов")
        for n in ("other",) + DETAIL:
            self.assertLess(_tone_amp(x, HZ[n]) / ref, 0.02, f"{n} в минусе без «прочего»")

    # ТК4
    def test_tc4_minus_without_drums_has_no_drum_parts(self):
        self._setup_sep(Path(self._td.name))
        jid, d = self._audio_job()
        self.assertEqual(self._stems(jid, RF).status_code, 200)
        for n in PARTS:
            self.assertTrue((d / f"stem-{n}.flac").is_file(), f"нет stem-{n}.flac")
        r = self._minus(jid, ["drums"])
        self.assertEqual(sorted(r.json().get("kept", [])), ["bass", "other", "vocals"])
        x = _read(d / "minus.flac")
        ref = _tone_amp(x, HZ["bass"])
        self.assertGreater(ref, 0.01)
        self.assertLess(_tone_amp(x, HZ["drums"]) / ref, 0.02, "барабаны в минусе без барабанов")
        for n in PARTS:
            self.assertLess(_tone_amp(x, PART_HZ[n]) / ref, 0.02, f"часть {n} вернула ударные в минус")

    def test_tc4_minus_vocals_only_main(self):
        self._setup_sep(Path(self._td.name))
        jid, _ = self._audio_job()
        self.assertEqual(self._stems(jid, RF).status_code, 200)
        r = self._minus(jid, ["vocals"])
        self.assertEqual(sorted(r.json().get("kept", [])), ["bass", "drums", "other"])

    # ТК5
    def test_tc5_no_sep_python_response_htdemucs(self):
        self._setup_sep(Path(self._td.name), sep_py_exists=False)
        jid, d = self._audio_job()
        r = self._stems(jid, RF)
        self.assertEqual(r.status_code, 200, r.text)
        self.assertEqual(r.json().get("model"), "htdemucs")
        for n in PARTS:
            self.assertFalse((d / f"stem-{n}.flac").exists())

    # ТК6
    def test_tc6_subprocess_error_200_htdemucs(self):
        self._setup_sep(Path(self._td.name), sep_mode="fail")
        jid, d = self._audio_job()
        with self.assertLogs("yue-worker", level="WARNING"):
            r = self._stems(jid, RF)
        self.assertEqual(r.status_code, 200, r.text)
        self.assertEqual(r.json().get("model"), "htdemucs")
        self.assertTrue((d / "stem-vocals.flac").is_file())

    # ТК7
    def test_tc7_rollback_response_htdemucs(self):
        self._setup_sep(Path(self._td.name), rollback_env="demucs")
        self.assertLess(self.client.post("/config", json={"stems_model": RF}).status_code, 300)
        for model in (RF, None):  # ни разовый выбор, ни настройка не обходят откат
            with self.subTest(model=model):
                jid, _ = self._audio_job()
                r = self._stems(jid, model)
                self.assertEqual(r.status_code, 200, r.text)
                self.assertEqual(r.json().get("model"), "htdemucs")
        self.assertEqual(self.sep.calls, [])
        self.assertEqual(self._config().get("stems_model"), "htdemucs")

    # ТК8
    def test_tc8_drumsep_failure_200_roformer(self):
        self._setup_sep(Path(self._td.name), sep_mode="drum_fail")
        jid, d = self._audio_job()
        r = self._stems(jid, RF)
        self.assertEqual(r.status_code, 200, r.text)
        self.assertEqual(r.json().get("model"), "bs-roformer-sw")
        for n in PARTS:
            self.assertFalse((d / f"stem-{n}.flac").exists())

    # ТК9
    def test_tc9_broken_audio_500_separation_failed(self):
        self._setup_sep(Path(self._td.name))
        for model in (RF, None):
            for content in (b"", b"garbage" * 200):
                with self.subTest(model=model, size=len(content)):
                    jid, d = self._audio_job(content)
                    r = self._stems(jid, model)
                    self.assertEqual(r.status_code, 500, r.text)
                    self.assertIn("separation failed", r.json().get("detail", ""))
                    left = sorted(p.name for p in d.iterdir() if p.name.startswith("stem-"))
                    self.assertEqual(left, [], f"частичные дорожки: {left}")

    # ТК11
    def test_tc11_default_settings_use_demucs(self):
        self._setup_sep(Path(self._td.name))  # окружение RoFormer есть
        self.assertFalse(self.settings.exists())
        jid, d = self._audio_job()
        r = self._stems(jid)
        self.assertEqual(r.status_code, 200, r.text)
        self.assertEqual(r.json().get("model"), "htdemucs")
        self.assertEqual(self.sep.calls, [], "по умолчанию RoFormer вызван")
        self.assertTrue((d / "stem-vocals.flac").is_file())

    # ТК12
    def test_tc12_setting_roformer_used_by_default(self):
        self._setup_sep(Path(self._td.name))
        r = self.client.post("/config", json={"stems_model": RF})
        self.assertLess(r.status_code, 300, r.text)
        jid, _ = self._audio_job()
        r = self._stems(jid)
        self.assertEqual(r.status_code, 200, r.text)
        self.assertEqual(r.json().get("model"), "bs-roformer-sw")
        self.assertEqual(len(self.sep.calls), 1)

    def test_tc12_param_htdemucs_overrides_setting_once(self):
        self._setup_sep(Path(self._td.name))
        self.assertLess(self.client.post("/config", json={"stems_model": RF}).status_code, 300)
        jid, _ = self._audio_job()
        r = self._stems(jid, "htdemucs")
        self.assertEqual(r.status_code, 200, r.text)
        self.assertEqual(r.json().get("model"), "htdemucs")
        self.assertEqual(self.sep.calls, [])
        # разово: настройка не изменилась, следующий вызов без параметра — RoFormer
        self.assertEqual(self._config().get("stems_pref"), RF)
        jid2, _ = self._audio_job()
        self.assertEqual(self._stems(jid2).json().get("model"), "bs-roformer-sw")

    def test_tc12_param_roformer_with_default_setting_once(self):
        self._setup_sep(Path(self._td.name))
        jid, _ = self._audio_job()
        r = self._stems(jid, RF)
        self.assertEqual(r.status_code, 200, r.text)
        self.assertEqual(r.json().get("model"), "bs-roformer-sw")
        self.assertEqual(self._config().get("stems_pref"), "htdemucs")
        jid2, _ = self._audio_job()
        self.assertEqual(self._stems(jid2).json().get("model"), "htdemucs")
        self.assertEqual(len(self.sep.calls), 1)

    # ТК13
    def test_tc13_unknown_model_param_422(self):
        self._setup_sep(Path(self._td.name))
        jid, d = self._audio_job()
        for bad in ("xyz", "ROFORMER", "demucs6"):
            with self.subTest(model=bad):
                r = self._stems(jid, bad)
                self.assertEqual(r.status_code, 422, r.text)
        self.assertEqual(self.sep.calls, [])
        self.assertEqual(self.demucs.calls, [])
        self.assertEqual([p.name for p in d.iterdir() if p.name.startswith("stem-")], [])

    def test_tc13_unknown_config_value_422_keeps_pref(self):
        self._setup_sep(Path(self._td.name))
        r = self.client.post("/config", json={"stems_model": "xyz"})
        self.assertEqual(r.status_code, 422, r.text)
        self.assertEqual(self._config().get("stems_pref"), "htdemucs")

    def test_tc13_config_roformer_persists(self):
        import json as _json
        self._setup_sep(Path(self._td.name))
        self.assertEqual(self._config().get("stems_pref"), "htdemucs")  # по умолчанию
        r = self.client.post("/config", json={"stems_model": RF})
        self.assertLess(r.status_code, 300, r.text)
        self.assertEqual(self._config().get("stems_pref"), RF)
        # «перезапуск»: значение живёт в файле настроек воркера и читается из него заново
        self.assertTrue(self.settings.exists(), "выбор не сохранён в настройки воркера")
        saved = self.settings.read_text(encoding="utf-8")
        self.settings.unlink()
        self.assertEqual(self._config().get("stems_pref"), "htdemucs")
        self.settings.write_text(saved, encoding="utf-8")
        self.assertEqual(self._config().get("stems_pref"), RF)
        self.assertEqual(_json.loads(saved).get("stems_model"), RF)

    def test_tc13_config_back_to_htdemucs(self):
        self._setup_sep(Path(self._td.name))
        self.client.post("/config", json={"stems_model": RF})
        r = self.client.post("/config", json={"stems_model": "htdemucs"})
        self.assertLess(r.status_code, 300, r.text)
        self.assertEqual(self._config().get("stems_pref"), "htdemucs")

    # ТК14
    def test_tc14_config_effective_model_and_availability(self):
        cases = [
            # (выбор в настройке, окружение есть, ждём stems_model, ждём roformer_available)
            (None, True, "htdemucs", True),
            (RF, True, "bs-roformer-sw", True),
            (RF, False, "htdemucs", False),
            ("htdemucs", True, "htdemucs", True),
            (None, False, "htdemucs", False),
        ]
        for pref, installed, want_model, want_avail in cases:
            with self.subTest(pref=pref, installed=installed):
                if self.settings.exists():
                    self.settings.unlink()
                self._setup_sep(Path(self._td.name), sep_py_exists=installed)
                if pref is not None:
                    self.assertLess(self.client.post("/config", json={"stems_model": pref}).status_code, 300)
                cfg = self._config()
                self.assertEqual(cfg.get("stems_model"), want_model, cfg)
                self.assertIs(cfg.get("roformer_available"), want_avail, cfg)
                self.assertEqual(cfg.get("stems_pref"), pref or "htdemucs", cfg)


if __name__ == "__main__":
    unittest.main()

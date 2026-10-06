"""Тесты карточки internal-sound-engine: HTTP воркера — POST /jobs/{id}/fx,
GET/POST /fx/assets, флаг fx_engine в /config (тест-кейсы 11–14 карточки).

Контракт (из карточки и контракта задачи):
- POST /jobs/{id}/fx {source, chain, from?, to?, output?, label?}: source — mix или
  дорожка; output mix — трек − дорожка + обработанная (для source=mix — обработанный
  трек), solo — только обработанная дорожка; окно from/to — вне окна (за кроссфейдом
  ≤ 10 мс) трек побитно тот же; результат — вариант dsp-fx-<source>-<8 hex>.flac
  в каталоге джобы, виден в GET /jobs/{id}/dsp; та же цепочка — тот же файл;
- ошибки: 404 нет джобы/звука, 422 цепочка/source/output/окно, 503 движок выключен;
- ресурсы — yue_worker.fx_resources() (тесты подменяют), цепочка с amp —
  внутри yue_worker.gpu_queue(...), без amp — очередь не берётся;
- GET /fx/assets → {amps: [{name, latency}], irs: [{name, sr, seconds}]};
  POST /fx/assets?kind=amp|ir&name=... — сырые байты; 422 на неверное, 413 > 50 МБ;
  хранение <data>/fx/amps, <data>/fx/irs;
- /config: fx_engine (по умолчанию да), POST сохраняет, YUE_FX_ENGINE=0 выключает.

Внешние границы подменяются: модель NAM (FakeAmp из test_fx_engine), загрузка
захвата fx_nam.load_nam (torch), demucs (stems._run_model). Дорожки кладутся
файлами stem-*.flac заранее.

Запуск: cd worker && python3 -m unittest test_fx_api -v
Без окружения воркера (fastapi/httpx/numpy/scipy/soundfile/librosa) — пропуск.
"""
import json
import os
import re
import unittest
from contextlib import contextmanager
from pathlib import Path
from unittest import mock

import test_pure as tp
from test_fx_engine import FakeAmp, FakeResources

try:
    import librosa  # noqa: F401 — метрики варианта (dsp.analyze_file)
    import scipy  # noqa: F401
    import soundfile  # noqa: F401
    _HAS_EXTRA = True
except ImportError:
    _HAS_EXTRA = False

_OK = tp._HAS_WORKER_DEPS and _HAS_EXTRA
_SKIP = "нужны fastapi/httpx/numpy/scipy/soundfile/librosa (окружение воркера)"

SR = 44100
DUR = 4.0
# у каждой дорожки свой тон — по спектру видно, что попало в вариант
HZ = {"drums": 100, "bass": 200, "other": 3000, "vocals": 1000}
AMP = 0.1
# цепочка, которая глушит 1 кГц (тон голоса) и почти не трогает остальные
CUT_1K = [{"type": "eq", "bands": [{"freq_hz": 1000, "gain_db": -24, "q": 4}]}]
# тождественная цепочка по контракту: delay с wet=0 → выход = вход
IDENTITY = [{"type": "delay", "wet": 0}]
NAME_RE = re.compile(r"^dsp-fx-([a-z]+)-[0-9a-f]{8}\.flac$")


def _tone(hz, amp=AMP, dur=DUR, sr=SR):
    import numpy as np
    t = np.arange(int(dur * sr)) / sr
    x = (amp * np.sin(2 * np.pi * hz * t)).astype(np.float32)
    return np.stack([x, x], axis=1)


def _read(path, dtype="float64"):
    import soundfile as sf
    x, sr = sf.read(str(path), dtype=dtype, always_2d=True)
    return x, sr


def _amp(x, hz, t0=1.0, t1=3.0, sr=SR):
    """Амплитуда тона hz на [t0, t1) (целое число периодов)."""
    import numpy as np
    x = np.asarray(x, dtype=np.float64)
    if x.ndim > 1:
        x = x.mean(axis=1)
    seg = x[int(t0 * sr):int(t1 * sr)]
    t = np.arange(len(seg)) / sr
    return 2 * abs(np.sum(seg * np.exp(-2j * np.pi * hz * t))) / len(seg)


def _db(r):
    import numpy as np
    return 20 * np.log10(max(r, 1e-12))


def _residual_db(got, want):
    import numpy as np
    n = min(len(got), len(want))
    d = got[:n] - want[:n]
    return _db(float(np.sqrt(np.mean(d ** 2)) / np.sqrt(np.mean(want[:n] ** 2))))


class _FxApiCase(tp._WorkerApiCase):
    """Своя БД, каталог джоб, каталог данных и настройки на тест; YUE_FX_ENGINE снят;
    fx_resources — фейк с захватом «fake» (задержка 5 сэмплов)."""

    def setUp(self):
        super().setUp()
        data = Path(self._td.name) / "data"
        data.mkdir()
        self.data = data
        self.settings = data / "settings.json"
        for name, val in (("DATA_DIR", data), ("SETTINGS_PATH", self.settings)):
            p = mock.patch.object(self.w, name, val)
            p.start()
            self.addCleanup(p.stop)
        p = mock.patch.dict(os.environ)
        p.start()
        self.addCleanup(p.stop)
        os.environ.pop("YUE_FX_ENGINE", None)
        self.amp_model = FakeAmp(shift=5)
        self.real_fx_resources = self.w.fx_resources  # для тестов с настоящим хранилищем
        self.resources = FakeResources(amps={"fake": self.amp_model})
        p = mock.patch.object(self.w, "fx_resources", lambda: self.resources)
        p.start()
        self.addCleanup(p.stop)

    def _audio_job(self, stems=True, extra=None):
        """Джоба со звуком audio.flac = сумма дорожек; дорожки — файлами stem-*.flac."""
        import numpy as np
        import soundfile as sf
        jid = self._job(duration=DUR, semantic=False)
        d = self.jobs_dir / str(jid)
        d.mkdir(parents=True, exist_ok=True)
        parts = {k: _tone(hz) for k, hz in HZ.items()}
        sf.write(str(d / "audio.flac"), np.sum(list(parts.values()), axis=0), SR)
        if stems:
            for k, x in parts.items():
                sf.write(str(d / f"stem-{k}.flac"), x, SR)
            for k, x in (extra or {}).items():
                sf.write(str(d / f"stem-{k}.flac"), x, SR)
        with self._conn() as c:
            c.execute("UPDATE jobs SET audio_file='audio.flac' WHERE id=?", (jid,))
        return jid, d

    def _fx(self, jid, **body):
        return self.client.post(f"/jobs/{jid}/fx", json=body)

    def _ok(self, jid, **body):
        r = self._fx(jid, **body)
        self.assertEqual(r.status_code, 200, r.text)
        return r.json()

    def _fx_files(self, d):
        return sorted(p.name for p in d.glob("dsp-fx-*.flac"))


# ---------- ТК11: эндпоинт ----------

@unittest.skipUnless(_OK, _SKIP)
class TestFxEndpoint(_FxApiCase):

    def test_tc11_mix_by_stem_replaces_stem(self):
        jid, d = self._audio_job()
        out = self._ok(jid, source="vocals", chain=CUT_1K, output="mix")
        y, sr = _read(d / out["file"])
        track, _ = _read(d / "audio.flac")
        self.assertEqual(sr, SR)
        self.assertEqual(y.shape, track.shape)
        # голос (1 кГц) приглушён, остальные дорожки на месте
        self.assertLess(_db(_amp(y, 1000) / AMP), -18)
        for k in ("drums", "bass", "other"):
            self.assertAlmostEqual(_db(_amp(y, HZ[k]) / AMP), 0, delta=1, msg=k)

    def test_tc11_mix_is_track_minus_stem_plus_processed(self):
        import fx_engine
        jid, d = self._audio_job()
        chain = [{"type": "drive", "gain_db": 20, "output_db": -12}]
        out = self._ok(jid, source="vocals", chain=chain, output="mix")
        y, _ = _read(d / out["file"])
        track, _ = _read(d / "audio.flac")
        stem, _ = _read(d / "stem-vocals.flac", dtype="float32")
        want = track - stem + fx_engine.process(stem, SR, chain).astype("float64")
        self.assertLessEqual(_residual_db(y, want), -60)

    def test_identity_chain_mix_equals_track(self):
        # критерий приёмки «сумма»: дорожка без изменений → mix = исходный трек (≤ −60 дБ)
        jid, d = self._audio_job()
        out = self._ok(jid, source="vocals", chain=IDENTITY, output="mix")
        y, _ = _read(d / out["file"])
        track, _ = _read(d / "audio.flac")
        self.assertEqual(y.shape, track.shape)
        self.assertLessEqual(_residual_db(y, track), -60)

    def test_tc11_output_defaults_to_mix(self):
        jid, d = self._audio_job()
        out = self._ok(jid, source="vocals", chain=CUT_1K)
        y, _ = _read(d / out["file"])
        self.assertAlmostEqual(_db(_amp(y, HZ["drums"]) / AMP), 0, delta=1)

    def test_tc11_solo_only_stem(self):
        jid, d = self._audio_job()
        out = self._ok(jid, source="vocals", chain=IDENTITY, output="solo")
        y, _ = _read(d / out["file"])
        stem, _ = _read(d / "stem-vocals.flac")
        self.assertLessEqual(_residual_db(y, stem), -60)
        for k in ("drums", "bass", "other"):
            self.assertLess(_db(_amp(y, HZ[k]) / AMP), -40, k)

    def test_tc11_source_mix_processes_whole_track(self):
        jid, d = self._audio_job()
        out = self._ok(jid, source="mix", chain=CUT_1K)
        self.assertTrue(NAME_RE.match(out["file"]), out["file"])
        self.assertEqual(NAME_RE.match(out["file"]).group(1), "mix")
        y, _ = _read(d / out["file"])
        self.assertLess(_db(_amp(y, 1000) / AMP), -18)
        for k in ("drums", "bass", "other"):
            self.assertAlmostEqual(_db(_amp(y, HZ[k]) / AMP), 0, delta=1, msg=k)

    def test_tc11_window_outside_bitwise_same(self):
        import numpy as np
        jid, d = self._audio_job()
        out = self._ok(jid, source="vocals", chain=CUT_1K, **{"from": 1.0, "to": 2.0})
        y, _ = _read(d / out["file"])
        track, _ = _read(d / "audio.flac")
        self.assertEqual(y.shape, track.shape)
        xf = int(0.010 * SR) + 1  # кроссфейд ≤ 10 мс + сэмпл на округление
        a, b = int(1.0 * SR) - xf, int(2.0 * SR) + xf
        np.testing.assert_array_equal(y[:a], track[:a], "до окна трек изменён")
        np.testing.assert_array_equal(y[b:], track[b:], "после окна трек изменён")
        # внутри окна обработка звучит
        self.assertLess(_db(_amp(y, 1000, 1.1, 1.9) / AMP), -18)
        self.assertAlmostEqual(_db(_amp(y, HZ["drums"], 1.1, 1.9) / AMP), 0, delta=1)

    def test_tc11_detail_stem_source(self):
        # подробная дорожка (часть барабанов) — тоже источник
        jid, d = self._audio_job(extra={"kick": _tone(60, amp=0.05)})
        out = self._ok(jid, source="kick", chain=IDENTITY, output="solo")
        self.assertEqual(NAME_RE.match(out["file"]).group(1), "kick")
        y, _ = _read(d / out["file"])
        kick, _ = _read(d / "stem-kick.flac")
        self.assertLessEqual(_residual_db(y, kick), -60)

    def test_variant_name_response_and_listing(self):
        jid, d = self._audio_job()
        out = self._ok(jid, source="vocals", chain=CUT_1K, label="Голос без 1 кГц")
        for k in ("file", "label", "created_at", "metrics"):
            self.assertIn(k, out)
        m = NAME_RE.match(out["file"])
        self.assertTrue(m, out["file"])
        self.assertEqual(m.group(1), "vocals")
        self.assertTrue(self.w.DSP_NAME_RE.match(out["file"]))
        self.assertEqual(out["label"], "Голос без 1 кГц")
        self.assertTrue((d / out["file"]).is_file())
        listed = {v["file"]: v for v in self.client.get(f"/jobs/{jid}/dsp").json()}
        self.assertIn(out["file"], listed)
        self.assertEqual(listed[out["file"]]["label"], "Голос без 1 кГц")

    def test_same_chain_overwrites_same_file(self):
        jid, d = self._audio_job()
        a = self._ok(jid, source="vocals", chain=CUT_1K)["file"]
        b = self._ok(jid, source="vocals", chain=CUT_1K)["file"]
        self.assertEqual(a, b)
        self.assertEqual(self._fx_files(d), [a])

    def test_different_chain_window_output_new_file(self):
        jid, d = self._audio_job()
        base = self._ok(jid, source="vocals", chain=CUT_1K)["file"]
        other_chain = self._ok(jid, source="vocals", chain=IDENTITY)["file"]
        window = self._ok(jid, source="vocals", chain=CUT_1K, **{"from": 1.0, "to": 2.0})["file"]
        solo = self._ok(jid, source="vocals", chain=CUT_1K, output="solo")["file"]
        self.assertEqual(len({base, other_chain, window, solo}), 4)
        self.assertEqual(len(self._fx_files(d)), 4)

    def test_stems_made_when_missing(self):
        import numpy as np
        import stems
        calls = []

        def fake_demucs(name, data, sr):
            calls.append(name)
            ks = {"drums": 0.4, "bass": 0.3, "other": 0.2, "vocals": 0.1}
            if name.endswith("6s"):
                ks = {"drums": 0.4, "bass": 0.3, "other": 0.1, "vocals": 0.1, "guitar": 0.06, "piano": 0.04}
            return {s: (np.asarray(data) * k).astype("float32") for s, k in ks.items()}

        p = mock.patch.object(stems, "_run_model", fake_demucs)
        p.start()
        self.addCleanup(p.stop)
        jid, d = self._audio_job(stems=False)
        self.assertFalse((d / "stem-vocals.flac").exists())
        out = self._ok(jid, source="vocals", chain=IDENTITY, output="solo")
        self.assertTrue(calls, "дорожки не делались")
        self.assertTrue((d / "stem-vocals.flac").is_file())
        y, _ = _read(d / out["file"])
        stem, _ = _read(d / "stem-vocals.flac")
        self.assertLessEqual(_residual_db(y, stem), -60)

    # ---------- ошибки ----------

    def test_job_not_found_404(self):
        r = self._fx(9999, source="mix", chain=CUT_1K)
        self.assertEqual(r.status_code, 404, r.text)

    def test_job_without_audio_404(self):
        jid = self._job(duration=DUR, semantic=False)
        r = self._fx(jid, source="mix", chain=CUT_1K)
        self.assertEqual(r.status_code, 404, r.text)

    def test_tc10_bad_chain_422_with_reason(self):
        jid, d = self._audio_job()
        for chain, word in (([{"type": "fuzz"}], "fuzz"),
                            ([{"type": "gate", "threshold_db": 5}], "threshold_db"),
                            ([], None),
                            ("gate", None)):
            with self.subTest(chain=chain):
                r = self._fx(jid, source="vocals", chain=chain)
                self.assertEqual(r.status_code, 422, r.text)
                if word:
                    self.assertIn(word, r.text)
        self.assertEqual(self._fx_files(d), [])

    def test_chain_type_not_a_string_422(self):
        # регрессия кросс-ревью: type — список/объект/число → 422, не 500
        jid, d = self._audio_job()
        for t in ([], {}, 5):
            with self.subTest(type=t):
                r = self._fx(jid, source="vocals", chain=[{"type": t}])
                self.assertEqual(r.status_code, 422, r.text)
        self.assertEqual(self._fx_files(d), [])

    def test_bad_source_output_422(self):
        jid, d = self._audio_job()
        for body in ({"source": "voice", "chain": CUT_1K},
                     {"source": "../audio", "chain": CUT_1K},
                     {"source": "vocals", "chain": CUT_1K, "output": "both"}):
            with self.subTest(body=body):
                self.assertEqual(self._fx(jid, **body).status_code, 422)
        self.assertEqual(self._fx_files(d), [])

    def test_bad_window_422(self):
        jid, d = self._audio_job()
        for fr, to in ((2.0, 1.0), (1.0, 1.0), (-1.0, 1.0), (1.0, DUR + 5)):
            with self.subTest(window=(fr, to)):
                r = self._fx(jid, source="vocals", chain=CUT_1K, **{"from": fr, "to": to})
                self.assertEqual(r.status_code, 422, r.text)
        self.assertEqual(self._fx_files(d), [])


# ---------- ТК12: флаг fx_engine ----------

@unittest.skipUnless(_OK, _SKIP)
class TestFxEngineFlag(_FxApiCase):

    def _config(self):
        r = self.client.get("/config")
        self.assertEqual(r.status_code, 200, r.text)
        return r.json()

    def test_tc12_default_on(self):
        self.assertIs(self._config().get("fx_engine"), True)

    def test_tc12_disabled_by_setting_503(self):
        jid, d = self._audio_job()
        r = self.client.post("/config", json={"fx_engine": False})
        self.assertLess(r.status_code, 300, r.text)
        self.assertIs(self._config().get("fx_engine"), False)
        r = self._fx(jid, source="vocals", chain=CUT_1K)
        self.assertEqual(r.status_code, 503, r.text)
        self.assertEqual(self._fx_files(d), [])
        # выбор живёт в настройках воркера (переживает перезапуск)
        self.assertIs(json.loads(self.settings.read_text(encoding="utf-8")).get("fx_engine"), False)
        # включили обратно — работает
        self.assertLess(self.client.post("/config", json={"fx_engine": True}).status_code, 300)
        self.assertIs(self._config().get("fx_engine"), True)
        self._ok(jid, source="vocals", chain=CUT_1K)

    def test_tc12_env_forces_off(self):
        jid, d = self._audio_job()
        self.assertLess(self.client.post("/config", json={"fx_engine": True}).status_code, 300)
        os.environ["YUE_FX_ENGINE"] = "0"
        self.assertIs(self._config().get("fx_engine"), False)
        r = self._fx(jid, source="vocals", chain=CUT_1K)
        self.assertEqual(r.status_code, 503, r.text)
        self.assertEqual(self._fx_files(d), [])


# ---------- ТК13: захваты и IR ----------

NAM_OK = json.dumps({"version": "0.5.4", "architecture": "WaveNet", "config": {},
                     "weights": [0.1, -0.2, 0.3], "sample_rate": 48000}).encode()


def _wav_bytes(seconds=0.5, sr=48000):
    import io

    import numpy as np
    import soundfile as sf
    buf = io.BytesIO()
    ir = np.zeros(int(seconds * sr), dtype=np.float32)
    ir[10] = 1.0
    sf.write(buf, ir, sr, format="WAV")
    return buf.getvalue()


@unittest.skipUnless(_OK, _SKIP)
class TestFxAssets(_FxApiCase):

    def setUp(self):
        super().setUp()
        # загрузка захвата в память — граница torch/NAM: фейк с известной задержкой
        import fx_nam
        p = mock.patch.object(fx_nam, "load_nam", lambda path: FakeAmp(shift=7))
        p.start()
        self.addCleanup(p.stop)

    def _upload(self, kind, name, data):
        return self.client.post("/fx/assets", params={"kind": kind, "name": name}, content=data)

    def _assets(self):
        r = self.client.get("/fx/assets")
        self.assertEqual(r.status_code, 200, r.text)
        return r.json()

    def test_empty_list(self):
        self.assertEqual(self._assets(), {"amps": [], "irs": []})

    def test_tc13_amp_not_in_store_422(self):
        jid, d = self._audio_job()
        self.resources = FakeResources()  # захватов нет
        r = self._fx(jid, source="vocals", chain=[{"type": "amp", "model": "Plexi"}])
        self.assertEqual(r.status_code, 422, r.text)
        self.assertEqual(self._fx_files(d), [])

    def test_upload_amp_ok_listed_and_stored(self):
        r = self._upload("amp", "Plexi Lead.nam", NAM_OK)
        self.assertEqual(r.status_code, 200, r.text)
        self.assertEqual(r.json(), {"name": "Plexi Lead.nam", "kind": "amp"})
        self.assertTrue((self.data / "fx" / "amps" / "Plexi Lead.nam").is_file())
        amps = self._assets()["amps"]
        self.assertEqual([a["name"] for a in amps], ["Plexi Lead.nam"])
        self.assertIn("latency", amps[0])

    def test_upload_ir_ok_listed_with_sr_and_seconds(self):
        r = self._upload("ir", "room.wav", _wav_bytes(0.5, 48000))
        self.assertEqual(r.status_code, 200, r.text)
        self.assertEqual(r.json(), {"name": "room.wav", "kind": "ir"})
        self.assertTrue((self.data / "fx" / "irs" / "room.wav").is_file())
        irs = self._assets()["irs"]
        self.assertEqual(len(irs), 1)
        self.assertEqual(irs[0]["name"], "room.wav")
        self.assertEqual(irs[0]["sr"], 48000)
        self.assertAlmostEqual(irs[0]["seconds"], 0.5, delta=0.01)

    def test_tc13_bad_amp_content_422(self):
        for data in (b"not json at all", b"", json.dumps({"architecture": "WaveNet"}).encode(),
                     json.dumps({"weights": [1, 2]}).encode(), b"[1, 2, 3]"):
            with self.subTest(data=data[:30]):
                self.assertEqual(self._upload("amp", "x.nam", data).status_code, 422)
        self.assertEqual(self._assets()["amps"], [])

    def test_tc13_wrong_extension_422(self):
        for kind, name, data in (("amp", "x.txt", NAM_OK), ("amp", "x.json", NAM_OK),
                                 ("ir", "x.nam", _wav_bytes()), ("ir", "x.mp3", _wav_bytes())):
            with self.subTest(kind=kind, name=name):
                self.assertEqual(self._upload(kind, name, data).status_code, 422)
        self.assertEqual(self._assets(), {"amps": [], "irs": []})

    def test_tc13_bad_ir_content_422(self):
        self.assertEqual(self._upload("ir", "x.wav", b"RIFF....garbage").status_code, 422)
        self.assertEqual(self._assets()["irs"], [])

    def test_tc13_bad_name_422(self):
        for name in ("a/b.nam", "../x.nam", "..\\x.nam", "a\\b.nam", "..", "/etc/x.nam"):
            with self.subTest(name=name):
                self.assertEqual(self._upload("amp", name, NAM_OK).status_code, 422)
        self.assertEqual(self._assets()["amps"], [])
        self.assertFalse((self.data / "x.nam").exists())
        self.assertFalse((self.data / "fx" / "x.nam").exists())

    def test_upload_ir_uppercase_ext_normalized_and_found(self):
        # регрессия кросс-ревью: «Room.WAV» сохраняется как «Room.wav», цепочка
        # находит IR по обоим написаниям (настоящее хранилище, не фейк ресурсов)
        r = self._upload("ir", "Room.WAV", _wav_bytes(0.5, 48000))
        self.assertEqual(r.status_code, 200, r.text)
        self.assertEqual(r.json().get("name"), "Room.wav")
        self.assertIn("Room.wav", [i["name"] for i in self._assets()["irs"]])
        self.resources = None
        p = mock.patch.object(self.w, "fx_resources", self.real_fx_resources)
        p.start()
        self.addCleanup(p.stop)
        jid, d = self._audio_job()
        for block in ("cab", "reverb"):
            for ir in ("Room.WAV", "Room.wav"):
                with self.subTest(block=block, ir=ir):
                    r = self._fx(jid, source="vocals", chain=[{"type": block, "ir": ir}])
                    self.assertEqual(r.status_code, 200, r.text)
        # контроль: несуществующий IR не находится
        r = self._fx(jid, source="vocals", chain=[{"type": "cab", "ir": "Nope.wav"}])
        self.assertEqual(r.status_code, 422, r.text)

    def test_bad_name_detail_lists_allowed(self):
        # регрессия кросс-ревью: причина отказа по имени — с перечнем допустимого
        for name in ("a/b.wav", "../x.wav"):
            with self.subTest(name=name):
                r = self._upload("ir", name, _wav_bytes())
                self.assertEqual(r.status_code, 422, r.text)
                detail = r.json().get("detail")
                detail = detail if isinstance(detail, str) else json.dumps(detail, ensure_ascii=False)
                self.assertGreater(len(detail), len("bad name"), detail)
                self.assertIn("allowed", detail.lower(), detail)

    def test_bad_kind_422(self):
        self.assertEqual(self._upload("cab", "x.nam", NAM_OK).status_code, 422)

    def test_too_big_413(self):
        big = b"{" + b" " * (50 * 1024 * 1024) + b"}"
        r = self._upload("amp", "big.nam", big)
        self.assertEqual(r.status_code, 413, r.text[:200])
        self.assertEqual(self._assets()["amps"], [])


# ---------- ТК14: очередь GPU только для amp ----------

@unittest.skipUnless(_OK, _SKIP)
class TestFxGpuQueue(_FxApiCase):

    def setUp(self):
        super().setUp()
        self.gpu_calls = []
        self.inside = False
        test = self

        @contextmanager
        def fake_queue(section="gpu"):
            test.gpu_calls.append(section)
            test.inside = True
            try:
                yield
            finally:
                test.inside = False

        p = mock.patch.object(self.w, "gpu_queue", fake_queue)
        p.start()
        self.addCleanup(p.stop)
        # модель отмечает, вызвали ли её внутри очереди
        self.model_inside = []
        orig = self.amp_model.__call__

        def model(x):
            self.model_inside.append(self.inside)
            return orig(x)

        self.resources = FakeResources(amps={"fake": _Wrap(model, self.amp_model)})

    def test_tc14_chain_without_amp_no_gpu_queue(self):
        jid, _ = self._audio_job()
        self._ok(jid, source="mix", chain=[{"type": t} for t in
                                           ("gate", "eq", "comp", "drive", "cab", "reverb", "delay")])
        self.assertEqual(self.gpu_calls, [])

    def test_tc14_chain_with_amp_takes_gpu_queue(self):
        jid, _ = self._audio_job()
        self._ok(jid, source="mix", chain=[{"type": "eq"}, {"type": "amp", "model": "fake"}])
        self.assertTrue(self.gpu_calls, "amp прошёл мимо gpu_queue")
        self.assertTrue(self.model_inside, "модель не вызывалась")
        self.assertTrue(all(self.model_inside), "модель вызвана вне gpu_queue")


class _Wrap:
    """Модель-обёртка: вызов через fn, sr/latency — как у исходной."""

    def __init__(self, fn, model):
        self._fn = fn
        self.sr = model.sr
        self.latency = model.latency

    def __call__(self, x):
        return self._fn(x)


if __name__ == "__main__":
    unittest.main()

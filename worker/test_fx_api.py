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
- GET /fx/assets → {amps: [{name, latency}], irs: [{name, sr, seconds}],
  kits: [{name, samples}]} (kits — internal-studio-engine, условие 14);
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
        self.assertEqual(self._assets(), {"amps": [], "irs": [], "kits": []})

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
        self.assertEqual(self._assets(), {"amps": [], "irs": [], "kits": []})

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


# ---------- Условие 36 (закрытие хвостов): ТК39 (422), ТК40 (36d), ТК41 (36e) ----------
#
# 36d: замер задержки захвата привязан к версии файла .nam (mtime_ns и размер): файл
#      заменили на диске без загрузки через API — при следующем использовании замер
#      заново. Граница torch/NAM — fx_nam.load_nam(path, latency): latency=None — «замерить»
#      (фейк ставит задержку, которую сейчас «показывает» модель), число — взять как есть.
#      Хранилище настоящее: <data>/fx/amps/<name>.nam, ресурсы — yue_worker.fx_resources().
# 36e/36c: ошибки цепочки по HTTP — 422 с понятной причиной, не 500.


@unittest.skipUnless(_OK, _SKIP)
class TestNamLatencyVersion(_FxApiCase):
    """ТК40 (36d): заменили файл захвата на диске — замер делается заново."""

    def setUp(self):
        super().setUp()
        import fx_nam
        self.true_shift = 7          # задержка, которую «покажет» замер модели сейчас
        self.loads = []              # (имя файла, переданная latency)
        test = self

        def fake_load(path, latency=None, *a, **kw):
            test.loads.append((Path(path).name, latency))
            m = FakeAmp(shift=test.true_shift)
            m.latency = test.true_shift if latency is None else int(latency)
            return m

        p = mock.patch.object(fx_nam, "load_nam", fake_load)
        p.start()
        self.addCleanup(p.stop)
        self.amps = self.data / "fx" / "amps"
        self.amps.mkdir(parents=True, exist_ok=True)

    def _latency(self, name):
        # каждый раз — свежий вызов fx_resources(), как у очередного запроса /fx
        return int(self.real_fx_resources().amp(name).latency)

    def _put(self, name, data, mtime_ns=None):
        path = self.amps / name
        path.write_bytes(data)
        if mtime_ns is not None:
            os.utime(path, ns=(mtime_ns, mtime_ns))
        return path

    def test_tc40_unchanged_file_keeps_saved_measurement(self):
        # охрана: файл не трогали — замер не повторяется, даже если модель «показала бы» другое
        self._put("Plexi.nam", NAM_OK)
        self.assertEqual(self._latency("Plexi.nam"), 7)
        self.true_shift = 11
        self.assertEqual(self._latency("Plexi.nam"), 7, "файл тот же, а замер сделан заново")

    def test_tc40_replaced_file_remeasured(self):
        other = NAM_OK.replace(b"0.3]", b"0.25]")      # другой захват: другой размер
        self.assertNotEqual(len(other), len(NAM_OK))
        path = self._put("Plexi.nam", NAM_OK)
        self.assertEqual(self._latency("Plexi.nam"), 7)
        st = path.stat()
        self.true_shift = 11
        self._put("Plexi.nam", other, mtime_ns=st.st_mtime_ns + 5 * 10**9)
        self.assertEqual(self._latency("Plexi.nam"), 11, "файл заменён, а взят старый замер")
        # и дальше новый замер держится (сохранён для новой версии файла)
        self.true_shift = 3
        self.assertEqual(self._latency("Plexi.nam"), 11)

    def test_tc40_same_size_new_mtime_remeasured(self):
        other = NAM_OK.replace(b"0.1,", b"0.2,")        # тот же размер, другое содержимое
        self.assertEqual(len(other), len(NAM_OK))
        path = self._put("Lead.nam", NAM_OK)
        self.assertEqual(self._latency("Lead.nam"), 7)
        st = path.stat()
        self.true_shift = 13
        self._put("Lead.nam", other, mtime_ns=st.st_mtime_ns + 5 * 10**9)
        self.assertEqual(self._latency("Lead.nam"), 13, "mtime другой — замер должен повториться")

    def test_tc40_same_mtime_new_size_remeasured(self):
        other = NAM_OK.replace(b"0.3]", b"0.333]")      # другой размер, mtime вернули прежний
        path = self._put("Crunch.nam", NAM_OK)
        self.assertEqual(self._latency("Crunch.nam"), 7)
        st = path.stat()
        self.true_shift = 9
        self._put("Crunch.nam", other, mtime_ns=st.st_mtime_ns)
        self.assertEqual(path.stat().st_mtime_ns, st.st_mtime_ns)
        self.assertEqual(self._latency("Crunch.nam"), 9, "размер другой — замер должен повториться")

    def test_tc40_replaced_file_used_by_chain_with_new_latency(self):
        # путь запроса: цепочка с amp на настоящих ресурсах после замены файла —
        # модель получает новую задержку (импульсный вход: пик выхода на месте)
        import numpy as np
        import fx_engine
        path = self._put("Plexi.nam", NAM_OK)
        self.assertEqual(self._latency("Plexi.nam"), 7)
        self.true_shift = 40
        self._put("Plexi.nam", NAM_OK + b" ", mtime_ns=path.stat().st_mtime_ns + 5 * 10**9)
        x = np.zeros(48000, dtype=np.float32)
        x[20011] = 1.0
        y = fx_engine.process(x, 48000, [{"type": "amp", "model": "Plexi.nam"}], self.real_fx_resources())
        self.assertEqual(int(np.argmax(np.abs(y))), 20011, "сдвиг: применён старый замер задержки")


@unittest.skipUnless(_OK, _SKIP)
class TestTailsChainErrors422(_FxApiCase):
    """ТК39/ТК41 по HTTP: 422 с причиной, не 500."""

    def _detail(self, r):
        d = r.json().get("detail")
        return d if isinstance(d, str) else json.dumps(d, ensure_ascii=False)

    def test_tc39_17_blocks_422(self):
        jid, d = self._audio_job()
        r = self._fx(jid, source="vocals", chain=[{"type": "gain"}] * 17, output="solo")
        self.assertEqual(r.status_code, 422, r.text)
        self.assertIn("16", self._detail(r))
        self.assertEqual(self._fx_files(d), [])

    def test_tc39_13_bands_422(self):
        jid, _ = self._audio_job()
        bands = [{"freq_hz": 100 + 150 * i, "gain_db": 1, "q": 1} for i in range(13)]
        r = self._fx(jid, source="vocals", chain=[{"type": "eq", "bands": bands}], output="solo")
        self.assertEqual(r.status_code, 422, r.text)
        self.assertIn("12", self._detail(r))

    def test_tc39_16_blocks_12_bands_ok(self):
        jid, _ = self._audio_job()
        bands = [{"freq_hz": 100 + 150 * i, "gain_db": 0, "q": 1} for i in range(12)]
        chain = [{"type": "eq", "bands": bands}] + [{"type": "gain"}] * 15
        self._ok(jid, source="vocals", chain=chain, output="solo")

    def test_tc41_latency_longer_than_input_422(self):
        jid, d = self._audio_job()
        n_model = int(DUR * 48000)                       # вход 4 с; задержка в сэмплах модели 48 кГц
        for lat in (n_model + 4800, -(n_model + 4800)):
            with self.subTest(latency=lat):
                self.resources = FakeResources(amps={"fake": FakeAmp(shift=0, sr=48000, latency=lat)})
                r = self._fx(jid, source="vocals", chain=[{"type": "amp", "model": "fake"}], output="solo")
                self.assertEqual(r.status_code, 422, r.text[:300])
                self.assertIn("захват", self._detail(r).lower())
        self.assertEqual(self._fx_files(d), [])



# ---------- Карточка internal-own-track, этап 4, условие 25 (ТК50): synth в /fx ----------
#
# Контракт: POST /jobs/{id}/fx {source: "mix", output: "solo", preview: true, from, to,
# chain: [synth]} — ноты synth в запросе от начала ТРЕКА; воркер сдвигает их на окно:
# нота t звучит в превью через t − from; нота до окна не звучит. output solo — в файле
# только синт (тонов трека нет). Реализацию не читали.

SYN_DUR = 16.0
SYN_TRACK_HZ = 100            # тон трека: в solo-файле его быть не должно
SYN_SINE = {"rel_db": 0, "osc1": 4, "osc2": 4, "osc2_semi": 0, "osc_mix": 0, "unison": 1, "detune_cents": 0,
            "sub": 0, "noise": 0, "cutoff_hz": 16000, "resonance": 0, "env_amount": 0,
            "vib_cents": 0, "lfo_cutoff": 0, "attack_s": 0.005, "decay_s": 0.05, "sustain": 1,
            "release_s": 0.05, "output_db": 0}


def _synth_chain(notes):
    return [{"type": "synth", "notes": notes, **SYN_SINE}]


@unittest.skipUnless(_OK, _SKIP)
class TestSynthWindowShift(_FxApiCase):
    """ТК50: from = 10 — нота t = 12 в превью на 2 с, нота t = 5 не звучит."""

    def _long_job(self):
        import numpy as np
        import soundfile as sf
        jid = self._job(duration=SYN_DUR, semantic=False)
        d = self.jobs_dir / str(jid)
        d.mkdir(parents=True, exist_ok=True)
        x = _tone(SYN_TRACK_HZ, amp=0.3, dur=SYN_DUR)
        sf.write(str(d / "audio.flac"), x.astype(np.float32), SR)
        with self._conn() as c:
            c.execute("UPDATE jobs SET audio_file='audio.flac' WHERE id=?", (jid,))
        return jid, d

    def _preview(self, jid, notes, fr=10.0, to=14.0):
        r = self._fx(jid, source="mix", output="solo", preview=True, chain=_synth_chain(notes),
                     **{"from": fr, "to": to})
        self.assertEqual(r.status_code, 200, r.text)
        return r.json()

    def test_tk50_note_in_window_shifted_by_from(self):
        import numpy as np
        jid, d = self._long_job()
        out = self._preview(jid, [{"t": 12.0, "d": 1.0, "midi": [69], "vel": 1.0}])
        y, sr = _read(d / out["file"])
        self.assertEqual(sr, SR)
        self.assertAlmostEqual(len(y) / SR, 4.0, delta=0.01)       # окно 10–14 с, без хвоста
        a = np.abs(y).max(axis=1)
        peak = float(a.max())
        # усл. 78 (этап 9): уровень «на ухо» — к тону трека 100 Гц (A ≈ −19 дБ) синус 440 Гц встаёт на ~15 дБ тише по
        # простому уровню, чем было по RMS; тест — про сдвиг окна, порог «синт есть» ниже
        self.assertGreater(peak, 0.02, "в превью нет синта")
        # до 2 с (12 − 10) — тишина, нота звучит на 2,1–2,9 с
        self.assertLess(_db(float(a[:int(1.98 * SR)].max()) / peak), -60, "нота не на t − from")
        self.assertGreater(_amp(y, 440, 2.1, 2.9), 0.02, "нота A4 не звучит через 2 с от начала окна")

    def test_tk50_solo_without_track(self):
        jid, d = self._long_job()
        out = self._preview(jid, [{"t": 12.0, "d": 1.0, "midi": [69], "vel": 1.0}])
        y, _ = _read(d / out["file"])
        # output solo: тона трека (100 Гц) в файле нет
        self.assertLess(_amp(y, SYN_TRACK_HZ, 0.0, 1.9), 1e-3)
        self.assertLess(_amp(y, SYN_TRACK_HZ, 2.0, 3.0), 1e-3)

    def test_tk50_note_before_window_silent(self):
        import numpy as np
        jid, d = self._long_job()
        out = self._preview(jid, [{"t": 5.0, "d": 1.0, "midi": [69], "vel": 1.0}])
        y, _ = _read(d / out["file"])
        self.assertLess(float(np.abs(y).max()), 1e-4, "нота до окна звучит в превью")

    def test_note_after_window_not_played(self):
        # кросс-ревью s4: нота после окна не звучит даже в хвосте реверба (партия — только в выделении)
        import numpy as np
        jid, d = self._long_job()
        chain = _synth_chain([{"t": 14.5, "d": 1.0, "midi": [69], "vel": 1.0}]) + [{"type": "reverb", "wet": 0.3}]
        r = self._fx(jid, source="mix", output="solo", preview=True, add=True, chain=chain,
                     **{"from": 10.0, "to": 14.0})     # как шлют студия и пересборка: добавление
        self.assertEqual(r.status_code, 200, r.text)
        y, _ = _read(d / r.json()["file"])
        self.assertLess(float(np.abs(y).max()), 1e-4, "нота за окном прозвучала в хвосте")

    def test_add_without_preview_422(self):
        # кросс-ревью s4: add — только у превью (вариант на весь трек заменил бы трек синтом)
        jid, _ = self._long_job()
        chain = _synth_chain([{"t": 12.0, "d": 1.0, "midi": [69], "vel": 1.0}])
        r = self._fx(jid, source="mix", output="mix", add=True, chain=chain, **{"from": 10.0, "to": 14.0})
        self.assertEqual(r.status_code, 422, r.text)

    def test_synth_silent_after_window_even_with_tail(self):
        # кросс-ревью s4 r2: длинная нота с долгим затуханием — синт молчит после края окна (хвост — только
        # эффекта; delay с wet 0 даёт буфер хвоста без самого эффекта)
        import numpy as np
        jid, d = self._long_job()
        chain = [{"type": "synth", "notes": [{"t": 11.0, "d": 10.0, "midi": [69], "vel": 1.0}], **SYN_SINE,
                  "release_s": 2.0}, {"type": "delay", "wet": 0.0}]
        r = self._fx(jid, source="mix", output="solo", preview=True, add=True, chain=chain,
                     **{"from": 10.0, "to": 14.0})
        self.assertEqual(r.status_code, 200, r.text)
        y, _ = _read(d / r.json()["file"])
        self.assertGreater(len(y) / SR, 4.5)                                   # хвост буфера есть
        self.assertLess(float(np.abs(y[int(4.0 * SR):]).max()), 1e-4, "синт звучит за краем окна")

    def test_bad_synth_notes_422(self):
        jid, _ = self._long_job()
        self._preview(jid, [{"t": 12.0, "d": 1.0, "midi": [69], "vel": 1.0}])  # опора: верная — 200
        r = self._fx(jid, source="mix", output="solo", preview=True,
                     chain=_synth_chain([{"t": 12.0, "d": 1.0, "midi": [200], "vel": 1.0}]),
                     **{"from": 10.0, "to": 14.0})
        self.assertEqual(r.status_code, 422, r.text)


if __name__ == "__main__":
    unittest.main()


@unittest.skipUnless(_OK, _SKIP)
class TestSynthWindowPrep(unittest.TestCase):
    """Кросс-ревью s4 r2: подготовка нот партии к окну превью (synth_window) — давние ноты не ломают превью."""

    def test_old_notes_dropped_long_start_clamped(self):
        import yue_worker as w
        chain = [{"type": "synth", "notes": [{"t": 0.0, "d": 2.0, "midi": [60]},        # отзвучала давно
                                             {"t": 100.0, "d": 700.0, "midi": [62]},    # тянется через окно
                                             {"t": 710.0, "d": 1.0, "midi": [64]}]}]    # после окна
        out = w.synth_window(chain, 700.0, 4.0)[0]
        self.assertEqual([n["midi"] for n in out["notes"]], [[62]])
        n = out["notes"][0]
        self.assertGreaterEqual(n["t"], -w.SYNTH_LEAD_S)
        self.assertAlmostEqual(n["t"] + n["d"], 4.0)
        self.assertEqual(out["_until"], 4.0)
        import fx_engine
        fx_engine.parse_chain([out])          # лимит начала ноты не мешает



@unittest.skipUnless(_OK, _SKIP)
class TestSynthRefLevel(unittest.TestCase):
    """Кросс-ревью условия 29: уровень синта — от громкости трека по ВСЕМ нотам партии, одинаково для превью
    любого окна и для пересборки (было: по окну — тихий куплет и весь трек давали разницу до 18 дБ)."""

    def test_ref_same_for_any_window(self):
        import numpy as np
        import yue_worker as w
        import fx_engine
        sr = 8000
        tone = np.sin(2 * np.pi * 1000 * np.arange(40 * sr) / sr)[:, None].repeat(2, axis=1)
        track = tone * np.concatenate([np.full(15 * sr, 0.01), np.full(25 * sr, 0.1)])[:, None]   # тихо, затем громко
        chain = [{"type": "synth", "notes": [{"t": 2.0, "d": 30.0, "midi": [60]}]}]
        a = w.synth_window(chain, 0.0, 15.0, track, sr)[0]["_ref_rms"]
        b = w.synth_window(chain, 0.0, 40.0, track, sr)[0]["_ref_rms"]
        self.assertAlmostEqual(a, b)
        # усл. 78: опора — уровень трека «на ухо» (A) на нотах партии
        want = float(np.sqrt(np.mean(fx_engine.a_weight(track, sr)[2 * sr:32 * sr] ** 2)))
        self.assertAlmostEqual(a, want, places=6)


    def test_window_level_equals_full(self):
        # кросс-ревью r2: тихая нота (vel 0,1) в окне превью звучит так же, как в полном рендере (было +17 дБ)
        import numpy as np
        import fx_engine
        import yue_worker as w
        sr = 8000
        track = np.full((40 * sr, 1), 0.1)
        blk = {"type": "synth", "osc1": 4, "cutoff_hz": 16000, "release_s": 0.05, "rel_db": -10,
               "notes": [{"t": 1.0, "d": 10.0, "midi": [69], "vel": 0.1},
                         {"t": 20.0, "d": 10.0, "midi": [69], "vel": 1.0}]}
        short = w.synth_window([blk], 0.0, 15.0, track, sr)
        full = w.synth_window([blk], 0.0, 40.0, track, sr)
        ys = fx_engine.process(track[:15 * sr], sr, short)
        yf = fx_engine.process(track, sr, full)
        seg = slice(3 * sr, 9 * sr)
        rs, rf = (20 * np.log10(np.sqrt(np.mean(y[seg] ** 2))) for y in (ys, yf))
        self.assertAlmostEqual(rs, rf, delta=0.5)


    def test_late_window_in_long_slow_note(self):
        # кросс-ревью r3: окно с 20 с внутри длинной ноты с медленной атакой/спадом — уровень как в полном рендере
        import numpy as np
        import fx_engine
        import yue_worker as w
        sr = 8000
        track = np.full((40 * sr, 1), 0.1)
        blk = {"type": "synth", "osc1": 4, "cutoff_hz": 16000, "attack_s": 5, "decay_s": 5, "sustain": 0.2,
               "release_s": 0.05, "notes": [{"t": 1.0, "d": 35.0, "midi": [69], "vel": 1.0}]}
        win = w.synth_window([blk], 20.0, 10.0, track, sr)
        full = w.synth_window([blk], 0.0, 40.0, track, sr)
        yw = fx_engine.process(track[20 * sr:30 * sr], sr, win)
        yf = fx_engine.process(track, sr, full)
        rw = 20 * np.log10(np.sqrt(np.mean(yw[2 * sr:8 * sr] ** 2)))
        rf = 20 * np.log10(np.sqrt(np.mean(yf[22 * sr:28 * sr] ** 2)))
        self.assertAlmostEqual(rw, rf, delta=0.5)


    def test_private_fields_from_request_ignored(self):
        # кросс-ревью r4: присланные _until/_ref_rms/_syn_rms не меняют уровень партии
        import yue_worker as w
        import numpy as np
        sr = 8000
        track = np.full((10 * sr, 1), 0.1)
        blk = {"type": "synth", "osc1": 4, "notes": [{"t": 1.0, "d": 5.0, "midi": [69], "vel": 1.0}]}
        clean = w.synth_window([blk], 0.0, 10.0, track, sr)[0]
        dirty = w.synth_window([dict(blk, _until=0.0, _ref_rms=1000.0, _syn_rms=1e-11, _syn_peak=0.0)],
                               0.0, 10.0, track, sr)[0]
        for k in ("_until", "_ref_rms", "_syn_rms", "_syn_peak"):
            self.assertAlmostEqual(dirty[k], clean[k], msg=k)


# ---------- Карточка internal-own-track, этап 5, условие 31 (ТК57): perc в /fx ----------
#
# Контракт: POST /jobs/{id}/fx {source: "mix", output: "solo", preview: true, add: true, from, to,
# chain: [perc]} — удары perc в запросе от начала ТРЕКА; воркер сдвигает их на окно (как synth):
# удар t звучит в превью через t − from; отзвучавший до окна — не звучит; уровень партии — по ВСЕМ
# ударам (превью любого окна и полный рендер дают одну громкость удара ±1 дБ); служебные поля
# (_ref_rms и т. п.) из запроса выбрасываются. Реализацию не читали.

PERC_DUR = 30.0


def _perc_chain(times, **extra):
    # ковбелл (voice 4) без humanize: тональный, место и пик удара однозначны (выбор теста)
    return [{"type": "perc", "voice": 4, "humanize_ms": 0, "rel_db": -14,
             "notes": [{"t": t, "d": 0.5, "vel": 1.0} for t in times], **extra}]


@unittest.skipUnless(_OK, _SKIP)
class TestPercWindow(_FxApiCase):
    """ТК57: превью perc с from = 10 — удар t = 12 на 2 с, удар t = 5 не звучит, громкость как в полном."""

    def _job30(self):
        # трек 30 с: первые 10 с тихо (тон 0,01), дальше громко (0,3) — уровень «по окну» и «по всем
        # ударам» различаются, если в тихой части есть удар (t = 5)
        import numpy as np
        import soundfile as sf
        jid = self._job(duration=PERC_DUR, semantic=False)
        d = self.jobs_dir / str(jid)
        d.mkdir(parents=True, exist_ok=True)
        x = _tone(SYN_TRACK_HZ, amp=0.3, dur=PERC_DUR)
        x[:int(10 * SR)] *= 0.01 / 0.3
        sf.write(str(d / "audio.flac"), x.astype(np.float32), SR)
        with self._conn() as c:
            c.execute("UPDATE jobs SET audio_file='audio.flac' WHERE id=?", (jid,))
        return jid, d

    def _preview(self, jid, d, chain, fr, to):
        r = self._fx(jid, source="mix", output="solo", preview=True, add=True, chain=chain,
                     **{"from": fr, "to": to})
        self.assertEqual(r.status_code, 200, r.text)
        y, sr = _read(d / r.json()["file"])
        self.assertEqual(sr, SR)
        return y

    def test_tk57_hit_shifted_by_from(self):
        import numpy as np
        jid, d = self._job30()
        y = self._preview(jid, d, _perc_chain([12.0]), 10.0, 14.0)
        a = np.abs(y).max(axis=1)
        peak = float(a.max())
        self.assertGreater(peak, 1e-3, "в превью нет удара")
        self.assertLess(_db(float(a[:int(1.998 * SR)].max()) / peak), -60, "удар раньше t − from")
        first = int(np.argmax(a >= 0.1 * peak)) / SR
        self.assertAlmostEqual(first, 2.0, delta=0.002, msg="удар не через 2 с от начала окна")

    def test_tk57_hit_before_window_silent(self):
        import numpy as np
        jid, d = self._job30()
        y = self._preview(jid, d, _perc_chain([5.0]), 10.0, 14.0)
        self.assertLess(float(np.abs(y).max()), 1e-4, "удар до окна звучит в превью")

    def test_tk57_window_level_equals_full(self):
        # окно [10, 25) против полного рендера [0, 30): пик удара t = 12 совпадает ±1 дБ
        import numpy as np
        jid, d = self._job30()
        chain = _perc_chain([5.0, 12.0, 20.0, 27.0])
        yw = self._preview(jid, d, chain, 10.0, 25.0)
        yf = self._preview(jid, d, chain, 0.0, PERC_DUR)
        pw = float(np.abs(yw[int(2.0 * SR):int(2.45 * SR)]).max())
        pf = float(np.abs(yf[int(12.0 * SR):int(12.45 * SR)]).max())
        self.assertGreater(pf, 1e-4)
        self.assertAlmostEqual(_db(pw / pf), 0, delta=1)

    def test_tk57_sent_ref_rms_ignored(self):
        import numpy as np
        jid, d = self._job30()
        clean = self._preview(jid, d, _perc_chain([12.0]), 10.0, 14.0)
        dirty = self._preview(jid, d, _perc_chain([12.0], _ref_rms=1000.0), 10.0, 14.0)
        pc, pd = float(np.abs(clean).max()), float(np.abs(dirty).max())
        self.assertGreater(pc, 1e-4)
        self.assertAlmostEqual(_db(pd / pc), 0, delta=0.1, msg="присланный _ref_rms изменил громкость")


# ---------- Карточка internal-own-track, уточнение 31а (ТК64–ТК65): perc в превью окна ----------

@unittest.skipUnless(_OK, _SKIP)
class TestPercWindowCarry(TestPercWindow):
    """ТК64: удар до окна, ещё звучащий, в превью — как в полном рендере; ТК65: нет набора → 422."""

    # тесты ТК57 наследуются только ради хелперов — здесь их не повторяем
    test_tk57_hit_shifted_by_from = None
    test_tk57_hit_before_window_silent = None
    test_tk57_window_level_equals_full = None
    test_tk57_sent_ref_rms_ignored = None

    def test_tk64_ringing_hit_before_window_sounds_as_full(self):
        # ковбелл decay 3, удар t = 8,9 — до окна [10, 12), но ещё звучит: первые 0,1 с превью
        # совпадают по пику с [10; 10,1) полного рендера [0, 30)
        import numpy as np
        jid, d = self._job30()
        chain = _perc_chain([8.9], decay=3)
        yw = self._preview(jid, d, chain, 10.0, 12.0)
        yf = self._preview(jid, d, chain, 0.0, PERC_DUR)
        pw = float(np.abs(yw[:int(0.1 * SR)]).max())
        pf = float(np.abs(yf[int(10.0 * SR):int(10.1 * SR)]).max())
        self.assertGreater(pf, 1e-4, "в полном рендере удар к 10 с уже затих — тест не проверяет перенос")
        self.assertGreater(pw, 1e-4, "звучащий удар до окна выброшен из превью")
        self.assertAlmostEqual(_db(pw / pf), 0, delta=1, msg="хвост удара в превью не как в полном рендере")

    def test_tk65_missing_kit_422(self):
        jid, d = self._job30()
        chain = [{"type": "perc", "voice": 0, "kit": "nosuch/kick", "humanize_ms": 0,
                  "notes": [{"t": 12.0, "d": 0.5, "vel": 1.0}]}]
        r = self._fx(jid, source="mix", output="solo", preview=True, add=True, chain=chain,
                     **{"from": 10.0, "to": 14.0})
        self.assertEqual(r.status_code, 422, r.text[:300])
        detail = r.json().get("detail")
        text = (detail if isinstance(detail, str) else json.dumps(detail, ensure_ascii=False)).lower()
        self.assertIn("набор", text)
        self.assertEqual(self._fx_files(d), [])

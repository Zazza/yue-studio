"""Тесты карточки internal-own-track, этап 7б: условия 56 и 58 — наборы, которые воркер синтезирует сам
(тест-кейс ТК91 и вторая половина ТК92).

Контракт (из карточки):
- POST /fx/kits/install?name=<набор> для tr808, tr909, linn, cr78, simmons и synthbass в сеть не ходит:
  пишет wav (48 кГц, моно, PCM 16) в <data>/fx/kits/<набор>/<часть>/, ответ
  {name, parts, downloaded: false, generated: true}; повтор — без перезаписи (generated false);
  неизвестный набор — 422;
- части драм-машины — 12 (kick … cowbell), по 8 слоёв (wav на слой); synthbass — moog/sub808/acid,
  по сэмплу на ноту BASS_RANGE (у всех трёх 28…55 — по 28, условие 58б);
- ТК91: sampler kit «tr808/kick» на дорожке с 4 щелчками → 4 удара на местах щелчков ±2 мс;
- ТК92 (новая редакция): блок bass с kit «synthbass/sub808» на синусе 41,2 Гц (E1) восьмыми → основной тон
  выхода 41,2 ±1,5 Гц (не 82,4); с kit «synthbass/moog» на синусе 196 Гц (G3) → 196 ±3 Гц (не 98);
- условие 58а: каждый модуль worker/*.py, который импортируют yue_worker.py или fx_engine.py, есть в команде
  scp в deploy.sh (иначе на машине воркера ModuleNotFoundError) — проверка по исходникам, без запуска.

Внешняя граница — сеть: urllib.request.OpenerDirector.open (через него идут urlopen, build_opener,
urlretrieve) подменён на ошибку и записывает вызовы; прямые сокеты запрещены. Хранилище — своё
на тест (каталог данных из test_fx_api). Обработка — fx_engine.process с настоящими ресурсами воркера
(fx_resources читает установленные файлы).

Запуск: cd worker && python3 -m unittest test_fx_kits_synth -v
"""
import ast
import os
import re
import socket
import unittest
import urllib.error
import urllib.request
from pathlib import Path
from unittest import mock

import test_fx_api as fa

MACHINES = ("tr808", "tr909", "linn", "cr78", "simmons")
DRUM_PARTS = ("kick", "snare", "hh-closed", "hh-open", "ride", "crash",
              "tom-small", "tom-medium", "tom-large", "clap", "rim", "cowbell")
BASS_PARTS = {"moog": 28, "sub808": 28, "acid": 28}
KIT_SR = 48000
OLD_NS = 1_000_000_000 * 1_000_000_000   # 2001-09-09: «старое» время файлов перед повтором


@unittest.skipUnless(fa._OK, fa._SKIP)
class _SynthKitCase(fa._FxApiCase):
    """Своё хранилище; сеть — ошибка с записью вызова; сокеты мимо urllib запрещены."""

    def setUp(self):
        super().setUp()
        self.net = []

        def no_open(director, fullurl, *a, **kw):
            self.net.append(getattr(fullurl, "full_url", fullurl))
            raise urllib.error.URLError("сеть в тестах выключена")

        def no_sock(*a, **kw):
            self.net.append(a[:1])
            raise OSError("сеть в тестах запрещена (запрос мимо urllib)")

        for target, attr, fake in ((urllib.request.OpenerDirector, "open", no_open),
                                   (socket, "create_connection", no_sock)):
            p = mock.patch.object(target, attr, fake)
            p.start()
            self.addCleanup(p.stop)
        self.kits = self.data / "fx" / "kits"

    def _install(self, name):
        return self.client.post("/fx/kits/install", params={"name": name})

    def _ok(self, name):
        r = self._install(name)
        self.assertEqual(r.status_code, 200, r.text)
        return r.json()

    def _files(self, kit):
        return sorted(p for p in (self.kits / kit).rglob("*") if p.is_file())

    def _resources(self):
        return self.real_fx_resources()


class TestMachineInstall(_SynthKitCase):
    """ТК91 и условие 56: установка драм-машины без сети."""

    def _check_layout(self, kit, body):
        import soundfile as sf
        self.assertEqual(body.get("name"), kit)
        self.assertIs(body.get("generated"), True)
        self.assertIs(body.get("downloaded"), False)
        self.assertEqual(body.get("parts"), {p: 8 for p in DRUM_PARTS})
        root = self.kits / kit
        self.assertEqual(sorted(p.name for p in root.iterdir() if p.is_dir()), sorted(DRUM_PARTS))
        for part in DRUM_PARTS:
            wavs = sorted((root / part).glob("*.wav"))
            self.assertEqual(len(wavs), 8, f"{kit}/{part}: слоёв не 8")
            for f in wavs:
                info = sf.info(str(f))
                self.assertEqual((info.samplerate, info.channels, info.subtype), (KIT_SR, 1, "PCM_16"),
                                 f"{kit}/{part}/{f.name}")

    def test_tr808_without_network(self):
        body = self._ok("tr808")
        self._check_layout("tr808", body)
        self.assertEqual(self.net, [], "установка драм-машины ходила в сеть")

    def test_all_machines_without_network(self):
        for kit in MACHINES:
            with self.subTest(kit=kit):
                self._check_layout(kit, self._ok(kit))
        self.assertEqual(self.net, [])

    def test_assets_list_generated_kit(self):
        self._ok("tr808")
        r = self.client.get("/fx/assets")
        self.assertEqual(r.status_code, 200, r.text)
        kits = {k["name"]: k["samples"] for k in r.json().get("kits", [])}
        for part in DRUM_PARTS:
            self.assertEqual(kits.get(f"tr808/{part}"), 8, part)

    def test_repeat_not_regenerated(self):
        self._ok("tr808")
        files = self._files("tr808")
        self.assertTrue(files)
        for f in files:
            os.utime(f, ns=(OLD_NS, OLD_NS))
        body = self._ok("tr808")
        self.assertIs(body.get("generated"), False)
        self.assertIs(body.get("downloaded"), False)
        self.assertEqual(body.get("parts"), {p: 8 for p in DRUM_PARTS})
        self.assertEqual(self._files("tr808"), files, "состав файлов изменился")
        for f in files:
            self.assertEqual(f.stat().st_mtime_ns, OLD_NS, f"{f.name} перезаписан")
        self.assertEqual(self.net, [])

    def test_unknown_kit_422(self):
        for name in ("tr808x", "TR808", "../tr808", "tr808/kick", "synthbass/moog", ""):
            with self.subTest(name=name):
                self.assertEqual(self._install(name).status_code, 422)
        self.assertFalse((self.kits / "tr808x").exists())
        self.assertEqual(self.net, [])


class TestSamplerTr808(_SynthKitCase):
    """ТК91: sampler kit «tr808/kick» на дорожке с 4 щелчками."""

    SR = 44100
    CLICKS = (0.5, 1.5, 2.5, 3.5)

    def _clicks(self):
        import numpy as np
        n = int(4.5 * self.SR)
        x = np.zeros(n)
        k = np.arange(int(0.01 * self.SR))
        burst = np.exp(-k / (0.0015 * self.SR)) * np.cos(2 * np.pi * 1000 * k / self.SR)
        at = []
        for t in self.CLICKS:
            i = int(t * self.SR)
            x[i:i + len(burst)] += 0.5 * burst
            at.append(i + int(np.argmax(np.abs(burst))))
        return np.stack([x, x], axis=1).astype(np.float32), at

    def test_four_hits_on_clicks(self):
        import numpy as np
        import fx_engine
        self._ok("tr808")
        x, at = self._clicks()
        y = fx_engine.process(x, self.SR, [{"type": "sampler", "kit": "tr808/kick"}], self._resources())
        self.assertEqual(y.shape, x.shape)
        m = np.abs(np.asarray(y, dtype=np.float64)).mean(axis=1)
        top = float(m.max())
        self.assertGreater(top, 0.0, "выход пустой")
        tol = int(0.002 * self.SR)
        # до первого щелчка — тишина (лишнего удара нет); 30 мс перед ним — атака сэмпла до его пика
        # (пик сэмпла — в первых 25 мс, ТК90, и ставится на пик удара)
        self.assertLess(float(m[:at[0] - int(0.03 * self.SR)].max(initial=0.0)), 0.01 * top,
                        "звук до первого щелчка")
        # удар на месте каждого щелчка: пик звука в окне щелчка — там же ±2 мс, и он громкий
        for i, t in enumerate(at):
            with self.subTest(click=i):
                a, b = t - int(0.05 * self.SR), t + int(0.05 * self.SR)
                j = a + int(np.argmax(m[a:b]))
                self.assertLessEqual(abs(j - t), tol, f"удар {i}: {1000 * (j - t) / self.SR:+.1f} мс")
                self.assertGreater(float(m[j]), 0.3 * top, f"удар {i} тихий или пропущен")
                # перед ударом (60…5 мс) — нет своего удара: звук там тише пика на месте щелчка
                pre = m[t - int(0.06 * self.SR):t - int(0.005 * self.SR)]
                self.assertLess(float(pre.max()), float(m[j]), f"лишний удар перед щелчком {i}")
        self.assertEqual(self.net, [])


class TestSynthBassInstall(_SynthKitCase):
    """Условие 58: synthbass — та же установка без сети; ТК92 — блок bass с synthbass/sub808."""

    def test_install_synthbass(self):
        import soundfile as sf
        body = self._ok("synthbass")
        self.assertEqual(body.get("name"), "synthbass")
        self.assertIs(body.get("generated"), True)
        self.assertIs(body.get("downloaded"), False)
        self.assertEqual(body.get("parts"), BASS_PARTS)
        for part, n in BASS_PARTS.items():
            wavs = sorted((self.kits / "synthbass" / part).glob("*.wav"))
            self.assertEqual(len(wavs), n, part)
            for f in wavs:
                info = sf.info(str(f))
                self.assertEqual((info.samplerate, info.channels, info.subtype), (KIT_SR, 1, "PCM_16"), f.name)
                self.assertAlmostEqual(info.duration, 2.5, delta=0.05)
        self.assertEqual(self.net, [])
        again = self._ok("synthbass")
        self.assertIs(again.get("generated"), False)
        self.assertEqual(again.get("parts"), BASS_PARTS)

    def _bass_out_hz(self, kit, hz_in, lo, hi):
        """Дорожка-синус hz_in восьмыми (120 BPM) через блок bass с kit → основной тон выхода (пик спектра lo…hi)."""
        import numpy as np
        import fx_engine
        self._ok("synthbass")
        sr, bpm, dur = 44100, 120, 8.0
        eighth = 60 / bpm / 2
        t = np.arange(int(dur * sr)) / sr
        x = np.zeros_like(t)
        for k in range(int(dur / eighth)):
            a, b = int(k * eighth * sr), int((k + 1) * eighth * sr)
            u = t[a:b] - t[a]
            # нота восьмой: быстрая атака, спад, глушение в конце
            g = np.minimum(1.0, u / 0.005) * np.exp(-u / 0.4) * np.minimum(1.0, (eighth - u) / 0.01)
            x[a:b] = 0.4 * g * np.sin(2 * np.pi * hz_in * u)
        stereo = np.stack([x, x], axis=1).astype(np.float32)
        chain = [{"type": "bass", "kit": kit, "division": 2, "floor_db": -20}]
        y = np.asarray(fx_engine.process(stereo, sr, chain, self._resources()), dtype=np.float64)
        self.assertEqual(y.shape, stereo.shape)
        m = y.mean(axis=1)
        self.assertGreater(float(np.sqrt(np.mean(m ** 2))), 1e-4, "выход пустой")
        seg = m[int(1.0 * sr):int(7.0 * sr)] * np.hanning(int(6.0 * sr))
        nfft = 1 << 20
        s = np.abs(np.fft.rfft(seg, n=nfft))
        f = np.fft.rfftfreq(nfft, 1 / sr)
        band = (f >= lo) & (f <= hi)
        self.assertEqual(self.net, [])
        return float(f[band][np.argmax(s[band])])

    def test_bass_block_sub808_on_e1_eighths(self):
        """ТК92: нижняя нота набора (E1) не уходит на октаву вверх."""
        hz = self._bass_out_hz("synthbass/sub808", 41.2, 20, 200)
        self.assertAlmostEqual(hz, 41.2, delta=1.5, msg=f"основной тон выхода {hz:.1f} Гц (ждали 41,2, не 82,4)")

    def test_bass_block_moog_on_g3_eighths(self):
        """ТК92: верхняя нота набора (G3) не уходит на октаву вниз."""
        hz = self._bass_out_hz("synthbass/moog", 196.0, 30, 300)
        self.assertAlmostEqual(hz, 196.0, delta=3.0, msg=f"основной тон выхода {hz:.1f} Гц (ждали 196, не 98)")


def _midi_hz(m):
    return 440.0 * 2 ** ((m - 69) / 12)


class _KitRes:
    """Подставное хранилище ресурсов (внешняя граница — файлы набора): kit(name) → (сэмплы, sr);
    kit_names(name) → имена сэмплов без расширения в том же порядке (как у ресурсов воркера)."""

    def __init__(self, name, samples, sr, names=None):
        self._name, self._samples, self._sr, self._names = name, samples, sr, names

    def kit(self, name):
        if name != self._name:
            raise KeyError(name)
        return list(self._samples), self._sr

    def kit_names(self, name):
        if name != self._name:
            raise KeyError(name)
        return list(self._names or [])


@unittest.skipUnless(fa._OK, fa._SKIP)
class TestBassKitNoteFolding(unittest.TestCase):
    """ТК92а (условие 58б): свёртка нот дорожки к диапазону набора баса.

    - набор без высот в именах (высота — замер по звуку), нижний сэмпл с дробной высотой
      MIDI 24,97: нота 49 (> 24,97 + 24) сворачивается на октаву вниз → тон выхода — нота 37;
    - набор с именами m28…m55 (высота из имени): нота 55 не сворачивается (тон 55),
      нота 56 — сворачивается (тон 44).
    """

    SR = 48000
    KIT = "test/fold"

    def _sample(self, midi):
        import numpy as np
        t = np.arange(int(2.5 * self.SR)) / self.SR
        env = np.minimum(1.0, t / 0.002) * np.exp(-t / 1.0)
        return (0.8 * env * np.sin(2 * np.pi * _midi_hz(midi) * t)).astype(np.float32)

    def _out_hz(self, res, midi_in):
        """Синус ноты midi_in восьмыми (120 BPM) через блок bass → пик спектра выхода 25…400 Гц."""
        import numpy as np
        import fx_engine
        sr, dur, eighth = self.SR, 8.0, 0.25
        hz_in = _midi_hz(midi_in)
        t = np.arange(int(dur * sr)) / sr
        x = np.zeros_like(t)
        for k in range(int(dur / eighth)):
            a, b = int(k * eighth * sr), int((k + 1) * eighth * sr)
            u = t[a:b] - t[a]
            g = np.minimum(1.0, u / 0.005) * np.exp(-u / 0.4) * np.minimum(1.0, (eighth - u) / 0.01)
            x[a:b] = 0.4 * g * np.sin(2 * np.pi * hz_in * u)
        stereo = np.stack([x, x], axis=1).astype(np.float32)
        chain = [{"type": "bass", "kit": self.KIT, "division": 2, "floor_db": -20}]
        y = np.asarray(fx_engine.process(stereo, sr, chain, res), dtype=np.float64)
        self.assertEqual(y.shape, stereo.shape)
        m = y.mean(axis=1)
        self.assertGreater(float(np.sqrt(np.mean(m ** 2))), 1e-4, "выход пустой")
        seg = m[int(1.0 * sr):int(7.0 * sr)] * np.hanning(int(6.0 * sr))
        nfft = 1 << 20
        s = np.abs(np.fft.rfft(seg, n=nfft))
        f = np.fft.rfftfreq(nfft, 1 / sr)
        band = (f >= 25) & (f <= 400)
        return float(f[band][np.argmax(s[band])])

    def _assert_note(self, hz, midi, why):
        # проверяется октава (свёрнута или нет), а не строй: допуск ±50 центов (ближе к ноте,
        # чем к соседней), октавная ошибка — 1200 центов
        import math
        cents = 1200 * math.log2(hz / _midi_hz(midi))
        self.assertLess(abs(cents), 50, f"{why}: тон выхода {hz:.1f} Гц, ждали ноту {midi} "
                                        f"({_midi_hz(midi):.1f} Гц), отклонение {cents:+.0f} центов")

    def test_unnamed_kit_fractional_low_folds_49_down(self):
        """Без высот в именах, lo = 24,97: нота 49 → тон ноты 37 (не 49)."""
        import fx_engine
        midis = (24.97, 36.97, 48.97)
        samples = [self._sample(m) for m in midis]
        names = ["growl-a", "growl-b", "growl-c"]          # имена высоты не знают
        real = fx_engine._kit_notes

        def exact_pitch(raw, ksr, *a, **kw):
            # замер yin синуса ~32,6 Гц ошибается на +0,7 полутона (24,97 → 25,69), поэтому
            # «замер» подменён точной высотой сэмпла; остальное (пик, атака, звук) — настоящее
            notes = real(raw, ksr, *a, **kw)
            self.assertEqual(len(notes), len(midis))
            self.assertTrue(all(not n[-1] for n in notes), "высота взята из имени, а в именах её нет")
            by_hz = sorted(range(len(notes)), key=lambda i: notes[i][0])
            out = list(notes)
            for rank, i in enumerate(by_hz):
                out[i] = (midis[rank],) + tuple(notes[i][1:])
            return out

        for label, res in (("имена без высот", _KitRes(self.KIT, samples, self.SR, names)),
                           ("пустые имена", _KitRes(self.KIT, samples, self.SR, []))):
            with self.subTest(res=label), mock.patch.object(fx_engine, "_kit_notes", side_effect=exact_pitch):
                self._assert_note(self._out_hz(res, 49), 37, "нота 49 не свернулась на октаву вниз")

    def _named_res(self):
        midis = range(28, 56)
        return _KitRes(self.KIT, [self._sample(m) for m in midis], self.SR, [f"m{m}" for m in midis])

    def test_named_kit_top_note_55_not_folded(self):
        """Имена m28…m55: нота 55 (верх набора) → тон 55."""
        self._assert_note(self._out_hz(self._named_res(), 55), 55, "нота 55 внутри набора, но свернулась")

    def test_named_kit_56_folds_to_44(self):
        """Имена m28…m55: нота 56 (выше верхнего) → тон 44."""
        self._assert_note(self._out_hz(self._named_res(), 56), 44, "нота 56 выше набора не свернулась вниз")


WORKER = Path(__file__).resolve().parent
DEPLOY = WORKER.parent / "deploy.sh"


def _imported_modules(path):
    """Имена модулей из import X / from X import … (на любом уровне вложенности, первый компонент)."""
    names = set()
    for node in ast.walk(ast.parse(path.read_text(encoding="utf-8"))):
        if isinstance(node, ast.Import):
            names.update(a.name.split(".")[0] for a in node.names)
        elif isinstance(node, ast.ImportFrom) and node.module and node.level == 0:
            names.add(node.module.split(".")[0])
    return names


def _deploy_scp_files():
    """worker/*-файлы из команд scp в deploy.sh (продолжения строк через \\ склеены)."""
    text = DEPLOY.read_text(encoding="utf-8").replace("\\\n", " ")
    files = set()
    for line in text.splitlines():
        if re.match(r"\s*scp\b", line):
            files.update(re.findall(r"worker/([\w.\-]+)", line))
    return files


class TestDeployShipsImports(unittest.TestCase):
    """Условие 58а: модули воркера, которые импортируют yue_worker.py и fx_engine.py, уезжают через deploy.sh."""

    def test_imported_worker_modules_in_scp(self):
        shipped = _deploy_scp_files()
        self.assertIn("yue_worker.py", shipped, "разбор deploy.sh не нашёл scp с файлами воркера")
        for src in ("yue_worker.py", "fx_engine.py"):
            for mod in sorted(_imported_modules(WORKER / src)):
                if not (WORKER / f"{mod}.py").is_file():
                    continue   # не модуль из worker/ (стандартная библиотека, пакеты)
                with self.subTest(src=src, module=mod):
                    self.assertIn(f"{mod}.py", shipped, f"{src} импортирует {mod}, а deploy.sh его не копирует")

    def test_drumsynth_shipped(self):
        """58а прямо: drumsynth.py — в списке deploy.sh."""
        self.assertIn("drumsynth.py", _deploy_scp_files())

if __name__ == "__main__":
    unittest.main()

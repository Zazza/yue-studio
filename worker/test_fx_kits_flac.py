"""Тесты карточки internal-own-track, этап 7в: условие 63 (тест-кейс ТК97) — наборы с файлами .flac
и набор фортепиано salamander.

Контракт (из карточки):
- наборы с файлами .flac: список (GET /fx/assets → kits [{name: "<набор>/<часть>", samples: N}]) и чтение
  набора (resources.kit / kit_names) учитывают *.flac наравне с *.wav;
- FX_KITS["salamander"] — Salamander Grand Piano V3, репозиторий sfzinstruments/SalamanderGrandPiano, версия
  закреплена (ref — коммит, 40 hex); части piano (слой силы v10) и piano-soft (v4): отбор файлов шаблоном —
  «A0v10.flac» в piano, «C4v4.flac» в piano-soft; соседние слои (v1, v4 / v14) — нет.

Хранилище — своё на тест (каталог данных из test_fx_api); наборы кладутся файлами, без установки и сети.
resources — настоящие ресурсы воркера (fx_resources читает файлы каталога данных).

Предположение (карточка не уточняет): в части с .flac и .wav вместе считаются оба; kit_names — имя файла
без расширения («A0v10»).

Запуск: cd worker && python3 -m unittest test_fx_kits_flac -v
"""
import re
import unittest

import test_fx_api as fa

KIT_SR = 48000


def _write(path, peak, fmt):
    import numpy as np
    import soundfile as sf
    path.parent.mkdir(parents=True, exist_ok=True)
    t = np.arange(int(0.2 * KIT_SR)) / KIT_SR
    sf.write(str(path), (peak * np.sin(2 * np.pi * 220 * t)).astype("float32"), KIT_SR, format=fmt,
             subtype="PCM_16")


@unittest.skipUnless(fa._OK, fa._SKIP)
class TestFlacKits(fa._FxApiCase):

    def setUp(self):
        super().setUp()
        self.kits = self.data / "fx" / "kits"

    def _put(self, kit, part, files):
        """files: {имя файла: пик}; формат — по расширению."""
        for name, peak in files.items():
            _write(self.kits / kit / part / name, peak, "FLAC" if name.endswith(".flac") else "WAV")

    def _assets(self):
        r = self.client.get("/fx/assets")
        self.assertEqual(r.status_code, 200, r.text)
        return r.json()

    def test_tc97_assets_list_flac_part(self):
        self._put("salamander", "piano", {"A0v10.flac": 0.3, "C1v10.flac": 0.4, "D#1v10.flac": 0.5})
        kits = self._assets().get("kits")
        self.assertEqual(sorted((k["name"], k["samples"]) for k in kits), [("salamander/piano", 3)])

    def test_assets_count_flac_and_wav_together(self):
        self._put("mykeys", "mixed", {"A3v10.flac": 0.3, "C4v10.flac": 0.4, "D#4v10.wav": 0.5})
        kits = self._assets().get("kits")
        self.assertEqual(sorted((k["name"], k["samples"]) for k in kits), [("mykeys/mixed", 3)])

    def test_tc97_resources_kit_reads_flac(self):
        import numpy as np
        peaks = {"A0v10.flac": 0.3, "C1v10.flac": 0.5, "D#1v10.flac": 0.7}
        self._put("salamander", "piano", peaks)
        res = self.real_fx_resources()
        samples, sr = res.kit("salamander/piano")
        self.assertEqual(sr, KIT_SR)
        self.assertEqual(len(samples), 3)
        names = res.kit_names("salamander/piano")
        self.assertEqual(sorted(names), ["A0v10", "C1v10", "D#1v10"])
        # имена — в том же порядке, что сэмплы: пик сэмпла совпадает с записанным под этим именем
        for name, s in zip(names, samples, strict=True):
            with self.subTest(name=name):
                self.assertAlmostEqual(float(np.abs(np.asarray(s)).max()), peaks[name + ".flac"], delta=0.01)

    def test_resources_kit_reads_flac_and_wav_together(self):
        self._put("mykeys", "mixed", {"A3v10.flac": 0.3, "D#4v10.wav": 0.5})
        res = self.real_fx_resources()
        samples, _ = res.kit("mykeys/mixed")
        self.assertEqual(len(samples), 2)
        self.assertEqual(sorted(res.kit_names("mykeys/mixed")), ["A3v10", "D#4v10"])

    def test_flac_kit_missing_still_key_error(self):
        res = self.real_fx_resources()
        with self.assertRaises(KeyError):
            res.kit("salamander/piano")


@unittest.skipUnless(fa._OK, fa._SKIP)
class TestSalamanderCatalog(fa._FxApiCase):

    def setUp(self):
        super().setUp()
        if "salamander" not in self.w.FX_KITS:
            self.fail("в FX_KITS нет набора salamander")
        self.spec = self.w.FX_KITS["salamander"]

    def test_tc97_salamander_pinned(self):
        self.assertEqual(self.spec["repo"], "sfzinstruments/SalamanderGrandPiano")
        self.assertRegex(self.spec["ref"], r"^[0-9a-f]{40}$", "версия набора не закреплена коммитом")

    def test_tc97_parts(self):
        self.assertEqual(sorted(self.spec["parts"]), ["piano", "piano-soft"])

    def test_tc97_piano_layer_v10(self):
        _, pattern = self.spec["parts"]["piano"]
        for name in ("A0v10.flac", "C4v10.flac", "F#4v10.flac", "C8v10.flac"):
            with self.subTest(name=name):
                self.assertIsNotNone(re.fullmatch(pattern, name), f"{name} не попадает в piano")
        for name in ("A0v4.flac", "A0v1.flac"):
            with self.subTest(name=name):
                self.assertIsNone(re.fullmatch(pattern, name), f"{name} попадает в piano")

    def test_tc97_piano_soft_layer_v4(self):
        _, pattern = self.spec["parts"]["piano-soft"]
        for name in ("C4v4.flac", "A0v4.flac", "D#1v4.flac"):
            with self.subTest(name=name):
                self.assertIsNotNone(re.fullmatch(pattern, name), f"{name} не попадает в piano-soft")
        for name in ("C4v14.flac", "C4v10.flac", "C4v1.flac"):
            with self.subTest(name=name):
                self.assertIsNone(re.fullmatch(pattern, name), f"{name} попадает в piano-soft")


if __name__ == "__main__":
    unittest.main()

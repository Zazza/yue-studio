"""Тесты карточки internal-own-track, условие 60 (тест-кейс ТК94): блок tape не громче входа.

Контракт из карточки:
- насыщение tape: тихий сигнал — без изменения уровня, громкие пики сжимаются;
- saturation 0 — без насыщения (выход = вход после среза верха, линейно);
- ТК94: wow 0, flutter 0, hiss 0, lowpass_hz 20000, saturation 0,5:
  синус 440 Гц −40 дБFS → уровень выхода −40 ±0,3 дБ; синус 0 дБFS → пик выхода < 0,6;
  saturation 0 → выход = вход после среза верха (±1e-6 к тому же без насыщения).
Параметры — worker/fx_blocks.json (wow, flutter, saturation, lowpass_hz, hiss).

Написаны по карточке, без чтения реализации. Внешних границ нет — моков нет.
Запуск: cd worker && python3 -m unittest test_fx_tape_level -v
"""
import unittest

try:
    import numpy as np
    import scipy  # noqa: F401
    _HAS_DEPS = True
except ImportError:
    _HAS_DEPS = False

_SKIP = "нужны numpy/scipy (окружение воркера)"

SR = 48000
FREQ = 440.0


def _fx():
    import fx_engine
    return fx_engine


def _sine(dbfs, seconds=2.0, ch=2):
    amp = 10 ** (dbfs / 20)
    t = np.arange(int(SR * seconds)) / SR
    s = amp * np.sin(2 * np.pi * FREQ * t)
    return np.tile(s[:, None], (1, ch)).astype(np.float64)


def _tape(saturation):
    return [{"type": "tape", "wow": 0, "flutter": 0, "hiss": 0,
             "lowpass_hz": 20000, "saturation": saturation}]


def _mid(y):
    """Середина без краёв (переходные процессы фильтра)."""
    y = np.asarray(y, dtype=np.float64)
    n = len(y)
    return y[n // 4: 3 * n // 4]


def _rms_db(y):
    return 20 * np.log10(np.sqrt(np.mean(np.square(y))) + 1e-30)


@unittest.skipUnless(_HAS_DEPS, _SKIP)
class TapeLevelTest(unittest.TestCase):
    def test_quiet_sine_keeps_level_with_saturation(self):
        x = _sine(-40)
        y = _fx().process(x, SR, _tape(0.5))
        self.assertEqual(np.asarray(y).shape, x.shape)
        diff = _rms_db(_mid(y)) - _rms_db(_mid(x))
        self.assertLessEqual(abs(diff), 0.3, f"тихий синус сдвинут на {diff:+.2f} дБ")

    def test_loud_sine_peaks_compressed(self):
        x = _sine(0)
        y = _fx().process(x, SR, _tape(0.5))
        peak = float(np.max(np.abs(_mid(y))))
        self.assertLess(peak, 0.6, f"пик {peak:.3f} не сжат")
        self.assertGreater(peak, 0.0)

    def test_saturation_zero_is_linear(self):
        """saturation 0 — без насыщения: громкий и тихий (×1/64, точно в float) дают одну форму."""
        loud = _sine(0)
        quiet = loud / 64.0
        fx = _fx()
        y_loud = np.asarray(fx.process(loud, SR, _tape(0)), dtype=np.float64)
        y_quiet = np.asarray(fx.process(quiet, SR, _tape(0)), dtype=np.float64)
        np.testing.assert_allclose(y_quiet * 64.0, y_loud, atol=1e-6)

    def test_saturation_zero_output_equals_input_at_440(self):
        """Срез верха 20 кГц не трогает 440 Гц: выход ≈ вход, громкий пик не сжат."""
        for db in (0, -40):
            x = _sine(db)
            y = _fx().process(x, SR, _tape(0))
            diff = _rms_db(_mid(y)) - _rms_db(_mid(x))
            self.assertLessEqual(abs(diff), 0.1, f"{db} дБFS: уровень сдвинут на {diff:+.2f} дБ")
        peak = float(np.max(np.abs(_mid(_fx().process(_sine(0), SR, _tape(0))))))
        self.assertGreater(peak, 0.95, f"saturation 0 сжал пик до {peak:.3f}")

    def test_silence_stays_silent(self):
        x = np.zeros((SR, 2))
        y = _fx().process(x, SR, _tape(0.5))
        self.assertEqual(float(np.max(np.abs(y))), 0.0)


# --- условие 60а, ТК94а: версия движка в ключе кэша превью /fx -----------------------
# Превью /fx цепочки tape дважды → один файл; ENGINE_VERSION + 1 (подмена) → другой файл
# (имя отличается); повтор при новой версии → тот же новый файл.
# Мок — только версия движка (граница «правка звучания блоков»), свои функции не мокаются.

try:
    import test_fx_api as _fa
    _API_OK, _API_SKIP = _fa._OK, _fa._SKIP
    _ApiBase = _fa._FxApiCase
except Exception as e:  # окружение без fastapi/httpx — пропуск, как у test_fx_api
    _API_OK, _API_SKIP = False, f"нет окружения воркера для API: {e}"
    _ApiBase = unittest.TestCase


@unittest.skipUnless(_API_OK, _API_SKIP)
class TapePreviewCacheEngineVersionTest(_ApiBase):
    """ТК94а: версия движка входит в ключ кэша превью /fx."""

    def _preview(self, jid):
        r = self._fx(jid, source="mix", output="mix", chain=_tape(0.5), preview=True,
                     **{"from": 1.0, "to": 2.0})
        self.assertEqual(r.status_code, 200, r.text)
        return r.json()["file"]

    def test_tc94a_engine_version_bump_changes_preview_file(self):
        from unittest import mock
        fx = _fx()
        old = fx.ENGINE_VERSION  # нет атрибута → красный по условию 60а
        self.assertIsInstance(old, int)
        jid, d = self._audio_job()

        first = self._preview(jid)
        again = self._preview(jid)
        self.assertEqual(again, first, "та же версия и запрос — тот же файл")
        self.assertTrue((d / first).exists())

        with mock.patch.object(fx, "ENGINE_VERSION", old + 1, create=False):
            bumped = self._preview(jid)
            self.assertNotEqual(bumped, first,
                                "превью до правки движка отдано из кэша при новой версии")
            self.assertTrue((d / bumped).exists())
            bumped_again = self._preview(jid)
            self.assertEqual(bumped_again, bumped, "повтор при новой версии — тот же новый файл")


if __name__ == "__main__":
    unittest.main()

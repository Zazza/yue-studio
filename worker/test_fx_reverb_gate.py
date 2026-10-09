"""Тесты карточки internal-own-track, условие 57а (тест-кейс ТК93а): гейт-реверб без гейта по уровню.

Контракт из карточки:
- у блока reverb параметр gate_ms (0 — выкл, 50…1000): отклик реверба после gate_ms обрывается (спад 5 мс),
  до него — почти ровный; уровень входа не важен (обработка линейна);
- ТК93а: импульс через reverb gate_ms 250, decay 2, wet 1: энергия хвоста после 260 мс ≤ −60 дБ от энергии
  0…250 мс; без gate_ms — после 260 мс есть хвост (≥ −20 дБ); вход ×0,01 → выход ×0,01 (линейно).

Предположение (карточка не уточняет, от чего отсчитывается gate_ms — от звука или от начала хвоста после
предзадержки): в тесте predelay_ms 0, тогда оба прочтения совпадают. Значения вне 0 и 50…1000 —
ChainError, как у остальных параметров с «0 — выкл» (fx_blocks.json).

Написаны по карточке, без чтения реализации. Внешних границ нет — моков нет.
Запуск: cd worker && python3 -m unittest test_fx_reverb_gate -v
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
DUR_S = 3.0


def _fx():
    import fx_engine
    return fx_engine


def _impulse(amp=1.0, ch=2):
    x = np.zeros((int(DUR_S * SR), ch))
    x[0, :] = amp
    return x


def _reverb(**extra):
    blk = {"type": "reverb", "decay_s": 2, "wet": 1, "predelay_ms": 0}
    blk.update(extra)
    return [blk]


def _energy(y, a_s, b_s=None):
    y = np.asarray(y, dtype=np.float64)
    a = int(round(a_s * SR))
    b = len(y) if b_s is None else int(round(b_s * SR))
    return float(np.sum(np.square(y[a:b])))


def _tail_db(y):
    """Энергия после 260 мс относительно энергии 0…250 мс, дБ."""
    head = _energy(y, 0.0, 0.250)
    tail = _energy(y, 0.260)
    return 10 * np.log10(max(tail, 1e-300) / max(head, 1e-300))


@unittest.skipUnless(_HAS_DEPS, _SKIP)
class ReverbGateTest(unittest.TestCase):
    def test_gate_250_cuts_tail(self):
        x = _impulse()
        y = _fx().process(x, SR, _reverb(gate_ms=250))
        self.assertEqual(np.asarray(y).shape, x.shape)
        db = _tail_db(y)
        self.assertLessEqual(db, -60.0, f"хвост после 260 мс {db:.1f} дБ — не обрезан")

    def test_gate_keeps_response_before_cut(self):
        """До gate_ms отклик есть (реверб звучит, а не глушится целиком)."""
        y = np.asarray(_fx().process(_impulse(), SR, _reverb(gate_ms=250)), dtype=np.float64)
        wet = _energy(y, 0.150, 0.240)
        self.assertGreater(wet, 1e-6, "отклик реверба до gate_ms пуст")

    def test_without_gate_tail_rings(self):
        y = _fx().process(_impulse(), SR, _reverb())
        db = _tail_db(y)
        self.assertGreaterEqual(db, -20.0, f"без gate_ms хвост после 260 мс {db:.1f} дБ — его нет")

    def test_gate_zero_is_off(self):
        fx = _fx()
        a = np.asarray(fx.process(_impulse(), SR, _reverb(gate_ms=0)))
        b = np.asarray(fx.process(_impulse(), SR, _reverb()))
        np.testing.assert_array_equal(a, b)

    def test_linear_in_input_level(self):
        """Уровень входа не важен: вход ×0,01 → выход ×0,01 (тихие части не глушатся)."""
        fx = _fx()
        loud = np.asarray(fx.process(_impulse(1.0), SR, _reverb(gate_ms=250)), dtype=np.float64)
        quiet = np.asarray(fx.process(_impulse(0.01), SR, _reverb(gate_ms=250)), dtype=np.float64)
        self.assertGreater(float(np.abs(quiet).max()), 0.0, "тихий вход заглушен")
        np.testing.assert_allclose(quiet * 100.0, loud, rtol=1e-4, atol=1e-5)

    def test_quiet_input_keeps_gated_shape(self):
        """Тихий вход (−40 дБ) обрезается так же, как громкий, и не исчезает."""
        y = np.asarray(_fx().process(_impulse(0.01), SR, _reverb(gate_ms=250)), dtype=np.float64)
        self.assertGreater(_energy(y, 0.150, 0.240), 1e-10, "тихий вход: отклик до gate_ms пуст")
        self.assertLessEqual(_tail_db(y), -60.0)

    def test_gate_range(self):
        fx = _fx()
        for ok in (0, 50, 1000):
            with self.subTest(ok=ok):
                fx.parse_chain(_reverb(gate_ms=ok))
        for bad in (10, 49, 1001, -1, "250", True):
            with self.subTest(bad=bad), self.assertRaises(fx.ChainError):
                fx.parse_chain(_reverb(gate_ms=bad))


@unittest.skipUnless(_HAS_DEPS, _SKIP)
class ReverbGateBeforePredelayTest(unittest.TestCase):
    """Условие 57б, ТК93б: gate_ms ≤ predelay_ms — отклик обрывается раньше, чем начался:
    реверб не звучит, выход = вход (±1e-9)."""

    def test_gate_before_predelay_is_dry(self):
        x = _impulse()
        y = np.asarray(_fx().process(x, SR, _reverb(gate_ms=50, predelay_ms=200)), dtype=np.float64)
        self.assertEqual(y.shape, x.shape)
        np.testing.assert_allclose(y, x, rtol=0, atol=1e-9)


if __name__ == "__main__":
    unittest.main()

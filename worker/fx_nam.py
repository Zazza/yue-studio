"""Усилитель NAM (Neural Amp Modeler) для звукового движка: загрузка захвата .nam
и замер его задержки. Пакет neural-amp-modeler 0.12.2 (MIT; читает .nam v0.5),
ставится без зависимостей — torch уже есть у воркера. torch импортируется только
в load_nam: measure_latency — чистый numpy (тесты без torch)."""
from __future__ import annotations

import json
from pathlib import Path

import numpy as np

DEFAULT_SR = 48000      # старые .nam без sample_rate обучены на 48 кГц
CLICK_AMP = 0.5         # щелчок для замера: громкий, но не в упор
CLICK_LEN_S = 0.25


def measure_latency(fn, sr: int) -> int:
    """Сдвиг модели в сэмплах: щелчок посередине тишины → позиция пика |выхода| минус
    позиция щелчка. Щелчком, не корреляцией: на повторяющемся риффе корреляция врёт."""
    n = max(int(sr * CLICK_LEN_S), 64)
    pos = n // 2
    x = np.zeros(n, dtype=np.float32)
    x[pos] = CLICK_AMP
    y = np.asarray(fn(x), dtype=np.float64).reshape(-1)
    return int(np.argmax(np.abs(y))) - pos


class NamAmp:
    """Захват NAM как AmpModel движка: model(x 1-D float32) → 1-D той же длины."""

    def __init__(self, model, sr: int, device: str, latency: int = 0):
        self._model = model
        self.sr = sr
        self.device = device
        self.latency = latency

    def __call__(self, x: np.ndarray) -> np.ndarray:
        import torch
        with torch.no_grad():
            t = torch.from_numpy(np.ascontiguousarray(x, dtype=np.float32)).to(self.device)
            y = self._model(t).float().cpu().numpy().reshape(-1)
        if len(y) != len(x):  # модель обязана сохранять длину; на всякий случай — подгонка
            y = np.pad(y, (0, max(0, len(x) - len(y))))[:len(x)]
        return y

    def close(self) -> None:
        """Освободить видеопамять (модель грузится на вызов)."""
        self._model = None
        try:
            import torch
            if torch.cuda.is_available():
                torch.cuda.empty_cache()
        except ImportError:
            pass


def load_nam(path: str | Path, latency: int | None = None) -> NamAmp:
    """Захват .nam → NamAmp на GPU (если есть). latency=None — замерить щелчком."""
    import torch
    from nam.models._from_nam import init_from_nam  # neural-amp-modeler 0.12.2
    cfg = json.loads(Path(path).read_text())
    dev = "cuda" if torch.cuda.is_available() else "cpu"
    model = init_from_nam(cfg).eval().to(dev)
    sr = int(cfg.get("sample_rate") or DEFAULT_SR)
    amp = NamAmp(model, sr, dev)
    amp.latency = measure_latency(amp, sr) if latency is None else int(latency)
    return amp

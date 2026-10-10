"""Поставить все наборы сэмплов каталога FX_KITS заранее (установка воркера, make worker): приложение потом не ждёт
скачивания. Уже стоящие части — без сети; синтезируемые (драм-машины, синт-басы) — считаются на месте.

    ~/yue/.venv/bin/python kits_install.py           # все наборы
    ~/yue/.venv/bin/python kits_install.py osdk vsco-violin

Код выхода 1 — какой-то набор не поставился (остальные поставлены); повторный запуск докачивает.
"""
from __future__ import annotations

import sys
import threading
import time

from fastapi import HTTPException

import yue_worker
from yue_worker import FX_KITS, fx_kit_install


def _size_mb(name: str) -> float:
    d = yue_worker._kits_dir() / name
    return sum(f.stat().st_size for f in d.rglob("*") if f.is_file()) / 1e6 if d.is_dir() else 0.0


def _watch(name: str, stop: threading.Event) -> None:
    """Прогресс по файлам, пока ставится набор: «часть ens: 12/45 файлов, 18 МБ» (строка — при каждом изменении)."""
    last = None
    while not stop.wait(1.0):
        p = dict(getattr(yue_worker, "_kit_progress", {}).get(name) or {})
        line = (f"    часть {p.get('part')}: {p.get('done', 0)}/{p.get('total', 0)} файлов, "
                f"{(p.get('bytes') or 0) / 1e6:.0f} МБ") if p else None
        if line and line != last:
            print(line, flush=True)
            last = line


def main(argv=None) -> int:
    names = list(argv if argv is not None else sys.argv[1:]) or list(FX_KITS)
    failed, total_mb = [], 0.0
    for i, name in enumerate(names, 1):
        t = time.time()
        print(f"[{i}/{len(names)}] {name} …", flush=True)
        stop = threading.Event()
        watcher = threading.Thread(target=_watch, args=(name, stop), daemon=True)
        watcher.start()
        try:
            r = fx_kit_install(name)
        except HTTPException as e:
            failed.append(name)
            print(f"    не поставлен: {e.detail}", flush=True)
            continue
        except OSError as e:   # диск, права — следующий набор всё равно пробуем
            failed.append(name)
            print(f"    не поставлен: {e}", flush=True)
            continue
        finally:
            stop.set()
            watcher.join()
        mb = _size_mb(name)
        total_mb += mb
        parts = ", ".join(f"{p} {n}" for p, n in (r.get("parts") or {}).items())
        how = "скачан" if r.get("downloaded") else "уже стоит"
        print(f"    {how}: {parts} — {mb:.0f} МБ, {time.time() - t:.0f} с", flush=True)
    print(f"Наборов: {len(names) - len(failed)} из {len(names)}, {total_mb:.0f} МБ"
          + (f"; не поставлены: {', '.join(failed)} (запустите ещё раз — докачает)" if failed else ""), flush=True)
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())

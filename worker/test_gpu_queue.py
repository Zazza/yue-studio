"""Тесты очереди к GPU (gpu_queue/_gpu_lock в yue_worker).

Запуск: python3 -m unittest worker.test_gpu_queue (из корня репозитория)
или: cd worker && python3 -m unittest test_gpu_queue

Импорт yue_worker создаёт каталоги данных и БД — уводим их во временную
папку через YUE_DATA_DIR до импорта. GPU в тестах не трогаем: проверяем
только сам лок (взаимоисключение, реентерабельность, освобождение при ошибке).
"""
import os
import tempfile
import threading
import time
import unittest
from pathlib import Path

_tmp = tempfile.TemporaryDirectory(prefix="yue-gpuq-test-")
os.environ["YUE_DATA_DIR"] = str(Path(_tmp.name) / "data")

import yue_worker  # noqa: E402


class TestGpuQueue(unittest.TestCase):
    def test_mutual_exclusion(self):
        """Два потока под gpu_queue не бывают внутри одновременно."""
        active = 0
        peak = 0
        bad = []

        def worker(n):
            nonlocal active, peak
            with yue_worker.gpu_queue(f"t{n}"):
                active += 1
                peak = max(peak, active)
                if active > 1:
                    bad.append(n)
                time.sleep(0.05)
                active -= 1

        ts = [threading.Thread(target=worker, args=(i,)) for i in range(4)]
        for t in ts:
            t.start()
        for t in ts:
            t.join()
        self.assertEqual(bad, [])
        self.assertEqual(peak, 1)

    def test_lock_released_on_error(self):
        """Исключение внутри секции не оставляет лок занятым."""
        with self.assertRaises(RuntimeError):
            with yue_worker.gpu_queue("boom"):
                raise RuntimeError("x")
        self.assertTrue(yue_worker._gpu_lock.acquire(blocking=False),
                        "лок не освобождён после исключения")
        yue_worker._gpu_lock.release()

    def test_reentrant(self):
        """RLock: вложенные gpu_queue в том же потоке не дедлочат."""
        with yue_worker.gpu_queue("outer"):
            got = yue_worker._gpu_lock.acquire(timeout=1)
            self.assertTrue(got, "лок не реентерабелен")
            yue_worker._gpu_lock.release()
            with yue_worker.gpu_queue("inner"):
                pass

    def test_fifo_wait_is_visible(self):
        """Ожидание за занятым локом действительно происходит (второй ждёт первого)."""
        order = []
        first = threading.Event()

        def slow():
            with yue_worker.gpu_queue("slow"):
                order.append("slow")
                first.set()
                time.sleep(0.15)

        def fast():
            first.wait()
            with yue_worker.gpu_queue("fast"):
                order.append("fast")

        t1, t2 = threading.Thread(target=slow), threading.Thread(target=fast)
        t1.start()
        t2.start()
        t1.join()
        t2.join()
        self.assertEqual(order, ["slow", "fast"])

    def test_waiters_counter(self):
        """_gpu_waiters: растёт, пока поток ждёт лок, и возвращается к нулю."""
        held = threading.Event()
        let_go = threading.Event()
        waiter_started = threading.Event()
        seen = []

        def holder():
            with yue_worker.gpu_queue("holder"):
                held.set()
                let_go.wait(2)

        def waiter():
            held.wait(1)
            waiter_started.set()
            with yue_worker.gpu_queue("waiter"):
                pass

        t1, t2 = threading.Thread(target=holder), threading.Thread(target=waiter)
        t1.start()
        t2.start()
        waiter_started.wait(1)
        time.sleep(0.05)
        seen.append(yue_worker._gpu_waiters)
        let_go.set()
        t1.join(2)
        t2.join(2)
        seen.append(yue_worker._gpu_waiters)
        self.assertGreaterEqual(seen[0], 1, "ожидающий не посчитан")
        self.assertEqual(seen[1], 0, "счётчик не обнулился после очереди")


if __name__ == "__main__":
    unittest.main()

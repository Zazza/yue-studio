"""Тесты карточки internal-own-track, этап 14б — находки кросс-ревью по условиям 116–118.
Написаны по карточке, без чтения реализации.

- 117 при нескольких установках: GET /fx/kits/progress отдаёт прогресс ИДУЩЕЙ установки. Две
  установки разных наборов идут одновременно; одна закончилась — прогресс не пустой и относится
  ко второй (общий на все установки слот, который первая очищает, — ошибка).
- 116 между процессами: один набор ставят два процесса сразу (приложение и kits_install.py из
  deploy.sh). Внутрипроцессный замок здесь не помогает, поэтому он подменён новым замком на каждый
  вызов (_fx_kits_guard_for) — как будто вызовы из разных процессов. Оба вызова должны пройти,
  набор — стоять целиком, каждый файл — скачан не больше одного раза.
- 117 для синтезируемых наборов (tr808): во время установки прогресс есть — name «tr808», total > 0,
  done растёт; после — {}. Замер — шпион на drumsynth.render (вызывает настоящий render), на каждом
  вызове читает GET /fx/kits/progress.
- 117, «идущая установка» при двух: набор A из двух частей ставится, позже начинается B и ждёт в сети;
  A переходит ко второй части — GET /fx/kits/progress продолжает отдавать B (последняя начатая
  установка), а не A. Правило «последняя начатая» — уточнение оркестратора к условию 117.
- 118: kits_install печатает прогресс по частям и файлам, пока идёт установка набора
  («… <done>/<total> файлов …»).

Внешние границы: сеть (_http_get воркера), список закреплённых файлов (_kit_pinned_files),
хранилище — своё на тест (<data> из _FxApiCase). time.sleep НЕ подменяется: межпроцессная
защита может ждать опросом.

Допущения (сверить с постановщиком, если тест красный не по делу):
- межпроцессная защита видна между потоками одного процесса, если у потоков разные внутрипроцессные
  замки (flock на отдельном открытом файле — да; POSIX lockf — нет, он у процесса общий);
- kits_install.main(argv) с именами наборов ставит только их; прогресс берёт из
  yue_worker._kit_progress ({имя набора: {name, part, done, total, bytes}}) и печатает его
  не реже раза в 2 с.

Запуск: cd worker && python3 -m unittest test_kit_cross -v
"""
import contextlib
import io
import threading
import time
import unittest
from unittest import mock

try:
    import test_fx_kits as fk
    _KitBase, _ApiBase = fk._KitCase, fk.fa._FxApiCase
    _OK = fk.fa._OK
except ImportError:   # окружение без fastapi и т.п. — классы ниже пропускаются
    _KitBase = _ApiBase = unittest.TestCase
    _OK = False

_SKIP = "нужно окружение воркера (fastapi/httpx/numpy/soundfile/librosa)"

REPO = "owner/kit-repo"
REF = "0123456789abcdef0123456789abcdef01234567"
RAW = f"https://raw.githubusercontent.com/{REPO}/{REF}/"
WAIT = 10.0   # предел ожидания событий и потоков: тест падает, а не висит


def _spec(folder):
    return {"repo": REPO, "ref": REF, "parts": {"main": (folder, r"F_.+\.wav")}, "max_s": 8.0}


@unittest.skipUnless(_OK, _SKIP)
class _CrossCase(_KitBase):
    """Наборы kit-a и kit-b (по части main, по 3 файла) в каталоге FX_KITS и закреплённом списке."""

    files = ["F_1.wav", "F_2.wav", "F_3.wav"]
    folders = {"kit-a": "A", "kit-b": "B"}

    def setUp(self):
        super().setUp()
        kits = {**self.w.FX_KITS, **{k: _spec(f) for k, f in self.folders.items()}}
        p = mock.patch.object(self.w, "FX_KITS", kits)
        p.start()
        self.addCleanup(p.stop)
        pinned = {k: {"main": list(self.files)} for k in self.folders}
        p = mock.patch.object(self.w, "_kit_pinned_files", return_value=pinned)
        p.start()
        self.addCleanup(p.stop)
        self.wav = fk._wav(0.5)
        self.lock = threading.Lock()
        self.calls = []            # (набор, файл) каждого запроса
        self.threads = []
        self.gates = []            # события, которые надо открыть при уборке, чтобы потоки не висли
        self.addCleanup(self._release)

    def _release(self):
        for g in self.gates:
            g.set()
        for t in self.threads:
            t.join(WAIT)

    def _whose(self, url):
        """(набор, файл) по raw-ссылке закреплённого списка."""
        for kit, folder in self.folders.items():
            for name in self.files:
                if url == f"{RAW}{folder}/{name}":
                    return kit, name
        raise AssertionError(f"неожиданный запрос: {url}")

    def _count(self, kit, name):
        with self.lock:
            return self.calls.count((kit, name))

    def _spawn(self, kit):
        """Поток с fx_kit_install(kit); результат или исключение — в словаре."""
        box = {}

        def run():
            try:
                box["result"] = self.w.fx_kit_install(kit)
            except BaseException as e:  # noqa: BLE001 — исключение потока проверяет тест
                box["error"] = e
        t = threading.Thread(target=run, daemon=True)
        self.threads.append(t)
        t.start()
        return t, box

    def _progress(self):
        r = self.client.get("/fx/kits/progress")
        self.assertEqual(r.status_code, 200, r.text)
        return r.json()

    def _wavs(self, kit):
        d = self.kits / kit / "main"
        return sorted(f.name for f in d.iterdir() if f.suffix == ".wav") if d.is_dir() else []


class TestProgressTwoInstalls(_CrossCase):
    """Условие 117: две одновременные установки — прогресс отдаёт идущую, а не пустоту."""

    def test_progress_follows_remaining_install(self):
        entered = {k: threading.Event() for k in self.folders}
        gate = {k: threading.Event() for k in self.folders}
        self.gates += list(gate.values())

        def fake_get(url, *a, **kw):
            kit, name = self._whose(url)
            with self.lock:
                self.calls.append((kit, name))
            if name == self.files[0]:      # первый файл набора ждёт своего разрешения
                entered[kit].set()
                gate[kit].wait(WAIT)
            return self.wav

        p = mock.patch.object(self.w, "_http_get", fake_get)
        p.start()
        self.addCleanup(p.stop)

        ta, box_a = self._spawn("kit-a")
        tb, box_b = self._spawn("kit-b")
        for k, e in entered.items():
            self.assertTrue(e.wait(WAIT), f"установка {k} не дошла до скачивания — "
                                          "две установки разных наборов не идут одновременно")

        both = self._progress()
        self.assertTrue(both, "во время двух установок прогресс пустой")
        self.assertIn(both.get("name"), self.folders, both)

        gate["kit-a"].set()                # первая доходит до конца, вторая ещё ждёт
        ta.join(WAIT)
        self.assertFalse(ta.is_alive(), "установка kit-a не закончилась")
        self.assertNotIn("error", box_a, box_a.get("error"))
        self.assertTrue(tb.is_alive())

        rest = self._progress()
        self.assertTrue(rest, "одна установка закончилась, вторая идёт — а прогресс пустой")
        self.assertEqual(rest.get("name"), "kit-b", rest)
        self.assertEqual(rest.get("part"), "main", rest)
        self.assertEqual(rest.get("total"), len(self.files), rest)

        gate["kit-b"].set()
        tb.join(WAIT)
        self.assertFalse(tb.is_alive(), "установка kit-b не закончилась")
        self.assertNotIn("error", box_b, box_b.get("error"))
        self.assertEqual(self._progress(), {}, "обе установки закончились — прогресс не сброшен")


class TestInstallAcrossProcesses(_CrossCase):
    """Условие 116: один набор ставят два «процесса» сразу — оба успешны, каждый файл один раз."""

    def test_same_kit_two_processes(self):
        kit = "kit-a"
        first_in = threading.Event()       # первый вызов начал качать первый файл
        second_in = threading.Event()      # второй вызов тоже добрался до сети
        gate = threading.Event()
        self.gates.append(gate)

        def fake_get(url, *a, **kw):
            k, name = self._whose(url)
            with self.lock:
                self.calls.append((k, name))
                n = self.calls.count((k, name))
            if name == self.files[0]:
                (first_in if n == 1 else second_in).set()
                gate.wait(WAIT)
            return self.wav

        p = mock.patch.object(self.w, "_http_get", fake_get)
        p.start()
        self.addCleanup(p.stop)
        # новый внутрипроцессный замок на каждый вызов — как будто вызовы из двух процессов
        p = mock.patch.object(self.w, "_fx_kits_guard_for", side_effect=lambda name: threading.Lock())
        p.start()
        self.addCleanup(p.stop)

        t1, box1 = self._spawn(kit)
        self.assertTrue(first_in.wait(WAIT), "первая установка не дошла до скачивания")
        t2, box2 = self._spawn(kit)
        # даём второму «процессу» время: без межпроцессной защиты он доберётся до сети
        second_in.wait(0.5)
        gate.set()
        t1.join(WAIT)
        t2.join(WAIT)
        self.assertFalse(t1.is_alive() or t2.is_alive(), "установка зависла")

        self.assertNotIn("error", box1, f"первый вызов упал: {box1.get('error')!r}")
        self.assertNotIn("error", box2, f"второй вызов упал: {box2.get('error')!r}")
        for name in self.files:
            self.assertLessEqual(self._count(kit, name), 1,
                                 f"{name} скачан {self._count(kit, name)} раза: {self.calls}")
        self.assertEqual(self._wavs(kit), sorted(self.files), "набор стоит не целиком")


DRUM_PARTS = ("kick", "snare", "hh-closed", "hh-open", "ride", "crash",
              "tom-small", "tom-medium", "tom-large", "clap", "rim", "cowbell")


@unittest.skipUnless(_OK, _SKIP)
class TestProgressSynthKit(_KitBase):
    """Условие 117: синтезируемый набор tr808 — прогресс во время установки есть и растёт, после — {}."""

    def _progress(self):
        r = self.client.get("/fx/kits/progress")
        self.assertEqual(r.status_code, 200, r.text)
        return r.json()

    def test_tr808_progress_during_synthesis(self):
        import drumsynth
        real = drumsynth.render
        seen = []

        def spy(*a, **kw):
            seen.append(self._progress())
            return real(*a, **kw)

        p = mock.patch.object(drumsynth, "render", spy)
        p.start()
        self.addCleanup(p.stop)
        # воркер мог взять функцию по имени (from drumsynth import render) — подменяем и такую ссылку
        for attr, val in list(vars(self.w).items()):
            if val is real:
                p = mock.patch.object(self.w, attr, spy)
                p.start()
                self.addCleanup(p.stop)

        self.assertEqual(self._progress(), {}, "до установки прогресс не пустой")
        self.w.fx_kit_install("tr808")
        self.assertTrue(seen, "drumsynth.render не вызывался — набор не синтезировался")

        mine = [s for s in seen if s.get("name") == "tr808"]
        self.assertTrue(mine, f"во время синтеза tr808 прогресс без name «tr808»: {seen[:3]}")
        self.assertEqual(len(mine), len(seen),
                         f"часть замеров во время синтеза — не про tr808: "
                         f"{[s for s in seen if s.get('name') != 'tr808'][:3]}")
        for s in mine:
            for key in ("part", "done", "total", "bytes"):
                self.assertIn(key, s, s)
            self.assertIn(s["part"], DRUM_PARTS, s)
            self.assertGreater(s["total"], 0, s)
            self.assertGreaterEqual(s["done"], 0, s)
            self.assertLessEqual(s["done"], s["total"], s)
        # в пределах части done не убывает; где-то он растёт
        grew = False
        for prev, cur in zip(mine, mine[1:], strict=False):
            if prev["part"] == cur["part"]:
                self.assertGreaterEqual(cur["done"], prev["done"], (prev, cur))
                grew = grew or cur["done"] > prev["done"]
        self.assertTrue(grew, f"done не растёт за установку: {[(s['part'], s['done']) for s in mine[:20]]}")
        self.assertGreater(len({s["part"] for s in mine}), 1, "прогресс не переходит по частям")
        self.assertEqual(self._progress(), {}, "после установки прогресс не сброшен")


class TestProgressLatestInstall(_CrossCase):
    """Условие 117: A (две части) ставится, позже начат B и ждёт; A переходит ко второй части —
    прогресс по-прежнему отдаёт B (последняя начатая установка)."""

    folders = {"kit-a": "A", "kit-b": "B"}
    parts_a = {"one": "A1", "two": "A2"}

    def setUp(self):
        super().setUp()
        spec_a = {"repo": REPO, "ref": REF, "max_s": 8.0,
                  "parts": {p: (f, r"F_.+\.wav") for p, f in self.parts_a.items()}}
        kits = {**self.w.FX_KITS, "kit-a": spec_a, "kit-b": _spec(self.folders["kit-b"])}
        p = mock.patch.object(self.w, "FX_KITS", kits)
        p.start()
        self.addCleanup(p.stop)
        pinned = {"kit-a": {p: list(self.files) for p in self.parts_a},
                  "kit-b": {"main": list(self.files)}}
        p = mock.patch.object(self.w, "_kit_pinned_files", return_value=pinned)
        p.start()
        self.addCleanup(p.stop)

    def _where(self, url):
        """(набор, каталог, файл) по raw-ссылке."""
        dirs = {**{f: "kit-a" for f in self.parts_a.values()}, self.folders["kit-b"]: "kit-b"}
        for folder, kit in dirs.items():
            for name in self.files:
                if url == f"{RAW}{folder}/{name}":
                    return kit, folder, name
        raise AssertionError(f"неожиданный запрос: {url}")

    def test_progress_stays_on_latest_started(self):
        a_in, b_in = threading.Event(), threading.Event()
        gate_a, gate_b = threading.Event(), threading.Event()
        self.gates += [gate_a, gate_b]
        state = {"first_folder": None}
        second_part = []           # прогресс, прочитанный, пока A качает вторую часть

        def fake_get(url, *a, **kw):
            kit, folder, name = self._where(url)
            if kit == "kit-b":
                if name == self.files[0]:
                    b_in.set()
                    gate_b.wait(WAIT)
                return self.wav
            if state["first_folder"] is None:
                state["first_folder"] = folder
            if folder == state["first_folder"]:
                if name == self.files[0]:
                    a_in.set()
                    gate_a.wait(WAIT)
            else:                  # A уже во второй части
                second_part.append(self._progress())
            return self.wav

        p = mock.patch.object(self.w, "_http_get", fake_get)
        p.start()
        self.addCleanup(p.stop)

        ta, box_a = self._spawn("kit-a")
        self.assertTrue(a_in.wait(WAIT), "установка kit-a не дошла до скачивания")
        self.assertEqual(self._progress().get("name"), "kit-a")
        tb, box_b = self._spawn("kit-b")
        self.assertTrue(b_in.wait(WAIT), "установка kit-b не началась, пока идёт kit-a")
        self.assertEqual(self._progress().get("name"), "kit-b", "начата kit-b — прогресс не о ней")

        gate_a.set()
        ta.join(WAIT)
        self.assertFalse(ta.is_alive(), "установка kit-a не закончилась")
        self.assertNotIn("error", box_a, box_a.get("error"))
        self.assertEqual(len(second_part), len(self.files), "kit-a не качала вторую часть")
        for s in second_part:
            self.assertEqual(s.get("name"), "kit-b",
                             f"kit-a перешла ко второй части и перехватила прогресс у kit-b: {s}")
            self.assertEqual(s.get("part"), "main", s)

        rest = self._progress()
        self.assertEqual(rest.get("name"), "kit-b", f"kit-a закончилась, kit-b идёт: {rest}")
        gate_b.set()
        tb.join(WAIT)
        self.assertFalse(tb.is_alive(), "установка kit-b не закончилась")
        self.assertNotIn("error", box_b, box_b.get("error"))
        self.assertEqual(self._progress(), {}, "обе установки закончились — прогресс не сброшен")


@unittest.skipUnless(_OK, _SKIP)
class TestKitsInstallProgress(_ApiBase):
    """Условие 118: kits_install печатает прогресс по файлам, пока набор ставится."""

    def test_prints_files_progress_during_install(self):
        import kits_install
        entry = {"x": {"name": "x", "part": "p", "done": 3, "total": 7, "bytes": 5_000_000}}
        progress = self.w._kit_progress

        def fake_install(name="", *a, **kw):
            progress.update(entry)
            try:
                time.sleep(2.2)
            finally:
                progress.pop("x", None)
            return {"name": name, "parts": {"p": 7}, "downloaded": True}

        kits = {**self.w.FX_KITS, "x": _spec("X")}
        patches = [mock.patch.object(self.w, "fx_kit_install", fake_install),
                   mock.patch.object(self.w, "FX_KITS", kits)]
        for attr, val in (("fx_kit_install", fake_install), ("FX_KITS", kits)):
            if hasattr(kits_install, attr):
                patches.append(mock.patch.object(kits_install, attr, val))
        out = io.StringIO()
        with contextlib.ExitStack() as st:
            for p in patches:
                st.enter_context(p)
            st.enter_context(contextlib.redirect_stdout(out))
            code = kits_install.main(["x"])
        text = out.getvalue()
        self.assertEqual(code, 0, text)
        lines = [ln for ln in text.splitlines() if "3/7" in ln and "файлов" in ln]
        self.assertTrue(lines, f"нет строки прогресса «3/7 … файлов»:\n{text}")


if __name__ == "__main__":
    unittest.main()

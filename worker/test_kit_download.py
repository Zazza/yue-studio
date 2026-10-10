"""Тесты карточки internal-own-track, этап 14б «Наборы: надёжная загрузка и прогресс»
(условия 115–118, тест-кейсы ТК144, ТК146). Написаны по карточке, без чтения реализации.

Контракт (из карточки):
- 115: закреплённые списки {набор: {часть: [имена файлов]}} (worker/fx_kit_files.json, читается
  через yue_worker._kit_pinned_files()); есть список — файлы качаются прямыми ссылками
  raw.githubusercontent.com/<repo>/<ref>/<каталог>/<имя> (пробел → %20, «#» → %23), API GitHub
  не вызывается; нет списка — как раньше (API contents). Файл ставится установщиком и deploy.sh.
- 116: каждый файл — до 3 попыток (паузы 1, 2 с); уже скачанные в <часть>.part файлы при
  повторной установке не качаются заново; каталог части публикуется только целиком;
  ошибка — 502 с причиной (набор/часть/файл).
- 117: GET /fx/kits/progress → {name, part, done, total, bytes} идущей установки, нет — {}.
- 118: worker/kits_install.py — main(argv) ставит все наборы FX_KITS; ошибка одного не
  останавливает остальные; код выхода 1 при ошибке, 0 без; вывод — имена наборов, итог «… МБ».

Внешние границы: сеть (_http_get воркера подменяется; сокеты мимо urllib запрещены базой
_KitCase), паузы ретраев (time.sleep подменяется — тест быстрый), список закреплённых файлов
(_kit_pinned_files подменяется своим словарём). Хранилище — своё на тест (<data> из _FxApiCase).

Допущения (сверить с постановщиком, если тест красный не по делу):
- сбой загрузки моделируется так, как его отдаёт urllib: _http_get бросает URLError;
- прогресс ведёт _fx_kit_install (а не только обёртка эндпоинта); done между файлами считается
  либо законченными файлами (0..total−1), либо текущим (1..total) — допустимы оба;
- в kits_install наборы ставятся через fx_kit_install(name) (эндпоинт воркера), его и подменяем.

Запуск: cd worker && python3 -m unittest test_kit_download -v
"""
import contextlib
import io
import json
import unittest
import urllib.error
from pathlib import Path
from unittest import mock

try:
    import test_fx_kits as fk
    _KitBase, _ApiBase = fk._KitCase, fk.fa._FxApiCase
    _OK = fk.fa._OK
except ImportError:   # окружение без fastapi и т.п. — классы ниже пропускаются
    _KitBase = _ApiBase = unittest.TestCase
    _OK = False

_SKIP = "нужно окружение воркера (fastapi/httpx/numpy/soundfile/librosa)"

ROOT = Path(__file__).resolve().parent.parent
REPO = "owner/kit-repo"
REF = "0123456789abcdef0123456789abcdef01234567"
KIT = "kit-x"
PART = "solo"
FOLDER = "Strings/Solo Violin/Arco Vib"
RAW = f"https://raw.githubusercontent.com/{REPO}/{REF}/Strings/Solo%20Violin/Arco%20Vib/"
FILES = ["X_A#2_v1.wav", "X_C4_v1.wav", "X_D4_v1.wav"]
URLS = {
    "X_A#2_v1.wav": RAW + "X_A%232_v1.wav",
    "X_C4_v1.wav": RAW + "X_C4_v1.wav",
    "X_D4_v1.wav": RAW + "X_D4_v1.wav",
}


def _spec(**extra):
    return {"repo": REPO, "ref": REF, "parts": {PART: (FOLDER, r"X_.+\.wav")}, "max_s": 8.0, **extra}


def _fname(url):
    """Имя файла, к которому относится URL (по закреплённому списку), или None."""
    for name, u in URLS.items():
        if url == u:
            return name
    return None


@unittest.skipUnless(_OK, _SKIP)
class _PinnedCase(_KitBase):
    """Своё хранилище; закреплённый список — {KIT: {PART: FILES}}; паузы подменены и записываются."""

    pinned = {KIT: {PART: list(FILES)}}

    def setUp(self):
        super().setUp()
        p = mock.patch.object(self.w, "_kit_pinned_files", return_value=self.pinned)
        p.start()
        self.addCleanup(p.stop)
        self.sleeps = []

        def fake_sleep(s):
            self.sleeps.append(float(s))

        p = mock.patch("time.sleep", fake_sleep)
        p.start()
        self.addCleanup(p.stop)
        if hasattr(self.w, "sleep"):   # на случай `from time import sleep`
            p = mock.patch.object(self.w, "sleep", fake_sleep)
            p.start()
            self.addCleanup(p.stop)
        self.calls = []        # все URL, запрошенные через _http_get
        self.wav = fk._wav(0.5)

    def _net(self, fail=None):
        """fake _http_get: raw-ссылки закреплённого списка → WAV; api.github.com — запрещён в pinned;
        fail(name, n) → True — n-я попытка файла name падает URLError."""
        tries = {}

        def fake_get(url, *a, **kw):
            self.calls.append(url)
            name = _fname(url)
            if name is None:
                raise AssertionError(f"неожиданный запрос: {url}")
            tries[name] = tries.get(name, 0) + 1
            if fail and fail(name, tries[name]):
                raise urllib.error.URLError("обрыв соединения")
            return self.wav
        return fake_get, tries

    def _install(self, fake_get, spec=None):
        with mock.patch.object(self.w, "_http_get", fake_get):
            return self.w._fx_kit_install(KIT, spec or _spec())

    def _part_dir(self):
        return self.kits / KIT / PART


class TestPinnedDirectLinks(_PinnedCase):
    """Условие 115, ТК144: есть закреплённый список — прямые raw-ссылки, API не вызывается."""

    def test_tc144_raw_urls_escaped_no_api(self):
        fake_get, _ = self._net()
        got = self._install(fake_get)
        self.assertEqual(got["parts"], {PART: len(FILES)})
        self.assertFalse([u for u in self.calls if "api.github.com" in u], f"вызван API: {self.calls}")
        self.assertEqual(sorted(self.calls), sorted(URLS.values()))
        for u in self.calls:
            self.assertNotIn(" ", u)
            self.assertNotIn("#", u, "«#» в URL не экранирован — всё после него уйдёт во фрагмент")
        self.assertIn(RAW + "X_A%232_v1.wav", self.calls)
        files = sorted(f.name for f in self._part_dir().iterdir() if f.suffix == ".wav")
        self.assertEqual(len(files), len(FILES), files)

    def test_no_pinned_list_uses_api_as_before(self):
        # набора нет в закреплённом списке — список файлов берётся из API contents, как раньше
        listing = [{"name": n, "type": "file", "download_url": f"https://dl.example/{i}.wav"}
                   for i, n in enumerate(FILES)]
        calls = []

        def fake_get(url, *a, **kw):
            calls.append(url)
            if "api.github.com" in url:
                return json.dumps(listing).encode()
            return self.wav

        with mock.patch.object(self.w, "_kit_pinned_files", return_value={}), \
                mock.patch.object(self.w, "_http_get", fake_get):
            got = self.w._fx_kit_install(KIT, _spec())
        self.assertEqual(got["parts"], {PART: len(FILES)})
        self.assertTrue([u for u in calls if "api.github.com" in u], f"API не вызван: {calls}")

    def test_pinned_list_is_shipped_for_session_kits(self):
        # настоящий список (fx_kit_files.json): наборы VSCO и swagbass/blackblue/meatbass/pastabass,
        # каждая часть набора из FX_KITS — непустой список имён файлов
        self.assertTrue((ROOT / "worker" / "fx_kit_files.json").is_file(), "нет worker/fx_kit_files.json")
        data = json.loads((ROOT / "worker" / "fx_kit_files.json").read_text(encoding="utf-8"))
        want = [k for k in self.w.FX_KITS if k.startswith("vsco-")] + \
            ["swagbass", "blackblue", "meatbass", "pastabass"]
        for kit in want:
            with self.subTest(kit=kit):
                self.assertIn(kit, data)
                self.assertEqual(set(data[kit]), set(self.w.FX_KITS[kit]["parts"]))
                for part, names in data[kit].items():
                    self.assertTrue(names, f"{kit}/{part}: пустой список")
                    self.assertTrue(all(isinstance(n, str) and n for n in names), f"{kit}/{part}")

    def test_pinned_file_installed_by_deploy_and_installer(self):
        # условие 115: файл ставится установщиком (install_worker) и deploy.sh
        for path in (ROOT / "deploy.sh", ROOT / "internal" / "mcp" / "tools_install.go"):
            with self.subTest(file=path.name):
                text = path.read_text(encoding="utf-8")
                self.assertIn("fx_kit_files.json", text)


class TestRetries(_PinnedCase):
    """Условие 116, ТК144: до 3 попыток на файл, паузы 1 и 2 с; 3 падения — 502, части нет."""

    def test_tc144_two_failures_then_success(self):
        flaky = FILES[1]
        fake_get, tries = self._net(fail=lambda name, n: name == flaky and n <= 2)
        got = self._install(fake_get)
        self.assertEqual(got["parts"], {PART: len(FILES)})
        self.assertEqual(tries[flaky], 3)
        self.assertEqual(len([f for f in self._part_dir().iterdir() if f.suffix == ".wav"]), len(FILES))
        self.assertIn(1.0, self.sleeps, f"нет паузы 1 с: {self.sleeps}")
        self.assertIn(2.0, self.sleeps, f"нет паузы 2 с: {self.sleeps}")

    def test_tc144_three_failures_is_502_and_no_part(self):
        bad = FILES[1]
        fake_get, tries = self._net(fail=lambda name, n: name == bad)
        with self.assertRaises(self.w.HTTPException) as cm:
            self._install(fake_get)
        self.assertEqual(cm.exception.status_code, 502)
        self.assertEqual(tries[bad], 3, "попыток должно быть ровно 3")
        detail = str(cm.exception.detail)
        for piece in (KIT, PART, "X_C4_v1"):
            self.assertIn(piece, detail, f"в причине нет «{piece}»: {detail}")
        self.assertFalse(self._part_dir().exists(), "часть опубликована не целиком")

    def test_endpoint_returns_502_with_reason(self):
        # через HTTP: набор из каталога, файл всё время падает → 502, в причине имя набора
        fake_get, _ = self._net(fail=lambda name, n: True)
        kits = {**self.w.FX_KITS, KIT: _spec()}
        with mock.patch.object(self.w, "FX_KITS", kits), mock.patch.object(self.w, "_http_get", fake_get):
            r = self.client.post("/fx/kits/install", params={"name": KIT})
        self.assertEqual(r.status_code, 502, r.text)
        self.assertIn(KIT, r.text)
        self.assertFalse(self._part_dir().exists())


class TestResume(_PinnedCase):
    """Условие 116, ТК144: повтор после падения не качает уже скачанные файлы (.part)."""

    def test_tc144_resume_skips_downloaded(self):
        last = FILES[-1]
        fake_get, tries = self._net(fail=lambda name, n: name == last)
        with self.assertRaises(self.w.HTTPException):
            self._install(fake_get)
        done_before = {n for n, k in tries.items() if n != last}
        self.assertTrue(done_before, "до падения ни один файл не скачан — проверять нечего")
        self.assertFalse(self._part_dir().exists())

        self.calls.clear()
        fake_get, tries2 = self._net()
        got = self._install(fake_get)
        self.assertEqual(got["parts"], {PART: len(FILES)})
        again = done_before & set(tries2)
        self.assertFalse(again, f"скачаны заново: {sorted(again)}")
        self.assertIn(last, tries2)
        self.assertEqual(len([f for f in self._part_dir().iterdir() if f.suffix == ".wav"]), len(FILES))


class TestProgress(_PinnedCase):
    """Условие 117, ТК144: GET /fx/kits/progress во время установки — растущий done; после — {}."""

    def _progress(self):
        r = self.client.get("/fx/kits/progress")
        self.assertEqual(r.status_code, 200, r.text)
        return r.json()

    def test_idle_is_empty(self):
        self.assertEqual(self._progress(), {})

    def test_tc144_progress_grows_then_empty(self):
        seen = []
        inner, _ = self._net()

        def fake_get(url, *a, **kw):
            seen.append(self._progress())
            return inner(url, *a, **kw)

        self._install(fake_get)
        self.assertEqual(len(seen), len(FILES))
        for p in seen:
            self.assertLessEqual({"name", "part", "done", "total", "bytes"}, set(p), p)
            self.assertEqual(p["name"], KIT)
            self.assertEqual(p["part"], PART)
            self.assertEqual(p["total"], len(FILES))
        dones = [p["done"] for p in seen]
        self.assertIn(dones, (list(range(0, len(FILES))), list(range(1, len(FILES) + 1))),
                      f"done должен расти по файлу: {dones}")
        sizes = [p["bytes"] for p in seen]
        self.assertEqual(sizes, sorted(sizes), f"bytes убывает: {sizes}")
        self.assertGreater(sizes[-1], 0, "к последнему файлу скачанные байты не учтены")
        self.assertEqual(self._progress(), {}, "после установки прогресс не сброшен")

    def test_progress_empty_after_failure(self):
        fake_get, _ = self._net(fail=lambda name, n: True)
        with self.assertRaises(self.w.HTTPException):
            self._install(fake_get)
        self.assertEqual(self._progress(), {}, "после ошибки прогресс не сброшен")


@unittest.skipUnless(_OK, _SKIP)
class TestKitsInstall(_ApiBase):
    """Условие 118, ТК146: kits_install.main ставит каждый набор каталога, ошибка одного — не стоп."""

    def _run(self, fail=(), exc=None):
        import kits_install
        from fastapi import HTTPException
        called = []

        def fake_install(name="", *a, **kw):
            called.append(name)
            if name in fail:
                raise (exc or HTTPException(502, f"{name}: HTTP Error 403: rate limit exceeded"))
            return {"name": name, "parts": {"p": 3}, "downloaded": True}

        patches = [mock.patch.object(self.w, "fx_kit_install", fake_install)]
        if hasattr(kits_install, "fx_kit_install"):
            patches.append(mock.patch.object(kits_install, "fx_kit_install", fake_install))
        out = io.StringIO()
        with contextlib.ExitStack() as st:
            for p in patches:
                st.enter_context(p)
            st.enter_context(contextlib.redirect_stdout(out))
            st.enter_context(contextlib.redirect_stderr(out))
            code = kits_install.main([])
        return code, called, out.getvalue()

    def test_tc146_all_kits_ok(self):
        code, called, out = self._run()
        self.assertEqual(code, 0, out)
        self.assertEqual(sorted(called), sorted(self.w.FX_KITS), "поставлены не все наборы каталога")
        for name in self.w.FX_KITS:
            self.assertIn(name, out, f"в выводе нет набора {name}")
        self.assertIn("МБ", out, f"нет итога в МБ: {out}")

    def test_tc146_one_failure_does_not_stop_others(self):
        names = list(self.w.FX_KITS)
        bad = names[0]
        code, called, out = self._run(fail={bad})
        self.assertEqual(code, 1, out)
        self.assertEqual(sorted(called), sorted(names), "после ошибки остальные наборы не ставились")
        self.assertIn(bad, out)

    def test_non_http_error_does_not_stop_others(self):
        # сбой не только HTTPException (например, диск) — остальные наборы всё равно ставятся
        names = list(self.w.FX_KITS)
        bad = names[-1]
        code, called, out = self._run(fail={bad}, exc=OSError("No space left on device"))
        self.assertEqual(code, 1, out)
        self.assertEqual(sorted(called), sorted(names))

    def test_install_steps_present(self):
        # условие 118: шаг в установщике воркера и в deploy.sh
        for path in (ROOT / "deploy.sh", ROOT / "internal" / "mcp" / "tools_install.go"):
            with self.subTest(file=path.name):
                self.assertIn("kits_install", path.read_text(encoding="utf-8"))


if __name__ == "__main__":
    unittest.main()

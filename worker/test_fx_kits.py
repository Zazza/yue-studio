"""Тесты карточки internal-studio-engine, условие 14 (этап 5а): наборы сэмплов
барабанов на воркере — POST /fx/kits/install, kits в GET /fx/assets,
resources.kit(name), sampler по HTTP с настоящим хранилищем.

Контракт (из карточки и контракта задачи):
- набор `<набор>/<часть>` — файлы `<data>/fx/kits/<набор>/<часть>/*.wav`;
- GET /fx/assets → добавлено `kits: [{name: "<набор>/<часть>", samples: N}]`;
- POST /fx/kits/install?name=osdk → `{name, parts: {kick: N, snare: M}, downloaded: bool}`;
  воркер сам качает The Open Source Drum Kit (GitHub crabacus/the-open-source-drumkit,
  ветка master): kick/* → kick, snare/snare-top<N>.wav → snare (snare-top-off*,
  snare-top-buttend*, snare-bottom* — нет); загрузка в `<data>/fx/kits/osdk/{kick,snare}/`;
  уже установлен → downloaded=false без сети; неизвестное имя → 422;
- fx_resources().kit("osdk/kick") → (список сэмплов, sr).

Внешняя граница — сеть: urllib.request.OpenerDirector.open подменяется фейковым
GitHub (FakeGitHub) — через него идут и urlopen, и urlretrieve, и build_opener,
при любом способе импорта. Фейк отвечает на обычные пути GitHub: API деревьев/
содержимого/архивов (api.github.com), raw.githubusercontent.com, архивы
github.com/.../archive и codeload.github.com. Прямое подключение сокетом (мимо
urllib) запрещено и записывается — тест падает, реальных запросов нет.

Запуск: cd worker && python3 -m unittest test_fx_kits -v
"""
import http.client
import io
import json
import re
import socket
import tarfile
import threading
import time
import unittest
import urllib.error
import urllib.parse
import urllib.request
import zipfile
from email.message import Message
from unittest import mock

import test_fx_api as fa

OWNER, REPO, REF = "crabacus", "the-open-source-drumkit", "master"

# раскладка фейкового репозитория (как у настоящего: kick/kickN, snare/snare-top*,
# snare-bottom*, другие части и README) → пик |x| каждого файла (уникальный)
TREE = {
    "README.md": None,
    "kick/kick1.wav": 0.51,
    "kick/kick2.wav": 0.52,
    "kick/kick10.wav": 0.53,
    "snare/snare-top1.wav": 0.61,
    "snare/snare-top12.wav": 0.62,
    "snare/snare-top-off1.wav": 0.71,
    "snare/snare-top-buttend3.wav": 0.72,
    "snare/snare-top-off-alt1.wav": 0.73,
    "snare/snare-bottom1.wav": 0.74,
    "snare/snare-bottom-off2.wav": 0.75,
    "hihat/hihat1.wav": 0.81,
    "toms/tom1.wav": 0.82,
}
KICK_PEAKS = {0.51, 0.52, 0.53}
SNARE_PEAKS = {0.61, 0.62}
KIT_SR = 44100


def _wav(peak, sr=KIT_SR):
    import numpy as np
    import soundfile as sf
    k = np.arange(int(0.05 * sr))
    x = (peak * np.exp(-k / (0.01 * sr)) * np.cos(2 * np.pi * 200 * k / sr)).astype(np.float32)
    buf = io.BytesIO()
    sf.write(buf, x, sr, format="WAV", subtype="PCM_24")
    return buf.getvalue()


class _Resp(io.BytesIO):
    """Ответ urllib: read/readinto/with, status, headers, info(), geturl()."""

    def __init__(self, data, url, ctype="application/octet-stream"):
        super().__init__(data)
        self.url = url
        self.status = self.code = 200
        self.reason = "OK"
        self.headers = self.msg = Message()
        self.headers["Content-Type"] = ctype
        self.headers["Content-Length"] = str(len(data))
        self.length = len(data)

    def info(self):
        return self.headers

    def getcode(self):
        return self.status

    def geturl(self):
        return self.url

    def getheader(self, name, default=None):
        return self.headers.get(name, default)


class _BrokenResp(_Resp):
    """Обрыв соединения посреди тела: отдаёт часть и бросает http.client.IncompleteRead."""

    def _broken(self):
        raise http.client.IncompleteRead(self.getvalue()[:100], self.length - 100)

    def read(self, *a):
        self._broken()

    def read1(self, *a):
        self._broken()

    def readinto(self, *a):
        self._broken()

    def readline(self, *a):
        self._broken()

    def __iter__(self):
        self._broken()


class FakeGitHub:
    """Фейк GitHub для одного репозитория: пишет все запрошенные URL."""

    def __init__(self):
        self.calls = []
        self.files = {p: (b"# The Open Source Drum Kit\n" if v is None else _wav(v)) for p, v in TREE.items()}
        self.hook = None       # вызывается на каждый запрос (задержка/барьер в тестах гонки)
        self.fault = None      # None | "incomplete" (обрыв ответа файла) | "not_list" (каталог — объект)
        self.ref = REF         # ref текущего запроса (?ref=…) — отражается в download_url, как у GitHub

    # --- данные ---
    def _dirs(self):
        return sorted({p.rsplit("/", 1)[0] for p in self.files if "/" in p})

    def _tree(self):
        items = [{"path": d, "type": "tree", "mode": "040000", "sha": "0" * 40} for d in self._dirs()]
        items += [{"path": p, "type": "blob", "mode": "100644", "sha": "1" * 40, "size": len(b)}
                  for p, b in sorted(self.files.items())]
        return {"sha": "f" * 40, "truncated": False, "tree": items}

    def _contents(self, path):
        path = path.strip("/")
        if path in self.files:
            return self._entry(path)
        if self.fault == "not_list":
            # ответ API не списком: объект (ограничение запросов, ошибка прокси и т.п.)
            return {"message": "API rate limit exceeded", "documentation_url": "https://docs.github.com"}
        pref = path + "/" if path else ""
        names = {}
        for p in self.files:
            if p.startswith(pref):
                rest = p[len(pref):]
                head = rest.split("/", 1)[0]
                names[head] = "dir" if "/" in rest else "file"
        if not names:
            return None
        return [self._entry(pref + n, t) for n, t in sorted(names.items())]

    def _entry(self, path, typ="file"):
        ref = self.ref
        e = {"name": path.rsplit("/", 1)[-1], "path": path, "type": typ, "sha": "1" * 40,
             "url": f"https://api.github.com/repos/{OWNER}/{REPO}/contents/{path}?ref={ref}",
             "html_url": f"https://github.com/{OWNER}/{REPO}/blob/{ref}/{path}"}
        if typ == "file":
            e["size"] = len(self.files[path])
            e["download_url"] = f"https://raw.githubusercontent.com/{OWNER}/{REPO}/{ref}/{path}"
        else:
            e["download_url"] = None
        return e

    def _zip(self):
        buf = io.BytesIO()
        with zipfile.ZipFile(buf, "w") as z:
            top = f"{REPO}-{REF}/"
            z.writestr(top, b"")
            for d in self._dirs():
                z.writestr(top + d + "/", b"")
            for p, b in self.files.items():
                z.writestr(top + p, b)
        return buf.getvalue()

    def _tgz(self):
        buf = io.BytesIO()
        with tarfile.open(fileobj=buf, mode="w:gz") as t:
            top = f"{REPO}-{REF}/"
            for p, b in self.files.items():
                info = tarfile.TarInfo(top + p)
                info.size = len(b)
                t.addfile(info, io.BytesIO(b))
        return buf.getvalue()

    # --- маршрутизация ---
    def open(self, fullurl, data=None, timeout=None, **kw):
        url = fullurl.full_url if isinstance(fullurl, urllib.request.Request) else str(fullurl)
        self.calls.append(url)
        if self.hook:
            self.hook(url)
        u = urllib.parse.urlsplit(url)
        host, parts = u.netloc.lower(), [urllib.parse.unquote(x) for x in u.path.strip("/").split("/")]
        self.ref = urllib.parse.parse_qs(u.query).get("ref", [REF])[0]
        body = self._route(host, parts)
        if body is None:
            raise urllib.error.HTTPError(url, 404, "Not Found", Message(), io.BytesIO(b"Not Found"))
        if isinstance(body, (dict, list)):
            return _Resp(json.dumps(body).encode(), url, "application/json")
        if self.fault == "incomplete" and parts[-1:] == ["kick2.wav"]:
            return _BrokenResp(body, url)
        return _Resp(body, url)

    def _route(self, host, parts):
        repo = [OWNER, REPO]
        if host == "api.github.com" and parts[:1] == ["repos"] and parts[1:3] == repo:
            rest = parts[3:]
            if not rest:
                return {"name": REPO, "full_name": f"{OWNER}/{REPO}", "default_branch": REF}
            if rest[:2] == ["git", "trees"] or rest[:1] in (["branches"], ["commits"]):
                if rest[0] == "git":
                    return self._tree()
                return {"name": REF, "sha": "f" * 40, "commit": {"sha": "f" * 40}}
            if rest[:1] == ["contents"]:
                return self._contents("/".join(rest[1:]))
            if rest[:1] == ["zipball"]:
                return self._zip()
            if rest[:1] == ["tarball"]:
                return self._tgz()
            return None
        if host == "raw.githubusercontent.com" and parts[:2] == repo:
            rest = parts[2:]
            if rest[:2] == ["refs", "heads"]:
                rest = rest[3:]
            else:
                rest = rest[1:]
            return self.files.get("/".join(rest))
        if host in ("github.com", "www.github.com") and parts[:2] == repo:
            rest = parts[2:]
            if rest[:1] in (["raw"], ["blob"]):
                rest = rest[1:]
                rest = rest[3:] if rest[:2] == ["refs", "heads"] else rest[1:]
                return self.files.get("/".join(rest))
            if rest[:1] == ["archive"]:
                last = rest[-1]
                return self._zip() if last.endswith(".zip") else self._tgz() if last.endswith(".tar.gz") else None
            return None
        if host == "codeload.github.com" and parts[:2] == repo:
            kind = parts[2] if len(parts) > 2 else ""
            return self._zip() if kind == "zip" else self._tgz() if kind in ("tar.gz", "legacy.tar.gz") else None
        return None


@unittest.skipUnless(fa._OK, fa._SKIP)
class _KitCase(fa._FxApiCase):
    """Своё хранилище (<data> на тест), сеть — FakeGitHub, сокеты мимо urllib запрещены."""

    def setUp(self):
        super().setUp()
        self.gh = FakeGitHub()
        gh = self.gh

        def fake_open(director, fullurl, data=None, timeout=None, *a, **kw):
            return gh.open(fullurl, data, timeout)

        p = mock.patch.object(urllib.request.OpenerDirector, "open", fake_open)
        p.start()
        self.addCleanup(p.stop)
        self.sockets = []

        def no_net(*a, **kw):
            self.sockets.append(a[:1])
            raise OSError("сеть в тестах запрещена (запрос мимо urllib)")

        p = mock.patch.object(socket, "create_connection", no_net)
        p.start()
        self.addCleanup(p.stop)
        self.kits = self.data / "fx" / "kits"

    def _install(self, name="osdk"):
        return self.client.post("/fx/kits/install", params={"name": name})

    def _assets(self):
        r = self.client.get("/fx/assets")
        self.assertEqual(r.status_code, 200, r.text)
        return r.json()

    def _peaks(self, d):
        import numpy as np
        import soundfile as sf
        out = []
        for p in sorted(d.glob("*.wav")):
            x, _ = sf.read(str(p), dtype="float64", always_2d=True)
            out.append(round(float(np.abs(x).max()), 2))
        return out

    def _real_resources(self):
        self.resources = None
        p = mock.patch.object(self.w, "fx_resources", self.real_fx_resources)
        p.start()
        self.addCleanup(p.stop)

    def _put_kit(self, kit, part, peaks, sr=KIT_SR, extra=()):
        d = self.kits / kit / part
        d.mkdir(parents=True, exist_ok=True)
        for i, pk in enumerate(peaks):
            (d / f"s{i + 1}.wav").write_bytes(_wav(pk, sr))
        for name in extra:
            (d / name).write_bytes(b"not a sample")
        return d


class TestKitInstall(_KitCase):

    def test_install_osdk_layout(self):
        r = self._install()
        self.assertEqual(r.status_code, 200, r.text)
        body = r.json()
        self.assertEqual(body.get("name"), "osdk")
        self.assertEqual(body.get("parts"), {"kick": 3, "snare": 2})
        self.assertIs(body.get("downloaded"), True)
        self.assertTrue(self.gh.calls, "набор не скачивался")
        self.assertEqual(self.sockets, [], "запрос мимо urllib")
        root = self.kits / "osdk"
        self.assertEqual(sorted(p.name for p in root.iterdir() if p.is_dir()), ["kick", "snare"])
        # kick/* → kick (все три), snare-top<N>.wav → snare (только два), прочее не тянется
        self.assertEqual(set(self._peaks(root / "kick")), KICK_PEAKS)
        self.assertEqual(len(self._peaks(root / "kick")), 3)
        self.assertEqual(set(self._peaks(root / "snare")), SNARE_PEAKS)
        self.assertEqual(len(self._peaks(root / "snare")), 2)

    def test_repeat_without_network(self):
        self.assertEqual(self._install().status_code, 200)
        self.gh.calls.clear()
        r = self._install()
        self.assertEqual(r.status_code, 200, r.text)
        body = r.json()
        self.assertIs(body.get("downloaded"), False)
        self.assertEqual(body.get("parts"), {"kick": 3, "snare": 2})
        self.assertEqual(self.gh.calls, [], "повтор качает заново")
        self.assertEqual(self.sockets, [])

    def test_unknown_name_422(self):
        for name in ("nope", "", "../osdk", "OSDK2"):
            with self.subTest(name=name):
                r = self._install(name)
                self.assertEqual(r.status_code, 422, r.text)
        self.assertEqual(self.gh.calls, [], "неизвестное имя — без сети")
        self.assertFalse((self.kits / "nope").exists())
        self.assertFalse((self.data / "fx" / "osdk").exists())

    def test_assets_list_kits_after_install(self):
        self.assertEqual(self._assets().get("kits"), [])
        self.assertEqual(self._install().status_code, 200)
        kits = self._assets().get("kits")
        self.assertIsInstance(kits, list)
        self.assertEqual(sorted((k["name"], k["samples"]) for k in kits),
                         [("osdk/kick", 3), ("osdk/snare", 2)])
        # прежние списки на месте
        a = self._assets()
        self.assertIn("amps", a)
        self.assertIn("irs", a)

    def test_resources_kit_after_install(self):
        import numpy as np
        self.assertEqual(self._install().status_code, 200)
        samples, sr = self.real_fx_resources().kit("osdk/kick")
        self.assertEqual(sr, KIT_SR)
        self.assertEqual(len(samples), 3)
        self.assertEqual({round(float(np.abs(np.asarray(s)).max()), 2) for s in samples}, KICK_PEAKS)
        snare, _ = self.real_fx_resources().kit("osdk/snare")
        self.assertEqual(len(snare), 2)


class TestKitInstallRobust(_KitCase):
    """Регрессия кросс-ревью (условие 14): установка по очереди, сбой сети без
    полукаталога, версия набора закреплена за коммитом."""

    def _assert_full_kit(self):
        root = self.kits / "osdk"
        self.assertEqual(sorted(p.name for p in root.iterdir() if p.is_dir()), ["kick", "snare"])
        self.assertEqual(sorted(self._peaks(root / "kick")), sorted(KICK_PEAKS))
        self.assertEqual(sorted(self._peaks(root / "snare")), sorted(SNARE_PEAKS))

    def _no_part_files(self):
        left = [str(p.relative_to(self.data)) for p in self.data.rglob("*") if ".part" in p.name]
        self.assertEqual(left, [], "остались временные *.part")

    def test_two_parallel_installs_of_same_kit(self):
        lock = threading.Lock()
        seen = set()
        barrier = threading.Barrier(2)

        def hook(url):
            # первый запрос каждого потока ждёт второй — оба качают одновременно;
            # если реализация ставит установки в очередь, второго не будет — ждём недолго
            me = threading.get_ident()
            with lock:
                first = me not in seen
                seen.add(me)
            if first:
                try:
                    barrier.wait(timeout=2)
                except threading.BrokenBarrierError:
                    pass  # установка сериализована — допустимо
            time.sleep(0.005)  # расширить окно гонки

        self.gh.hook = hook
        results = [None, None]

        def run(i):
            results[i] = self._install()

        ts = [threading.Thread(target=run, args=(i,)) for i in range(2)]
        for t in ts:
            t.start()
        for t in ts:
            t.join(timeout=120)
        for i, r in enumerate(results):
            self.assertIsNotNone(r, f"установка {i} не завершилась")
            self.assertEqual(r.status_code, 200, r.text)
            self.assertEqual(r.json().get("parts"), {"kick": 3, "snare": 2})
        flags = sorted(r.json().get("downloaded") for r in results)
        self.assertIn(flags, ([False, True], [True, True]), f"downloaded: {flags}")
        self._assert_full_kit()
        self._no_part_files()
        self.assertEqual(sorted((k["name"], k["samples"]) for k in self._assets()["kits"]),
                         [("osdk/kick", 3), ("osdk/snare", 2)])
        self.assertEqual(self.sockets, [])

    def _check_failure_then_retry(self, fault):
        self.gh.fault = fault
        r = self._install()
        self.assertEqual(r.status_code, 502, r.text)
        detail = r.json().get("detail")
        self.assertTrue(isinstance(detail, str) and detail.strip(), f"502 без причины: {r.text}")
        self.assertTrue(self.gh.calls, "сеть не вызывалась — сбой не проверен")
        # каталогов частей (kick/snare) и файлов набора нет; пустой каталог osdk допускается
        # (условие — «каталога части нет»), но установку не должен считать готовой — см. повтор ниже
        osdk = self.kits / "osdk"
        left = sorted(str(p.relative_to(osdk)) for p in osdk.rglob("*")) if osdk.exists() else []
        self.assertEqual(left, [], "после сбоя остался полукаталог набора")
        self._no_part_files()
        self.assertEqual(self._assets().get("kits"), [], "недокачанный набор в списке")
        # сеть починилась — установка проходит
        self.gh.fault = None
        r = self._install()
        self.assertEqual(r.status_code, 200, r.text)
        self.assertIs(r.json().get("downloaded"), True)
        self.assertEqual(r.json().get("parts"), {"kick": 3, "snare": 2})
        self._assert_full_kit()
        self._no_part_files()

    def test_incomplete_read_502_no_partial_then_retry(self):
        self._check_failure_then_retry("incomplete")
        self.assertTrue(any(u.endswith("kick2.wav") for u in self.gh.calls), "оборванный файл не запрашивался")

    def test_api_not_a_list_502_no_partial_then_retry(self):
        self._check_failure_then_retry("not_list")

    def test_contents_pinned_to_commit_sha(self):
        r = self._install()
        self.assertEqual(r.status_code, 200, r.text)
        contents = [u for u in self.gh.calls
                    if urllib.parse.urlsplit(u).netloc == "api.github.com" and "/contents" in u]
        self.assertTrue(contents, f"запросов к GitHub contents нет: {self.gh.calls}")
        refs = set()
        for u in contents:
            with self.subTest(url=u):
                ref = urllib.parse.parse_qs(urllib.parse.urlsplit(u).query).get("ref", [""])[0]
                self.assertRegex(ref, r"^[0-9a-f]{40}$", "contents без ?ref=<sha коммита>")
                refs.add(ref)
        self.assertEqual(len(refs), 1, f"разные версии в одной установке: {refs}")
        sha = refs.pop()
        # файлы — той же версии (не ветки master)
        files = [u for u in self.gh.calls if u.lower().endswith(".wav")]
        self.assertTrue(files)
        for u in files:
            with self.subTest(file=u):
                self.assertIn(sha, u, "файл набора скачан не с закреплённой версии")
                self.assertIsNone(re.search(r"/(master|main)/", u), "файл набора — с ветки")


class TestKitStore(_KitCase):
    """Наборы, положенные в хранилище файлами (без установки): список и resources.kit."""

    def test_assets_kits_from_files(self):
        self._put_kit("mykit", "kick", [0.3, 0.4], extra=("readme.txt",))
        self._put_kit("mykit", "snare", [0.5])
        kits = self._assets().get("kits")
        self.assertEqual(sorted((k["name"], k["samples"]) for k in kits),
                         [("mykit/kick", 2), ("mykit/snare", 1)])
        self.assertEqual(self.gh.calls, [])

    def test_resources_kit_reads_samples_and_rate(self):
        import numpy as np
        self._put_kit("mykit", "kick", [0.3, 0.4], sr=48000, extra=("readme.txt",))
        samples, sr = self.real_fx_resources().kit("mykit/kick")
        self.assertEqual(sr, 48000)
        self.assertEqual(sorted(round(float(np.abs(np.asarray(s)).max()), 2) for s in samples), [0.3, 0.4])

    def test_resources_kit_missing_is_key_error(self):
        self._put_kit("mykit", "kick", [0.3])
        res = self.real_fx_resources()
        for name in ("mykit/snare", "nope/kick", "mykit", "../mykit/kick", "mykit/../mykit/kick"):
            with self.subTest(name=name), self.assertRaises(KeyError):
                res.kit(name)


class TestSamplerOverHttp(_KitCase):
    """Цепочка с sampler через POST /jobs/{id}/fx с настоящим хранилищем наборов."""

    def test_sampler_with_loaded_kit_ok_missing_kit_422(self):
        self._put_kit("mykit", "kick", [0.3, 0.5, 0.8])
        self._real_resources()
        jid, d = self._audio_job()
        r = self._fx(jid, source="drums", chain=[{"type": "sampler", "kit": "mykit/kick"}], output="solo")
        self.assertEqual(r.status_code, 200, r.text)
        r = self._fx(jid, source="drums", chain=[{"type": "sampler", "kit": "nope/kick"}], output="solo")
        self.assertEqual(r.status_code, 422, r.text)
        self.assertIn("nope/kick", r.text)

    def test_sampler_no_network(self):
        self._put_kit("mykit", "kick", [0.5])
        self._real_resources()
        jid, _ = self._audio_job()
        r = self._fx(jid, source="drums", chain=[{"type": "sampler", "kit": "mykit/kick"}], output="solo")
        self.assertEqual(r.status_code, 200, r.text)
        self.assertEqual(self.gh.calls, [], "применение набора не ходит в сеть")


# ---------- Условие 15 (internal-studio-engine, этап 5б): набор growlybass, ТК14 ----------
#
# Контракт: FX_KITS["growlybass"] — Karoryfer growlybass (GitHub
# sfzinstruments/karoryfer.growlybass), версия закреплена коммитом (40 hex);
# sustain/*.wav → часть bass.

class TestGrowlybassCatalog(_KitCase):

    def setUp(self):
        super().setUp()
        self.kits = self.w.FX_KITS

    def test_tc14_growlybass_pinned(self):
        self.assertIn("growlybass", self.kits)
        spec = self.kits["growlybass"]
        self.assertEqual(spec["repo"], "sfzinstruments/karoryfer.growlybass")
        self.assertRegex(spec["ref"], r"^[0-9a-f]{40}$", "версия набора не закреплена коммитом")

    def test_tc14_part_bass_from_sustain_wavs(self):
        parts = self.kits["growlybass"]["parts"]
        self.assertEqual(list(parts), ["bass"])
        folder, pattern = parts["bass"]
        self.assertEqual(folder, "sustain")
        for name in ("a1_vl1_rr1.wav", "E1.wav", "x.wav"):
            with self.subTest(name=name):
                self.assertTrue(re.fullmatch(pattern, name), f"{name} не попадает в часть bass")
        for name in ("readme.txt", "a1.sfz", "a1.wav.bak"):
            with self.subTest(name=name):
                self.assertFalse(re.fullmatch(pattern, name), f"{name} попал в часть bass")


# ---------- Условие 15, ТК22: сэмпл набора читается до 6 с (FX_KIT_MAX_S) ----------

class TestKitMaxLength(_KitCase):

    def test_tc22_max_len_constant(self):
        self.assertEqual(self.w.FX_KIT_MAX_S, 6)

    def test_tc22_long_sample_read_up_to_6s(self):
        import numpy as np
        import soundfile as sf
        d = self.kits / "growlybass" / "bass"
        d.mkdir(parents=True)
        k = np.arange(8 * KIT_SR)
        x = (0.5 * np.exp(-k / KIT_SR / 4.0) * np.sin(2 * np.pi * 55 * k / KIT_SR)).astype(np.float32)
        sf.write(str(d / "a1.wav"), x, KIT_SR, subtype="PCM_24")
        samples, sr = self.real_fx_resources().kit("growlybass/bass")
        self.assertEqual(sr, KIT_SR)
        self.assertEqual(len(samples), 1)
        n = len(np.asarray(samples[0]))
        self.assertLessEqual(n, 6 * KIT_SR, "сэмпл длиннее 6 с прочитан целиком")
        self.assertGreaterEqual(n, 6 * KIT_SR - KIT_SR // 100, "сэмпл обрезан короче 6 с")


if __name__ == "__main__":
    unittest.main()

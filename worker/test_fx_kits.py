"""Тесты карточки internal-studio-engine, условие 14 (этап 5а): наборы сэмплов
барабанов на воркере — POST /fx/kits/install, kits в GET /fx/assets,
resources.kit(name), sampler по HTTP с настоящим хранилищем.

Контракт (из карточки и контракта задачи):
- набор `<набор>/<часть>` — файлы `<data>/fx/kits/<набор>/<часть>/*.wav`;
- GET /fx/assets → добавлено `kits: [{name: "<набор>/<часть>", samples: N}]`;
- POST /fx/kits/install?name=osdk → `{name, parts: {kick: N, snare: M, …}, downloaded: bool}`
  (с условия 26 — ещё hh-closed, hh-half, hh-open, ride, crash, см. ТК30–32);
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
    # части хэта и тарелок (условие 26): папки и имена — как у настоящего набора,
    # плюс посторонние файлы рядом (ride-bell, ride-mid-out, crash-bell)
    "hihat/closed-hihat/chh1.wav": 0.11,
    "hihat/closed-hihat/chh2.wav": 0.12,
    "hihat/half-closed-hihat/hchh1.wav": 0.21,
    "hihat/half-closed-hihat/hchh2.wav": 0.22,
    "hihat/half-closed-hihat/hchh3.wav": 0.23,
    "hihat/half-open-hihat/hohh1.wav": 0.31,
    "hihat/half-open-hihat/hohh2.wav": 0.32,
    "ride/ride-mid-in1.wav": 0.41,
    "ride/ride-mid-in2.wav": 0.42,
    "ride/ride-bell1.wav": 0.91,
    "ride/ride-mid-out1.wav": 0.92,
    "crash/crash1.wav": 0.33,
    "crash/crash2.wav": 0.34,
    "crash/crash3.wav": 0.35,
    "crash/crash-bell1.wav": 0.93,
}
KICK_PEAKS = {0.51, 0.52, 0.53}
SNARE_PEAKS = {0.61, 0.62}
# часть набора osdk → пики файлов, которые в неё попадают (посторонние — нет)
PART_PEAKS = {
    "kick": KICK_PEAKS,
    "snare": SNARE_PEAKS,
    "hh-closed": {0.11, 0.12},
    "hh-half": {0.21, 0.22, 0.23},
    "hh-open": {0.31, 0.32},
    "ride": {0.41, 0.42},
    "crash": {0.33, 0.34, 0.35},
}
ALL_PARTS = {k: len(v) for k, v in PART_PEAKS.items()}
ALL_KITS = sorted((f"osdk/{k}", n) for k, n in ALL_PARTS.items())
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
        self.assertEqual(body.get("parts"), ALL_PARTS)
        self.assertIs(body.get("downloaded"), True)
        self.assertTrue(self.gh.calls, "набор не скачивался")
        self.assertEqual(self.sockets, [], "запрос мимо urllib")
        root = self.kits / "osdk"
        self.assertEqual(sorted(p.name for p in root.iterdir() if p.is_dir()), sorted(PART_PEAKS))
        # kick/* → kick (все три), snare-top<N>.wav → snare (только два), части хэта и
        # тарелок — только свои файлы (ride-bell, ride-mid-out, crash-bell — нет), прочее не тянется
        for part, peaks in PART_PEAKS.items():
            with self.subTest(part=part):
                got = self._peaks(root / part)
                self.assertEqual(set(got), peaks)
                self.assertEqual(len(got), len(peaks))

    def test_repeat_without_network(self):
        self.assertEqual(self._install().status_code, 200)
        self.gh.calls.clear()
        r = self._install()
        self.assertEqual(r.status_code, 200, r.text)
        body = r.json()
        self.assertIs(body.get("downloaded"), False)
        self.assertEqual(body.get("parts"), ALL_PARTS)
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
                         ALL_KITS)
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
        self.assertEqual(sorted(p.name for p in root.iterdir() if p.is_dir()), sorted(PART_PEAKS))
        for part, peaks in PART_PEAKS.items():
            with self.subTest(part=part):
                self.assertEqual(sorted(self._peaks(root / part)), sorted(peaks))

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
            self.assertEqual(r.json().get("parts"), ALL_PARTS)
        flags = sorted(r.json().get("downloaded") for r in results)
        self.assertIn(flags, ([False, True], [True, True]), f"downloaded: {flags}")
        self._assert_full_kit()
        self._no_part_files()
        self.assertEqual(sorted((k["name"], k["samples"]) for k in self._assets()["kits"]), ALL_KITS)
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
        self.assertEqual(r.json().get("parts"), ALL_PARTS)
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


# ---------- Условие 15, ТК22: предел длины сэмпла — в каталоге набора ----------
#
# FX_KITS["growlybass"]["max_s"] = 6: сэмпл баса читается до 6 с; у прочих наборов
# (osdk) предела нет — файл 8 с читается целиком.

def _long_wav(path, dur=8, sr=KIT_SR):
    import numpy as np
    import soundfile as sf
    path.parent.mkdir(parents=True, exist_ok=True)
    k = np.arange(dur * sr)
    x = (0.5 * np.exp(-k / sr / 4.0) * np.sin(2 * np.pi * 55 * k / sr)).astype(np.float32)
    sf.write(str(path), x, sr, subtype="PCM_24")


class TestKitMaxLength(_KitCase):

    def test_tc22_max_s_in_kit_catalog(self):
        self.assertEqual(self.w.FX_KITS["growlybass"].get("max_s"), 6)
        self.assertNotIn("max_s", self.w.FX_KITS["osdk"], "у osdk предела длины быть не должно")

    def _read_len(self, name):
        import numpy as np
        samples, sr = self.real_fx_resources().kit(name)
        self.assertEqual(sr, KIT_SR)
        self.assertEqual(len(samples), 1)
        return len(np.asarray(samples[0]))

    def test_tc22_bass_sample_read_up_to_6s(self):
        _long_wav(self.kits / "growlybass" / "bass" / "a1.wav")
        n = self._read_len("growlybass/bass")
        self.assertLessEqual(n, 6 * KIT_SR, "сэмпл баса длиннее 6 с прочитан целиком")
        self.assertGreaterEqual(n, 6 * KIT_SR - KIT_SR // 100, "сэмпл баса обрезан короче 6 с")

    def test_tc22_osdk_sample_read_whole(self):
        _long_wav(self.kits / "osdk" / "kick" / "k1.wav")
        self.assertEqual(self._read_len("osdk/kick"), 8 * KIT_SR, "сэмпл osdk 8 с обрезан")


# ---------- Условие 26 (этап 5в), ТК30: части хэта и тарелок в наборе osdk ----------
#
# Контракт: FX_KITS["osdk"] (та же версия c58808b) получает части hh-closed
# (hihat/closed-hihat, chh*.wav), hh-half (hihat/half-closed-hihat, hchh*.wav),
# hh-open (hihat/half-open-hihat, hohh*.wav), ride (ride, ride-mid-in*.wav),
# crash (crash, crash<N>.wav); шаблон принимает только файлы своей части.

OSDK_NEW_PARTS = {
    "hh-closed": ("hihat/closed-hihat", ["chh1.wav", "chh12.wav"]),
    "hh-half": ("hihat/half-closed-hihat", ["hchh1.wav", "hchh7.wav"]),
    "hh-open": ("hihat/half-open-hihat", ["hohh1.wav", "hohh10.wav"]),
    "ride": ("ride", ["ride-mid-in1.wav", "ride-mid-in12.wav"]),
    "crash": ("crash", ["crash1.wav", "crash4.wav"]),
}


class TestOsdkCymbalParts(_KitCase):

    def setUp(self):
        super().setUp()
        self.osdk = self.w.FX_KITS["osdk"]

    def test_tc30_same_pinned_version(self):
        self.assertTrue(self.osdk["ref"].startswith("c58808b"), "версия osdk сменилась")
        self.assertRegex(self.osdk["ref"], r"^[0-9a-f]{40}$")

    def test_tc30_parts_and_folders(self):
        parts = self.osdk["parts"]
        for part in ("kick", "snare"):
            self.assertIn(part, parts, f"часть {part} этапа 5а пропала")
        for part, (folder, _) in OSDK_NEW_PARTS.items():
            with self.subTest(part=part):
                self.assertIn(part, parts)
                self.assertEqual(parts[part][0], folder)

    def test_tc30_patterns_accept_only_own_files(self):
        parts = self.osdk["parts"]
        for part, (_, own) in OSDK_NEW_PARTS.items():
            pattern = parts[part][1]
            for name in own:
                with self.subTest(part=part, file=name):
                    self.assertTrue(re.fullmatch(pattern, name), f"{name} не попадает в часть {part}")
            foreign = [f for p, (_, files) in OSDK_NEW_PARTS.items() if p != part for f in files]
            foreign += ["kick1.wav", "snare-top1.wav", "readme.txt", f"{own[0]}.bak"]
            for name in foreign:
                with self.subTest(part=part, foreign=name):
                    self.assertFalse(re.fullmatch(pattern, name), f"{name} попал в часть {part}")

    def test_tc30_case_from_card_chh_vs_hchh(self):
        # пример карточки: chh1.wav — да в hh-closed, hchh1.wav — нет
        pattern = self.osdk["parts"]["hh-closed"][1]
        self.assertTrue(re.fullmatch(pattern, "chh1.wav"))
        self.assertFalse(re.fullmatch(pattern, "hchh1.wav"))


# ---------- Условие 26 (этап 5в), ТК32: osdk поверх набора этапа 5а докачивает части ----------
#
# На воркере уже есть только osdk/kick и osdk/snare (установка этапа 5а). Повторная установка
# скачивает лишь недостающие части (hh-closed, hh-half, hh-open, ride, crash), kick/snare не
# перекачиваются; ответ — downloaded: true, parts — все 7 частей.

LOCAL_KICK = [0.95, 0.96, 0.97]    # пики «старых» файлов: отличаются от файлов GitHub
LOCAL_SNARE = [0.98, 0.99]


class TestOsdkUpgradeAddsParts(_KitCase):

    def setUp(self):
        super().setUp()
        self.kick = self._put_kit("osdk", "kick", LOCAL_KICK)
        self.snare = self._put_kit("osdk", "snare", LOCAL_SNARE)
        self.before = {str(p.relative_to(self.kits)): p.stat().st_mtime_ns
                       for p in (self.kits / "osdk").rglob("*.wav")}

    def test_tc32_downloads_only_missing_parts(self):
        r = self._install()
        self.assertEqual(r.status_code, 200, r.text)
        body = r.json()
        self.assertIs(body.get("downloaded"), True, "недостающие части не докачаны")
        self.assertEqual(body.get("parts"), {**ALL_PARTS, "kick": len(LOCAL_KICK), "snare": len(LOCAL_SNARE)})
        self.assertEqual(self.sockets, [])
        root = self.kits / "osdk"
        for part in ("hh-closed", "hh-half", "hh-open", "ride", "crash"):
            with self.subTest(part=part):
                self.assertEqual(sorted(self._peaks(root / part)), sorted(PART_PEAKS[part]))

    def test_tc32_kick_snare_not_redownloaded(self):
        self.assertEqual(self._install().status_code, 200)
        # файлы kick/snare — те же («старые» пики, тот же mtime, новых нет)
        self.assertEqual(sorted(self._peaks(self.kick)), LOCAL_KICK)
        self.assertEqual(sorted(self._peaks(self.snare)), LOCAL_SNARE)
        after = {str(p.relative_to(self.kits)): p.stat().st_mtime_ns
                 for d in (self.kick, self.snare) for p in d.glob("*.wav")}
        self.assertEqual(after, self.before, "файлы kick/snare перезаписаны")
        wavs = [urllib.parse.unquote(urllib.parse.urlsplit(u).path) for u in self.gh.calls
                if u.lower().split("?")[0].endswith(".wav")]
        self.assertTrue(wavs, "новые части не скачивались")
        for u in wavs:
            with self.subTest(url=u):
                self.assertNotRegex(u, r"/(kick|snare)/", "файл kick/snare скачан заново")

    def test_tc32_then_repeat_without_network(self):
        self.assertEqual(self._install().status_code, 200)
        self.gh.calls.clear()
        r = self._install()
        self.assertEqual(r.status_code, 200, r.text)
        self.assertIs(r.json().get("downloaded"), False)
        self.assertEqual(self.gh.calls, [], "набор полный, а установка снова ходит в сеть")
        self.assertEqual(sorted((k["name"], k["samples"]) for k in self._assets()["kits"]),
                         sorted([(n, c) for n, c in ALL_KITS if n not in ("osdk/kick", "osdk/snare")]
                                + [("osdk/kick", len(LOCAL_KICK)), ("osdk/snare", len(LOCAL_SNARE))]))


if __name__ == "__main__":
    unittest.main()

"""Run the Python and Go services over copies of the same local data."""

from __future__ import annotations

import argparse
import asyncio
import base64
from dataclasses import asdict
from difflib import unified_diff
import os
from pathlib import Path
from pprint import pformat
import shutil
import socket
import subprocess
import tempfile
import time
from unittest.mock import patch
import zipfile

import httpx

from app.archive.store import ArchiveStore
from app.cache.disk import DiskImageCache
from app.eh.models import GalleryMetadata, GalleryTag
from app.favorites.state import FavoritesSyncState
from app.main import app


ROOT = Path(__file__).resolve().parents[1]
GID = 12345
TOKEN = "abcdef0123"
KEY = f"{GID}:{TOKEN}"
IDENTITY = f"{GID}/{TOKEN}"
ARCHIVE = f"/api/archive/{IDENTITY}"
STREAM = f"/stream/{IDENTITY}/page/0"
CACHED_STREAM = f"/stream/{GID + 1}/{TOKEN}/page/0"
PUBLICATION = f"/opds/v2.0/gallery/{IDENTITY}/publication"
CHAPTERS = f"/opds/v1.2/gallery/{IDENTITY}/chapters"
PUBLIC_BASE = "https://catalog.example"
AUTH = ("parity", "fixture:password")
JPEG = base64.b64decode(
    "/9j/4AAQSkZJRgABAQAAAQABAAD/2wBDAP//////////////////////////////////////////////////"
    "////////////////////////////////////2wBDAf//////////////////////////////////////////"
    "////////////////////////////////////////////wAARCAABAAEDASIAAhEBAxEB/8QAFQABAQAAAAAA"
    "AAAAAAAAAAAAAAb/xAAUEAEAAAAAAAAAAAAAAAAAAAAA/9oADAMBAAIQAxAAAAGf/8QAFBABAAAAAAAAAAAA"
    "AAAAAAAAAP/aAAgBAQABBQJ//8QAFBEBAAAAAAAAAAAAAAAAAAAAAP/aAAgBAwEBPwF//8QAFBEBAAAAAAAA"
    "AAAAAAAAAAAAAP/aAAgBAgEBPwF//8QAFBABAAAAAAAAAAAAAAAAAAAAAP/aAAgBAQAGPwJ//8QAFBABAAAA"
    "AAAAAAAAAAAAAAAAAP/aAAgBAQABPyF//9oADAMBAAIAAwAAABAf/8QAFBABAAAAAAAAAAAAAAAAAAAAAP/a"
    "AAgBAwEBPxB//8QAFBABAAAAAAAAAAAAAAAAAAAAAP/aAAgBAgEBPxB//8QAFBABAAAAAAAAAAAAAAAAAAAA"
    "AP/aAAgBAQABPxB//9k="
)
PNG = base64.b64decode(
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jcJ0AAAAASUVORK5CYII="
)
CASES = [
    ("/health", False),
    ("/", True),
    ("/api/status", False),
    ("/api/status", True),
    ("/api/config", True),
    ("/api/home", True),
    ("/api/favorites/categories", True),
    ("/api/favorites/sync", True),
    ("/api/archive", True),
    (ARCHIVE, True),
    (f"{ARCHIVE}/metadata", True),
    (f"{ARCHIVE}/cover", True),
    (f"{ARCHIVE}/quote", True),
    ("/opds/v1.2", True),
    ("/opds/v1.2/search.xml", True),
    ("/opds/v1.2/archives", True),
    (CHAPTERS, True),
    ("/opds/v2.0", True),
    ("/opds/v2.0/search.xml", True),
    ("/opds/v2.0/archives", True),
    (PUBLICATION, True),
    (STREAM, False),
    (CACHED_STREAM, False),
    (f"/stream/{IDENTITY}/page/-1", False),
    (f"/image/{IDENTITY}/thumb", True),
    ("/image/fetch?url=https://example.invalid/cover.jpg", True),
]


async def seed(directory: Path) -> None:
    store = ArchiveStore(directory / "archives")
    await store.upsert(GID, TOKEN, {
        "status": "downloading",
        "title": "A local gallery",
        "quality": "Original",
        "or": "org",
        "upgrade_backup": {"or": "res", "quality": "Resample", "page_count": 2},
    })
    with zipfile.ZipFile(store.zip_path(GID, TOKEN), "w") as archive:
        archive.writestr("02.jpg", JPEG)
        archive.writestr("01.png", PNG)
    disk = DiskImageCache(directory / "cache")
    await disk.put(GID, TOKEN, 1, PNG)
    await disk.put(GID + 1, TOKEN, 1, PNG)
    await store.write_cover(GID, TOKEN, JPEG)
    metadata = GalleryMetadata(
        gid=GID, token=TOKEN, title="A local gallery", title_jpn="ローカル",
        category="Manga", thumb="", rating=4.5,
        tags={"language": [GalleryTag("language", "japanese")]},
        filecount=5, filesize=1234, posted=1700000000, uploader="fixture",
        torrentcount=0, expunged=False,
    )
    await store.write_metadata_snapshot(GID, TOKEN, {
        **asdict(metadata), "saved_at": 1700000000, "cover_mime": "image/jpeg",
    })
    state = FavoritesSyncState(directory / "favorites_sync.json")
    await state.update(known={KEY}, favcats={KEY: 2}, baseline=True)
    (directory / "home.toml").write_text(
        '[[section]]\nkind = "navigation"\ntype = "preset"\nquery = "archives"\n',
        encoding="utf-8",
    )


def environment(directory: Path) -> dict[str, str]:
    return {
        "PATH": os.environ["PATH"],
        "EH_SITE": "e-hentai",
        "IPB_MEMBER_ID": "",
        "IPB_PASS_HASH": "",
        "IGNEOUS": "",
        "AUTH_USERNAME": AUTH[0],
        "AUTH_PASSWORD": AUTH[1],
        "AUTH_EXEMPT_PREFIXES": "/stream/",
        "AUTH_EXEMPT_PATHS": "",
        "ARCHIVE_DIR": str(directory / "archives"),
        "CACHE_DIR": str(directory / "cache"),
        "HOME_CONFIG": str(directory / "home.toml"),
        "FAVORITES_SYNC_STATE": str(directory / "favorites_sync.json"),
        "MYTAGS_STATE": str(directory / "mytags.json"),
        "TAG_TRANSLATION_STATE": str(directory / "tag_translation.json"),
        "TAG_TRANSLATION_ENABLED": "0",
        "FAVORITES_SYNC_INTERVAL_SECONDS": "0",
        "FAVORITES_SYNC_ARCHIVE": "0",
        "PUBLIC_BASE_URL": PUBLIC_BASE,
        "PSE_PAGE_BASE": "0",
        "TIMEOUT_SECONDS": "0.5",
        "RETRIES": "0",
        "HTTP_PROXY": "http://127.0.0.1:1",
        "HTTPS_PROXY": "http://127.0.0.1:1",
        "NO_PROXY": "127.0.0.1",
        "PYTHON_DOTENV_DISABLED": "1",
    }


async def capture(client: httpx.AsyncClient) -> dict:
    return {
        (path, authenticated): await client.get(path, auth=AUTH if authenticated else None)
        for path, authenticated in CASES
    }


async def check_go_docs(client: httpx.AsyncClient) -> None:
    for path in ("/docs", "/openapi.json", "/docs/assets/init.js"):
        unauthorized = await client.get(path)
        assert unauthorized.status_code == 401, path
    page = await client.get("/docs", auth=AUTH)
    assert page.status_code == 200 and "./docs/assets/init.js" in page.text
    initializer = await client.get("/docs/assets/init.js", auth=AUTH)
    assert initializer.status_code == 200
    assert 'supportedSubmitMethods: ["get", "head"]' in initializer.text
    spec = await client.get("/openapi.json", auth=AUTH)
    assert spec.status_code == 200
    expected = (ROOT / "api" / "openapi.json").read_bytes()
    assert spec.content == expected
    print("PASS: Go docs require auth and serve the pinned read-only UI and API contract")


def offline(request: httpx.Request) -> httpx.Response:
    return httpx.Response(404, request=request)


async def python_reference(directory: Path) -> dict:
    with patch.dict(os.environ, environment(directory), clear=True):
        async with app.router.lifespan_context(app):
            upstream = app.state.service.client
            await upstream._client.aclose()
            upstream._client = httpx.AsyncClient(transport=httpx.MockTransport(offline))
            async with httpx.AsyncClient(
                transport=httpx.ASGITransport(app=app), base_url="http://python"
            ) as client:
                return await capture(client)


async def go_candidate(directory: Path, binary: Path) -> dict:
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        port = listener.getsockname()[1]
    env = environment(directory) | {"PORT": str(port)}
    with tempfile.TemporaryFile() as log:
        process = subprocess.Popen(
            [str(binary)], cwd=directory, env=env, stdout=log, stderr=log
        )
        try:
            async with httpx.AsyncClient(
                base_url=f"http://127.0.0.1:{port}", trust_env=False, timeout=5
            ) as client:
                deadline = time.monotonic() + 10
                while process.poll() is None and time.monotonic() < deadline:
                    try:
                        await client.get("/health")
                    except httpx.ConnectError:
                        await asyncio.sleep(0.05)
                    else:
                        await check_go_docs(client)
                        return await capture(client)
                log.seek(0)
                raise RuntimeError(f"Go service failed to start\n{log.read().decode()}")
        finally:
            process.terminate()
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()


def selected(value: dict, *keys: str) -> dict:
    return {key: value[key] for key in keys}


def stable_body(path: str, response: httpx.Response, *, python_reference=False) -> object:
    mime = response.headers.get("content-type", "")
    if path == "/" and response.status_code == 200:
        from lxml import html

        assert "text/html" in mime
        assert "PandaOPDS" in html.fromstring(response.content).findtext(".//title", default="")
        return "PandaOPDS administration page"
    if "json" not in mime:
        if "xml" in mime:
            from lxml import etree
            def normalize(element):
                text = (element.text or "").strip()
                if etree.QName(element).localname == "updated":
                    text = ""
                return element.tag, dict(element.attrib), text, list(map(normalize, element))

            return normalize(etree.fromstring(response.content))
        return response.content
    value = response.json()
    if response.status_code != 200:
        return value
    if path == "/health":
        return selected(value, "status", "config_error", "site", "host", "public_base_url")
    if path == "/api/status":
        return selected(value, "app", "site")
    if path == "/api/config":
        return {
            "derived": {key: value for key, value in value["derived"].items() if "path" not in key},
            "fields": [field["key"] for group in value["groups"] for field in group["fields"]],
        }
    if path == "/api/home":
        return selected(value, "using_file", "error", "groups", "sections")
    if path == "/api/archive":
        return {
            "stats": selected(value["stats"], "entries", "ready", "by_status"),
            "entries": [selected(entry, "gid", "token", "status") for entry in value["entries"]],
        }
    if path == ARCHIVE:
        return selected(value, "gid", "token", "status", "quality", "or", "page_count")
    if path in ("/opds/v2.0", "/opds/v2.0/archives"):
        value["metadata"].pop("modified", None)
    if path == "/opds/v2.0" and python_reference:
        # The baseline prepends PUBLIC_BASE_URL twice to home navigation.
        # Compare with a valid single prefix so a Go regression still fails.
        for link in value.get("navigation", []):
            if link["href"].startswith(PUBLIC_BASE * 2):
                link["href"] = link["href"][len(PUBLIC_BASE):]
    return value


def compare(python: dict, go: dict) -> None:
    mismatches = []
    for key, reference in python.items():
        path, authenticated = key
        candidate = go[key]
        label = f"{path} (auth={authenticated})"
        if candidate.status_code != reference.status_code:
            mismatches.append(f"{label}: status Go={candidate.status_code}, Python={reference.status_code}")
            continue
        for header in ("Content-Type", "Cache-Control", "WWW-Authenticate"):
            if candidate.headers.get(header) != reference.headers.get(header):
                mismatches.append(f"{label}: header {header} differs")
        try:
            expected = stable_body(path, reference, python_reference=True)
            actual = stable_body(path, candidate)
            if actual != expected:
                mismatches.append(f"{label}: response body differs")
                lines = unified_diff(
                    pformat(expected).splitlines(),
                    pformat(actual).splitlines(),
                    fromfile="Python",
                    tofile="Go",
                    n=1,
                    lineterm="",
                )
                for index, line in enumerate(lines):
                    if index == 24:
                        mismatches.append("…")
                        break
                    mismatches.append(line)
        except (KeyError, TypeError, ValueError) as exc:
            mismatches.append(f"{label}: invalid response shape ({exc})")
    if mismatches:
        raise SystemExit("\n".join(mismatches))
    print(f"PASS: {len(python)} offline HTTP comparisons")


async def run(binary: Path | None) -> None:
    with tempfile.TemporaryDirectory(prefix="pandaopds-go-parity-") as name:
        root = Path(name)
        reference_dir = root / "python"
        candidate_dir = root / "go"
        reference_dir.mkdir()
        await seed(reference_dir)
        shutil.copytree(reference_dir, candidate_dir)
        reference = await python_reference(reference_dir)
        assert reference[STREAM, False].content == JPEG
        assert reference[CACHED_STREAM, False].content == PNG
        assert reference[ARCHIVE, True].json()["or"] == "res"
        if binary is None:
            print(f"PASS: Python baseline captured {len(reference)} offline responses")
            return
        candidate = await go_candidate(candidate_dir, binary)
        compare(reference, candidate)
        # Exercise Python's production reader against the metadata written by Go.
        recovered = ArchiveStore(candidate_dir / "archives").get(GID, TOKEN)
        assert recovered is not None
        assert recovered["or"] == "res" and recovered["status"] == "ready"
        assert recovered["upgrade_backup"] == {}
        print("PASS: Python reopened the Go-recovered archive")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=lambda value: Path(value).resolve())
    asyncio.run(run(parser.parse_args().binary))

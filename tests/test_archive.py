"""Tests for the archive feature: archiver-page parsing + manager lifecycle.

Fixtures mirror the real archiver.php markup (tier forms with dltype/dlcheck,
hath.network status pages, ?start=1 download links).
"""

import asyncio
import io
import time
import zipfile

import pytest

from app.eh.parser import parse_archiver_page

# --------------------------------------------------------------------------
# archiver.php fixtures (real markup)
# --------------------------------------------------------------------------

TIERS_HTML = """
<!DOCTYPE html><html><body><div id="db">
<h1>Test Gallery Title [Artist]</h1>
<p>Current Funds:</p><p>161,479,373 GP [<a href="#">?</a>]</p>
<div style="width:180px; float:left">
  <div>Download Cost: &nbsp; <strong>Free!</strong></div>
  <form action="https://e-hentai.org/archiver.php?gid=4122641&amp;token=15037e1d97" method="post">
    <input type="hidden" name="dltype" value="org" />
    <div><input type="submit" name="dlcheck" value="Download Original Archive" /></div>
  </form>
  <p>Estimated Size: &nbsp; <strong>298.7 MiB</strong></p>
</div>
<div style="width:180px; float:right">
  <div>Download Cost: &nbsp; <strong>315,000 GP</strong></div>
  <form action="https://e-hentai.org/archiver.php?gid=4122641&amp;token=15037e1d97" method="post">
    <input type="hidden" name="dltype" value="res" />
    <div><input type="submit" name="dlcheck" value="Download Resample Archive" disabled="disabled" /></div>
  </form>
  <p>Estimated Size: &nbsp; <strong>N/A</strong></p>
</div>
</div></body></html>
"""

UNLOCKED_HTML = """
<!DOCTYPE html><html><body><div id="db">
<h1>Unlocked Gallery</h1>
<div>Download Cost: &nbsp; <strong>Free!</strong></div>
<form action="https://e-hentai.org/archiver.php?gid=1&amp;token=t" method="post">
  <input type="hidden" name="dltype" value="org" />
  <div><input type="submit" name="dlcheck" value="Download Original Archive" /></div>
</form>
<p>You unlocked an original download of this archive on 2026-08-16 09:20</p>
</div></body></html>
"""

TIERS_RES_HTML = """
<!DOCTYPE html><html><body><div id="db">
<h1>Res Tiers Gallery</h1>
<div>Download Cost: &nbsp; <strong>315,000 GP</strong></div>
<form action="https://e-hentai.org/archiver.php?gid=1&amp;token=t" method="post">
  <input type="hidden" name="dltype" value="res" />
  <div><input type="submit" name="dlcheck" value="Download Resample Archive" /></div>
</form>
</div></body></html>
"""

PREPARING_HTML = """
<!DOCTYPE html><html><body><div id="db">
<p>Locating archive server and preparing file for download...</p>
<p>(this can take several minutes)</p>
<p id="continue">(<a href="https://encvgvvzml.hath.network/archive/4122641/hash/abc/2">Click here if your browser does not continue automatically</a>)</p>
</div></body></html>
"""

READY_HTML = """
<!DOCTYPE html><html><body><div id="db">
<p>The file was successfully prepared, and is ready for download.<br /><br />
<strong>Test Gallery.zip</strong><br /><br />
<a href="/archive/4122641/hash/abc/2?start=1">Click here to download</a></p>
</div></body></html>
"""

NOT_MEMBER_HTML = """
<!DOCTYPE html><html><body><div id="db">
<p>You must be a Star member to use the archiver.</p>
</div></body></html>
"""

STATUS_URL = "https://encvgvvzml.hath.network/archive/4122641/hash/abc/2"
DL_URL = "https://encvgvvzml.hath.network/archive/4122641/hash/abc/2?start=1"


# --------------------------------------------------------------------------
# parser
# --------------------------------------------------------------------------

def test_tiers_options_and_prices():
    info = parse_archiver_page(TIERS_HTML)
    assert info.title == "Test Gallery Title [Artist]"
    assert info.gp_balance == 161479373
    assert info.error == ""
    assert info.download_state == ""
    assert len(info.options) == 2
    org = info.options[0]
    assert org.or_value == "org"
    assert org.label == "Original Archive"
    assert org.dlcheck == "Download Original Archive"
    assert org.gp_price == 0  # Free!
    assert org.size == "298.7 MiB"
    assert org.available is True
    res = info.options[1]
    assert res.or_value == "res"
    assert res.gp_price == 315000
    assert res.available is False  # disabled


def test_unlocked_flag():
    info = parse_archiver_page(UNLOCKED_HTML)
    assert info.options[0].unlocked is True


def test_preparing_state_extracts_hath_url():
    info = parse_archiver_page(PREPARING_HTML)
    assert info.download_state == "preparing"
    assert info.download_url == STATUS_URL


def test_ready_state_extracts_download_link():
    info = parse_archiver_page(READY_HTML, page_url=STATUS_URL)
    assert info.download_state == "ready"
    assert info.download_url == DL_URL


def test_not_member_error():
    info = parse_archiver_page(NOT_MEMBER_HTML)
    assert info.error == "Archiver requires Star membership"
    assert info.options == []


# --------------------------------------------------------------------------
# manager (mock client, real store/throttle)
# --------------------------------------------------------------------------

from app.archive import manager as manager_mod  # noqa: E402
from app.archive.manager import ArchiveManager  # noqa: E402
from app.archive.store import ArchiveStore, ST_FAILED, ST_READY  # noqa: E402
from app.config import Settings  # noqa: E402
from app.eh.exceptions import ArchiverUnavailableError, EHException  # noqa: E402
from app.eh.models import GalleryMetadata, GalleryTag  # noqa: E402
from app.throttle.limiter import Throttle  # noqa: E402


class FakeClient:
    """Minimal EHClient stand-in: scripted pages / URLs / archive bytes."""

    def __init__(self):
        self.pages = {}      # ("get"|"submit", gid, token) -> html
        self.urls = {}       # url -> html (hath status/ready pages)
        self.archives = {}   # url -> bytes (final download)
        self.submits = []
        self.session_calls = 0

    async def establish_session(self):
        self.session_calls += 1

    async def get_archiver_page(self, gid, token):
        return self.pages[("get", gid, token)]

    async def submit_archiver(self, gid, token, dltype, dlcheck):
        self.submits.append((gid, token, dltype, dlcheck))
        return self.pages[("submit", gid, token)]

    async def get_absolute_html(self, url, **kw):
        return self.urls[url]

    async def stream_archive(self, url, dest, *, progress_cb=None, offset=0):
        data = self.archives[url]
        dest.parent.mkdir(parents=True, exist_ok=True)
        if offset and offset < len(data):
            with dest.open("ab") as fh:
                fh.write(data[offset:])
            n = len(data)
        else:
            dest.write_bytes(data)
            n = len(data)
        if progress_cb:
            progress_cb(n)
        return n


def make_manager(tmp_path, **overrides):
    kwargs = {
        "ipb_member_id": "1",
        "ipb_pass_hash": "h",
        "archive_dir": tmp_path / "archives",
        "archive_download_concurrency": 2,
    }
    kwargs.update(overrides)
    settings = Settings(**kwargs)
    client = FakeClient()
    manager = ArchiveManager(
        settings, client=client, throttle=Throttle(settings),
        store=ArchiveStore(settings.archive_dir),
    )
    return settings, client, manager


def zip_bytes(names, payload=b"page-"):
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w") as zf:
        for i, name in enumerate(names):
            zf.writestr(name, payload + str(i).encode())
    return buf.getvalue()


async def wait_done(manager, gid, token, timeout=5.0):
    key = manager.store.key(gid, token)
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        t = manager._tasks.get(key)
        if t is None or t.done():
            if t is not None and not t.cancelled():
                t.result()  # surface task exceptions
            return
        await asyncio.sleep(0.01)
    raise AssertionError("archive task did not finish in time")


def wire_archive(client, gid=1, token="t", names=("a.jpg", "b.jpg", "c.jpg"),
                 tier=("org", "Download Original Archive")):
    """Wire the full mock chain: tiers page -> submit -> preparing -> ready -> zip."""
    client.pages[("get", gid, token)] = TIERS_HTML
    client.pages[("submit", gid, token)] = PREPARING_HTML
    client.urls[STATUS_URL] = READY_HTML
    client.archives[DL_URL] = zip_bytes(names)
    return tier


@pytest.mark.asyncio
async def test_start_requires_ipb(tmp_path):
    settings, client, manager = make_manager(tmp_path, ipb_member_id="", ipb_pass_hash="")
    with pytest.raises(ArchiverUnavailableError):
        await manager.start(1, "t")


@pytest.mark.asyncio
async def test_quote_returns_options(tmp_path):
    _, client, manager = make_manager(tmp_path)
    client.pages[("get", 1, "t")] = TIERS_HTML
    quote = await manager.quote(1, "t")
    assert quote["title"] == "Test Gallery Title [Artist]"
    assert quote["gp_balance"] == 161479373
    assert len(quote["options"]) == 2
    org = next(o for o in quote["options"] if o["or"] == "org")
    assert org["gp_price"] == 0 and org["available"] is True
    res = next(o for o in quote["options"] if o["or"] == "res")
    assert res["gp_price"] == 315000 and res["available"] is False


@pytest.mark.asyncio
async def test_start_full_lifecycle_to_ready(tmp_path):
    _, client, manager = make_manager(tmp_path)
    wire_archive(client)

    st = await manager.start(1, "t")  # default quality -> original -> org
    assert st["status"] == "pending"
    assert client.submits == [(1, "t", "org", "Download Original Archive")]
    await wait_done(manager, 1, "t")

    meta = manager.get_status(1, "t")
    assert meta["status"] == ST_READY
    assert meta["page_count"] == 3
    assert meta["quality"] == "Original Archive"
    # pages readable from the archived zip
    assert await manager.get_page_bytes(1, "t", 2) == b"page-1"
    assert await manager.get_page_bytes(1, "t", 4) is None
    # no .part left behind
    assert not manager.store.part_path(1, "t").exists()


@pytest.mark.asyncio
async def test_start_specific_quality(tmp_path):
    _, client, manager = make_manager(tmp_path)
    wire_archive(client, names=("p1.jpg",))
    client.pages[("get", 1, "t")] = TIERS_RES_HTML  # res available here
    await manager.start(1, "t", quality="res")
    assert client.submits == [(1, "t", "res", "Download Resample Archive")]
    await wait_done(manager, 1, "t")
    assert manager.get_status(1, "t")["status"] == ST_READY


@pytest.mark.asyncio
async def test_quality_matches_label_alias(tmp_path):
    _, client, manager = make_manager(tmp_path)
    wire_archive(client, names=("p1.jpg",))
    await manager.start(1, "t", quality="original")  # alias -> org
    assert client.submits[0][2] == "org"
    await wait_done(manager, 1, "t")


@pytest.mark.asyncio
async def test_7z_converted_to_zip_ready(tmp_path):
    pytest.importorskip("py7zr")
    import py7zr

    buf = io.BytesIO()
    with py7zr.SevenZipFile(buf, "w") as z:
        z.writestr(b"seven-0", "x.jpg")
        z.writestr(b"seven-1", "y.jpg")
    seven_bytes = buf.getvalue()

    _, client, manager = make_manager(tmp_path)
    client.pages[("get", 1, "t")] = TIERS_HTML
    client.pages[("submit", 1, "t")] = PREPARING_HTML
    client.urls[STATUS_URL] = READY_HTML
    client.archives[DL_URL] = seven_bytes

    await manager.start(1, "t")
    await wait_done(manager, 1, "t")

    meta = manager.get_status(1, "t")
    assert meta["status"] == ST_READY
    assert meta["page_count"] == 2
    assert await manager.get_page_bytes(1, "t", 1) == b"seven-0"
    assert await manager.get_page_bytes(1, "t", 2) == b"seven-1"


@pytest.mark.asyncio
async def test_unknown_format_fails(tmp_path):
    _, client, manager = make_manager(tmp_path)
    client.pages[("get", 1, "t")] = TIERS_HTML
    client.pages[("submit", 1, "t")] = PREPARING_HTML
    client.urls[STATUS_URL] = READY_HTML
    client.archives[DL_URL] = b"definitely not an archive"

    await manager.start(1, "t")
    await wait_done(manager, 1, "t")

    meta = manager.get_status(1, "t")
    assert meta["status"] == ST_FAILED
    assert "format" in (meta["error"] or "")


@pytest.mark.asyncio
async def test_remove_cancels_and_deletes(tmp_path):
    _, client, manager = make_manager(tmp_path)
    wire_archive(client)
    await manager.start(1, "t")
    await wait_done(manager, 1, "t")
    assert manager.get_status(1, "t")["status"] == ST_READY

    assert await manager.remove(1, "t") is True
    assert manager.get_status(1, "t")["status"] == "absent"
    assert not (tmp_path / "archives" / "1").exists()


@pytest.mark.asyncio
async def test_refresh_retriggers_download(tmp_path):
    _, client, manager = make_manager(tmp_path)
    wire_archive(client)
    await manager.start(1, "t")
    await wait_done(manager, 1, "t")
    assert len(client.submits) == 1

    await manager.refresh(1, "t")
    assert len(client.submits) == 2  # re-POST same tier
    await wait_done(manager, 1, "t")
    assert manager.get_status(1, "t")["status"] == ST_READY


# --------------------------------------------------------------------------
# service integration: /stream serves archived pages first
# --------------------------------------------------------------------------

from app.cache.disk import DiskImageCache  # noqa: E402
from app.eh.service import EHService  # noqa: E402


class NoUpstreamClient:
    """Any upstream call is a test failure: archive/disk must cover us."""

    async def establish_session(self):
        raise AssertionError("upstream session established")

    async def get_html(self, path, **kw):
        raise AssertionError("upstream HTML requested: " + str(path))

    async def get_absolute_html(self, url, **kw):
        raise AssertionError("upstream absolute HTML requested: " + url)

    async def post_api(self, payload):
        raise AssertionError("upstream API requested")

    async def fetch_image_bytes(self, url, **kw):
        raise AssertionError("upstream image requested: " + url)


class FakeArchive:
    """Archive manager stand-in: page 1 archived only."""

    async def get_page_bytes(self, gid, token, page_no_1):
        return b"ARCHIVE" if page_no_1 == 1 else None


@pytest.mark.asyncio
async def test_get_image_prefers_archive_over_disk(tmp_path):
    service = EHService(
        Settings(pse_page_base=1, image_cache_enabled=True),
        client=NoUpstreamClient(),
        disk_cache=DiskImageCache(tmp_path, max_gb=1.0, ttl_seconds=3600),
    )
    await service.disk.put(1, "t", 1, b"DISK")
    service.attach_archive(FakeArchive())

    data, mime = await service.get_image(1, "t", 1)
    assert data == b"ARCHIVE"  # archived page wins over the disk LRU
    assert mime == "application/octet-stream"


@pytest.mark.asyncio
async def test_get_image_archive_miss_falls_through_to_upstream(tmp_path):
    service = EHService(
        Settings(pse_page_base=1, image_cache_enabled=True),
        client=NoUpstreamClient(),
        disk_cache=DiskImageCache(tmp_path, max_gb=1.0, ttl_seconds=3600),
    )
    service.attach_archive(FakeArchive())
    # page 2 is not archived and not in the disk cache -> upstream must be hit
    with pytest.raises(AssertionError, match="upstream"):
        await service.get_image(1, "t", 2)


@pytest.mark.asyncio
async def test_get_image_pse_zero_based_maps_to_archive(tmp_path):
    service = EHService(
        Settings(pse_page_base=0, image_cache_enabled=True),
        client=NoUpstreamClient(),
        disk_cache=DiskImageCache(tmp_path, max_gb=1.0, ttl_seconds=3600),
    )
    service.attach_archive(FakeArchive())
    # PSE page 0 == EH page 1 -> archived
    data, _ = await service.get_image(1, "t", 0)
    assert data == b"ARCHIVE"


# --------------------------------------------------------------------------
# metadata snapshot (gdata + cover) — stub of the EHService surface used by
# ArchiveManager._snapshot_metadata / refresh_metadata
# --------------------------------------------------------------------------


class FakeMetaService:
    """Stub of EHService.get_metadata / fetch_cover_bytes."""

    def __init__(self, meta, fail_cover=False):
        self.meta = meta
        self.fail_cover = fail_cover
        self.calls = []  # (gid, token, force)

    async def get_metadata(self, gid, token, *, force=False):
        self.calls.append((gid, token, force))
        return self.meta

    async def fetch_cover_bytes(self, url):
        if self.fail_cover:
            raise EHException("cover fetch failed")
        return b"JPEGDATA", "image/jpeg"


META = GalleryMetadata(
    gid=1, token="t", title="Test Gallery Title [Artist]", title_jpn="",
    category="Doujinshi", thumb="https://ehgt.org/xx/1.jpg", rating=4.5,
    tags={"female": [GalleryTag("female", "foo")]}, filecount=3, filesize=1000,
    posted=1700000000, uploader="artist", torrentcount=0, expunged=False,
)


@pytest.mark.asyncio
async def test_archive_snapshots_metadata_after_ready(tmp_path):
    settings, client, manager = make_manager(tmp_path)
    wire_archive(client)
    fake = FakeMetaService(META)
    manager.service = fake

    await manager.start(1, "t")
    await wait_done(manager, 1, "t")

    meta = manager.store.get(1, "t")
    assert meta["status"] == "ready"
    assert meta["metadata_at"] > 0
    snap = manager.store.read_metadata_snapshot(1, "t")
    assert snap["title"] == META.title
    assert snap["category"] == "Doujinshi"
    assert snap["tags"]["female"][0]["key"] == "foo"  # full tag structure kept
    assert snap["cover_mime"] == "image/jpeg"
    assert manager.store.cover_path(1, "t").read_bytes() == b"JPEGDATA"
    # the snapshot must never break the archived page path
    assert await manager.get_page_bytes(1, "t", 1) == b"page-0"


@pytest.mark.asyncio
async def test_archive_snapshot_cover_failure_keeps_ready(tmp_path):
    settings, client, manager = make_manager(tmp_path)
    wire_archive(client)
    fake = FakeMetaService(META, fail_cover=True)
    manager.service = fake

    await manager.start(1, "t")
    await wait_done(manager, 1, "t")

    meta = manager.store.get(1, "t")
    assert meta["status"] == "ready"  # cover failure never fails the task
    assert meta["metadata_at"] > 0  # metadata still snapshotted
    snap = manager.store.read_metadata_snapshot(1, "t")
    assert snap["cover_mime"] == ""
    assert not manager.store.cover_path(1, "t").exists()


@pytest.mark.asyncio
async def test_archive_snapshot_gdata_none_still_ready(tmp_path):
    settings, client, manager = make_manager(tmp_path)
    wire_archive(client)
    manager.service = FakeMetaService(None)

    await manager.start(1, "t")
    await wait_done(manager, 1, "t")

    meta = manager.store.get(1, "t")
    assert meta["status"] == "ready"
    assert meta.get("metadata_at") is None


@pytest.mark.asyncio
async def test_refresh_metadata_force_overwrites_snapshot(tmp_path):
    settings, client, manager = make_manager(tmp_path)
    await manager.store.upsert(1, "t", {"title": "T", "status": "ready"})
    fake = FakeMetaService(META)
    manager.service = fake

    summary = await manager.refresh_metadata(1, "t")
    assert summary["filecount"] == 3
    assert summary["cover_mime"] == "image/jpeg"
    assert fake.calls[-1] == (1, "t", True)  # force bypasses the memory cache
    snap = manager.store.read_metadata_snapshot(1, "t")
    assert snap["saved_at"] > 0
    assert manager.store.get(1, "t")["metadata_at"] == snap["saved_at"]


@pytest.mark.asyncio
async def test_refresh_metadata_requires_entry(tmp_path):
    settings, client, manager = make_manager(tmp_path)
    manager.service = FakeMetaService(META)
    with pytest.raises(EHException):
        await manager.refresh_metadata(1, "t")


# --------------------------------------------------------------------------
# offline (source-independent) reading surface: ready archives render
# thumbnails / detail documents / the Archives shelf with zero upstream.
# --------------------------------------------------------------------------

from app.eh.models import DetailPageInfo, GalleryListItem  # noqa: E402


class _FakeState:
    def __init__(self, service, settings):
        self.service = service
        self.settings = settings
        self.archive = getattr(service, "archive", None)


class _FakeApp:
    def __init__(self, service, settings):
        self.state = _FakeState(service, settings)


class _FakeReq:
    def __init__(self, service, settings):
        self.app = _FakeApp(service, settings)


META_LANG = GalleryMetadata(
    gid=1, token="t", title="Test Gallery Title [Artist]", title_jpn="",
    category="Doujinshi", thumb="https://ehgt.org/xx/1.jpg", rating=4.5,
    tags={
        "language": [GalleryTag("language", "chinese")],
        "female": [GalleryTag("female", "foo")],
    },
    filecount=3, filesize=1234, posted=1700000000, uploader="artist",
    torrentcount=0, expunged=False,
)


async def make_ready_service(tmp_path, meta=None):
    """EHService (NoUpstreamClient) with one ready archive + snapshot + cover."""
    meta = meta or META_LANG
    settings, client, manager = make_manager(tmp_path)
    wire_archive(client)
    manager.service = FakeMetaService(meta)
    await manager.start(1, "t")
    await wait_done(manager, 1, "t")
    assert manager.get_status(1, "t")["status"] == ST_READY

    service = EHService(settings, client=NoUpstreamClient())
    service.attach_archive(manager)
    return service, manager


@pytest.mark.asyncio
async def test_build_detail_page_offline(tmp_path):
    _, manager = await make_ready_service(tmp_path)
    detail = manager.build_detail_page(1, "t")
    assert isinstance(detail, DetailPageInfo)
    assert detail.title == "Test Gallery Title [Artist]"
    assert detail.category == "Doujinshi"
    assert detail.language == "zh"            # BCP47 from snapshot language tag
    assert detail.image_count == 3            # zip page count (authoritative)
    assert detail.filesize_bytes == 1234
    assert detail.expunged is False
    assert [str(t) for t in detail.tags] == ["language:chinese", "female:foo"]


@pytest.mark.asyncio
async def test_list_ready_items_offline(tmp_path):
    _, manager = await make_ready_service(tmp_path)
    items = manager.list_ready_items()
    assert isinstance(items[0], GalleryListItem)
    assert items[0].gid == 1 and items[0].page_count == 3
    assert manager.ready_count() == 1


@pytest.mark.asyncio
async def test_get_detail_doc_offline_no_upstream(tmp_path):
    service, _ = await make_ready_service(tmp_path)
    detail = await service.get_detail_doc(1, "t")
    assert detail.image_count == 3 and detail.language == "zh"


@pytest.mark.asyncio
async def test_get_thumb_serves_archive_cover(tmp_path):
    service, _ = await make_ready_service(tmp_path)
    data, mime = await service.get_thumb(1, "t")
    assert data == b"JPEGDATA" and mime == "image/jpeg"


@pytest.mark.asyncio
async def test_archived_galleries_shelf(tmp_path):
    service, _ = await make_ready_service(tmp_path)
    info = await service.archived_galleries()
    assert len(info.galleries) == 1
    assert info.total_count == 1 and info.next_page is None


@pytest.mark.asyncio
async def test_v12_chapter_feed_offline(tmp_path):
    from app.opds.router import chapter_feed as v12_chapters

    service, _ = await make_ready_service(tmp_path)
    req = _FakeReq(service, service.settings)
    resp = await v12_chapters(req, 1, "t")
    body = resp.body.decode()
    assert "Chapter 1:" in body
    assert 'pse:count="3"' in body


@pytest.mark.asyncio
async def test_v2_publication_offline(tmp_path):
    from app.opds2.router import gallery_publication as v2_pub
    import json

    service, _ = await make_ready_service(tmp_path)
    req = _FakeReq(service, service.settings)
    resp = await v2_pub(req, 1, "t")
    doc = json.loads(resp.body.decode())
    assert doc["metadata"]["language"] == ["zh"]
    assert doc["metadata"]["numberOfPages"] == 3
    assert len(doc["readingOrder"]) == 3


@pytest.mark.asyncio
async def test_v2_archives_feed_offline(tmp_path):
    from app.opds2.router import archives_feed as v2_archives
    import json

    service, _ = await make_ready_service(tmp_path)
    req = _FakeReq(service, service.settings)
    resp = await v2_archives(req)
    doc = json.loads(resp.body.decode())
    assert doc["metadata"]["title"] == "E-Hentai: Archives"
    pubs = doc["publications"]
    assert len(pubs) == 1 and pubs[0]["metadata"]["numberOfPages"] == 3


@pytest.mark.asyncio
async def test_v12_root_nav_gates_archives(tmp_path):
    from app.opds.router import root_feed as v12_root

    # empty store -> no Archives entry
    settings, client, manager = make_manager(tmp_path)
    service = EHService(settings, client=NoUpstreamClient())
    service.attach_archive(manager)
    req = _FakeReq(service, settings)
    xml = (await v12_root(req)).body.decode()
    assert "Archives" not in xml

    # ready store -> Archives entry present
    _, manager = await make_ready_service(tmp_path)
    service = EHService(settings, client=NoUpstreamClient())
    service.attach_archive(manager)
    req = _FakeReq(service, settings)
    xml = (await v12_root(req)).body.decode()
    assert "/opds/v1.2/archives" in xml


@pytest.mark.asyncio
async def test_v2_root_archives_preset_store_gated(tmp_path):
    """Archives is a plain preset: declared position, hidden when the store
    is empty, never auto-injected when undeclared."""
    from app.opds2.router import root_feed as v2_root
    import json

    async def _doc(service, settings):
        resp = await v2_root(_FakeReq(service, settings))
        return json.loads(resp.body.decode())

    declared = tmp_path / "declared.toml"
    declared.write_text(
        '[[section]]\nkind = "navigation"\ntitle = "Latest"\ntype = "preset"\nquery = "latest"\n'
        '[[section]]\nkind = "navigation"\ntitle = "Archives"\ntype = "preset"\nquery = "archives"\n',
        encoding="utf-8",
    )
    undeclared = tmp_path / "undeclared.toml"
    undeclared.write_text(
        '[[section]]\nkind = "navigation"\ntitle = "Latest"\ntype = "preset"\nquery = "latest"\n',
        encoding="utf-8",
    )

    # declared + empty store -> hidden
    s1 = make_manager(tmp_path, home_config_path=declared)[0]
    svc1 = EHService(s1, client=NoUpstreamClient())
    svc1.attach_archive(make_manager(tmp_path)[2])
    assert [n["title"] for n in (await _doc(svc1, s1))["navigation"]] == ["Latest"]

    # declared + ready store -> shown at the declared position
    s2 = make_manager(tmp_path, home_config_path=declared)[0]
    _, manager2 = await make_ready_service(tmp_path)
    svc2 = EHService(s2, client=NoUpstreamClient())
    svc2.attach_archive(manager2)
    assert [n["title"] for n in (await _doc(svc2, s2))["navigation"]] == ["Latest", "Archives"]

    # undeclared + ready store -> never auto-injected
    s3 = make_manager(tmp_path, home_config_path=undeclared)[0]
    svc3 = EHService(s3, client=NoUpstreamClient())
    svc3.attach_archive(manager2)
    doc = await _doc(svc3, s3)
    assert [n["title"] for n in doc["navigation"]] == ["Latest"]
    assert not any(g["metadata"]["title"] == "Archives" for g in doc.get("groups", []))


@pytest.mark.asyncio
async def test_archives_shelf_prefers_title_jpn(tmp_path):
    """Shelf entries use the Japanese title when snapshotted, matching the
    detail documents (parse_detail_title preference)."""
    from dataclasses import replace
    from app.opds2.router import archives_feed as v2_archives
    from app.opds2.router import gallery_publication as v2_pub
    import json

    meta = replace(
        META_LANG,
        title="[Original Author] Original Title [Digital]",
        title_jpn="[\u8457\u8005] \u65e5\u672c\u8a9e\u30bf\u30a4\u30c8\u30eb [\u4e2d\u56fd\u7ffb\u8a33]",
    )
    service, _ = await make_ready_service(tmp_path, meta=meta)
    req = _FakeReq(service, service.settings)

    shelf = json.loads((await v2_archives(req)).body.decode())["publications"][0]
    detail = json.loads((await v2_pub(req, 1, "t")).body.decode())
    assert shelf["metadata"]["title"] == detail["metadata"]["title"]
    assert shelf["metadata"]["title"] == "\u65e5\u672c\u8a9e\u30bf\u30a4\u30c8\u30eb"
    assert shelf["metadata"]["authors"] == [{"name": "\u8457\u8005"}]

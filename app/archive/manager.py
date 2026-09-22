"""Archive lifecycle manager: quote / trigger / download / zip-convert / validate.

State machine (per gallery, persisted in meta.json):

    absent → pending (POST triggered, upstream preparing)
          → downloading (bytes → archive.part)
          → zipping (7z → zip conversion)
          → ready | failed (failed is retryable)

Concurrency: at most ``archive_download_concurrency`` active archive tasks
(download or zip-conversion) share a semaphore; the rest queue. Archive
traffic is NOT subject to the reading throttle pools, but the global circuit
breaker is always checked (banned / image-limit trips pause archiving too).
"""

from __future__ import annotations

import asyncio
import logging
import os
import time
import zipfile
from dataclasses import asdict
from datetime import datetime, timezone
from pathlib import Path

from ..config import Settings
from ..eh.client import EHClient
from ..eh.exceptions import (
    ArchiverUnavailableError,
    BannedError,
    EHException,
    ExceedLimitError,
    InsufficientGPError,
)
from ..eh.languages import map_language
from ..eh.parser import parse_archiver_page
from ..eh.models import (
    TAG_STATUS_CONFIDENCE,
    ArchiveOption,
    DetailPageInfo,
    GalleryListItem,
    GalleryTag,
    TagStyle,
)
from ..eh.service import EHService
from ..throttle.limiter import KIND_HTML, Throttle
from .store import ST_DOWNLOADING, ST_FAILED, ST_PENDING, ST_READY, ST_ZIPPING, ArchiveStore

try:  # py7zr is only needed for the rare 7z archives
    import py7zr  # type: ignore

    _HAS_PY7ZR = True
except ImportError:
    py7zr = None  # type: ignore
    _HAS_PY7ZR = False

logger = logging.getLogger(__name__)

# Fallback favcat map for archive gallery cards: last successfully fetched
# mapping (long-lived, survives upstream fetch failures).
_last_favcat_map: dict[int, str] = {}

_7Z_MAGIC = b"7z\xbc\xaf\x27\x1c"
_ZIP_MAGIC = b"PK\x03\x04"

# Preparing poll: EH generates large archives for minutes; poll every 20s up
# to 2h before giving up (the page itself suggests checking back later).
_PREPARE_POLL_SECONDS = 20.0
_PREPARE_TIMEOUT_SECONDS = 2 * 3600.0


# ---------------------------------------------------------------------------
# Local (offline) snapshot → feed-model conversions
# ---------------------------------------------------------------------------
# A ready archive is the long-term source of truth for /stream, the thumbnail
# proxy and the OPDS detail documents: everything a reader needs lives in the
# entry directory, so a gallery deleted upstream keeps serving fully offline.
# The gdata snapshot (metadata.json) is ``asdict(GalleryMetadata)`` JSON; the
# conversion helpers below turn it back into the typed feed models without
# touching the network.


def _snapshot_style(raw) -> TagStyle | None:
    """Rehydrate a serialized TagStyle (None when absent/empty)."""
    if not isinstance(raw, dict):
        return None
    style = TagStyle(
        color=str(raw.get("color") or ""),
        border_color=str(raw.get("border_color") or ""),
        background=str(raw.get("background") or ""),
    )
    return style if style.as_dict() else None


def _flatten_snapshot_tags(grouped) -> list[GalleryTag]:
    """Flatten the snapshot's ``{namespace: [GalleryTag-json, ...]}`` into a
    flat ``list[GalleryTag]`` (the shape the feed layer filters/sorts)."""
    out: list[GalleryTag] = []
    for namespace, entries in (grouped or {}).items():
        for raw in entries or []:
            if not isinstance(raw, dict):
                continue
            key = str(raw.get("key") or "")
            if not key:
                continue
            out.append(
                GalleryTag(
                    namespace=str(raw.get("namespace") or namespace),
                    key=key,
                    status=str(raw.get("status") or TAG_STATUS_CONFIDENCE),
                    style=_snapshot_style(raw.get("style")),
                )
            )
    return out


def _snapshot_language(grouped) -> str:
    """First `language:` tag key mapped to BCP 47 (mirrors
    GalleryMetadata.language); "" when none maps."""
    if not isinstance(grouped, dict):
        return ""
    for raw in grouped.get("language") or []:
        if isinstance(raw, dict):
            mapped = map_language(str(raw.get("key") or ""))
            if mapped:
                return mapped
    return ""


def _dt_text(unix_seconds: int) -> str:
    """Snapshot posted ts → the publish-time display string the feed layer's
    ISO parser understands ("%Y-%m-%d %H:%M", treated as UTC)."""
    if not unix_seconds:
        return ""
    return datetime.fromtimestamp(
        int(unix_seconds), tz=timezone.utc
    ).strftime("%Y-%m-%d %H:%M")


def _detect_archive_format(path: Path) -> str:
    """zip | 7z | unknown by magic bytes."""
    try:
        head = path.read_bytes()[:8]
    except OSError:
        return "unknown"
    if head[:4] == _ZIP_MAGIC:
        return "zip"
    if head[:6] == _7Z_MAGIC:
        return "7z"
    return "unknown"


class ArchiveManager:
    def __init__(
        self,
        settings: Settings,
        *,
        client: EHClient,
        throttle: Throttle,
        store: ArchiveStore,
        service: EHService | None = None,
    ):
        self.settings = settings
        self.client = client
        self.throttle = throttle
        self.store = store
        # Optional service layer: when injected, a gdata metadata + cover
        # snapshot is persisted after each successful archive download (and on
        # manual refresh). None keeps the manager self-contained (tests).
        self.service = service
        self._slots = asyncio.Semaphore(settings.archive_download_concurrency)
        self._tasks: dict[str, asyncio.Task] = {}
        self._progress: dict[str, int] = {}
        self._lock = asyncio.Lock()

    # -- availability ------------------------------------------------------

    @property
    def has_ipb(self) -> bool:
        """The archiver requires a logged-in session (Star membership)."""
        return bool(self.settings.ipb_member_id and self.settings.ipb_pass_hash)

    # -- errors ------------------------------------------------------------

    @staticmethod
    def _map_error(message: str) -> EHException:
        if "Star membership" in message:
            return ArchiverUnavailableError(message)
        if "Insufficient GP" in message:
            return InsufficientGPError(message)
        if "cannot be archived" in message:
            return ArchiverUnavailableError(message)
        return EHException(message)

    async def _trip_if_fatal(self, exc: EHException) -> None:
        if isinstance(exc, BannedError):
            await self.throttle.trip(
                f"{type(exc).__name__}: {exc}",
                cooldown=self.settings.banned_cooldown_seconds,
            )
        elif isinstance(exc, ExceedLimitError):
            await self.throttle.trip(
                f"{type(exc).__name__}: {exc}",
                cooldown=self.settings.exceed_cooldown_seconds,
            )

    # -- upstream access (HTML traffic: throttle + session + breaker) ------

    async def _archiver_get(self, gid: int, token: str) -> str:
        async with self.throttle.acquired(KIND_HTML):
            await self.client.establish_session()
            try:
                return await self.client.get_archiver_page(gid, token)
            except EHException as exc:
                await self._trip_if_fatal(exc)
                raise

    async def _archiver_submit(
        self, gid: int, token: str, dltype: str, dlcheck: str
    ) -> str:
        async with self.throttle.acquired(KIND_HTML):
            await self.client.establish_session()
            try:
                return await self.client.submit_archiver(gid, token, dltype, dlcheck)
            except EHException as exc:
                await self._trip_if_fatal(exc)
                raise

    async def _fetch_url(self, url: str) -> str:
        """GET an arbitrary (hath.network) status/download page."""
        async with self.throttle.acquired(KIND_HTML):
            await self.client.establish_session()
            return await self.client.get_absolute_html(url)

    # -- public API --------------------------------------------------------

    @staticmethod
    def _match_option(
        options: list[ArchiveOption], quality: str | None, default_quality: str
    ) -> ArchiveOption | None:
        """Pick a tier for the requested quality.

        Matches the ``dltype`` value exactly, then the tier label, then a
        known-name map (original->org, resample->res). Falls back to the
        configured default quality and finally to the single available tier.
        """
        quality = (quality or default_quality).strip().lower()
        if not options:
            return None
        for o in options:
            if o.or_value.lower() == quality:
                return o
        for o in options:
            if quality in o.label.lower():
                return o
        alias = {"original": "org", "resample": "res", "resized": "res"}
        if quality in alias:
            for o in options:
                if o.or_value.lower() == alias[quality]:
                    return o
        if len(options) == 1:
            return options[0]
        for o in options:
            if o.available:
                return o
        return options[0]

    async def quote(self, gid: int, token: str) -> dict:
        """Fetch the archiver page and return tiers + prices (no GP spent)."""
        if not self.has_ipb:
            raise ArchiverUnavailableError(
                "archiver requires IPB cookies (IPB_MEMBER_ID + IPB_PASS_HASH)"
            )
        html = await self._archiver_get(gid, token)
        page = parse_archiver_page(html)
        if page.error:
            raise self._map_error(page.error)
        return {
            "gid": gid,
            "token": token,
            "title": page.title,
            "gp_balance": page.gp_balance,
            "options": [
                {
                    "or": o.or_value,
                    "label": o.label,
                    "gp_price": o.gp_price,
                    "size": o.size,
                    "available": o.available,
                    "unlocked": o.unlocked,
                }
                for o in page.options
            ],
            "download_state": page.download_state,
            "download_url": page.download_url,
        }

    async def start(
        self, gid: int, token: str, quality: str | None = None, force: bool = False
    ) -> dict:
        """Trigger an archive download and start the background task.

        Idempotent without ``force``: entries that are already ready or
        still in flight (pending / downloading / zipping) are returned
        as-is — no upstream POST, no GP spent, no task respawn. Ready
        entries carry ``skipped: True`` so callers can tell the skip
        apart from a freshly triggered task. Failed / absent entries
        always run the normal flow (start retries a failed entry).

        With ``force=True`` every state re-runs the submit → pending →
        download pipeline (tier switch included). ``refresh()`` is a
        thin alias of this forced path reusing the stored tier.
        """
        if not self.has_ipb:
            raise ArchiverUnavailableError(
                "archiver requires IPB cookies (IPB_MEMBER_ID + IPB_PASS_HASH)"
            )
        if not force:
            meta = self.store.get(gid, token)
            if meta is not None:
                state = meta.get("status")
                if state in (ST_PENDING, ST_DOWNLOADING, ST_ZIPPING):
                    return self._status(gid, token)
                if state == ST_READY:
                    return {
                        **self._status(gid, token),
                        "skipped": True,
                        "reason": "already_archived",
                    }
        html = await self._archiver_get(gid, token)
        page = parse_archiver_page(html)
        if page.error:
            raise self._map_error(page.error)

        option = self._match_option(
            page.options, quality, self.settings.archive_quality
        )
        if option is None:
            raise EHException("archiver offered no tiers for this gallery")
        if not option.available:
            raise EHException(
                f"tier {option.label!r} is not available for this gallery"
            )

        submit_url = f"{self.settings.http_origin}/archiver.php?gid={gid}&token={token}"
        html = await self._archiver_submit(gid, token, option.or_value, option.dlcheck)
        page = parse_archiver_page(html, page_url=submit_url)
        if page.error:
            raise self._map_error(page.error)
        if not page.download_url:
            raise EHException(
                "archive submission returned no status URL "
                f"(state={page.download_state!r})"
            )

        await self.store.upsert(gid, token, {
            "title": page.title or None,
            "quality": option.label,
            "or": option.or_value,
            "gp_price": option.gp_price,
            "download_url": page.download_url,
            "status": ST_PENDING,
            "error": None,
        })
        self._spawn(gid, token)
        return self._status(gid, token)

    async def refresh(self, gid: int, token: str) -> dict:
        """Re-trigger the archive download (existing entry; same tier).

        Thin alias of ``start(force=True)``: requires an existing entry,
        reuses its stored tier, and always re-runs the submit → pending →
        download pipeline. Free/unlocked tiers cost no GP; a paid tier
        whose session already exists simply returns its status URL again.
        The local zip is replaced once the re-download finishes.
        """
        if not self.has_ipb:
            raise ArchiverUnavailableError(
                "archiver requires IPB cookies (IPB_MEMBER_ID + IPB_PASS_HASH)"
            )
        meta = self.store.get(gid, token)
        if meta is None:
            raise EHException("no archive entry to refresh (start first)")
        return await self.start(gid, token, quality=meta.get("or"), force=True)

    async def remove(self, gid: int, token: str) -> bool:
        """Cancel any in-flight task and delete the local entry."""
        key = self.store.key(gid, token)
        task = self._tasks.pop(key, None)
        if task is not None and not task.done():
            task.cancel()
            try:
                await task
            except asyncio.CancelledError:
                pass
        self._progress.pop(key, None)
        return await self.store.remove(gid, token)

    # -- metadata snapshot (gdata + cover, persisted locally) --------------

    async def _snapshot_metadata(
        self, gid: int, token: str, *, force: bool = False
    ) -> dict | None:
        """Fetch gdata metadata + cover and persist them as a local snapshot.

        Runs after a successful archive download (and on manual refresh).
        Best-effort by design: failures are logged and never fail the archive
        task — the entry stays ready; a missing snapshot is regenerated on
        the next manual refresh.
        """
        if self.service is None:
            return None
        try:
            meta = await self.service.get_metadata(gid, token, force=force)
            if meta is None:
                logger.warning(
                    "archive metadata snapshot skipped for %s:%s "
                    "(gdata returned nothing)", gid, token,
                )
                return None
            cover_mime = ""
            if meta.thumb:
                try:
                    data, cover_mime = await self.service.fetch_cover_bytes(meta.thumb)
                    await self.store.write_cover(gid, token, data)
                except EHException as exc:
                    # cover is optional: metadata snapshot is still persisted
                    logger.warning(
                        "archive cover snapshot failed for %s:%s (%s); "
                        "metadata kept", gid, token, exc,
                    )
            saved_at = time.time()
            payload = {
                **asdict(meta),
                "gid": gid,
                "token": token,
                "saved_at": saved_at,
                "cover_mime": cover_mime,
            }
            await self.store.write_metadata_snapshot(gid, token, payload)
            await self.store.upsert(gid, token, {"metadata_at": saved_at})
            return {
                "gid": gid,
                "token": token,
                "title": meta.title,
                "title_jpn": meta.title_jpn,
                "category": meta.category,
                "rating": meta.rating,
                "filecount": meta.filecount,
                "filesize": meta.filesize,
                "posted": meta.posted,
                "uploader": meta.uploader,
                "expunged": meta.expunged,
                "cover_mime": cover_mime or None,
                "saved_at": saved_at,
            }
        except Exception as exc:  # noqa: BLE001 - snapshot must never fail the task
            logger.warning(
                "archive metadata snapshot failed for %s:%s (%s)", gid, token, exc
            )
            return None

    async def refresh_metadata(self, gid: int, token: str) -> dict:
        """Force-refetch gdata metadata + cover and overwrite the snapshot."""
        meta = self.store.get(gid, token)
        if meta is None:
            raise EHException(
                "no archive entry to refresh metadata (start an archive first)"
            )
        summary = await self._snapshot_metadata(gid, token, force=True)
        if summary is None:
            raise EHException("metadata refresh failed (gdata returned nothing)")
        return summary

    async def get_metadata_snapshot(self, gid: int, token: str) -> dict | None:
        """Return the persisted gdata snapshot (None when not archived)."""
        return self.store.read_metadata_snapshot(gid, token)

    def get_status(self, gid: int, token: str) -> dict | None:
        return self._status(gid, token)

    def list_entries(self) -> list[dict]:
        return self.store.list_entries()

    def list_entries_enriched(
        self,
        fav_state: object | None = None,
        favcat_map: dict[int, str] | None = None,
    ) -> list[dict]:
        """Archive entries enriched with local metadata + favorites state for gallery cards.

        fav_state: FavoritesSyncState or None (for is_favorited).
        favcat_map: id->name mapping for precise "{id}: {name}" badge. Uses
        module-level fallback + state-persisted map on fetch failure (long-lived cache)."""
        global _last_favcat_map
        # merge live map into fallback
        if favcat_map is not None:
            _last_favcat_map = {**_last_favcat_map, **dict(favcat_map)}
        # also merge state-persisted map
        state_map: dict[int, str] = {}
        favcats_per_key: dict[str, int] = {}
        known: set[str] = set()
        if fav_state is not None:
            try:
                known = fav_state.known()  # type: ignore[union-attr]
            except Exception:
                known = set()
            try:
                favcats_per_key = fav_state.favcats()  # type: ignore[union-attr]
            except Exception:
                favcats_per_key = {}
            try:
                state_map = fav_state.favcat_map()  # type: ignore[union-attr]
                if state_map:
                    _last_favcat_map = {**_last_favcat_map, **state_map}
            except Exception:
                state_map = {}
        if favcat_map is None:
            favcat_map = _last_favcat_map
        else:
            # ensure fallback contains latest
            favcat_map = {**_last_favcat_map, **favcat_map}
        out: list[dict] = []
        for entry in self.store.list_entries():
            gid = int(entry.get("gid", 0))
            token = str(entry.get("token", ""))
            key = f"{gid}:{token}"
            snap = self.store.read_metadata_snapshot(gid, token)
            has_cover = self.store.cover_path(gid, token).exists()
            # normalize metadata for the card (ignore missing fields)
            meta = None
            if snap is not None:
                # tags grouped already; keep as-is for frontend grouping
                meta = {
                    "title": snap.get("title"),
                    "title_jpn": snap.get("title_jpn"),
                    "category": snap.get("category"),
                    "rating": snap.get("rating"),
                    "filecount": snap.get("filecount"),
                    "filesize": snap.get("filesize"),
                    "posted": snap.get("posted"),
                    "uploader": snap.get("uploader"),
                    "expunged": snap.get("expunged"),
                    "torrentcount": snap.get("torrentcount"),
                    "tags": snap.get("tags"),
                    "thumb": snap.get("thumb"),
                }
            is_fav = key in known
            favcat = favcats_per_key.get(key)
            favcat_name = favcat_map.get(favcat) if favcat is not None else None
            enriched = {
                **entry,
                "metadata": meta,
                "cover_url": f"/api/archive/{gid}/{token}/cover" if has_cover else None,
                "is_favorited": is_fav,
                "favcat": favcat,
                "favcat_name": favcat_name,
            }
            out.append(enriched)
        return out

    def get_cover_bytes(self, gid: int, token: str) -> tuple[bytes, str] | None:
        """Cover bytes + mime, or None when missing/unreadable."""
        p = self.store.cover_path(gid, token)
        try:
            data = p.read_bytes()
        except OSError:
            return None
        # cover_mime from snapshot if present, else sniff
        snap = self.store.read_metadata_snapshot(gid, token)
        mime = (snap or {}).get("cover_mime") if snap else None
        if mime and mime.startswith("image/"):
            return data, mime
        # fallback sniff (jpeg/png/webp)
        if data[:3] == b"\xff\xd8\xff":
            return data, "image/jpeg"
        if data[:8].startswith(b"\x89PNG"):
            return data, "image/png"
        if data[:4] == b"RIFF" and b"WEBP" in data[:12]:
            return data, "image/webp"
        return data, "image/jpeg"

    def stats(self) -> dict:
        return self.store.stats()

    # -- local (offline) read surface --------------------------------------

    def is_ready(self, gid: int, token: str) -> bool:
        """Whether a validated zip master exists for this gallery."""
        return self.store.is_ready(gid, token)

    def ready_count(self) -> int:
        """Number of ready entries (drives the Archives shelf visibility)."""
        return self.store.ready_count()

    def build_detail_page(self, gid: int, token: str) -> DetailPageInfo | None:
        """Detail-page info rendered purely from local files (zero upstream).

        Only for ready archives. Sources: ``meta.json`` (state machine: title,
        zip page count) + ``metadata.json`` (gdata snapshot: full tags,
        rating, uploader, posted, filesize ...). The advertised page count
        comes from the zip master itself — authoritative for what /stream can
        actually serve once the upstream gallery is gone. Returns None when
        the gallery has no ready archive.
        """
        if not self.store.is_ready(gid, token):
            return None
        meta = self.store.get(gid, token) or {}
        snap = self.store.read_metadata_snapshot(gid, token) or {}
        count = int(meta.get("page_count") or 0)
        if not count:
            count = int(snap.get("filecount") or 0)
        grouped = snap.get("tags")
        return DetailPageInfo(
            image_no_from=0,
            image_no_to=max(0, count - 1),
            image_count=count,
            current_page_no=0,
            page_count=(count + 19) // 20 if count else 0,
            thumbnails=[],
            tags=_flatten_snapshot_tags(grouped),
            title=str(snap.get("title") or meta.get("title") or ""),
            title_jpn=str(snap.get("title_jpn") or ""),
            category=str(snap.get("category") or ""),
            cover_url="",
            rating=float(snap.get("rating") or 0),
            uploader=str(snap.get("uploader") or ""),
            publish_time=_dt_text(int(snap.get("posted") or 0)),
            language=_snapshot_language(grouped),
            filesize_text="",
            filesize_bytes=int(snap.get("filesize") or 0) or None,
            torrent_count=int(snap.get("torrentcount") or 0),
            expunged=bool(snap.get("expunged")),
            comments=[],
        )

    def list_ready_items(self) -> list[GalleryListItem]:
        """Archived galleries as list-page items (ready only, newest first).

        Feeds the local "Archives" OPDS shelf — zero upstream. Page counts
        come from the zip masters; titles/metadata from the gdata snapshot
        when present (else the archiver-page title saved in meta.json).
        The display title prefers the Japanese title, mirroring
        ``parse_detail_title`` so shelf entries match the detail documents.
        """
        out: list[GalleryListItem] = []
        for meta in self.store.list_entries():
            if meta.get("status") != ST_READY:
                continue
            gid = int(meta.get("gid", 0))
            token = str(meta.get("token", ""))
            snap = self.store.read_metadata_snapshot(gid, token) or {}
            grouped = snap.get("tags")
            count = int(meta.get("page_count") or 0)
            if not count:
                count = int(snap.get("filecount") or 0)
            out.append(
                GalleryListItem(
                    gid=gid,
                    token=token,
                    title=str(snap.get("title_jpn") or snap.get("title") or meta.get("title") or ""),
                    category=str(snap.get("category") or ""),
                    cover_url="",
                    page_count=count or None,
                    rating=float(snap.get("rating") or 0),
                    publish_time=_dt_text(int(snap.get("posted") or 0)),
                    language=_snapshot_language(grouped),
                    is_expunged=bool(snap.get("expunged")),
                    tags=_flatten_snapshot_tags(grouped),
                )
            )
        return out

    async def get_page_bytes(self, gid: int, token: str, page_no_1: int) -> bytes | None:
        """Page bytes from the archived zip (None when not archived/ready)."""
        return await self.store.get_page_bytes(gid, token, page_no_1)

    # -- status assembly ---------------------------------------------------

    def _status(self, gid: int, token: str) -> dict:
        meta = self.store.get(gid, token)
        if meta is None:
            return {"gid": gid, "token": token, "status": "absent"}
        key = self.store.key(gid, token)
        return {
            **meta,
            "active": key in self._tasks and not self._tasks[key].done(),
            "bytes_downloaded": self._progress.get(key, meta.get("bytes", 0)),
        }

    # -- task scheduling ---------------------------------------------------

    def _spawn(self, gid: int, token: str) -> None:
        key = self.store.key(gid, token)
        # single-threaded event loop: no await points below -> atomic check+create
        if key in self._tasks and not self._tasks[key].done():
            return
        task = asyncio.create_task(self._run_entry(gid, token))

        def _done(t: asyncio.Task, k: str = key) -> None:
            self._tasks.pop(k, None)
            self._progress.pop(k, None)

        task.add_done_callback(_done)
        self._tasks[key] = task

    # -- background pipeline ----------------------------------------------

    async def _run_entry(self, gid: int, token: str) -> None:
        try:
            await self._run(gid, token)
        except asyncio.CancelledError:
            raise
        except Exception as exc:  # noqa: BLE001 - terminal failed state
            logger.warning("archive task failed for %s:%s (%s)", gid, token, exc)
            await self._fail(gid, token, str(exc))

    async def _run(self, gid: int, token: str) -> None:
        meta = self.store.get(gid, token)
        if meta is None:
            return
        status = meta["status"]
        if status == ST_PENDING:
            await self._wait_ready(gid, token)
        meta = self.store.get(gid, token)
        if meta is None or meta["status"] != ST_PENDING:
            return  # cancelled / failed / removed meanwhile
        await self._download(gid, token)
        await self._finalize(gid, token)
        # Best-effort gdata + cover snapshot (never fails the task).
        await self._snapshot_metadata(gid, token)

    async def _wait_ready(self, gid: int, token: str) -> None:
        """Poll the hath.network status URL until the archive is ready."""
        meta = self.store.get(gid, token)
        if meta is None:
            return
        url = meta.get("download_url", "")
        if not url:
            raise EHException("no status URL to poll")
        deadline = time.monotonic() + _PREPARE_TIMEOUT_SECONDS
        while time.monotonic() < deadline:
            try:
                html = await self._fetch_url(url)
            except EHException as exc:
                logger.warning("archive status poll failed (%s); retrying", exc)
            else:
                page = parse_archiver_page(html, page_url=url)
                if page.error:
                    raise self._map_error(page.error)
                if page.download_state == "ready" and page.download_url:
                    await self.store.upsert(gid, token, {
                        "download_url": page.download_url,
                    })
                    return
            await asyncio.sleep(_PREPARE_POLL_SECONDS)
        raise EHException("archive preparation timed out upstream")

    async def _download(self, gid: int, token: str) -> None:
        meta = self.store.get(gid, token)
        if meta is None:
            return
        url = meta.get("download_url", "")
        if not url or "?start=" not in url:
            raise EHException("no final download URL available")

        await self.store.upsert(gid, token, {"status": ST_DOWNLOADING, "error": None})
        part = self.store.part_path(gid, token)
        key = self.store.key(gid, token)

        async with self._slots:
            await self.throttle.circuit.check()
            await self.client.establish_session()
            offset = 0
            try:
                offset = part.stat().st_size
            except OSError:
                offset = 0
            self._progress[key] = offset

            def _on_progress(n: int) -> None:
                self._progress[key] = n

            total = 0
            try:
                total = await self.client.stream_archive(
                    url, part, progress_cb=_on_progress, offset=offset
                )
            finally:
                self._progress.pop(key, None)
            await self.store.upsert(gid, token, {
                "bytes": total,
                "total_bytes": total,
            })

    async def _finalize(self, gid: int, token: str) -> None:
        """Detect format, produce archive.zip, validate, mark ready."""
        part = self.store.part_path(gid, token)
        if not part.exists():
            raise EHException("download produced no file")
        fmt = _detect_archive_format(part)
        zip_path = self.store.zip_path(gid, token)

        if fmt == "zip":
            await asyncio.to_thread(os.replace, part, zip_path)
        elif fmt == "7z":
            await self.store.upsert(gid, token, {"status": ST_ZIPPING})
            ok = await self._convert_7z(part, zip_path)
            if not ok:
                raise EHException("7z conversion failed")
            await asyncio.to_thread(self._remove_file, part)
        else:
            # keep the unknown file for inspection, mark failed
            raise EHException(
                f"unknown archive format ({fmt}); file kept at {part.name}"
            )

        ok, count = await asyncio.to_thread(self._verify_zip, zip_path)
        if not ok:
            raise EHException("archive zip failed validation")
        await self.store.upsert(gid, token, {
            "status": ST_READY,
            "page_count": count,
            "total_bytes": zip_path.stat().st_size if zip_path.exists() else 0,
        })
        logger.info("archive ready: %s:%s (%d pages, %d bytes)",
                    gid, token, count, zip_path.stat().st_size if zip_path.exists() else 0)

    async def _convert_7z(self, src: Path, dst: Path) -> bool:
        """Re-package a 7z archive as zip, preserving internal (page) order."""
        if not _HAS_PY7ZR:
            logger.error("py7zr not installed; cannot convert 7z archive %s", src)
            return False
        tmpdir = src.parent / ".tmp7z"
        try:
            await asyncio.to_thread(tmpdir.mkdir, parents=True, exist_ok=True)

            def _extract() -> list[str]:
                with py7zr.SevenZipFile(src) as z:
                    names = z.getnames()
                    z.extractall(tmpdir)
                    return names

            names = await asyncio.to_thread(_extract)

            def _repack() -> None:
                with zipfile.ZipFile(dst, "w", zipfile.ZIP_STORED) as zf:
                    for name in names:
                        src_file = tmpdir / name
                        zf.write(src_file, name)

            await asyncio.to_thread(_repack)
            return True
        except Exception as exc:  # noqa: BLE001
            logger.warning("7z -> zip conversion failed for %s (%s)", src, exc)
            return False
        finally:
            await asyncio.to_thread(self._remove_tree, tmpdir)

    def _verify_zip(self, path: Path) -> tuple[bool, int]:
        """Verify the zip opens, has entries, and the first page is readable."""
        try:
            with zipfile.ZipFile(path) as zf:
                names = zf.namelist()
                if not names:
                    return False, 0
                zf.read(names[0])  # spot-check the first member's data
                return True, len(names)
        except (OSError, zipfile.BadZipFile, KeyError, RuntimeError):
            return False, 0

    @staticmethod
    def _remove_file(path: Path) -> None:
        try:
            path.unlink()
        except OSError:
            pass

    @staticmethod
    def _remove_tree(path: Path) -> None:
        if not path.exists():
            return
        for child in path.iterdir():
            if child.is_dir():
                ArchiveManager._remove_tree(child)
            else:
                try:
                    child.unlink()
                except OSError:
                    pass
        try:
            path.rmdir()
        except OSError:
            pass

    async def _fail(self, gid: int, token: str, message: str) -> None:
        await self.store.upsert(gid, token, {
            "status": ST_FAILED,
            "error": message[:500],
        })

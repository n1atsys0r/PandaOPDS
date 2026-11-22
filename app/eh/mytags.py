"""Static tag-style map scraped from the account's My Tags page.

The detail-page ``#taglist`` carries no inline styles, so highlight colours
for detail documents come from this map: /mytags is parsed (``parse_mytags``)
into a ``namespace:key -> TagStyle`` table, kept in memory behind a TTL and
persisted to a JSON file (atomic write) so
a restart doesn't immediately re-hit the upstream.

Refresh is lazy only: the map is re-fetched when a reader asks for it past
the TTL (no background loop). A successful refresh with zero styled tags is
still recorded — an empty account must not hammer upstream on every request.
"""

from __future__ import annotations

import asyncio
import json
import logging
import os
import time
from pathlib import Path

from .models import TagStyle

logger = logging.getLogger(__name__)


class MyTagsMap:
    """TTL-cached, disk-persisted tag -> style table (async-safe)."""

    def __init__(self, path: str | Path, ttl_seconds: float):
        self.path = Path(path)
        self.ttl = ttl_seconds
        self.refresh_lock = asyncio.Lock()
        self._styles: dict[str, TagStyle] = {}
        self._saved_at: float = 0.0
        self._loaded = False  # a successful fetch/parse happened at least once
        self._load()

    # -- internals ---------------------------------------------------------

    def _load(self) -> None:
        """Restore the persisted snapshot (corrupt/missing file → empty)."""
        try:
            data = json.loads(self.path.read_text(encoding="utf-8"))
            styles = data.get("styles")
            saved_at = float(data.get("saved_at") or 0.0)
            if isinstance(styles, dict):
                self._styles = {
                    str(k): TagStyle(
                        color=str(v.get("color") or ""),
                        border_color=str(v.get("borderColor") or ""),
                        background=str(v.get("background") or ""),
                    )
                    for k, v in styles.items()
                    if isinstance(v, dict)
                }
                self._saved_at = saved_at
                self._loaded = True
        except (OSError, json.JSONDecodeError, ValueError, TypeError):
            pass

    async def _atomic_write(self) -> None:
        tmp = self.path.with_suffix(".json.tmp")
        payload = {
            "styles": {k: v.as_dict() for k, v in sorted(self._styles.items())},
            "saved_at": self._saved_at,
        }
        await asyncio.to_thread(
            tmp.write_text,
            json.dumps(payload, ensure_ascii=False, indent=2),
            "utf-8",
        )
        await asyncio.to_thread(os.replace, tmp, self.path)

    # -- read API ----------------------------------------------------------

    def get(self) -> dict[str, TagStyle]:
        """Current map (possibly stale; possibly empty before first fetch)."""
        return dict(self._styles)

    def stale(self) -> bool:
        """True when the next reader should trigger an upstream refresh."""
        if not self._loaded:
            return True
        return self.ttl <= 0 or (time.time() - self._saved_at) >= self.ttl

    # -- mutations ---------------------------------------------------------

    async def replace(self, styles: dict[str, TagStyle]) -> None:
        """Swap in a freshly parsed map and persist it atomically."""
        self._styles = dict(styles)
        self._saved_at = time.time()
        self._loaded = True
        try:
            self.path.parent.mkdir(parents=True, exist_ok=True)
            await self._atomic_write()
        except OSError as exc:
            logger.warning("mytags state write failed (%s): %s", self.path, exc)

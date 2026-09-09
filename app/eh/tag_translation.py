"""Chinese tag translation backed by the EhTagTranslation/Database dictionary.

Source: https://github.com/EhTagTranslation/Database (release asset
``db.text.json``). When enabled, OPDS 2.0 subject entries are rendered with
Chinese translations ("中文命名空间:译名", emoji stripped) and free-text search
queries containing translated tag names are rewritten into native E-Hentai
keyword syntax before hitting upstream.

Design notes:

- GitHub is NOT E-Hentai traffic: it bypasses the throttle pool by design and
  uses its own client with a relaxed read timeout (the asset is ~10-15 MB).
  Failures degrade gracefully — the previous snapshot keeps serving, requests
  never fail because of the translator.
- The persisted snapshot (atomic JSON write, same scheme as MyTagsMap) means
  restarts don't re-download; a corrupt/missing file just means the first
  refresh fetches upstream.
- Three lookup tables, all built from one cleaned (emoji-stripped) tag map so
  forward and reverse lookups always agree:
    forward   ("ns","key")            -> display string "中文ns:译名"
    ns map    中文ns / 英文ns (lower)  -> english namespace
    reverse   prefixed/bare 译名      -> (english ns, original key)
- Search rewriting is deliberately conservative: a token translates ONLY on an
  exact case-insensitive match of the translated name. Bare translated words
  must hit exactly ONE candidate across namespaces — anything ambiguous (or
  unknown) passes through verbatim as a standard keyword. Quoted phrases never
  translate.
"""

from __future__ import annotations

import asyncio
import json
import logging
import os
import re
import time
from pathlib import Path

import httpx

from ..config import Settings

logger = logging.getLogger(__name__)

DEFAULT_DB_URL = (
    "https://github.com/EhTagTranslation/Database/releases/latest/download/db.text.json"
)

# Emoji/symbol ranges that appear inside community translations but must never
# reach OPDS output or search lookups: supplementary emoji planes, misc
# symbols/dingbats, arrows&stars blocks, variation selectors, ZWJ joiners and
# keycap combining marks. CJK text is intentionally outside every range.
_EMOJI_RE = re.compile(
    "[\U0001F000-\U0001FFFF\u2600-\u27BF\u2B00-\u2BFF\uFE0F\u200D\u20E3]"
)
_WS_RE = re.compile(r"\s+")

# Extra namespace search aliases accepted by E-Hentai's own keyword syntax
# but absent from the db's frontMatters.abbr (which only carries f/m/x/a/p/
# c/g/l/r/o/cos/loc). Merged into the ns lookup with setdefault — never
# shadows db-provided abbreviations or full names.
_NS_ALIAS_SUPPLEMENT: dict[str, str] = {
    "char": "character",
    "circle": "group",
    "lang": "language",
    "series": "parody",
}

# Curly / full-width double-quote variants that iOS/macOS smart punctuation
# emits (U+201C/U+201D/U+201E, U+00AB/U+00BB, U+FF02). Only double quotes
# are normalized — single quotes stay verbatim (EH syntax uses only ").
_DOUBLE_QUOTE_RE = re.compile("[\u201c\u201d\u201e\u00ab\u00bb\uff02]")
_COLON_SPACE_QUOTE_RE = re.compile(r':\s+"')


def _normalize_query(query: str) -> str:
    """Normalize double-quote variants and ``ns: "phrase"`` spacing."""
    query = _DOUBLE_QUOTE_RE.sub('"', query)
    query = _COLON_SPACE_QUOTE_RE.sub(':"', query)
    return query


# Whitespace tokenizer that keeps double-quoted phrases as single tokens
# (same scheme as query_ext._TOKEN_RE). Also accepts ``ns:"phrase"`` forms.
_TOKEN_RE = re.compile(r'[^\s"]+:"[^"]*"|"[^"]*"|\S+')
_PREFIXED_QUOTED_RE = re.compile(r'([^:\s"]+):"([^"]*)"')


def clean_name(name: str) -> str:
    """Strip emoji and collapse whitespace from a translated name."""
    return _WS_RE.sub(" ", _EMOJI_RE.sub("", name)).strip()


def parse_db_text(
    payload: dict,
) -> tuple[dict[str, str], dict[str, str], dict[str, str]]:
    """Extract ``(namespaces, tags, abbrs)`` from a parsed db.text.json doc.

    Real release structure (verified 2026-08)::

        {"data": [ {"namespace": "female",
                    "frontMatters": {"name": "女性", "abbr": "f", ...},
                    "data": {"<raw tag>": {"name": "译名", ...}, ...}}, ...] }

    ``data`` is a LIST of namespace buckets; the Chinese namespace name lives
    in each bucket's ``frontMatters.name``, its search abbreviation in
    ``frontMatters.abbr`` (f/m/x/...), and tags sit in ``bucket.data``.
    The meta-namespace ``rows`` (table of contents) is skipped. The legacy
    dict shape from the wiki (``data.<ns>.raw.<key>`` + ``data.ns.raw``) is
    still accepted for robustness (no abbreviations there). Unknown shapes
    degrade to empty maps (the old snapshot keeps serving).
    """
    data = payload.get("data")
    if isinstance(data, list):
        buckets = [(b.get("namespace"), b) for b in data if isinstance(b, dict)]
    elif isinstance(data, dict):
        buckets = list(data.items())
    else:
        return {}, {}, {}

    namespaces: dict[str, str] = {}
    tags: dict[str, str] = {}
    abbrs: dict[str, str] = {}
    for ns, bucket in buckets:
        ns = str(ns or "")
        if not ns or ns in ("ns", "rows"):
            continue
        # Namespace translation + search abbreviation: frontMatters (new);
        # data.ns.raw (legacy, no abbreviations there)
        fm = bucket.get("frontMatters")
        if isinstance(fm, dict):
            cleaned = clean_name(str(fm.get("name") or ""))
            if cleaned:
                namespaces[ns] = cleaned
            abbr = str(fm.get("abbr") or "").strip()
            if abbr:
                abbrs[abbr.lower()] = ns
        raw = bucket.get("raw") or bucket.get("data")
        if not isinstance(raw, dict):
            continue
        for key, entry in raw.items():
            if not isinstance(entry, dict):
                continue
            cleaned = clean_name(str(entry.get("name") or ""))
            if not cleaned:
                continue
            tags[f"{ns}:{key}"] = cleaned
    # Legacy shape kept namespace translations under data.ns.raw
    if isinstance(data, dict):
        ns_raw = data.get("ns")
        if isinstance(ns_raw, dict):
            for en, entry in (ns_raw.get("raw") or {}).items():
                if not isinstance(entry, dict):
                    continue
                cleaned = clean_name(str(entry.get("name") or ""))
                if cleaned:
                    namespaces.setdefault(str(en), cleaned)
    return namespaces, tags, abbrs


class TagTranslator:
    """EhTagTranslation lookup tables with periodic refresh + disk snapshot."""

    def __init__(self, settings: Settings):
        self.settings = settings
        self.url = settings.tag_translation_url or DEFAULT_DB_URL
        self._client: httpx.AsyncClient | None = None
        self._task: asyncio.Task | None = None
        self._refresh_lock = asyncio.Lock()
        # english ns -> cleaned chinese display ns (falls back to english)
        self.namespaces: dict[str, str] = {}
        # search abbreviation -> english ns (f->female, m->male, ...)
        self.abbrs: dict[str, str] = {}
        # "ns:key" (original casing preserved) -> cleaned translated name
        self.tags: dict[str, str] = {}
        # --- derived lookup tables ---
        self._forward: dict[tuple[str, str], tuple[str, str]] = {}
        self._display_ns: dict[str, str] = {}
        self._ns_lookup: dict[str, str] = {}
        self._reverse_prefixed: dict[tuple[str, str], tuple[str, str]] = {}
        self._reverse_bare: dict[str, list[tuple[str, str]]] = {}
        self.saved_at: float = 0.0
        self.last_error: str | None = None
        self._load()

    # -- derived tables ----------------------------------------------------

    def _rebuild(self) -> None:
        """Recompute all lookup tables from ``namespaces`` + ``tags``."""
        self._display_ns = {}
        for en, cn in self.namespaces.items():
            self._display_ns[en.lower()] = cn or en
        self._ns_lookup = {}
        for en, cn in self.namespaces.items():
            self._ns_lookup[en.lower()] = en
            if cn:
                self._ns_lookup[cn.lower()] = en
        # Search abbreviations from db frontMatters (f/m/x/...) plus EH-native
        # alias syntax (circle:/series:/lang:/char:). Aliases never shadow
        # full namespace names or db abbreviations (setdefault).
        for abbr, en in self.abbrs.items():
            self._ns_lookup.setdefault(abbr.lower(), en)
        for alias, en in _NS_ALIAS_SUPPLEMENT.items():
            self._ns_lookup.setdefault(alias, en)

        self._forward = {}
        self._reverse_prefixed = {}
        self._reverse_bare = {}
        for full, name in sorted(self.tags.items()):
            ns, _, key = full.partition(":")
            if not ns or not key:
                continue
            display_ns = self.namespaces.get(ns) or ns
            self._forward[(ns.lower(), key.lower())] = (display_ns, name)
            rev_key = (ns.lower(), name.lower())
            self._reverse_prefixed.setdefault(rev_key, (ns, key))
            self._reverse_bare.setdefault(name.lower(), []).append((ns, key))

    def _install(
        self,
        namespaces: dict[str, str],
        tags: dict[str, str],
        abbrs: dict[str, str] | None = None,
    ) -> None:
        """Swap in freshly parsed tables, rebuild lookups and persist.

        Called from ``refresh`` (via ``asyncio.to_thread``): parsing + JSON
        dump of the multi-MB document must not stall the event loop.
        """
        ns_out: dict[str, str] = {}
        for en, raw in namespaces.items():
            cleaned = clean_name(str(raw))
            ns_out[str(en)] = cleaned or str(en)
        self.namespaces = ns_out
        self.abbrs = {
            str(k).lower(): str(v)
            for k, v in (abbrs or {}).items()
            if k and v
        }
        tags_out: dict[str, str] = {}
        for full, raw in tags.items():
            cleaned = clean_name(str(raw))
            if cleaned:
                tags_out[full] = cleaned
        self.tags = tags_out
        self.saved_at = time.time()
        self._rebuild()
        try:
            self.path.parent.mkdir(parents=True, exist_ok=True)
            tmp = self.path.with_suffix(".json.tmp")
            payload = {
                "namespaces": dict(sorted(self.namespaces.items())),
                "abbrs": dict(sorted(self.abbrs.items())),
                "tags": dict(sorted(self.tags.items())),
                "saved_at": self.saved_at,
            }
            tmp.write_text(json.dumps(payload, ensure_ascii=False), encoding="utf-8")
            os.replace(tmp, self.path)
        except OSError as exc:
            logger.warning("tag translation state write failed (%s)", exc)

    # -- persistence -------------------------------------------------------

    @property
    def path(self) -> Path:
        return Path(self.settings.tag_translation_state)

    def _load(self) -> None:
        """Restore the persisted snapshot (corrupt/missing file -> empty)."""
        try:
            data = json.loads(self.path.read_text(encoding="utf-8"))
            namespaces = data.get("namespaces")
            tags = data.get("tags")
            if isinstance(namespaces, dict) and isinstance(tags, dict):
                self.namespaces = {str(k): str(v) for k, v in namespaces.items()}
                abbrs = data.get("abbrs")
                self.abbrs = (
                    {str(k): str(v) for k, v in abbrs.items()}
                    if isinstance(abbrs, dict)
                    else {}
                )
                self.tags = {str(k): str(v) for k, v in tags.items()}
                self.saved_at = float(data.get("saved_at") or 0.0)
                self._rebuild()
        except (OSError, json.JSONDecodeError, ValueError, TypeError):
            pass

    # -- lifecycle ---------------------------------------------------------

    def start(self) -> None:
        """Spawn the refresh loop: immediate first refresh, then per interval.

        ``TAG_TRANSLATION_INTERVAL_SECONDS=0`` still performs the startup
        refresh once, then stops (no periodic loop).
        """
        if self._task is not None and not self._task.done():
            return
        self._task = asyncio.create_task(self._loop())

    async def stop(self) -> None:
        if self._task is not None:
            self._task.cancel()
            try:
                await self._task
            except asyncio.CancelledError:
                pass
            self._task = None
        if self._client is not None:
            await self._client.aclose()
            self._client = None

    async def _loop(self) -> None:
        interval = self.settings.tag_translation_interval_seconds
        logger.info("tag translation loop started (every %.0fs)", interval)
        while True:
            await self.refresh()
            if interval <= 0:
                return
            await asyncio.sleep(interval)

    def _get_client(self) -> httpx.AsyncClient:
        if self._client is None:
            timeout = httpx.Timeout(
                self.settings.timeout_seconds * 10,
                connect=self.settings.timeout_seconds,
            )
            self._client = httpx.AsyncClient(timeout=timeout, follow_redirects=True)
        return self._client

    async def refresh(self) -> bool:
        """Fetch + install a fresh database; False on any failure (old
        snapshot keeps serving; error recorded in ``last_error``)."""
        async with self._refresh_lock:
            try:
                resp = await self._get_client().get(self.url)
                resp.raise_for_status()
                namespaces, tags, abbrs = parse_db_text(resp.json())
                if not tags:
                    raise ValueError("db.text.json contained no tags")
                await asyncio.to_thread(self._install, namespaces, tags, abbrs)
                self.last_error = None
                logger.info(
                    "tag translation updated: %d tags, %d namespaces",
                    len(tags),
                    len(namespaces),
                )
                return True
            except Exception as exc:  # noqa: BLE001 - degrade, keep serving
                self.last_error = str(exc)
                logger.warning("tag translation refresh failed: %s", exc)
                return False

    # -- read API ----------------------------------------------------------

    @property
    def loaded(self) -> bool:
        return bool(self.tags)

    def stale(self) -> bool:
        """True when no snapshot exists or the TTL has elapsed."""
        if not self.loaded:
            return True
        interval = self.settings.tag_translation_interval_seconds
        return interval > 0 and (time.time() - self.saved_at) >= interval

    def translate_namespace(self, namespace: str) -> str | None:
        """Chinese display namespace for ``namespace``; None when unknown.

        Lookup is case-insensitive and also accepts already-translated
        (Chinese) names and search abbreviations / EH aliases (``f``,
        ``circle:``...) via the shared ``_ns_lookup`` table, so an
        already-Chinese prefix round-trips to itself instead of missing.
        """
        if not namespace:
            return None
        hit = self._display_ns.get(namespace.lower())
        if hit is not None:
            return hit
        en = self._ns_lookup.get(namespace.lower())
        if en is not None:
            return self._display_ns.get(en.lower())
        return None

    def display_tag(self, namespace: str, key: str) -> str | None:
        """Best-effort display string for a tag.

        Full translation (``\u4e2d\u6587ns:\u8bd1\u540d``) when the ``(ns, key)``
        pair is known; otherwise a namespace-only fallback
        (``\u4e2d\u6587ns:\u539f\u6587key``) when the namespace is known
        (the overwhelmingly common case for proper nouns such as
        ``artist:<name>`` / ``group:<name>``); None only when even the
        namespace is unknown. The original ``key`` casing is preserved
        verbatim — matching stays case-insensitive downstream.
        """
        hit = self._forward.get((namespace.lower(), key.lower()))
        if hit is not None:
            display_ns, name = hit
            return f"{display_ns}:{name}"
        display_ns = self.translate_namespace(namespace)
        if display_ns is None:
            return None
        return f"{display_ns}:{key}"

    def translate_tag(self, namespace: str, key: str) -> str | None:
        """Display string ``中文命名空间:译名`` for a tag; None when untranslatable.

        The namespace prefix always travels with the translation so clients
        can round-trip it back through search disambiguation.
        """
        hit = self._forward.get((namespace.lower(), key.lower()))
        if hit is None:
            return None
        display_ns, name = hit
        return f"{display_ns}:{name}"

    def translate_query(self, query: str) -> str:
        """Rewrite translated tag tokens into native EH keyword syntax.

        Token rules (whitespace tokenizer, quoted phrases protected):

        - ``中文ns:译名`` / ``英文ns:译名`` -> ``english_ns:key`` (quoted when
          the key contains spaces);
        - bare 译名 -> rewritten ONLY on a unique cross-namespace match;
        - known prefix + unknown key -> namespace-only normalization
          (画师:shindol -> artist:shindol; circle:x -> group:x);
        - anything else (English keywords, unknown words, ambiguity) passes
          through untouched.
        """
        if not query or not self.loaded:
            return query
        query = _normalize_query(query)
        toks = _TOKEN_RE.findall(query)
        out: list[str] = []
        changed = False
        i = 0
        while i < len(toks):
            tok = toks[i]
            # Quoted phrase: keep verbatim, but handle outer-quoted
            # ``"ns:key with spaces"`` (tag jump whole-string quoting)
            # by unwrapping and translating inner prefixed form.
            if tok.startswith('"'):
                if len(tok) >= 2 and tok.endswith('"'):
                    inner = tok[1:-1]
                    if ":" in inner:
                        prefix, _, rest = inner.partition(":")
                        rep = self._translate_prefixed(prefix, rest.strip())
                        if rep is not None:
                            out.append(rep)
                            changed = True
                            i += 1
                            continue
                        norm = self._normalize_prefixed(prefix, rest.strip())
                        if norm is not None:
                            out.append(norm)
                            changed = True
                            i += 1
                            continue
                out.append(tok)
                i += 1
                continue
            # Try single-token prefixed/quoted form first
            rep = self.translate_token(tok)
            if rep is not None:
                out.append(rep)
                changed = True
                i += 1
                continue
            # Unquoted ``ns:key with spaces``: merge following bare tokens
            # (most compatible for tag jumps: ``团队:Digital Lover`` without
            # any quotes should still translate; no hard-coded keys).
            if ":" in tok:
                prefix, _, rest = tok.partition(":")
                en = self._ns_lookup.get(prefix.lower())
                if en is not None and rest:
                    # Collect consecutive bare tokens (no ":", not quoted)
                    best: str | None = None
                    best_end = i
                    candidate = rest
                    # Check single-token rest first (already tried, miss)
                    # then extend with following bare tokens, longest match wins
                    for j in range(i + 1, min(len(toks), i + 6)):
                        nxt = toks[j]
                        if ":" in nxt or nxt.startswith('"'):
                            break
                        candidate = f"{candidate} {nxt}"
                        if self._reverse_prefixed.get((en.lower(), candidate.lower())):
                            best = candidate
                            best_end = j
                    if best is not None:
                        rep2 = self._translate_prefixed(prefix, best)
                        if rep2 is not None:
                            out.append(rep2)
                            changed = True
                            i = best_end + 1
                            continue
                    # Full translation missed (single token already tried,
                    # multi-token merge found nothing): fall back to
                    # namespace-only normalization so upstream sees a
                    # namespace it understands (key stays verbatim).
                    norm = self._normalize_prefixed(prefix, rest, original=tok)
                    if norm is not None:
                        out.append(norm)
                        changed = True
                        i += 1
                        continue
            out.append(tok)
            i += 1
        return " ".join(out) if changed else query

    def translate_token(self, tok: str) -> str | None:
        """One token -> native EH keyword, or None to pass through."""
        m = _PREFIXED_QUOTED_RE.fullmatch(tok)
        if m is not None:
            return self._translate_prefixed(m.group(1), m.group(2))
        prefix, sep, rest = tok.partition(":")
        if sep and rest:
            return self._translate_prefixed(prefix, rest.strip('"'))
        cands = self._reverse_bare.get(tok.lower())
        if cands is not None and len(cands) == 1:
            return self._format(*cands[0])
        return None

    def _translate_prefixed(self, prefix: str, rest: str) -> str | None:
        en = self._ns_lookup.get(prefix.lower())
        if en is None or not rest:
            return None
        hit = self._reverse_prefixed.get((en.lower(), rest.lower()))
        if hit is None:
            return None
        return self._format(*hit)

    def _normalize_prefixed(
        self, prefix: str, rest: str, original: str | None = None
    ) -> str | None:
        """Namespace-only normalization: ``<known prefix>:<unknown key>``.

        Returns ``english_ns:rest`` (quoting ``rest`` when it holds spaces)
        so upstream receives a namespace it understands while the key
        travels verbatim. None when the prefix is unknown or the result
        would equal ``original`` (already-canonical native syntax stays a
        passthrough instead of a no-op rewrite).
        """
        en = self._ns_lookup.get(prefix.lower())
        if en is None:
            return None
        rest_clean = rest.strip().strip('"').strip()
        if not rest_clean:
            return None
        normalized = self._format(en, rest_clean)
        if original is not None and normalized == original:
            return None
        return normalized

    @staticmethod
    def _format(ns: str, key: str) -> str:
        if " " in key:
            return f'{ns}:"{key}"'
        return f"{ns}:{key}"

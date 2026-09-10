"""OPDS 2.0 routes: navigation document, OpenSearch, gallery feeds, single publication.

Versioned under /opds/v2.0 (JSON); the v1.2 Atom feeds live under /opds/v1.2.
"""

from __future__ import annotations

import asyncio
import logging
import re
from dataclasses import replace
from typing import Callable
from urllib.parse import quote, urlsplit

from fastapi import APIRouter, HTTPException, Request
from fastapi.responses import Response

from ..eh.models import (
    DetailPageInfo,
    GalleryComment,
    GalleryListItem,
    GalleryPageInfo,
    GalleryTag,
)
from ..eh.parser import _parse_size_text, apply_status_filter, parse_publish_time_iso
from ..eh.service import EHService
from ..eh.title_parser import parse_detail_title, parse_title_authors
from ..home_config import (
    Section,
    build_href,
    fetch_section,
    is_auth_required,
    load_home_config,
)
from .feed import (
    MIME_ACQ,
    MIME_NAV,
    MIME_PUBLICATION,
    REL_SUBSECTION,
    Opds2Builder,
    _iso,
)

logger = logging.getLogger(__name__)

router = APIRouter(prefix="/opds/v2.0", tags=["opds2"])

# EH `next=` lastGid cursor: a plain gid ("2367467") or a favorites
# composite cursor ("2753175-1786365950”, gid-favoritedAt). Anything else
# is malformed and rejected before it reaches the upstream request.
_NEXT_CURSOR_RE = re.compile(r"^\d+(?:-\d+)*$")

# Gallery URLs inside comment HTML: https://(e-hentai|exhentai).org/g/{gid}/{token}/
# (optionally with ?p= / #anchors; /mpv/ viewer links map to the same detail doc).
# The trailing `[^"']*` eats any query/fragment so the rewritten href always
# points at the OPDS 2.0 detail document for that gallery.
_CONTENT_GALLERY_LINK_RE = re.compile(
    r'(href=["\'])(https?://(?:e-hentai|exhentai)\.org/'
    r'(?:g|mpv)/(\d+)/([0-9a-fA-F]+)/[^"\']*)(["\'])'
)


def _rewrite_gallery_links(content: str, href: Callable[[str], str]) -> str:
    """Rewrite E-Hentai gallery links inside comment HTML to OPDS links.

    Lets the first-party app open galleries referenced in comments in-app
    (``/opds/v2.0/gallery/{gid}/{token}``) instead of leaving the site. The
    anchor text is left untouched — clients may keep showing the original URL
    while navigating via the rewritten href. Non-gallery links (uploader
    pages, forums, external sites) are left verbatim.
    """

    def _repl(m: re.Match) -> str:
        gid, token = m.group(3), m.group(4)
        return f"{m.group(1)}{href(f'/opds/v2.0/gallery/{gid}/{token}')}{m.group(5)}"

    return _CONTENT_GALLERY_LINK_RE.sub(_repl, content)


# Standard gallery cover / preview image URLs inside comment HTML. They can
# appear in several shapes on the two cover CDNs, e.g.
#   https://ehgt.org/w/02/597/37188-uwng9q95.webp            (e-hentai cover)
#   https://s.exhentai.org/w/02/597/37188-uwng9q95.webp      (exhentai cover)
#   https://s.exhentai.org/t/0f/e1/...-png_l.jpg             (preview thumb)
# Both CDN hosts (ehgt.org / s.exhentai.org) serve ONLY image bytes, all of
# them cross-origin, so every inline image URL on these hosts is proxied
# same-origin. The URL is OPAQUE: the leading number is NOT the gallery gid
# and the hash is NOT the gallery token (verified against a real detail page),
# so we proxy the exact bytes instead of parsing gid/token.
# Only embedded images (img src="..." / url(...) backgrounds) are rewritten;
# bare <a href> links pointing at CDN files are not inline images and stay
# verbatim (not fetched by the browser as part of layout).
# Default proxy host allowlist (mirrors config.image_proxy_hosts); the live
# allowlist comes from settings and can be overridden via IMAGE_PROXY_HOSTS.
_DEFAULT_IMAGE_PROXY_HOSTS = ("ehgt.org", "s.exhentai.org")


def _comment_cdn_image_re(hosts: tuple[str, ...]) -> re.Pattern:
    """Compile the comment-CDN-image rewrite regex for the given hosts.

    Hosts are already lowercase (config). Only inline images (img src="..." /
    url(...)) are matched so bare <a href> CDN links stay verbatim.
    """
    alt = "|".join(re.escape(h) for h in hosts)
    return re.compile(
        r"(?:(?<=src=\")|(?<=src=')|(?<=url\()|(?<=url\(')|(?<=url\"))"
        rf"(https://(?:{alt})/[^\"')]+)"
    )

# Diagnostic scan: every <img src> / style url() image target inside a comment.
# Used to surface cover-ish CDN URLs that our rewrite leaves cross-origin so
# host/path drift is visible in the log instead of silently 404-ing on the
# client. Hosts that could plausibly carry a gallery cover.
_COVER_ISH_HOSTS = frozenset(
    {"ehgt.org", "s.exhentai.org", "e-hentai.org", "exhentai.org"}
)
_COMMENT_ANY_IMAGE_RE = re.compile(
    r"(?:src=[\"']|url\([\"']?)(https?://[^\"')\s]+)"
)


# Standard-cover CDN hosts actually proxied come from the caller (settings);
# the diagnostic below uses the same set to flag what stays cross-origin.


def _rewrite_comment_covers(
    content: str,
    href: Callable[[str], str],
    hosts: tuple[str, ...] = _DEFAULT_IMAGE_PROXY_HOSTS,
) -> str:
    """Rewrite inline CDN cover/preview URLs in comment HTML to the proxy.

    eh/ex cover and preview images (ehgt.org / s.exhentai.org) live on a
    different origin; a web client rendering comment HTML fetches them
    cross-origin and hits CORS. Pointing them at the same-origin
    ``/image/fetch`` proxy loads the bytes through PandaOPDS. Only the bare URL
    token is swapped (keeps surrounding ``src="..."`` / ``url(...)``
    delimiters intact); bare ``<a href>`` CDN links and all other links stay
    verbatim.

    Logs a diagnostic line per cover-ish image URL so the real comment-cover
    format (host/path) is visible when a client reports images not loading.
    """

    def _repl(m: re.Match) -> str:
        return href(f"/image/fetch?url={quote(m.group(1))}")

    out = _comment_cdn_image_re(hosts).sub(_repl, content)
    # Diagnostic: surface every cover-ish image URL and whether we proxied it,
    # so host/path drift (client reports images not loading, nothing rewritten)
    # is visible in the log instead of silently failing.
    for m in _COMMENT_ANY_IMAGE_RE.finditer(content):
        u = m.group(1)
        host = urlsplit(u).netloc.lower()
        path = urlsplit(u).path
        if host in _COVER_ISH_HOSTS:
            proxied = host in set(hosts)
            logger.info(
                "comment image host=%s path=%s %s",
                host, path, "proxied" if proxied else "NOT proxied (cover CORS risk)",
            )
    return out


# Gallery list titles for the built-in browsing dimensions.
_LIST_TITLES = {"popular": "Popular", "watched": "Watched", "favorites": "Favorites"}

_TOPLIST_PERIODS = {
    "yesterday": "Yesterday",
    "month": "Past Month",
    "year": "Past Year",
    "alltime": "All Time",
}


def _service(request: Request) -> EHService:
    return request.app.state.service


def _builder(request: Request) -> Opds2Builder:
    builder = Opds2Builder(request.app.state.settings)
    # Tag translation lives on app.state (started in main.lifespan when
    # TAG_TRANSLATION_ENABLED); None keeps every subject verbatim.
    builder.translator = getattr(request.app.state, "tag_translation", None)
    return builder


# Namespaces excluded from the *list* subject (already exposed as dedicated
# fields / parsed by the client): language (standalone field) and artist
# (the client derives the author from image filenames). Detail documents
# carry the full #taglist subject, so list subject stays a strict subset.
_LIST_SUBJECT_EXCLUDED = frozenset({"language", "artist"})


def _flatten_subjects(
    tags: list[GalleryTag],
    exclude: frozenset[str] = frozenset(),
    translator=None,
) -> list[dict]:
    """RWPM subject objects over the full tag set: {"name": "ns:key", ...}.

    Highlighted tags (with inline style) carry an ``x:style`` member — the
    style travels with its tag, replacing the old side-channel ``mytags``
    bucket. List feeds pass ``_LIST_SUBJECT_EXCLUDED`` to drop fields already
    exposed elsewhere; detail documents pass nothing (complete #taglist;
    highlight styles are backfilled from the My Tags map by the caller).

    When a tag translator (EhTagTranslation, opt-in) is supplied and knows the
    tag, ``name`` becomes Chinese-namespace plus translated name (emoji
    stripped upstream); when only the namespace is known (proper nouns such
    as ``artist:<name>`` almost never have a key translation), ``name``
    becomes Chinese-namespace plus the verbatim original key; only
    fully-unknown namespaces keep their verbatim ``ns:key`` form. Translation happens
    here — after status filtering / style backfill / sorting, all of which
    operate on raw ``ns:key`` — so those mechanisms are unaffected.
    """
    seen: set[str] = set()
    out: list[dict] = []
    for t in tags:
        if t.namespace in exclude:
            continue
        s = str(t)
        if translator is not None:
            display = None
            display_fn = getattr(translator, "display_tag", None)
            if callable(display_fn):
                display = display_fn(t.namespace, t.key)
            else:  # duck-typed translator without namespace fallback
                display = translator.translate_tag(t.namespace, t.key)
            if display:
                s = display
        if s not in seen:
            seen.add(s)
            entry: dict = {"name": s}
            if t.style:
                entry["x:style"] = t.style.as_dict()
            out.append(entry)
    return out


def _sort_tags(tags: list[GalleryTag]) -> list[GalleryTag]:
    """Stable sort: highlighted tags (with style) first — list feeds only."""
    return sorted(tags, key=lambda t: t.style is None)


# Detail subject: namespace weight strictly per user spec, styled-first
# *within* each namespace group. Unknown namespaces (temp/reclass/…) fall
# after "other" preserving their original relative order.
_DETAIL_NAMESPACE_ORDER = [
    "language",
    "parody",
    "character",
    "cosplayer",
    "group",
    "artist",
    "female",
    "male",
    "mixed",
    "location",
    "other",
]
_DETAIL_ORDER_INDEX = {ns: i for i, ns in enumerate(_DETAIL_NAMESPACE_ORDER)}


def _sort_tags_detail(tags: list[GalleryTag]) -> list[GalleryTag]:
    """Group-stable sort for detail subjects: namespace weight, styled first within group."""
    if not tags:
        return tags
    # Group by namespace preserving first-appearance order (OrderedDict insertion order)
    from collections import OrderedDict

    groups: OrderedDict[str, list[GalleryTag]] = OrderedDict()
    for t in tags:
        groups.setdefault(t.namespace, []).append(t)
    # Sort namespaces by custom weight; unknown namespaces keep original relative order
    # (stable sort: equal keys preserve insertion order)
    ordered_namespaces = sorted(
        groups.keys(),
        key=lambda ns: _DETAIL_ORDER_INDEX.get(ns, len(_DETAIL_NAMESPACE_ORDER)),
    )
    out: list[GalleryTag] = []
    for ns in ordered_namespaces:
        lst = groups[ns]
        out.extend(sorted(lst, key=lambda t: t.style is None))
    return out


def _apply_mytags_styles(
    tags: list[GalleryTag], styles: dict
) -> list[GalleryTag]:
    """Backfill highlight styles from the My Tags map onto style-less tags.

    The detail-page #taglist carries no inline styles; the account's /mytags
    page (scraped into a static map by the service) is the style source for
    detail documents. Lookup: exact case-insensitive ``namespace:key`` first;
    entries stored under the ``*:key`` wildcard (namespace omitted upstream)
    match on key alone. Tags that already carry an inline style (or are
    absent from the map) pass through untouched.
    """
    if not styles:
        return tags
    out: list[GalleryTag] = []
    for t in tags:
        if t.style is None:
            name = str(t)
            s = styles.get(name.lower())
            if s is None:
                # wildcard entry (*:key): namespace omitted on /mytags
                _, _, key = name.partition(":")
                s = styles.get(f"*:{key.lower()}")
            if s is not None:
                t = replace(t, style=s)
        out.append(t)
    return out


def _comment_payload(
    c: GalleryComment,
    href: Callable[[str], str] | None = None,
    image_proxy_hosts: tuple[str, ...] = _DEFAULT_IMAGE_PROXY_HOSTS,
) -> dict:
    """``x:reviews`` entry: display-relevant subset only.

    Display-relevant fields: id/username/userId/time/lastEditTime/content
    (raw HTML). Interactive
    flags (fromMe/votedUp/votedDown) and score details are deliberately
    omitted (MVP); empty optional fields are dropped. When ``href`` is given
    (the feed's href() helper), gallery links inside the content are rewritten
    to OPDS 2.0 detail links for in-app navigation.
    """
    item: dict = {"id": c.id, "username": c.username, "time": c.time}
    if c.user_id is not None:
        item["userId"] = c.user_id
    if c.last_edit_time:
        item["lastEditTime"] = c.last_edit_time
    if c.content_html:
        content = c.content_html
        if href is not None:
            content = _rewrite_gallery_links(content, href)
            content = _rewrite_comment_covers(content, href, image_proxy_hosts)
        item["content"] = content
    return item


def _detail_eh_fields(
    detail: DetailPageInfo,
    comments_enabled: bool = True,
    href: Callable[[str], str] | None = None,
    image_proxy_hosts: tuple[str, ...] = _DEFAULT_IMAGE_PROXY_HOSTS,
) -> dict:
    """EH-specific metadata members, flattened under the ``x:`` prefix.

    Declared by the document's inline JSON-LD context (see feed.py);
    generic clients ignore unknown members. Scraped from the detail page
    (gdata-equivalent): rating, Japanese title, uploader, size, expunged,
    category, and — when enabled — the gallery comment block (``x:reviews``,
    raw HTML content with gallery links rewritten to OPDS detail links via
    ``href``). Tags never appear here: styles travel inline on `subject`
    entries (list pages parse them inline; detail subjects get them backfilled
    from the My Tags map).
    """
    ext: dict = {}
    if detail.rating:
        ext["x:rating"] = detail.rating
    if detail.title_jpn:
        ext["x:titleJpn"] = detail.title_jpn
    if detail.uploader:
        ext["x:uploader"] = detail.uploader
    if detail.filesize_bytes:
        ext["x:sizeBytes"] = detail.filesize_bytes
    elif detail.filesize_text:
        size = _parse_size_text(detail.filesize_text)
        if size:
            ext["x:sizeBytes"] = size
    if detail.expunged:
        ext["x:expunged"] = True
    if detail.category:
        ext["x:category"] = detail.category
    if comments_enabled and detail.comments:
        ext["x:reviews"] = [
            _comment_payload(c, href, image_proxy_hosts) for c in detail.comments
        ]
    return ext


def _item_modified(item: GalleryListItem) -> str:
    """`modified` from the list page's publish time; else now."""
    return _detail_modified(item.publish_time)


def _detail_modified(publish_time: str) -> str:
    """ISO `modified` from a publish-time string; fall back to now."""
    if publish_time:
        iso = parse_publish_time_iso(publish_time)
        if iso:
            return iso
    return _iso()


def _publication(
    builder: Opds2Builder,
    item: GalleryListItem,
) -> dict:
    """One publication object rendered purely from list-page HTML data.

    Browsing feeds never call the ehapi; ``x:*`` metadata carries only what
    the list page exposed (category, rating), while highlighted-tag styles
    travel inline on `subject` entries. Full tags live in `subject` (minus
    fields exposed elsewhere: language/artist). The client opens the detail
    document for full metadata.
    """
    category = item.category
    modified = _item_modified(item)
    page_count = item.page_count
    language = item.language

    clean_title, authors = parse_title_authors(item.title, category)

    tags = apply_status_filter(list(item.tags), builder.settings.tag_status_filter)
    tags = _sort_tags(tags)
    subjects = _flatten_subjects(tags, _LIST_SUBJECT_EXCLUDED, builder.translator)
    ext: dict = {}
    if item.rating:
        ext["x:rating"] = item.rating
    if item.category:
        ext["x:category"] = item.category

    return builder.publication(
        gid=item.gid,
        token=item.token,
        title=clean_title,
        modified=modified,
        authors=authors if authors else None,
        language=language,
        page_count=page_count,
        published=modified,
        subjects=subjects,
        number_of_pages=page_count,
        extra_metadata=ext or None,
    )


@router.get("", response_class=Response)
async def root_feed(request: Request):
    """Root OPDS 2.0 document.

    Layout is driven by a TOML config: ``[[group]]`` declares named groups;
    ``[[section]]`` references a group via ``group`` field.  Publication and
    navigation sections can co-exist in the same group.

    Sections without a ``group``:
    * ``kind="publication"`` → standalone ``groups[]`` entry
    * ``kind="navigation"``  → root ``navigation[]`` entry

    Watched / Favorites are auth-gated: omitted when no IPB cookie is set.
    The Archives preset (``query="archives"``) is store-gated: omitted
    while the archive store holds no ready entries.
    """
    service = _service(request)
    builder = _builder(request)
    settings = request.app.state.settings

    has_auth = bool(settings.ipb_member_id and settings.ipb_pass_hash)
    home = load_home_config(settings.home_config_path)

    # Archives shelf (local, zero upstream) is a plain preset section
    # (``type="preset" query="archives"``), positioned by the layout like
    # Watched / Favorites. It is hidden while the archive store holds no
    # ready entries and appears automatically once the first archive lands.
    archive = getattr(service, "archive", None)

    def _visible(s: Section) -> bool:
        if is_auth_required(s.type, s.query) and not has_auth:
            return False
        if s.type == "preset" and s.query == "archives":
            if archive is None or archive.ready_count() == 0:
                return False
        return True

    # Collect all visible sections; group definitions always visible.
    sections = [s for s in home.sections if _visible(s)]
    group_defs: dict[str, str] = {g.id: g.title for g in home.groups}

    # Phase 1 — concurrently fetch list pages for publication sections.
    pub_sections = [s for s in sections if s.kind == "publication" and s.count > 0]
    results = await asyncio.gather(
        *[fetch_section(service, s) for s in pub_sections],
        return_exceptions=True,
    )
    fetched: dict[int, GalleryPageInfo] = {}
    for section, result in zip(pub_sections, results):
        if isinstance(result, Exception):
            logger.warning("section %r list error: %s", section.title, result)
        else:
            fetched[id(section)] = result

    # Phase 2 — collect sections by group; preserve insertion order.
    grouped: dict[str, list[Section]] = {}   # group_id → sections
    ungrouped: list[Section] = []            # sections without group (in order)
    for s in sections:
        if s.group:
            grouped.setdefault(s.group, []).append(s)
        else:
            ungrouped.append(s)

    # Phase 3 — walk ungrouped sections in TOML order; emit named groups
    # at the position of their first referencing section (we approximate
    # by emitting them before ungrouped when they share a position, which
    # is fine since named groups are declared separately).
    #
    # Strategy: walk sections, emit each unique group on first encounter.
    groups_out: list[dict] = []
    root_nav: list[dict] = []
    emitted_groups: set[str] = set()

    def _emit_group(gid: str, grp_sections: list[Section]) -> dict:
        """Build an OPDS group dict from a list of sections."""
        title = group_defs.get(gid, grp_sections[0].title)
        first = grp_sections[0]
        g: dict = {
            "metadata": {
                "title": title,
                "identifier": f"urn:ehentai:group:{gid}",
                "modified": _iso(),
            },
            "links": [{
                "rel": "self",
                "href": builder.href(build_href(type=first.type, query=first.query)),
                "type": MIME_ACQ,
                "title": title,
            }],
        }
        pubs: list[dict] = []
        navs: list[dict] = []
        for s in grp_sections:
            if s.kind == "publication":
                if id(s) in fetched:
                    items = fetched[id(s)].galleries[: s.count]
                    for item in items:
                        pubs.append(_publication(builder, item))
                else:
                    # Fetch skipped (count=0 opt-out) or failed: surface the
                    # silent drop instead of vanishing without a trace.
                    logger.warning(
                        "publication section %r (group=%r) rendered no "
                        "preview: fetch skipped or failed",
                        s.title,
                        s.group,
                    )
            elif s.kind == "navigation":
                navs.append({
                    "title": s.title,
                    "href": builder.href(build_href(type=s.type, query=s.query)),
                    "rel": REL_SUBSECTION,
                    "type": MIME_ACQ,
                })
        if pubs:
            g["publications"] = pubs
        if navs:
            g["navigation"] = navs
        return g

    for s in sections:
        if s.group:
            if s.group not in emitted_groups:
                groups_out.append(_emit_group(s.group, grouped[s.group]))
                emitted_groups.add(s.group)
        else:
            if s.kind == "publication":
                groups_out.append(_emit_group(f"__s_{id(s)}", [s]))
            else:
                root_nav.append({
                    "title": s.title,
                    "href": builder.href(build_href(type=s.type, query=s.query)),
                })

    content = builder.navigation_document(
        navigation=root_nav or None,
        groups=groups_out or None,
    )
    return Response(
        content=content,
        media_type=MIME_NAV,
        headers={"Cache-Control": "public, max-age=300"},
    )


@router.get("/search.xml", response_class=Response)
async def open_search(request: Request):
    # OpenSearchDescription is version-agnostic XML; only the template differs.
    from ..opds.feed import FeedBuilder, MIME_OPEN_SEARCH

    content = FeedBuilder(request.app.state.settings).open_search(
        "/opds/v2.0/gallery"
    )
    return Response(
        content=content,
        media_type=MIME_OPEN_SEARCH,
        headers={"Cache-Control": "public, max-age=3600"},
    )


@router.get("/gallery", response_class=Response)
async def gallery_feed(
    request: Request,
    query: str = "",
    next: str | None = None,  # opaque lastGid cursor ("2367467" or "gid-favtime")
    category: str = "",
):
    service = _service(request)
    builder = _builder(request)
    settings = request.app.state.settings

    if next is not None and not _NEXT_CURSOR_RE.fullmatch(next):
        raise HTTPException(
            status_code=400, detail="invalid next cursor (expected digits or gid-favtime)"
        )

    # Resolve category name → f_cats exclude mask.
    f_cats: int | None = None
    if category:
        for name, mask in settings.facets:
            if category.lower() == name.lower():
                f_cats = mask
                break

    # `favorites` (full view) and `favorites:N` (single folder,
    # `/favorites.php?favcat=N`) share the favorites upstream; anything else
    # starting with `favorites:` is malformed and rejected (v2.0 only).
    favcat: str | None = None
    is_favorites = query == "favorites" or query.startswith("favorites:")
    if query.startswith("favorites:"):
        favcat = query.split(":", 1)[1]
        if not favcat.isdigit():
            raise HTTPException(
                status_code=400,
                detail=f"unknown favorites folder {favcat!r} (expected favorites or favorites:N)",
            )

    try:
        if query == "popular":
            info = await service.popular_galleries(last_gid=next)
        elif query == "watched":
            info = await service.watched_galleries(last_gid=next)
        elif is_favorites:
            info = await service.favorites_galleries(last_gid=next, favcat=favcat)
        else:
            info = await service.search_galleries(query=query, last_gid=next, f_cats=f_cats)
    except Exception as exc:  # mapped to proper statuses by app-level handlers
        logger.warning("gallery feed upstream error: %s", exc)
        raise

    publications = [_publication(builder, item) for item in info.galleries]

    next_href = None
    if info.next_gid:
        q_parts = []
        if query:
            q_parts.append(f"query={quote(query)}")
        if category:
            q_parts.append(f"category={quote(category)}")
        next_href = builder.href(
            f"/opds/v2.0/gallery?next={info.next_gid}"
            + ("&" + "&".join(q_parts) if q_parts else "")
        )

    if query.startswith("favorites:"):
        title = f"Favorites {favcat}"
    else:
        title = _LIST_TITLES.get(query, "Search") if query else "Latest"
    if category:
        title = f"{title} — {category}"
    title = f"E-Hentai: {title}"

    # Only emit facets for the main search feed (not popular/watched/favorites
    # which use different upstream URLs that may not support f_cats).
    facets = None
    if query not in ("popular", "watched", "favorites") and not query.startswith("favorites:"):
        facets = builder.build_category_facets(current_category=category)

    content = builder.acquisition_document(
        title=title,
        identifier=f"urn:ehentai:gallery-list:{query or 'latest'}",
        publications=publications,
        self_href="/opds/v2.0/gallery",
        next_href=next_href,
        facets=facets,
    )
    return Response(
        content=content,
        media_type=MIME_ACQ,
        headers={"Cache-Control": "public, max-age=300"},
    )


@router.get("/toplist", response_class=Response)
async def toplist_feed(
    request: Request,
    period: str = "yesterday",
    page: int = 1,
):
    """Ranklist acquisition document. `period` ∈ yesterday|month|year|alltime;
    pagination uses `page` (1-based, `?p=` upstream) — not the lastGid
    `next` mechanism used by the front-page feeds.
    """
    if period not in _TOPLIST_PERIODS:
        raise HTTPException(
            status_code=400,
            detail=f"unknown period {period!r} (expected {sorted(_TOPLIST_PERIODS)})",
        )

    service = _service(request)
    builder = _builder(request)

    try:
        info = await service.toplist_galleries(period=period, page=page)
    except Exception as exc:  # mapped to proper statuses by app-level handlers
        logger.warning("toplist feed upstream error: %s", exc)
        raise

    publications = [_publication(builder, item) for item in info.galleries]

    next_href = None
    if info.next_page:
        next_href = builder.href(
            f"/opds/v2.0/toplist?period={period}&page={info.next_page}"
        )

    title = f"E-Hentai: Toplist {_TOPLIST_PERIODS.get(period, period)}"
    # Period facets (OPDS 2.0): pick the ranklist period inside the feed;
    # the active period link carries "active": true.
    facets = [{
        "metadata": {"title": "Period"},
        "links": [
            {
                "href": builder.href(f"/opds/v2.0/toplist?period={p}"),
                "title": label,
                "active": p == period,
            }
            for p, label in _TOPLIST_PERIODS.items()
        ],
    }]
    content = builder.acquisition_document(
        title=title,
        identifier=f"urn:ehentai:toplist:{period}",
        publications=publications,
        self_href=f"/opds/v2.0/toplist?period={period}",
        next_href=next_href,
        facets=facets,
    )
    return Response(
        content=content,
        media_type=MIME_ACQ,
        headers={"Cache-Control": "public, max-age=300"},
    )


@router.get("/archives", response_class=Response)
async def archives_feed(request: Request, page: int = 1):
    """Local Archives shelf (ready zip masters), newest first — zero upstream.

    The OPDS entry point for purchased archives: entries keep their stream/
    thumb/detail URLs working even after the upstream gallery is deleted.
    Pagination mirrors toplist (`page` 1-based).
    """
    service = _service(request)
    builder = _builder(request)

    info = await service.archived_galleries(page=page)
    publications = [_publication(builder, item) for item in info.galleries]

    next_href = None
    if info.next_page:
        next_href = builder.href(f"/opds/v2.0/archives?page={info.next_page}")

    content = builder.acquisition_document(
        title="E-Hentai: Archives",
        identifier="urn:ehentai:archives",
        publications=publications,
        self_href="/opds/v2.0/archives",
        next_href=next_href,
    )
    return Response(
        content=content,
        media_type=MIME_ACQ,
        headers={"Cache-Control": "public, max-age=300"},
    )


async def _detail_publication(
    service: EHService, builder: Opds2Builder, gid: int, token: str
) -> dict:
    """Render the single-publication object for the detail endpoints.

    A ready archive renders entirely from the local snapshot (zero upstream,
    see EHService.get_detail_doc) — reading an archived gallery never touches
    the source site, so a deleted gallery keeps serving a full document. The
    local path uses the persisted My Tags map as-is (no TTL-triggered refresh)
    and carries no comments (snapshot has none). Non-archived galleries keep
    the cached upstream detail-page HTML (which also pre-warms the page-URL
    mapping for fast reader entry). Zero gdata in both paths.
    """
    detail = await service.get_detail_doc(gid, token)
    clean_title, authors = parse_detail_title(
        detail.title, detail.title_jpn, detail.category
    )
    modified = _detail_modified(detail.publish_time)
    tags = apply_status_filter(list(detail.tags), builder.settings.tag_status_filter)
    if service.archive is not None and service.archive.is_ready(gid, token):
        # offline path: never trigger an upstream /mytags refresh
        styles = service.get_mytags_cached()
    else:
        styles = await service.get_mytags()
    tags = _apply_mytags_styles(tags, styles)
    tags = _sort_tags_detail(tags)
    subjects = _flatten_subjects(tags, frozenset(), builder.translator)
    return builder.publication(
        gid=gid,
        token=token,
        title=clean_title,
        modified=modified,
        authors=authors if authors else None,
        language=detail.language or None,
        page_count=detail.image_count,
        published=modified,
        subjects=subjects,
        number_of_pages=detail.image_count,
        extra_metadata=_detail_eh_fields(
            detail,
            builder.settings.comments_enabled,
            builder.href,
            builder.settings.image_proxy_hosts,
        ),
        detail_document=True,
    )


@router.get("/gallery/{gid}/{token}", response_class=Response)
async def gallery_detail(request: Request, gid: int, token: str):
    """Single-publication acquisition document.

    Ready archives render from the local snapshot (zero upstream); otherwise
    from the detail-page HTML (which also pre-warms the page-URL mapping cache
    so the first /stream request skips one upstream round trip). Zero gdata.
    """
    service = _service(request)
    builder = _builder(request)

    pub = await _detail_publication(service, builder, gid, token)
    clean_title = pub["metadata"]["title"]
    content = builder.acquisition_document(
        title=clean_title,
        identifier=f"urn:ehentai:gallery:{gid}:{token}",
        publications=[pub],
        self_href=f"/opds/v2.0/gallery/{gid}/{token}",
    )
    return Response(
        content=content,
        media_type=MIME_ACQ,
        headers={"Cache-Control": "public, max-age=300"},
    )


@router.get("/gallery/{gid}/{token}/publication", response_class=Response)
async def gallery_publication(request: Request, gid: int, token: str):
    """Single-publication document: a top-level RWPM/OPDS publication object.

    This is the target of every publication's `rel="self"` link. Clients
    like Stump follow `self` to open details and read through the embedded
    `readingOrder` (per-page image URLs); the response shape matches what
    their parser expects (a publication object, not an acquisition feed).
    Ready archives render from the local snapshot (zero upstream).
    """
    service = _service(request)
    builder = _builder(request)

    pub = await _detail_publication(service, builder, gid, token)
    return Response(
        content=builder.serialize(pub),
        media_type=MIME_PUBLICATION,
        headers={"Cache-Control": "public, max-age=300"},
    )

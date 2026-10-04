"""My Tags style-map tests (offline).

Covers:
- parse_mytags against the real /mytags page capture (example/mytags.html)
- MyTagsMap persistence (atomic JSON snapshot survives restart) + TTL staleness
- EHService.get_mytags: no-IPB short-circuit, lazy refresh, stale-on-failure
- opds2 detail documents backfill x:style from the map (and re-sort)
"""

from pathlib import Path

import httpx
import pytest

from app.config import Settings
from app.eh.models import GalleryTag
from app.eh.mytags import MyTagsMap
from app.eh.service import EHService

MYTAGS_HTML = Path(__file__).resolve().parent.parent / "example" / "mytags.html"


def _settings(**kw) -> Settings:
    base = dict(ipb_member_id="1", ipb_pass_hash="abc", home_config_path=None)
    base.update(kw)
    return Settings(**base)


def _install_app_state(settings: Settings, service: EHService) -> None:
    from app.main import app

    app.state.settings = settings
    app.state.service = service


async def _get(path: str):
    from app.main import app

    transport = httpx.ASGITransport(app=app)
    async with httpx.AsyncClient(transport=transport, base_url="http://test") as client:
        return await client.get(path)


# --------------------------------------------------------------------------
# parser
# --------------------------------------------------------------------------


def test_parse_mytags_real_page():
    """The real page capture yields lowercase 'ns:key' keys (spaces, not
    underscores) with the full colour triple."""
    from app.eh.parser import parse_mytags

    if not MYTAGS_HTML.exists():
        pytest.skip("example/mytags.html not present")

    styles = parse_mytags(MYTAGS_HTML.read_text(encoding="utf-8"))
    assert len(styles) >= 30  # tagset #1 holds 34 tags in the capture

    s = styles["language:chinese"]
    assert s.color == "#f1f1f1"
    assert s.border_color == "#1357df"
    assert "radial-gradient(#1357df,#3377FF)" in s.background

    # spaces, not '+' or '_'; keys are lowercase
    assert "male:males only" in styles
    assert styles["male:males only"].border_color == "#df4646"

    # every entry has at least one colour component; !important stripped
    for k, v in styles.items():
        assert ":" in k and k == k.lower()
        assert v.as_dict(), f"{k} parsed to an empty style"


def test_parse_mytags_skips_unstyled_and_empty():
    from app.eh.parser import parse_mytags

    doc = """
    <html><body><div id="usertags_outer">
      <div id="usertag_1"><div><div id="tagpreview_1" class="gt"
           style="color:#123456" title="Female:Big Breasts">f:big breasts</div></div></div>
      <div id="usertag_2"><div><div id="tagpreview_2" class="gt"
           title="male:plain"></div></div></div>
      <div id="usertag_3"><div><a href="/tag/temp:x"><div id="tagpreview_3"
           class="gt" style="border-color:#abcdef;background:#244444 !important" title="temp:x">x</div></a></div></div>
    </div></body></html>
    """
    styles = parse_mytags(doc)
    assert set(styles) == {"female:big breasts", "temp:x"}
    assert styles["female:big breasts"].color == "#123456"
    assert styles["temp:x"].border_color == "#abcdef"
    assert styles["temp:x"].background == "#244444"


def test_parse_mytags_temp_namespace_and_whitespace():
    """Abbreviated namespaces expand (f/m/x -> female/male/mixed); titles
    without a namespace become `*:key` wildcard entries (the true namespace
    is unknowable upstream); internal whitespace collapses."""
    from app.eh.parser import parse_mytags

    doc = """
    <html><body><div id="usertags_outer">
      <div id="usertag_9"><div><div id="tagpreview_9" class="gt"
           style="color:#111111" title="f:glasses">f:glasses</div></div></div>
      <div id="usertag_10"><div><div id="tagpreview_10" class="gt"
           style="color:#222222" title="m:glasses">m:glasses</div></div></div>
      <div id="usertag_11"><div><div id="tagpreview_11" class="gt"
           style="color:#333333" title="x:glasses">x:glasses</div></div></div>
      <div id="usertag_12"><div><div id="tagpreview_12" class="gt"
           style="color:#444444" title="glasses">glasses</div></div></div>
      <div id="usertag_13"><div><div id="tagpreview_13" class="gt"
           style="color:#555555" title="male:double  space">d</div></div></div>
      <div id="usertag_14"><div><div id="tagpreview_14" class="gt"
           style="color:#666666" title="language:full ns">d</div></div></div>
    </div></body></html>
    """
    styles = parse_mytags(doc)
    assert set(styles) == {
        "female:glasses",
        "male:glasses",
        "mixed:glasses",
        "*:glasses",          # omitted namespace -> wildcard by key
        "male:double space",  # whitespace collapsed
        "language:full ns",   # full namespaces pass through
    }


# --------------------------------------------------------------------------
# MyTagsMap store
# --------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_mytags_map_persists_across_restart(tmp_path):
    """A replaced map is written atomically and restored by a fresh instance."""
    from app.eh.models import TagStyle

    path = tmp_path / "mytags.json"
    m1 = MyTagsMap(path, ttl_seconds=21600)
    assert m1.stale() is True  # nothing loaded yet -> first reader refreshes
    await m1.replace({"language:chinese": TagStyle(color="#f1f1f1")})

    m2 = MyTagsMap(path, ttl_seconds=21600)
    assert m2.stale() is False  # persisted snapshot is fresh within TTL
    assert m2.get() == {"language:chinese": TagStyle(color="#f1f1f1")}


@pytest.mark.asyncio
async def test_mytags_map_empty_snapshot_counts_as_fresh(tmp_path):
    """A successful fetch with zero styled tags must not re-hit upstream on
    every request: emptiness is a valid, fresh state."""
    m = MyTagsMap(tmp_path / "mytags.json", ttl_seconds=21600)
    await m.replace({})
    assert m.stale() is False


def test_mytags_map_corrupt_file_is_empty(tmp_path):
    path = tmp_path / "mytags.json"
    path.write_text("{not json", encoding="utf-8")
    m = MyTagsMap(path, ttl_seconds=21600)
    assert m.get() == {}
    assert m.stale() is True


# --------------------------------------------------------------------------
# service
# --------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_get_mytags_without_ipb_is_empty_and_offline(tmp_path):
    settings = _settings(ipb_member_id="", ipb_pass_hash="")
    service = EHService(settings)

    async def boom(*a, **k):
        raise AssertionError("upstream must not be touched without IPB cookies")

    service._html_get = boom
    assert await service.get_mytags() == {}


@pytest.mark.asyncio
async def test_get_mytags_lazy_refresh_and_stale_on_failure(tmp_path, monkeypatch):
    """Past-TTL readers trigger exactly one upstream fetch (single-flight);
    a failing fetch serves the stale snapshot instead of raising."""
    from app.eh.models import TagStyle

    settings = _settings(mytags_state=tmp_path / "mytags.json")
    service = EHService(settings)

    calls = {"n": 0}

    async def fake_html_get(path, params=None):
        calls["n"] += 1
        if calls["n"] == 1:
            return (
                '<html><div id="tagpreview_1" class="gt" style="color:#111111" '
                'title="female:ahegao">f:ahegao</div></html>'
            )
        raise RuntimeError("upstream down")

    monkeypatch.setattr(service, "_html_get", fake_html_get)

    styles = await service.get_mytags()
    assert calls["n"] == 1
    assert styles == {"female:ahegao": TagStyle(color="#111111")}

    # fresh within TTL: no further upstream traffic
    assert await service.get_mytags() == styles
    assert calls["n"] == 1

    # force-stale + upstream failure -> stale map served, no exception
    service.mytags._saved_at = 0.0
    assert await service.get_mytags() == styles
    assert calls["n"] == 2


@pytest.mark.asyncio
async def test_get_mytags_requests_leading_slash_path(tmp_path, monkeypatch):
    """Regression: the upstream path must be "/mytags" (leading slash) —
    "mytags" would concatenate into https://e-hentai.orgmytags."""
    settings = _settings(mytags_state=tmp_path / "mytags.json")
    service = EHService(settings)
    seen: list[str] = []

    async def fake_get_html(path, *, params=None, referer=None):
        seen.append(path)
        return (
            '<html><div id="tagpreview_1" class="gt" style="color:#111111" '
            'title="female:ahegao">f:ahegao</div></html>'
        )

    async def fake_session():
        return None

    monkeypatch.setattr(service.client, "get_html", fake_get_html)
    monkeypatch.setattr(service.client, "establish_session", fake_session)

    styles = await service.get_mytags()
    assert seen == ["/mytags"]
    from app.eh.models import TagStyle

    assert styles == {"female:ahegao": TagStyle(color="#111111")}


# --------------------------------------------------------------------------
# opds2 detail backfill
# --------------------------------------------------------------------------


def _detail_stub():
    from app.eh.models import DetailPageInfo

    return DetailPageInfo(
        image_no_from=0,
        image_no_to=2,
        image_count=3,
        current_page_no=1,
        page_count=1,
        title="[Author] Title",
        title_jpn="",
        category="Manga",
        uploader="up1",
        publish_time="2026-08-12 13:11",
        language="chinese",
        tags=[
            GalleryTag("language", "chinese"),
            GalleryTag("female", "ahegao"),
            GalleryTag("parody", "original"),
        ],
    )


@pytest.mark.asyncio
async def test_detail_subject_backfills_styles_from_mytags(tmp_path, monkeypatch):
    """Detail subject entries get x:style from the My Tags map; styled tags
    sort first; tags already carrying an inline style keep their own."""
    from app.eh.models import DetailPageInfo, GalleryTag, TagStyle

    detail = _detail_stub()
    detail.tags = [
        GalleryTag("language", "chinese"),
        GalleryTag("female", "netorare", style=TagStyle(background="#0f0")),
        GalleryTag("parody", "original"),
    ]
    settings = _settings()
    service = EHService(settings)
    monkeypatch.setattr(service, "get_detail_page", _async_value(detail))

    async def fake_mytags():
        return {
            "language:chinese": TagStyle(
                color="#f1f1f1",
                border_color="#1357df",
                background="radial-gradient(#1357df,#3377FF)",
            ),
            "parody:original": TagStyle(border_color="#df4646"),
        }

    monkeypatch.setattr(service, "get_mytags", fake_mytags)
    _install_app_state(settings, service)

    r = await _get("/opds/v2.0/gallery/1/tok1")
    assert r.status_code == 200
    md = r.json()["publications"][0]["metadata"]
    # namespace weight: language -> parody -> female, styled-first within group
    assert md["subject"] == [
        {
            "name": "language:chinese",
            "x:style": {
                "color": "#f1f1f1",
                "borderColor": "#1357df",
                "background": "radial-gradient(#1357df,#3377FF)",
            },
        },
        {"name": "parody:original", "x:style": {"borderColor": "#df4646"}},
        # inline list-page style wins over the static map (never overwritten)
        {"name": "female:netorare", "x:style": {"background": "#0f0"}},
        {"name": "category:Manga"},
    ]
    assert "mytags" not in md


@pytest.mark.asyncio
async def test_detail_subject_wildcard_and_abbreviation_match(tmp_path, monkeypatch):
    """Live-page mytags variants match detail tags: abbreviated f:/m:/x:
    expand to full namespaces; omitted-namespace entries (*:key wildcard)
    fall back to a key-only match."""
    from app.eh.models import TagStyle

    detail = _detail_stub()
    detail.tags = [
        GalleryTag("female", "glasses"),   # stored as f:glasses on /mytags
        GalleryTag("parody", "original"),  # stored bare on /mytags
        GalleryTag("male", "plain"),       # absent from the map
    ]
    settings = _settings()
    service = EHService(settings)
    monkeypatch.setattr(service, "get_detail_page", _async_value(detail))

    async def fake_mytags():
        return {
            "female:glasses": TagStyle(color="#111111"),
            "*:original": TagStyle(color="#222222"),
        }

    monkeypatch.setattr(service, "get_mytags", fake_mytags)
    _install_app_state(settings, service)

    r = await _get("/opds/v2.0/gallery/1/tok1")
    assert r.status_code == 200
    md = r.json()["publications"][0]["metadata"]
    # namespace weight: parody before female before male
    assert md["subject"] == [
        {"name": "parody:original", "x:style": {"color": "#222222"}},
        {"name": "female:glasses", "x:style": {"color": "#111111"}},
        {"name": "male:plain"},
        {"name": "category:Manga"},
    ]


def _async_value(v):
    import asyncio

    async def _f(*a, **k):
        return v

    return _f

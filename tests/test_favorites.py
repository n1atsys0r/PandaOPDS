"""Favorites feature tests (offline): favcat parsing and /api/favorites routes.

Fixtures mirror the real favorites page markup (extended view): the
``div.nosel div.fp`` category picker carries the favcat map and each gallery's
posted element carries the folder name in its ``title`` attribute.
"""

import httpx
import pytest

from app.config import Settings
from app.eh.parser import parse_list_page, parse_favorites_categories
from app.eh.service import EHService

FAVCAT_NAMES = {0: "All Favorites", 1: "Common", 2: "Videos"}


def _settings(**kw) -> Settings:
    base = dict(
        ipb_member_id="1",
        ipb_pass_hash="abc",
    )
    base.update(kw)
    return Settings(**base)


def _fav_page(rows, *, picker=True, next_gid=None) -> str:
    """Build an extended-view favorites page. rows: (gid, token, title, favcat_name)."""
    rows_html = ""
    for gid, token, title, favcat_name in rows:
        rows_html += f"""
        <tr>
          <td class="gl1e" style="width:250px"><div><a href="https://e-hentai.org/g/{gid}/{token}/"><img src="/x/c{gid}.jpg"></a></div></td>
          <td class="gl2e"><div>
            <div class="gl3e">
              <div class="cn">Manga</div>
              <div onclick="popUp('https://e-hentai.org/gallerypopups.php?gid={gid}&amp;t={token}')" id="posted_{gid}" title="{favcat_name}">2026-08-12 00:00</div>
              <div class="ir" style="background-position:0px -21px;opacity:1"></div>
              <div><a href="https://e-hentai.org/uploader/u1">u1</a></div>
              <div>42 pages</div>
            </div>
            <a href="https://e-hentai.org/g/{gid}/{token}/"><div class="gl4e glname" style="min-height:100px">
              <div class="glink">{title}</div><div><table></table></div>
            </div></a>
          </div></td>
        </tr>"""
    picker_html = ""
    if picker:
        fp = "".join(
            f'<div class="fp" onclick="popUp(\'https://e-hentai.org/gallerypopups.php?gid=0&amp;t=0&amp;act=addfav&amp;favcat={i}\')">'
            f"<div>{i}</div><div>Favorite</div><div>{name}</div></div>"
            for i, name in FAVCAT_NAMES.items()
        )
        picker_html = f'<div class="nosel">{fp}</div>'
    nav = (
        f'<div class="searchnav"><a id="unext" href="?next={next_gid}">Next</a></div>'
        if next_gid else ""
    )
    return f"<html><body>{picker_html}<table class=\"itg glte\">{rows_html}</table>{nav}</body></html>"


class FakeClient:
    """EHClient stand-in for favorites: records write calls, serves pages."""

    def __init__(self):
        self.calls: list[tuple] = []
        self.pages: dict[str, str] = {}
        self.session_calls = 0

    async def establish_session(self):
        self.session_calls += 1

    async def add_favorite(self, gid, token, favcat, note=""):
        self.calls.append(("add", gid, token, favcat, note))
        return "<html>ok</html>"

    async def move_favorite(self, gid, token, favcat, note=""):
        self.calls.append(("move", gid, token, favcat, note))
        return "<html>ok</html>"

    async def remove_favorite(self, gid, token):
        self.calls.append(("remove", gid, token))
        return "<html>ok</html>"

    async def get_html(self, path, params=None):
        if params:
            query = "&".join(f"{k}={v}" for k, v in sorted(params.items()))
            full = f"{path}?{query}"
            if full in self.pages:
                return self.pages[full]
        return self.pages[path]


def make_service(tmp_path, **kw) -> tuple[Settings, FakeClient, EHService]:
    settings = _settings(cache_dir=tmp_path, **kw)
    client = FakeClient()
    service = EHService(settings, client=client)
    return settings, client, service


# --------------------------------------------------------------------------
# parser: favcat map + per-gallery favcat
# --------------------------------------------------------------------------


def test_parse_favcat_map_and_gallery_favcat():
    html = _fav_page([
        (100, "aaa", "New One", "Common"),
        (101, "bbb", "Old Two", "Videos"),
    ])
    info = parse_list_page(html)
    assert info.favcat_map == FAVCAT_NAMES
    by_gid = {g.gid: g for g in info.galleries}
    assert by_gid[100].favcat == 1          # "Common"
    assert by_gid[101].favcat == 2          # "Videos"
    # categories from raw html (public entry used by the service)
    assert parse_favorites_categories(html) == FAVCAT_NAMES


def test_non_favorites_page_has_empty_favcat():
    # same extended layout, no picker + no title attr -> favcat stays None
    html = _fav_page([(100, "aaa", "No Cat", "Common")], picker=False)
    info = parse_list_page(html)
    assert info.favcat_map == {}
    assert info.galleries[0].favcat is None


# --------------------------------------------------------------------------
# service: write ops
# --------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_favorite_action_single(tmp_path):
    settings, client, service = make_service(tmp_path)
    results = await service.favorite_action(
        "add", [(100, "aaa")], favcat=2, note="hi"
    )
    assert results == [{"gid": 100, "token": "aaa", "ok": True}]
    assert client.calls == [("add", 100, "aaa", 2, "hi")]


@pytest.mark.asyncio
async def test_favorite_action_batch_and_remove(tmp_path):
    settings, client, service = make_service(tmp_path)
    results = await service.favorite_action(
        "move", [(100, "aaa"), (101, "bbb")], favcat=1
    )
    assert [r["ok"] for r in results] == [True, True]
    assert client.calls == [("move", 100, "aaa", 1, ""), ("move", 101, "bbb", 1, "")]

    client.calls.clear()
    results = await service.favorite_action("remove", [(100, "aaa")])
    assert results[0]["ok"] is True
    assert client.calls == [("remove", 100, "aaa")]


@pytest.mark.asyncio
async def test_favorite_action_invalidates_list_cache(tmp_path):
    settings, client, service = make_service(tmp_path)
    await service.mem.set("list:favorites:search:", {"cached": True})
    await service.mem.set("list:popular:", {"keep": True})
    await service.favorite_action("add", [(100, "aaa")], favcat=1)
    assert await service.mem.get("list:favorites:search:") is None
    assert await service.mem.get("list:popular:") is not None


@pytest.mark.asyncio
async def test_favorite_categories_cached(tmp_path):
    settings, client, service = make_service(tmp_path)
    client.pages["/favorites.php"] = _fav_page([(100, "aaa", "X", "Common")])
    cat1 = await service.favorite_categories()
    assert cat1 == FAVCAT_NAMES
    # cached: second call does not hit the client again
    client.pages.pop("/favorites.php")
    cat2 = await service.favorite_categories()
    assert cat2 == FAVCAT_NAMES


# routes
# --------------------------------------------------------------------------


def _install_app_state(settings, service) -> None:
    from app.main import app

    app.state.settings = settings
    app.state.service = service


async def _post(path: str, payload: dict | None = None):
    from app.main import app

    transport = httpx.ASGITransport(app=app)
    async with httpx.AsyncClient(transport=transport, base_url="http://test") as client:
        return await client.post(path, json=payload)


async def _get(path: str):
    from app.main import app

    transport = httpx.ASGITransport(app=app)
    async with httpx.AsyncClient(transport=transport, base_url="http://test") as client:
        return await client.get(path)


@pytest.mark.asyncio
async def test_route_add_single(tmp_path):
    settings, client, service = make_service(tmp_path)
    _install_app_state(settings, service)
    resp = await _post("/api/favorites", {
        "action": "add", "gid": 100, "token": "aaa", "favcat": 1, "note": "n"
    })
    assert resp.status_code == 200
    body = resp.json()
    assert body["ok"] is True
    assert body["ok_count"] == 1
    assert client.calls == [("add", 100, "aaa", 1, "n")]


@pytest.mark.asyncio
async def test_route_batch_and_validation(tmp_path):
    settings, client, service = make_service(tmp_path)
    _install_app_state(settings, service)
    resp = await _post("/api/favorites", {
        "action": "remove", "items": [{"gid": 1, "token": "a"}, {"gid": 2, "token": "b"}]
    })
    assert resp.status_code == 200
    assert resp.json()["ok"] is True
    assert len(client.calls) == 2

    # missing action
    resp = await _post("/api/favorites", {"gid": 1, "token": "a"})
    assert resp.status_code == 400
    # missing favcat for add
    resp = await _post("/api/favorites", {"action": "add", "gid": 1, "token": "a"})
    assert resp.status_code == 400


@pytest.mark.asyncio
async def test_route_requires_ipb(tmp_path):
    settings = _settings(cache_dir=tmp_path, ipb_member_id="", ipb_pass_hash="")
    service = EHService(settings)
    _install_app_state(settings, service)
    resp = await _post("/api/favorites", {"action": "add", "gid": 1, "token": "a", "favcat": 1})
    assert resp.status_code == 403

"""Extension search keywords -> advanced-search params (app/eh/query_ext.py).

Covers the pure tokenizer/translator and its integration point in
EHService.search_galleries (params merging + cache-key isolation).
"""

import pytest

from app.config import Settings, load_settings
from app.eh.query_ext import extract_adv_params
from app.eh.service import EHService

FIXTURE_HTML = """<html><body>\
<div class="gl1c"><a href="https://e-hentai.org/g/1/abc/"><div class="glname">t</div></a></div>\
</body></html>"""


# -- pure function ---------------------------------------------------------

def test_no_keywords_untouched():
    q = 'language:chinese  "exact phrase"  female:x'
    rest, params = extract_adv_params(q)
    assert rest == q
    assert params == {}


def test_rating_keyword():
    rest, params = extract_adv_params("rating:5")
    assert rest == ""
    assert params == {"advsearch": "1", "f_srdd": "5"}


def test_rating_mixed_with_text():
    rest, params = extract_adv_params("naruto rating:4 english")
    assert rest == "naruto english"
    assert params == {"advsearch": "1", "f_srdd": "4"}


@pytest.mark.parametrize("bad", ["rating:0", "rating:6", "rating:abc", "rating:"])
def test_invalid_rating_silently_dropped(bad):
    rest, params = extract_adv_params(f"foo {bad} bar")
    # keyword-shaped garbage is removed, plain terms kept, no params emitted
    assert bad not in rest
    assert rest == "foo bar"
    assert params == {}


def test_expunged_keyword():
    rest, params = extract_adv_params("expunged")
    assert rest == ""
    assert params == {"advsearch": "1", "f_sh": "on"}


def test_nohide_single():
    rest, params = extract_adv_params("nohide:tags")
    assert rest == ""
    assert params == {"advsearch": "1", "f_sft": "on"}


def test_nohide_all_expands_to_three_flags():
    rest, params = extract_adv_params("nohide:all")
    assert rest == ""
    assert params == {"advsearch": "1", "f_sfu": "on", "f_sfl": "on", "f_sft": "on"}


def test_combined_keywords_dedupe_advsearch():
    rest, params = extract_adv_params("expunged nohide:uploader rating:2")
    assert rest == ""
    assert params == {
        "advsearch": "1",
        "f_sh": "on",
        "f_sfu": "on",
        "f_srdd": "2",
    }


def test_case_insensitive():
    rest, params = extract_adv_params("RATING:3 Expunged NoHide:Language")
    assert rest == ""
    assert params["f_srdd"] == "3"
    assert params["f_sh"] == "on"
    assert params["f_sfl"] == "on"


def test_quoted_phrase_protected():
    rest, params = extract_adv_params('"rating:5" expunged')
    # quoted phrase stays verbatim as a search term
    assert '"rating:5"' in rest
    assert params == {"advsearch": "1", "f_sh": "on"}


def test_keyword_shaped_garbage_dropped():
    # looks like our keywords but invalid -> dropped, not searched literally
    rest, params = extract_adv_params("nohide:bogus")
    assert rest == ""
    assert params == {}


def test_whitespace_normalized_only_when_matched():
    rest, _ = extract_adv_params("a   b\tc   rating:1")
    assert rest == "a b c"


def test_empty_query():
    assert extract_adv_params("") == ("", {})


# -- integration into EHService -------------------------------------------

@pytest.mark.asyncio
async def test_search_galleries_merges_adv_params(monkeypatch):
    """search_galleries strips keywords from query and passes upstream params.

    The list-cache key is built from all params, so differently-filtered
    searches must produce distinct keys.
    """
    settings = load_settings()
    svc = EHService(settings)
    captured: list[tuple[str, dict]] = []

    async def fake_html_get(path, params=None):
        captured.append((path, dict(params or {})))
        return FIXTURE_HTML

    monkeypatch.setattr(svc, "_html_get", fake_html_get)

    await svc.search_galleries(query="naruto rating:4 nohide:tags")
    path, params = captured[-1]
    assert path == "/"
    assert params["f_search"] == "naruto"
    assert params["advsearch"] == "1"
    assert params["f_srdd"] == "4"
    assert params["f_sft"] == "on"

    await svc.close()


@pytest.mark.asyncio
async def test_search_galleries_category_keywords_and_facets(monkeypatch):
    settings = load_settings()
    svc = EHService(settings)
    captured = []

    async def fake_html_get(path, params=None):
        captured.append(dict(params or {}))
        return FIXTURE_HTML

    monkeypatch.setattr(svc, "_html_get", fake_html_get)
    await svc.search_galleries(query='naruto category:"Artist CG" category:doujinshi')
    assert captured[-1]["f_search"] == "naruto"
    # Union of two allowed categories; exclude every other category.
    assert captured[-1]["f_cats"] == "1013"

    await svc.search_galleries(query="category:doujinshi")
    assert "f_search" not in captured[-1]
    assert captured[-1]["f_cats"] == "1021"

    await svc.search_galleries(query='"category:doujinshi"')
    assert captured[-1]["f_search"] == '"category:doujinshi"'
    assert "f_cats" not in captured[-1]

    # The translated category namespace is only reserved when translation is on.
    await svc.search_galleries(query="分类:同人志")
    assert captured[-1]["f_search"] == "分类:同人志"
    assert "f_cats" not in captured[-1]

    # reclass is the ordinary EH tag namespace, not a category-search alias.
    await svc.search_galleries(query="reclass:doujinshi")
    assert captured[-1]["f_search"] == "reclass:doujinshi"
    assert "f_cats" not in captured[-1]

    # An explicit facet intersects the category union from search keywords.
    await svc.search_galleries(
        query='category:"Artist CG" category:doujinshi', f_cats=1021, last_gid="42"
    )
    assert "f_search" not in captured[-1]
    assert captured[-1]["f_cats"] == "1021"

    await svc.search_galleries(query="category:unknown naruto", f_cats=1021)
    assert captured[-1]["f_search"] == "naruto"
    assert captured[-1]["f_cats"] == "1021"
    await svc.close()


@pytest.mark.asyncio
async def test_search_galleries_facets_aliases_override_builtin_names(monkeypatch):
    settings = Settings(facets=[("Doujinshi", 17), ("自定义分类", 7)])
    svc = EHService(settings)
    captured = []

    async def fake_html_get(path, params=None):
        captured.append(dict(params or {}))
        return FIXTURE_HTML

    monkeypatch.setattr(svc, "_html_get", fake_html_get)
    await svc.search_galleries(query="category:doujinshi")
    assert captured[-1]["f_cats"] == "17"  # configured name overrides built-in

    await svc.search_galleries(query="category:自定义分类")
    assert captured[-1]["f_cats"] == "7"
    assert "f_search" not in captured[-1]
    await svc.close()


@pytest.mark.asyncio
async def test_search_galleries_translated_category_keyword(monkeypatch):
    settings = load_settings()
    svc = EHService(settings)
    captured = []

    class Translator:
        def category_namespace(self):
            return "分类"

        def category_aliases(self, aliases):
            return {"同人志": aliases["doujinshi"]}

        def category_mask(self, value, aliases):
            return aliases.get(value.casefold())

        def translate_query(self, query):
            return query

    svc.translator = Translator()

    async def fake_html_get(path, params=None):
        captured.append(dict(params or {}))
        return FIXTURE_HTML

    monkeypatch.setattr(svc, "_html_get", fake_html_get)
    await svc.search_galleries(query="分类:同人志")
    assert "f_search" not in captured[-1]
    assert captured[-1]["f_cats"] == "1021"
    await svc.search_galleries(query="category:同人志", last_gid="1")
    assert "f_search" not in captured[-1]
    assert captured[-1]["f_cats"] == "1021"
    await svc.search_galleries(query="reclass:doujinshi")
    assert captured[-1]["f_search"] == "reclass:doujinshi"
    assert "f_cats" not in captured[-1]
    await svc.close()

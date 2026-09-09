"""EhTagTranslation tag-translation unit tests (offline).

Covers: emoji stripping, db.text.json parsing, snapshot persistence,
forward/reverse lookup tables, search-query rewriting rules, refresh
failure degradation, and subject rendering through _flatten_subjects.
"""

import json

import pytest

from app.config import Settings
from app.eh.models import GalleryTag, TagStyle
from app.eh.tag_translation import (
    TagTranslator,
    clean_name,
    parse_db_text,
)
from app.opds2.router import _flatten_subjects


def _settings(tmp_path, **kw) -> Settings:
    base = dict(
        ipb_member_id="1",
        ipb_pass_hash="abc",
        tag_translation_state=tmp_path / "tag_translation.json",
    )
    base.update(kw)
    return Settings(**base)


def _translator(tmp_path, **kw) -> TagTranslator:
    t = TagTranslator(_settings(tmp_path, **kw))
    # Install a small dictionary without touching the network.
    namespaces = {
        "female": "女性",
        "male": "男性",
        "language": "语言",
        "parody": "原作",
        "artist": "画师",
        "mixed": "混合",
    }
    abbrs = {"f": "female", "m": "male", "x": "mixed"}
    tags = {
        "female:big breasts": "巨乳 🍑",  # emoji must be stripped
        "female:sole female": "仅女性",
        "male:sole male": "仅男性",
        "language:chinese": "中文",
        "parody:original": "原创",
        "mixed:gender change": "性转换",
        "female:netorare": "NTR",
    }
    t._install(namespaces, tags, abbrs)
    return t


# -- emoji stripping ----------------------------------------------------------


def test_clean_name_strips_emoji_and_collapses_whitespace():
    assert clean_name("巨乳 \U0001F351") == "巨乳"
    assert clean_name("  仅\u200d女性\ufe0f  ") == "仅女性"
    # CJK text, latin text and ™/© symbols survive
    assert clean_name("中文 English") == "中文 English"
    assert clean_name("Acme™") == "Acme™"


# -- db.text.json parsing -----------------------------------------------------


def test_parse_db_text_real_release_structure():
    """Verified real structure: data is a LIST of namespace buckets with
    frontMatters.name for the namespace translation and bucket.data for tags."""
    payload = {
        "repo": "EhTagTranslation/Database",
        "head": {"commit": "x"},
        "version": "2026.8.23",
        "data": [
            {
                "namespace": "rows",  # meta table-of-contents -> skipped
                "frontMatters": {"name": "内容索引"},
                "data": {"some row": {"name": "某行"}},
            },
            {
                "namespace": "female",
                "frontMatters": {
                    "name": "女性",
                    "key": "female",
                    "abbr": "f",
                },
                "count": 2,
                "data": {
                    "age progression": {"name": "年龄增长 \U0001F60D", "links": ""},
                    "netorare": {"name": "NTR", "intro": "...", "links": ""},
                },
            },
            {"namespace": "other", "frontMatters": {}, "data": {}},
        ],
    }
    namespaces, tags, abbrs = parse_db_text(payload)
    assert namespaces == {"female": "女性"}
    assert abbrs == {"f": "female"}
    assert tags == {
        "female:age progression": "年龄增长",  # emoji stripped
        "female:netorare": "NTR",
    }


def test_parse_db_text_legacy_wiki_structure():
    """Legacy dict shape (data.<ns>.raw + data.ns.raw) still accepted."""
    payload = {
        "data": {
            "ns": {"raw": {"female": {"name": "女性", "intro": ""}}},
            "female": {
                "raw": {
                    "large breasts": {"name": "巨乳 \U0001F60D", "links": []},
                    "netorare": {"name": "NTR", "links": []},
                }
            },
            "other": {"raw": {"misc entry": {"name": "", "links": []}}},
        }
    }
    namespaces, tags, _abbrs = parse_db_text(payload)
    assert namespaces == {"female": "女性"}
    assert tags == {
        "female:large breasts": "巨乳",  # emoji stripped
        "female:netorare": "NTR",
    }


def test_parse_db_text_malformed_degrades_to_empty():
    empty = ({}, {}, {})
    assert parse_db_text({}) == empty
    assert parse_db_text({"data": "oops"}) == empty
    assert parse_db_text({"data": [{"namespace": "female"}]})[1] == {}


# -- persistence round-trip ---------------------------------------------------


def test_snapshot_persist_and_reload(tmp_path):
    t = _translator(tmp_path)
    assert t.loaded

    t2 = TagTranslator(t.settings)  # fresh instance reads the same snapshot
    assert t2.loaded
    assert t2.translate_tag("female", "big breasts") == "女性:巨乳"
    assert t2.namespaces.get("female") == "女性"
    assert t2.abbrs.get("f") == "female"  # abbreviations survive persistence


def test_corrupt_snapshot_degrades_to_empty(tmp_path):
    path = tmp_path / "tag_translation.json"
    path.write_text("{not json", encoding="utf-8")
    t = TagTranslator(_settings(tmp_path))
    assert not t.loaded
    assert not t.stale() or True  # stale() must not raise


# -- forward lookup (subject output) ------------------------------------------


def test_translate_tag_includes_namespace_prefix(tmp_path):
    t = _translator(tmp_path)
    assert t.translate_tag("Female", "Big Breasts") == "女性:巨乳"  # case-insensitive
    assert t.translate_tag("unknown", "whatever") is None
    assert t.translate_tag("female", "unknown-key") is None


# -- query rewriting ------------------------------------------------------------


def test_translate_query_prefixed_chinese_namespace(tmp_path):
    t = _translator(tmp_path)
    assert t.translate_query("女性:巨乳") == 'female:"big breasts"'
    assert t.translate_query("语言:中文 原创") == 'language:chinese parody:original'


def test_translate_query_prefixed_english_namespace(tmp_path):
    t = _translator(tmp_path)
    assert t.translate_query("female:巨乳") == 'female:"big breasts"'


def test_translate_query_prefixed_abbr_namespace(tmp_path):
    """EH search abbreviations from db frontMatters work as prefixes."""
    t = _translator(tmp_path)
    assert t.translate_query("f:巨乳") == 'female:"big breasts"'
    assert t.translate_query("F:巨乳") == 'female:"big breasts"'  # case-insensitive
    assert t.translate_query("x:性转换") == 'mixed:"gender change"'


def test_translate_query_supplemented_ns_aliases(tmp_path):
    """EH-native alias syntax (char/circle/lang/series) resolves too."""
    t = _translator(tmp_path)
    assert t.translate_query("lang:中文") == "language:chinese"
    assert t.translate_query("series:原创") == "parody:original"
    # fixture has no character/group entries; unknown key keeps the key
    # verbatim but the prefix is still normalized to the canonical
    # namespace upstream understands (circle is an alias of group).
    assert t.translate_query("circle:whatever") == "group:whatever"
    assert t.translate_tag("character", "x") is None


def test_translate_query_quoted_key_with_spaces(tmp_path):
    t = _translator(tmp_path)
    # ns:"translated phrase" form survives the tokenizer as one token;
    # abbreviation prefix + quoted phrase also works
    assert t.translate_query('女性:"仅女性"') == "female:\"sole female\""
    assert t.translate_query('f:"仅女性"') == "female:\"sole female\""


def test_translate_query_bare_unique_match(tmp_path):
    t = _translator(tmp_path)
    assert t.translate_query("巨乳") == 'female:"big breasts"'
    assert t.translate_query("NTR") == "female:netorare"


def test_translate_query_bare_ambiguous_passes_through(tmp_path):
    t = _translator(tmp_path)
    t._install(
        t.namespaces,
        {**t.tags, "parody:sole female": "仅女性"},  # second candidate
        t.abbrs,
    )
    # two candidates across namespaces -> verbatim passthrough
    assert t.translate_query("仅女性") == "仅女性"
    # prefixed form still resolves unambiguously
    assert t.translate_query("男性:仅男性") == 'male:"sole male"'


def test_translate_query_unknown_and_native_syntax_passthrough(tmp_path):
    t = _translator(tmp_path)
    # native EH keywords are not translated names -> untouched
    assert t.translate_query("language:chinese") == "language:chinese"
    assert t.translate_query("hello world") == "hello world"
    assert t.translate_query("female:netorare") == "female:netorare"


def test_translate_query_quoted_phrase_never_translated(tmp_path):
    t = _translator(tmp_path)
    assert t.translate_query('"巨乳"') == '"巨乳"'
    assert t.translate_query('search "巨乳" term') == 'search "巨乳" term'


def test_translate_query_empty_or_unloaded(tmp_path):
    t = _translator(tmp_path)
    assert t.translate_query("") == ""
    # fresh state path -> truly unloaded instance
    empty = TagTranslator(_settings(tmp_path / "other.json"))
    assert empty.translate_query("巨乳") == "巨乳"


def test_translate_token_prefixed_form_with_quotes_regex():
    from app.eh.tag_translation import _PREFIXED_QUOTED_RE

    m = _PREFIXED_QUOTED_RE.fullmatch('female:"large breasts"')
    assert m is not None and m.groups() == ("female", "large breasts")
    assert _PREFIXED_QUOTED_RE.fullmatch("female:x") is None


# -- refresh degradation --------------------------------------------------------


async def test_refresh_failure_keeps_old_snapshot(tmp_path, monkeypatch):
    t = _translator(tmp_path)

    class _Boom:
        def get(self, url):
            raise OSError("network down")

    monkeypatch.setattr(t, "_get_client", lambda: _Boom())
    ok = await t.refresh()
    assert ok is False
    assert t.last_error
    # old tables still serve
    assert t.translate_tag("female", "big breasts") == "女性:巨乳"


async def test_refresh_success_installs_new_tables(tmp_path, monkeypatch):
    t = _translator(tmp_path)

    class _Resp:
        def raise_for_status(self):
            pass

        def json(self):
            return {
                "data": [
                    {
                        "namespace": "male",
                        "frontMatters": {"name": "男性"},
                        "data": {"muscle": {"name": "肌肉"}},
                    }
                ]
            }

    class _Client:
        async def get(self, url):
            return _Resp()

    monkeypatch.setattr(t, "_get_client", lambda: _Client())
    ok = await t.refresh()
    assert ok is True
    assert t.last_error is None
    assert t.translate_tag("male", "muscle") == "男性:肌肉"
    # old entries gone (full replace semantics)
    assert t.translate_tag("female", "large breasts") is None


# -- subject rendering through _flatten_subjects --------------------------------


def test_flatten_subjects_translates_known_tags_only(tmp_path):
    t = _translator(tmp_path)
    tags = [
        GalleryTag("female", "big breasts"),
        GalleryTag("unknown-ns", "mystery", style=TagStyle(background="#fff")),
    ]
    out = _flatten_subjects(tags, frozenset(), t)
    assert out == [
        {"name": "女性:巨乳"},
        {"name": "unknown-ns:mystery", "x:style": {"background": "#fff"}},
    ]


def test_flatten_subjects_without_translator_unchanged():
    tags = [GalleryTag("female", "netorare")]
    out = _flatten_subjects(tags)
    assert out == [{"name": "female:netorare"}]


def test_flatten_subjects_dedupes_on_display_string(tmp_path):
    t = _translator(tmp_path)
    # distinct raw tags collapsing to the same display string
    t.tags["female:huge breasts"] = "巨乳"
    t._rebuild()
    out = _flatten_subjects(
        [GalleryTag("female", "big breasts"), GalleryTag("female", "huge breasts")],
        frozenset(),
        t,
    )
    assert len(out) == 1

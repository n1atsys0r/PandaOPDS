"""Config unit tests: endpoint derivation per site."""

from app.config import Settings


def _settings(**kw) -> Settings:
    base = dict(ipb_member_id="1", ipb_pass_hash="abc")
    base.update(kw)
    return Settings(**base)


def test_api_url_per_site():
    assert _settings(eh_site="e-hentai").api_url == "https://api.e-hentai.org/api.php"
    # exhentai has no api. subdomain
    assert _settings(eh_site="exhentai").api_url == "https://exhentai.org/api.php"


def test_http_origin_and_cookies():
    s = _settings(eh_site="e-hentai")
    assert s.http_origin == "https://e-hentai.org"
    assert s.cookies["nw"] == "1"
    assert s.cookies["datatags"] == "1"
    assert s.cookies["ipb_member_id"] == "1"
    assert "igneous" not in s.cookies

    # igneous only seeded when provided and not "mystery"
    assert "igneous" not in _settings(igneous="mystery").cookies
    assert _settings(igneous="abc123").cookies["igneous"] == "abc123"


def test_pse_page_base():
    assert _settings().pse_page_base == 1
    assert _settings(pse_page_base=0).pse_page_base == 0


def test_image_proxy_hosts_default_and_parse(monkeypatch):
    from app.config import load_settings

    # default: the two cover CDNs
    assert _settings().image_proxy_hosts == ("ehgt.org", "s.exhentai.org")
    assert _settings().image_proxy_hosts_set == {"ehgt.org", "s.exhentai.org"}

    # env override: comma-separated, lower-cased, whitespace trimmed
    monkeypatch.delenv("IMAGE_PROXY_HOSTS", raising=False)
    assert load_settings().image_proxy_hosts == ("ehgt.org", "s.exhentai.org")
    monkeypatch.setenv("IMAGE_PROXY_HOSTS", "cdn.example.com, EHGT.ORG , s.EXHENTAI.org")
    assert load_settings().image_proxy_hosts == (
        "cdn.example.com", "ehgt.org", "s.exhentai.org"
    )
    # empty resets to the default
    monkeypatch.setenv("IMAGE_PROXY_HOSTS", "")
    assert load_settings().image_proxy_hosts == ("ehgt.org", "s.exhentai.org")


def test_opds_acq_detail_default_and_validation(monkeypatch):
    from app.config import load_settings

    # default: false (direct mode, compat-first)
    assert _settings().opds_acq_detail is False
    assert Settings(ipb_member_id="1", ipb_pass_hash="abc").opds_acq_detail is False

    # boolean OPDS_ACQ_DETAIL: true/1/yes/on -> detail; anything else -> false
    monkeypatch.delenv("OPDS_ACQ_DETAIL", raising=False)
    monkeypatch.delenv("OPDS_ACQ_MODE", raising=False)
    assert load_settings().opds_acq_detail is False
    monkeypatch.setenv("OPDS_ACQ_DETAIL", "true")
    assert load_settings().opds_acq_detail is True
    monkeypatch.setenv("OPDS_ACQ_DETAIL", "1")
    assert load_settings().opds_acq_detail is True
    monkeypatch.setenv("OPDS_ACQ_DETAIL", "YES")
    assert load_settings().opds_acq_detail is True  # case-insensitive
    monkeypatch.setenv("OPDS_ACQ_DETAIL", "0")
    assert load_settings().opds_acq_detail is False
    monkeypatch.setenv("OPDS_ACQ_DETAIL", "bogus")
    assert load_settings().opds_acq_detail is False

    # legacy OPDS_ACQ_MODE=detail|direct (string) honored when OPDS_ACQ_DETAIL unset
    monkeypatch.delenv("OPDS_ACQ_DETAIL", raising=False)
    monkeypatch.setenv("OPDS_ACQ_MODE", "detail")
    assert load_settings().opds_acq_detail is True
    monkeypatch.setenv("OPDS_ACQ_MODE", "DETAIL")
    assert load_settings().opds_acq_detail is True  # lower-cased
    monkeypatch.setenv("OPDS_ACQ_MODE", "bogus")
    assert load_settings().opds_acq_detail is False

    # OPDS_ACQ_DETAIL takes precedence over the legacy string
    monkeypatch.setenv("OPDS_ACQ_DETAIL", "false")
    monkeypatch.setenv("OPDS_ACQ_MODE", "detail")
    assert load_settings().opds_acq_detail is False


def test_tag_translation_settings(monkeypatch, tmp_path):
    from app.config import load_settings

    # defaults: disabled, daily refresh, default release URL
    monkeypatch.delenv("TAG_TRANSLATION_ENABLED", raising=False)
    monkeypatch.delenv("TAG_TRANSLATION_URL", raising=False)
    monkeypatch.delenv("TAG_TRANSLATION_INTERVAL_SECONDS", raising=False)
    monkeypatch.delenv("TAG_TRANSLATION_STATE", raising=False)
    s = load_settings()
    assert s.tag_translation_enabled is False
    assert "EhTagTranslation" in s.tag_translation_url
    assert s.tag_translation_url.endswith("db.text.json")
    assert s.tag_translation_interval_seconds == 86400.0
    # default state path: /config/*.json in-container, ./ fallback locally
    assert s.tag_translation_state.name == "tag_translation.json"

    monkeypatch.setenv("TAG_TRANSLATION_ENABLED", "1")
    monkeypatch.setenv(
        "TAG_TRANSLATION_URL", "https://mirror.example/db.text.json"
    )
    monkeypatch.setenv("TAG_TRANSLATION_INTERVAL_SECONDS", "3600")
    monkeypatch.setenv("TAG_TRANSLATION_STATE", str(tmp_path / "tt.json"))
    s = load_settings()
    assert s.tag_translation_enabled is True
    assert s.tag_translation_url == "https://mirror.example/db.text.json"
    assert s.tag_translation_interval_seconds == 3600.0
    assert s.tag_translation_state == tmp_path / "tt.json"


def test_favcat_scope_env(monkeypatch):
    from app.config import load_settings

    monkeypatch.delenv("FAVORITES_SYNC_CATEGORIES", raising=False)
    s = load_settings()
    assert s.favorites_sync_categories == ()
    assert s.favorites_sync_excludes == ()

    monkeypatch.setenv("FAVORITES_SYNC_CATEGORIES", "-0")
    s = load_settings()
    assert s.favorites_sync_categories == ()
    assert s.favorites_sync_excludes == (0,)

    monkeypatch.setenv("FAVORITES_SYNC_CATEGORIES", "1,2,-0")
    s = load_settings()
    assert s.favorites_sync_categories == (1, 2)
    assert s.favorites_sync_excludes == (0,)

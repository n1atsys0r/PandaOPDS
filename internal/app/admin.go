package app

import (
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"

	"pandaopds/app/webui"
	"pandaopds/internal/config"
	"pandaopds/internal/home"
)

const appVersion = "0.2.0"

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	site := map[string]any{"site": s.Settings.EHSite, "host": s.Settings.SiteHost(), "public_base_url": s.Settings.PublicBaseURL}
	if site["public_base_url"] == "" {
		site["public_base_url"] = "(relative URLs)"
	}
	status := "ok"
	if !s.configOK {
		status = "degraded"
	}
	writeJSON(w, 200, map[string]any{"status": status, "config_error": s.configError, "site": s.Settings.EHSite, "host": s.Settings.SiteHost(), "public_base_url": site["public_base_url"], "stats": s.Service.Status()})
}
func (s *Server) apiStatus(w http.ResponseWriter, r *http.Request) {
	counts := s.Service.Throttle.Counts()
	snap := s.Service.Status()
	cache, _ := snap["cache"].(map[string]any)
	img := cache["image"]
	homeCfg, homeUsing, homeErr := s.currentHome()
	mem := map[string]any{"size": cache["list_entries"], "max_entries": 2000, "hits": 0, "misses": 0}
	disk := img
	site := map[string]any{"site": s.Settings.EHSite, "host": s.Settings.SiteHost(), "http_origin": "https://" + s.Settings.SiteHost(), "api_url": apiURL(s.Settings.EHSite), "public_base_url": s.Settings.PublicBaseURL, "is_exhentai": s.Settings.EHSite == "exhentai", "has_ipb": s.Settings.HasIPB()}
	if s.Settings.PublicBaseURL == "" {
		site["public_base_url"] = "（相对路径）"
	}
	th := map[string]any{"html_requests": counts["html"], "api_requests": counts["api"], "image_requests": counts["image"], "thumb_requests": counts["thumb"], "max_concurrency": s.Settings.MaxConcurrency, "image_max_concurrency": s.Settings.ImageMaxConcurrency, "thumb_max_concurrency": s.Settings.ThumbMaxConcurrency, "html_interval_seconds": s.Settings.HTMLIntervalSeconds}
	writeJSON(w, 200, map[string]any{"app": map[string]any{"config_ok": s.configOK, "config_error": s.configError, "version": appVersion, "auth_enabled": s.Settings.AuthEnabled()}, "site": site, "throttle": th, "circuit": s.Service.Throttle.Circuit.Snapshot(), "memory_cache": mem, "disk_cache": disk, "archive": s.Archives.Stats(), "favorites": s.Favorites.Status(), "home": map[string]any{"path": s.Settings.HomeConfigPath, "using_file": homeUsing, "error": homeErr, "groups": len(homeCfg.Groups), "sections": len(homeCfg.Sections)}})
}
func apiURL(site string) string {
	if site == "exhentai" {
		return "https://exhentai.org/api.php"
	}
	return "https://api.e-hentai.org/api.php"
}
func (s *Server) currentHome() (home.Config, bool, any) {
	cfg, err := home.Load(s.Settings.HomeConfigPath)
	if err != nil {
		return home.Default(), false, err.Error()
	}
	if _, e := os.Stat(s.Settings.HomeConfigPath); e == nil {
		return cfg, true, nil
	}
	return cfg, false, nil
}
func (s *Server) apiHome(w http.ResponseWriter, r *http.Request) {
	cfg, using, err := s.currentHome()
	groups := []map[string]any{}
	for _, g := range cfg.Groups {
		groups = append(groups, map[string]any{"id": g.ID, "title": g.Title})
	}
	sections := []map[string]any{}
	for _, sec := range cfg.Sections {
		count := 0
		if sec.Count != nil {
			count = *sec.Count
		}
		var group any
		if sec.Group != "" {
			group = sec.Group
		}
		title := sec.Title
		if title == "" {
			title = sec.Query
		}
		sections = append(sections, map[string]any{"kind": sec.Kind, "title": title, "type": sec.Type, "query": sec.Query, "count": count, "group": group, "href": home.BuildHref(sec.Type, sec.Query), "auth_required": home.AuthRequired(sec)})
	}
	writeJSON(w, 200, map[string]any{"path": s.Settings.HomeConfigPath, "using_file": using, "error": err, "groups": groups, "sections": sections})
}
func (s *Server) apiConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.Settings
	ehentaiHost := "exhentai.org"
	if cfg.EHSite == "exhentai" {
		ehentaiHost = "e-hentai.org"
	}
	groups := []map[string]any{
		{"id": "auth", "title": "访问认证（Basic Auth）", "fields": []map[string]any{
			field("auth_username", "认证用户名", firstNonEmpty(cfg.AuthUsername, "（未设置）"), false, true, "两者都设置才启用；未启用时服务公开可访问"),
			field("auth_password", "认证密码", nil, true, cfg.AuthPassword != "", "明文环境变量，脱敏显示；仅建议 HTTPS 反代下启用"),
			field("auth_enabled", "认证启用", choose(cfg.AuthEnabled(), "开（全部路由需凭据，/health 除外）", "关（未配置）"), false, true, "由 AUTH_USERNAME + AUTH_PASSWORD 同时设置推导"),
			field("auth_exempt_paths", "豁免路径", joinOr(sorted(cfg.AuthExemptPaths), "（无）"), false, true, "AUTH_EXEMPT_PATHS：逗号分隔的精确路径，认证下仍公开；/health 恒豁免"),
			field("auth_exempt_prefixes", "豁免前缀", joinOr(sorted(cfg.AuthExemptPrefixes), "（无）"), false, true, "AUTH_EXEMPT_PREFIXES：逗号分隔的路径前缀，命中即公开；如 /image/ 让 <img> 封面免认证"),
			field("image_proxy_hosts", "评论封面代理 host 白名单", strings.Join(sorted(cfg.ImageProxyHosts), ", "), false, true, "IMAGE_PROXY_HOSTS：逗号分隔、允许经 /image/fetch 代理的 host；评论封面/预览图命中才重写为同源，否则客户端跨域拉图被 CORS 拦"),
		}},
		{"id": "identity", "title": "E-Hentai 身份与站点", "fields": []map[string]any{
			field("eh_site", "站点", cfg.EHSite, false, true, "页面 host: "+cfg.SiteHost()),
			field("ipb_member_id", "IPB Member ID", nil, true, cfg.IPBMemberID != "", "登录标识；未设置时 Watched/Favorites 导航不输出"),
			field("ipb_pass_hash", "IPB Pass Hash", nil, true, cfg.IPBPassHash != "", "登录凭据，脱敏显示"),
			field("igneous", "Ignéous 种子", nil, true, cfg.Igneous != "" && !strings.EqualFold(cfg.Igneous, "mystery"), "可选；exhentai 会话建立时自动下发，无需提供"),
			field("nw", "NW（绕过 Offensive 警告）", cfg.NW, false, true, nil),
			field("datatags", "DATATAGS（新缩略图结构）", cfg.DataTags, false, true, nil),
			field("eh_profile", "uconfig 独立 Profile", firstNonEmpty(cfg.EHProfile, "（关闭）"), false, true, nil),
		}},
		{"id": "http", "title": "HTTP 与节流", "fields": []map[string]any{
			field("timeout_seconds", "出站超时（秒）", cfg.TimeoutSeconds, false, true, nil),
			field("retries", "网络错误重试次数", cfg.Retries, false, true, nil),
			field("html_interval_seconds", "HTML 请求最小间隔（秒）", cfg.HTMLIntervalSeconds, false, true, "防封关键参数"),
			field("max_concurrency", "HTML/API 并发上限", cfg.MaxConcurrency, false, true, nil),
			field("image_max_concurrency", "全图并发上限", cfg.ImageMaxConcurrency, false, true, nil),
			field("thumb_max_concurrency", "封面图并发上限", cfg.ThumbMaxConcurrency, false, true, "对齐浏览器在源站 25 缩略图并发"),
		}},
		{"id": "opds", "title": "OPDS / PSE", "fields": []map[string]any{
			field("public_base_url", "对外基础 URL", firstNonEmpty(cfg.PublicBaseURL, "（相对路径）"), false, true, "设置后 feed 输出绝对 URL（Stump 等客户端必需）"),
			field("pse_page_base", "PSE 页码基数", cfg.PSEPageBase, false, true, "1 = 1-based（默认，与主流 PSE 阅读器一致）；0 = 规范原文"),
			field("opds_acq_detail", "acquisition 指向详情文档", choose(cfg.OPDSAcqDetail, "是（二次请求流程）", "否（直接指向图片流）"), false, true, nil),
			field("list_layout", "列表布局", "extended", false, true, "固定 extended（元数据最全），不可配置"),
		}},
		{"id": "breaker", "title": "熔断与缓存", "fields": []map[string]any{
			field("banned_cooldown_seconds", "IP 封禁熔断冷却（秒）", cfg.BannedCooldownSeconds, false, true, nil),
			field("exceed_cooldown_seconds", "图片限额熔断冷却（秒）", cfg.ExceedCooldownSeconds, false, true, nil),
			field("image_source_retries", "换源重试次数（IMAGE_SOURCE_RETRIES）", cfg.ImageSourceRetries, false, true, "同 CDN 重试耗尽后换源轮次；0=关闭，1=?nl，2=重抓详情（默认 2）"),
			field("cache_dir", "磁盘缓存目录", cfg.CacheDir, false, true, nil),
			field("cache_max_gb", "磁盘缓存上限（GB）", cfg.CacheMaxGB, false, true, nil),
			field("image_cache_enabled", "磁盘缓存开关", boolText(cfg.ImageCacheEnabled), false, true, nil),
			field("metadata_ttl_seconds", "元数据缓存 TTL（秒）", cfg.MetadataTTLSeconds, false, true, "gdata 结果；主链路零 gdata"),
			field("page_url_ttl_seconds", "页面 URL 映射 TTL（秒）", cfg.PageURLTTLSeconds, false, true, nil),
			field("list_cache_ttl_seconds", "列表解析缓存 TTL（秒）", cfg.ListCacheTTLSeconds, false, true, nil),
		}},
		{"id": "tags", "title": "标签与评论", "fields": []map[string]any{
			field("tag_status_filter", "标签可信度过滤", cfg.TagStatusFilter, false, true, "strict（仅 confidence）/ balanced（默认）/ off"),
			field("comments_enabled", "评论区输出（x:reviews）", boolText(cfg.CommentsEnabled), false, true, nil),
		}},
		{"id": "home", "title": "首页与分类", "fields": []map[string]any{
			field("home_config_path", "首页布局配置文件", cfg.HomeConfigPath, false, true, "仅 OPDS 2.0 消费；缺失/损坏时回退内置默认布局"),
			field("facets", "分类筛选（facets）", facetText(cfg.Facets), false, true, nil),
		}},
		{"id": "archive", "title": "归档（archiver）", "fields": []map[string]any{
			field("archive_dir", "归档 ZIP 目录", cfg.ArchiveDir, false, true, "持久目录：保存 ZIP 母本；状态与图库 sidecar 位于 ARCHIVE_STATE_DIR，不受磁盘缓存 LRU/7 天 TTL 约束"),
			field("archive_quality", "默认画质档位", cfg.ArchiveQuality, false, true, "start API 未传 quality 时的默认档（匹配 archiver 页面选项，默认 original 无损）"),
			field("archive_download_concurrency", "归档任务并发", cfg.ArchiveDownloadConcurrency, false, true, "下载/7z 转换并发上限，其余排队；不受阅读限流约束（仍过熔断）"),
			field("archive_update_interval_days", "归档版本检查周期（天）", cfg.ArchiveUpdateIntervalSeconds/86400, false, true, "默认 7 天；0 关闭后台检查"),
			field("archive_auto_update", "自动更新归档", cfg.ArchiveAutoUpdate, false, true, "启用后可能消耗 GP"),
			field("archive_enabled", "启用条件", choose(cfg.HasIPB(), "开（已配置 IPB 登录态）", "关（需 IPB_MEMBER_ID + IPB_PASS_HASH）"), false, true, "archiver 为会员服务（星会员 + GP）；无登录态时归档 API 返回 403"),
		}},
		{"id": "favorites", "title": "收藏夹（操作与同步）", "fields": []map[string]any{
			field("favorites_enabled", "启用条件", choose(cfg.HasIPB(), "开（已配置 IPB 登录态）", "关（需 IPB_MEMBER_ID + IPB_PASS_HASH）"), false, true, "写操作（add/move/remove）与目录列表需要登录态；无登录态时 API 返回 403"),
			field("favorites_sync_interval_seconds", "同步周期（秒）", cfg.FavoritesSyncIntervalSeconds, false, true, "默认 3600=每小时；0 = 关闭周期扫描（写操作后的防抖触发 + 手动 sync/run 仍生效）"),
			field("favorites_sync_archive", "自动归档新增项", choose(cfg.FavoritesSyncArchive, "开（消耗 GP！）", "关（仅扫描记录）"), false, true, "必须显式开启；对每次扫描发现的新增收藏项调用 archiver start（首次运行仅建档不归档，自第二次起生效）"),
			field("favorites_sync_categories", "自动归档范围", favScope(cfg), false, true, "FAVORITES_SYNC_CATEGORIES：只决定哪些新增收藏会被自动归档；扫描恒全量。逗号分隔的收藏夹 ID；留空 = 全归档；-ID = 排除该分区（如 -0）"),
			field("favorites_sync_state", "同步状态文件", cfg.FavoritesSyncState, false, true, "记录已扫描/已归档 gid；删除可重置增量扫描基线"),
			field("favorites_sync_match_threshold", "增量停止阈值", cfg.FavoritesSyncMatchThreshold, false, true, "连续 N 个已知 gid 即停止翻页"),
			field("favorites_sync_max_pages", "单轮扫描页数上限", cfg.FavoritesSyncMaxPages, false, true, "硬上限，防止异常时无限翻页"),
		}},
	}
	derived := map[string]any{"site_host": cfg.SiteHost(), "http_origin": "https://" + cfg.SiteHost(), "api_url": apiURL(cfg.EHSite), "ehentai_host": ehentaiHost, "is_exhentai": cfg.EHSite == "exhentai", "has_ipb": cfg.HasIPB(), "auth_enabled": cfg.AuthEnabled(), "auth_exempt_paths": sorted(cfg.AuthExemptPaths), "auth_exempt_prefixes": sorted(cfg.AuthExemptPrefixes), "image_proxy_hosts": sorted(cfg.ImageProxyHosts), "archive_enabled": cfg.HasIPB(), "favorites_sync_interval_seconds": cfg.FavoritesSyncIntervalSeconds, "favorites_sync_archive": cfg.FavoritesSyncArchive, "favorites_sync_categories": intList(cfg.FavoritesSyncCategories), "favorites_sync_excludes": intList(cfg.FavoritesSyncExcludes)}
	writeJSON(w, 200, map[string]any{"derived": derived, "groups": groups})
}
func (s *Server) cacheClear(w http.ResponseWriter, r *http.Request) {
	before := s.Service.Status()
	cache, _ := before["cache"].(map[string]any)
	disk, _ := cache["image"].(map[string]any)
	removed, _ := disk["entries"].(int)
	if e := s.Service.ClearCache(); e != nil {
		writeJSON(w, 500, map[string]any{"detail": e.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"cleared": removed, "enabled": s.Settings.ImageCacheEnabled})
}
func (s *Server) webPage(w http.ResponseWriter, r *http.Request) {
	writeBytes(w, 200, "text/html; charset=utf-8", map[string]string{"Cache-Control": "no-cache"}, webui.Page)
}
func field(key, label string, value any, masked bool, set bool, note any) map[string]any {
	var v any = value
	if masked {
		v = "••••••••"
	}
	return map[string]any{"key": key, "label": label, "value": v, "note": note, "masked": masked, "set": set}
}
func choose(value bool, yes, no string) string {
	if value {
		return yes
	}
	return no
}
func boolText(b bool) string {
	if b {
		return "开"
	}
	return "关"
}
func joinOr(a []string, f string) string {
	if len(a) == 0 {
		return f
	}
	return strings.Join(a, ", ")
}
func facetText(in []config.Facet) string {
	out := []string{}
	for _, f := range in {
		out = append(out, fmt.Sprintf("%s（掩码 %d）", f.Name, f.Mask))
	}
	return strings.Join(out, ", ")
}
func favScope(c config.Settings) string {
	base := "全部"
	if len(c.FavoritesSyncCategories) > 0 {
		parts := []string{}
		for _, v := range c.FavoritesSyncCategories {
			parts = append(parts, fmt.Sprint(v))
		}
		base = strings.Join(parts, ", ")
	}
	if len(c.FavoritesSyncExcludes) > 0 {
		parts := []string{}
		for _, v := range c.FavoritesSyncExcludes {
			parts = append(parts, fmt.Sprintf("-%d", v))
		}
		return base + "（排除 " + strings.Join(parts, ", ") + "）"
	}
	return base
}
func sorted(a []string) []string { b := append([]string(nil), a...); sort.Strings(b); return b }
func intList(a []int) []int      { return append([]int{}, a...) }
func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

# E-Hentai 上游集成

Go 实现位于 `internal/eh/`。本页记录上游交互和运行语义；HTTP API 的精确路径及响应以 [`api/openapi.json`](../api/openapi.json) 为准，配置项见[配置指南](configuration.md)。

## 会话与请求数据流

`EH_SITE` 选择 e-hentai.org 或 exhentai.org。请求首次需要会话时，客户端使用配置 cookie 建立会话、加载 uconfig profile；访问 ExHentai 时先访问 E-Hentai，再请求 ExHentai，由 cookie jar 维护会话级 `igneous`。`IPB_MEMBER_ID` 与 `IPB_PASS_HASH` 须成对配置才启用登录能力；未登录仍可访问公开目录。

日常浏览从列表 HTML 解析搜索、最新、热门、Watched、Favorites 与 Toplist 条目。图库详情 HTML 提供分页缩略图及详情字段；随后图片页解析真实图像 URL，才拉取图片字节。**gdata 不在普通浏览主链路中**；它保留作元数据补充，并用于归档完成后的快照及用户主动刷新。封面通常复用列表解析得到的 URL，未命中时才回退详情页。

## 页面解析与缓存

详情页解析器支持新版 `#gdt` 缩略图结构（包括 `data-orghash`/MPV 链接）和旧版 `.gdtm`/`.gdtl` 结构。页面 URL 映射缓存图片页地址，默认 TTL 为 1 小时（`PAGE_URL_TTL_SECONDS`）；列表解析缓存默认 10 分钟（`LIST_CACHE_TTL_SECONDS`），解析时也缓存封面 URL。图片字节走独立磁盘 LRU，默认上限 4 GB、按 7 天淘汰；`CACHE_DIR`、`CACHE_MAX_GB` 和 `IMAGE_CACHE_ENABLED` 控制该层。详情 URL 映射和图片字节缓存用途不同，清理/命中不能相互替代。归档 zip 则是独立的长期缓存，见[归档说明](archive.md)。

## 限流与失败处理

HTML/API、原图、缩略图请求使用不同并发类别，并由 `internal/eh/throttle.go` 执行请求间隔和熔断。网络读写错误按重试配置退避重试；403/503 与明确的 fatal server error 会重试。图像源 URL 失效时服务可重新解析详情页 URL；图片页提供 reload key 时可附带 `nl` 重试。

| 上游信号 | 处理原则 |
|---|---|
| `Your IP address` / `This IP address` | 识别为 IP 封禁，触发全局封禁冷却，返回服务不可用。 |
| `You have exceeded your image` 或 509 占位图 | 识别图片配额耗尽，触发图片限额冷却；不按一般网络错误重复请求。 |
| 空响应 | 作为登录态失效/需要登录信号，返回上游错误。 |
| HTTP 403 | 按 Cloudflare/拒绝访问类失败进行有限重试。 |
| HTTP 404 | 作为图库/页面不存在返回；图像源重解析不会对 404 无限重试。 |
| fatal server error 文本、临时网络读写错误 | 有限退避重试，耗尽后返回错误；HTTP 403/503 也会有限重试，其他 HTTP 错误按状态返回。 |

服务由多个客户端共用同一出口 IP，避免调高并发或在熔断期间持续试探。排障时查看熔断、节流和缓存状态，不要记录 cookie 明文。页面结构变化时更新 Go 解析器及 fixture 测试；Go 实现是当前行为依据。

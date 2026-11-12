# 部署与配置

Go 配置解析和默认值以 [`internal/config/settings.go`](../internal/config/settings.go) 为准；Compose/Docker 可能在容器环境中覆盖部分默认值。`.env.example` 提供可复制的配置模板。所有变量均可省略。

## Docker Compose

```bash
cp .env.example .env   # 可选
docker compose up -d
```

Compose 默认拉取发布镜像。升级运行 `docker compose pull && docker compose up -d`；本地构建使用：

```bash
docker compose -f docker-compose.yml -f docker-compose.build.yml up -d --build
```

Compose 用命名卷保存图片缓存和归档，并将宿主机的 `./config` 挂载到 `/config`。要覆盖 `OPDS_ACQ_DETAIL`，请在 `.env` 或 Compose 覆盖文件中显式设置。

## 环境变量

下面默认值是 Go 应用默认值；部署模板或镜像可覆盖它们。

| 变量 | 默认值 | 用途 |
|---|---:|---|
| `EH_SITE` | `e-hentai` | 上游站点：`e-hentai` 或 `exhentai`。 |
| `PORT` | `8000` | Go HTTP 服务监听端口（读取自进程环境变量）。 |
| `SETTINGS_CONFIG` | 本地 `./config/settings.toml`，Docker `/config/settings.toml` | 网页可编辑配置的 TOML 文件路径。 |
| `IPB_MEMBER_ID`, `IPB_PASS_HASH` | 空 | 可选登录 cookie；须成对提供以启用需登录功能。 |
| `IGNEOUS` | 空 | 可选会话 cookie 种子；ExHentai 可在建立會話時取得 session cookie。 |
| `NW`, `DATATAGS` | `1`, `1` | 上游 cookie 偏好：安全警告绕过与新版标签缩略图结构。 |
| `PUBLIC_BASE_URL` | 空 | feed 中使用的外部基址；反代部署时设为外部 HTTPS URL。 |
| `AUTH_USERNAME`, `AUTH_PASSWORD` | 空 | 两者均非空时启用 HTTP Basic Auth。 |
| `AUTH_EXEMPT_PATHS`, `AUTH_EXEMPT_PREFIXES` | 空 | 认证豁免的精确路径列表与路径前缀列表；`/health` 恒豁免。 |
| `TIMEOUT_SECONDS`, `RETRIES` | `6`, `3` | 上游请求超时与重试次数。 |
| `HTML_INTERVAL_SECONDS` | `0.3` | HTML 请求最小间隔秒数。 |
| `MAX_CONCURRENCY` | `5` | HTML/API 请求并发上限。 |
| `IMAGE_MAX_CONCURRENCY` | `5` | 原图请求并发上限。 |
| `THUMB_MAX_CONCURRENCY` | `25` | 缩略图请求并发上限。 |
| `BANNED_COOLDOWN_SECONDS`, `EXCEED_COOLDOWN_SECONDS` | `1800`, `300` | IP 封禁与图片限额熔断冷却秒数。 |
| `CACHE_DIR`, `CACHE_MAX_GB`, `IMAGE_CACHE_ENABLED` | `./cache`, `4`, `true` | 图片字节磁盘缓存位置、容量上限和开关；`CACHE_DIR/archive-staging` 保存归档续传与转换临时文件，不受图片缓存淘汰或清空影响。需要跨重启续传时，为该目录配置持久本地卷。 |
| `METADATA_TTL_SECONDS` | `600` | gdata 元数据内存缓存 TTL。 |
| `PAGE_URL_TTL_SECONDS` | `3600` | 详情页图片 URL 映射缓存 TTL。 |
| `LIST_CACHE_TTL_SECONDS` | `600` | 列表结果缓存 TTL。 |
| `IMAGE_SOURCE_RETRIES` | `2` | 图片源失效后的重新解析次数。 |
| `PSE_PAGE_BASE` | `1` | OPDS-PSE 页码基数，支持 1 或 0。 |
| `OPDS_ACQ_DETAIL` | `false` | v2.0 acquisition 是否先进入详情文档；旧 `OPDS_ACQ_MODE=detail` 兼容此行为。 |
| `TAG_STATUS_FILTER` | `balanced` | 标签可信度过滤：`balanced`、`strict`、`off`。 |
| `COMMENTS_ENABLED` | `true` | 控制 v2.0 详情是否输出评论扩展。 |
| `IMAGE_PROXY_HOSTS` | `ehgt.org,s.exhentai.org` | `/image/fetch` 允许代理的 host。 |
| `EH_PROFILE` | `PandaOPDS` | 上游 uconfig profile 名；空值关闭。 |
| `HOME_CONFIG` | `./config/home.toml` | v2.0 首页布局文件。 |
| `FACETS` | 内置 10 分类 | 分类筛选配置，`名称:掩码` 逗号分隔。 |
| `ARCHIVE_DIR` | `./archives` | 长期归档存储目录。 |
| `ARCHIVE_STATE_DIR` | `./config/archive`（容器内 `/config/archive`） | 本地归档状态与图库元数据目录；应与归档 ZIP 卷分开持久化。 |
| `ARCHIVE_QUALITY` | `original` | 默认归档画质选择。 |
| `ARCHIVE_DOWNLOAD_CONCURRENCY` | `5` | 归档下载和转换并发上限。 |
| `FAVORITES_SYNC_INTERVAL_SECONDS` | `3600` | 收藏增量扫描周期；`0` 关闭周期扫描。 |
| `FAVORITES_SYNC_ARCHIVE` | `false` | 是否自动归档新收藏；可能消耗 GP。 |
| `FAVORITES_SYNC_CATEGORIES` | 空 | 自动归档作用域 favcat ID；可用 `-ID` 排除分区。 |
| `FAVORITES_SYNC_STATE` | `./favorites_sync.json`* | 收藏同步状态快照路径。 |
| `FAVORITES_SYNC_MATCH_THRESHOLD` | `5` | 连续命中已知收藏后停止增量扫描。 |
| `FAVORITES_SYNC_MAX_PAGES` | `50` | 单轮扫描页数上限。 |
| `MYTAGS_TTL_SECONDS`, `MYTAGS_STATE` | `21600`, `./mytags.json`* | My Tags 样式缓存有效期及状态文件路径。 |
| `TAG_TRANSLATION_ENABLED` | `false` | 启用中文标签翻译和搜索反查。 |
| `TAG_TRANSLATION_URL` | GitHub latest | 标签词典下载 URL。 |
| `TAG_TRANSLATION_INTERVAL_SECONDS` | `86400` | 词典刷新周期；`0` 表示仅启动时拉取。 |
| `TAG_TRANSLATION_STATE` | `./tag_translation.json`* | 词典磁盘快照路径。 |

\* 若 `/config` 目录存在，三个状态文件的默认路径为 `/config/` 下同名文件。

`.env.example` 目前列有 `LOG_LEVEL`，但当前 Go 二进制没有读取该变量，因此设置它不会改变日志行为。

## 网页配置编辑器

服务设置保存在 `SETTINGS_CONFIG` 指定的 TOML 文件。环境变量优先于文件，文件优先于 Go 默认值。编辑器分别显示当前运行值和下次启动生效值，环境变量覆盖的项目显示为只读。若任一 IPB 登录变量来自环境，整对凭据由环境管理。保存后需重启服务，凭据在页面上仅显示设置状态。

归档版本检查周期和自动更新是仅存于该 TOML 文件的 `ARCHIVE_UPDATE_INTERVAL_DAYS` 与 `ARCHIVE_AUTO_UPDATE` 设置：周期默认 7 天，填 `0` 可关闭；自动更新默认关闭，启用后可能消耗 GP。请在网页的归档配置项中明确启用，同名环境变量会被忽略。

收藏自动归档开关 `FAVORITES_SYNC_ARCHIVE` 与归档范围 `FAVORITES_SYNC_CATEGORIES` 也可在网页的收藏夹配置项中设置。自动归档默认关闭，启用后可能消耗 GP，首次扫描只建立增量基线。范围使用逗号分隔的收藏夹 ID（0–9）；留空表示全部，`-ID` 表示排除该收藏夹。范围只限制自动归档，不影响同步扫描。环境变量优先于 `settings.toml`，因此由环境变量设置时网页显示只读；保存的文件设置在重启后生效。

配置和首页布局编辑均要求有效的 Basic Auth。认证豁免规则仅适用于常规接口，未配置 Basic Auth 的管理页提供只读视图。编辑接口的写请求接受同源 JSON，并要求使用读取时返回的 `ETag` 作为 `If-Match`。首页布局写入 `HOME_CONFIG` 指定的文件，旧文件备份为 `.bak`，下一次 OPDS 2.0 首页请求读取新布局。

## 反向代理

反代须转发原始请求路径和 `Host`/转发协议头，并将 `PUBLIC_BASE_URL` 配成用户访问的外部基址，确保 feed 中的链接指向外部地址。参考配置：[`nginx.conf.example`](../deploy/nginx.conf.example) 与 [`Caddyfile.example`](../deploy/Caddyfile.example)。

启用 Basic Auth 时必须经 HTTPS 提供服务。浏览器原生图片加载不能附带认证头；只有在了解访问影响后，才将 `/image/fetch` 加入 `AUTH_EXEMPT_PATHS`。管理页与 OpenAPI UI 遵循服务认证配置。

## 管理与契约

管理界面位于 `/`，Swagger UI 位于 `/docs`，运行时 OpenAPI 文档位于 `/openapi.json`。仓库契约文件是 [`api/openapi.json`](../api/openapi.json)。接口 schema 不在本指南重复维护。

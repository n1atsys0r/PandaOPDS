# OPDS 与 OPDS-PSE

精确路径、查询参数、媒体类型和响应结构以 [`api/openapi.json`](../api/openapi.json) 为契约；本页说明客户端行为层面的组织方式。

## 目录族与首页

- OPDS 1.2 使用 Atom，入口为 `/opds/v1.2`，含图库采集、Toplist、归档 shelf、章节详情和 OpenSearch 描述文档。
- OPDS 2.0 使用 JSON，入口为 `/opds/v2.0`，含导航/采集文档、Toplist、归档 shelf、图库详情与 publication 文档。
- 作品条目和 publication 提供 OPDS Progression 1.0 链接（`rel=http://opds-spec.org/progression`，`application/opds-progression+json`），服务路径为 `/opds/progression/{gid}/{token}`。GET 返回当前进度或空响应；PUT 提交带 `modified`、`device`、`progression` 的 JSON，可选 `title` 和 URI `references`。新记录返回 201，更新或相同重试返回 200，过期或同时间冲突返回 409 Problem Details。Progression 使用独立的 `ARCHIVE_STATE_DIR/opds-progression.db` bbolt 文件，最多保留 10,000 个未归档作品；归档作品不计入该上限。
- `/stream/{gid}/{token}/page/{page}` 是两种目录共用的逐页图像代理。缩略图代理提供封面、内页预览及评论图片转发。
- WebUI 与管理 API 另属 `/` 和 `/api/*` 命名空间；完整接口列表见 OpenAPI。

## PSE 阅读

OPDS-PSE stream link 使用 PSE stream 关系、`image/jpeg` 类型、页数以及含 `{pageNumber}` 的模板地址。默认页码从 1 开始；`PSE_PAGE_BASE=0` 可切换为从 0 开始。每页按原图流式代理，服务端不进行双页拆分。

v1.2 根导航固定由服务端提供，不读取 `HOME_CONFIG`。v2.0 根导航可由 `HOME_CONFIG` 指向的 TOML 调整分组与区块；文件缺失时用内置默认布局，配置解析失败时回退默认值。Watched/Favorites 入口依赖成对配置的登录 cookie。两版的 Archives shelf 均只列出本地 ready 归档，按页浏览，不请求上游；没有 ready 条目时根导航中的入口隐藏。归档 shelf 的列表元数据来自本地归档信息/快照，阅读仍可使用本地 zip。

Favorites 列表在 OPDS 1.2 与 2.0 中提供 Favorite category facets：All 不带 `favcat`，`favcat=0`–`9` 对应账户自定义收藏夹，显示名称取自上游收藏夹目录。v2.0 也继续接受旧式 `query=favorites:N`，新 facet 链接使用 `query=favorites&favcat=N`。

## 搜索、分类与翻译

两版均提供搜索入口和分页；gallery 使用不透明的上游 `next` 游标，Toplist 使用 `page`。v2.0 的 category facets 由 `FACETS` 生成，搜索文本中的受支持分类表达式也会转换为分类过滤。高级搜索支持 `rating:1`–`rating:5`、`expunged`、`nohide:uploader|language|tags|all`，命中时自动开启 EH advanced search；双引号内的文本不解析为扩展词。非法扩展值静默移除，其他 EH 搜索语法照常透传。

启用 `TAG_TRANSLATION_ENABLED` 后，v2.0 subject 可输出中文标签，搜索可将词典命中的中文标签还原为 EH 标签语法；关闭时保留原始行为。`reclass:` 不作为分类别名。精确字段、链接结构、评论扩展和 Atom/JSON 示例见[客户端元数据格式](client-metadata.md)。

## 逆向兼容与实现依据

E-Hentai 页面结构没有稳定公开的机器接口契约；解析逻辑基于真实页面结构并由 fixture 测试保护。遇到上游变化时，以 Go 解析器、测试和 OpenAPI 当前契约为准；不要根据本概览推断未列出的 schema。

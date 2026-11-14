# Breaking Changes · OPDS 2.0 Divina 迁移

这次变更影响从 OPDS 2.0 目录打开作品、加载详情和逐页阅读的客户端。

## 链接迁移

| 操作 | 旧客户端用法 | 新客户端用法 |
| --- | --- | --- |
| 打开单本详情 | 跟随 `self`，按 `application/opds+json` 解析单本 `/publication`。部分客户端跟随 `detail` 模式的 `acquisition`，再从 `publications[0]` 取详情。 | 在作品的 `links` 中找到 `rel: self` 和 `type: application/divina+json`，GET 其 `href`，将响应顶层解析为单本清单。 |
| 逐页阅读 | 从目录条目读取 PSE `stream` 或 `acquisition` 的 `{pageNumber}` 模板。 | 使用清单中的 `readingOrder`，按数组顺序请求各页的 `href`。跳页时按数组索引定位。 |
| 打开原站 | 跟随 `alternate`。 | 继续跟随 `alternate` 的 `href`。 |
| 同步进度 | 从作品链接中发现进度端点。 | 继续使用 `rel: http://opds-spec.org/progression`、`type: application/opds-progression+json` 和该链接的 `href`。继续使用原有 GET/PUT 协议。 |

新 `self` 当前指向 `/opds/v2.0/gallery/{gid}/{token}/manifest`。清单使用 `@context`、`metadata.conformsTo` 和 `readingOrder`，响应的 `Content-Type` 为 `application/divina+json`。

客户端从每个链接读取实际 `href`，并按响应文档的 URL 解析相对链接。用 `metadata.identifier` 关联现有书签、缓存及进度。启用认证时，按部署的认证方式访问清单、图片和进度端点，进度链接的 `properties.authenticate` 仍可用于发现认证文档。

## 详情加载

列表只读取目录数据。用户打开详情时，先显示目录已有的标题和封面，再异步请求可识别的 `self`。用清单实际提供的字段补全或更新界面，遇到缺失字段或请求失败时保留列表数据。可以缓存清单并合并同一本书的并发请求。开始阅读时复用已加载的清单，直接使用 `readingOrder` 的 URL，避免用旧页码基准拼接图片路径。服务端暂时无法确定页数时，清单端点返回 `503`，客户端可保留已显示的条目，让用户稍后重试。

## 配置及兼容性

- `OPDS_ACQ_DETAIL` 与旧式 `OPDS_ACQ_MODE=detail` 对 OPDS 2.0 目录链接不再生效。目录里的 `acquisition` 和 PSE `stream` 已移除。旧 TOML 中的 `OPDS_ACQ_DETAIL` 会被忽略，管理 API 也已移除 `opds_acq_detail` 字段。
- 已保存的 `/publication` 和旧采集详情 URL 仍可访问，媒体类型沿用旧值，其中作品的链接也更新为 `self`、`alternate` 和进度。新客户端应从目录的 `self.href` 发现清单。
- 只会通过 `acquisition` 下载文件，或只会从 OPDS 2.0 条目读取 PSE 的客户端，需要适配 Divina 才能从新目录阅读。OPDS 2.0 的 OPDS Publication 模型要求至少一个 acquisition 链接，严格按该模型校验的客户端可能忽略只有 Divina `self` 的目录条目。支持 Atom PSE 的客户端可以继续使用 `/opds/v1.2`。
- OPDS 2.0 目录没有 CBZ 获取链接。访问清单和逐页阅读无需触发归档。

## 客户端验收

1. 浏览目录时确认客户端不会逐条请求清单。打开未归档作品，确认详情补全、首图、翻页和末页都能正常工作。
2. 在启用认证和反向代理的部署上，确认清单、图片和进度 URL 都可访问。
3. 在一个设备写入进度，再用另一设备从同一作品续读，确认进度与页序都正确。

详细响应结构见 [客户端元数据格式](client-metadata.md)和 [OpenAPI](../api/openapi.json)。

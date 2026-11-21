# 文档索引

- [部署配置与反向代理](configuration.md)：Docker、环境变量、持久化目录和外部 URL。
- [E-Hentai 集成](eh-integration.md)：会话、页面解析、限流、失败处理与缓存。
- [OPDS 与 OPDS-PSE](opds.md)：目录形态、分页、首页、搜索和流式阅读概念。
- [归档](archive.md)：用户发起的归档、SQLite 元数据、ZIP 文件与迁移边界。
- [订阅与下载任务](subscriptions.md)：收藏同步、关键词缓存、自动下载与任务安全语义。
- [收藏夹](favorites.md)：上游收藏写操作和兼容入口。
- [客户端元数据](client-metadata.md)：面向阅读器开发者的响应结构与消费指南。
- [Breaking Changes](breaking-changes.md)：OPDS 2.0 Divina 变更与客户端迁移验收。
- [Go 迁移验收](go-acceptance.md)：Go 与既有实现的迁移验收步骤。
- [历史实施计划](plans/PLAN-2026-08-11-archived.md)：已归档的早期计划，仅作历史记录。
- [SQLite 与订阅／归档解耦方案](plans/PLAN-2026-11-19-sqlite-subscriptions-archives.md)：本次重构的设计记录。
- [OPDS 归档关联身份与操作流程](plans/PLAN-2026-11-21-opds-archive-association-identity.md)：关联身份切换、详情页解除/删除归档和画廊载荷路由计划。

精确 HTTP 路径、参数、请求体与响应 schema 以 [`api/openapi.json`](../api/openapi.json) 为准；运行时亦提供 `/docs` 与 `/openapi.json`。

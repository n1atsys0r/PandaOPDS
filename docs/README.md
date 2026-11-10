# 文档索引

- [部署配置与反向代理](configuration.md)：Docker、环境变量、持久化目录和外部 URL。
- [E-Hentai 集成](eh-integration.md)：会话、页面解析、限流、失败处理与缓存。
- [OPDS 与 OPDS-PSE](opds.md)：目录形态、分页、首页、搜索和流式阅读概念。
- [归档](archive.md)：用户发起的归档、持久存储和离线访问。
- [收藏夹](favorites.md)：收藏写操作、同步和自动归档语义。
- [客户端元数据](client-metadata.md)：面向阅读器开发者的响应结构与消费指南。
- [Go 迁移验收](go-acceptance.md)：Go 与既有实现的迁移验收步骤。
- [历史实施计划](plans/PLAN-2026-08-11-archived.md)：已归档的早期计划，仅作历史记录。

精确 HTTP 路径、参数、请求体与响应 schema 以 [`api/openapi.json`](../api/openapi.json) 为准；运行时亦提供 `/docs` 与 `/openapi.json`。

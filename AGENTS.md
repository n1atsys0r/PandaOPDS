# PandaOPDS Agent 指南

## 项目事实

- PandaOPDS 是 E-Hentai 中转代理，输出 OPDS 1.2、OPDS 2.0 和 OPDS-PSE 图片流。
- 当前主服务为 Go：入口 `cmd/pandaopds/`，实现位于 `internal/`。`app/` 中的 Python/FastAPI 服务仅作行为对照；新功能以 Go 实现和 OpenAPI 为准。
- Docker 是主要部署方式；反向代理部署需正确配置外部 URL。
- 不托管图库内容。归档与自动归档可能消耗用户账户 GP，所有相关行为须保持明确、显式且默认安全。

## 协作与修改约定

- 先读相关代码、测试和契约，确认现状再修改；遵循现有结构，避免无关重构。
- `api/openapi.json` 是 HTTP API 契约，`internal/config/settings.go` 是 Go 配置默认值与解析的权威来源。修改接口或配置时同步更新契约和用户文档。
- 不在文档中维护完整的逐端点 schema；接口明细以 OpenAPI 为准。
- 优先补充针对行为和回归风险的测试；报告实际执行的检查，不把未运行的检查说成通过。
- 保持中文项目文档简洁、可核实；历史计划保留其编写时语境，不将其改写成当前架构说明。

## 跨组件不变量

- OPDS 路径按版本隔离：`/opds/v1.2` 与 `/opds/v2.0`；OPDS-PSE 默认页码为 1-based，`PSE_PAGE_BASE=0` 可切换。
- 上游请求需经过限流、重试及熔断策略；列表、详情、图片和归档缓存各自用途不同。服务器多客户端共用出口 IP，节流与缓存命中率优先。
- 登录 cookie 是可选配置；只有成对提供 `IPB_MEMBER_ID` 和 `IPB_PASS_HASH` 才视为已配置登录态。不要记录或回显凭据。
- 归档是用户显式操作；收藏自动归档默认关闭。归档 zip 是长期本地缓存，不能与可淘汰的图片 LRU 混为一谈。
- 公共响应字段、分页游标和客户端可见行为须与 OpenAPI 及 Go 实现一致。

## 常用命令

```bash
go build ./cmd/pandaopds
go test ./...
docker compose up -d
docker compose -f docker-compose.yml -f docker-compose.build.yml up -d --build
```

## 详细资料

- [文档索引](docs/README.md)
- [配置与反向代理](docs/configuration.md)
- [E-Hentai 上游集成](docs/eh-integration.md)
- [OPDS 与 OPDS-PSE](docs/opds.md)
- [归档](docs/archive.md) · [收藏夹](docs/favorites.md)
- [客户端元数据格式](docs/client-metadata.md)
- [Go 迁移验收指南](docs/go-acceptance.md)
- OpenAPI 契约：`api/openapi.json`；运行服务后可在 `/docs` 查看 Swagger UI、在 `/openapi.json` 查看服务实际提供的文档。

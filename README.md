# PandaOPDS

PandaOPDS 是面向支持 OPDS-PSE 的阅读器的 E-Hentai 中转服务器，提供 OPDS 1.2（Atom）、OPDS 2.0（JSON）目录和逐页图片流。项目以 Go 单进程服务为主；仓库中的 Python/FastAPI 实现保留作行为对照。项目不托管图库内容。

## Docker Compose 快速开始

```bash
git clone https://github.com/n1atsys0r/PandaOPDS.git
cd PandaOPDS
cp .env.example .env   # 可选：配置 E-Hentai 登录 cookie
docker compose up -d
```

Compose 默认拉取 GHCR 发布镜像；升级使用 `docker compose pull && docker compose up -d`。公开目录可不配置登录 cookie；Watched、Favorites、归档和收藏写操作需要成对的 `IPB_MEMBER_ID` 与 `IPB_PASS_HASH`。镜像缓存与归档数据保存在 Compose 卷中。

本机访问 `http://127.0.0.1:8000/` 可查看仪表盘和归档，并在启用 Basic Auth 后编辑服务设置与 OPDS 首页布局。`/docs` 是 Swagger UI，`/openapi.json` 是运行时 OpenAPI 文档。

## 从源码构建

从仓库根目录构建需要 Go 和 Node.js 24。

```sh
make webui
go build ./cmd/pandaopds
```

Go 二进制内嵌前端资源。使用 Docker 构建时，镜像构建过程会自动打包前端。

## 安全与部署

- 对外提供服务时建议放在 HTTPS 反向代理后，并设置 `PUBLIC_BASE_URL` 为外部 HTTPS 地址；示例见 [`deploy/`](deploy/)。
- Basic Auth 由 `AUTH_USERNAME` 与 `AUTH_PASSWORD` 同时设置启用。凭据只经 HTTPS 传输；默认仅将服务绑定到本机回环地址。
- 服务集中使用单一出口 IP 访问上游。请保守设置并发、保留缓存，避免触发 E-Hentai 限流或封禁。
- 归档操作可能消耗 E-Hentai GP；仅由用户显式发起。收藏自动归档默认关闭。

## 文档

- [文档索引](docs/README.md)
- [配置与反向代理](docs/configuration.md)
- [E-Hentai 上游集成](docs/eh-integration.md)
- [OPDS 与 OPDS-PSE](docs/opds.md)
- [归档行为](docs/archive.md)
- [收藏夹行为](docs/favorites.md)
- [客户端元数据格式](docs/client-metadata.md)
- [Go 迁移验收指南](docs/go-acceptance.md)

## 许可证与致谢

项目采用 Apache License 2.0（见 `LICENSE`）。上游抓取与解析参考 JHenTai，归属声明见 `THIRD_PARTY_LICENSES`。可选中文标签词典来自 EhTagTranslation/Database，采用 CC BY-NC-SA 3.0 中国大陆许可；详情亦见 `THIRD_PARTY_LICENSES`。

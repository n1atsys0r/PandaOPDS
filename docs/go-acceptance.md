# Go 版并行验收

Python 对照提交为 `4d71eeca27f12ca02b1a2c6610b4da35f171a9d8`。以下步骤在现有服务器上用单独的容器和数据副本运行。当前生产容器保持原样。

## 离线检查

在重构分支根目录运行。Python 环境沿用项目现有的测试依赖。

```sh
make webui
go test ./...
go test -race ./...
go build -o /tmp/pandaopds-go ./cmd/pandaopds
PYTHONPATH=. RUN_EH_INTEGRATION=0 .venv/bin/python tests/compare_go.py --binary /tmp/pandaopds-go
```

离线对照会由 Python 创建带中断升级记录的归档副本，对比两版的本地阅读和管理响应，并让 Python 重新打开 Go 恢复后的状态。单独运行 `tests/compare_go.py` 可以检查 Python 对照数据。

Go 版首页链接将 `PUBLIC_BASE_URL` 只拼接一次。对照脚本按这个预期验证，避免保留 Python 基线中首页导航的重复前缀。

## 在服务器上另起容器

1. 从重构分支构建镜像。镜像标签取该版本的提交短 SHA。

   ```sh
   tag="pandaopds:go-$(git rev-parse --short HEAD)"
   docker build --tag "$tag" .
   ```

2. 从一致性备份复制生产归档、缓存和配置到独立的验收目录。确保备份取自归档任务空闲时。将用于验收的环境变量放在该目录下权限受限的 `acceptance.env`。保留原有认证及节流配置，将 `PUBLIC_BASE_URL` 设为验收入口。需要通过阅读器访问时，为这个 loopback 端口配置单独的反代入口。

   ```sh
   acceptance_dir=/path/to/pandaopds-go-acceptance
   docker run -d --name pandaopds-go-acceptance \
     --publish 127.0.0.1:18080:8000 \
     --env-file "$acceptance_dir/acceptance.env" \
     --env PORT=8000 \
     --env CACHE_DIR=/data/cache \
     --env ARCHIVE_DIR=/data/archives \
     --env HOME_CONFIG=/config/home.toml \
     --env FAVORITES_SYNC_ARCHIVE=0 \
     --env FAVORITES_SYNC_INTERVAL_SECONDS=0 \
     --mount "type=bind,source=$acceptance_dir/cache,target=/data/cache" \
     --mount "type=bind,source=$acceptance_dir/archives,target=/data/archives" \
     --mount "type=bind,source=$acceptance_dir/config,target=/config" \
     "$tag"
   curl --fail --silent http://127.0.0.1:18080/health
   ```

   如果使用显式的收藏、标签词典或 My Tags 快照路径，将其设到 Go 容器的验收副本中。Python 对照使用另一份同源副本。需要对照冷启动时，将当前生产镜像或基线提交另起一个对照容器，同样关闭周期同步和自动归档。

## 集中验收

- 在日常阅读器验证 OPDS 1.2 和 2.0 的首页、搜索、分页、详情、封面和前后翻页。检查反代生成的链接指向验收入口，确认已有 `gid/token` 阅读链接可用。使用归档目录确认本地覆盖图和母本页可读。
- 检查 Basic Auth、现有豁免规则、管理台状态与配置脱敏、首页布局和归档列表。在桌面和窄屏上切换深浅主题，检查键盘操作和刷新后的主题记忆。用两页同时编辑验证保存冲突，并验证服务设置重启生效、首页布局在下一次请求生效。访问 `/docs`，确认规范和静态资源都从验收入口提供。试调 `/health` 和 `/api/status`，确认写接口有示例且文档页面仅展示契约。
- 交替访问 Python 和 Go 的真实上游入口。仅在指定图库上做可恢复的收藏操作，记录并恢复原分类及备注。仅在报价显示免费或已解锁时提交该档位的归档。评分、GP 消耗和故障场景使用离线测试。
- 同机、同镜像架构、同配置和同一份数据起点下，对两版分别记录初始化后空闲、同一轮串流时的峰值和串流后重新空闲的 `docker stats` 内存。记录镜像标签、状态量和采样时间。内存结果用于比较收益。

  ```sh
  docker stats --no-stream --format '{{.Name}} {{.MemUsage}}' pandaopds-go-acceptance
  ```

## 退出与回滚

并行验收期间只需移除验收反代入口并停止验收容器，验收数据副本保留以便复盘。

```sh
docker stop --time 30 pandaopds-go-acceptance
```

如果日后切换生产，切换前保留 Python 镜像标签与数据备份。回滚时先停止 Go 实例，再用原 Python 镜像和配置重启。需要还原磁盘状态时，从切换前的备份恢复后再启动。两个实例应分别使用自己的可写数据目录。

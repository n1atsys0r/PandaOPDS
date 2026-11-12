# 归档

归档实现位于 `internal/archive/`，通过管理 API 显式操作。接口参数与状态 schema 见 [`api/openapi.json`](../api/openapi.json)。它需要成对配置 IPB 登录 cookie，账户还须具备使用官方 archiver 的资格；未解锁档位可能消耗 GP。

## 明确报价后再购买

正常操作顺序是 **quote → 用户查看价格/大小/档位并决定 → start → 准备 → 下载**。quote 只读取报价，不扣 GP、不创建任务。start 选择上游提供的档位并 POST 请求准备归档，然后轮询准备状态，直到取得最终下载 URL；下载支持 Range 续传。完成后统一保存为 zip。应用不会在浏览或普通收藏操作中隐式购买归档。

下载、转换与校验在后台任务中进行。续传文件 `archive.part` 和 7z 转换临时文件位于现有 `CACHE_DIR/archive-staging/{gid}/{token}/`；这里是专用子目录，不受图片缓存的容量淘汰、TTL 或清空操作影响。归档下载先在该目录完成并校验，再复制到 `ARCHIVE_DIR` 同目录临时文件并重命名为最终 zip，因此 ready 表示已在 `ARCHIVE_DIR` 发布，可供流式阅读，不代表上传到远程存储。普通 start 对 ready/进行中条目保持幂等；强制重下需显式 force。失败任务会记录错误并可再次手动启动。

## 长期存储与离线阅读

Web UI 的“导入本地归档”按钮会异步发现 `ARCHIVE_DIR` 中带有有效状态和 token 的旧归档，并显示新增、跳过、冲突或错误结果。扫描结果会直接加入当前服务的归档列表，无需重启；没有状态身份的 ZIP 不会被推断或导入，也不会触发购买或删除。

新归档按实际版本的 gid 将 ZIP 放在 `ARCHIVE_DIR/{gid}.zip`，发布临时文件为同目录的 `{gid}.zip.publish.tmp`；状态和图库 sidecar 保存在 `ARCHIVE_STATE_DIR`（默认 `./config/archive`，容器内 `/config/archive`），以便分别挂载。`state.json` 记录本地归档状态，原始版本的状态还记录书架当前指向的版本。`gallery.json` 保存上游图库资料快照，封面保存在 `cover.jpg`。已有归档继续使用原 ZIP 相对布局，包括 `meta.json`、`metadata.json`、`{gid}/{token}/` 布局及按原始 gid 分组的版本 ZIP。首次升级到拆分存储时，Compose 部署按以下顺序迁移：

```sh
docker compose stop pandaopds
docker compose run --rm pandaopds archive import
docker compose up -d
```

命令会复制缺失的状态、图库资料和封面，并将有本地状态记录且通过 ZIP 目录校验的旧版嵌套 payload 移至 `ARCHIVE_DIR/{actual-gid}.zip`。此校验读取 ZIP 中央目录并使用文件大小、条目名、CRC 字段和压缩属性生成重试指纹，不会读取整份 ZIP 内容。源 sidecar 保留在原处；已有的 flat ZIP 如果没有对应的迁移日志所有权，会按冲突处理且不会覆盖。每个书架的迁移意图先写入本地状态卷并 fsync，再在同一 `ARCHIVE_DIR` 卷中重命名 payload，最后更新本地 `payload_layout`、ZIP 大小和页数并清除日志；重跑会恢复中断在重命名与状态写入之间的操作。内容相同的 sidecar 计为 skipped，不同 sidecar 或 flat ZIP 冲突计为 conflict。从另一新实例迁移 sidecar 时，可追加其 state 目录：`docker compose run --rm pandaopds archive import /path/to/peer/archive-state`。迁移需先停止服务，并确保没有其他进程同时写 `ARCHIVE_DIR`；目标存储必须支持同卷重命名。云端 FUSE 若不支持该操作，命令会失败并保留本地迁移日志供重试，不能在服务运行时强行迁移。本地 `index.snapshot` 丢失、损坏或存在 `.dirty` 标记时，服务从 `ARCHIVE_STATE_DIR` 的本地状态文件重建索引，不扫描 ZIP 卷。归档是长期存储，不受图片 LRU 的大小或时效淘汰。删除整个条目会清除本地归档，站点账户中的归档记录保留。请为 `CACHE_DIR` 配置持久本地卷以保留跨重启续传，为 `ARCHIVE_DIR` 使用支持同目录重命名的持久存储。发布及转换期间需要预留临时文件空间。

ready zip 优先为 `/stream` 提供图片页，封面优先使用本地快照；归档 shelf（`/opds/v1.2/archives` 与 `/opds/v2.0/archives`）仅从本地 ready 索引生成，不依赖上游。收藏同步建立的 `unarchived` 记录与归档失败（`failed`）条目仅在 `ARCHIVE_STATE_DIR` 保存元数据与封面，仍可由常规 OPDS 详情与 `/stream` 在线/缓存路径阅读。图库详情优先尝试在线 HTML，失败时可回退本地图库快照；图片页数以 zip 实际条目数为准。元数据刷新可重新获取 gdata 元数据与封面，不会重新购买归档。

## 图库版本更新

后台按批次查询已归档的当前版本，每批最多 25 个图库。检测到新版后，归档管理页可报价并更新，提交时确认目标版本及最高 GP 花费。网页配置编辑器中的检查周期默认 7 天，设为 0 可关闭周期检查；自动更新默认关闭。启用自动更新后会按现有档位自动归档新版，可能消耗 GP。两项设置仅写入 `settings.toml`，并在重启后生效。

新版 ZIP 校验并发布后，归档书架保留原条目标识，旧 OPDS 详情入口返回新版。切换成功后自动删除旧 ZIP，保留旧版的元数据和封面。已发出的旧图片流链接仍请求旧版，可通过缓存或上游读取。刷新详情后可获取新版图片流地址。

更新中断时，已保存的上游状态 URL 可用于继续下载。若提交结果不明且没有可用的状态 URL，需要先在上游核实，后台会保留原版并暂停该次更新。

## 升级恢复与资源限制

跨档升级时保留旧 ready 状态与 zip，直到新档下载并校验通过再替换。升级失败或重启中断时，管理器尝试恢复旧 ready 档；普通未升级任务的中断则标记为可重试失败。下载、转换与发布共用 `ARCHIVE_DOWNLOAD_CONCURRENCY` 并发上限，与阅读请求池分离，但仍检查全局熔断状态。监控存储空间，为 `CACHE_DIR` 配置持久本地卷，并为 `ARCHIVE_DIR` 配置持久存储。

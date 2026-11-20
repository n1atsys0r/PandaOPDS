# 收藏夹

收藏读取和写入需要成对配置 IPB 登录 cookie。添加、移动和移除操作由 `/api/favorites` 代理到上游；成功后同步更新内置「EH 收藏」订阅的本地成员关系。若上游写入成功但本地 SQLite 更新失败，响应会附带 `local_warnings`，下一轮订阅抓取可重新校准本地缓存。

收藏夹的定期扫描、增量状态和缓存条目现在由 SQLite 订阅管理。旧 `/api/favorites/sync` 与 `/api/favorites/sync/run` 路径保留兼容，新的订阅接口和行为见[订阅与下载任务](subscriptions.md)。旧 favorites JSON 只在迁移期间作为导入来源，不再是运行期权威数据。

第一次同步建立基线，不会自动下载已有收藏。收藏来源范围由订阅的 `query.favorite_categories` 控制；自动下载的 `download_rules.categories` 与 `excluded_categories` 独立限制哪些新收藏允许排队。自动下载默认关闭，开启后可能产生账户费用；详见[归档](archive.md)中的购买安全规则。

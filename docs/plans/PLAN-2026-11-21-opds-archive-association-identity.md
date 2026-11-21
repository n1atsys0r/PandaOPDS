# OPDS 归档关联身份与操作流程实施计划

日期：2026-11-21。状态：实施中（历史重复关联在启动迁移中保留一个可信归档候选，其余解除关联；无可信候选时全部解除，文件均保留）。

## 1. 目标与已确认决定

本计划收敛本次讨论中的本地归档与 EH 画廊关系：

- 本地文件仍以稳定的 `document_id` 和归档相对路径管理；关联或解除关联不更换该内部 ID。
- 文档有有效 EH 关联时，对外 OPDS publication 使用 `urn:ehentai:gallery:{gid}:{token}`；解除关联后恢复 `urn:pandaopds:document:{document_id}`。EH 身份只在关联存在期间生效，不保留永久别名。
- 本地文档的显式关联同时表示它可作为对应画廊的本地归档载荷。暂不增加全局缓存开关。
- 解除关联只移除文档与 EH publication 的关系，保留本地文件供归档管理器阅读或重新关联。删除归档才删除本地文件。
- 关联、解除关联和删除都不触发购买。重新下载仍须经过报价、用户确认和现有下载任务流程。

本地文档 ID 路由继续提供直接本地阅读；画廊 GID/token 路由在有有效关联时使用关联文件，解除关联后按现有画廊来源规则读取。这样同一文件仍可由本地文档 ID 管理，而归档 OPDS 项目在关联期间与 EH 画廊共享一个公开身份。

## 2. 当前实现基线

- `BuildDocumentPublication` 和 OPDS 1.2 归档 Atom feed 当前总是输出 PandaOPDS 文档身份；关联时只附加 EH manifest 为 `alternate`。
- `/opds/v2.0/archives` 与 `/opds/v1.2/archives` 从本地 `documents` 表构建；本地 manifest 和图片流使用 `document_id` 路由。
- `/api/documents/{id}/associate` 已支持 `{ "gid": 0, "token": "" }` 解除关联；归档管理器也已有手动编辑关联的界面。需把解除语义写入 OpenAPI 契约。
- `/api/archive` 目前把文档状态 `available` 直接放进 archive `status`；前端详情页只把 EH 状态 `ready` 视为已归档。`quoteArchiveFor` 对非 `unarchived` 项会走 refresh，下载时设置 `force: true`。因此状态对齐必须先于详情页操作改造，避免已关联的本地文件触发强制下载。
- 画廊 manifest / 图片流目前使用 EH 详情与图片服务；图片服务先查受管 EH ZIP，再查图片缓存，之后请求上游，尚未把手动关联的 `documents` 文件接入该路径。
- 现有 `DELETE /api/archive/{gid}/{token}` 会删除精确 GID/token 下匹配的本地文档，不删除上游画廊记录，并经文件删除钩子写入下载抑制。删除前端需明确展示其作用范围。

## 3. 目标契约

### 3.1 身份与链接

身份由当前关联关系派生，不新增或持久化“规范 ID”字段：

| 状态 | OPDS identifier / Atom entry ID | OPDS 2.0 归档条目 `self` | 本地直接阅读 |
|---|---|---|---|
| 已关联且文件可用 | `urn:ehentai:gallery:{gid}:{token}` | `/opds/v2.0/gallery/{gid}/{token}/manifest` | 保留 `/opds/v2.0/documents/{id}/manifest` 作为本地入口/备用链接 |
| 未关联且文件可用 | `urn:pandaopds:document:{id}` | `/opds/v2.0/documents/{id}/manifest` | 同左 |

OPDS 1.2 归档 feed 的 entry ID 使用同一身份规则；其本地 acquisition 和 PSE 链接须按关联状态走对应画廊或文档路由。普通 EH 画廊目录继续使用 EH 身份，不受本地文件解除关联影响。

`/api/documents` 和 `/api/archive` 管理响应中的 `document_id` 始终是内部文档 ID。已保存的文档 ID manifest URL 继续有效，并在下一次读取时返回当前关联状态下的 identifier。已关联画廊条目的新 `self` 使用稳定的 GID/token manifest；解除关联后，客户端刷新本地归档 feed 会看到恢复后的 PandaOPDS 身份。

### 3.2 画廊使用关联文件

关联文件用于画廊 manifest、PSE 图片流及封面时，须统一按相同的 GID/token 解析，不能只改 `self` 或 `metadata.identifier`：

- 对可用关联文档建立或读取安全页索引；manifest 的页数与 readingOrder 以实际选中的归档文件为准。
- 图片页和封面从该文档读取；对应本地文档 ID 路由保持独立可用。
- 解除关联后，GID/token 路由不再读取该文档，回到受管 EH 归档、现有图片缓存和上游读取路径。
- 关联文件缺失、失效或读取失败时，响应和 UI 必须明确反映本地归档不可用；不得继续展示为可用本地归档，也不得静默把无效关联伪装成成功缓存。

数据库允许多个文档关联同一 publication，但 GID/token 画廊路由和 OPDS 归档 feed 需要唯一载荷及唯一 EH 标识。实施前须确定多文件策略。建议首期对同一精确 GID/token 只允许一个可用关联文档；已有重复关联不自动删除或合并，在管理器中提示用户处理。若保留多文件关系，则必须另行规定主载荷选择、去重 feed 表达以及详情页解除/删除的作用范围。

## 4. 详情页归档按钮状态与操作

前端依据服务端归一化后的画廊归档状态展示按钮，不再直接把本地文件状态 `available` 当成 EH 任务状态。

| 画廊状态 | 点击行为 |
|---|---|
| 有可用关联文档 / ready | 展示「解除关联」与「删除归档」两个明确选项 |
| 无本地归档 / unarchived | 沿用 quote → 显示档位、大小和 GP → 用户确认 → start |
| pending / submitting / downloading / zipping | 显示进行中并禁用重复提交 |
| failed | 沿用明确报价与重试流程，不以 force 作为默认动作 |
| 关联文件 missing / invalid | 显示不可用状态；提供管理器修复入口以及明确的解除/删除操作 |

操作语义：

1. **解除关联**：对当前精确 GID/token 解除文档关系；文件保留，文档在 `/api/documents` 与本地归档 OPDS 中恢复 PandaOPDS 身份。若支持一个 publication 下多个文件，界面须明确是逐文件操作还是对全部文件操作。
2. **删除归档**：删除本地归档文件，不删除 EH publication、收藏、订阅成员或 EH 进度记录；明确展示文件名/数量并二次确认。保留现有下载抑制，避免自动任务立即重下。
3. **重新下载**：解除关联后，用户可从画廊发起正常报价和下载，成功发布后由可信下载任务重新关联新归档。可信发布必须能显式覆盖“用户刚解除关联”的保护标记；普通扫描或不相关任务不得自行恢复该关联。若新文件与仍保留的本地文件路径冲突，须有确定的替换/冲突处理，不得静默覆盖用户文件。
4. **手动重新关联**：归档管理器继续允许提交 GID/token；成功后 OPDS 身份和画廊载荷路由立即切换到 EH 身份。

详情页级解除关联建议使用 gallery-scoped 原子操作，以避免一次解除多个文档时部分成功；可新增 `DELETE /api/archive/{gid}/{token}/association`，或在确认一对一约束后复用单文档关联接口。删除仍使用现有 archive delete 语义。

## 5. 实施阶段

| 阶段 | 工作内容 | 完成条件 |
|---|---|---|
| 1. 状态与关联边界 | 定义 `/api/archive` 的画廊级状态、来源和关联文档 ID；确定多文档关联策略；确认手动解除后的可信重下载行为 | `available`、`ready`、`unarchived` 不再混用；不能把已关联文件误判为未归档或强制刷新 |
| 2. OPDS 身份和归档 feed | 为 v2.0 publication、文档 manifest 和 v1.2 Atom entry 增加统一身份派生；按关联状态输出 self/acquisition/stream 链接 | 关联、解除关联后重新请求 feed/manifest 可观察到规定身份和链接；document ID 不变 |
| 3. 画廊载荷解析 | 在 gallery manifest、图片流和封面路径接入关联文档；解除关联后停止访问该文档；保留受管 ZIP 与上游 fallback | 关联时阅读本地归档；解除后画廊走 EH 路径；本地 document ID 阅读始终可用 |
| 4. 详情页操作 | 依据统一状态展示归档按钮；增加解除关联/删除选项、确认文案、完成后的状态刷新；保留 quote/GP 确认 | 已关联点击不再进入 refresh/force；解除保留文件，删除明确移除文件；重新下载必须显式确认 |
| 5. 契约与文档 | 更新 `api/openapi.json`、`docs/archive.md`、`docs/client-metadata.md` 及相关归档计划说明 | 对外 API、身份规则、OPDS 链接和删除范围一致；`docs/README.md` 收录本计划 |

## 6. 验收点

- 已关联本地文档在 OPDS 2.0、OPDS 1.2 和本地 manifest 响应中呈现 EH identifier；解除关联后呈现 PandaOPDS document identifier；内部 `document_id` 始终稳定。
- 关联条目的 v2.0 self 指向 gallery manifest，画廊阅读实际读取对应文档；文档本地 manifest/stream 仍可直接阅读。
- 解除关联后原本地文件仍在管理器中，可阅读、重新关联；旧 gallery manifest URL 不再把该文件当缓存，并能按既有规则读取 EH 画廊。
- 详情页已关联状态显示「解除关联/删除归档」，不再把 `available` 传入报价状态机造成 `force: true`；只有明确选择重新下载并确认报价后才创建下载任务。
- 删除操作只影响本地归档载荷，保留 EH catalog/favorites/subscriptions/progression，并尊重下载抑制；重新下载成功后可信发布流程可重新建立关联。
- 已保存的 `/opds/v2.0/documents/{id}/manifest`、`/opds/v2.0/gallery/{gid}/{token}/manifest` 和旧 PSE 链接继续返回兼容结果；变更关联时 OPDS archive feed 不被 300 秒缓存长期遮蔽。
- 对重复关联、关联文件缺失/损坏、正在下载时取消、GP 报价过期、删除失败和重下载路径冲突分别验证，不发生静默覆盖、误删或付费重试。

以上验收项是后续实施要求；本次只建立计划，不修改行为，也未运行测试。

## 7. 实施确认（2026-11-21）

以下是实施阶段收敛的决定，保留前文作为计划编写时的历史语境：

- 同一精确 GID/token 最多一个关联文档；含 missing/invalid 在内的文档都占用关联。手动关联冲突返回 409。
- 历史重复关联迁移优先保留匹配 ready `archive_versions.archive_path` 的可信归档下载；否则保留更新时间最新的可信下载（ID 作稳定并列次序），其余解除关联。没有可信下载时全部解除关联。迁移不删除文件或进度。
- 可信下载完成可覆盖手动 unlink 标记。若另一文档占用 gallery association，下载 job 仍成功，新文档保持 unlinked。保留原有受信任同路径发布规则；本次不分配新发布路径。
- 解除关联使用 gallery-scoped 原子操作，文件保留；gallery 删除只移除本地载荷/记录，保留 EH catalog、收藏、订阅成员和进度。新购买仍须 quote 与用户确认。
- 手动解除或改绑后，画廊路由跳过原文档路径对应的 managed ZIP；缓存及上游仍可作为 fallback。关联文档不可用时 gallery reading 同样回退 managed ZIP/缓存/上游。

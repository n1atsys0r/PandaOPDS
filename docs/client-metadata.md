# PandaOPDS 客户端元数据手册

面向阅读器开发者，说明 PandaOPDS 输出的 OPDS 1.2（Atom）与 OPDS 2.0（JSON）文档结构、字段、取值与渲染规则。通用客户端消费标准层即可；`metadata` 内 `x:*` 前缀扩展字段由需要扩展信息的客户端消费。示例和客户端消费建议不替代运行时契约；HTTP 路径及 OpenAPI schema 请以 [`api/openapi.json`](../api/openapi.json) 为准。

---

## 1. 概览

| 项 | 值 |
|---|---|
| 版本路径 | `/opds/v1.2`（Atom）、`/opds/v2.0`（JSON） |
| 媒体类型 | v1.2 导航 `application/atom+xml;profile=opds-catalog;kind=navigation`；采集 `…;kind=acquisition` |
| | v2.0 导航 `application/opds+json;profile=navigation`；采集 `application/opds+json;profile=acquisition` |
| href | 默认**相对路径**；设置 `PUBLIC_BASE_URL` 时输出绝对 URL。**Stump 必须设置**（它用自身服务器地址解析相对链接，相对路径会 404）；其他客户端不受影响 |
| 页码 | PSE stream 默认 **1-based**（第 1 页 = `page/1`）；`PSE_PAGE_BASE=0` 可切 0-based（带外约定，不在链路中传输） |
| v2.0 作品详情 | 列表中的 `self` 指向 `/opds/v2.0/gallery/{gid}/{token}/manifest`（Divina）；作品链接不暴露图片流模板或归档下载。 |
| 缓存 | 目录文档通常使用短期 HTTP 缓存；缓存头按文档类型与运行状态可能不同 |

**登录态**：`IPB_MEMBER_ID`/`IPB_PASS_HASH` 可选。未提供时 Watched/Favorites 导航项**不输出**；提供时输出（不做探测验证，cookie 失效时对应 feed 返回 503）。

---

## 2. 主页文档（`GET /opds/v2.0`）

`HOME_CONFIG` 指向首页布局文件，默认使用 `config/home.toml`。每个 `[[section]]` 通过 `kind` 和布尔值 `group` 决定输出位置。配置示例见 [home.toml.example](../config/home.toml.example)。

| `kind` | `group = false`（默认） | `group = true` |
|---|---|---|
| `publication` | 顶层 `publications[]`，最多一个来源 | 独立的 `groups[]` 项，内含一个 `publications[]` 集合 |
| `navigation` | 顶层 `navigation[]` 中的一个链接 | 独立的 `groups[]` 项，内含一个 `navigation[]` 集合 |
| `facets` | 顶层 `facets[]` 中的一套筛选 | 无效配置 |

### 2.1 栏目与分组

`type = "preset"` 使用内置来源，`type = "search"` 使用 `query` 指定的搜索表达式。顶层 `publications[]` 与 `groups[]` 互斥：配置任一 `group = false` 的 publication 后，不得再有任何 `group = true` 栏目；反之亦然。顶层 navigation 可与两者任一并存。相同输出集合内按配置顺序排列。旧配置中的命名分组在读取时拆成独立栏目，通过管理页保存时写成布尔格式并备份原文件。若旧文件完全没有分组字段，请为希望保留独立预览分组的出版物栏目显式设置 `group = true`。

分组的 `metadata.title` 来自栏目标题。出版物分组可通过 `links[]` 中的 `rel="self"` 打开完整列表。导航集合由带 `title` 和 `href` 的链接对象组成。出版物的 `count` 默认是 10，填 0 关闭预览。空预览和失败的预览会省略，首页没有可用内容集合时提供 Latest 导航入口。 [OPDS 2.0 §2.1、§2.5](https://specs.opds.io/opds-2.0.html#25-groups)

### 2.2 Facets

Facet 栏目的 `type` 和 `query` 必须与顶层出版物栏目一致。每个 facet 包含 `metadata.title` 和指向筛选或排序结果的 `links[]`，出版物预览可用时才输出。搜索与 `latest` 支持类别筛选，`toplist:*` 支持切换周期，`favorites` 和 `favorites:N` 支持切换收藏目录。分组中的出版物可在其完整列表页提供自己的顶层 facets。 [OPDS 2.0 §2.4](https://specs.opds.io/opds-2.0.html#24-facets)

### 2.3 内置来源

| title | type / query | href | 出现条件 |
|---|---|---|---|
| Latest | `preset` / `latest` | `/opds/v2.0/gallery` | 恒有 |
| Watched | `preset` / `watched` | `/opds/v2.0/gallery?query=watched` | 有 IPB cookie |
| Favorites | `preset` / `favorites` | `/opds/v2.0/gallery?query=favorites` | 有 IPB cookie |
| Popular | `preset` / `popular` | `/opds/v2.0/gallery?query=popular` | 恒有 |
| Toplist: Yesterday | `preset` / `toplist:yesterday` | `/opds/v2.0/toplist?period=yesterday` | 恒有 |
| Toplist: Past Month | `preset` / `toplist:month` | `/opds/v2.0/toplist?period=month` | 恒有 |
| Toplist: Past Year | `preset` / `toplist:year` | `/opds/v2.0/toplist?period=year` | 恒有 |
| Toplist: All Time | `preset` / `toplist:alltime` | `/opds/v2.0/toplist?period=alltime` | 恒有 |
| 自定义搜索 | `search` / 任意表达式 | `/opds/v2.0/gallery?query=…` | 恒有 |

---

## 3. publication（条目/Item）元数据

任意采集文档（首页 Latest、gallery feed、toplist feed、详情文档）中的单个条目。字段分两层：**标准层**（通用客户端直接消费）与**扩展层 `metadata` 内 `x:*` 前缀字段**（EH 专属扩展）。前缀由文档顶层内联 JSON-LD context 声明：`"context": ["https://readium.org/webpub-manifest/context.jsonld", {"x": "https://github.com/n1atsys0r/PandaOPDS/vocab#"}]`——通用客户端忽略未知成员，无需感知。

### 3.1 标准层

| 字段 | 类型 | 说明 | 条件 |
|---|---|---|---|
| `title` | string | 干净标题（已剥离 `[...]`/`(...)` 标记；作者见 `authors` 字段） | 恒有 |
| `identifier` | string | `urn:ehentai:gallery:{gid}:{token}`，归档条目沿用首次归档的 gid/token | 恒有 |
| `modified` | string | 上传时间 ISO8601（UTC） | 恒有 |
| `authors` | [ {`name`} ] | 作者（从标题 `[Author]` 括号解析，见 §3.6）；上传者本人见详情文档 `x:uploader` | 非空时 |
| `author` | [ {`name`} ] | RWPM 单数形式，与 `authors` 并存（Stump/Readium 解析器只认 `author`） | 非空时 |
| `language` | [string] | 语言（**BCP 47 / RFC 5646 码**，如 `zh`/`ja`/`zh-Hans`；由 EH `language:` 标签映射，未知与标记伪标签不输出） | 非空时 |
| `published` | string | = `modified`（上传时间） | 恒有 |
| `description` | string | **当前不输出**（预留字段；客户端如需描述，可自行拼接 `language`/`numberOfPages`/`authors`/`x:rating`/`x:sizeBytes`） | — |
| `subject` | [ {`name`, `x:style`?} ] | RWPM collection：标签为 `ns:key`，图库分类默认为 `category:<分类名>`；启用翻译时分类使用固定 namespace `分类`，仅分类名通过 EhTagTranslation `reclass` 查译（无译名则保留原分类名）。详情文档分类排第一；列表文档分类位于所有带 `x:style` 的条目之后、未着色标签之前；标签原有顺序不变 | 有标签或分类时 |
| `numberOfPages` | int | 页数（= `filecount`） | >0 时 |

### 3.2 扩展层 `x:*` 字段（拍平进 `metadata`，全部 EH 专属）

| 字段 | 类型 | 说明 | 条件 |
|---|---|---|---|
| `x:rating` | float | 评分（0–5，保留原精度如 4.5） | ≠0 时 |
| `x:titleJpn` | string | 日文标题 | 非空时 |
| `x:sizeBytes` | int | 文件总字节 | ≠0 时 |
| `x:expunged` | bool | 已删除标记 | 仅 `true` 时输出 |
| `x:uploader` | string | 上传者（详情页 `#gdn`） | 仅详情文档，非空时 |
| `x:note` | string（HTML） | 提示存在较新版本，并附指向最新版本图库详情的链接 | 仅详情文档；解析到较新版本信息时 |
| `x:reviews` | array | 评论区（仅详情文档；每项 `id`/`username`/`userId`(可选)/`time`/`lastEditTime`(可选)/`content`(**原始 HTML**)）；`COMMENTS_ENABLED=0` 关闭 | 有评论时 |

> **评论 `content` 的重写**（客户端无需感知，直接渲染即可）：① 图库链接 `(e-hentai|exhentai).org/(g|mpv)/{gid}/{token}/` → `/opds/v2.0/gallery/{gid}/{token}`（app 内跳转）；② eh/ex 封面/预览图（host ∈ `IMAGE_PROXY_HOSTS`，默认 `ehgt.org,s.exhentai.org`）在 `src`/`url()` 内 → 同源代理 `/image/fetch?url=<编码>`，**规避 WebView 跨域 CORS**（否则 `<img>` 原生跨域拉图被拦）。URL 不透明、不解析 gid/token。

> 原 `metadata.extensions` 嵌套桶已移除；所有 EH 专属字段直接平铺在 `metadata` 下。分类不再作为 `x:*` 字段输出，而是进入 `subject`。

### 3.3 subject 条目与高亮样式（`x:style`）

```json
{ "name": "female:netorare", "x:style": {
    "color": "#f1f1f1",
    "borderColor": "#048751",
    "background": "radial-gradient(#048751,#24A771)"
} }
```

| 成员 | 说明 | 条件 |
|---|---|---|
| `name` | 标签全名 `命名空间:标签`（下划线已还原为空格） | 恒有 |
| `x:style` | 高亮标签样式（投票高的 featured 标签）：`color`/`borderColor`/`background`，取自上游 inline style，`!important` 已剥离 | 仅高亮标签 |

- **无 `status` 字段**：标签可信度（`gt`/`gtl`/`gtw`）由服务端 `TAG_STATUS_FILTER` 全局消费后即丢弃，不传输给客户端；客户端无法感知被过滤标签的存在。
- **样式来源**：列表 feed 的 `x:style` 来自列表页解析的高亮标签（仅含带 inline style 的 featured 标签，经 status 过滤）；**详情文档的 subject 来自 My Tags 静态映射表回填**（详情页 `#taglist` 本无 inline style）——键按 name 匹配，已有内联 style 的条目不被覆盖。
- **合并规则**：客户端展开详情时**按 name 合并**——以详情 subject 为全集替换，回填列表条目带来的 `x:style`，勿整体丢弃样式。
- **全量标签**：进 `subject`（列表精简 / 详情完整，二者同经 `TAG_STATUS_FILTER`，保持子集关系）；详情文档的完整 `subject` 即为全量标签。

### 3.4 链接（`links[]`）

OPDS 2.0 每个作品 publication 恒有且仅有三个链接：

| rel | href | type | 说明 |
|---|---|---|---|
| `self` | `/opds/v2.0/gallery/{gid}/{token}/manifest` | `application/divina+json` | Divina 图片阅读清单；客户端进入详情或阅读时跟随此链接。 |
| `alternate` | 上游 E-Hentai 图库页 | `text/html` | 可分享的上游页面。 |
| `http://opds-spec.org/progression` | `/opds/progression/{activeGid}/{activeToken}` | `application/opds-progression+json` | 保留 OPDS progression 同步，包含归档版本映射后的 active identity。 |

作品 publication 不再提供 OPDS acquisition、OPDS-PSE stream 模板或 CBZ/归档下载链接。OPDS 1.2 的 Atom/PSE 行为不变。旧 `OPDS_ACQ_DETAIL` 与 `OPDS_ACQ_MODE` 配置不再控制 v2.0 行为；旧 TOML 中的 `OPDS_ACQ_DETAIL` 会被忽略，以便其余设置继续加载。

### 3.5 封面（`images[]`）

publication 保留 `/image/{activeGid}/{activeToken}/thumb` 的 `images[]` 封面。Divina manifest 也包含该封面和按页排列的 `readingOrder`。

### 3.6 列表 publication 示例

```json
{
  "context": [
    "https://readium.org/webpub-manifest/context.jsonld",
    { "x": "https://github.com/n1atsys0r/PandaOPDS/vocab#" }
  ],
  "metadata": {
    "title": "Nejire",
    "identifier": "urn:ehentai:gallery:4113236:73634e0e9a",
    "numberOfPages": 42
  },
  "links": [
    { "rel": "self", "href": "/opds/v2.0/gallery/4113236/73634e0e9a/manifest", "type": "application/divina+json" },
    { "rel": "alternate", "href": "https://e-hentai.org/g/4113236/73634e0e9a/", "type": "text/html" },
    { "rel": "http://opds-spec.org/progression", "href": "/opds/progression/4113236/73634e0e9a", "type": "application/opds-progression+json" }
  ],
  "images": [{ "href": "/image/4113236/73634e0e9a/thumb", "type": "image/jpeg" }]
}
```

---

## 4. 各文档形态

### 4.1 采集文档（gallery feed / toplist feed）

| 端点 | `metadata.identifier` | 分页 |
|---|---|---|
| `/opds/v2.0/gallery?query=watched` | `urn:ehentai:gallery-list:watched` | `rel="next"` → `?next={lastGid}&query=…` |
| `/opds/v2.0/gallery`（Latest） | `urn:ehentai:gallery-list:latest` | `rel="next"` → `?next={lastGid}` |
| `/opds/v2.0/toplist?period=month` | `urn:ehentai:toplist:month` | `rel="next"` → `?period=month&page={n}`（**`page` 分页**，与 lastGid 不同轨） |

- 采集文档恒带 `self` / `start` / `search` 链接；`search` 为 JSON 模板（§5）。
- `query` 取值：空=Latest、`watched`、`favorites`、`popular`；其他任意值 = 搜索词（`f_search`）。

### 4.2 详情文档（`/opds/v2.0/gallery/{gid}/{token}`）

此历史 endpoint 仍返回 OPDS 采集文档，供已保存旧 URL 的客户端使用；新 publication 不再链接到它。文档中的作品保留完整元数据和逐页 `readingOrder`。图库不存在时返回 404。

### 4.3 Divina manifest（`/opds/v2.0/gallery/{gid}/{token}/manifest`）

新目录条目的 `rel="self"` 指向此 endpoint。成功响应的 `Content-Type` 为 `application/divina+json`，包含 `@context`、`metadata.conformsTo`、封面 `images` 和逐页 `readingOrder`。每页 href 使用当前图片流路由；页码仍遵循 `PSE_PAGE_BASE`。Ready archive/catalog 路径同样输出 manifest，图库标识保持稳定，图片资源使用归档的 active version。

如果页数未知或为零，服务端返回 503，因为 Divina manifest 需要非空的 `readingOrder`。不会为此触发归档或图片下载。

旧 `/publication` endpoint 保留以兼容已保存 URL，响应仍使用 `application/opds+json`；新目录不会再广告它。

### 4.4 搜索

- **v2.0**：顶层 `rel="search"` link 的 `href` 直接含模板 `/opds/v2.0/gallery?query={searchTerms}`（标 `templated: true`）——客户端替换 `{searchTerms}` 即得搜索结果文档，无需先请求 OpenSearch。
- **v1.2**：`rel="search"` 指向 `/opds/v1.2/search.xml`（OpenSearchDescription），模板 `?query={searchTerms}`。

---

## 5. 链接语义（rel 表）

| rel | 用途 |
|---|---|
| `self` / `start` | 本文档 / 根导航 |
| `search` | 搜索（v2.0 JSON 模板；v1.2 OpenSearch 文档） |
| `next` | 下一页（gallery 用 lastGid；toplist 用 page） |
| `subsection` | 导航项 → 采集文档 |
| `self` | 本文档 / Divina manifest（客户端跟随其打开详情并阅读） |
| `http://opds-spec.org/acquisition` | OPDS 2.0 作品不再输出此链接 |
| `http://opds-spec.org/image/thumbnail` | 封面（**仅 v1.2 Atom**；v2.0 走 `images[]` 集合，§3.5） |
| `http://vaemendis.net/opds-pse/stream` | PSE 串流（仅 OPDS 1.2 Atom） |
| `alternate` | 上游 E-Hentai 原始网页（恒有，分享/跳浏览器用） |

---

## 6. 端点 href 模板

| 用途 | href |
|---|---|
| 图片流 | `/stream/{gid}/{token}/page/{pageNumber}` → `image/jpeg`；小于当前页码基数的页号返回 400，图片限额返回 429；无效图库/页号错误以 OpenAPI 响应为准 |
| 封面 | `/image/{gid}/{token}/thumb` → `image/jpeg` |
| 详情（v2.0 采集文档） | `/opds/v2.0/gallery/{gid}/{token}` |
| Divina manifest（v2.0，`self` 落点） | `/opds/v2.0/gallery/{gid}/{token}/manifest` |
| 章节（v1.2） | `/opds/v1.2/gallery/{gid}/{token}/chapters` |
| Toplist | `/opds/{v1.2,v2.0}/toplist?period=yesterday\|month\|year\|alltime&page={n}` |

---

## 7. v1.2（Atom）对照——仅标准，无扩展

**约束：v1.2 不输出任何 `x:*` 扩展字段，也不在根 feed 混入采集条目。** 客户端如需 v1.2 兼容，只消费标准字段即可。

### 7.1 根导航 entry

```xml
<entry>
  <id>urn:ehentai:subsection:popular</id>
  <title>Popular</title>
  <updated>2026-08-11T15:31:30Z</updated>
  <summary>Popular this week</summary>
  <link rel="subsection" href="/opds/v1.2/gallery?query=popular"
        type="application/atom+xml;profile=opds-catalog;kind=acquisition"/>
</entry>
```

### 7.2 图库 entry（列表/章节）

| 元素 | 说明 |
|---|---|
| `id` | `urn:ehentai:gallery:{gid}:{token}` |
| `title` | 列表 = 标题；章节 = `Chapter 1: {title}` |
| `updated` / `author/name` | 上传时间 / 上传者 |
| `category` | `term`/`label` = 分类，`scheme="http://e-hentai.org"` |
| `summary` | **当前不输出**（预留；v1.2 列表/章节条目 `summary` 恒为空，与 v2.0 `description` 一致） |
| link `http://opds-spec.org/image/thumbnail` | 封面 |
| link `http://opds-spec.org/acquisition` | `/opds/v1.2/gallery/{gid}/{token}/chapters` |
| link `http://vaemendis.net/opds-pse/stream` | `/stream/{gid}/{token}/page/{pageNumber}`，`type="image/jpeg"`，**`pse:count` 属性** = 页数（命名空间 `http://vaemendis.net/opds-pse/ns`） |
| link `alternate` | 上游 E-Hentai 图库页（`type="text/html"`），分享/跳浏览器用 |

---

## 8. 客户端渲染规则速查

1. 请求 `/opds/v2.0` 作为主页文档。
2. `groups[]` → 每个 group 直接渲染为一个网格区块：标题 = `metadata.title`，内容 = `publications[]`。点击区块条目 → 跟随 publication 的 `self` 获取 Divina manifest；点击区块标题 → 完整列表（`links[0].href`）。通用客户端同样原生支持 groups，无需任何自定义扩展解析。
3. 完整元数据（详情补全）走 `rel="self"`：每个 publication 的 `self` → `/opds/v2.0/gallery/{gid}/{token}/manifest`（顶层 Divina manifest，含完整 metadata/readingOrder）。**进入详情视图 = 明确信号 → 经 self 拉一次完整 manifest 回填**（每次进入拉一次；同一详情视图会话内去重）。**响应形状探测**：顶层含 `publications` 键 = 采集文档（取 `publications[0]`），否则整个对象即 publication。**Stump 类客户端**跟随 `self` 打开详情，并通过内嵌 `readingOrder` 流式阅读。
4. `navigation[]` → 渲染为普通导航列表（可点击进入完整列表）。
5. 搜索：用顶层 `search` link 的 JSON 模板替换 `{searchTerms}`。
6. 分页：`rel="next"`（gallery 传 `next`，toplist 传 `page`）。
7. 详情：`/opds/v2.0/gallery/{gid}/{token}` 的 `subject` 为完整标签（经 status 过滤，高亮样式由 My Tags 映射表内联）——展开详情时**按 name 合并**：以详情 subject 为全集替换，回填列表条目带来的 `x:style`，勿整体丢弃样式。EH 专属标量字段读 `metadata` 下 `x:*` 键（`x:rating` 等），不再有嵌套 `extensions` 桶。
8. 失效兜底：单个出版物栏目上游故障时省略其预览；首页布局由 `home.toml` 配置驱动，客户端无需感知。
9. **分享**：取 publication / entry 的 `rel="alternate"` link（`type="text/html"`）作为分享 URL——即上游 E-Hentai 页面（e-hentai.org 或 exhentai.org，服务端已按 `EH_SITE` 拼好），客户端无需感知 `EH_SITE`。作品链接不再提供 acquisition/stream；用 `alternate` 分享上游页面。
10. **模板链接**：href 含 `{...}`（stream 的 `{pageNumber}`、search 的 `{searchTerms}`，标 `templated: true`）一律**替换占位符后使用，永不按字面请求**；作品详情入口由 `self` 指向 Divina manifest；不要把 acquisition MIME 当作详情入口。

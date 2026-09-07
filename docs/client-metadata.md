# PandaOPDS 客户端元数据手册

面向阅读器开发者。描述 PandaOPDS 输出的 OPDS 1.2（Atom）与 OPDS 2.0（JSON）全部文档结构、字段、取值与渲染规则。通用客户端消费标准层即可；`metadata` 内 `x:*` 前缀扩展字段由需要扩展信息的客户端消费。

---

## 1. 概览

| 项 | 值 |
|---|---|
| 版本路径 | `/opds/v1.2`（Atom）、`/opds/v2.0`（JSON） |
| 媒体类型 | v1.2 导航 `application/atom+xml;profile=opds-catalog;kind=navigation`；采集 `…;kind=acquisition` |
| | v2.0 导航 `application/opds+json;profile=navigation`；采集 `application/opds+json;profile=acquisition` |
| href | 默认**相对路径**；设置 `PUBLIC_BASE_URL` 时输出绝对 URL。**Stump 必须设置**（它用自身服务器地址解析相对链接，相对路径会 404）；其他客户端不受影响 |
| 页码 | PSE stream 默认 **1-based**（第 1 页 = `page/1`）；`PSE_PAGE_BASE=0` 可切 0-based（带外约定，不在链路中传输） |
| 获取模式 | `OPDS_ACQ_DETAIL=false\|true`（布尔，默认 **`false`**=direct）：acquisition 直接指向图片流（零二次请求）或指向详情文档（二次请求流程），§3.4 |
| 缓存 | feed 均 `Cache-Control: public, max-age=300` |

**登录态**：`IPB_MEMBER_ID`/`IPB_PASS_HASH` 可选。未提供时 Watched/Favorites 导航项**不输出**；提供时输出（不做探测验证，cookie 失效时对应 feed 返回 503）。

---

## 2. 主页文档（`GET /opds/v2.0`）——设计主页的核心

文档包含 `navigation[]`（纯导航链接）和 `groups[]`（含内联 publication 预览的分组区块，OPDS 2.0 §2.5）。真实输出：

```json
{
  "metadata": { "title": "PandaOPDS", "identifier": "urn:ehentai:root", "modified": "2026-08-11T15:31:30Z" },
  "links": [
    { "href": "/opds/v2.0", "rel": "self", "type": "application/opds+json;profile=navigation", "title": "PandaOPDS" },
    { "href": "/opds/v2.0", "rel": "start", "type": "application/opds+json;profile=navigation", "title": "PandaOPDS" },
    { "href": "/opds/v2.0/gallery?query={searchTerms}", "rel": "search", "type": "application/opds+json;profile=acquisition", "title": "Search", "templated": true }
  ],
  "navigation": [
    {
      "metadata": {
        "title": "Watched",
        "identifier": "urn:ehentai:subsection:watched",
        "modified": "2026-08-11T15:31:30Z",
        "description": "Watched galleries"
      },
      "links": [
        { "href": "/opds/v2.0/gallery?query=watched", "rel": "subsection",
          "type": "application/opds+json;profile=acquisition", "title": "Watched" }
      ]
    }
  ],
  "groups": [
    {
      "metadata": {
        "title": "Latest",
        "identifier": "urn:ehentai:group:latest",
        "modified": "2026-08-11T15:31:30Z"
      },
      "links": [
        { "rel": "self", "href": "/opds/v2.0/gallery",
          "type": "application/opds+json;profile=acquisition", "title": "Latest" }
      ],
      "publications": [ "…前 N 条 publication（见 §3）…" ]
    }
  ]
}
```

### 2.1 分区逻辑

| 组件 | 内容 | 说明 |
|---|---|---|
| `groups[]` | 内联 publication 预览的分组区块 | `config/home.toml`（`[[group]]`）控制；环境变量 `HOME_CONFIG` 可指定路径 |
| `navigation[]` | 纯导航链接（不含 `x:*` 扩展字段） | `config/home.toml`（`[[navigation]]`）控制；Watched/Favorites 无 IPB cookie 时不输出 |
| `links[].rel="search"` | 搜索模板 | 顶层 link，客户端替换 `{searchTerms}` 即得搜索结果 |

### 2.2 groups[] 元素结构（OPDS 2.0 标准，§2.5）

| 字段 | 说明 |
|---|---|
| `metadata.title` | 区块标题（如 `Latest`、`Popular`、`Toplist: Yesterday`） |
| `metadata.identifier` | `urn:ehentai:group:{key}` |
| `metadata.modified` | ISO8601（UTC） |
| `links[0]` | `rel="self"`，`href` = 该区块的完整采集文档 |
| `publications[]` | 内联预览条目（数量由 TOML `publications` 字段控制），字段见 §3 |

每个 group 是 OPDS 2.0 标准结构——**任何兼容客户端均可原生渲染为分栏网格**，无需自定义扩展标记。

### 2.3 navigation[] 元素结构

| 字段 | 说明 |
|---|---|
| `metadata.title` | 导航入口标题 |
| `metadata.identifier` | `urn:ehentai:subsection:{title 小写}` |
| `metadata.description` | 一句话描述 |
| `links[0]` | `rel="subsection"`，`href` = 完整采集文档 |

> **不再有 layout 自定义扩展**：showcase 机制已被 groups 取代。`navigation[]` 中所有条目均为纯导航链接，客户端按标准 `subsection` 语义处理即可。

### 2.4 所有已知区块清单

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

**服务端调控**：`config/home.toml`（`[[group]]` / `[[navigation]]`），环境变量 `HOME_CONFIG` 可指定路径。书写顺序 = 输出顺序；`publications` 字段控制预览条数。

---

## 3. publication（条目/Item）元数据

任意采集文档（首页 Latest、gallery feed、toplist feed、详情文档）中的单个条目。字段分两层：**标准层**（通用客户端直接消费）与**扩展层 `metadata` 内 `x:*` 前缀字段**（EH 专属扩展）。前缀由文档顶层内联 JSON-LD context 声明：`"context": ["https://readium.org/webpub-manifest/context.jsonld", {"x": "https://github.com/n1atsys0r/PandaOPDS/vocab#"}]`——通用客户端忽略未知成员，无需感知。

### 3.1 标准层

| 字段 | 类型 | 说明 | 条件 |
|---|---|---|---|
| `title` | string | 干净标题（已剥离 `[...]`/`(...)` 标记；作者见 `authors` 字段） | 恒有 |
| `identifier` | string | `urn:ehentai:gallery:{gid}:{token}` | 恒有 |
| `modified` | string | 上传时间 ISO8601（UTC） | 恒有 |
| `authors` | [ {`name`} ] | 作者（从标题 `[Author]` 括号解析，见 §3.6）；上传者本人见详情文档 `x:uploader` | 非空时 |
| `author` | [ {`name`} ] | RWPM 单数形式，与 `authors` 并存（Stump/Readium 解析器只认 `author`） | 非空时 |
| `language` | [string] | 语言（**BCP 47 / RFC 5646 码**，如 `zh`/`ja`/`zh-Hans`；由 EH `language:` 标签映射，未知与标记伪标签不输出） | 非空时 |
| `published` | string | = `modified`（上传时间） | 恒有 |
| `description` | string | **当前不输出**（预留字段；客户端如需描述，可自行拼接 `language`/`numberOfPages`/`authors`/`x:rating`/`x:sizeBytes`） | — |
| `subject` | [ {`name`, `x:style`?} ] | RWPM collection of objects：每项 `{"name": "ns:key"}`（去重保序，**不含分类**，分类见 `x:category`）；带高亮样式的标签额外内联 `x:style`（§3.3） | 有标签时 |
| `numberOfPages` | int | 页数（= `filecount`） | >0 时 |

### 3.2 扩展层 `x:*` 字段（拍平进 `metadata`，全部 EH 专属）

| 字段 | 类型 | 说明 | 条件 |
|---|---|---|---|
| `x:rating` | float | 评分（0–5，保留原精度如 4.5） | ≠0 时 |
| `x:titleJpn` | string | 日文标题 | 非空时 |
| `x:sizeBytes` | int | 文件总字节 | ≠0 时 |
| `x:expunged` | bool | 已删除标记 | 仅 `true` 时输出 |
| `x:category` | string | 分类（Doujinshi/Manga/Artist CG/Game CG/Image Set/Non-H/Western/Misc…）；**刻意不进 `subject`**（避免与标签混淆），搜索维度的分类筛选走 facets（`category=` 参数 + `FACETS` 掩码） | 恒有 |
| `x:uploader` | string | 上传者（详情页 `#gdn`） | 仅详情文档，非空时 |
| `x:reviews` | array | 评论区（仅详情文档；每项 `id`/`username`/`userId`(可选)/`time`/`lastEditTime`(可选)/`content`(**原始 HTML**)）；`COMMENTS_ENABLED=0` 关闭 | 有评论时 |

> **评论 `content` 的重写**（客户端无需感知，直接渲染即可）：① 图库链接 `(e-hentai|exhentai).org/(g|mpv)/{gid}/{token}/` → `/opds/v2.0/gallery/{gid}/{token}`（app 内跳转）；② eh/ex 封面/预览图（host ∈ `IMAGE_PROXY_HOSTS`，默认 `ehgt.org,s.exhentai.org`）在 `src`/`url()` 内 → 同源代理 `/image/fetch?url=<编码>`，**规避 WebView 跨域 CORS**（否则 `<img>` 原生跨域拉图被拦）。URL 不透明、不解析 gid/token。

> 原 `metadata.extensions` 嵌套桶已移除；所有 EH 专属字段直接平铺在 `metadata` 下。旧字段名映射：`rating`→`x:rating`、`uploader`→`x:uploader`、`titleJpn`→`x:titleJpn`、`sizeBytes`→`x:sizeBytes`、`expunged`→`x:expunged`、`category`→`x:category`。

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

**获取模式**：acquisition link 的指向由服务端布尔配置 `OPDS_ACQ_DETAIL` 决定（默认 `false` = direct，兼容至上）：

- **`OPDS_ACQ_DETAIL=false`（默认，direct）**：acquisition 直接指向图片流——客户端点击即读，**零二次请求**。不输出指向详情文档的 acquisition（详情文档仍可访问，客户端由 identifier 中的 gid/token 拼 URL）。
- **`OPDS_ACQ_DETAIL=true`（detail）**：acquisition 指向详情文档——客户端二次请求详情（完整元数据）后再读。
- 未知页数时（无 `page_count`）：`direct` 模式不输出 acquisition/stream；`detail` 模式保留指向详情文档的 acquisition（无 `numberOfItems`）。

| rel | href | type | 附加 |
|---|---|---|---|
| `self` | `/opds/v2.0/gallery/{gid}/{token}/publication` | `application/opds+json` | **恒有**；单 publication 文档（顶层 RWPM 对象）；**详情补全入口**（Stump 等客户端跟随 `self` 打开详情；客户端进详情经 self 拉一次） |
| `http://opds-spec.org/acquisition` | `direct`：`/stream/{gid}/{token}/page/{pageNumber}`；`detail`：`/opds/v2.0/gallery/{gid}/{token}` | `direct`：`image/jpeg`；`detail`：`application/opds+json;profile=acquisition` | `properties.numberOfItems` = 页数（>0 时）；**只承担内容获取**（读流/下载），不承担详情入口；`direct` 模式为模板链接，标 `templated: true` |
| `http://vaemendis.net/opds-pse/stream` | `/stream/{gid}/{token}/page/{pageNumber}` | `image/jpeg` | `properties.numberOfItems` = 页数；`{pageNumber}` 占位符由客户端替换；**`templated: true`**；页数>0 时 |
| `alternate` | 上游 E-Hentai 图库页 `https://{e-hentai\|exhentai}.org/g/{gid}/{token}/` | `text/html` | **恒有**；**分享表单取此 link**（客户端无需感知 `EH_SITE`）；绝对 URL，不受 `PUBLIC_BASE_URL` 影响 |

> **模板标记**：href 含 `{...}` 的链接（stream/acquisition 的 `{pageNumber}`、search 的 `{searchTerms}`）统一标 `templated: true`（RWPM link 语义）——客户端**替换占位符后使用，永不按字面请求**；self/alternate/next/facets 等具体 URL 不带此标记。v1.2（Atom）无 templated 属性，PSE rel 语义自身定义 href 为模板。

> **封面不在 `links` 中**：thumbnail link rel（`http://opds-spec.org/image/thumbnail`）是 OPDS 1.x 的做法，v2.0 按规范 §2.3 放入 `images[]` 集合（见 §3.5）。v1.2（Atom）仍用 link rel。

### 3.5 封面（`images[]` 集合）

OPDS 2.0 将视觉表现（封面/缩略图）放在顶层 `images` 集合。**恒有**（缩略图代理零 ehapi，不依赖 gdata）：

```json
"images": [
  { "href": "/image/{gid}/{token}/thumb", "type": "image/jpeg" }
]
```

| 字段 | 说明 |
|---|---|
| `href` | 缩略图代理（302 到上游或磁盘缓存字节） |
| `type` | `image/jpeg` |

当前仅输出一个尺寸；响应式多尺寸（`width`/`height` 变体）预留，未来有尺寸数据时再加。

### 3.6 完整 publication 示例

```json
{
  "context": [
    "https://readium.org/webpub-manifest/context.jsonld",
    { "x": "https://github.com/n1atsys0r/PandaOPDS/vocab#" }
  ],
  "metadata": {
    "title": "Nejire",
    "identifier": "urn:ehentai:gallery:4113236:73634e0e9a",
    "modified": "2025-08-11T08:13:20Z",
    "authors": [{ "name": "leopoldo" }],
    "language": ["zh"],
    "published": "2025-08-11T08:13:20Z",
    "subject": [
      { "name": "female:netorare", "x:style": {
          "color": "#f1f1f1", "borderColor": "#048751",
          "background": "radial-gradient(#048751,#24A771)" } },
      { "name": "parody:zenless zone zero" }
    ],
    "numberOfPages": 42,
    "x:rating": 4.5,
    "x:category": "Manga"
  },
  "links": [
    { "rel": "http://opds-spec.org/acquisition", "href": "/stream/4113236/73634e0e9a/page/{pageNumber}",
      "type": "image/jpeg", "templated": true, "title": "Nejire",
      "properties": { "numberOfItems": 42 } },
    { "rel": "http://vaemendis.net/opds-pse/stream", "href": "/stream/4113236/73634e0e9a/page/{pageNumber}",
      "type": "image/jpeg", "templated": true, "properties": { "numberOfItems": 42 } },
    { "rel": "self", "href": "/opds/v2.0/gallery/4113236/73634e0e9a/publication",
      "type": "application/opds+json", "title": "Nejire" },
    { "rel": "alternate", "href": "https://e-hentai.org/g/4113236/73634e0e9a/",
      "type": "text/html", "title": "e-hentai.org" }
  ],
  "images": [
    { "href": "/image/4113236/73634e0e9a/thumb", "type": "image/jpeg" }
  ]
}
```

> 以上为默认 `OPDS_ACQ_DETAIL=false`（direct）模式；`OPDS_ACQ_DETAIL=true` 时 acquisition 指向 `/opds/v2.0/gallery/{gid}/{token}`（`type="application/opds+json;profile=acquisition"`），客户端二次请求详情后再读。

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

- `publications` 仅 1 条；**不输出 `description`**；完整标签在 `subject`（详情 `#taglist` 全量，经 `TAG_STATUS_FILTER` 过滤，高亮样式由 My Tags 映射表回填）；`metadata` 含 `x:rating`/`x:uploader`/`x:titleJpn`/`x:sizeBytes`/`x:expunged`/`x:category`（无列表专属的样式回填差异，§3.3）。
- **详情 publication 的 acquisition 恒直接指向图片流**（`/stream/{gid}/{token}/page/{pageNumber}`，`image/jpeg`，两种模式一致），**绝不指向自身**（无自循环）；并内嵌 `readingOrder`（逐页图片 URL，见 §4.3）。
- 图库不存在 → 404。

### 4.3 单 publication 文档（`/opds/v2.0/gallery/{gid}/{token}/publication`）

每个 publication 的 `rel="self"` link 指向此端点。响应是**顶层 RWPM publication 对象**（非采集文档），Stump 等客户端跟随 `self` 打开详情：

```json
{
  "context": [
    "https://readium.org/webpub-manifest/context.jsonld",
    { "x": "https://github.com/n1atsys0r/PandaOPDS/vocab#" }
  ],
  "metadata": { "title": "…", "author": [{ "name": "…" }], "numberOfPages": 42, "x:rating": 4.5, "…": "…" },
  "links": [
    { "rel": "self", "href": "/opds/v2.0/gallery/{gid}/{token}/publication", "type": "application/opds+json" },
    { "rel": "http://opds-spec.org/acquisition", "href": "/stream/{gid}/{token}/page/{pageNumber}", "type": "image/jpeg", "templated": true, "properties": { "numberOfItems": 42 } },
    { "rel": "http://vaemendis.net/opds-pse/stream", "href": "/stream/{gid}/{token}/page/{pageNumber}", "type": "image/jpeg", "templated": true, "properties": { "numberOfItems": 42 } },
    { "rel": "alternate", "href": "https://e-hentai.org/g/{gid}/{token}/", "type": "text/html" }
  ],
  "images": [{ "href": "/image/{gid}/{token}/thumb", "type": "image/jpeg" }],
  "readingOrder": [
    { "href": "/stream/{gid}/{token}/page/1", "type": "image/jpeg" },
    …共页数条（默认 1-based，`PSE_PAGE_BASE=0` 时从 0 起）…
  ]
}
```

- **`readingOrder`**：RWPM 逐页图片 URL 列表——Stump 的 Stream 阅读器据此逐页拉图（零额外查询）。
- `metadata.author`（RWPM 单数）与 `authors` 并存（Stump/Readium 只认 `author`）。
- 图库不存在 → 404。

### 4.3 搜索

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
| `self` | 本文档 / 单 publication 文档（Stump 跟随其打开详情） |
| `http://opds-spec.org/acquisition` | 获取内容（`direct`：直接指向图片流；`detail`：指向详情文档） |
| `http://opds-spec.org/image/thumbnail` | 封面（**仅 v1.2 Atom**；v2.0 走 `images[]` 集合，§3.5） |
| `http://vaemendis.net/opds-pse/stream` | PSE 串流（`{pageNumber}` 占位符） |
| `alternate` | 上游 E-Hentai 原始网页（恒有，分享/跳浏览器用） |

---

## 6. 端点 href 模板

| 用途 | href |
|---|---|
| 图片流 | `/stream/{gid}/{token}/page/{pageNumber}` → `image/jpeg`；越界/509 → 429/404 |
| 封面 | `/image/{gid}/{token}/thumb` → `image/jpeg` |
| 详情（v2.0 采集文档） | `/opds/v2.0/gallery/{gid}/{token}` |
| 单 publication（v2.0，`self` 落点） | `/opds/v2.0/gallery/{gid}/{token}/publication` |
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
2. `groups[]` → 每个 group 直接渲染为一个网格区块：标题 = `metadata.title`，内容 = `publications[]`。点击区块条目 → 走 `acquisition`（默认 `direct` 模式即图片流，直接读）或 `stream`；点击区块标题 → 完整列表（`links[0].href`）。通用客户端同样原生支持 groups，无需任何自定义扩展解析。
3. 完整元数据（详情补全）走 `rel="self"`：每个 publication 的 `self` → `/opds/v2.0/gallery/{gid}/{token}/publication`（顶层 RWPM 对象，含 reviews/完整 tags/readingOrder）。**进入详情视图 = 明确信号 → 经 self 拉一次完整 manifest 回填**（每次进入拉一次；同一详情视图会话内去重；服务端详情页 HTML 缓存 1h，成本可忽略）；无 self 时回退 acquisition 白名单（type 属 `application/opds+json`/`application/atom+xml`/`application/xml`，剥离 `;profile=` 参数）或静默用列表数据兜底。**响应形状探测**：顶层含 `publications` 键 = 采集文档（取 `publications[0]`），否则整个对象即 publication。`acquisition` 只承担内容获取（读流/下载），不承担详情入口；**Stump 类客户端**跟随 `self` 打开详情，并通过内嵌 `readingOrder` 流式阅读。
4. `navigation[]` → 渲染为普通导航列表（可点击进入完整列表）。
5. 搜索：用顶层 `search` link 的 JSON 模板替换 `{searchTerms}`。
6. 分页：`rel="next"`（gallery 传 `next`，toplist 传 `page`）。
7. 详情：`/opds/v2.0/gallery/{gid}/{token}` 的 `subject` 为完整标签（经 status 过滤，高亮样式由 My Tags 映射表内联）——展开详情时**按 name 合并**：以详情 subject 为全集替换，回填列表条目带来的 `x:style`，勿整体丢弃样式。EH 专属标量字段读 `metadata` 下 `x:*` 键（`x:rating` 等），不再有嵌套 `extensions` 桶。
8. 失效兜底：单个 group 上游故障时该 group 不出现在 `groups[]` 中（其他 groups 和 navigation 照常）；首页布局由 `home.toml` 配置驱动，客户端无需感知。
9. **分享**：取 publication / entry 的 `rel="alternate"` link（`type="text/html"`）作为分享 URL——即上游 E-Hentai 页面（e-hentai.org 或 exhentai.org，服务端已按 `EH_SITE` 拼好），客户端无需感知 `EH_SITE`。勿用 acquisition/stream（那些是服务端资源，离开服务端不可达）。
10. **模板链接**：href 含 `{...}`（stream 的 `{pageNumber}`、search 的 `{searchTerms}`，标 `templated: true`）一律**替换占位符后使用，永不按字面请求**；「可二次请求」的唯一依据 = acquisition type ∈ {`application/opds+json`、`application/atom+xml`、`application/xml`}（剥离 `;profile=` 参数）——archive/模板/图片/未知 MIME 一律跳过。

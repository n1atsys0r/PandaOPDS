<script setup>
import { ref, computed, watch, nextTick } from "vue";
import {
  Save,
  Plus,
  Trash2,
  ChevronUp,
  ChevronDown,
  ChevronRight,
  Compass,
  Layers,
  BookOpen,
  Filter,
  AlertTriangle,
  RotateCcw,
  Check,
} from "@lucide/vue";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { useApp } from "@/state.js";
import { canUseFacets, replacesAlternateMode } from "@/home-layout.js";

const s = useApp();

// Tracking expanded items via Set of object references
const expandedItems = ref(new Set());

function isExpanded(item) {
  if (!item) return false;
  return expandedItems.value.has(item);
}

function toggleExpand(item) {
  if (!item) return;
  const next = new Set(expandedItems.value);
  if (next.has(item)) {
    next.delete(item);
  } else {
    next.add(item);
  }
  expandedItems.value = next;
}

function expandItem(item) {
  if (!item) return;
  const next = new Set(expandedItems.value);
  next.add(item);
  expandedItems.value = next;
}

function collapseAll() {
  expandedItems.value = new Set();
}

function expandAll() {
  const next = new Set();
  (s.layout.value.navigation || []).forEach((x) => next.add(x));
  (s.layout.value.groups || []).forEach((x) => next.add(x));
  if (s.layout.value.publication) next.add(s.layout.value.publication);
  expandedItems.value = next;
}

// Automatically ensure publication is expanded when selected
watch(
  () => s.layout.value.publication,
  (pub) => {
    if (pub) expandItem(pub);
  },
  { immediate: true },
);

function countLabel(section) {
  if (!section || section.count === 0) return "无预览";
  if (section.count == null || section.count === "") return "默认 10 项";
  return section.count + " 项";
}

const homeReplaceWarning = computed(() => {
  const persisted = s.home.value?.sections || [];
  if (
    replacesAlternateMode(persisted, s.homeMode.value) &&
    s.homeMode.value === "groups"
  )
    return "保存分组布局会移除服务器上当前的顶层出版物和筛选。此草稿仍暂存在本页。";
  if (
    replacesAlternateMode(persisted, s.homeMode.value) &&
    s.homeMode.value === "publications"
  )
    return "保存出版物布局会移除服务器上当前的分组栏目。此草稿仍暂存在本页。";
  return "";
});

const homeFacetWarning = computed(() =>
  s.homeMode.value === "publications" &&
  s.layout.value.facets.length > 0 &&
  !canUseFacets(s.layout.value.publication)
    ? "当前出版物来源不支持筛选。请移除所有筛选，或改用搜索、latest、toplist 或 favorites 来源。"
    : "",
);

// Actions with auto-expand
function handleAddNavigation() {
  s.addNavigation();
  nextTick(() => {
    const list = s.layout.value.navigation;
    if (list.length > 0) {
      expandItem(list[list.length - 1]);
    }
  });
}

function handleAddGroup(kind) {
  s.addGroupSection(kind);
  nextTick(() => {
    const list = s.layout.value.groups;
    if (list.length > 0) {
      expandItem(list[list.length - 1]);
    }
  });
}

function handleAddPublication() {
  s.addPublication();
  nextTick(() => {
    if (s.layout.value.publication) {
      expandItem(s.layout.value.publication);
    }
  });
}

function handleAddFacet() {
  s.addFacet();
}

const PRESET_GROUPS = [
  {
    label: "基础画廊预设",
    options: [
      { value: "latest", label: "最新画廊 (latest)" },
      { value: "popular", label: "热门画廊 (popular)" },
      { value: "watched", label: "关注订阅 (watched)" },
      { value: "favorites", label: "全部收藏 (favorites)" },
      { value: "archives", label: "本地归档 (archives)" },
    ],
  },
  {
    label: "排行榜 (Toplist)",
    options: [
      { value: "toplist:yesterday", label: "昨日排行 (toplist:yesterday)" },
      { value: "toplist:month", label: "月度排行 (toplist:month)" },
      { value: "toplist:year", label: "年度排行 (toplist:year)" },
      { value: "toplist:alltime", label: "历史总榜 (toplist:alltime)" },
    ],
  },
  {
    label: "收藏夹分类 (0 - 9)",
    options: Array.from({ length: 10 }, (_, i) => ({
      value: `favorites:${i}`,
      label: `收藏夹 ${i} (favorites:${i})`,
    })),
  },
];

function isKnownPreset(q) {
  return PRESET_GROUPS.some((g) => g.options.some((o) => o.value === q));
}

function handleTypeChange(item) {
  if (item.type === "preset" && !isKnownPreset(item.query)) {
    item.query = "latest";
  }
  s.homeDirty.value = true;
}
</script>

<template>
  <div class="home-editor-view space-y-6">
    <!-- Page Heading -->
    <div class="heading">
      <div>
        <small>OPDS 入口</small>
        <h1>首页编排</h1>
        <p>统一编排首页导航与主体内容，输出直观清晰的 OPDS 目录结构。</p>
      </div>
      <div class="flex items-center gap-2">
        <Button
          class="primary-action"
          :disabled="
            !s.homeDirty.value ||
            s.busy.value ||
            s.homeModeRequired.value ||
            Boolean(homeFacetWarning)
          "
          @click="s.saveHome"
        >
          <Save class="size-4" />保存布局
        </Button>
      </div>
    </div>

    <!-- Legacy Readonly Fallback -->
    <Card v-if="s.home.value?.legacy" class="panel">
      <CardHeader class="panel-header">
        <CardTitle>首页布局（只读）</CardTitle>
        <CardDescription
          >编辑接口不可用 · {{ s.home.value.legacy.path }}</CardDescription
        >
      </CardHeader>
      <CardContent class="panel-body">
        <div
          v-for="(item, i) in s.home.value.legacy.sections || []"
          :key="i"
          class="preview"
        >
          <b>{{ i + 1 }}</b>
          <div>
            <strong>{{ item.title || item.query }}</strong>
            <small>{{ item.kind }} · {{ item.query }}</small>
          </div>
        </div>
      </CardContent>
    </Card>

    <!-- Error State -->
    <Card v-else-if="s.home.value?.error" class="panel">
      <CardHeader class="panel-header">
        <CardTitle>首页布局无法读取</CardTitle>
      </CardHeader>
      <CardContent class="panel-body">
        <div class="archive-error">{{ s.home.value.error }}</div>
      </CardContent>
    </Card>

    <!-- Main Workspace -->
    <template v-else-if="s.home.value?.sections">
      <!-- Validation / Repair Banner -->
      <Alert
        v-if="s.home.value?.validation_error"
        variant="destructive"
        class="border-amber-500/40 bg-amber-500/10 text-foreground"
      >
        <AlertTriangle class="size-4 text-amber-500" />
        <div class="flex-1 space-y-1">
          <AlertTitle class="text-sm font-semibold"
            >需要修复现有布局</AlertTitle
          >
          <AlertDescription class="text-xs text-muted-foreground">
            {{ s.home.value.validation_error }} · 原文件会在保存时备份。
          </AlertDescription>
          <div class="repair-drafts mt-3 grid grid-cols-1 sm:grid-cols-2 gap-2">
            <div
              class="p-2.5 rounded-md border border-border/80 bg-background/80"
            >
              <strong class="text-xs font-medium block">出版物草稿</strong>
              <span class="text-xs text-muted-foreground block mt-0.5">
                {{
                  s.layout.value.publication
                    ? s.layout.value.publication.title ||
                      s.layout.value.publication.query
                    : "无顶层出版物"
                }}
                · {{ s.layout.value.facets.length }} 个筛选
              </span>
            </div>
            <div
              class="p-2.5 rounded-md border border-border/80 bg-background/80"
            >
              <strong class="text-xs font-medium block">
                分组草稿 · {{ s.layout.value.groups.length }} 个栏目
              </strong>
              <span class="text-xs text-muted-foreground block mt-0.5">
                {{
                  s.layout.value.groups
                    .map((g) => g.title || g.query)
                    .slice(0, 3)
                    .join("、")
                }}
                {{ s.layout.value.groups.length > 3 ? "等" : "" }}
              </span>
            </div>
          </div>
        </div>
      </Alert>

      <!-- Warning Banners -->
      <Alert
        v-if="homeReplaceWarning"
        class="border-amber-500/30 bg-amber-500/5 text-foreground py-2.5"
      >
        <AlertTriangle class="size-4 text-amber-500 shrink-0" />
        <AlertDescription class="text-xs text-muted-foreground">
          {{ homeReplaceWarning }}
        </AlertDescription>
      </Alert>

      <Alert v-if="homeFacetWarning" variant="destructive" class="py-2.5">
        <AlertTriangle class="size-4 text-destructive shrink-0" />
        <AlertDescription class="text-xs">
          {{ homeFacetWarning }} 你仍可删除现有筛选。
        </AlertDescription>
      </Alert>

      <!-- Unified Workbench Card -->
      <Card class="panel home-workbench">
        <CardContent class="p-5 sm:p-6 space-y-6">
          <!-- ================= SECTION 1: 顶层导航 ================= -->
          <section class="home-section space-y-4">
            <!-- Section Header -->
            <div class="flex items-start justify-between gap-4 pb-1">
              <div>
                <div class="flex items-center gap-2">
                  <Compass class="size-4 text-primary" />
                  <h2 class="text-base font-semibold tracking-tight">
                    顶层导航
                  </h2>
                  <Badge
                    variant="secondary"
                    class="h-5 px-1.5 text-[11px] font-mono font-normal"
                  >
                    {{ s.layout.value.navigation.length }}
                  </Badge>
                </div>
                <p class="text-xs text-muted-foreground mt-1">
                  固定展示在 OPDS
                  首页顶层，用于跨功能快捷跳转，可与任一内容模式共存。
                </p>
              </div>
              <div class="flex items-center gap-2 shrink-0">
                <Button
                  variant="outline"
                  size="sm"
                  class="h-8 gap-1.5 text-xs"
                  @click="handleAddNavigation"
                >
                  <Plus class="size-3.5" />添加导航
                </Button>
              </div>
            </div>

            <!-- Navigation Items List -->
            <div
              v-if="s.layout.value.navigation.length > 0"
              class="space-y-2.5"
            >
              <div
                v-for="(nav, i) in s.layout.value.navigation"
                :key="'nav-' + i"
                class="home-item-card rounded-lg border border-border bg-card transition-[border-color,box-shadow] duration-150 overflow-hidden shadow-xs hover:border-border/80"
                :class="{
                  'ring-1 ring-primary/20 border-primary/30': isExpanded(nav),
                }"
              >
                <!-- Summary Bar -->
                <div
                  class="home-item-bar flex items-center justify-between gap-3 px-3.5 py-2.5 cursor-pointer select-none transition-colors hover:bg-muted/30"
                  @click="toggleExpand(nav)"
                >
                  <div class="flex items-center gap-2.5 min-w-0 flex-1">
                    <span
                      class="size-5 rounded text-[11px] font-mono font-medium flex items-center justify-center shrink-0 bg-muted text-muted-foreground"
                    >
                      {{ i + 1 }}
                    </span>
                    <span
                      class="font-medium text-sm text-foreground truncate max-w-[200px] sm:max-w-[280px]"
                    >
                      {{ nav.title || nav.query || "未命名导航" }}
                    </span>
                    <div class="flex items-center gap-1.5 shrink-0 flex-wrap">
                      <Badge
                        variant="outline"
                        class="text-[10px] px-1.5 py-0 h-4.5 font-normal text-muted-foreground border-border/80"
                      >
                        {{ nav.type === "search" ? "搜索" : "预设" }}
                      </Badge>
                      <code
                        class="text-[11px] font-mono bg-muted/80 text-muted-foreground px-1.5 py-0.5 rounded truncate max-w-[160px] sm:max-w-[220px]"
                      >
                        {{ nav.query || "未设置" }}
                      </code>
                    </div>
                  </div>

                  <!-- Quick Actions (Never hidden in fold) -->
                  <div class="flex items-center gap-0.5 shrink-0" @click.stop>
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      :disabled="i === 0"
                      aria-label="导航上移"
                      title="上移"
                      @click="s.moveItem('navigation', i, -1)"
                    >
                      <ChevronUp class="size-3.5" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      :disabled="i === s.layout.value.navigation.length - 1"
                      aria-label="导航下移"
                      title="下移"
                      @click="s.moveItem('navigation', i, 1)"
                    >
                      <ChevronDown class="size-3.5" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      class="text-muted-foreground hover:text-destructive hover:bg-destructive/10"
                      aria-label="删除导航"
                      title="删除"
                      @click="s.removeItem('navigation', i)"
                    >
                      <Trash2 class="size-3.5" />
                    </Button>
                    <Separator orientation="vertical" class="h-3.5 mx-1" />
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      :aria-expanded="isExpanded(nav)"
                      :title="isExpanded(nav) ? '收起配置' : '展开编辑'"
                      @click="toggleExpand(nav)"
                    >
                      <ChevronRight
                        class="size-3.5 text-muted-foreground transition-transform duration-200"
                        :class="{
                          'rotate-90 text-foreground': isExpanded(nav),
                        }"
                      />
                    </Button>
                  </div>
                </div>

                <!-- Expanded Form -->
                <div
                  v-if="isExpanded(nav)"
                  class="border-t border-border bg-muted/20 px-4 py-3.5"
                >
                  <div class="grid grid-cols-1 sm:grid-cols-12 gap-3">
                    <div class="sm:col-span-5 space-y-1.5">
                      <label class="text-xs font-medium text-muted-foreground"
                        >导航标题</label
                      >
                      <Input
                        v-model="nav.title"
                        placeholder="导航标题 (如: 关注订阅)"
                        class="h-8 text-xs bg-background"
                        @update:model-value="s.homeDirty.value = true"
                      />
                    </div>
                    <div class="sm:col-span-3 space-y-1.5">
                      <label class="text-xs font-medium text-muted-foreground"
                        >来源类型</label
                      >
                      <select
                        v-model="nav.type"
                        class="home-select w-full h-8 text-xs rounded-md border border-input bg-background px-2.5 text-foreground shadow-xs outline-none focus:border-ring focus:ring-1 focus:ring-ring"
                        @change="handleTypeChange(nav)"
                      >
                        <option value="preset">预设 (Preset)</option>
                        <option value="search">搜索 (Search)</option>
                      </select>
                    </div>
                    <div class="sm:col-span-4 space-y-1.5">
                      <label class="text-xs font-medium text-muted-foreground">
                        {{ nav.type === "preset" ? "预设名称" : "搜索表达式" }}
                      </label>
                      <select
                        v-if="nav.type === 'preset'"
                        v-model="nav.query"
                        class="home-select w-full h-8 text-xs rounded-md border border-input bg-background px-2.5 text-foreground shadow-xs outline-none focus:border-ring focus:ring-1 focus:ring-ring font-mono"
                        @change="s.homeDirty.value = true"
                      >
                        <option v-if="!nav.query" value="" disabled>
                          选择预设...
                        </option>
                        <option
                          v-else-if="!isKnownPreset(nav.query)"
                          :value="nav.query"
                        >
                          {{ nav.query }} (自定义预设)
                        </option>
                        <optgroup
                          v-for="grp in PRESET_GROUPS"
                          :key="grp.label"
                          :label="grp.label"
                        >
                          <option
                            v-for="opt in grp.options"
                            :key="opt.value"
                            :value="opt.value"
                          >
                            {{ opt.label }}
                          </option>
                        </optgroup>
                      </select>
                      <Input
                        v-else
                        v-model="nav.query"
                        placeholder="如: language:chinese"
                        class="h-8 text-xs bg-background font-mono"
                        @update:model-value="s.homeDirty.value = true"
                      />
                    </div>
                  </div>
                </div>
              </div>
            </div>

            <!-- Empty Navigation State -->
            <div
              v-else
              class="flex flex-col items-center justify-center p-7 rounded-lg border border-dashed border-border bg-muted/10 text-center"
            >
              <Compass class="size-7 text-muted-foreground/60 mb-2" />
              <p class="text-xs font-medium text-foreground">
                尚未配置顶层导航
              </p>
              <p class="text-[11px] text-muted-foreground mt-0.5">
                点击上方「添加导航」可加入常用入口，在客户端中始终置顶。
              </p>
            </div>
          </section>

          <Separator class="my-6" />

          <!-- ================= SECTION 2: 内容布局 ================= -->
          <section class="home-section space-y-4">
            <!-- Section Header & Segmented Mode Switcher -->
            <div
              class="flex flex-col sm:flex-row sm:items-center justify-between gap-3 pb-1"
            >
              <div>
                <div class="flex items-center gap-2">
                  <Layers class="size-4 text-primary" />
                  <h2 class="text-base font-semibold tracking-tight">
                    内容布局
                  </h2>
                </div>
                <p class="text-xs text-muted-foreground mt-1">
                  选择首页主体内容的呈现形式。出版物流与分组栏目二选一。
                </p>
              </div>

              <!-- Segmented Control Mode Switch -->
              <div
                class="segmented-control shrink-0"
                role="tablist"
                aria-label="首页内容布局模式"
              >
                <button
                  type="button"
                  role="tab"
                  class="segment-btn"
                  :class="{ active: s.homeMode.value === 'publications' }"
                  :aria-selected="s.homeMode.value === 'publications'"
                  @click="s.selectHomeMode('publications')"
                >
                  <BookOpen class="size-3.5" />
                  <span>出版物流模式</span>
                </button>
                <button
                  type="button"
                  role="tab"
                  class="segment-btn"
                  :class="{ active: s.homeMode.value === 'groups' }"
                  :aria-selected="s.homeMode.value === 'groups'"
                  @click="s.selectHomeMode('groups')"
                >
                  <Layers class="size-3.5" />
                  <span>分组栏目模式</span>
                </button>
              </div>
            </div>

            <!-- MODE A: 分组栏目模式 -->
            <div v-if="s.homeMode.value === 'groups'" class="space-y-3 pt-1">
              <!-- Toolbar for Groups -->
              <div class="flex items-center justify-between gap-3">
                <div class="flex items-center gap-2">
                  <span class="text-xs font-medium text-foreground"
                    >分组栏目列表</span
                  >
                  <Badge
                    variant="secondary"
                    class="h-5 px-1.5 text-[11px] font-mono font-normal"
                  >
                    {{ s.layout.value.groups.length }}
                  </Badge>
                  <span
                    class="text-[11px] text-muted-foreground hidden sm:inline"
                  >
                    · 每个分组在客户端独立呈现为一个横向画廊流或子分类
                  </span>
                </div>
                <div class="flex items-center gap-2 shrink-0">
                  <Button
                    variant="outline"
                    size="sm"
                    class="h-8 gap-1.5 text-xs"
                    @click="handleAddGroup('publication')"
                  >
                    <Plus class="size-3.5" />出版物分组
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    class="h-8 gap-1.5 text-xs"
                    @click="handleAddGroup('navigation')"
                  >
                    <Plus class="size-3.5" />导航分组
                  </Button>
                </div>
              </div>

              <!-- Group Items List -->
              <div v-if="s.layout.value.groups.length > 0" class="space-y-2.5">
                <div
                  v-for="(item, i) in s.layout.value.groups"
                  :key="'group-' + i"
                  class="home-item-card rounded-lg border border-border bg-card transition-[border-color,box-shadow] duration-150 overflow-hidden shadow-xs hover:border-border/80"
                  :class="{
                    'ring-1 ring-primary/20 border-primary/30':
                      isExpanded(item),
                  }"
                >
                  <!-- Summary Bar -->
                  <div
                    class="home-item-bar flex items-center justify-between gap-3 px-3.5 py-2.5 cursor-pointer select-none transition-colors hover:bg-muted/30"
                    @click="toggleExpand(item)"
                  >
                    <div class="flex items-center gap-2.5 min-w-0 flex-1">
                      <span
                        class="size-5 rounded text-[11px] font-mono font-medium flex items-center justify-center shrink-0 bg-muted text-muted-foreground"
                      >
                        {{ i + 1 }}
                      </span>
                      <span
                        class="font-medium text-sm text-foreground truncate max-w-[180px] sm:max-w-[260px]"
                      >
                        {{ item.title || item.query || "未命名分组" }}
                      </span>
                      <div class="flex items-center gap-1.5 shrink-0 flex-wrap">
                        <Badge
                          :variant="
                            item.kind === 'publication'
                              ? 'default'
                              : 'secondary'
                          "
                          class="text-[10px] px-1.5 py-0 h-4.5 font-normal"
                        >
                          {{ item.kind === "publication" ? "出版物" : "导航" }}
                        </Badge>
                        <Badge
                          variant="outline"
                          class="text-[10px] px-1.5 py-0 h-4.5 font-normal text-muted-foreground border-border/80"
                        >
                          {{ item.type === "search" ? "搜索" : "预设" }}
                        </Badge>
                        <code
                          class="text-[11px] font-mono bg-muted/80 text-muted-foreground px-1.5 py-0.5 rounded truncate max-w-[140px] sm:max-w-[200px]"
                        >
                          {{ item.query || "未设置" }}
                        </code>
                        <span
                          v-if="item.kind === 'publication'"
                          class="text-[11px] text-muted-foreground/80 font-normal hidden sm:inline"
                        >
                          · {{ countLabel(item) }}
                        </span>
                      </div>
                    </div>

                    <!-- Quick Actions -->
                    <div class="flex items-center gap-0.5 shrink-0" @click.stop>
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        :disabled="i === 0"
                        aria-label="分组上移"
                        title="上移"
                        @click="s.moveItem('groups', i, -1)"
                      >
                        <ChevronUp class="size-3.5" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        :disabled="i === s.layout.value.groups.length - 1"
                        aria-label="分组下移"
                        title="下移"
                        @click="s.moveItem('groups', i, 1)"
                      >
                        <ChevronDown class="size-3.5" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        class="text-muted-foreground hover:text-destructive hover:bg-destructive/10"
                        aria-label="删除分组"
                        title="删除"
                        @click="s.removeItem('groups', i)"
                      >
                        <Trash2 class="size-3.5" />
                      </Button>
                      <Separator orientation="vertical" class="h-3.5 mx-1" />
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        :aria-expanded="isExpanded(item)"
                        :title="isExpanded(item) ? '收起配置' : '展开编辑'"
                        @click="toggleExpand(item)"
                      >
                        <ChevronRight
                          class="size-3.5 text-muted-foreground transition-transform duration-200"
                          :class="{
                            'rotate-90 text-foreground': isExpanded(item),
                          }"
                        />
                      </Button>
                    </div>
                  </div>

                  <!-- Expanded Form -->
                  <div
                    v-if="isExpanded(item)"
                    class="border-t border-border bg-muted/20 px-4 py-3.5"
                  >
                    <div class="grid grid-cols-1 sm:grid-cols-12 gap-3">
                      <div class="sm:col-span-4 space-y-1.5">
                        <label class="text-xs font-medium text-muted-foreground"
                          >分组标题</label
                        >
                        <Input
                          v-model="item.title"
                          placeholder="分组标题 (如: 昨日热门)"
                          class="h-8 text-xs bg-background"
                          @update:model-value="s.homeDirty.value = true"
                        />
                      </div>
                      <div class="sm:col-span-2 space-y-1.5">
                        <label class="text-xs font-medium text-muted-foreground"
                          >栏目类型</label
                        >
                        <select
                          v-model="item.kind"
                          class="home-select w-full h-8 text-xs rounded-md border border-input bg-background px-2.5 text-foreground shadow-xs outline-none focus:border-ring focus:ring-1 focus:ring-ring"
                          @change="s.homeDirty.value = true"
                        >
                          <option value="publication">出版物</option>
                          <option value="navigation">导航</option>
                        </select>
                      </div>
                      <div class="sm:col-span-2 space-y-1.5">
                        <label class="text-xs font-medium text-muted-foreground"
                          >来源类型</label
                        >
                        <select
                          v-model="item.type"
                          class="home-select w-full h-8 text-xs rounded-md border border-input bg-background px-2.5 text-foreground shadow-xs outline-none focus:border-ring focus:ring-1 focus:ring-ring"
                          @change="handleTypeChange(item)"
                        >
                          <option value="preset">预设 (Preset)</option>
                          <option value="search">搜索 (Search)</option>
                        </select>
                      </div>
                      <div
                        :class="
                          item.kind === 'publication'
                            ? 'sm:col-span-2'
                            : 'sm:col-span-4'
                        "
                        class="space-y-1.5"
                      >
                        <label
                          class="text-xs font-medium text-muted-foreground"
                        >
                          {{
                            item.type === "preset" ? "预设名称" : "搜索表达式"
                          }}
                        </label>
                        <select
                          v-if="item.type === 'preset'"
                          v-model="item.query"
                          class="home-select w-full h-8 text-xs rounded-md border border-input bg-background px-2.5 text-foreground shadow-xs outline-none focus:border-ring focus:ring-1 focus:ring-ring font-mono"
                          @change="s.homeDirty.value = true"
                        >
                          <option v-if="!item.query" value="" disabled>
                            选择预设...
                          </option>
                          <option
                            v-else-if="!isKnownPreset(item.query)"
                            :value="item.query"
                          >
                            {{ item.query }} (自定义预设)
                          </option>
                          <optgroup
                            v-for="grp in PRESET_GROUPS"
                            :key="grp.label"
                            :label="grp.label"
                          >
                            <option
                              v-for="opt in grp.options"
                              :key="opt.value"
                              :value="opt.value"
                            >
                              {{ opt.label }}
                            </option>
                          </optgroup>
                        </select>
                        <Input
                          v-else
                          v-model="item.query"
                          placeholder="如: language:chinese"
                          class="h-8 text-xs bg-background font-mono"
                          @update:model-value="s.homeDirty.value = true"
                        />
                      </div>
                      <div
                        v-if="item.kind === 'publication'"
                        class="sm:col-span-2 space-y-1.5"
                      >
                        <label class="text-xs font-medium text-muted-foreground"
                          >预览条数</label
                        >
                        <Input
                          v-model.number="item.count"
                          type="number"
                          min="0"
                          placeholder="默认 10"
                          class="h-8 text-xs bg-background font-mono"
                          @update:model-value="s.homeDirty.value = true"
                        />
                      </div>
                    </div>
                  </div>
                </div>
              </div>

              <!-- Empty Groups State -->
              <div
                v-else
                class="flex flex-col items-center justify-center p-8 rounded-lg border border-dashed border-border bg-muted/10 text-center"
              >
                <Layers class="size-7 text-muted-foreground/60 mb-2" />
                <p class="text-xs font-medium text-foreground">
                  尚未添加任何分组栏目
                </p>
                <p class="text-[11px] text-muted-foreground mt-0.5 max-w-sm">
                  分组栏目模式会将每个栏目输出为一个横向滚动画廊流或子分类。
                </p>
                <div class="flex items-center gap-2 mt-3.5">
                  <Button
                    variant="outline"
                    size="sm"
                    class="h-8 text-xs"
                    @click="handleAddGroup('publication')"
                  >
                    <Plus class="size-3.5" />出版物分组
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    class="h-8 text-xs"
                    @click="handleAddGroup('navigation')"
                  >
                    <Plus class="size-3.5" />导航分组
                  </Button>
                </div>
              </div>
            </div>

            <!-- MODE B: 出版物流模式 -->
            <div
              v-else-if="s.homeMode.value === 'publications'"
              class="space-y-5 pt-1"
            >
              <!-- Top-level Publication Section -->
              <div class="space-y-2">
                <div class="flex items-center justify-between gap-3">
                  <div class="flex items-center gap-2">
                    <span class="text-xs font-medium text-foreground"
                      >顶层出版物</span
                    >
                    <Badge
                      v-if="s.layout.value.publication"
                      variant="secondary"
                      class="h-5 px-1.5 text-[11px] font-normal"
                    >
                      已配置
                    </Badge>
                    <Badge
                      v-else
                      variant="outline"
                      class="h-5 px-1.5 text-[11px] font-normal text-muted-foreground"
                    >
                      纯导航模式
                    </Badge>
                  </div>
                  <Button
                    v-if="!s.layout.value.publication"
                    variant="outline"
                    size="sm"
                    class="h-8 gap-1.5 text-xs"
                    @click="handleAddPublication"
                  >
                    <Plus class="size-3.5" />添加出版物
                  </Button>
                </div>

                <!-- Publication Item Card -->
                <div
                  v-if="s.layout.value.publication"
                  class="home-item-card rounded-lg border border-border bg-card overflow-hidden shadow-xs"
                >
                  <div
                    class="home-item-bar flex items-center justify-between gap-3 px-3.5 py-2.5 cursor-pointer select-none transition-colors hover:bg-muted/30"
                    @click="toggleExpand(s.layout.value.publication)"
                  >
                    <div class="flex items-center gap-2.5 min-w-0 flex-1">
                      <BookOpen class="size-4 text-primary shrink-0" />
                      <span
                        class="font-medium text-sm text-foreground truncate max-w-[200px] sm:max-w-[280px]"
                      >
                        {{
                          s.layout.value.publication.title ||
                          s.layout.value.publication.query ||
                          "首页出版物"
                        }}
                      </span>
                      <div class="flex items-center gap-1.5 shrink-0 flex-wrap">
                        <Badge
                          variant="outline"
                          class="text-[10px] px-1.5 py-0 h-4.5 font-normal text-muted-foreground border-border/80"
                        >
                          {{
                            s.layout.value.publication.type === "search"
                              ? "搜索"
                              : "预设"
                          }}
                        </Badge>
                        <code
                          class="text-[11px] font-mono bg-muted/80 text-muted-foreground px-1.5 py-0.5 rounded truncate max-w-[160px] sm:max-w-[220px]"
                        >
                          {{ s.layout.value.publication.query || "未设置" }}
                        </code>
                        <span
                          class="text-[11px] text-muted-foreground/80 font-normal"
                        >
                          · {{ countLabel(s.layout.value.publication) }}
                        </span>
                      </div>
                    </div>

                    <div class="flex items-center gap-1 shrink-0" @click.stop>
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        class="text-muted-foreground hover:text-destructive hover:bg-destructive/10"
                        aria-label="移除出版物"
                        title="移除出版物"
                        @click="s.removePublication"
                      >
                        <Trash2 class="size-3.5" />
                      </Button>
                      <Separator orientation="vertical" class="h-3.5 mx-1" />
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        :aria-expanded="isExpanded(s.layout.value.publication)"
                        :title="
                          isExpanded(s.layout.value.publication)
                            ? '收起配置'
                            : '展开编辑'
                        "
                        @click="toggleExpand(s.layout.value.publication)"
                      >
                        <ChevronRight
                          class="size-3.5 text-muted-foreground transition-transform duration-200"
                          :class="{
                            'rotate-90 text-foreground': isExpanded(
                              s.layout.value.publication,
                            ),
                          }"
                        />
                      </Button>
                    </div>
                  </div>

                  <!-- Expanded Publication Form -->
                  <div
                    v-if="isExpanded(s.layout.value.publication)"
                    class="border-t border-border bg-muted/20 px-4 py-3.5"
                  >
                    <div class="grid grid-cols-1 sm:grid-cols-12 gap-3">
                      <div class="sm:col-span-5 space-y-1.5">
                        <label class="text-xs font-medium text-muted-foreground"
                          >出版物标题</label
                        >
                        <Input
                          v-model="s.layout.value.publication.title"
                          placeholder="出版物标题 (如: 最新画廊)"
                          class="h-8 text-xs bg-background"
                          @update:model-value="s.homeDirty.value = true"
                        />
                      </div>
                      <div class="sm:col-span-2 space-y-1.5">
                        <label class="text-xs font-medium text-muted-foreground"
                          >来源类型</label
                        >
                        <select
                          v-model="s.layout.value.publication.type"
                          class="home-select w-full h-8 text-xs rounded-md border border-input bg-background px-2.5 text-foreground shadow-xs outline-none focus:border-ring focus:ring-1 focus:ring-ring"
                          @change="handleTypeChange(s.layout.value.publication)"
                        >
                          <option value="preset">预设 (Preset)</option>
                          <option value="search">搜索 (Search)</option>
                        </select>
                      </div>
                      <div class="sm:col-span-3 space-y-1.5">
                        <label
                          class="text-xs font-medium text-muted-foreground"
                        >
                          {{
                            s.layout.value.publication.type === "preset"
                              ? "预设名称"
                              : "搜索表达式"
                          }}
                        </label>
                        <select
                          v-if="s.layout.value.publication.type === 'preset'"
                          v-model="s.layout.value.publication.query"
                          class="home-select w-full h-8 text-xs rounded-md border border-input bg-background px-2.5 text-foreground shadow-xs outline-none focus:border-ring focus:ring-1 focus:ring-ring font-mono"
                          @change="s.homeDirty.value = true"
                        >
                          <option
                            v-if="!s.layout.value.publication.query"
                            value=""
                            disabled
                          >
                            选择预设...
                          </option>
                          <option
                            v-else-if="
                              !isKnownPreset(s.layout.value.publication.query)
                            "
                            :value="s.layout.value.publication.query"
                          >
                            {{ s.layout.value.publication.query }} (自定义预设)
                          </option>
                          <optgroup
                            v-for="grp in PRESET_GROUPS"
                            :key="grp.label"
                            :label="grp.label"
                          >
                            <option
                              v-for="opt in grp.options"
                              :key="opt.value"
                              :value="opt.value"
                            >
                              {{ opt.label }}
                            </option>
                          </optgroup>
                        </select>
                        <Input
                          v-else
                          v-model="s.layout.value.publication.query"
                          placeholder="如: language:chinese"
                          class="h-8 text-xs bg-background font-mono"
                          @update:model-value="s.homeDirty.value = true"
                        />
                      </div>
                      <div class="sm:col-span-2 space-y-1.5">
                        <label class="text-xs font-medium text-muted-foreground"
                          >预览条数</label
                        >
                        <Input
                          v-model.number="s.layout.value.publication.count"
                          type="number"
                          min="0"
                          placeholder="默认 10；0 关闭预览"
                          class="h-8 text-xs bg-background font-mono"
                          @update:model-value="s.homeDirty.value = true"
                        />
                      </div>
                    </div>
                  </div>
                </div>

                <!-- Empty Publication State -->
                <div
                  v-else
                  class="flex flex-col items-center justify-center p-7 rounded-lg border border-dashed border-border bg-muted/10 text-center"
                >
                  <BookOpen class="size-7 text-muted-foreground/60 mb-2" />
                  <p class="text-xs font-medium text-foreground">
                    当前为纯导航首页
                  </p>
                  <p class="text-[11px] text-muted-foreground mt-0.5">
                    未配置顶层出版物。首页仅输出顶层导航列表。
                  </p>
                  <Button
                    variant="outline"
                    size="sm"
                    class="h-8 text-xs mt-3"
                    @click="handleAddPublication"
                  >
                    <Plus class="size-3.5" />添加顶层出版物
                  </Button>
                </div>
              </div>

              <!-- Facets Subsection -->
              <div class="space-y-2.5 pt-2">
                <div class="flex items-center justify-between gap-3">
                  <div class="flex items-center gap-2">
                    <Filter class="size-3.5 text-muted-foreground" />
                    <span class="text-xs font-medium text-foreground"
                      >筛选集合 (Facets)</span
                    >
                    <Badge
                      variant="secondary"
                      class="h-5 px-1.5 text-[11px] font-mono font-normal"
                    >
                      {{ s.layout.value.facets.length }}
                    </Badge>
                    <span
                      class="text-[11px] text-muted-foreground hidden sm:inline"
                    >
                      {{
                        s.layout.value.publication
                          ? "· 筛选来源自动跟随上方顶层出版物"
                          : "· 添加顶层出版物后可启用筛选"
                      }}
                    </span>
                  </div>
                  <Button
                    variant="outline"
                    size="sm"
                    class="h-8 gap-1.5 text-xs"
                    :disabled="!s.canAddFacet()"
                    @click="handleAddFacet"
                  >
                    <Plus class="size-3.5" />添加筛选
                  </Button>
                </div>

                <!-- Facets List -->
                <div v-if="s.layout.value.facets.length > 0" class="space-y-2">
                  <div
                    v-for="(facet, i) in s.layout.value.facets"
                    :key="'facet-' + i"
                    class="flex items-center justify-between gap-3 px-3 py-2 rounded-lg border border-border bg-card shadow-xs"
                  >
                    <div class="flex items-center gap-2.5 flex-1 min-w-0">
                      <span
                        class="size-5 rounded text-[11px] font-mono font-medium flex items-center justify-center shrink-0 bg-muted text-muted-foreground"
                      >
                        {{ i + 1 }}
                      </span>
                      <Input
                        v-model="facet.title"
                        placeholder="筛选集合标题 (如: 分类)"
                        class="h-8 text-xs max-w-xs bg-background"
                        @update:model-value="s.homeDirty.value = true"
                      />
                      <span
                        class="text-[11px] text-muted-foreground hidden md:inline truncate"
                      >
                        跟随出版物：{{
                          s.layout.value.publication?.query || "未选择"
                        }}
                      </span>
                    </div>

                    <div class="flex items-center gap-0.5 shrink-0">
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        :disabled="i === 0"
                        aria-label="筛选上移"
                        title="上移"
                        @click="s.moveItem('facets', i, -1)"
                      >
                        <ChevronUp class="size-3.5" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        :disabled="i === s.layout.value.facets.length - 1"
                        aria-label="筛选下移"
                        title="下移"
                        @click="s.moveItem('facets', i, 1)"
                      >
                        <ChevronDown class="size-3.5" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        class="text-muted-foreground hover:text-destructive hover:bg-destructive/10"
                        aria-label="删除筛选"
                        title="删除"
                        @click="s.removeItem('facets', i)"
                      >
                        <Trash2 class="size-3.5" />
                      </Button>
                    </div>
                  </div>
                </div>

                <div
                  v-else-if="s.layout.value.publication"
                  class="p-4 rounded-lg border border-dashed border-border/70 text-center text-[11px] text-muted-foreground"
                >
                  未配置筛选集合。点击上方「添加筛选」可配置分类标签筛选组。
                </div>
              </div>
            </div>

            <!-- Unselected Fallback -->
            <div
              v-else
              class="p-8 rounded-lg border border-dashed border-border text-center text-xs text-muted-foreground"
            >
              请在上方右上角选择「出版物流模式」或「分组栏目模式」以继续编排。
            </div>
          </section>
        </CardContent>
      </Card>
    </template>
  </div>
</template>

<script setup>
import { ref, computed, watch, nextTick, onMounted } from "vue";
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
import { t } from "@/i18n.js";

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

onMounted(() => {
  s.loadHome();
});

function countLabel(section) {
  if (!section || section.count === 0) return t("home_editor.no_preview");
  if (section.count == null || section.count === "") return t("home_editor.default_items");
  return t("home_editor.n_items", { count: section.count });
}

const homeReplaceWarning = computed(() => {
  const persisted = s.home.value?.sections || [];
  if (
    replacesAlternateMode(persisted, s.homeMode.value) &&
    s.homeMode.value === "groups"
  )
    return t("home_editor.replace_groups_warning");
  if (
    replacesAlternateMode(persisted, s.homeMode.value) &&
    s.homeMode.value === "publications"
  )
    return t("home_editor.replace_pubs_warning");
  return "";
});

const homeFacetWarning = computed(() =>
  s.homeMode.value === "publications" &&
  s.layout.value.facets.length > 0 &&
  !canUseFacets(s.layout.value.publication)
    ? t("home_editor.facet_unsupported_warning")
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

const PRESET_GROUPS = computed(() => [
  {
    label: t("home_editor.preset_gallery"),
    options: [
      { value: "latest", label: t("home_editor.latest") },
      { value: "popular", label: t("home_editor.popular") },
      { value: "watched", label: t("home_editor.watched") },
      { value: "favorites", label: t("home_editor.favorites") },
      { value: "archives", label: t("home_editor.archives") },
    ],
  },
  {
    label: t("home_editor.preset_toplist"),
    options: [
      { value: "toplist:yesterday", label: t("home_editor.toplist_yesterday") },
      { value: "toplist:month", label: t("home_editor.toplist_month") },
      { value: "toplist:year", label: t("home_editor.toplist_year") },
      { value: "toplist:alltime", label: t("home_editor.toplist_alltime") },
    ],
  },
  {
    label: t("home_editor.preset_favcats"),
    options: Array.from({ length: 10 }, (_, i) => ({
      value: `favorites:${i}`,
      label: t("home_editor.favcat_item", { id: i }),
    })),
  },
]);

function isKnownPreset(q) {
  return PRESET_GROUPS.value.some((g) => g.options.some((o) => o.value === q));
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
        <small>{{ t("home_editor.header_small") }}</small>
        <h1>{{ t("home_editor.title") }}</h1>
        <p>{{ t("home_editor.desc") }}</p>
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
          <Save class="size-4" />{{ t("home_editor.save") }}
        </Button>
      </div>
    </div>

    <!-- Legacy Readonly Fallback -->
    <Card v-if="s.home.value?.legacy" class="panel">
      <CardHeader class="panel-header">
        <CardTitle>{{ t("home_editor.readonly_title") }}</CardTitle>
        <CardDescription
          >{{ t("home_editor.readonly_desc", { path: s.home.value.legacy.path }) }}</CardDescription
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
        <CardTitle>{{ t("home_editor.error_title") }}</CardTitle>
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
            >{{ t("home_editor.repair_title") }}</AlertTitle
          >
          <AlertDescription class="text-xs text-muted-foreground">
            {{ s.home.value.validation_error }} · {{ t("home_editor.repair_desc", { error: "" }).replace(/^ · /, "") }}
          </AlertDescription>
          <div class="repair-drafts mt-3 grid grid-cols-1 sm:grid-cols-2 gap-2">
            <div
              class="p-2.5 rounded-md border border-border/80 bg-background/80"
            >
              <strong class="text-xs font-medium block">{{ t("home_editor.pub_draft") }}</strong>
              <span class="text-xs text-muted-foreground block mt-0.5">
                {{
                  s.layout.value.publication
                    ? s.layout.value.publication.title ||
                      s.layout.value.publication.query
                    : t("home_editor.no_top_pub")
                }}
                · {{ t("home_editor.n_facets", { count: s.layout.value.facets.length }) }}
              </span>
            </div>
            <div
              class="p-2.5 rounded-md border border-border/80 bg-background/80"
            >
              <strong class="text-xs font-medium block">
                {{ t("home_editor.groups_draft", { count: s.layout.value.groups.length }) }}
              </strong>
              <span class="text-xs text-muted-foreground block mt-0.5">
                {{
                  s.layout.value.groups
                    .map((g) => g.title || g.query)
                    .slice(0, 3)
                    .join(", ")
                }}
                {{ s.layout.value.groups.length > 3 ? t("home_editor.etc") : "" }}
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
          {{ homeFacetWarning }} {{ t("home_editor.facet_warning_suffix") }}
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
                    {{ t("home_editor.nav_title") }}
                  </h2>
                  <Badge
                    variant="secondary"
                    class="h-5 px-1.5 text-[11px] font-mono font-normal"
                  >
                    {{ s.layout.value.navigation.length }}
                  </Badge>
                </div>
                <p class="text-xs text-muted-foreground mt-1">
                  {{ t("home_editor.nav_desc") }}
                </p>
              </div>
              <div class="flex items-center gap-2 shrink-0">
                <Button
                  variant="outline"
                  size="sm"
                  class="h-8 gap-1.5 text-xs"
                  @click="handleAddNavigation"
                >
                  <Plus class="size-3.5" />{{ t("home_editor.add_nav") }}
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
                      {{ nav.title || nav.query || t("home_editor.unnamed_nav") }}
                    </span>
                    <div class="flex items-center gap-1.5 shrink-0 flex-wrap">
                      <Badge
                        variant="outline"
                        class="text-[10px] px-1.5 py-0 h-4.5 font-normal text-muted-foreground border-border/80"
                      >
                        {{ nav.type === "search" ? t("home_editor.type_search") : t("home_editor.type_preset") }}
                      </Badge>
                      <code
                        class="text-[11px] font-mono bg-muted/80 text-muted-foreground px-1.5 py-0.5 rounded truncate max-w-[160px] sm:max-w-[220px]"
                      >
                        {{ nav.query || t("home_editor.not_set") }}
                      </code>
                    </div>
                  </div>

                  <!-- Quick Actions (Never hidden in fold) -->
                  <div class="flex items-center gap-0.5 shrink-0" @click.stop>
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      :disabled="i === 0"
                      :aria-label="t('home_editor.reorder_up')"
                      :title="t('home_editor.reorder_up')"
                      @click="s.moveItem('navigation', i, -1)"
                    >
                      <ChevronUp class="size-3.5" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      :disabled="i === s.layout.value.navigation.length - 1"
                      :aria-label="t('home_editor.reorder_down')"
                      :title="t('home_editor.reorder_down')"
                      @click="s.moveItem('navigation', i, 1)"
                    >
                      <ChevronDown class="size-3.5" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      class="text-muted-foreground hover:text-destructive hover:bg-destructive/10"
                      :aria-label="t('home_editor.delete')"
                      :title="t('home_editor.delete')"
                      @click="s.removeItem('navigation', i)"
                    >
                      <Trash2 class="size-3.5" />
                    </Button>
                    <Separator orientation="vertical" class="h-3.5 mx-1" />
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      :aria-expanded="isExpanded(nav)"
                      :title="isExpanded(nav) ? t('home_editor.collapse') : t('home_editor.expand')"
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
                        >{{ t("home_editor.nav_title_label") }}</label
                      >
                      <Input
                        v-model="nav.title"
                        :placeholder="t('home_editor.nav_title_placeholder')"
                        class="h-8 text-xs bg-background"
                        @update:model-value="s.homeDirty.value = true"
                      />
                    </div>
                    <div class="sm:col-span-3 space-y-1.5">
                      <label class="text-xs font-medium text-muted-foreground"
                        >{{ t("home_editor.source_type") }}</label
                      >
                      <select
                        v-model="nav.type"
                        class="home-select w-full h-8 text-xs rounded-md border border-input bg-background px-2.5 text-foreground shadow-xs outline-none focus:border-ring focus:ring-1 focus:ring-ring"
                        @change="handleTypeChange(nav)"
                      >
                        <option value="preset">{{ t("home_editor.type_preset") }}</option>
                        <option value="search">{{ t("home_editor.type_search") }}</option>
                      </select>
                    </div>
                    <div class="sm:col-span-4 space-y-1.5">
                      <label class="text-xs font-medium text-muted-foreground">
                        {{ nav.type === "preset" ? t("home_editor.preset_name") : t("home_editor.search_query") }}
                      </label>
                      <select
                        v-if="nav.type === 'preset'"
                        v-model="nav.query"
                        class="home-select w-full h-8 text-xs rounded-md border border-input bg-background px-2.5 text-foreground shadow-xs outline-none focus:border-ring focus:ring-1 focus:ring-ring font-mono"
                        @change="s.homeDirty.value = true"
                      >
                        <option v-if="!nav.query" value="" disabled>
                          {{ t("home_editor.select_preset") }}
                        </option>
                        <option
                          v-else-if="!isKnownPreset(nav.query)"
                          :value="nav.query"
                        >
                          {{ t("home_editor.custom_preset", { query: nav.query }) }}
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
                        :placeholder="t('home_editor.search_placeholder')"
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
                {{ t("home_editor.empty_nav_title") }}
              </p>
              <p class="text-[11px] text-muted-foreground mt-0.5">
                {{ t("home_editor.empty_nav_desc") }}
              </p>
            </div>
          </section>

          <Separator class="my-6" />

          <!-- ================= SECTION 2: 内容布局 ================= -->
          <section class="home-section space-y-4">
            <!-- Section Header & Segmented Mode Switcher -->
            <div
              class="flex flex-col md:flex-row md:items-center justify-between gap-3 pb-1"
            >
              <div>
                <div class="flex items-center gap-2">
                  <Layers class="size-4 text-primary shrink-0" />
                  <h2 class="text-base font-semibold tracking-tight whitespace-nowrap">
                    {{ t("home_editor.content_layout") }}
                  </h2>
                </div>
                <p class="text-xs text-muted-foreground mt-1">
                  {{ t("home_editor.content_layout_desc") }}
                </p>
              </div>

              <!-- Segmented Control Mode Switch -->
              <div
                class="segmented-control shrink-0"
                role="tablist"
                :aria-label="t('home_editor.content_layout')"
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
                  <span>{{ t("home_editor.mode_publications_btn") }}</span>
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
                  <span>{{ t("home_editor.mode_groups_btn") }}</span>
                </button>
              </div>
            </div>

            <!-- MODE A: 分组栏目模式 -->
            <div v-if="s.homeMode.value === 'groups'" class="space-y-3 pt-1">
              <!-- Toolbar for Groups -->
              <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
                <div class="flex items-center gap-2 flex-wrap">
                  <span class="text-xs font-medium text-foreground whitespace-nowrap"
                    >{{ t("home_editor.groups_list") }}</span
                  >
                  <Badge
                    variant="secondary"
                    class="h-5 px-1.5 text-[11px] font-mono font-normal"
                  >
                    {{ s.layout.value.groups.length }}
                  </Badge>
                  <span
                    class="text-[11px] text-muted-foreground hidden lg:inline"
                  >
                    {{ t("home_editor.groups_list_desc") }}
                  </span>
                </div>
                <div class="flex items-center gap-2 shrink-0">
                  <Button
                    variant="outline"
                    size="sm"
                    class="h-8 gap-1.5 text-xs"
                    @click="handleAddGroup('publication')"
                  >
                    <Plus class="size-3.5" />{{ t("home_editor.add_pub_group") }}
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    class="h-8 gap-1.5 text-xs"
                    @click="handleAddGroup('navigation')"
                  >
                    <Plus class="size-3.5" />{{ t("home_editor.add_nav_group") }}
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
                        {{ item.title || item.query || t("home_editor.unnamed_group") }}
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
                          {{ item.kind === "publication" ? t("home_editor.kind_publication") : t("home_editor.kind_navigation") }}
                        </Badge>
                        <Badge
                          variant="outline"
                          class="text-[10px] px-1.5 py-0 h-4.5 font-normal text-muted-foreground border-border/80"
                        >
                          {{ item.type === "search" ? t("home_editor.type_search") : t("home_editor.type_preset") }}
                        </Badge>
                        <code
                          class="text-[11px] font-mono bg-muted/80 text-muted-foreground px-1.5 py-0.5 rounded truncate max-w-[140px] sm:max-w-[200px]"
                        >
                          {{ item.query || t("home_editor.not_set") }}
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
                        :aria-label="t('home_editor.reorder_up')"
                        :title="t('home_editor.reorder_up')"
                        @click="s.moveItem('groups', i, -1)"
                      >
                        <ChevronUp class="size-3.5" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        :disabled="i === s.layout.value.groups.length - 1"
                        :aria-label="t('home_editor.reorder_down')"
                        :title="t('home_editor.reorder_down')"
                        @click="s.moveItem('groups', i, 1)"
                      >
                        <ChevronDown class="size-3.5" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        class="text-muted-foreground hover:text-destructive hover:bg-destructive/10"
                        :aria-label="t('home_editor.delete')"
                        :title="t('home_editor.delete')"
                        @click="s.removeItem('groups', i)"
                      >
                        <Trash2 class="size-3.5" />
                      </Button>
                      <Separator orientation="vertical" class="h-3.5 mx-1" />
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        :aria-expanded="isExpanded(item)"
                        :title="isExpanded(item) ? t('home_editor.collapse') : t('home_editor.expand')"
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
                          >{{ t("home_editor.group_title") }}</label
                        >
                        <Input
                          v-model="item.title"
                          :placeholder="t('home_editor.group_title_placeholder')"
                          class="h-8 text-xs bg-background"
                          @update:model-value="s.homeDirty.value = true"
                        />
                      </div>
                      <div class="sm:col-span-2 space-y-1.5">
                        <label class="text-xs font-medium text-muted-foreground"
                          >{{ t("home_editor.section_type") }}</label
                        >
                        <select
                          v-model="item.kind"
                          class="home-select w-full h-8 text-xs rounded-md border border-input bg-background px-2.5 text-foreground shadow-xs outline-none focus:border-ring focus:ring-1 focus:ring-ring"
                          @change="s.homeDirty.value = true"
                        >
                          <option value="publication">{{ t("home_editor.kind_publication") }}</option>
                          <option value="navigation">{{ t("home_editor.kind_navigation") }}</option>
                        </select>
                      </div>
                      <div class="sm:col-span-2 space-y-1.5">
                        <label class="text-xs font-medium text-muted-foreground"
                          >{{ t("home_editor.source_type") }}</label
                        >
                        <select
                          v-model="item.type"
                          class="home-select w-full h-8 text-xs rounded-md border border-input bg-background px-2.5 text-foreground shadow-xs outline-none focus:border-ring focus:ring-1 focus:ring-ring"
                          @change="handleTypeChange(item)"
                        >
                          <option value="preset">{{ t("home_editor.type_preset") }}</option>
                          <option value="search">{{ t("home_editor.type_search") }}</option>
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
                            item.type === "preset" ? t("home_editor.preset_name") : t("home_editor.search_query")
                          }}
                        </label>
                        <select
                          v-if="item.type === 'preset'"
                          v-model="item.query"
                          class="home-select w-full h-8 text-xs rounded-md border border-input bg-background px-2.5 text-foreground shadow-xs outline-none focus:border-ring focus:ring-1 focus:ring-ring font-mono"
                          @change="s.homeDirty.value = true"
                        >
                          <option v-if="!item.query" value="" disabled>
                            {{ t("home_editor.select_preset") }}
                          </option>
                          <option
                            v-else-if="!isKnownPreset(item.query)"
                            :value="item.query"
                          >
                            {{ t("home_editor.custom_preset", { query: item.query }) }}
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
                          :placeholder="t('home_editor.search_placeholder')"
                          class="h-8 text-xs bg-background font-mono"
                          @update:model-value="s.homeDirty.value = true"
                        />
                      </div>
                      <div
                        v-if="item.kind === 'publication'"
                        class="sm:col-span-2 space-y-1.5"
                      >
                        <label class="text-xs font-medium text-muted-foreground"
                          >{{ t("home_editor.preview_count") }}</label
                        >
                        <Input
                          v-model.number="item.count"
                          type="number"
                          min="0"
                          :placeholder="t('home_editor.preview_count_placeholder')"
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
                  {{ t("home_editor.empty_groups_title") }}
                </p>
                <p class="text-[11px] text-muted-foreground mt-0.5 max-w-sm">
                  {{ t("home_editor.empty_groups_desc") }}
                </p>
                <div class="flex items-center gap-2 mt-3.5">
                  <Button
                    variant="outline"
                    size="sm"
                    class="h-8 text-xs"
                    @click="handleAddGroup('publication')"
                  >
                    <Plus class="size-3.5" />{{ t("home_editor.add_pub_group") }}
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    class="h-8 text-xs"
                    @click="handleAddGroup('navigation')"
                  >
                    <Plus class="size-3.5" />{{ t("home_editor.add_nav_group") }}
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
                      >{{ t("home_editor.top_publication") }}</span
                    >
                    <Badge
                      v-if="s.layout.value.publication"
                      variant="secondary"
                      class="h-5 px-1.5 text-[11px] font-normal"
                    >
                      {{ t("home_editor.configured") }}
                    </Badge>
                    <Badge
                      v-else
                      variant="outline"
                      class="h-5 px-1.5 text-[11px] font-normal text-muted-foreground"
                    >
                      {{ t("home_editor.pure_nav_mode") }}
                    </Badge>
                  </div>
                  <Button
                    v-if="!s.layout.value.publication"
                    variant="outline"
                    size="sm"
                    class="h-8 gap-1.5 text-xs"
                    @click="handleAddPublication"
                  >
                    <Plus class="size-3.5" />{{ t("home_editor.add_top_pub") }}
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
                          t("home_editor.default_pub_title")
                        }}
                      </span>
                      <div class="flex items-center gap-1.5 shrink-0 flex-wrap">
                        <Badge
                          variant="outline"
                          class="text-[10px] px-1.5 py-0 h-4.5 font-normal text-muted-foreground border-border/80"
                        >
                          {{
                            s.layout.value.publication.type === "search"
                              ? t("home_editor.type_search")
                              : t("home_editor.type_preset")
                          }}
                        </Badge>
                        <code
                          class="text-[11px] font-mono bg-muted/80 text-muted-foreground px-1.5 py-0.5 rounded truncate max-w-[160px] sm:max-w-[220px]"
                        >
                          {{ s.layout.value.publication.query || t("home_editor.not_set") }}
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
                        :aria-label="t('common.remove')"
                        :title="t('common.remove')"
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
                            ? t('home_editor.collapse')
                            : t('home_editor.expand')
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
                          >{{ t("home_editor.pub_title") }}</label
                        >
                        <Input
                          v-model="s.layout.value.publication.title"
                          :placeholder="t('home_editor.pub_title_placeholder')"
                          class="h-8 text-xs bg-background"
                          @update:model-value="s.homeDirty.value = true"
                        />
                      </div>
                      <div class="sm:col-span-2 space-y-1.5">
                        <label class="text-xs font-medium text-muted-foreground"
                          >{{ t("home_editor.source_type") }}</label
                        >
                        <select
                          v-model="s.layout.value.publication.type"
                          class="home-select w-full h-8 text-xs rounded-md border border-input bg-background px-2.5 text-foreground shadow-xs outline-none focus:border-ring focus:ring-1 focus:ring-ring"
                          @change="handleTypeChange(s.layout.value.publication)"
                        >
                          <option value="preset">{{ t("home_editor.type_preset") }}</option>
                          <option value="search">{{ t("home_editor.type_search") }}</option>
                        </select>
                      </div>
                      <div class="sm:col-span-3 space-y-1.5">
                        <label
                          class="text-xs font-medium text-muted-foreground"
                        >
                          {{
                            s.layout.value.publication.type === "preset"
                              ? t("home_editor.preset_name")
                              : t("home_editor.search_query")
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
                            {{ t("home_editor.select_preset") }}
                          </option>
                          <option
                            v-else-if="
                              !isKnownPreset(s.layout.value.publication.query)
                            "
                            :value="s.layout.value.publication.query"
                          >
                            {{ t("home_editor.custom_preset", { query: s.layout.value.publication.query }) }}
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
                          :placeholder="t('home_editor.search_placeholder')"
                          class="h-8 text-xs bg-background font-mono"
                          @update:model-value="s.homeDirty.value = true"
                        />
                      </div>
                      <div class="sm:col-span-2 space-y-1.5">
                        <label class="text-xs font-medium text-muted-foreground"
                          >{{ t("home_editor.preview_count") }}</label
                        >
                        <Input
                          v-model.number="s.layout.value.publication.count"
                          type="number"
                          min="0"
                          :placeholder="t('home_editor.preview_count_pub_placeholder')"
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
                    {{ t("home_editor.pure_nav_title") }}
                  </p>
                  <p class="text-[11px] text-muted-foreground mt-0.5">
                    {{ t("home_editor.pure_nav_desc") }}
                  </p>
                  <Button
                    variant="outline"
                    size="sm"
                    class="h-8 text-xs mt-3"
                    @click="handleAddPublication"
                  >
                    <Plus class="size-3.5" />{{ t("home_editor.add_top_pub_btn") }}
                  </Button>
                </div>
              </div>

              <!-- Facets Subsection -->
              <div class="space-y-2.5 pt-2">
                <div class="flex items-center justify-between gap-3">
                  <div class="flex items-center gap-2">
                    <Filter class="size-3.5 text-muted-foreground" />
                    <span class="text-xs font-medium text-foreground"
                      >{{ t("home_editor.facets_title") }}</span
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
                          ? t("home_editor.facets_follow_pub")
                          : t("home_editor.facets_require_pub")
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
                    <Plus class="size-3.5" />{{ t("home_editor.add_facet") }}
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
                        :placeholder="t('home_editor.facet_title_placeholder')"
                        class="h-8 text-xs max-w-xs bg-background"
                        @update:model-value="s.homeDirty.value = true"
                      />
                      <span
                        class="text-[11px] text-muted-foreground hidden md:inline truncate"
                      >
                        {{ t("home_editor.follow_pub") }}{{
                          s.layout.value.publication?.query || t("home_editor.not_selected")
                        }}
                      </span>
                    </div>

                    <div class="flex items-center gap-0.5 shrink-0">
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        :disabled="i === 0"
                        :aria-label="t('home_editor.reorder_up')"
                        :title="t('home_editor.reorder_up')"
                        @click="s.moveItem('facets', i, -1)"
                      >
                        <ChevronUp class="size-3.5" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        :disabled="i === s.layout.value.facets.length - 1"
                        :aria-label="t('home_editor.reorder_down')"
                        :title="t('home_editor.reorder_down')"
                        @click="s.moveItem('facets', i, 1)"
                      >
                        <ChevronDown class="size-3.5" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        class="text-muted-foreground hover:text-destructive hover:bg-destructive/10"
                        :aria-label="t('home_editor.delete')"
                        :title="t('home_editor.delete')"
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
                  {{ t("home_editor.empty_facets_desc") }}
                </div>
              </div>
            </div>

            <!-- Unselected Fallback -->
            <div
              v-else
              class="p-8 rounded-lg border border-dashed border-border text-center text-xs text-muted-foreground"
            >
              {{ t("home_editor.choose_mode_hint") }}
            </div>
          </section>
        </CardContent>
      </Card>
    </template>
  </div>
</template>

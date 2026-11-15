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
  AlertTriangle,
  RotateCcw,
  Check,
  Home,
  PanelsTopLeft,
  Pencil,
  FileText,
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
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { useApp } from "@/state.js";
import { canUseFacets, replacesAlternateMode, isValidSlug } from "@/home-layout.js";
import { t } from "@/i18n.js";

const s = useApp();

// Active page computed
const activePage = computed(() => {
  return s.pages.value.find((p) => p.id === s.activePageId.value) || s.pages.value[0] || { id: "home", title: "Home", mode: "groups" };
});

const currentPageMode = computed(() => {
  return activePage.value?.mode || s.homeMode.value || "publications";
});

function getPageDisplayTitle(page) {
  if (!page) return "";
  if (page.id === "home") {
    if (!page.title || page.title === "Home" || page.title === "首页") {
      return t("home_editor.home_page_title");
    }
  }
  return page.title || page.id;
}

function getPageTitle(pageId) {
  if (!pageId) return "";
  const p = s.pages.value.find((x) => x.id === pageId);
  if (p) return getPageDisplayTitle(p);
  if (pageId === "home") return t("home_editor.home_page_title");
  return pageId;
}

// Add Page Dialog State
const resetDefaultsOpen = ref(false);
const addPageOpen = ref(false);
const addPageForm = ref({ title: "", id: "", mode: "publications" });
const addPageError = ref("");
const addPageAutoSlug = ref(true);

function openAddPageModal() {
  addPageForm.value = { title: "", id: "", mode: "publications" };
  addPageError.value = "";
  addPageAutoSlug.value = true;
  addPageOpen.value = true;
}

function handleAddPageTitleInput(val) {
  addPageForm.value.title = val;
  if (addPageAutoSlug.value) {
    const slug = val
      .trim()
      .toLowerCase()
      .replace(/[^a-z0-9_-]/g, "-")
      .replace(/-+/g, "-")
      .replace(/^-|-$/g, "");
    if (slug) {
      addPageForm.value.id = slug;
    }
  }
}

function confirmAddPage() {
  const title = addPageForm.value.title.trim();
  const id = addPageForm.value.id.trim().toLowerCase();
  if (!id || !isValidSlug(id)) {
    addPageError.value = t("home_editor.page_id_invalid");
    return;
  }
  if (s.pages.value.some((p) => p.id === id)) {
    addPageError.value = t("home_editor.page_id_exists");
    return;
  }
  const ok = s.addPage({ id, title: title || id, mode: addPageForm.value.mode });
  if (ok) {
    addPageOpen.value = false;
  } else {
    addPageError.value = t("home_editor.page_id_exists");
  }
}

// Edit Page Meta Dialog State
const editPageOpen = ref(false);
const editPageForm = ref({ origId: "", title: "", id: "" });
const editPageError = ref("");

function openEditPageModal(page) {
  editPageForm.value = {
    origId: page.id,
    title: page.id === "home" ? getPageDisplayTitle(page) : (page.title || page.id),
    id: page.id,
  };
  editPageError.value = "";
  editPageOpen.value = true;
}

function confirmEditPage() {
  const title = editPageForm.value.title.trim();
  const newId = editPageForm.value.id.trim().toLowerCase();
  if (editPageForm.value.origId !== "home") {
    if (!newId || !isValidSlug(newId)) {
      editPageError.value = t("home_editor.page_id_invalid");
      return;
    }
    if (newId !== editPageForm.value.origId && s.pages.value.some((p) => p.id === newId)) {
      editPageError.value = t("home_editor.page_id_exists");
      return;
    }
  }
  const ok = s.updatePageMeta(editPageForm.value.origId, {
    title: title || newId,
    id: editPageForm.value.origId === "home" ? "home" : newId,
  });
  if (ok) {
    editPageOpen.value = false;
  }
}

// Delete Page Dialog State
const deletePageOpen = ref(false);
const deletePageTarget = ref(null);

function openDeletePageConfirm(page) {
  if (page.id === "home") return;
  deletePageTarget.value = page;
  deletePageOpen.value = true;
}

function confirmDeletePage() {
  if (!deletePageTarget.value) return;
  s.removePage(deletePageTarget.value.id);
  deletePageOpen.value = false;
  deletePageTarget.value = null;
}

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

// When active page changes, clear expanded items
watch(
  () => s.activePageId.value,
  () => {
    expandedItems.value = new Set();
  },
);

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

const activePersistedSections = computed(() => {
  const activeId = s.activePageId.value;
  if (!activeId) return [];
  const p = s.home.value?.pages?.find((x) => x.id === activeId);
  if (p) return p.sections || [];
  if (activeId === "home") return s.home.value?.sections || [];
  return [];
});

const homeReplaceWarning = computed(() => {
  const persisted = activePersistedSections.value;
  const currentMode = currentPageMode.value;
  if (
    replacesAlternateMode(persisted, currentMode) &&
    currentMode === "groups"
  )
    return t("home_editor.replace_groups_warning");
  if (
    replacesAlternateMode(persisted, currentMode) &&
    currentMode === "publications"
  )
    return t("home_editor.replace_pubs_warning");
  return "";
});

const homeFacetWarning = computed(() =>
  currentPageMode.value === "publications" &&
  s.layout.value.facets.length > 0 &&
  !canUseFacets(s.layout.value.publication)
    ? t("home_editor.facet_unsupported_warning")
    : "",
);

function handleNavTypeChange(item) {
  if (item.type === "page") {
    if (!item.query || !s.pages.value.some((p) => p.id === item.query)) {
      item.query = s.pages.value[0]?.id || "home";
    }
  } else if (item.type === "preset") {
    if (!isKnownPreset(item.query)) {
      item.query = "latest";
    }
  } else if (item.type === "search") {
    if (item.query && (isKnownPreset(item.query) || s.pages.value.some((p) => p.id === item.query))) {
      item.query = "";
    }
  }
  s.homeDirty.value = true;
}

function handleNavTargetChange(nav) {
  s.homeDirty.value = true;
}

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

function getNavTargetTitle(nav) {
  if (!nav) return "";
  if (nav.type === "page") {
    return getPageTitle(nav.query);
  }
  if (nav.type === "preset") {
    for (const grp of PRESET_GROUPS.value) {
      const found = grp.options.find((o) => o.value === nav.query);
      if (found) return found.label;
    }
    return nav.query || "";
  }
  if (nav.type === "search") {
    return nav.query || "";
  }
  return getPageTitle(nav.query);
}

function handleTypeChange(item) {
  if (item.type === "preset" && !isKnownPreset(item.query)) {
    item.query = "latest";
  }
  s.homeDirty.value = true;
}

function handleGroupKindChange(item) {
  if (item.kind === "navigation") {
    if (!Array.isArray(item.items) || item.items.length === 0) {
      item.items = [
        {
          title: "",
          type: "page",
          query: s.pages.value[0]?.id || "home",
        },
      ];
    }
  } else if (item.kind === "publication") {
    if (!item.query && item.items && item.items.length > 0) {
      item.type = "preset";
      item.query = "latest";
    }
    if (item.count == null) item.count = 10;
  }
  s.homeDirty.value = true;
}
</script>

<template>
  <div class="home-editor-view flex flex-col gap-6">
    <!-- Page Heading -->
    <div class="heading">
      <div>
        <small>{{ t("home_editor.header_small") }}</small>
        <h1>{{ t("home_editor.title") }}</h1>
        <p>{{ t("home_editor.desc") }}</p>
      </div>
      <div class="flex items-center gap-2">
        <Button
          variant="outline"
          class="gap-1.5"
          :disabled="s.busy.value"
          @click="resetDefaultsOpen = true"
        >
          <RotateCcw class="size-4" />{{ t("home_editor.reset_defaults") }}
        </Button>
        <Button
          class="gap-1.5"
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
    <template v-else-if="s.pages.value.length > 0 || s.home.value?.sections">
      <!-- Validation / Repair Banner -->
      <Alert
        v-if="s.home.value?.validation_error"
        variant="warning"
      >
        <AlertTriangle class="size-4 shrink-0" />
        <div class="flex-1 flex flex-col gap-1">
          <AlertTitle class="text-sm font-semibold"
            >{{ t("home_editor.repair_title") }}</AlertTitle
          >
          <AlertDescription class="text-xs">
            {{ s.home.value.validation_error }} · {{ t("home_editor.repair_desc", { error: "" }).replace(/^ · /, "") }}
          </AlertDescription>
        </div>
      </Alert>

      <!-- Warning Banners -->
      <Alert
        v-if="homeReplaceWarning"
        variant="warning"
        class="py-2.5"
      >
        <AlertTriangle class="size-4 shrink-0" />
        <AlertDescription class="text-xs">
          {{ homeReplaceWarning }}
        </AlertDescription>
      </Alert>

      <Alert v-if="homeFacetWarning" variant="destructive" class="py-2.5">
        <AlertTriangle class="size-4 text-destructive shrink-0" />
        <AlertDescription class="text-xs">
          {{ homeFacetWarning }} {{ t("home_editor.facet_warning_suffix") }}
        </AlertDescription>
      </Alert>

      <!-- Master-Detail Layout: Left Internal Sidebar & Right Workbench -->
      <div class="grid grid-cols-1 lg:grid-cols-12 gap-6 items-start">
        <!-- ================= LEFT INTERNAL SIDEBAR: 页面列表 ================= -->
        <div class="lg:col-span-4 xl:col-span-3 flex flex-col gap-3">
          <Card class="panel">
            <CardHeader class="p-4 pb-3 flex flex-row items-center justify-between gap-2 border-b border-border/60">
              <div class="flex items-center gap-2">
                <PanelsTopLeft class="size-4 text-primary" />
                <span class="font-semibold text-sm">{{ t("home_editor.page_list_title") }}</span>
                <Badge variant="secondary" class="h-5 px-1.5 text-[11px] font-mono font-normal">
                  {{ s.pages.value.length }}
                </Badge>
              </div>
              <Button
                variant="outline"
                size="sm"
                class="h-7 gap-1 text-xs px-2"
                @click="openAddPageModal"
              >
                <Plus class="size-3.5" />{{ t("home_editor.add_page") }}
              </Button>
            </CardHeader>
            <CardContent class="p-2 flex flex-col gap-1">
              <div
                v-for="page in s.pages.value"
                :key="page.id"
                class="group relative flex items-center justify-between gap-2 px-3 py-2.5 rounded-xl cursor-pointer transition-colors text-xs select-none"
                :class="page.id === s.activePageId.value ? 'bg-accent text-accent-foreground font-medium shadow-xs' : 'hover:bg-muted/60 text-muted-foreground hover:text-foreground'"
                @click="s.selectActivePage(page.id)"
              >
                <div class="flex items-center gap-2.5 min-w-0 flex-1">
                  <Home v-if="page.id === 'home'" class="size-4 shrink-0 text-primary" />
                  <PanelsTopLeft v-else class="size-4 shrink-0 text-muted-foreground group-hover:text-foreground" />
                  <div class="min-w-0 flex-1">
                    <div class="truncate text-sm font-medium leading-tight">
                      {{ getPageDisplayTitle(page) }}
                    </div>
                    <div class="flex items-center gap-1.5 mt-0.5">
                      <span class="font-mono text-[10px] text-muted-foreground/80">
                        {{ page.id === 'home' ? '/' : '/page/' + page.id }}
                      </span>
                      <Badge variant="outline" class="text-[9px] px-1 py-0 h-3.5 font-normal">
                        {{ page.mode === 'publications' ? t("home_editor.mode_publications") : t("home_editor.mode_sections") }}
                      </Badge>
                    </div>
                  </div>
                </div>

                <!-- Action buttons for custom pages -->
                <div v-if="page.id !== 'home'" class="flex items-center gap-0.5 shrink-0 opacity-80 group-hover:opacity-100" @click.stop>
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    class="text-muted-foreground hover:text-foreground"
                    :title="t('home_editor.edit_page_meta')"
                    @click="openEditPageModal(page)"
                  >
                    <Pencil class="size-3" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    class="text-muted-foreground hover:text-destructive hover:bg-destructive/10"
                    :title="t('home_editor.delete_page')"
                    @click="openDeletePageConfirm(page)"
                  >
                    <Trash2 class="size-3" />
                  </Button>
                </div>
              </div>
            </CardContent>
          </Card>
        </div>

        <!-- ================= RIGHT PANE: 当前页面工作台 ================= -->
        <div class="lg:col-span-8 xl:col-span-9 flex flex-col gap-6">
          <Card class="panel home-workbench">
            <CardHeader class="p-5 pb-3 border-b border-border/60">
              <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
                <div class="flex items-center gap-2.5">
                  <Home v-if="activePage.id === 'home'" class="size-5 text-primary shrink-0" />
                  <PanelsTopLeft v-else class="size-5 text-primary shrink-0" />
                  <div>
                    <div class="flex items-center gap-2">
                      <h2 class="text-lg font-bold tracking-tight">
                        {{ getPageDisplayTitle(activePage) }}
                      </h2>
                      <Badge variant="secondary" class="font-mono text-xs">
                        {{ activePage.id === 'home' ? '/' : '/page/' + activePage.id }}
                      </Badge>
                    </div>
                  </div>
                </div>

                <div class="flex items-center gap-2">
                  <Button
                    v-if="activePage.id !== 'home'"
                    variant="ghost"
                    size="sm"
                    class="h-8 gap-1 text-xs"
                    @click="openEditPageModal(activePage)"
                  >
                    <Pencil class="size-3.5" />{{ t("home_editor.edit_page_meta") }}
                  </Button>
                </div>
              </div>
            </CardHeader>

            <CardContent class="p-5 sm:p-6 flex flex-col gap-6">
              <!-- ================= SECTION 1: 顶层导航 ================= -->
              <section class="home-section flex flex-col gap-4">
                <div class="flex items-start justify-between gap-4 pb-1">
                  <div>
                    <div class="flex items-center gap-2">
                      <Compass class="size-4 text-primary" />
                      <h3 class="text-base font-semibold tracking-tight">
                        {{ t("home_editor.nav_title") }}
                      </h3>
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
                  class="flex flex-col gap-2.5"
                >
                  <div
                    v-for="(nav, i) in s.layout.value.navigation"
                    :key="'nav-' + i"
                    class="home-item-card rounded-xl border border-border bg-card transition-[border-color,box-shadow] duration-150 overflow-hidden shadow-xs hover:border-border/80"
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
                          {{ nav.title || getNavTargetTitle(nav) || t("home_editor.unnamed_nav") }}
                        </span>
                        <div class="flex items-center gap-1.5 shrink-0 flex-wrap">
                          <Badge
                            variant="outline"
                            class="text-[10px] px-1.5 py-0 h-4.5 font-normal text-muted-foreground border-border/80"
                          >
                            {{ nav.type === "page" ? t("home_editor.target_page") : (nav.type === "search" ? t("home_editor.type_search") : t("home_editor.type_preset")) }}
                          </Badge>
                          <code
                            class="text-[11px] font-mono bg-muted/80 text-muted-foreground px-1.5 py-0.5 rounded truncate max-w-[160px] sm:max-w-[220px]"
                          >
                            {{ nav.type === "page" ? (nav.query === "home" ? "/" : "/page/" + nav.query) : (nav.query || t("home_editor.not_set")) }}
                          </code>
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
                        <div class="sm:col-span-3 flex flex-col gap-1.5">
                          <label class="text-xs font-medium text-foreground">
                            {{ t("home_editor.source_type") }}
                          </label>
                          <select
                            v-model="nav.type"
                            class="home-select w-full h-8 text-xs rounded-xl border border-input bg-background dark:bg-input/30 px-2.5 text-foreground shadow-xs outline-none focus:border-ring focus:ring-1 focus:ring-ring"
                            @change="handleNavTypeChange(nav)"
                          >
                            <option value="page">{{ t("home_editor.target_page") }}</option>
                            <option value="preset">{{ t("home_editor.type_preset") }}</option>
                            <option value="search">{{ t("home_editor.type_search") }}</option>
                          </select>
                        </div>

                        <div class="sm:col-span-4 flex flex-col gap-1.5">
                          <label class="text-xs font-medium text-foreground">
                            {{ nav.type === "page" ? t("home_editor.target_page") : (nav.type === "preset" ? t("home_editor.preset_name") : t("home_editor.search_query")) }}
                          </label>
                          <!-- Target: Page -->
                          <select
                            v-if="nav.type === 'page'"
                            v-model="nav.query"
                            class="home-select w-full h-8 text-xs rounded-xl border border-input bg-background dark:bg-input/30 px-2.5 text-foreground shadow-xs outline-none focus:border-ring focus:ring-1 focus:ring-ring font-mono"
                            @change="s.homeDirty.value = true"
                          >
                            <option
                              v-for="p in s.pages.value"
                              :key="p.id"
                              :value="p.id"
                            >
                              {{ p.title || p.id }} ({{ p.id === 'home' ? '/' : '/page/' + p.id }})
                            </option>
                          </select>

                          <!-- Target: Preset -->
                          <select
                            v-else-if="nav.type === 'preset'"
                            v-model="nav.query"
                            class="home-select w-full h-8 text-xs rounded-xl border border-input bg-background dark:bg-input/30 px-2.5 text-foreground shadow-xs outline-none focus:border-ring focus:ring-1 focus:ring-ring font-mono"
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

                          <!-- Target: Search -->
                          <Input
                            v-else
                            v-model="nav.query"
                            :placeholder="t('home_editor.search_placeholder')"
                            class="h-8 text-xs bg-background dark:bg-input/30 font-mono"
                            @update:model-value="s.homeDirty.value = true"
                          />
                        </div>

                        <div class="sm:col-span-5 flex flex-col gap-1.5">
                          <label class="text-xs font-medium text-foreground">
                            {{ t("home_editor.nav_title_label") }}
                          </label>
                          <Input
                            v-model="nav.title"
                            :placeholder="getNavTargetTitle(nav) || t('home_editor.nav_title_placeholder')"
                            class="h-8 text-xs bg-background dark:bg-input/30"
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
                  class="flex flex-col items-center justify-center p-7 rounded-2xl border border-dashed border-border bg-muted/10 text-center"
                >
                  <Compass class="size-7 text-muted-foreground/60 mb-2" />
                  <p class="text-xs font-medium text-foreground">
                    {{ t("home_editor.empty_nav_title") }}
                  </p>
                  <p class="text-[11px] text-muted-foreground mt-1 max-w-sm">
                    {{ t("home_editor.empty_nav_desc") }}
                  </p>
                  <Button
                    variant="outline"
                    size="sm"
                    class="h-7 text-xs mt-3 gap-1"
                    @click="handleAddNavigation"
                  >
                    <Plus class="size-3" />{{ t("home_editor.add_nav") }}
                  </Button>
                </div>
              </section>

              <Separator class="my-6" />

              <!-- ================= SECTION 2: 内容布局 ================= -->
              <section class="home-section flex flex-col gap-4">
                <!-- Section Header & Segmented Mode Switcher -->
                <div
                  class="flex flex-col md:flex-row md:items-center justify-between gap-3 pb-1"
                >
                  <div>
                    <div class="flex items-center gap-2">
                      <Layers class="size-4 text-primary shrink-0" />
                      <h3 class="text-base font-semibold tracking-tight whitespace-nowrap">
                        {{ t("home_editor.content_layout") }}
                      </h3>
                    </div>
                    <p class="text-xs text-muted-foreground mt-1">
                      {{ t("home_editor.content_layout_desc") }}
                    </p>
                  </div>

                  <!-- ToggleGroup Mode Switch (Luma-compliant) -->
                  <ToggleGroup
                    type="single"
                    :model-value="currentPageMode"
                    class="bg-muted/60 p-1 border border-border/60 rounded-full shrink-0 gap-1"
                    @update:model-value="(val) => val && s.selectHomeMode(val)"
                  >
                    <ToggleGroupItem
                      value="publications"
                      class="px-3 py-1.5 text-xs font-medium rounded-full gap-1.5 border border-transparent data-[state=on]:border-border/60 data-[state=on]:shadow-xs"
                    >
                      <BookOpen class="size-3.5" />
                      <span>{{ t("home_editor.mode_publications_btn") }}</span>
                    </ToggleGroupItem>
                    <ToggleGroupItem
                      value="groups"
                      class="px-3 py-1.5 text-xs font-medium rounded-full gap-1.5 border border-transparent data-[state=on]:border-border/60 data-[state=on]:shadow-xs"
                    >
                      <Layers class="size-3.5" />
                      <span>{{ t("home_editor.mode_groups_btn") }}</span>
                    </ToggleGroupItem>
                  </ToggleGroup>
                </div>

                <!-- MODE A: 分组栏目模式 -->
                <div v-if="currentPageMode === 'groups'" class="flex flex-col gap-3 pt-1">
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

                  <!-- Groups Item List -->
                  <div
                    v-if="s.layout.value.groups.length > 0"
                    class="flex flex-col gap-2.5"
                  >
                    <div
                      v-for="(item, i) in s.layout.value.groups"
                      :key="'grp-' + i"
                      class="home-item-card rounded-xl border border-border bg-card transition-[border-color,box-shadow] duration-150 overflow-hidden shadow-xs hover:border-border/80"
                      :class="{
                        'ring-1 ring-primary/20 border-primary/30': isExpanded(item),
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
                            class="font-medium text-sm text-foreground truncate max-w-[200px] sm:max-w-[260px]"
                          >
                            {{ item.title || item.query || t("home_editor.unnamed_section") }}
                          </span>
                          <div class="flex items-center gap-1.5 shrink-0 flex-wrap">
                            <Badge
                              variant="outline"
                              class="text-[10px] px-1.5 py-0 h-4.5 font-normal border-border/80"
                              :class="
                                item.kind === 'publication'
                                  ? 'text-primary border-primary/30 bg-primary/5'
                                  : 'text-muted-foreground border-border/80 bg-muted/40'
                              "
                            >
                              {{
                                item.kind === "publication"
                                  ? t("home_editor.kind_publication")
                                  : t("home_editor.kind_navigation")
                              }}
                            </Badge>

                            <template v-if="item.kind === 'publication'">
                              <Badge
                                variant="secondary"
                                class="text-[10px] px-1.5 py-0 h-4.5 font-normal text-muted-foreground"
                              >
                                {{ countLabel(item) }}
                              </Badge>
                              <code
                                class="text-[11px] font-mono bg-muted/80 text-muted-foreground px-1.5 py-0.5 rounded truncate max-w-[140px] sm:max-w-[180px]"
                              >
                                {{ item.query || t("home_editor.not_set") }}
                              </code>
                            </template>
                            <template v-else>
                              <Badge
                                variant="secondary"
                                class="text-[10px] px-1.5 py-0 h-4.5 font-normal text-muted-foreground font-mono"
                              >
                                {{ t("home_editor.n_subnavs", { count: (item.items || []).length }) }}
                              </Badge>
                            </template>
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
                        <!-- PUBLICATION GROUP FORM -->
                        <div
                          v-if="item.kind === 'publication'"
                          class="grid grid-cols-1 sm:grid-cols-12 gap-3"
                        >
                          <div class="sm:col-span-4 flex flex-col gap-1.5">
                            <label class="text-xs font-medium text-foreground"
                              >{{ t("home_editor.group_title") }}</label
                            >
                            <Input
                              v-model="item.title"
                              :placeholder="t('home_editor.pub_group_title_placeholder')"
                              class="h-8 text-xs bg-background dark:bg-input/30"
                              @update:model-value="s.homeDirty.value = true"
                            />
                          </div>
                          <div class="sm:col-span-2 flex flex-col gap-1.5">
                            <label class="text-xs font-medium text-foreground"
                              >{{ t("home_editor.source_type") }}</label
                            >
                            <select
                              v-model="item.type"
                              class="home-select w-full h-8 text-xs rounded-xl border border-input bg-background dark:bg-input/30 px-2.5 text-foreground shadow-xs outline-none focus:border-ring focus:ring-1 focus:ring-ring"
                              @change="handleTypeChange(item)"
                            >
                              <option value="preset">{{ t("home_editor.type_preset") }}</option>
                              <option value="search">{{ t("home_editor.type_search") }}</option>
                            </select>
                          </div>
                          <div class="sm:col-span-4 flex flex-col gap-1.5">
                            <label class="text-xs font-medium text-foreground">
                              {{ item.type === "preset" ? t("home_editor.preset_name") : t("home_editor.search_query") }}
                            </label>
                            <select
                              v-if="item.type === 'preset'"
                              v-model="item.query"
                              class="home-select w-full h-8 text-xs rounded-xl border border-input bg-background dark:bg-input/30 px-2.5 text-foreground shadow-xs outline-none focus:border-ring focus:ring-1 focus:ring-ring font-mono"
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
                              class="h-8 text-xs bg-background dark:bg-input/30 font-mono"
                              @update:model-value="s.homeDirty.value = true"
                            />
                          </div>
                          <div class="sm:col-span-2 flex flex-col gap-1.5">
                            <label class="text-xs font-medium text-foreground"
                              >{{ t("home_editor.preview_count") }}</label
                            >
                            <Input
                              v-model.number="item.count"
                              type="number"
                              min="0"
                              :placeholder="t('home_editor.preview_count_placeholder')"
                              class="h-8 text-xs bg-background dark:bg-input/30 font-mono"
                              @update:model-value="s.homeDirty.value = true"
                            />
                          </div>
                        </div>

                        <!-- NAVIGATION GROUP FORM -->
                        <div v-else class="flex flex-col gap-3.5">
                          <div class="grid grid-cols-1 sm:grid-cols-12 gap-3">
                            <div class="sm:col-span-9 flex flex-col gap-1.5">
                              <label class="text-xs font-medium text-foreground"
                                >{{ t("home_editor.group_title") }}</label
                              >
                              <Input
                                v-model="item.title"
                                :placeholder="t('home_editor.nav_group_title_placeholder')"
                                class="h-8 text-xs bg-background dark:bg-input/30 font-medium"
                                @update:model-value="s.homeDirty.value = true"
                              />
                            </div>
                            <div class="sm:col-span-3 flex flex-col gap-1.5">
                              <label class="text-xs font-medium text-foreground"
                                >{{ t("home_editor.section_type") }}</label
                              >
                              <select
                                v-model="item.kind"
                                class="home-select w-full h-8 text-xs rounded-xl border border-input bg-background dark:bg-input/30 px-2.5 text-foreground shadow-xs outline-none focus:border-ring focus:ring-1 focus:ring-ring"
                                @change="handleGroupKindChange(item)"
                              >
                                <option value="navigation">{{ t("home_editor.kind_navigation") }}</option>
                                <option value="publication">{{ t("home_editor.kind_publication") }}</option>
                              </select>
                            </div>
                          </div>

                          <!-- Sub Navigations Panel -->
                          <div class="rounded-xl border border-border/80 bg-background/60 dark:bg-input/10 p-3 flex flex-col gap-2.5">
                            <div class="flex items-center justify-between pb-0.5">
                              <div class="flex items-center gap-2">
                                <Compass class="size-3.5 text-primary" />
                                <span class="text-xs font-medium text-foreground">
                                  {{ t("home_editor.nav_items_list") }}
                                </span>
                                <Badge
                                  variant="secondary"
                                  class="h-4.5 px-1.5 text-[10px] font-mono font-normal"
                                >
                                  {{ (item.items || []).length }}
                                </Badge>
                              </div>
                              <Button
                                variant="outline"
                                size="sm"
                                class="h-7 px-2.5 text-xs gap-1"
                                @click="s.addGroupNavigationItem(i)"
                              >
                                <Plus class="size-3" />
                                <span>{{ t("home_editor.add_nav_item") }}</span>
                              </Button>
                            </div>

                            <!-- Sub Nav List -->
                            <div
                              v-if="item.items && item.items.length > 0"
                              class="flex flex-col gap-2"
                            >
                              <div
                                v-for="(subItem, subIdx) in item.items"
                                :key="'subnav-' + i + '-' + subIdx"
                                class="flex flex-col sm:flex-row sm:items-center gap-2 p-2 rounded-xl border border-border bg-card/80 text-xs"
                              >
                                <span
                                  class="size-4.5 rounded text-[10px] font-mono font-medium flex items-center justify-center shrink-0 bg-muted text-muted-foreground self-start sm:self-center"
                                >
                                  {{ subIdx + 1 }}
                                </span>

                                <!-- Target Type Select -->
                                <div class="w-full sm:w-28 shrink-0">
                                  <select
                                    v-model="subItem.type"
                                    class="home-select w-full h-7 text-xs rounded-xl border border-input bg-background dark:bg-input/30 px-2 text-foreground shadow-xs outline-none focus:border-ring focus:ring-1 focus:ring-ring"
                                    @change="handleNavTypeChange(subItem)"
                                  >
                                    <option value="page">{{ t("home_editor.target_page") }}</option>
                                    <option value="preset">{{ t("home_editor.type_preset") }}</option>
                                    <option value="search">{{ t("home_editor.type_search") }}</option>
                                  </select>
                                </div>

                                <!-- Target Value Control -->
                                <div class="w-full sm:w-44 md:w-52 shrink-0">
                                  <!-- If page -->
                                  <select
                                    v-if="subItem.type === 'page'"
                                    v-model="subItem.query"
                                    class="home-select w-full h-7 text-xs rounded-xl border border-input bg-background dark:bg-input/30 px-2 text-foreground shadow-xs outline-none focus:border-ring focus:ring-1 focus:ring-ring font-mono"
                                    @change="s.homeDirty.value = true"
                                  >
                                    <option
                                      v-for="p in s.pages.value"
                                      :key="p.id"
                                      :value="p.id"
                                    >
                                      {{ p.title || p.id }} ({{ p.id === 'home' ? '/' : '/page/' + p.id }})
                                    </option>
                                  </select>

                                  <!-- If preset -->
                                  <select
                                    v-else-if="subItem.type === 'preset'"
                                    v-model="subItem.query"
                                    class="home-select w-full h-7 text-xs rounded-xl border border-input bg-background dark:bg-input/30 px-2 text-foreground shadow-xs outline-none focus:border-ring focus:ring-1 focus:ring-ring font-mono"
                                    @change="s.homeDirty.value = true"
                                  >
                                    <option v-if="!subItem.query" value="" disabled>
                                      {{ t("home_editor.select_preset") }}
                                    </option>
                                    <option
                                      v-else-if="!isKnownPreset(subItem.query)"
                                      :value="subItem.query"
                                    >
                                      {{ t("home_editor.custom_preset", { query: subItem.query }) }}
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

                                  <!-- If search -->
                                  <Input
                                    v-else
                                    v-model="subItem.query"
                                    :placeholder="t('home_editor.search_placeholder')"
                                    class="h-7 text-xs bg-background dark:bg-input/30 font-mono"
                                    @update:model-value="s.homeDirty.value = true"
                                  />
                                </div>

                                <!-- Navigation Title -->
                                <div class="flex-1 min-w-[120px]">
                                  <Input
                                    v-model="subItem.title"
                                    :placeholder="getNavTargetTitle(subItem) || t('home_editor.nav_item_title_placeholder')"
                                    class="h-7 text-xs bg-background dark:bg-input/30"
                                    @update:model-value="s.homeDirty.value = true"
                                  />
                                </div>

                                <!-- Actions -->
                                <div class="flex items-center gap-0.5 self-end sm:self-center shrink-0">
                                  <Button
                                    variant="ghost"
                                    size="icon-xs"
                                    :disabled="subIdx === 0"
                                    :aria-label="t('home_editor.reorder_up')"
                                    :title="t('home_editor.reorder_up')"
                                    @click="s.moveGroupNavigationItem(i, subIdx, -1)"
                                  >
                                    <ChevronUp class="size-3" />
                                  </Button>
                                  <Button
                                    variant="ghost"
                                    size="icon-xs"
                                    :disabled="subIdx === item.items.length - 1"
                                    :aria-label="t('home_editor.reorder_down')"
                                    :title="t('home_editor.reorder_down')"
                                    @click="s.moveGroupNavigationItem(i, subIdx, 1)"
                                  >
                                    <ChevronDown class="size-3" />
                                  </Button>
                                  <Button
                                    variant="ghost"
                                    size="icon-xs"
                                    class="text-muted-foreground hover:text-destructive hover:bg-destructive/10"
                                    :aria-label="t('home_editor.delete')"
                                    :title="t('home_editor.delete')"
                                    @click="s.removeGroupNavigationItem(i, subIdx)"
                                  >
                                    <Trash2 class="size-3" />
                                  </Button>
                                </div>
                              </div>
                            </div>

                            <div
                              v-else
                              class="text-center py-3 text-[11px] text-muted-foreground/70"
                            >
                              {{ t("home_editor.empty_nav_items_desc") }}
                            </div>
                          </div>
                        </div>
                      </div>
                    </div>
                  </div>

                  <!-- Empty Groups State -->
                  <div
                    v-else
                    class="flex flex-col items-center justify-center p-8 rounded-2xl border border-dashed border-border bg-muted/10 text-center"
                  >
                    <Layers class="size-7 text-muted-foreground/60 mb-2" />
                    <p class="text-xs font-medium text-foreground">
                      {{ t("home_editor.empty_groups_title") }}
                    </p>
                    <p class="text-[11px] text-muted-foreground mt-1 max-w-sm">
                      {{ t("home_editor.empty_groups_desc") }}
                    </p>
                    <div class="flex items-center gap-2 mt-3.5">
                      <Button
                        variant="outline"
                        size="sm"
                        class="h-7 text-xs gap-1"
                        @click="handleAddGroup('publication')"
                      >
                        <Plus class="size-3" />{{ t("home_editor.add_pub_group") }}
                      </Button>
                      <Button
                        variant="outline"
                        size="sm"
                        class="h-7 text-xs gap-1"
                        @click="handleAddGroup('navigation')"
                      >
                        <Plus class="size-3" />{{ t("home_editor.add_nav_group") }}
                      </Button>
                    </div>
                  </div>
                </div>

                <!-- MODE B: 出版物流模式 -->
                <div
                  v-else-if="currentPageMode === 'publications'"
                  class="flex flex-col gap-5 pt-1"
                >
                  <!-- Top-level Publication Section -->
                  <div class="flex flex-col gap-2">
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
                      class="home-item-card rounded-xl border border-border bg-card overflow-hidden shadow-xs"
                    >
                      <div
                        class="home-item-bar flex items-center justify-between gap-3 px-3.5 py-2.5 cursor-pointer select-none transition-colors hover:bg-muted/30"
                        @click="toggleExpand(s.layout.value.publication)"
                      >
                        <div class="flex items-center gap-2.5 min-w-0 flex-1">
                          <BookOpen class="size-4 text-primary shrink-0" />
                          <span class="font-medium text-sm text-foreground truncate max-w-[200px] sm:max-w-[280px]">
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
                              class="text-[11px] font-mono bg-muted/80 text-muted-foreground px-1.5 py-0.5 rounded truncate max-w-[140px] sm:max-w-[180px]"
                            >
                              {{ s.layout.value.publication.query || t("home_editor.not_set") }}
                            </code>
                            <Badge
                              variant="secondary"
                              class="text-[10px] px-1.5 py-0 h-4.5 font-normal text-muted-foreground"
                            >
                              {{ countLabel(s.layout.value.publication) }}
                            </Badge>
                          </div>
                        </div>

                        <!-- Actions -->
                        <div class="flex items-center gap-0.5 shrink-0" @click.stop>
                          <Button
                            variant="ghost"
                            size="icon-xs"
                            class="text-muted-foreground hover:text-destructive hover:bg-destructive/10"
                            :aria-label="t('home_editor.delete')"
                            :title="t('home_editor.delete')"
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
                                'rotate-90 text-foreground': isExpanded(s.layout.value.publication),
                              }"
                            />
                          </Button>
                        </div>
                      </div>

                      <!-- Expanded Form -->
                      <div
                        v-if="isExpanded(s.layout.value.publication)"
                        class="border-t border-border bg-muted/20 px-4 py-3.5"
                      >
                        <div class="grid grid-cols-1 sm:grid-cols-12 gap-3">
                          <div class="sm:col-span-4 flex flex-col gap-1.5">
                            <label class="text-xs font-medium text-foreground"
                              >{{ t("home_editor.pub_title") }}</label
                            >
                            <Input
                              v-model="s.layout.value.publication.title"
                              :placeholder="t('home_editor.pub_title_placeholder')"
                              class="h-8 text-xs bg-background dark:bg-input/30"
                              @update:model-value="s.homeDirty.value = true"
                            />
                          </div>
                          <div class="sm:col-span-2 flex flex-col gap-1.5">
                            <label class="text-xs font-medium text-foreground"
                              >{{ t("home_editor.source_type") }}</label
                            >
                            <select
                              v-model="s.layout.value.publication.type"
                              class="home-select w-full h-8 text-xs rounded-xl border border-input bg-background dark:bg-input/30 px-2.5 text-foreground shadow-xs outline-none focus:border-ring focus:ring-1 focus:ring-ring"
                              @change="handleTypeChange(s.layout.value.publication)"
                            >
                              <option value="preset">{{ t("home_editor.type_preset") }}</option>
                              <option value="search">{{ t("home_editor.type_search") }}</option>
                            </select>
                          </div>
                          <div class="sm:col-span-4 flex flex-col gap-1.5">
                            <label class="text-xs font-medium text-foreground">
                              {{
                                s.layout.value.publication.type === "preset"
                                  ? t("home_editor.preset_name")
                                  : t("home_editor.search_query")
                              }}
                            </label>
                            <select
                              v-if="s.layout.value.publication.type === 'preset'"
                              v-model="s.layout.value.publication.query"
                              class="home-select w-full h-8 text-xs rounded-xl border border-input bg-background dark:bg-input/30 px-2.5 text-foreground shadow-xs outline-none focus:border-ring focus:ring-1 focus:ring-ring font-mono"
                              @change="s.homeDirty.value = true"
                            >
                              <option v-if="!s.layout.value.publication.query" value="" disabled>
                                {{ t("home_editor.select_preset") }}
                              </option>
                              <option
                                v-else-if="!isKnownPreset(s.layout.value.publication.query)"
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
                              class="h-8 text-xs bg-background dark:bg-input/30 font-mono"
                              @update:model-value="s.homeDirty.value = true"
                            />
                          </div>
                          <div class="sm:col-span-2 flex flex-col gap-1.5">
                            <label class="text-xs font-medium text-foreground"
                              >{{ t("home_editor.preview_count") }}</label
                            >
                            <Input
                              v-model.number="s.layout.value.publication.count"
                              type="number"
                              min="0"
                              :placeholder="t('home_editor.preview_count_pub_placeholder')"
                              class="h-8 text-xs bg-background dark:bg-input/30 font-mono"
                              @update:model-value="s.homeDirty.value = true"
                            />
                          </div>
                        </div>
                      </div>
                    </div>

                    <!-- Pure Navigation Fallback Card -->
                    <div
                      v-else
                      class="flex flex-col sm:flex-row sm:items-center justify-between gap-3 p-4 rounded-xl border border-dashed border-border/70 bg-muted/5"
                    >
                      <div>
                        <span class="text-xs font-medium text-foreground block">
                          {{ t("home_editor.pure_nav_title") }}
                        </span>
                        <span class="text-[11px] text-muted-foreground block mt-0.5">
                          {{ t("home_editor.pure_nav_desc") }}
                        </span>
                      </div>
                      <Button
                        variant="outline"
                        size="sm"
                        class="h-8 text-xs gap-1.5 shrink-0"
                        @click="handleAddPublication"
                      >
                        <Plus class="size-3.5" />{{ t("home_editor.add_top_pub_btn") }}
                      </Button>
                    </div>
                  </div>

                  <!-- Facets Section -->
                  <div class="flex flex-col gap-2 pt-2">
                    <div class="flex items-center justify-between gap-3">
                      <div class="flex items-center gap-2">
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
                    <div
                      v-if="s.layout.value.facets.length > 0"
                      class="flex flex-col gap-2"
                    >
                      <div
                        v-for="(facet, i) in s.layout.value.facets"
                        :key="'facet-' + i"
                        class="flex items-center justify-between gap-3 p-2.5 rounded-xl border border-border bg-card shadow-xs text-xs"
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
                            class="h-8 text-xs bg-background dark:bg-input/30 max-w-xs"
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
                      class="p-4 rounded-xl border border-dashed border-border/70 text-center text-[11px] text-muted-foreground"
                    >
                      {{ t("home_editor.empty_facets_desc") }}
                    </div>
                  </div>
                </div>

                <!-- Unselected Fallback -->
                <div
                  v-else
                  class="p-8 rounded-2xl border border-dashed border-border text-center text-xs text-muted-foreground"
                >
                  {{ t("home_editor.choose_mode_hint") }}
                </div>
              </section>
            </CardContent>
          </Card>
        </div>
      </div>
    </template>

    <!-- ================= ADD PAGE DIALOG ================= -->
    <Dialog v-model:open="addPageOpen">
      <DialogContent class="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{{ t("home_editor.add_page") }}</DialogTitle>
          <DialogDescription>
            {{ t("home_editor.add_page_desc") }}
          </DialogDescription>
        </DialogHeader>

        <div class="flex flex-col gap-4 py-2">
          <div v-if="addPageError" class="p-2.5 rounded-xl bg-destructive/10 border border-destructive/20 text-destructive text-xs">
            {{ addPageError }}
          </div>

          <div class="flex flex-col gap-1.5">
            <label class="text-xs font-medium text-foreground">
              {{ t("home_editor.page_title_label") }}
            </label>
            <Input
              v-model="addPageForm.title"
              :placeholder="t('home_editor.page_title_placeholder')"
              class="h-8 text-xs bg-background dark:bg-input/30"
              @update:model-value="handleAddPageTitleInput"
            />
          </div>

          <div class="flex flex-col gap-1.5">
            <div class="flex items-center justify-between">
              <label class="text-xs font-medium text-foreground">
                {{ t("home_editor.page_id_label") }}
              </label>
              <span class="text-[10px] text-muted-foreground font-mono">
                /page/{{ addPageForm.id || 'slug' }}
              </span>
            </div>
            <Input
              v-model="addPageForm.id"
              :placeholder="t('home_editor.page_id_placeholder')"
              class="h-8 text-xs bg-background dark:bg-input/30 font-mono"
              @input="addPageAutoSlug = false"
            />
          </div>

          <div class="flex flex-col gap-1.5">
            <label class="text-xs font-medium text-foreground">
              {{ t("home_editor.page_mode_label") }}
            </label>
            <ToggleGroup
              type="single"
              v-model="addPageForm.mode"
              class="w-full grid grid-cols-2 bg-muted/60 p-1 border border-border/60 rounded-full gap-1"
            >
              <ToggleGroupItem
                value="publications"
                class="w-full py-1.5 text-xs font-medium rounded-full gap-1.5 border border-transparent data-[state=on]:bg-background data-[state=on]:text-foreground data-[state=on]:border-border/60 data-[state=on]:shadow-xs"
              >
                <BookOpen class="size-3.5" />
                <span>{{ t("home_editor.mode_publications") }}</span>
              </ToggleGroupItem>
              <ToggleGroupItem
                value="groups"
                class="w-full py-1.5 text-xs font-medium rounded-full gap-1.5 border border-transparent data-[state=on]:bg-background data-[state=on]:text-foreground data-[state=on]:border-border/60 data-[state=on]:shadow-xs"
              >
                <Layers class="size-3.5" />
                <span>{{ t("home_editor.mode_sections") }}</span>
              </ToggleGroupItem>
            </ToggleGroup>
          </div>
        </div>

        <DialogFooter class="gap-2 sm:gap-0">
          <Button variant="outline" size="sm" @click="addPageOpen = false">
            {{ t("common.cancel") }}
          </Button>
          <Button size="sm" :disabled="!addPageForm.title.trim() || !addPageForm.id.trim()" @click="confirmAddPage">
            {{ t("common.confirm") }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- ================= EDIT PAGE META DIALOG ================= -->
    <Dialog v-model:open="editPageOpen">
      <DialogContent class="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{{ t("home_editor.edit_page_meta") }}</DialogTitle>
        </DialogHeader>

        <div class="flex flex-col gap-4 py-2">
          <div v-if="editPageError" class="p-2.5 rounded-xl bg-destructive/10 border border-destructive/20 text-destructive text-xs">
            {{ editPageError }}
          </div>

          <div class="flex flex-col gap-1.5">
            <label class="text-xs font-medium text-foreground">
              {{ t("home_editor.page_title_label") }}
            </label>
            <Input
              v-model="editPageForm.title"
              :placeholder="t('home_editor.page_title_placeholder')"
              class="h-8 text-xs bg-background dark:bg-input/30"
            />
          </div>

          <div v-if="editPageForm.origId !== 'home'" class="flex flex-col gap-1.5">
            <div class="flex items-center justify-between">
              <label class="text-xs font-medium text-foreground">
                {{ t("home_editor.page_id_label") }}
              </label>
              <span class="text-[10px] text-muted-foreground font-mono">
                /page/{{ editPageForm.id }}
              </span>
            </div>
            <Input
              v-model="editPageForm.id"
              class="h-8 text-xs bg-background dark:bg-input/30 font-mono"
            />
          </div>
        </div>

        <DialogFooter class="gap-2 sm:gap-0">
          <Button variant="outline" size="sm" @click="editPageOpen = false">
            {{ t("common.cancel") }}
          </Button>
          <Button size="sm" :disabled="!editPageForm.title.trim() || (editPageForm.origId !== 'home' && !editPageForm.id.trim())" @click="confirmEditPage">
            {{ t("common.save") }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- ================= DELETE PAGE CONFIRM DIALOG ================= -->
    <Dialog v-model:open="deletePageOpen">
      <DialogContent class="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{{ t("home_editor.delete_page_title", { title: getPageDisplayTitle(deletePageTarget) }) }}</DialogTitle>
          <DialogDescription>
            {{ t("home_editor.delete_page_desc") }}
          </DialogDescription>
        </DialogHeader>

        <DialogFooter class="gap-2 sm:gap-0">
          <Button variant="outline" size="sm" @click="deletePageOpen = false">
            {{ t("common.cancel") }}
          </Button>
          <Button variant="destructive" size="sm" @click="confirmDeletePage">
            {{ t("common.delete") }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- ================= RESET TO DEFAULTS CONFIRM DIALOG ================= -->
    <Dialog v-model:open="resetDefaultsOpen">
      <DialogContent class="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{{ t("home_editor.reset_defaults_title") }}</DialogTitle>
          <DialogDescription>
            {{ t("home_editor.reset_defaults_desc") }}
          </DialogDescription>
        </DialogHeader>

        <DialogFooter class="gap-2 sm:gap-0">
          <Button variant="outline" size="sm" @click="resetDefaultsOpen = false">
            {{ t("common.cancel") }}
          </Button>
          <Button
            variant="destructive"
            size="sm"
            @click="
              s.resetHomeToDefault();
              resetDefaultsOpen = false;
            "
          >
            {{ t("home_editor.reset_defaults_confirm") }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>

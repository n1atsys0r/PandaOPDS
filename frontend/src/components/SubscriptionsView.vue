<script setup>
import { computed, onMounted, onBeforeUnmount, ref, watch } from "vue";
import { useApp } from "@/state.js";
import { t } from "@/i18n.js";
import PublicationCard from "@/components/PublicationCard.vue";
import PublicationDetailDialog from "@/components/PublicationDetailDialog.vue";
import GalleryCategoryPicker from "@/components/GalleryCategoryPicker.vue";
import AutoArchiveOptions from "@/components/AutoArchiveOptions.vue";
import {
  createSubscriptionItemsController,
  createSubscriptionPayload,
  createSubscriptionUpdate,
  cleanupSubscription,
  submitBatchDownload,
  createSubscriptionPoller,
  EH_CATEGORIES,
  getCategoryColor,
  isFavoritesPreset,
  SUBSCRIPTION_PRESET_GROUPS,
  getPresetLabel,
} from "@/subscription-view.js";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Card, CardHeader, CardContent } from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@/components/ui/dropdown-menu";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import {
  RefreshCw,
  Plus,
  Check,
  CheckCheck,
  CheckSquare,
  Search,
  Bookmark,
  Zap,
  Settings,
  MoreVertical,
  Download,
  Trash2,
  Layers,
  BookmarkCheck,
  Flame,
  Eye,
  TrendingUp,
  Sparkles,
} from "@lucide/vue";

const s = useApp();
const emit = defineEmits([
  "read-gallery",
  "archive-gallery",
  "archive-unlink",
  "archive-delete",
  "archive-manage",
  "favorite-gallery",
]);

const subscriptions = ref([]);
const selectedId = ref("");
const items = ref([]);
const total = ref(0);
const runInfo = ref(null);
const page = ref(1);
const perPage = 30;
const view = ref("unread");
const filterText = ref("");
const filterCategory = ref("ALL_CATEGORIES");
const loading = ref(false);
const error = ref("");
const creating = ref(false);
const isSelecting = ref(false);
const selectedItems = ref(new Set());
const batchQuality = ref("res");
const batchResult = ref("");

// Dialog modals
const addModalOpen = ref(false);
const settingsModalOpen = ref(false);
const editingSubscription = ref(null);

const draft = ref({
  name: "",
  source: "preset",
  preset: "latest",
  keywords: "",
  query_categories: [],
  auto_archive: "disabled",
  interval: 3600,
});

watch(
  () => draft.value.preset,
  (newPreset, oldPreset) => {
    const oldLabel = oldPreset ? getPresetLabel(oldPreset, t) : "";
    if (!draft.value.name || draft.value.name === oldLabel) {
      draft.value.name = getPresetLabel(newPreset, t);
    }
  },
);

function openAddModal() {
  editingSubscription.value = null;
  draft.value = {
    name: getPresetLabel("latest", t),
    source: "preset",
    preset: "latest",
    keywords: "",
    query_categories: [],
    auto_archive: "disabled",
    interval: 3600,
  };
  addModalOpen.value = true;
}

function iconForSub(sub) {
  if (sub.source === "search") return Search;
  const p = sub.query?.preset || "latest";
  if (p === "popular") return Flame;
  if (p === "watched") return Eye;
  if (p.startsWith("toplist:")) return TrendingUp;
  if (p === "favorites" || p.startsWith("favorites:")) return Bookmark;
  return Sparkles;
}

function subtitleForSub(sub) {
  if (sub.source === "search") {
    return sub.query?.keywords
      ? `“${sub.query.keywords}”`
      : t("subscription.source_search");
  }
  return getPresetLabel(sub.query?.preset || "latest", t);
}

const settingsDraft = ref({});
const itemsController = createSubscriptionItemsController(s.api);

// Detail dialog
const detailOpen = ref(false);
const selectedPub = ref(null);

let itemsLoadID = 0;
const selected = computed(() =>
  subscriptions.value.find((x) => x.id === selectedId.value),
);
const allSelected = computed(() => selectedId.value === "all");
const pages = computed(() => Math.max(1, Math.ceil(total.value / perPage)));
const running = computed(() =>
  allSelected.value
    ? subscriptions.value.some((x) => x.running)
    : Boolean(selected.value?.running),
);
const targets = computed(() =>
  allSelected.value ? subscriptions.value : [selected.value].filter(Boolean),
);
const presetGroups = computed(() => {
  const allowFavorites = Boolean(s.status.value?.site?.has_ipb) ||
    (editingSubscription.value?.source === "preset" &&
      isFavoritesPreset(settingsDraft.value.preset));
  return SUBSCRIPTION_PRESET_GROUPS.map((group) => ({
    ...group,
    items: group.items.filter(
      (item) => allowFavorites || !isFavoritesPreset(item.id),
    ),
  })).filter((group) => group.items.length > 0);
});

function openSettingsModal(sub) {
  editingSubscription.value = sub;
  settingsDraft.value = {
    name: sub.name || "",
    keywords: sub.query?.keywords || "",
    query_categories: [...(sub.query?.categories || [])],
    sort: sub.query?.sort || "newest",
    preset: sub.query?.preset || "latest",
    interval_seconds:
      sub.enabled === false ? 0 : (sub.interval_seconds ?? 3600),
    auto_archive: sub.auto_download
      ? sub.download_rules?.quality === "org"
        ? "org"
        : "res"
      : "disabled",
  };
  settingsModalOpen.value = true;
}

const poller = createSubscriptionPoller({
  loadSubscriptions,
  loadItems,
  getSubscriptions: () => subscriptions.value,
});

async function loadSubscriptions() {
  try {
    const { data } = await s.api("/api/subscriptions");
    subscriptions.value = data.items || [];
    if (!selectedId.value && subscriptions.value.length) {
      selectedId.value =
        subscriptions.value.length > 1 ? "all" : subscriptions.value[0].id;
    }
    if (
      selectedId.value &&
      selectedId.value !== "all" &&
      !subscriptions.value.some((x) => x.id === selectedId.value)
    ) {
      selectedId.value =
        subscriptions.value.length > 1
          ? "all"
          : subscriptions.value[0]?.id || "";
    }
  } catch (e) {
    error.value = e.message;
  }
}

async function loadItems() {
  const loadID = ++itemsLoadID;
  if (!selectedId.value) {
    itemsController.invalidate();
    items.value = [];
    total.value = 0;
    runInfo.value = null;
    loading.value = false;
    return;
  }
  loading.value = true;
  error.value = "";
  try {
    const ids = allSelected.value ? subscriptions.value.map((x) => x.id) : [];
    const catQuery =
      filterCategory.value === "ALL_CATEGORIES" ? "" : filterCategory.value;
    const response = await itemsController.load({
      subscriptionId: selectedId.value,
      subscriptionIds: ids,
      view: view.value,
      page: page.value,
      perPage,
      q: filterText.value,
      category: catQuery,
    });
    if (!response || loadID !== itemsLoadID) return;
    const { data } = response;
    items.value = data.items || [];
    total.value = Number(data.total || 0);
    runInfo.value = data.run || null;
  } catch (e) {
    if (loadID === itemsLoadID) error.value = e.message;
  } finally {
    if (loadID === itemsLoadID) loading.value = false;
  }
}

watch([selectedId, view, page, filterText, filterCategory], loadItems);
watch([selectedId, view, page, filterText, filterCategory], () => {
  selectedItems.value = new Set();
  batchResult.value = "";
});
watch(selectedId, () => {
  page.value = 1;
  detailOpen.value = false;
  selectedPub.value = null;
});

function startPolling() {
  poller.start();
}

onMounted(async () => {
  await loadSubscriptions();
  if (subscriptions.value.some((x) => x.running)) startPolling();
});

onBeforeUnmount(() => {
  poller.stop();
  itemsLoadID++;
  itemsController.invalidate();
});

async function createSubscription() {
  if (!draft.value.name.trim()) return;
  creating.value = true;
  error.value = "";
  try {
    const { data } = await s.api("/api/subscriptions", {
      method: "POST",
      body: JSON.stringify(createSubscriptionPayload(draft.value)),
    });
    await loadSubscriptions();
    selectedId.value = data.id || selectedId.value;
    addModalOpen.value = false;
    draft.value = {
      name: "",
      source: "preset",
      preset: "latest",
      keywords: "",
      query_categories: [],
      auto_archive: "disabled",
      interval: 3600,
    };
  } catch (e) {
    error.value = e.message;
  } finally {
    creating.value = false;
  }
}

const selectedPageItems = computed(() =>
  items.value.filter((item) =>
    selectedItems.value.has(`${item.gid}:${item.token}`),
  ),
);

const allPageSelected = computed(
  () =>
    items.value.length > 0 &&
    items.value.every((item) =>
      selectedItems.value.has(`${item.gid}:${item.token}`),
    ),
);

function itemKey(item) {
  return `${item.gid}:${item.token}`;
}

function toggleItem(item) {
  const next = new Set(selectedItems.value);
  const key = itemKey(item);
  if (next.has(key)) next.delete(key);
  else next.add(key);
  selectedItems.value = next;
}

function togglePageSelection() {
  const next = new Set(selectedItems.value);
  if (allPageSelected.value)
    items.value.forEach((item) => next.delete(itemKey(item)));
  else items.value.forEach((item) => next.add(itemKey(item)));
  selectedItems.value = next;
}

function toggleSelectMode() {
  isSelecting.value = !isSelecting.value;
  if (!isSelecting.value) {
    selectedItems.value = new Set();
  }
}

function exitSelectMode() {
  isSelecting.value = false;
  selectedItems.value = new Set();
}

async function cleanupHistory(mode) {
  const scopes = targets.value;
  if (!scopes.length) return;
  const scopeNames = scopes.map((sub) => sub.name).join(", ");
  if (
    !window.confirm(
      t("subscription.cleanup_confirm", {
        mode: t(`subscription.cleanup_${mode}_label`),
        scope: scopeNames,
      }),
    )
  )
    return;
  try {
    const results = await Promise.all(
      scopes.map((sub) => cleanupSubscription(s.api, sub.id, mode)),
    );
    const removed = results.reduce(
      (sum, result) => sum + Number(result.data?.removed || 0),
      0,
    );
    batchResult.value = t("subscription.cleanup_result", { count: removed });
    await loadItems();
  } catch (e) {
    error.value = e.message;
  }
}

async function batchDownload() {
  const chosen = selectedPageItems.value;
  if (!chosen.length) return;
  if (
    !window.confirm(
      t("subscription.batch_confirm", {
        count: chosen.length,
        quality: t(
          `archive.tier_${batchQuality.value === "org" ? "original" : "resample"}`,
        ),
      }),
    )
  )
    return;
  try {
    const { data } = await submitBatchDownload(
      s.api,
      chosen,
      batchQuality.value,
    );
    const failed = (data.items || []).filter((item) => item.error).length;
    batchResult.value = t("subscription.batch_result", {
      queued: (data.items || []).length - failed,
      failed,
    });
    selectedItems.value = new Set();
    await loadItems();
  } catch (e) {
    error.value = e.message;
  }
}

async function runNow() {
  if (!selected.value && !allSelected.value) return;
  error.value = "";
  try {
    const targets = allSelected.value
      ? subscriptions.value.filter((x) => x.enabled && !x.running)
      : [selected.value].filter(Boolean);
    if (!targets.length) return;
    await Promise.all(
      targets.map((sub) =>
        s.api(`/api/subscriptions/${encodeURIComponent(sub.id)}/run`, {
          method: "POST",
        }),
      ),
    );
    await loadSubscriptions();
    await loadItems();
    startPolling();
  } catch (e) {
    error.value = e.message;
  }
}

async function saveSettings() {
  if (!editingSubscription.value) return;
  try {
    const { data } = await s.api(
      `/api/subscriptions/${encodeURIComponent(editingSubscription.value.id)}`,
      {
        method: "PATCH",
        body: JSON.stringify(
          createSubscriptionUpdate(
            editingSubscription.value,
            settingsDraft.value,
          ),
        ),
      },
    );
    subscriptions.value = subscriptions.value.map((x) =>
      x.id === data.id ? data : x,
    );
    settingsModalOpen.value = false;
  } catch (e) {
    error.value = e.message;
  }
}

async function deleteSubscription(sub) {
  if (!sub || !window.confirm(t("subscription.delete_confirm"))) return;
  try {
    await s.api(`/api/subscriptions/${encodeURIComponent(sub.id)}`, {
      method: "DELETE",
    });
    settingsModalOpen.value = false;
    selectedId.value = "";
    await loadSubscriptions();
  } catch (e) {
    error.value = e.message;
  }
}

async function setItem(item, patch) {
  try {
    const provider = "eh";
    const ids = allSelected.value
      ? item.subscription_ids || item.membership_ids || []
      : [selectedId.value];
    if (allSelected.value)
      await s.api(
        `/api/subscription-items/${provider}/${item.gid}/${encodeURIComponent(item.token)}`,
        {
          method: "PATCH",
          body: JSON.stringify({ ...patch, subscription_ids: ids }),
        },
      );
    else
      await s.api(
        `/api/subscriptions/${encodeURIComponent(selectedId.value)}/items/${provider}/${item.gid}/${encodeURIComponent(item.token)}`,
        { method: "PATCH", body: JSON.stringify(patch) },
      );
  } catch (e) {
    error.value = e.message;
  }
}

function badgeFor(item) {
  if (item.download_status)
    return { label: item.download_status, variant: "secondary" };
  if (item.archived)
    return { label: t("subscription.local_archive"), variant: "outline" };
  if (!item.inspected)
    return { label: t("subscription.new_badge"), variant: "default" };
  return null;
}

function handleCardClick(item) {
  if (isSelecting.value) {
    toggleItem(item);
    return;
  }
  openDetail(item);
}

function openDetail(item) {
  selectedPub.value = item;
  detailOpen.value = true;
  if (!item.inspected) {
    setItem(item, { inspected: true });
    item.inspected = true;
  }
}

function readItem(payload) {
  detailOpen.value = false;
  if (selectedPub.value && !selectedPub.value.inspected) {
    setItem(selectedPub.value, { inspected: true });
    selectedPub.value.inspected = true;
  }
  emit("read-gallery", payload);
}

function favoriteItem(payload) {
  emit("favorite-gallery", payload);
}

function downloadItem(item) {
  emit("archive-gallery", item);
}

function onDetailSearch(q) {
  detailOpen.value = false;
  s.enterExploreSubRoute({
    title: `${t("common.search")}: ${q}`,
    query: q,
  });
}

function onDetailCategory(cat) {
  detailOpen.value = false;
  s.enterExploreSubRoute({
    title: t("explore.search_title_category", { cat }),
    category: cat,
  });
}
</script>

<template>
  <div class="subscriptions-view flex flex-col gap-6">
    <!-- Standard Page Heading (Aligned with HomeEditor) -->
    <div class="heading">
      <div>
        <small>{{ t("subscription.header") }}</small>
        <h1>{{ t("subscription.title") }}</h1>
        <p>{{ t("subscription.description") }}</p>
      </div>
      <div class="flex items-center gap-2 flex-wrap">
        <Button
          variant="outline"
          class="gap-1.5"
          :disabled="!targets.length || running"
          @click="runNow"
        >
          <RefreshCw class="size-4" :class="{ 'animate-spin': running }" />
          {{ running ? t("subscription.running") : t("subscription.run_now") }}
        </Button>
      </div>
    </div>

    <!-- Master-Detail Layout (Aligned with HomeEditor grid: Left 3-4 cols, Right 8-9 cols) -->
    <div class="grid grid-cols-1 lg:grid-cols-12 gap-6 items-start">
      <!-- ================= LEFT SIDEBAR: 平铺订阅列表 ================= -->
      <div class="lg:col-span-4 xl:col-span-3 flex flex-col gap-3">
        <Card class="panel">
          <CardHeader
            class="p-4 pb-3 flex flex-row items-center justify-between gap-2 border-b border-border/60"
          >
            <div class="flex items-center gap-2">
              <BookmarkCheck class="size-4 text-primary" />
              <span class="font-semibold text-sm">{{
                t("subscription.list_title")
              }}</span>
              <Badge
                variant="secondary"
                class="h-5 px-1.5 text-[11px] font-mono font-normal"
              >
                {{ subscriptions.length }}
              </Badge>
            </div>
            <Button
              variant="outline"
              size="sm"
              class="h-7 gap-1 text-xs px-2"
              @click="openAddModal"
            >
              <Plus class="size-3.5" />{{ t("subscription.add") }}
            </Button>
          </CardHeader>
          <CardContent class="p-2 flex flex-col gap-1">
            <!-- Pinned All Subscriptions Item (when multiple subscriptions exist) -->
            <div
              v-if="subscriptions.length > 1"
              class="group relative flex items-center justify-between gap-2 px-3 py-2.5 rounded-xl cursor-pointer transition-colors text-xs select-none border border-transparent"
              :class="
                selectedId === 'all'
                  ? 'bg-primary/10 border-primary/20 text-primary font-medium shadow-xs'
                  : 'hover:bg-muted/60 text-muted-foreground hover:text-foreground'
              "
              @click="selectedId = 'all'"
            >
              <div class="flex items-center gap-2.5 min-w-0 flex-1">
                <Layers class="size-4 shrink-0 text-primary" />
                <div class="min-w-0 flex-1">
                  <div class="truncate text-sm font-medium leading-tight">
                    {{ t("subscription.all_subscriptions") }}
                  </div>
                  <div class="flex items-center gap-1.5 mt-0.5">
                    <span class="text-[10px] text-muted-foreground/80">
                      {{ subscriptions.length }}
                      {{ t("subscription.list_title") }}
                    </span>
                  </div>
                </div>
              </div>
              <RefreshCw
                v-if="subscriptions.some((x) => x.running)"
                class="size-3.5 shrink-0 text-primary animate-spin"
              />
            </div>

            <!-- Tiled Individual Subscriptions -->
            <div
              v-for="sub in subscriptions"
              :key="sub.id"
              class="group relative flex items-center justify-between gap-2 px-3 py-2.5 rounded-xl cursor-pointer transition-colors text-xs select-none border border-transparent"
              :class="
                selectedId === sub.id
                  ? 'bg-primary/10 border-primary/20 text-primary font-medium shadow-xs'
                  : 'hover:bg-muted/60 text-muted-foreground hover:text-foreground'
              "
              @click="selectedId = sub.id"
            >
              <div class="flex items-center gap-2.5 min-w-0 flex-1">
                <component
                  :is="iconForSub(sub)"
                  class="size-4 shrink-0"
                  :class="
                    sub.query?.preset?.startsWith('favorites')
                      ? 'text-primary'
                      : sub.source === 'preset'
                        ? 'text-amber-500'
                        : 'text-blue-500'
                  "
                />

                <div class="min-w-0 flex-1">
                  <div
                    class="truncate text-sm font-medium leading-tight flex items-center gap-1.5"
                  >
                    <span class="truncate">{{ sub.name }}</span>
                    <span
                      v-if="!sub.enabled"
                      class="size-1.5 rounded-full bg-muted-foreground/50 shrink-0"
                    />
                  </div>
                  <div
                    class="flex items-center gap-1.5 mt-0.5 text-[10px] text-muted-foreground/80 truncate"
                  >
                    <span class="font-medium text-foreground/70 shrink-0">
                      {{ subtitleForSub(sub) }}
                    </span>
                    <span>·</span>
                    <span
                      v-if="sub.running"
                      class="text-primary font-medium flex items-center gap-1"
                    >
                      <RefreshCw class="size-2.5 animate-spin" />
                      {{ t("subscription.running") }}
                    </span>
                    <span
                      v-else-if="sub.last_error"
                      class="text-destructive truncate"
                    >
                      {{ sub.last_error }}
                    </span>
                    <span v-else-if="sub.last_run_at" class="truncate">
                      {{ sub.last_run_at }}
                    </span>
                    <span v-else>
                      {{ t("subscription.never_run") }}
                    </span>
                  </div>
                </div>
              </div>

              <div class="flex items-center gap-1 shrink-0">
                <Button
                  variant="ghost"
                  size="icon-xs"
                  class="size-6 rounded-md opacity-0 group-hover:opacity-100 transition-opacity hover:bg-background/80"
                  :title="t('subscription.settings')"
                  @click.stop="openSettingsModal(sub)"
                >
                  <Settings class="size-3.5" />
                </Button>
              </div>
            </div>

            <div
              v-if="!subscriptions.length"
              class="p-4 text-center text-xs text-muted-foreground"
            >
              {{ t("subscription.no_subscriptions") }}
            </div>
          </CardContent>
        </Card>
      </div>

      <!-- ================= RIGHT WORKBENCH: 工具栏与画廊网格 ================= -->
      <div class="lg:col-span-8 xl:col-span-9 flex flex-col gap-4 min-w-0">
        <!-- Diagnostic Status Banner -->
        <div
          v-if="runInfo"
          class="rounded-xl border border-border/60 bg-muted/30 p-3 text-xs text-muted-foreground flex flex-wrap items-center justify-between gap-2"
        >
          <div class="flex items-center gap-1.5 flex-wrap">
            <span>{{ t("subscription.run_diagnostics") }}:</span>
            <Badge
              variant="outline"
              class="text-[10px] py-0 px-1.5 font-normal"
            >
              {{ runInfo.status }}
            </Badge>
            <span>· {{ runInfo.pages }} {{ t("subscription.run_pages") }}</span>
            <span
              >· {{ runInfo.results }} {{ t("subscription.run_results") }}</span
            >
            <span
              >·
              {{
                t(`subscription.coverage_${runInfo.coverage || "unknown"}`)
              }}</span
            >
          </div>
          <span
            v-if="runInfo.coverage === 'partial'"
            class="text-amber-500 font-medium text-[11px]"
          >
            {{ t("subscription.partial_run") }}
          </span>
        </div>

        <!-- Single Consolidated Toolbar (Luma Theme Aligned) -->
        <div
          class="p-3 rounded-2xl border border-border/60 bg-card shadow-xs flex flex-wrap items-center justify-between gap-3"
        >
          <!-- Left: View Switcher (Unread / All) + Item Count -->
          <div class="flex items-center gap-2.5">
            <ToggleGroup
              type="single"
              v-model="view"
              class="bg-muted/60 p-0.5 rounded-lg border border-border/40"
            >
              <ToggleGroupItem
                value="unread"
                class="h-7 px-3 text-xs data-[state=on]:bg-background data-[state=on]:text-foreground data-[state=on]:shadow-xs rounded-md"
              >
                {{ t("subscription.view_unread") }}
              </ToggleGroupItem>
              <ToggleGroupItem
                value="all"
                class="h-7 px-3 text-xs data-[state=on]:bg-background data-[state=on]:text-foreground data-[state=on]:shadow-xs rounded-md"
              >
                {{ t("subscription.view_all") }}
              </ToggleGroupItem>
            </ToggleGroup>
            <Badge
              variant="secondary"
              class="h-6 px-2 text-xs font-mono font-normal"
            >
              {{ t("subscription.item_count", { count: total }) }}
            </Badge>
          </div>

          <!-- Middle: Search Input and Category Selector -->
          <div class="flex items-center gap-2 flex-1 max-w-md min-w-[200px]">
            <div class="relative flex-1">
              <Search
                class="absolute left-2.5 top-2.5 size-3.5 text-muted-foreground pointer-events-none"
              />
              <Input
                v-model="filterText"
                class="h-8 pl-8 pr-2.5 text-xs bg-muted/30 border-border/60 focus-visible:bg-background"
                :placeholder="t('subscription.filter_local')"
              />
            </div>
            <Select v-model="filterCategory">
              <SelectTrigger
                class="h-8 text-xs w-[130px] bg-muted/30 border-border/60"
              >
                <SelectValue :placeholder="t('subscription.filter_category')" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="ALL_CATEGORIES">
                  {{ t("subscription.filter_all_categories") }}
                </SelectItem>
                <SelectItem
                  v-for="cat in EH_CATEGORIES"
                  :key="cat.key"
                  :value="cat.key"
                >
                  <div class="flex items-center gap-1.5">
                    <span
                      class="size-2 rounded-full shrink-0"
                      :style="{ backgroundColor: cat.color }"
                    />
                    <span class="truncate">{{ cat.label }}</span>
                  </div>
                </SelectItem>
              </SelectContent>
            </Select>
          </div>

          <!-- Right: Selection Mode Button + Overflow Actions -->
          <div class="flex items-center gap-2">
            <Button
              :variant="isSelecting ? 'default' : 'outline'"
              size="sm"
              class="h-8 gap-1.5 text-xs px-2.5"
              @click="toggleSelectMode"
            >
              <CheckSquare class="size-3.5" />
              {{
                isSelecting
                  ? t("subscription.exit_select")
                  : t("subscription.select_mode")
              }}
            </Button>

            <DropdownMenu>
              <DropdownMenuTrigger as-child>
                <Button variant="outline" size="sm" class="h-8 size-8 p-0">
                  <MoreVertical class="size-4" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem
                  @click="cleanupHistory('inspected')"
                  :disabled="!targets.length"
                >
                  <CheckCheck class="size-4 mr-2" />
                  {{ t("subscription.cleanup_inspected") }}
                </DropdownMenuItem>
                <DropdownMenuItem
                  @click="runNow"
                  :disabled="!targets.length || running"
                >
                  <RefreshCw
                    class="size-4 mr-2"
                    :class="{ 'animate-spin': running }"
                  />
                  {{ t("subscription.run_now") }}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>

        <!-- Sticky Docked Multi-Select Action Bar -->
        <div
          v-if="isSelecting || selectedItems.size > 0"
          class="sticky top-4 z-20 flex flex-wrap items-center justify-between gap-3 p-3 rounded-2xl border border-primary/20 bg-background/95 backdrop-blur-md shadow-lg"
        >
          <div class="flex items-center gap-3">
            <label
              class="flex items-center gap-2 text-xs font-medium cursor-pointer select-none"
            >
              <input
                type="checkbox"
                class="rounded border-border size-4 accent-primary"
                :checked="allPageSelected"
                @change="togglePageSelection"
              />
              {{ t("subscription.select_page") }}
            </label>
            <Badge variant="outline" class="font-mono text-xs">
              {{
                t("subscription.selected_count", { count: selectedItems.size })
              }}
            </Badge>
          </div>

          <div class="flex items-center gap-2">
            <Select v-model="batchQuality">
              <SelectTrigger class="h-8 text-xs w-[110px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="res">{{
                  t("archive.tier_resample")
                }}</SelectItem>
                <SelectItem value="org">{{
                  t("archive.tier_original")
                }}</SelectItem>
              </SelectContent>
            </Select>

            <Button
              size="sm"
              class="h-8 text-xs gap-1.5"
              :disabled="!selectedItems.size"
              @click="batchDownload"
            >
              <Download class="size-3.5" />
              {{
                t("subscription.batch_download", { count: selectedItems.size })
              }}
            </Button>

            <Button
              v-if="isSelecting"
              size="sm"
              variant="ghost"
              class="h-8 text-xs text-muted-foreground hover:text-foreground"
              @click="exitSelectMode"
            >
              {{ t("subscription.exit_select") }}
            </Button>
          </div>
        </div>

        <!-- Batch Result Message -->
        <p v-if="batchResult" class="text-xs text-muted-foreground">
          {{ batchResult }}
        </p>

        <!-- Error Alert -->
        <p
          v-if="error"
          role="alert"
          class="rounded-xl border border-destructive/40 p-3 text-sm text-destructive"
        >
          {{ error }}
        </p>

        <!-- Gallery Poster Grid (Fully aligned with Explore) -->
        <div
          v-if="loading"
          class="py-16 text-center text-sm text-muted-foreground"
        >
          <RefreshCw class="size-5 animate-spin mx-auto mb-2 text-primary" />
          {{ t("common.loading") }}
        </div>

        <div
          v-else-if="items.length"
          class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 gap-4"
        >
          <div
            v-for="item in items"
            :key="`${item.publication_id || item.gid}:${item.token}`"
            class="group relative"
          >
            <PublicationCard
              :publication="item"
              :badge="badgeFor(item)"
              :category-color="getCategoryColor(item.category)"
              @select="handleCardClick(item)"
            />

            <!-- Selection Indicator in Multi-Select Mode -->
            <div
              v-if="isSelecting"
              class="absolute top-2 left-2 z-10 transition-transform active:scale-90 cursor-pointer"
              @click.stop="toggleItem(item)"
            >
              <div
                class="size-6 rounded-full border-2 flex items-center justify-center shadow-md transition-colors"
                :class="
                  selectedItems.has(itemKey(item))
                    ? 'bg-primary border-primary text-primary-foreground'
                    : 'bg-black/50 border-white/80 text-transparent hover:border-white'
                "
              >
                <Check class="size-3.5 stroke-[3]" />
              </div>
            </div>
          </div>
        </div>

        <!-- Empty State -->
        <div
          v-else-if="subscriptions.length && selectedId"
          class="rounded-2xl border border-dashed border-border/80 p-12 text-center text-sm text-muted-foreground"
        >
          {{ t("subscription.empty") }}
        </div>

        <!-- Pagination Controls -->
        <div
          v-if="pages > 1"
          class="flex items-center justify-center gap-3 pt-4"
        >
          <Button
            variant="outline"
            size="sm"
            :disabled="page <= 1"
            @click="page--"
          >
            {{ t("common.previous") }}
          </Button>
          <span class="text-xs text-muted-foreground font-mono">
            {{ page }} / {{ pages }}
          </span>
          <Button
            variant="outline"
            size="sm"
            :disabled="page >= pages"
            @click="page++"
          >
            {{ t("common.next") }}
          </Button>
        </div>
      </div>
    </div>

    <!-- Apple Books Style Gallery Detail Dialog (Reused from Explore) -->
    <PublicationDetailDialog
      v-model:open="detailOpen"
      v-model:publication="selectedPub"
      :list="items"
      @read-gallery="readItem"
      @archive-gallery="downloadItem"
      @archive-unlink="emit('archive-unlink', $event)"
      @archive-delete="emit('archive-delete', $event)"
      @archive-manage="emit('archive-manage', $event)"
      @favorite-gallery="favoriteItem"
      @tag-click="onDetailSearch"
      @category-click="onDetailCategory"
      @author-click="onDetailSearch"
      @uploader-click="onDetailSearch"
    />

    <!-- Add Subscription Dialog -->
    <Dialog v-model:open="addModalOpen">
      <DialogContent class="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{{ t("subscription.add_dialog_title") }}</DialogTitle>
          <DialogDescription>{{
            t("subscription.add_dialog_desc")
          }}</DialogDescription>
        </DialogHeader>
        <form class="space-y-4 pt-2" @submit.prevent="createSubscription">
          <div class="space-y-1.5">
            <label class="text-xs font-semibold">{{
              t("subscription.name")
            }}</label>
            <Input
              v-model="draft.name"
              required
              :placeholder="t('subscription.name')"
            />
          </div>

          <div class="space-y-1.5">
            <label class="text-xs font-semibold">{{
              t("subscription.choose")
            }}</label>
            <Select v-model="draft.source">
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="preset">{{
                  t("subscription.source_preset")
                }}</SelectItem>
                <SelectItem value="search">{{
                  t("subscription.source_search")
                }}</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <!-- When Preset: Grouped Presets (Aligned with HomeEditor) -->
          <div v-if="draft.source === 'preset'" class="space-y-1.5">
            <label class="text-xs font-semibold">{{
              t("subscription.preset_select")
            }}</label>
            <Select v-model="draft.preset">
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent class="max-h-72">
                <SelectGroup
                  v-for="grp in presetGroups"
                  :key="grp.group"
                >
                  <SelectLabel
                    class="text-[11px] font-semibold text-muted-foreground uppercase px-2 py-1.5"
                  >
                    {{ t(grp.group) }}
                  </SelectLabel>
                  <SelectItem
                    v-for="it in grp.items"
                    :key="it.id"
                    :value="it.id"
                    class="text-xs"
                  >
                    {{
                      it.favcatIndex !== undefined
                        ? t(it.labelKey, { id: it.favcatIndex })
                        : t(it.labelKey)
                    }}
                  </SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
          </div>

          <GalleryCategoryPicker
            v-if="draft.source === 'preset'"
            v-model="draft.query_categories"
            :label="t('subscription.gallery_categories')"
            :description="t('subscription.gallery_category_filter_desc')"
          />

          <template v-if="draft.source === 'search'">
            <div class="space-y-1.5">
              <label class="text-xs font-semibold">{{
                t("subscription.keywords")
              }}</label>
              <Input
                v-model="draft.keywords"
                :placeholder="t('subscription.keywords')"
              />
            </div>
            <GalleryCategoryPicker
              v-model="draft.query_categories"
              :label="t('subscription.gallery_categories')"
              :description="t('subscription.gallery_category_filter_desc')"
            />
          </template>

          <div class="space-y-1.5">
            <label class="text-xs font-semibold">{{
              t("subscription.interval")
            }}</label>
            <Input v-model.number="draft.interval" type="number" min="0" />
          </div>

          <AutoArchiveOptions
            v-if="s.status.value?.site?.has_ipb"
            v-model="draft.auto_archive"
            name="new-subscription-auto-archive"
          />

          <DialogFooter class="pt-2">
            <Button
              type="button"
              variant="outline"
              @click="addModalOpen = false"
            >
              {{ t("common.cancel") }}
            </Button>
            <Button type="submit" :disabled="creating">
              {{ t("subscription.create") }}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>

    <!-- Subscription Settings Dialog -->
    <Dialog v-model:open="settingsModalOpen">
      <DialogContent class="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{{
            t("subscription.settings_dialog_title")
          }}</DialogTitle>
          <DialogDescription>{{
            t("subscription.settings_dialog_desc")
          }}</DialogDescription>
        </DialogHeader>
        <form
          v-if="editingSubscription"
          class="space-y-4 pt-2"
          @submit.prevent="saveSettings"
        >
          <div class="space-y-1.5">
            <label class="text-xs font-semibold">{{
              t("subscription.name")
            }}</label>
            <Input v-model="settingsDraft.name" required />
          </div>

          <div class="space-y-1.5">
            <label class="text-xs font-semibold">{{
              t("subscription.interval")
            }}</label>
            <Input
              v-model.number="settingsDraft.interval_seconds"
              type="number"
              min="0"
            />
          </div>

          <AutoArchiveOptions
            v-if="s.status.value?.site?.has_ipb"
            v-model="settingsDraft.auto_archive"
            name="settings-auto-archive"
          />
          <!-- Preset selector when editing a preset subscription -->
          <div
            v-if="editingSubscription.source === 'preset'"
            class="space-y-1.5"
          >
            <label class="text-xs font-semibold">{{
              t("subscription.preset_select")
            }}</label>
            <Select v-model="settingsDraft.preset">
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent class="max-h-72">
                <SelectGroup
                  v-for="grp in presetGroups"
                  :key="grp.group"
                >
                  <SelectLabel
                    class="text-[11px] font-semibold text-muted-foreground uppercase px-2 py-1.5"
                  >
                    {{ t(grp.group) }}
                  </SelectLabel>
                  <SelectItem
                    v-for="it in grp.items"
                    :key="it.id"
                    :value="it.id"
                    class="text-xs"
                  >
                    {{
                      it.favcatIndex !== undefined
                        ? t(it.labelKey, { id: it.favcatIndex })
                        : t(it.labelKey)
                    }}
                  </SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
          </div>

          <GalleryCategoryPicker
            v-if="editingSubscription.source === 'preset'"
            v-model="settingsDraft.query_categories"
            :label="t('subscription.gallery_categories')"
            :description="t('subscription.gallery_category_filter_desc')"
          />

          <div v-if="editingSubscription.source === 'search'" class="space-y-3">
            <div class="space-y-1.5">
              <label class="text-xs font-semibold">{{
                t("subscription.keywords")
              }}</label>
              <Input v-model="settingsDraft.keywords" />
            </div>
            <GalleryCategoryPicker
              v-model="settingsDraft.query_categories"
              :label="t('subscription.gallery_categories')"
              :description="t('subscription.gallery_category_filter_desc')"
            />
          </div>


          <DialogFooter
            class="pt-2 flex items-center justify-between sm:justify-between w-full"
          >
            <Button
              type="button"
              variant="ghost"
              size="sm"
              class="text-destructive hover:text-destructive hover:bg-destructive/10"
              @click="deleteSubscription(editingSubscription)"
            >
              <Trash2 class="size-3.5 mr-1" />
              {{ t("common.delete") }}
            </Button>

            <div class="flex items-center gap-2">
              <Button
                type="button"
                variant="outline"
                size="sm"
                @click="settingsModalOpen = false"
              >
                {{ t("common.cancel") }}
              </Button>
              <Button type="submit" size="sm">
                {{ t("common.save") }}
              </Button>
            </div>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  </div>
</template>

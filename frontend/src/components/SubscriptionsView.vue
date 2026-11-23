<script setup>
import { computed, onMounted, onBeforeUnmount, ref, watch } from "vue";
import { useApp } from "@/state.js";
import { t, currentLocale } from "@/i18n.js";
import PublicationCard from "@/components/PublicationCard.vue";
import PublicationDetailDialog from "@/components/PublicationDetailDialog.vue";
import GalleryCategoryPicker from "@/components/GalleryCategoryPicker.vue";
import AutoArchiveOptions from "@/components/AutoArchiveOptions.vue";
import SubscriptionList from "@/components/SubscriptionList.vue";
import {
  createSubscriptionItemsController,
  createSubscriptionPayload,
  createSubscriptionUpdate,
  cleanupSubscription,
  submitBatchDownload,
  submitBatchInspect,
  createSubscriptionPoller,
  EH_CATEGORIES,
  getCategoryColor,
  isFavoritesPreset,
  SUBSCRIPTION_PRESET_GROUPS,
  getPresetLabel,
  formatRelativeTime,
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
  DropdownMenuSeparator,
} from "@/components/ui/dropdown-menu";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import {
  RefreshCw,
  Plus,
  Check,
  CheckSquare,
  Search,
  Bookmark,
  Zap,
  Settings,
  Download,
  Trash2,
  Layers,
  BookmarkCheck,
  Flame,
  Eye,
  TrendingUp,
  Sparkles,
  ChevronsUpDown,
  PanelLeft,
  SlidersHorizontal,
  X,
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
const view = ref("new");
const filterText = ref("");
const filterCategory = ref("ALL_CATEGORIES");
const loading = ref(false);
const error = ref("");
const creating = ref(false);
const isSelecting = ref(false);
const selectedItems = ref(new Set());
const defaultQuality = ref("org");
const batchQuality = ref("org");
const batchResult = ref("");
const batchDownloadModalOpen = ref(false);
const submittingBatch = ref(false);

// Sidebar Pinned State
const pinnedSidebar = ref(
  typeof window !== "undefined" &&
    localStorage.getItem("panda_subscriptions_sidebar_pinned") === "true",
);

function togglePinSidebar() {
  pinnedSidebar.value = !pinnedSidebar.value;
  if (typeof window !== "undefined") {
    localStorage.setItem(
      "panda_subscriptions_sidebar_pinned",
      String(pinnedSidebar.value),
    );
  }
}

const activeSubscriptionTitle = computed(() => {
  if (allSelected.value) {
    return t("subscription.all_subscriptions");
  }
  return selected.value?.name || t("subscription.choose");
});

const activeSubscriptionIcon = computed(() => {
  if (allSelected.value) {
    return Layers;
  }
  return selected.value ? iconForSub(selected.value) : BookmarkCheck;
});

const visibleSubscriptions = computed(() =>
  subscriptions.value.filter((x) => !x.exclude_from_all),
);

const allBadgeText = computed(() => {
  if (visibleSubscriptions.value.length === subscriptions.value.length) {
    return String(subscriptions.value.length);
  }
  return `${visibleSubscriptions.value.length}/${subscriptions.value.length}`;
});

const activeSubscriptionRunning = computed(() => {
  if (allSelected.value) {
    return visibleSubscriptions.value.some((x) => x.running);
  }
  return Boolean(selected.value?.running);
});

// Dialog modals
const addModalOpen = ref(false);
const settingsModalOpen = ref(false);
const allFilterModalOpen = ref(false);
const subDropdownOpen = ref(false);
const filterSelectionMap = ref({});
const savingFilter = ref(false);
const editingSubscription = ref(null);

const tempSelectedCount = computed(() => {
  return Object.values(filterSelectionMap.value).filter(Boolean).length;
});

function openAllFilterModal() {
  subDropdownOpen.value = false;
  const map = {};
  subscriptions.value.forEach((sub) => {
    map[sub.id] = !sub.exclude_from_all;
  });
  filterSelectionMap.value = map;
  allFilterModalOpen.value = true;
}

function selectAllInFilter(val) {
  const map = { ...filterSelectionMap.value };
  subscriptions.value.forEach((sub) => {
    map[sub.id] = val;
  });
  filterSelectionMap.value = map;
}

function toggleSubFilter(id) {
  filterSelectionMap.value = {
    ...filterSelectionMap.value,
    [id]: !filterSelectionMap.value[id],
  };
}

async function saveFilterSettings() {
  savingFilter.value = true;
  error.value = "";
  try {
    const changed = subscriptions.value.filter(
      (sub) => !sub.exclude_from_all !== Boolean(filterSelectionMap.value[sub.id]),
    );
    if (changed.length > 0) {
      await Promise.all(
        changed.map((sub) =>
          s.api(`/api/subscriptions/${encodeURIComponent(sub.id)}`, {
            method: "PATCH",
            body: JSON.stringify({
              exclude_from_all: !filterSelectionMap.value[sub.id],
            }),
          }),
        ),
      );
      await loadSubscriptions();
      await loadItems();
    }
    allFilterModalOpen.value = false;
  } catch (e) {
    error.value = e.message;
  } finally {
    savingFilter.value = false;
  }
}

const draft = ref({
  name: "",
  source: "preset",
  preset: "latest",
  keywords: "",
  query_categories: [],
  auto_archive: "disabled",
  interval: 3600,
  show_in_all: true,
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
    show_in_all: true,
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
    ? visibleSubscriptions.value.some((x) => x.running)
    : Boolean(selected.value?.running),
);
const targets = computed(() =>
  allSelected.value ? visibleSubscriptions.value : [selected.value].filter(Boolean),
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
  subDropdownOpen.value = false;
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
    show_in_all: !sub.exclude_from_all,
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
    const ids = allSelected.value
      ? visibleSubscriptions.value.map((x) => x.id)
      : [];
    if (allSelected.value && ids.length === 0) {
      itemsController.invalidate();
      items.value = [];
      total.value = 0;
      runInfo.value = null;
      loading.value = false;
      return;
    }
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

// Viewport Dwell Auto-Inspect
let inspectObserver = null;
const cardElements = new WeakMap();
const dwellTimers = new Map();
const pendingBatchItems = new Map();
let batchSyncTimeout = null;

function cancelDwell(key) {
  if (dwellTimers.has(key)) {
    clearTimeout(dwellTimers.get(key));
    dwellTimers.delete(key);
  }
}

function flushBatchInspect() {
  if (batchSyncTimeout) {
    clearTimeout(batchSyncTimeout);
    batchSyncTimeout = null;
  }
  if (pendingBatchItems.size === 0) return;
  const toSubmit = Array.from(pendingBatchItems.values());
  pendingBatchItems.clear();
  submitBatchInspect(s.api, toSubmit).catch((e) => {
    console.error("Batch inspect sync failed:", e);
  });
}

function scheduleBatchInspect(item) {
  const key = itemKey(item);
  const subIds = allSelected.value
    ? item.subscription_ids || item.membership_ids || []
    : [selectedId.value].filter(Boolean);
  pendingBatchItems.set(key, {
    gid: item.gid,
    token: item.token,
    subscription_ids: subIds,
  });
  // Mark local inspected so NEW badge fades out
  item.inspected = true;

  if (batchSyncTimeout) clearTimeout(batchSyncTimeout);
  batchSyncTimeout = setTimeout(flushBatchInspect, 1500);
}

function initObserver() {
  if (typeof window === "undefined" || !("IntersectionObserver" in window)) return;
  inspectObserver = new IntersectionObserver(
    (entries) => {
      entries.forEach((entry) => {
        const item = cardElements.get(entry.target);
        if (!item || item.inspected) {
          if (item) cancelDwell(itemKey(item));
          inspectObserver?.unobserve(entry.target);
          return;
        }
        const key = itemKey(item);
        if (entry.isIntersecting && entry.intersectionRatio >= 0.5) {
          if (!dwellTimers.has(key)) {
            const timer = setTimeout(() => {
              dwellTimers.delete(key);
              inspectObserver?.unobserve(entry.target);
              scheduleBatchInspect(item);
            }, 1000);
            dwellTimers.set(key, timer);
          }
        } else {
          cancelDwell(key);
        }
      });
    },
    { threshold: 0.5 },
  );
}

function observeCard(el, item) {
  if (!el) return;
  if (!item || item.inspected) return;
  cardElements.set(el, item);
  inspectObserver?.observe(el);
}

watch([selectedId, view, page, filterText, filterCategory], loadItems);
watch([selectedId, view, page, filterText, filterCategory], () => {
  flushBatchInspect();
  dwellTimers.forEach((timer) => clearTimeout(timer));
  dwellTimers.clear();
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

async function loadDefaultQuality() {
  try {
    const res = await s.api("/api/config");
    const archiveGroup = (res.data?.groups || []).find((g) => g.id === "archive");
    const qualityField = (archiveGroup?.fields || []).find((f) => f.key === "archive_quality");
    const val = String(qualityField?.value || "").toLowerCase().trim();
    if (val === "resample" || val === "res" || val === "resized") {
      defaultQuality.value = "res";
    } else {
      defaultQuality.value = "org";
    }
    batchQuality.value = defaultQuality.value;
  } catch (_) {
    // Keep fallback "org"
  }
}

onMounted(async () => {
  initObserver();
  await Promise.all([loadSubscriptions(), loadDefaultQuality()]);
  if (subscriptions.value.some((x) => x.running)) startPolling();
});

onBeforeUnmount(() => {
  flushBatchInspect();
  dwellTimers.forEach((timer) => clearTimeout(timer));
  dwellTimers.clear();
  if (inspectObserver) {
    inspectObserver.disconnect();
    inspectObserver = null;
  }
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
      body: JSON.stringify(
        createSubscriptionPayload({
          ...draft.value,
          exclude_from_all: !draft.value.show_in_all,
        }),
      ),
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
      show_in_all: true,
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

function openBatchDownloadModal() {
  if (!selectedPageItems.value.length) return;
  batchQuality.value = defaultQuality.value;
  batchDownloadModalOpen.value = true;
}

async function confirmBatchDownload() {
  const chosen = selectedPageItems.value;
  if (!chosen.length) return;
  submittingBatch.value = true;
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
    batchDownloadModalOpen.value = false;
    await loadItems();
  } catch (e) {
    error.value = e.message;
  } finally {
    submittingBatch.value = false;
  }
}

async function runNow() {
  if (!selected.value && !allSelected.value) return;
  error.value = "";
  try {
    const targets = allSelected.value
      ? visibleSubscriptions.value.filter((x) => x.enabled && !x.running)
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
            {
              ...settingsDraft.value,
              exclude_from_all: !settingsDraft.value.show_in_all,
            },
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
  const isCompleted =
    typeof item.progression === "number" && item.progression >= 0.95;
  if (isCompleted)
    return { label: t("subscription.completed_badge"), variant: "outline" };
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
          :variant="isSelecting ? 'default' : 'outline'"
          class="gap-1.5 cursor-pointer"
          @click="toggleSelectMode"
        >
          <CheckSquare class="size-4" />
          {{
            isSelecting
              ? t("subscription.exit_select")
              : t("subscription.select_mode")
          }}
        </Button>

        <Button
          variant="outline"
          class="gap-1.5 cursor-pointer"
          :disabled="!targets.length || running"
          @click="runNow"
        >
          <RefreshCw class="size-4" :class="{ 'animate-spin': running }" />
          {{ running ? t("subscription.running") : t("subscription.run_now") }}
        </Button>

        <Button
          variant="outline"
          class="gap-1.5 cursor-pointer"
          @click="openAddModal"
        >
          <Plus class="size-4" />
          {{ t("subscription.add") }}
        </Button>
      </div>
    </div>

    <!-- Master-Detail Layout: Pinned Sidebar or Full-Width Gallery -->
    <div
      :class="
        pinnedSidebar
          ? 'flex flex-col lg:flex-row gap-5 items-start'
          : 'flex flex-col gap-4'
      "
    >
      <!-- ================= LEFT PINNED SIDEBAR (Only visible on lg+ when pinned) ================= -->
      <div
        v-if="pinnedSidebar"
        class="hidden lg:flex w-60 xl:w-64 shrink-0 flex-col gap-3"
      >
        <SubscriptionList
          :subscriptions="subscriptions"
          :selected-id="selectedId"
          :is-pinned="true"
          @select="selectedId = $event"
          @open-settings="openSettingsModal"
          @open-all-filter="openAllFilterModal"
          @toggle-pin="togglePinSidebar"
        />
      </div>

      <!-- ================= RIGHT / FULL WORKBENCH ================= -->
      <div
        class="flex-1 min-w-0 flex flex-col gap-4 transition-[padding] duration-200"
        :class="(isSelecting || selectedItems.size > 0) ? 'pb-20 sm:pb-24' : ''"
      >
        <!-- Single Consolidated Toolbar (Luma Theme Aligned) -->
        <div
          class="p-3 rounded-2xl border border-border/60 bg-card shadow-xs flex flex-wrap items-center justify-between gap-3"
        >
          <!-- Left: Compact Sidebar Toggle (when unpinned) + Active Subscription Dropdown + View Switcher -->
          <div class="flex items-center gap-2 flex-wrap">
            <!-- Desktop Pin Sidebar Toggle (when unpinned) -->
            <Button
              v-if="!pinnedSidebar"
              variant="ghost"
              size="icon-xs"
              class="hidden lg:flex size-8 text-muted-foreground hover:text-foreground cursor-pointer"
              :title="t('subscription.pin_sidebar')"
              @click="togglePinSidebar"
            >
              <PanelLeft class="size-4" />
            </Button>

            <DropdownMenu v-if="!pinnedSidebar" v-model:open="subDropdownOpen">
              <DropdownMenuTrigger as-child>
                <Button
                  variant="outline"
                  size="sm"
                  class="h-8 gap-2 text-xs px-2.5 max-w-[220px] border-border/60 bg-muted/20 hover:bg-muted/40 font-medium cursor-pointer"
                  :title="t('subscription.choose')"
                >
                  <component
                    :is="activeSubscriptionIcon"
                    class="size-3.5 shrink-0 text-primary"
                  />
                  <span class="truncate">{{ activeSubscriptionTitle }}</span>
                  <RefreshCw
                    v-if="activeSubscriptionRunning"
                    class="size-3 shrink-0 text-primary animate-spin"
                  />
                  <ChevronsUpDown
                    class="size-3 text-muted-foreground shrink-0 opacity-60 ml-0.5"
                  />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start" class="w-72 max-h-[70vh] overflow-y-auto">
                <!-- All Subscriptions -->
                <DropdownMenuItem
                  v-if="subscriptions.length > 1"
                  class="flex items-center justify-between gap-2 py-2 cursor-pointer"
                  :class="selectedId === 'all' ? 'bg-primary/10 text-primary font-medium' : ''"
                  @click="
                    selectedId = 'all';
                    subDropdownOpen = false;
                  "
                >
                  <div class="flex items-center gap-2.5 min-w-0 flex-1">
                    <Layers class="size-4 shrink-0 text-primary" />
                    <span class="truncate text-xs font-medium">
                      {{ t("subscription.all_subscriptions") }}
                    </span>
                  </div>
                  <div class="flex items-center gap-1.5 shrink-0">
                    <RefreshCw
                      v-if="subscriptions.some((x) => !x.exclude_from_all && x.running)"
                      class="size-3 text-primary animate-spin"
                    />
                    <Badge
                      variant="secondary"
                      class="h-4.5 px-1.5 text-[10px] font-mono font-normal"
                    >
                      {{ allBadgeText }}
                    </Badge>
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      class="size-6 rounded-md text-muted-foreground hover:text-foreground opacity-50 hover:opacity-100 transition-opacity cursor-pointer"
                      :title="t('subscription.all_filter_settings')"
                      @click.stop="openAllFilterModal"
                    >
                      <SlidersHorizontal class="size-3.5" />
                    </Button>
                  </div>
                </DropdownMenuItem>

                <DropdownMenuSeparator v-if="subscriptions.length > 1" />

                <!-- Individual Subscriptions -->
                <DropdownMenuItem
                  v-for="sub in subscriptions"
                  :key="sub.id"
                  class="group flex items-center justify-between gap-2 py-2 cursor-pointer"
                  :class="selectedId === sub.id ? 'bg-primary/10 text-primary font-medium' : ''"
                  @click="
                    selectedId = sub.id;
                    subDropdownOpen = false;
                  "
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
                    <span class="truncate text-xs font-medium">
                      {{ sub.name }}
                    </span>
                    <span
                      v-if="!sub.enabled"
                      class="size-1.5 rounded-full bg-muted-foreground/50 shrink-0"
                    />
                  </div>

                  <div class="flex items-center gap-1.5 shrink-0">
                    <span
                      v-if="sub.running"
                      class="text-primary flex items-center shrink-0"
                    >
                      <RefreshCw class="size-3 animate-spin" />
                    </span>
                    <span
                      v-else-if="sub.last_error"
                      class="size-1.5 rounded-full bg-destructive shrink-0"
                      :title="sub.last_error"
                    />
                    <span
                      v-else-if="sub.last_run_at"
                      class="text-[11px] text-muted-foreground/70 shrink-0"
                      :title="sub.last_run_at"
                    >
                      {{ formatRelativeTime(sub.last_run_at, currentLocale) }}
                    </span>

                    <Button
                      variant="ghost"
                      size="icon-xs"
                      class="size-6 rounded-md text-muted-foreground hover:text-foreground opacity-50 hover:opacity-100 transition-opacity cursor-pointer"
                      :title="t('subscription.settings')"
                      @click.stop="openSettingsModal(sub)"
                    >
                      <Settings class="size-3.5" />
                    </Button>
                  </div>
                </DropdownMenuItem>

                <div
                  v-if="!subscriptions.length"
                  class="p-3 text-center text-xs text-muted-foreground"
                >
                  {{ t("subscription.no_subscriptions") }}
                </div>
              </DropdownMenuContent>
            </DropdownMenu>

            <!-- View Switcher (New / Reading / All) + Item Count -->
            <ToggleGroup
              type="single"
              v-model="view"
              class="bg-muted/60 p-0.5 rounded-lg border border-border/40"
            >
              <ToggleGroupItem
                value="new"
                class="h-7 px-3 text-xs data-[state=on]:bg-background data-[state=on]:text-foreground data-[state=on]:shadow-xs rounded-md cursor-pointer"
              >
                {{ t("subscription.view_new") }}
              </ToggleGroupItem>
              <ToggleGroupItem
                value="reading"
                class="h-7 px-3 text-xs data-[state=on]:bg-background data-[state=on]:text-foreground data-[state=on]:shadow-xs rounded-md cursor-pointer"
              >
                {{ t("subscription.view_reading") }}
              </ToggleGroupItem>
              <ToggleGroupItem
                value="all"
                class="h-7 px-3 text-xs data-[state=on]:bg-background data-[state=on]:text-foreground data-[state=on]:shadow-xs rounded-md cursor-pointer"
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

          <!-- Right: Search Input and Category Selector (Right-aligned) -->
          <div class="flex items-center gap-2 flex-1 sm:flex-initial justify-end min-w-[200px] ml-auto">
            <div class="relative w-full sm:w-60 md:w-64">
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
                class="h-8 text-xs w-[130px] bg-muted/30 border-border/60 shrink-0"
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
          class="grid gap-4"
          :class="
            pinnedSidebar
              ? 'grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-4 xl:grid-cols-5 2xl:grid-cols-6'
              : 'grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7'
          "
        >
          <div
            v-for="item in items"
            :key="`${item.publication_id || item.gid}:${item.token}`"
            :ref="(el) => observeCard(el, item)"
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
          v-if="allSelected && subscriptions.length > 0 && visibleSubscriptions.length === 0"
          class="rounded-2xl border border-dashed border-border/80 p-12 text-center text-sm text-muted-foreground flex flex-col items-center gap-3"
        >
          <SlidersHorizontal class="size-8 text-muted-foreground/60 stroke-[1.5]" />
          <p class="max-w-md">{{ t("subscription.all_filter_all_hidden_tip") }}</p>
          <Button
            variant="outline"
            size="sm"
            class="gap-1.5 cursor-pointer"
            @click="openAllFilterModal"
          >
            <SlidersHorizontal class="size-3.5" />
            {{ t("subscription.open_filter_settings") }}
          </Button>
        </div>

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

    <!-- Floating Bottom Multi-Select Capsule Toolbar (Reader-Style) -->
    <Transition
      enter-active-class="transition duration-200 ease-out"
      enter-from-class="opacity-0 translate-y-6 scale-95"
      enter-to-class="opacity-100 translate-y-0 scale-100"
      leave-active-class="transition duration-150 ease-in"
      leave-from-class="opacity-100 translate-y-0 scale-100"
      leave-to-class="opacity-0 translate-y-6 scale-95"
    >
      <div
        v-if="isSelecting || selectedItems.size > 0"
        class="fixed bottom-4 sm:bottom-6 inset-x-3 sm:inset-x-6 z-50 pointer-events-none flex justify-center select-none"
      >
        <div
          class="pointer-events-auto flex items-center gap-2 sm:gap-3 rounded-full border border-border/70 bg-background/85 px-3 sm:px-4 py-1.5 text-foreground backdrop-blur-xl shadow-xl ring-1 ring-black/5 dark:ring-white/10"
          @click.stop
        >
          <!-- Left: Page Select Checkbox + Selected Count Badge -->
          <div class="flex items-center gap-2 pl-1 sm:pl-1.5">
            <label
              class="flex items-center gap-2 text-xs font-medium cursor-pointer select-none text-foreground hover:text-primary transition-colors"
            >
              <input
                type="checkbox"
                class="rounded border-border size-4 accent-primary cursor-pointer"
                :checked="allPageSelected"
                @change="togglePageSelection"
              />
              <span class="whitespace-nowrap">{{ t("subscription.select_page") }}</span>
            </label>
            <Badge
              variant="secondary"
              class="h-5 px-2 text-[11px] font-mono font-medium rounded-full"
            >
              {{ t("subscription.selected_count", { count: selectedItems.size }) }}
            </Badge>
          </div>

          <!-- Divider -->
          <div class="h-4 w-px bg-border/60 shrink-0 mx-0.5" />

          <!-- Right: Batch Download Button + Exit Button -->
          <div class="flex items-center gap-1.5 sm:gap-2">
            <Button
              size="sm"
              class="h-7.5 text-xs gap-1.5 rounded-full px-3 shadow-xs cursor-pointer"
              :disabled="!selectedItems.size"
              @click="openBatchDownloadModal"
            >
              <Download class="size-3.5" />
              <span class="whitespace-nowrap">
                {{ t("subscription.batch_download", { count: selectedItems.size }) }}
              </span>
            </Button>

            <Button
              variant="ghost"
              size="icon"
              class="size-7.5 rounded-full text-muted-foreground hover:text-foreground hover:bg-muted/70 cursor-pointer shrink-0"
              :title="t('subscription.exit_select')"
              :aria-label="t('subscription.exit_select')"
              @click="exitSelectMode"
            >
              <X class="size-4" />
            </Button>
          </div>
        </div>
      </div>
    </Transition>

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

    <!-- Batch Download Confirmation Dialog with Quality Selection -->
    <Dialog v-model:open="batchDownloadModalOpen">
      <DialogContent class="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{{ t("subscription.batch_dialog_title") }}</DialogTitle>
          <DialogDescription>
            {{ t("subscription.batch_dialog_desc", { count: selectedPageItems.length }) }}
          </DialogDescription>
        </DialogHeader>

        <div class="space-y-3 pt-2">
          <label class="text-xs font-semibold text-foreground">
            {{ t("subscription.batch_quality_label") }}
          </label>

          <div class="grid grid-cols-1 gap-2">
            <!-- Option 1: Resampled -->
            <label
              class="flex cursor-pointer items-center gap-3 rounded-xl border px-3.5 py-2.5 transition-colors"
              :class="
                batchQuality === 'res'
                  ? 'border-primary bg-primary/5 text-foreground font-medium'
                  : 'border-border/60 bg-muted/20 text-muted-foreground hover:bg-muted/40 hover:text-foreground'
              "
            >
              <input
                type="radio"
                name="batch_quality"
                value="res"
                :checked="batchQuality === 'res'"
                class="size-4 accent-primary cursor-pointer"
                @change="batchQuality = 'res'"
              />
              <span class="text-xs text-foreground">
                {{ t("archive.tier_resample") }}
              </span>
            </label>

            <!-- Option 2: Original -->
            <label
              class="flex cursor-pointer items-center gap-3 rounded-xl border px-3.5 py-2.5 transition-colors"
              :class="
                batchQuality === 'org'
                  ? 'border-primary bg-primary/5 text-foreground font-medium'
                  : 'border-border/60 bg-muted/20 text-muted-foreground hover:bg-muted/40 hover:text-foreground'
              "
            >
              <input
                type="radio"
                name="batch_quality"
                value="org"
                :checked="batchQuality === 'org'"
                class="size-4 accent-primary cursor-pointer"
                @change="batchQuality = 'org'"
              />
              <span class="text-xs text-foreground">
                {{ t("archive.tier_original") }}
              </span>
            </label>
          </div>
        </div>

        <DialogFooter class="gap-2 sm:gap-0 pt-3">
          <Button
            type="button"
            variant="outline"
            class="cursor-pointer"
            @click="batchDownloadModalOpen = false"
          >
            {{ t("common.cancel") }}
          </Button>
          <Button
            type="button"
            class="cursor-pointer"
            :disabled="submittingBatch"
            @click="confirmBatchDownload"
          >
            <RefreshCw
              v-if="submittingBatch"
              class="size-4 animate-spin mr-1.5"
            />
            {{ t("subscription.batch_submit", { count: selectedPageItems.length }) }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

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

          <div class="flex items-start justify-between gap-3 rounded-xl border border-border/60 p-3 bg-muted/20">
            <div class="space-y-0.5">
              <label class="text-xs font-semibold cursor-pointer select-none" for="add-show-in-all">
                {{ t("subscription.show_in_all") }}
              </label>
              <p class="text-[11px] text-muted-foreground leading-relaxed">
                {{ t("subscription.show_in_all_desc") }}
              </p>
            </div>
            <input
              id="add-show-in-all"
              v-model="draft.show_in_all"
              type="checkbox"
              class="rounded border-border size-4 accent-primary cursor-pointer mt-0.5"
            />
          </div>

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


          <div class="flex items-start justify-between gap-3 rounded-xl border border-border/60 p-3 bg-muted/20">
            <div class="space-y-0.5">
              <label class="text-xs font-semibold cursor-pointer select-none" for="settings-show-in-all">
                {{ t("subscription.show_in_all") }}
              </label>
              <p class="text-[11px] text-muted-foreground leading-relaxed">
                {{ t("subscription.show_in_all_desc") }}
              </p>
            </div>
            <input
              id="settings-show-in-all"
              v-model="settingsDraft.show_in_all"
              type="checkbox"
              class="rounded border-border size-4 accent-primary cursor-pointer mt-0.5"
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

    <!-- All Subscriptions Filter Settings Dialog -->
    <Dialog v-model:open="allFilterModalOpen">
      <DialogContent class="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{{
            t("subscription.all_filter_dialog_title")
          }}</DialogTitle>
          <DialogDescription>{{
            t("subscription.all_filter_dialog_desc")
          }}</DialogDescription>
        </DialogHeader>
        <div class="space-y-4 pt-1">
          <!-- Quick Selection Bar -->
          <div
            class="flex items-center justify-between text-xs text-muted-foreground border-b border-border/40 pb-2"
          >
            <span>{{
              t("subscription.all_filter_selected_count", {
                selected: tempSelectedCount,
                total: subscriptions.length,
              })
            }}</span>
            <div class="flex items-center gap-2">
              <button
                type="button"
                class="text-primary hover:underline cursor-pointer font-medium"
                @click="selectAllInFilter(true)"
              >
                {{ t("subscription.all_filter_select_all") }}
              </button>
              <span>·</span>
              <button
                type="button"
                class="text-primary hover:underline cursor-pointer font-medium"
                @click="selectAllInFilter(false)"
              >
                {{ t("subscription.all_filter_deselect_all") }}
              </button>
            </div>
          </div>

          <!-- Subscription Rows List -->
          <div class="max-h-[50vh] overflow-y-auto space-y-1.5 pr-1">
            <div
              v-for="sub in subscriptions"
              :key="sub.id"
              class="flex items-center justify-between p-2.5 rounded-xl border border-border/60 hover:bg-muted/40 transition-colors cursor-pointer select-none"
              @click="toggleSubFilter(sub.id)"
            >
              <div class="flex items-center gap-2.5 min-w-0 flex-1 mr-3">
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
                <span class="text-xs font-medium truncate text-foreground">
                  {{ sub.name }}
                </span>
                <span
                  v-if="!sub.enabled"
                  class="size-1.5 rounded-full bg-muted-foreground/50 shrink-0"
                />
                <span
                  v-if="sub.auto_download"
                  class="text-[10px] text-emerald-600 dark:text-emerald-400 bg-emerald-500/10 px-1.5 py-0.5 rounded font-medium shrink-0"
                >
                  {{ t("subscription.local_archive") }}
                </span>
              </div>
              <input
                type="checkbox"
                class="rounded border-border size-4 accent-primary cursor-pointer shrink-0"
                :checked="Boolean(filterSelectionMap[sub.id])"
                @click.stop="toggleSubFilter(sub.id)"
              />
            </div>
          </div>

          <DialogFooter class="pt-2 flex justify-end gap-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              @click="allFilterModalOpen = false"
            >
              {{ t("common.cancel") }}
            </Button>
            <Button
              type="button"
              size="sm"
              :disabled="savingFilter"
              @click="saveFilterSettings"
            >
              <RefreshCw
                v-if="savingFilter"
                class="size-3.5 mr-1.5 animate-spin"
              />
              {{ t("common.save") }}
            </Button>
          </DialogFooter>
        </div>
      </DialogContent>
    </Dialog>
  </div>
</template>

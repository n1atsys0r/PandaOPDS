<script setup>
import { computed, onMounted, onBeforeUnmount, ref, watch } from "vue";
import { useApp } from "@/state.js";
import { t } from "@/i18n.js";
import PublicationCard from "@/components/PublicationCard.vue";
import DownloadJobsPanel from "@/components/DownloadJobsPanel.vue";
import {
  createSubscriptionItemsController,
  createSubscriptionPayload,
  createSubscriptionUpdate,
  cleanupSubscription,
  submitBatchDownload,
  createSubscriptionPoller,
} from "@/subscription-view.js";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  RefreshCwIcon as RefreshCw,
  PlusIcon as Plus,
  CheckIcon as Check,
  EyeOffIcon as EyeOff,
} from "@lucide/vue";

const s = useApp();
const emit = defineEmits([
  "read-gallery",
  "archive-gallery",
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
const filterCategory = ref("");
const loading = ref(false);
const error = ref("");
const creating = ref(false);
const selectedItems = ref(new Set());
const batchQuality = ref("res");
const batchResult = ref("");
const draft = ref({
  name: "",
  source: "favorites",
  keywords: "",
  categories: "",
  query_categories: "",
  favorite_categories: "",
  sort: "newest",
  preset: "latest",
  interval: 3600,
});
const settingsDraft = ref({});
const itemsController = createSubscriptionItemsController(s.api);
const detail = ref(null);
const settingsId = ref("");
const settingsDirty = ref(false);
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
watch(
  selected,
  (sub) => {
    if (!sub || allSelected.value) {
      settingsDraft.value = {};
      return;
    }
    if (settingsId.value === sub.id && settingsDirty.value) return;
    settingsId.value = sub.id;
    settingsDirty.value = false;
    settingsDraft.value = {
      name: sub.name || "",
      keywords: sub.query?.keywords || "",
      query_categories: (sub.query?.categories || []).join(","),
      favorite_categories: (sub.query?.favorite_categories || []).join(","),
      sort: sub.query?.sort || "newest",
      preset: sub.query?.preset || "latest",
      max_pages: sub.query?.max_pages || 10,
      interval_seconds: sub.interval_seconds ?? 3600,
      enabled: sub.enabled !== false,
      auto_download: Boolean(sub.auto_download),
      quality: sub.download_rules?.quality || "res",
      categories: (sub.download_rules?.categories || []).join(","),
      excluded_categories: (sub.download_rules?.excluded_categories || []).join(
        ",",
      ),
    };
  },
  { immediate: true },
);
const poller = createSubscriptionPoller({
  loadSubscriptions,
  loadItems,
  getSubscriptions: () => subscriptions.value,
});

async function loadSubscriptions() {
  try {
    const { data } = await s.api("/api/subscriptions");
    subscriptions.value = data.items || [];
    if (!selectedId.value && subscriptions.value.length)
      selectedId.value =
        subscriptions.value.length > 1 ? "all" : subscriptions.value[0].id;
    if (
      selectedId.value &&
      selectedId.value !== "all" &&
      !subscriptions.value.some((x) => x.id === selectedId.value)
    )
      selectedId.value =
        subscriptions.value.length > 1
          ? "all"
          : subscriptions.value[0]?.id || "";
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
    const response = await itemsController.load({
      subscriptionId: selectedId.value,
      subscriptionIds: ids,
      view: view.value,
      page: page.value,
      perPage,
      q: filterText.value,
      category: filterCategory.value,
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
  detail.value = null;
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
    draft.value = {
      name: "",
      source: "favorites",
      keywords: "",
      categories: "",
      query_categories: "",
      favorite_categories: "",
      sort: "newest",
      preset: "latest",
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
async function setAutoDownload(enabled) {
  if (!selected.value) return;
  if (enabled && !window.confirm(t("subscription.auto_download_confirm")))
    return;
  try {
    const { data } = await s.api(
      `/api/subscriptions/${encodeURIComponent(selectedId.value)}`,
      { method: "PATCH", body: JSON.stringify({ auto_download: enabled }) },
    );
    subscriptions.value = subscriptions.value.map((x) =>
      x.id === data.id ? data : x,
    );
  } catch (e) {
    error.value = e.message;
  }
}
async function saveSettings() {
  if (!selected.value) return;
  if (
    settingsDraft.value.auto_download &&
    !selected.value.auto_download &&
    !window.confirm(t("subscription.auto_download_confirm"))
  )
    return;
  try {
    const { data } = await s.api(
      `/api/subscriptions/${encodeURIComponent(selected.value.id)}`,
      {
        method: "PATCH",
        body: JSON.stringify(
          createSubscriptionUpdate(selected.value, settingsDraft.value),
        ),
      },
    );
    subscriptions.value = subscriptions.value.map((x) =>
      x.id === data.id ? data : x,
    );
    settingsDirty.value = false;
    settingsId.value = data.id;
    settingsDraft.value = {
      ...settingsDraft.value,
      keywords: data.query?.keywords || "",
      query_categories: (data.query?.categories || []).join(","),
      favorite_categories: (data.query?.favorite_categories || []).join(","),
      sort: data.query?.sort || "newest",
    };
  } catch (e) {
    error.value = e.message;
  }
}
async function deleteSelected() {
  if (!selected.value || !window.confirm(t("subscription.delete_confirm")))
    return;
  try {
    await s.api(`/api/subscriptions/${encodeURIComponent(selected.value.id)}`, {
      method: "DELETE",
    });
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
    await loadItems();
  } catch (e) {
    error.value = e.message;
  }
}
function badgeFor(item) {
  if (item.download_status)
    return { label: item.download_status, variant: "secondary" };
  if (item.archived)
    return { label: t("subscription.local_archive"), variant: "outline" };
  return null;
}
function openDetail(item) {
  detail.value = item;
}
function readItem(item) {
  detail.value = null;
  emit("read-gallery", {
    gid: item.gid,
    token: item.token,
    title: item.title,
    page_count: item.page_count || 0,
    reading_order: [],
    status: "unarchived",
  });
}
function favoriteItem(item) {
  detail.value = null;
  emit("favorite-gallery", { pub: item, action: "add", favcat: 0 });
}
function downloadItem(item) {
  detail.value = null;
  emit("archive-gallery", item);
}
</script>

<template>
  <section class="space-y-5 p-4 sm:p-6">
    <header class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <small class="text-muted-foreground">{{
          t("subscription.header")
        }}</small>
        <h1 class="text-2xl font-semibold">{{ t("subscription.title") }}</h1>
        <p class="mt-1 text-sm text-muted-foreground">
          {{ t("subscription.description") }}
        </p>
        <p
          v-if="selected?.last_success_at"
          class="mt-1 text-xs text-muted-foreground"
        >
          {{ t("subscription.last_success") }}: {{ selected.last_success_at }}
        </p>
      </div>
      <Button
        variant="outline"
        :disabled="!targets.length || running"
        @click="runNow"
        ><RefreshCw class="size-4" :class="{ 'animate-spin': running }" />{{
          running ? t("subscription.running") : t("subscription.run_now")
        }}</Button
      >
    </header>
    <div class="grid gap-4 lg:grid-cols-[260px_minmax(0,1fr)]">
      <aside class="space-y-3 rounded-lg border bg-card p-3">
        <label class="text-xs font-medium">{{
          t("subscription.choose")
        }}</label>
        <Select v-model="selectedId"
          ><SelectTrigger
            ><SelectValue
              :placeholder="
                t('subscription.no_subscriptions')
              " /></SelectTrigger
          ><SelectContent
            ><SelectItem v-if="subscriptions.length > 1" value="all">{{
              t("subscription.all_subscriptions")
            }}</SelectItem
            ><SelectItem
              v-for="sub in subscriptions"
              :key="sub.id"
              :value="sub.id"
              >{{ sub.name }}</SelectItem
            ></SelectContent
          ></Select
        >
        <details class="rounded-md border p-3">
          <summary class="cursor-pointer text-sm font-medium">
            <Plus class="mr-1 inline size-4" />{{ t("subscription.add") }}
          </summary>
          <form class="mt-3 space-y-2" @submit.prevent="createSubscription">
            <Input
              v-model="draft.name"
              required
              :placeholder="t('subscription.name')"
            />
            <Select v-model="draft.source"
              ><SelectTrigger><SelectValue /></SelectTrigger
              ><SelectContent
                ><SelectItem value="favorites">{{
                  t("subscription.source_favorites")
                }}</SelectItem
                ><SelectItem value="preset">{{
                  t("subscription.source_preset")
                }}</SelectItem
                ><SelectItem value="search">{{
                  t("subscription.source_search")
                }}</SelectItem></SelectContent
              ></Select
            >
            <p
              v-if="draft.source === 'preset'"
              class="rounded bg-muted px-2 py-1.5 text-xs text-muted-foreground"
            >
              {{ t("subscription.preset_latest") }}
            </p>
            <Input
              v-if="draft.source === 'favorites'"
              v-model="draft.favorite_categories"
              :placeholder="t('subscription.favorite_categories')"
            />
            <template v-if="draft.source === 'search'"
              ><Input
                v-model="draft.keywords"
                :placeholder="t('subscription.keywords')" /><Input
                v-model="draft.query_categories"
                :placeholder="t('subscription.search_categories')"
            /></template>
            <label class="block text-xs text-muted-foreground"
              >{{ t("subscription.interval")
              }}<Input
                v-model.number="draft.interval"
                type="number"
                min="0"
                class="mt-1"
            /></label>
            <p class="text-xs text-muted-foreground">
              {{ t("subscription.auto_download_off") }}
            </p>
            <Button type="submit" class="w-full" :disabled="creating"
              ><Plus class="size-4" />{{ t("subscription.create") }}</Button
            >
          </form>
        </details>
        <div
          v-if="allSelected"
          class="border-t pt-3 text-xs text-muted-foreground"
        >
          <p class="font-medium">{{ t("subscription.all_subscriptions") }}</p>
          <p v-for="sub in subscriptions" :key="sub.id" class="mt-1">
            {{ sub.name }} ·
            {{
              sub.running
                ? t("subscription.running")
                : sub.last_run_at || t("subscription.never_run")
            }}<span v-if="sub.last_error" class="text-destructive">
              · {{ sub.last_error }}</span
            >
          </p>
        </div>
        <div
          v-if="selected"
          class="border-t pt-3 text-xs text-muted-foreground"
        >
          <p>
            {{ t("subscription.last_run") }}:
            {{ selected.last_run_at || t("common.unknown") }}
          </p>
          <p v-if="selected.last_error" class="mt-1 text-destructive">
            {{ selected.last_error }}
          </p>
          <p v-else-if="selected.baseline_complete" class="mt-1">
            {{ t("subscription.baseline_ready") }}
          </p>
          <details class="mt-3 border-t pt-3">
            <summary class="cursor-pointer font-medium text-foreground">
              {{ t("subscription.settings") }}
            </summary>
            <form
              class="mt-3 space-y-2"
              @input="settingsDirty = true"
              @change="settingsDirty = true"
              @submit.prevent="saveSettings"
            >
              <Input
                v-model="settingsDraft.name"
                :placeholder="t('subscription.name')"
              /><label class="block"
                >{{ t("subscription.interval")
                }}<Input
                  v-model.number="settingsDraft.interval_seconds"
                  type="number"
                  min="0"
                  class="mt-1" /></label
              ><label v-if="selected.source === 'search'" class="block"
                >{{ t("subscription.keywords")
                }}<Input v-model="settingsDraft.keywords" class="mt-1" /><Input
                  v-model="settingsDraft.query_categories"
                  :placeholder="t('subscription.search_categories')"
                  class="mt-1"
              /></label>
              <label v-if="selected.source === 'favorites'" class="block"
                >{{ t("subscription.favorite_categories")
                }}<Input
                  v-model="settingsDraft.favorite_categories"
                  class="mt-1"
              /></label>
              <p v-if="selected.source === 'preset'" class="text-xs">
                {{ t("subscription.preset_latest") }}
              </p>
              <label class="block"
                >{{ t("subscription.max_pages")
                }}<Input
                  v-model.number="settingsDraft.max_pages"
                  type="number"
                  min="1"
                  max="50"
                  class="mt-1" /></label
              ><label class="flex gap-2"
                ><input v-model="settingsDraft.enabled" type="checkbox" />{{
                  t("subscription.enabled")
                }}</label
              ><label class="flex items-start gap-2"
                ><input
                  v-model="settingsDraft.auto_download"
                  type="checkbox"
                  class="mt-0.5"
                /><span>{{
                  settingsDraft.auto_download
                    ? t("subscription.auto_download_on")
                    : t("subscription.auto_download_off")
                }}</span></label
              >
              <div
                v-if="settingsDraft.auto_download"
                class="space-y-2 rounded-md bg-muted/50 p-2"
              >
                <label class="block"
                  >{{ t("subscription.quality")
                  }}<select
                    v-model="settingsDraft.quality"
                    class="mt-1 h-9 w-full rounded-md border bg-background px-3"
                  >
                    <option value="res">
                      {{ t("archive.tier_resample") }}
                    </option>
                    <option value="org">
                      {{ t("archive.tier_original") }}
                    </option>
                  </select></label
                ><label v-if="selected.source === 'favorites'" class="block"
                  >{{ t("subscription.categories")
                  }}<Input
                    v-model="settingsDraft.categories"
                    class="mt-1" /></label
                ><label v-if="selected.source === 'favorites'" class="block"
                  >{{ t("subscription.excluded_categories")
                  }}<Input
                    v-model="settingsDraft.excluded_categories"
                    class="mt-1"
                /></label>
                <p>{{ t("subscription.auto_download_confirm") }}</p>
              </div>
              <div class="flex gap-2">
                <Button type="submit" size="sm">{{ t("common.save") }}</Button
                ><Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  class="text-destructive"
                  @click="deleteSelected"
                  >{{ t("common.delete") }}</Button
                >
              </div>
            </form>
          </details>
        </div>
      </aside>
      <main class="min-w-0 space-y-4">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <div class="inline-flex rounded-md border p-0.5">
            <button
              v-for="tab in ['unread', 'all', 'hidden']"
              :key="tab"
              class="rounded px-3 py-1.5 text-sm"
              :class="
                view === tab
                  ? 'bg-accent text-accent-foreground'
                  : 'text-muted-foreground'
              "
              @click="
                page = 1;
                view = tab;
              "
            >
              {{ t(`subscription.view_${tab}`) }}
            </button>
          </div>
          <span class="text-xs text-muted-foreground">{{
            t("subscription.item_count", { count: total })
          }}</span>
        </div>
        <div class="grid gap-2 sm:grid-cols-[1fr_220px]">
          <Input
            v-model="filterText"
            :placeholder="t('subscription.filter_local')"
          /><Input
            v-model="filterCategory"
            :placeholder="t('subscription.filter_category')"
          />
        </div>
        <div
          v-if="runInfo"
          class="rounded-md border bg-muted/30 p-3 text-xs text-muted-foreground"
        >
          <p>
            {{ t("subscription.run_diagnostics") }}: {{ runInfo.status }} ·
            {{ runInfo.pages }} {{ t("subscription.run_pages") }} ·
            {{ runInfo.results }} {{ t("subscription.run_results") }} ·
            {{ t(`subscription.coverage_${runInfo.coverage || "unknown"}`) }}
          </p>
          <p v-if="runInfo.coverage === 'partial'" class="mt-1">
            {{ t("subscription.partial_run") }}
          </p>
          <p v-else-if="runInfo.coverage === 'window'" class="mt-1">
            {{ t("subscription.window_run") }}
          </p>
        </div>
        <div class="flex flex-wrap items-center gap-2 border-t pt-3">
          <label class="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              :checked="allPageSelected"
              @change="togglePageSelection"
            />
            {{ t("subscription.select_page") }}
          </label>
          <span class="text-xs text-muted-foreground">{{
            t("subscription.selected_count", { count: selectedItems.size })
          }}</span>
          <select
            v-model="batchQuality"
            class="h-9 rounded-md border bg-background px-2 text-sm"
          >
            <option value="res">{{ t("archive.tier_resample") }}</option>
            <option value="org">{{ t("archive.tier_original") }}</option>
          </select>
          <Button
            size="sm"
            variant="outline"
            :disabled="!selectedItems.size"
            @click="batchDownload"
          >
            {{
              t("subscription.batch_download", { count: selectedItems.size })
            }}
          </Button>
          <Button
            size="sm"
            variant="ghost"
            :disabled="!targets.length"
            @click="cleanupHistory('inspected')"
            >{{ t("subscription.cleanup_inspected") }}</Button
          >
          <Button
            size="sm"
            variant="ghost"
            :disabled="!targets.length"
            @click="cleanupHistory('hidden')"
            >{{ t("subscription.cleanup_hidden") }}</Button
          >
        </div>
        <p v-if="batchResult" class="text-xs text-muted-foreground">
          {{ batchResult }}
        </p>
        <p
          v-if="error"
          role="alert"
          class="rounded-md border border-destructive/40 p-3 text-sm text-destructive"
        >
          {{ error }}
        </p>
        <p
          v-if="!subscriptions.length && !error"
          class="rounded-lg border p-8 text-center text-sm text-muted-foreground"
        >
          {{ t("subscription.no_subscriptions") }}
        </p>
        <div
          v-else-if="loading"
          class="py-12 text-center text-sm text-muted-foreground"
        >
          {{ t("common.loading") }}
        </div>
        <div
          v-else-if="items.length"
          class="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 xl:grid-cols-6"
        >
          <div
            v-for="item in items"
            :key="`${item.publication_id || item.gid}:${item.token}`"
            class="space-y-2"
          >
            <PublicationCard
              :publication="item"
              :badge="badgeFor(item)"
              @select="openDetail(item)"
            />
            <label class="flex items-center gap-2 text-xs">
              <input
                type="checkbox"
                :checked="selectedItems.has(itemKey(item))"
                @click.stop
                @change="toggleItem(item)"
              />
              {{ t("subscription.select_item") }}
            </label>
            <div class="flex gap-1">
              <Button
                size="sm"
                variant="outline"
                class="h-7 flex-1 text-xs"
                :disabled="item.inspected"
                @click="setItem(item, { inspected: true })"
                ><Check class="size-3" />{{
                  t("subscription.mark_inspected")
                }}</Button
              ><Button
                size="sm"
                variant="ghost"
                class="h-7 px-2"
                :aria-label="
                  item.hidden
                    ? t('subscription.unhide')
                    : t('subscription.hide')
                "
                @click="setItem(item, { hidden: !item.hidden })"
                ><EyeOff class="size-3"
              /></Button>
            </div>
          </div>
        </div>
        <p
          v-else-if="subscriptions.length && selectedId"
          class="rounded-lg border p-8 text-center text-sm text-muted-foreground"
        >
          {{ t("subscription.empty") }}
        </p>
        <div v-if="pages > 1" class="flex items-center justify-center gap-3">
          <Button
            variant="outline"
            size="sm"
            :disabled="page <= 1"
            @click="page--"
            >{{ t("common.previous") }}</Button
          ><span class="text-xs text-muted-foreground"
            >{{ page }} / {{ pages }}</span
          ><Button
            variant="outline"
            size="sm"
            :disabled="page >= pages"
            @click="page++"
            >{{ t("common.next") }}</Button
          >
        </div>
      </main>
    </div>
    <DownloadJobsPanel />
    <div
      v-if="detail"
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
      role="presentation"
      @click.self="detail = null"
    >
      <section
        role="dialog"
        aria-modal="true"
        class="w-full max-w-xl space-y-4 rounded-xl border bg-background p-5 shadow-xl"
      >
        <div class="flex items-start justify-between gap-3">
          <div>
            <small class="text-muted-foreground">{{ detail.category }}</small>
            <h2 class="text-lg font-semibold">{{ detail.title }}</h2>
            <p class="text-xs text-muted-foreground">
              {{ detail.language }} · {{ detail.page_count || 0 }}
              {{ t("common.pages") }}
            </p>
          </div>
          <Button variant="ghost" size="sm" @click="detail = null">{{
            t("common.close")
          }}</Button>
        </div>
        <img
          v-if="detail.cover_url"
          :src="detail.cover_url"
          :alt="detail.title"
          class="max-h-72 max-w-full rounded-md object-contain"
        />
        <div v-if="detail.tags" class="flex flex-wrap gap-1.5">
          <span
            v-for="tag in Array.isArray(detail.tags)
              ? detail.tags
              : Object.values(detail.tags).flat()"
            :key="
              typeof tag === 'string'
                ? tag
                : `${tag.namespace}:${tag.key || tag.name}`
            "
            class="rounded-full bg-muted px-2 py-1 text-xs"
            >{{
              typeof tag === "string"
                ? tag
                : `${tag.namespace ? tag.namespace + ":" : ""}${tag.key || tag.name}`
            }}</span
          >
        </div>
        <p class="text-xs text-muted-foreground">
          {{ t("subscription.first_seen") }}: {{ detail.first_seen_at }}
        </p>
        <div class="flex flex-wrap gap-2">
          <Button :disabled="!detail.page_count" @click="readItem(detail)">{{
            t("documents.read")
          }}</Button
          ><Button
            variant="outline"
            @click="
              setItem(detail, { inspected: true });
              detail = null;
            "
            >{{ t("subscription.mark_inspected") }}</Button
          ><Button variant="outline" @click="favoriteItem(detail)">{{
            t("subscription.favorite")
          }}</Button
          ><Button variant="outline" @click="downloadItem(detail)">{{
            t("subscription.manual_download")
          }}</Button>
        </div>
      </section>
    </div>
  </section>
</template>

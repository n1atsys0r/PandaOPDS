<script setup>
import { computed, onMounted, onBeforeUnmount, ref, watch } from "vue";
import { useApp } from "@/state.js";
import { t } from "@/i18n.js";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Skeleton } from "@/components/ui/skeleton";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from "@/components/ui/dropdown-menu";
import CoverImage from "@/components/CoverImage.vue";
import DownloadJobsPanel from "@/components/DownloadJobsPanel.vue";
import {
  documentReadErrorMessage,
  prepareDocumentForReading,
} from "@/document-reader.js";
import {
  BookOpenIcon as BookOpen,
  LinkIcon as Link,
  Link2OffIcon as LinkOff,
  RefreshCwIcon as RefreshCw,
  Trash2Icon as Trash2,
  CircleAlertIcon as CircleAlert,
  PlusIcon as Plus,
  SearchIcon as Search,
  InfoIcon as Info,
  MoreVerticalIcon as MoreVertical,
  FileSearchIcon as FileSearch,
} from "@lucide/vue";

const emit = defineEmits(["read-document"]);
const s = useApp();
const documents = ref([]);
const total = ref(0);
const page = ref(1);
const perPage = 50;
const linked = ref("all");
const status = ref("all");
const filterText = ref("");
const loading = ref(false);
const error = ref("");
const scan = ref(null);
const scanReportOpen = ref(false);
const associateDialogOpen = ref(false);
const associateDoc = ref(null);
const associateInput = ref("");
const association = ref({ gid: "", token: "" });
const createArchiveDialogOpen = ref(false);
const pages = computed(() => Math.max(1, Math.ceil(total.value / perPage)));
const displayedDocuments = computed(() => {
  const q = filterText.value.trim().toLowerCase();
  if (!q) return documents.value;
  return documents.value.filter((d) => {
    const title = (d.title || "").toLowerCase();
    const filename = (d.filename || "").toLowerCase();
    const id = String(d.id || "").toLowerCase();
    const gid = String(d.gid || "");
    return (
      title.includes(q) ||
      filename.includes(q) ||
      id.includes(q) ||
      gid.includes(q)
    );
  });
});
let documentRequest = 0;
let previousScanStatus = "idle";
let activeScanID = "";
let poll;

async function loadDocuments() {
  const request = ++documentRequest;
  loading.value = true;
  error.value = "";
  try {
    const query = new URLSearchParams({
      page: String(page.value),
      per_page: String(perPage),
      status: status.value,
    });
    if (linked.value !== "all") query.set("linked", linked.value);
    const { data } = await s.api(`/api/documents?${query}`);
    if (request !== documentRequest) return;
    documents.value = data.items || [];
    total.value = Number(data.total || 0);
  } catch (e) {
    if (request === documentRequest) error.value = e.message;
  } finally {
    if (request === documentRequest) loading.value = false;
  }
}
async function startScan(deep = false) {
	if (deep && !window.confirm(t("documents.deep_confirm"))) return;
  error.value = "";
  try {
    const { data } = await s.api("/api/archive/import/scan", {
      method: "POST",
      ...(deep
        ? { body: JSON.stringify({ deep: true, acknowledged: true }) }
        : {}),
    });
    scan.value = data;
    activeScanID = data.id || data.run_id || "";
    previousScanStatus = data.status || "running";
    if (previousScanStatus !== "running") {
      await loadDocuments();
    }
  } catch (e) {
    error.value = e.message;
  }
}
async function loadScan() {
  try {
    const { data } = await s.api("/api/archive/import/scan");
    const old = previousScanStatus;
    const runID = data.id || data.run_id || "";
    if (!activeScanID && data.status === "running") activeScanID = runID;
    previousScanStatus = data.status || "idle";
    scan.value = data;
    if (
      old === "running" &&
      data.status !== "running" &&
      (!activeScanID || !runID || activeScanID === runID)
    ) {
      activeScanID = "";
      await loadDocuments();
    }
  } catch {
    /* keep the last scan state visible */
  }
}
watch([page, linked, status], loadDocuments);
onMounted(() => {
  loadDocuments();
  loadScan();
  poll = setInterval(() => {
    if (scan.value?.status === "running") loadScan();
  }, 2500);
});
onBeforeUnmount(() => {
  clearInterval(poll);
  documentRequest++;
});
function parseGalleryInput(input) {
  const str = String(input || "").trim();
  let m =
    str.match(/\/(?:g|mpv)\/(\d+)\/([0-9a-fA-F]+)/) ||
    str.match(/(?:gid|g)=?(\d+)[^0-9a-fA-F]+(?:token|t)=?([0-9a-fA-F]+)/) ||
    str.match(/^(\d+)[\s,;\/]+([0-9a-fA-F]{6,})\/?$/);
  return m ? { gid: Number(m[1]), token: m[2] } : null;
}

function openAssociateDialog(doc) {
  associateDoc.value = doc;
  associateInput.value = doc.gid && doc.token ? `${doc.gid} ${doc.token}` : "";
  association.value = {
    gid: doc.gid || "",
    token: doc.token || "",
  };
  associateDialogOpen.value = true;
}

function onAssociateInput() {
  const parsed = parseGalleryInput(associateInput.value);
  if (parsed) {
    association.value.gid = parsed.gid;
    association.value.token = parsed.token;
  }
}

async function saveAssociation() {
  if (!associateDoc.value) return;
  const parsed = parseGalleryInput(associateInput.value);
  const gid = parsed ? parsed.gid : Number(association.value.gid || 0);
  const token = parsed ? parsed.token : association.value.token.trim();
  if (!gid || !token) {
    error.value = t("notice.invalid_input") || "Invalid GID or Token";
    return;
  }
  error.value = "";
  try {
    await s.api(
      `/api/documents/${encodeURIComponent(associateDoc.value.id)}/associate`,
      {
        method: "POST",
        body: JSON.stringify({ gid, token }),
      },
    );
    associateDialogOpen.value = false;
    await loadDocuments();
  } catch (e) {
    error.value = e.message;
  }
}

async function removeLink(doc) {
  if (!window.confirm(t("documents.remove_association_confirm"))) return;
  error.value = "";
  try {
    await s.api(`/api/documents/${encodeURIComponent(doc.id)}/associate`, {
      method: "POST",
      body: JSON.stringify({ gid: 0, token: "" }),
    });
    await loadDocuments();
  } catch (e) {
    error.value = e.message;
  }
}

function openCreateArchiveDialog() {
  s.archiveInput.value = "";
  s.quote.value = null;
  s.selectedQuality.value = "";
  createArchiveDialogOpen.value = true;
}

function closeCreateArchiveDialog() {
  createArchiveDialogOpen.value = false;
  s.archiveInput.value = "";
  s.quote.value = null;
  s.selectedQuality.value = "";
}

async function confirmAndStartArchive() {
  await s.startArchive();
  createArchiveDialogOpen.value = false;
}
async function deleteDocument(doc) {
  if (!window.confirm(t("documents.delete_confirm"))) return;
  try {
    await s.api(`/api/documents/${encodeURIComponent(doc.id)}`, {
      method: "DELETE",
    });
    await loadDocuments();
  } catch (e) {
    error.value = e.message;
  }
}
async function readDocument(doc) {
  try {
    const ready = await prepareDocumentForReading(doc, s.api);
    emit("read-document", ready);
  } catch (e) {
    error.value = documentReadErrorMessage(e, t);
  }
}

function getDiagnosticLabel(reason) {
  const key = `documents.diagnostics_keys.${reason}`;
  const label = t(key);
  return label !== key ? label : reason;
}
</script>

<template>
  <div class="documents-view flex flex-col gap-6">
    <div class="heading">
      <div>
        <small>{{ t("documents.header") }}</small>
        <h1>{{ t("documents.title") }}</h1>
        <p>{{ t("documents.description") }}</p>
      </div>
      <div class="heading-actions">
        <Button
          class="gap-1.5"
          @click="openCreateArchiveDialog"
        >
          <Plus class="size-4" data-icon="inline-start" />
          {{ t("archive.new_archive") }}
        </Button>
        <Button
          variant="outline"
          class="gap-1.5"
          :disabled="scan?.status === 'running'"
          @click="startScan(false)"
        >
          <RefreshCw
            class="size-4"
            :class="{ 'animate-spin': scan?.status === 'running' }"
            data-icon="inline-start"
          />
          {{ scan?.status === "running" ? t("documents.scanning") : t("documents.scan") }}
        </Button>
      </div>
    </div>

    <Alert v-if="error" variant="destructive">
      <CircleAlert class="size-4" />
      <AlertDescription>{{ error }}</AlertDescription>
    </Alert>

    <!-- 仅在正在扫描或扫描报错时显示的动态提示条，空闲/正常时零垂直空间占用 -->
    <div
      v-if="scan?.status === 'running'"
      class="flex items-center justify-between gap-3 rounded-xl border border-primary/20 bg-primary/5 px-4 py-2.5 text-xs text-primary shadow-xs"
    >
      <div class="flex items-center gap-2">
        <RefreshCw class="size-3.5 animate-spin text-primary" data-icon="inline-start" />
        <span class="font-medium">
          {{ scan.deep ? t("documents.deep_scan") : t("documents.scan") }}
          {{ t("documents.scanning") }}...
        </span>
        <span v-if="scan.scanned != null || scan.imported != null" class="text-muted-foreground">
          ({{ scan.scanned ?? scan.imported }} {{ t("documents.scanned") }})
        </span>
      </div>
      <Button
        variant="ghost"
        size="sm"
        class="h-6 px-2 text-xs"
        @click="scanReportOpen = true"
      >
        {{ t("documents.scan_report") }}
      </Button>
    </div>
    <Alert v-else-if="scan?.status === 'error' || scan?.error" variant="destructive">
      <CircleAlert class="size-4" />
      <div class="flex flex-1 items-center justify-between gap-2">
        <AlertDescription>{{ scan.error || 'Scan failed' }}</AlertDescription>
        <Button
          variant="outline"
          size="sm"
          class="h-6 px-2 text-xs"
          @click="scanReportOpen = true"
        >
          {{ t("documents.scan_report") }}
        </Button>
      </div>
    </Alert>

    <!-- 规范化单行卡片工具栏（完全对齐 SubscriptionsView 结构与视觉层次） -->
    <div
      class="p-3 rounded-2xl border border-border/60 bg-card shadow-xs flex flex-wrap items-center justify-between gap-3"
    >
      <!-- 左侧：主要分类分段器 (全部 / 已关联 / 未关联) + 计数 Badge -->
      <div class="flex items-center gap-2.5">
        <ToggleGroup
          type="single"
          :model-value="linked"
          @update:model-value="(val) => { if (val) { page = 1; linked = val; } }"
          class="bg-muted/60 p-0.5 rounded-lg border border-border/40"
        >
          <ToggleGroupItem
            value="all"
            class="h-7 px-3 text-xs data-[state=on]:bg-background data-[state=on]:text-foreground data-[state=on]:shadow-xs rounded-md"
          >
            {{ t("common.all") }}
          </ToggleGroupItem>
          <ToggleGroupItem
            value="linked"
            class="h-7 px-3 text-xs data-[state=on]:bg-background data-[state=on]:text-foreground data-[state=on]:shadow-xs rounded-md"
          >
            {{ t("documents.linked") }}
          </ToggleGroupItem>
          <ToggleGroupItem
            value="unlinked"
            class="h-7 px-3 text-xs data-[state=on]:bg-background data-[state=on]:text-foreground data-[state=on]:shadow-xs rounded-md"
          >
            {{ t("documents.unlinked") }}
          </ToggleGroupItem>
        </ToggleGroup>

        <Badge
          variant="secondary"
          class="h-6 px-2 text-xs font-mono font-normal"
        >
          {{ filterText ? `${displayedDocuments.length} / ${total}` : t("subscription.item_count", { count: total }) }}
        </Badge>
      </div>

      <!-- 中间：搜索框与次要状态下拉筛选组合（撑起呼吸感，告别空旷断层） -->
      <div class="flex items-center gap-2 flex-1 max-w-md min-w-[220px]">
        <div class="relative flex-1">
          <Search
            class="absolute left-2.5 top-2.5 size-3.5 text-muted-foreground pointer-events-none"
          />
          <Input
            v-model="filterText"
            class="h-8 pl-8 pr-2.5 text-xs bg-muted/30 border-border/60 focus-visible:bg-background"
            :placeholder="t('documents.search_placeholder')"
          />
        </div>
        <Select
          :model-value="status"
          @update:model-value="(val) => { if (val) { page = 1; status = val; } }"
        >
          <SelectTrigger class="h-8 text-xs w-[130px] shrink-0 bg-muted/30 border-border/60">
            <SelectValue :placeholder="t('documents.status_all')" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem
              v-for="tab in ['all', 'available', 'missing', 'invalid']"
              :key="tab"
              :value="tab"
            >
              {{ t(`documents.status_${tab}`) }}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>

      <!-- 右侧：下载任务面板 + 更多操作下拉菜单 (Ellipsis) -->
      <div class="flex items-center gap-2">
        <DownloadJobsPanel />

        <DropdownMenu>
          <DropdownMenuTrigger as-child>
            <Button
              variant="outline"
              size="sm"
              class="h-8 size-8 p-0"
              :aria-label="t('common.more_options')"
            >
              <MoreVertical class="size-4" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem
              :disabled="scan?.status === 'running'"
              @click="startScan(true)"
            >
              <FileSearch class="size-4 mr-2" />
              {{ t("documents.deep_scan") }}
            </DropdownMenuItem>
            <DropdownMenuItem @click="scanReportOpen = true">
              <Info class="size-4 mr-2" />
              {{ t("documents.scan_report") }}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </div>

    <div
      v-if="loading"
      class="grid gap-3 sm:grid-cols-2 xl:grid-cols-3"
    >
      <div
        v-for="i in 6"
        :key="i"
        class="flex flex-col justify-between rounded-2xl border border-border/60 bg-card p-4 shadow-xs"
      >
        <div class="flex items-start gap-3.5">
          <Skeleton class="aspect-[1/1.414] w-16 shrink-0 rounded-md" />
          <div class="flex-1 space-y-2">
            <Skeleton class="h-4 w-3/4" />
            <Skeleton class="h-3 w-1/2" />
            <Skeleton class="h-3 w-1/3 mt-2" />
          </div>
        </div>
        <div class="mt-4 flex gap-2 border-t border-border/40 pt-3">
          <Skeleton class="h-8 w-16 rounded-full" />
          <Skeleton class="h-8 w-24 rounded-full" />
        </div>
      </div>
    </div>

    <div
      v-else-if="displayedDocuments.length"
      class="grid gap-3 sm:grid-cols-2 xl:grid-cols-3"
    >
      <Card
        v-for="doc in displayedDocuments"
        :key="doc.id"
        size="sm"
        class="group/doc min-w-0 p-4 justify-between transition-all hover:border-border hover:shadow-sm"
      >
        <div class="flex items-start gap-3.5">
          <div
            v-if="doc.cover_url"
            class="relative aspect-[1/1.414] w-16 shrink-0 overflow-hidden rounded-md border border-border/60 bg-muted/40 shadow-xs"
          >
            <CoverImage
              :src="doc.cover_url"
              :alt="doc.title"
            />
          </div>
          <div
            v-else
            class="relative aspect-[1/1.414] w-16 shrink-0 flex items-center justify-center rounded-md border border-dashed border-border/70 bg-muted/20 text-muted-foreground/40 select-none"
          >
            <BookOpen class="size-6 stroke-[1.5]" />
          </div>

          <div class="min-w-0 flex-1">
            <div class="flex items-start justify-between gap-2">
              <div class="min-w-0">
                <h2
                  class="line-clamp-2 font-medium text-sm text-foreground leading-snug"
                  :title="doc.title || doc.path"
                >
                  {{ doc.title || doc.path }}
                </h2>
                <p class="mt-1 break-all text-xs font-mono text-muted-foreground">
                  {{ doc.path }}
                </p>
              </div>
              <Badge
                variant="secondary"
                class="shrink-0 text-[10px] font-mono uppercase px-1.5 py-0.5"
              >
                {{ doc.format }}
              </Badge>
            </div>
            <p class="mt-2 text-xs text-muted-foreground">
              <template v-if="doc.page_count > 0">{{ t("documents.pages", { count: doc.page_count }) }}</template>
              <template v-else>{{ t("documents.page_count_unknown") }}</template>
              · {{ t(`documents.status_${doc.status || "available"}`) }}
              <span v-if="doc.gid"> · EH {{ doc.gid }}</span>
              <span v-else> · {{ t("documents.unlinked") }}</span>
            </p>
            <p
              v-if="doc.association_source"
              class="mt-1 text-[11px] text-muted-foreground font-mono"
            >
              {{ t("documents.association_source") }}: {{ doc.association_source }}
            </p>
          </div>
        </div>

        <div class="mt-auto flex flex-wrap items-center gap-1.5 border-t border-border/40 pt-3">
          <Button
            size="sm"
            class="h-8 gap-1.5 text-xs"
            :disabled="doc.status !== 'available' && doc.status !== 'invalid'"
            @click="readDocument(doc)"
          >
            <BookOpen class="size-3.5" data-icon="inline-start" />
            {{ t("documents.read") }}
          </Button>

          <!-- 合并为一个按钮：已关联时为「解除关联」，未关联时为「关联画廊」 -->
          <Button
            v-if="doc.gid"
            size="sm"
            variant="outline"
            class="h-8 gap-1.5 text-xs text-muted-foreground hover:text-destructive hover:border-destructive/40"
            @click="removeLink(doc)"
          >
            <LinkOff class="size-3.5" data-icon="inline-start" />
            {{ t("documents.remove_association") }}
          </Button>
          <Button
            v-else
            size="sm"
            variant="outline"
            class="h-8 gap-1.5 text-xs"
            @click="openAssociateDialog(doc)"
          >
            <Link class="size-3.5" data-icon="inline-start" />
            {{ t("documents.associate") }}
          </Button>

          <Button
            v-if="doc.status === 'available'"
            size="icon-sm"
            variant="ghost"
            class="ml-auto size-8 text-muted-foreground hover:text-destructive hover:bg-destructive/10"
            :title="t('documents.delete_file')"
            :aria-label="t('documents.delete_file')"
            @click="deleteDocument(doc)"
          >
            <Trash2 class="size-3.5" />
            <span class="sr-only">{{ t("documents.delete_file") }}</span>
          </Button>
        </div>
      </Card>
    </div>

    <div
      v-else-if="!loading"
      class="flex flex-col items-center justify-center gap-2 rounded-2xl border border-dashed border-border/80 p-12 text-center"
    >
      <BookOpen class="size-8 text-muted-foreground/40 mb-1" />
      <p class="text-sm font-medium text-foreground">
        {{ filterText ? t("documents.no_search_results") : t("documents.empty") }}
      </p>
      <Button
        v-if="filterText"
        variant="outline"
        size="sm"
        class="mt-1 h-7 text-xs"
        @click="filterText = ''"
      >
        {{ t("documents.clear_search") }}
      </Button>
    </div>

    <div v-if="pages > 1" class="flex items-center justify-center gap-3 pt-2">
      <Button
        variant="outline"
        size="sm"
        :disabled="page <= 1"
        @click="page--"
      >{{ t("common.previous") }}</Button>
      <span class="text-xs font-mono text-muted-foreground"
        >{{ page }} / {{ pages }}</span
      >
      <Button
        variant="outline"
        size="sm"
        :disabled="page >= pages"
        @click="page++"
      >{{ t("common.next") }}</Button>
    </div>

    <!-- 关联画廊弹窗 -->
    <Dialog v-model:open="associateDialogOpen">
      <DialogContent class="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{{ t("documents.associate_dialog_title") }}</DialogTitle>
          <DialogDescription>
            {{ t("documents.associate_dialog_desc") }}
          </DialogDescription>
        </DialogHeader>

        <form class="flex flex-col gap-4 py-2" @submit.prevent="saveAssociation">
          <div
            v-if="associateDoc"
            class="rounded-lg bg-muted/40 p-3 border border-border/50 text-xs"
          >
            <div class="font-medium text-foreground truncate">
              {{ associateDoc.title || associateDoc.path }}
            </div>
            <div class="text-muted-foreground font-mono text-[11px] mt-0.5 break-all">
              {{ associateDoc.path }}
            </div>
          </div>

          <div class="flex flex-col gap-1.5">
            <label class="text-xs font-medium text-foreground">
              {{ t("documents.associate_url_or_token") }}
            </label>
            <Input
              v-model="associateInput"
              :placeholder="t('documents.associate_input_placeholder')"
              class="text-xs font-mono"
              @input="onAssociateInput"
            />
            <p class="text-[11px] text-muted-foreground">
              {{ t("documents.associate_url_hint") }}
            </p>
          </div>

          <div class="grid grid-cols-2 gap-3 pt-1">
            <div class="flex flex-col gap-1.5">
              <label class="text-xs text-muted-foreground">{{ t("documents.gid") }}</label>
              <Input
                v-model="association.gid"
                type="number"
                min="1"
                placeholder="4337580"
                required
                class="text-xs font-mono h-8"
              />
            </div>
            <div class="flex flex-col gap-1.5">
              <label class="text-xs text-muted-foreground">{{ t("documents.token") }}</label>
              <Input
                v-model="association.token"
                placeholder="abcdef1234"
                required
                class="text-xs font-mono h-8"
              />
            </div>
          </div>

          <DialogFooter class="pt-2">
            <Button
              variant="outline"
              size="sm"
              type="button"
              @click="associateDialogOpen = false"
            >
              {{ t("common.cancel") }}
            </Button>
            <Button
              size="sm"
              type="submit"
              :disabled="!association.gid || !association.token"
            >
              {{ t("documents.save_association") }}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>

    <!-- 新建画廊归档弹窗 -->
    <Dialog v-model:open="createArchiveDialogOpen">
      <DialogContent class="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{{ t("archive.dialog_new_title") }}</DialogTitle>
          <DialogDescription>
            {{ t("archive.dialog_new_desc") }}
          </DialogDescription>
        </DialogHeader>

        <div class="flex flex-col gap-3 my-2 text-left">
          <div v-if="!s.status.value?.site?.has_ipb" class="inline-alert">
            <Alert variant="destructive">
              <CircleAlert class="size-4" />
              <AlertDescription class="text-xs">
                {{ t("archive.dialog_new_no_ipb") }}
              </AlertDescription>
            </Alert>
          </div>
          <div class="flex gap-2">
            <Input
              v-model="s.archiveInput.value"
              :placeholder="t('archive.dialog_new_placeholder')"
              @keydown.enter.prevent="s.getQuote('start')"
              class="text-xs flex-1 font-mono"
            />
            <Button
              variant="outline"
              size="sm"
              :disabled="
                !s.status.value?.site?.has_ipb ||
                s.busy.value ||
                !s.archiveInput.value.trim()
              "
              @click="s.getQuote('start')"
            >
              <Search class="size-3.5" data-icon="inline-start" />
              {{ t("archive.dialog_new_query") }}
            </Button>
          </div>

          <div
            v-if="s.quote.value"
            class="rounded-xl border border-border/70 bg-muted/30 p-3.5 flex flex-col gap-2.5 shadow-xs"
          >
            <div class="font-medium text-foreground text-xs line-clamp-2">
              {{ s.quote.value.title || t("archive.dialog_new_quote_ready") }}
            </div>
            <div
              v-if="s.quote.value.gp_balance"
              class="text-muted-foreground text-[11px] font-mono"
            >
              {{
                t("archive.dialog_new_balance", {
                  balance: s.quote.value.gp_balance.toLocaleString(),
                })
              }}
            </div>
            <div class="flex items-center gap-2 pt-1">
              <label class="text-xs text-muted-foreground shrink-0">
                {{ t("archive.dialog_new_quality_label") }}
              </label>
              <Select v-model="s.selectedQuality.value">
                <SelectTrigger class="h-8 text-xs flex-1">
                  <SelectValue
                    :placeholder="t('archive.dialog_new_quality_placeholder')"
                  />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem
                    v-for="o in (s.quote.value.options || []).filter(
                      (x) => x.available,
                    )"
                    :key="o.or"
                    :value="o.or"
                  >
                    {{ o.label
                    }}{{ o.unlocked ? t("archive.dialog_new_unlocked") : "" }}
                    ·
                    {{
                      o.gp_price
                        ? o.gp_price.toLocaleString() + " GP"
                        : "Free"
                    }}{{ o.size ? " · " + o.size : "" }}
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>
            <p class="text-[11px] text-amber-600 dark:text-amber-400">
              {{ t("archive.dialog_new_warning") }}
            </p>
          </div>
        </div>

        <DialogFooter class="pt-2">
          <Button
            variant="outline"
            size="sm"
            @click="closeCreateArchiveDialog"
          >
            {{ t("common.cancel") }}
          </Button>
          <Button
            size="sm"
            :disabled="
              !s.quote.value || !s.selectedQuality.value || s.busy.value
            "
            @click="confirmAndStartArchive"
          >
            {{ t("archive.dialog_new_confirm") }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- 扫描报告与历史诊断弹窗 -->
    <Dialog :open="scanReportOpen" @update:open="(val) => (scanReportOpen = val)">
      <DialogContent class="max-w-lg">
        <DialogHeader>
          <DialogTitle>{{ t("documents.scan_report") }}</DialogTitle>
          <DialogDescription>
            {{ t("documents.scan_report_desc") }}
          </DialogDescription>
        </DialogHeader>

        <div v-if="scan" class="flex flex-col gap-4 text-xs">
          <!-- 运行状态 -->
          <div class="flex items-center justify-between rounded-lg border border-border/60 bg-muted/40 p-3">
            <div class="flex items-center gap-2">
              <span class="text-muted-foreground">{{ t("documents.scan_status") }}:</span>
              <Badge
                :variant="scan.status === 'running' ? 'default' : scan.error ? 'destructive' : 'secondary'"
                class="font-mono capitalize text-[11px]"
              >
                <RefreshCw
                  v-if="scan.status === 'running'"
                  class="mr-1 size-3 animate-spin"
                  data-icon="inline-start"
                />
                {{ scan.status }}
              </Badge>
            </div>
            <div class="flex items-center gap-2">
              <span class="text-muted-foreground">{{ t("documents.scan_mode") }}:</span>
              <span class="font-medium text-foreground">
                {{ scan.deep ? t("documents.scan_deep") : t("documents.scan_normal") }}
              </span>
            </div>
          </div>

          <!-- 核心指标 -->
          <div class="grid grid-cols-3 gap-2 text-center">
            <div class="rounded-lg border border-border/40 bg-card p-2.5 shadow-2xs">
              <div class="text-muted-foreground">{{ t("documents.scanned") }}</div>
              <div class="mt-1 font-mono text-base font-semibold text-foreground">
                {{ scan.scanned ?? scan.imported ?? 0 }}
              </div>
            </div>
            <div class="rounded-lg border border-border/40 bg-card p-2.5 shadow-2xs">
              <div class="text-muted-foreground">{{ t("documents.registered") }}</div>
              <div class="mt-1 font-mono text-base font-semibold text-foreground">
                {{ scan.registered ?? scan.skipped ?? 0 }}
              </div>
            </div>
            <div class="rounded-lg border border-border/40 bg-card p-2.5 shadow-2xs">
              <div class="text-muted-foreground">{{ t("documents.invalid") }}</div>
              <div
                class="mt-1 font-mono text-base font-semibold"
                :class="(scan.invalid || scan.conflicts) ? 'text-destructive' : 'text-foreground'"
              >
                {{ scan.invalid ?? scan.conflicts ?? 0 }}
              </div>
            </div>
          </div>

          <!-- 关联诊断 Link Diagnostics -->
          <div class="flex flex-col gap-2">
            <span class="font-medium text-foreground">{{ t("documents.link_diagnostics") }}</span>
            <div
              v-if="scan.link_diagnostics && Object.keys(scan.link_diagnostics).length"
              class="grid grid-cols-2 gap-2 rounded-lg border border-border/40 bg-muted/20 p-2.5"
            >
              <div
                v-for="([reason, count]) in Object.entries(scan.link_diagnostics)"
                :key="reason"
                class="flex items-center justify-between rounded-md bg-background px-2.5 py-1.5 font-mono text-[11px] border border-border/40 shadow-2xs"
              >
                <span class="text-muted-foreground truncate mr-2" :title="reason">
                  {{ getDiagnosticLabel(reason) }}
                </span>
                <span class="font-semibold text-foreground shrink-0">{{ count }}</span>
              </div>
            </div>
            <div
              v-else
              class="rounded-lg border border-dashed border-border/60 p-3 text-center text-muted-foreground"
            >
              {{ t("documents.no_diagnostics") }}
            </div>
          </div>

          <!-- 时间信息 -->
          <div
            v-if="scan.started_at || scan.completed_at"
            class="flex items-center justify-between text-[11px] text-muted-foreground pt-1"
          >
            <span v-if="scan.started_at">
              {{ t("documents.scan_started_at") }}: {{ new Date(scan.started_at * 1000).toLocaleTimeString() }}
            </span>
            <span v-if="scan.completed_at">
              {{ t("documents.scan_completed_at") }}: {{ new Date(scan.completed_at * 1000).toLocaleTimeString() }}
            </span>
          </div>

          <!-- 错误展示 -->
          <Alert v-if="scan.error" variant="destructive">
            <CircleAlert class="size-4" />
            <AlertDescription>{{ scan.error }}</AlertDescription>
          </Alert>
        </div>
        <div v-else class="py-4 text-center text-muted-foreground">
          {{ t("documents.empty") }}
        </div>

        <DialogFooter>
          <Button variant="outline" size="sm" @click="scanReportOpen = false">
            {{ t("common.close") }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>

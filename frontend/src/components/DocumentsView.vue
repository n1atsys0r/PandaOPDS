<script setup>
import { computed, onMounted, onBeforeUnmount, ref, watch } from "vue";
import { useApp } from "@/state.js";
import { t } from "@/i18n.js";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import CoverImage from "@/components/CoverImage.vue";
import DownloadJobsPanel from "@/components/DownloadJobsPanel.vue";
import {
  BookOpenIcon as BookOpen,
  LinkIcon as Link,
  Link2OffIcon as LinkOff,
  RefreshCwIcon as RefreshCw,
  Trash2Icon as Trash2,
} from "@lucide/vue";

const emit = defineEmits(["read-document"]);
const s = useApp();
const documents = ref([]);
const total = ref(0);
const page = ref(1);
const perPage = 50;
const linked = ref("all");
const status = ref("available");
const loading = ref(false);
const error = ref("");
const scan = ref(null);
const editing = ref("");
const association = ref({ gid: "", token: "" });
const pages = computed(() => Math.max(1, Math.ceil(total.value / perPage)));
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
async function startScan() {
  error.value = "";
  try {
    const { data } = await s.api("/api/archive/import/scan", {
      method: "POST",
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
function editLink(doc) {
  editing.value = doc.id;
  association.value = { gid: doc.gid || "", token: doc.token || "" };
}
async function saveLink(doc) {
  error.value = "";
  try {
    await s.api(`/api/documents/${encodeURIComponent(doc.id)}/associate`, {
      method: "POST",
      body: JSON.stringify({
        gid: Number(association.value.gid || 0),
        token: association.value.token.trim(),
      }),
    });
    editing.value = "";
    await loadDocuments();
  } catch (e) {
    error.value = e.message;
  }
}
async function removeLink(doc) {
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
    const { data } = await s.api(
      `/api/documents/${encodeURIComponent(doc.id)}`,
    );
    emit("read-document", { ...doc, ...data, document_id: doc.id });
  } catch (e) {
    error.value = e.message;
  }
}
</script>

<template>
  <section class="space-y-5 p-4 sm:p-6">
    <header class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <small class="text-muted-foreground">{{ t("documents.header") }}</small>
        <h1 class="text-2xl font-semibold">{{ t("documents.title") }}</h1>
        <p class="mt-1 text-sm text-muted-foreground">
          {{ t("documents.description") }}
        </p>
      </div>
      <Button
        variant="outline"
        :disabled="scan?.status === 'running'"
        @click="startScan"
        ><RefreshCw
          class="size-4"
          :class="{ 'animate-spin': scan?.status === 'running' }"
        />{{
          scan?.status === "running"
            ? t("documents.scanning")
            : t("documents.scan")
        }}</Button
      >
    </header>
    <p
      v-if="error"
      role="alert"
      class="rounded-md border border-destructive/40 p-3 text-sm text-destructive"
    >
      {{ error }}
    </p>
    <div
      v-if="scan"
      class="rounded-md border bg-muted/30 px-3 py-2 text-xs text-muted-foreground"
    >
      <span>{{ scan.status }}</span
      ><span v-if="scan.scanned != null">
        · {{ scan.scanned }} {{ t("documents.scanned") }}</span
      ><span v-if="scan.registered != null">
        · {{ scan.registered }} {{ t("documents.registered") }}</span
      ><span v-if="scan.invalid != null">
        · {{ scan.invalid }} {{ t("documents.invalid") }}</span
      ><span v-if="scan.error" class="text-destructive">
        · {{ scan.error }}</span
      >
    </div>
    <div class="flex flex-wrap items-center justify-between gap-2">
      <div class="inline-flex rounded-md border p-0.5">
        <button
          v-for="tab in ['all', 'linked', 'unlinked']"
          :key="tab"
          class="rounded px-3 py-1.5 text-sm"
          :class="
            linked === tab
              ? 'bg-accent text-accent-foreground'
              : 'text-muted-foreground'
          "
          @click="
            page = 1;
            linked = tab;
          "
        >
          {{
            tab === "all"
              ? t("common.all")
              : tab === "linked"
                ? t("documents.linked")
                : t("documents.unlinked")
          }}
        </button>
      </div>
      <div class="inline-flex rounded-md border p-0.5">
        <button
          v-for="tab in ['all', 'available', 'missing', 'invalid']"
          :key="tab"
          class="rounded px-3 py-1.5 text-sm"
          :class="
            status === tab
              ? 'bg-accent text-accent-foreground'
              : 'text-muted-foreground'
          "
          @click="
            page = 1;
            status = tab;
          "
        >
          {{ t(`documents.status_${tab}`) }}
        </button>
      </div>
      <span class="text-xs text-muted-foreground">{{ total }}</span>
    </div>
    <div v-if="loading" class="py-12 text-center text-sm text-muted-foreground">
      {{ t("common.loading") }}
    </div>
    <div
      v-else-if="documents.length"
      class="grid gap-3 sm:grid-cols-2 xl:grid-cols-3"
    >
      <article
        v-for="doc in documents"
        :key="doc.id"
        class="min-w-0 rounded-lg border bg-card p-4"
      >
        <div class="flex items-start gap-3">
          <CoverImage
            v-if="doc.cover_url"
            :src="doc.cover_url"
            :alt="doc.title"
            class="h-24 w-16 shrink-0 rounded border object-cover"
          />
          <div class="min-w-0 flex-1">
            <div class="flex items-start justify-between gap-2">
              <div class="min-w-0">
                <h2 class="line-clamp-2 font-medium">
                  {{ doc.title || doc.path }}
                </h2>
                <p class="mt-1 break-all text-xs text-muted-foreground">
                  {{ doc.path }}
                </p>
              </div>
              <span class="shrink-0 rounded bg-muted px-2 py-1 text-[11px]">{{
                doc.format?.toUpperCase()
              }}</span>
            </div>
            <p class="mt-2 text-xs text-muted-foreground">
              {{ t("documents.pages", { count: doc.page_count || 0 }) }} ·
              {{ t(`documents.status_${doc.status || "available"}`) }}
              <span v-if="doc.gid">· EH {{ doc.gid }}</span
              ><span v-else>· {{ t("documents.unlinked") }}</span>
            </p>
            <p
              v-if="doc.association_source"
              class="mt-1 text-[11px] text-muted-foreground"
            >
              {{ t("documents.association_source") }}:
              {{ doc.association_source }}
            </p>
          </div>
        </div>
        <form
          v-if="editing === doc.id"
          class="mt-3 grid grid-cols-2 gap-2"
          @submit.prevent="saveLink(doc)"
        >
          <Input
            v-model="association.gid"
            type="number"
            min="1"
            :placeholder="t('documents.gid')"
            required
          /><Input
            v-model="association.token"
            :placeholder="t('documents.token')"
            required
          />
          <div class="col-span-2 flex gap-2">
            <Button size="sm" type="submit">{{
              t("documents.save_association")
            }}</Button
            ><Button
              size="sm"
              variant="ghost"
              type="button"
              @click="editing = ''"
              >{{ t("common.cancel") }}</Button
            >
          </div>
        </form>
        <div class="mt-4 flex flex-wrap gap-2">
          <Button
            size="sm"
            :disabled="doc.status !== 'available'"
            @click="readDocument(doc)"
            ><BookOpen class="size-3.5" />{{ t("documents.read") }}</Button
          ><Button size="sm" variant="outline" @click="editLink(doc)"
            ><Link class="size-3.5" />{{ t("documents.associate") }}</Button
          ><Button
            v-if="doc.gid"
            size="sm"
            variant="ghost"
            :title="t('documents.remove_association')"
            @click="removeLink(doc)"
            ><LinkOff class="size-3.5" /></Button
          ><Button
            v-if="doc.status === 'available'"
            size="sm"
            variant="ghost"
            class="ml-auto text-destructive"
            :aria-label="t('documents.delete_file')"
            @click="deleteDocument(doc)"
            ><Trash2 class="size-3.5"
          /></Button>
        </div>
      </article>
    </div>
    <p
      v-else-if="!loading"
      class="rounded-lg border p-8 text-center text-sm text-muted-foreground"
    >
      {{ t("documents.empty") }}
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
    <DownloadJobsPanel />
  </section>
</template>

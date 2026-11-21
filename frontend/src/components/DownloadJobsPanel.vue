<script setup>
import { computed, onMounted, onBeforeUnmount, ref } from "vue";
import { useApp } from "@/state.js";
import { t } from "@/i18n.js";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Alert, AlertDescription } from "@/components/ui/alert";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet";
import {
  DownloadIcon as Download,
  RefreshCwIcon as RefreshCw,
  CircleAlertIcon as CircleAlert,
  AlertTriangleIcon as AlertTriangle,
  CheckCircle2Icon as CheckCircle2,
} from "@lucide/vue";
import { reviewDownloadJob } from "@/subscription-view.js";

const s = useApp();
const jobs = ref([]);
const error = ref("");
const open = ref(false);
let timer;
let requestSeq = 0;

const runningCount = computed(
  () =>
    jobs.value.filter((job) =>
      ["running", "queued", "pending"].includes(job.status),
    ).length,
);

const needsReviewCount = computed(
  () => jobs.value.filter((job) => job.status === "needs_review").length,
);

async function load() {
  const request = ++requestSeq;
  try {
    const { data } = await s.api("/api/download-jobs");
    if (request === requestSeq)
      jobs.value = (data.items || []).filter((job) =>
        ["queued", "pending", "running", "needs_review"].includes(job.status),
      );
  } catch (e) {
    if (request === requestSeq) error.value = e.message;
  }
}

async function cancel(job) {
  try {
    await s.api(`/api/download-jobs/${encodeURIComponent(job.id)}/cancel`, {
      method: "POST",
    });
    await load();
  } catch (e) {
    error.value = e.message;
  }
}

async function review(job, resolution) {
  const message =
    resolution === "reconcile"
      ? t("subscription.review_reconcile_confirm")
      : t("subscription.review_retry_confirm");
  if (!window.confirm(message)) return;
  try {
    await reviewDownloadJob(s.api, job.id, resolution);
    await load();
  } catch (e) {
    error.value = e.message;
  }
}

onMounted(() => {
  load();
  timer = setInterval(load, 5000);
});

onBeforeUnmount(() => {
  clearInterval(timer);
  requestSeq++;
});
</script>

<template>
  <Sheet v-model:open="open">
    <SheetTrigger as-child>
      <!-- Trigger Button in Toolbar -->
      <Button
        v-if="needsReviewCount > 0"
        variant="outline"
        size="sm"
        class="h-7 text-xs gap-1.5 px-2.5 border-amber-500/50 bg-amber-500/10 text-amber-700 dark:text-amber-400 hover:bg-amber-500/20 shadow-xs cursor-pointer"
        :title="t('documents.download_jobs')"
      >
        <AlertTriangle class="size-3.5 shrink-0 text-amber-600 dark:text-amber-400" />
        <span>{{ t("documents.download_jobs") }}</span>
        <Badge
          variant="destructive"
          class="h-4 px-1 text-[10px] font-mono leading-none bg-amber-600 text-white dark:bg-amber-500 hover:bg-amber-600"
        >
          {{ needsReviewCount }}
        </Badge>
      </Button>

      <Button
        v-else-if="runningCount > 0"
        variant="outline"
        size="sm"
        class="h-7 text-xs gap-1.5 px-2.5 border-primary/40 text-primary bg-primary/5 hover:bg-primary/10 shadow-xs cursor-pointer"
        :title="t('documents.download_jobs')"
      >
        <RefreshCw class="size-3.5 shrink-0 animate-spin" />
        <span>{{ t("documents.download_jobs") }}</span>
        <Badge
          variant="secondary"
          class="h-4 px-1 text-[10px] font-mono leading-none bg-primary/15 text-primary"
        >
          {{ runningCount }}
        </Badge>
      </Button>

      <Button
        v-else
        variant="outline"
        size="sm"
        class="h-7 text-xs gap-1.5 px-2.5 text-muted-foreground hover:text-foreground cursor-pointer"
        :title="t('documents.download_jobs')"
      >
        <Download class="size-3.5 shrink-0" />
        <span>{{ t("documents.download_jobs") }}</span>
      </Button>
    </SheetTrigger>

    <SheetContent
      side="right"
      class="w-full sm:max-w-md flex flex-col p-6 gap-5 bg-card border-l border-border/80 shadow-2xl"
    >
      <SheetHeader class="p-0 gap-1.5">
        <div class="flex items-center justify-between pr-8">
          <div class="flex items-center gap-2">
            <SheetTitle class="text-base font-semibold tracking-tight">
              {{ t("documents.download_jobs") }}
            </SheetTitle>
            <Badge
              v-if="jobs.length"
              :variant="needsReviewCount > 0 ? 'destructive' : 'secondary'"
              class="h-5 px-1.5 text-[11px] font-mono font-normal"
            >
              {{ jobs.length }}
            </Badge>
          </div>
          <Button
            size="xs"
            variant="ghost"
            class="h-7 text-xs gap-1 text-muted-foreground hover:text-foreground"
            @click="load"
          >
            <RefreshCw class="size-3" data-icon="inline-start" />
            {{ t("common.refresh") }}
          </Button>
        </div>
        <SheetDescription class="text-xs text-muted-foreground">
          {{ t("documents.download_jobs_desc") }}
        </SheetDescription>
      </SheetHeader>

      <Alert v-if="error" variant="destructive">
        <CircleAlert class="size-4" />
        <AlertDescription class="text-xs">{{ error }}</AlertDescription>
      </Alert>

      <!-- Jobs List -->
      <div v-if="jobs.length" class="flex-1 overflow-y-auto pr-1 flex flex-col gap-3">
        <article
          v-for="job in jobs"
          :key="job.id"
          class="flex flex-col gap-2.5 rounded-xl border border-border/70 bg-muted/20 p-3.5 text-sm shadow-xs transition-colors hover:border-border"
        >
          <div class="flex items-start justify-between gap-2">
            <p class="truncate font-medium text-foreground text-xs leading-snug">
              {{ job.title || `${job.gid}:${job.token}` }}
            </p>
            <Badge
              :variant="job.status === 'needs_review' ? 'destructive' : job.status === 'running' ? 'default' : 'outline'"
              class="font-mono text-[10px] uppercase shrink-0"
            >
              {{ job.status }}
            </Badge>
          </div>

          <p class="text-xs text-muted-foreground font-mono">
            {{ job.stage }}
            <span v-if="job.error" class="text-destructive font-medium block mt-0.5">
              {{ job.error }}
            </span>
          </p>

          <div
            v-if="job.status === 'needs_review'"
            class="rounded-lg bg-destructive/10 border border-destructive/20 p-2 text-xs text-destructive/90"
          >
            {{ t("subscription.needs_review_help") }}
          </div>

          <div class="flex flex-wrap items-center gap-2 pt-1 border-t border-border/40 mt-1">
            <Button
              v-if="['queued', 'pending'].includes(job.status)"
              size="xs"
              variant="outline"
              class="text-xs"
              @click="cancel(job)"
            >
              {{ t("common.cancel") }}
            </Button>
            <template v-if="job.status === 'needs_review'">
              <Button
                size="xs"
                variant="outline"
                class="text-xs border-primary/30 text-primary hover:bg-primary/10"
                @click="review(job, 'reconcile')"
              >
                {{ t("subscription.review_reconcile") }}
              </Button>
              <Button
                size="xs"
                variant="secondary"
                class="text-xs"
                @click="review(job, 'allow_manual_retry')"
              >
                {{ t("subscription.review_allow_retry") }}
              </Button>
            </template>
          </div>
        </article>
      </div>

      <!-- Empty State -->
      <div
        v-else
        class="flex-1 flex flex-col items-center justify-center p-8 text-center text-muted-foreground border border-dashed border-border/70 rounded-xl my-auto"
      >
        <CheckCircle2 class="size-8 mb-2 text-muted-foreground/30" />
        <p class="text-xs font-medium text-foreground">
          {{ t("subscription.no_jobs") }}
        </p>
      </div>
    </SheetContent>
  </Sheet>
</template>

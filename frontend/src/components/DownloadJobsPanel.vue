<script setup>
import { onMounted, onBeforeUnmount, ref } from "vue";
import { useApp } from "@/state.js";
import { t } from "@/i18n.js";
import { Button } from "@/components/ui/button";
import { reviewDownloadJob } from "@/subscription-view.js";

const s = useApp();
const jobs = ref([]);
const error = ref("");
let timer;
let requestSeq = 0;

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
  <section class="space-y-3 border-t pt-5">
    <header class="flex items-center justify-between gap-2">
      <h2 class="font-semibold">{{ t("subscription.jobs") }}</h2>
      <Button size="sm" variant="ghost" @click="load">{{
        t("common.refresh")
      }}</Button>
    </header>
    <p v-if="error" role="alert" class="text-sm text-destructive">
      {{ error }}
    </p>
    <p v-if="!jobs.length" class="text-sm text-muted-foreground">
      {{ t("subscription.no_jobs") }}
    </p>
    <article
      v-for="job in jobs"
      :key="job.id"
      class="flex flex-wrap items-center justify-between gap-3 rounded-md border p-3 text-sm"
    >
      <div class="min-w-0 flex-1">
        <p class="truncate font-medium">
          {{ job.title || `${job.gid}:${job.token}` }}
        </p>
        <p class="text-xs text-muted-foreground">
          {{ job.status }} · {{ job.stage
          }}<span v-if="job.error"> · {{ job.error }}</span>
        </p>
        <p
          v-if="job.status === 'needs_review'"
          class="mt-2 text-xs text-muted-foreground"
        >
          {{ t("subscription.needs_review_help") }}
        </p>
      </div>
      <div class="flex flex-wrap gap-2">
        <Button
          v-if="['queued', 'pending'].includes(job.status)"
          size="sm"
          variant="outline"
          @click="cancel(job)"
          >{{ t("common.cancel") }}</Button
        >
        <template v-if="job.status === 'needs_review'">
          <Button
            size="sm"
            variant="outline"
            @click="review(job, 'reconcile')"
            >{{ t("subscription.review_reconcile") }}</Button
          >
          <Button
            size="sm"
            variant="outline"
            @click="review(job, 'allow_manual_retry')"
            >{{ t("subscription.review_allow_retry") }}</Button
          >
        </template>
      </div>
    </article>
  </section>
</template>

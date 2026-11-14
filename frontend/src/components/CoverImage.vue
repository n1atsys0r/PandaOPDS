<script>
// Module-level shared IntersectionObserver for all CoverImage instances
let sharedObserver = null;
const observerCallbacks = new Map();

function getSharedObserver() {
  if (typeof IntersectionObserver === "undefined") return null;
  if (!sharedObserver) {
    sharedObserver = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (entry.isIntersecting) {
            const cb = observerCallbacks.get(entry.target);
            if (cb) {
              observerCallbacks.delete(entry.target);
              sharedObserver?.unobserve(entry.target);
              cb();
            }
          }
        }
      },
      { rootMargin: "250px" },
    );
  }
  return sharedObserver;
}
</script>

<script setup>
import { ref, watch, onMounted, onBeforeUnmount } from "vue";
import { useApp } from "@/state.js";
import { t } from "@/i18n.js";

const props = defineProps({
  src: { type: String, required: true },
  alt: { type: String, default: "" },
});

const s = useApp();
const rootEl = ref(null);
const initial = s.peekCoverUrl(props.src);
const visible = ref(Boolean(initial) || typeof IntersectionObserver === "undefined");
const resolvedSrc = ref(initial);
const failed = ref(false);
let loadSeq = 0;

async function load() {
  const seq = ++loadSeq;
  failed.value = false;
  if (!props.src || !visible.value) {
    resolvedSrc.value = "";
    return;
  }
  const immediate = s.peekCoverUrl(props.src);
  if (immediate) {
    resolvedSrc.value = immediate;
    return;
  }
  resolvedSrc.value = "";
  const url = await s.resolveCoverUrl(props.src);
  if (seq !== loadSeq) return;
  if (url) {
    resolvedSrc.value = url;
  } else {
    failed.value = true;
  }
}

onMounted(() => {
  if (!visible.value && rootEl.value) {
    const obs = getSharedObserver();
    if (obs) {
      observerCallbacks.set(rootEl.value, () => {
        visible.value = true;
        load();
      });
      obs.observe(rootEl.value);
    } else {
      visible.value = true;
      load();
    }
  } else if (!resolvedSrc.value) {
    load();
  }
});

onBeforeUnmount(() => {
  if (rootEl.value) {
    observerCallbacks.delete(rootEl.value);
    sharedObserver?.unobserve(rootEl.value);
  }
});

watch([() => props.src, () => s.authToken.value], () => {
  const cached = s.peekCoverUrl(props.src);
  if (cached) {
    resolvedSrc.value = cached;
    failed.value = false;
    if (rootEl.value) {
      observerCallbacks.delete(rootEl.value);
      sharedObserver?.unobserve(rootEl.value);
    }
    visible.value = true;
    return;
  }
  if (visible.value) load();
});
</script>

<template>
  <div ref="rootEl" class="absolute inset-0 size-full grid place-items-center">
    <img
      v-if="resolvedSrc && !failed"
      :src="resolvedSrc"
      :alt="alt || t('archive.cover')"
      loading="lazy"
      class="size-full object-cover"
      @error="failed = true"
    />
    <span v-else-if="failed">{{ t("archive.no_cover") }}</span>
  </div>
</template>

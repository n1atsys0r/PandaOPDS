<script setup>
import { ref, watch, onMounted, onBeforeUnmount } from "vue";
import { useApp } from "@/state.js";

const props = defineProps({
  src: { type: String, required: true },
  alt: { type: String, default: "封面" },
});

const s = useApp();
const rootEl = ref(null);
const initial = s.peekCoverUrl(props.src);
const visible = ref(Boolean(initial) || typeof IntersectionObserver === "undefined");
const resolvedSrc = ref(initial);
const failed = ref(false);
let observer = null;
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
    observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) {
          visible.value = true;
          observer?.disconnect();
          observer = null;
          load();
        }
      },
      { rootMargin: "200px" },
    );
    observer.observe(rootEl.value);
  } else if (!resolvedSrc.value) {
    load();
  }
});

onBeforeUnmount(() => {
  observer?.disconnect();
  observer = null;
});

watch([() => props.src, () => s.authToken.value], () => {
  const cached = s.peekCoverUrl(props.src);
  if (cached) {
    resolvedSrc.value = cached;
    failed.value = false;
    observer?.disconnect();
    observer = null;
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
      :alt="alt"
      loading="lazy"
      class="size-full object-cover"
      @error="failed = true"
    />
    <span v-else-if="failed">暂无封面</span>
  </div>
</template>

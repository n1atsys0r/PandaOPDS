<script setup>
import {
  ref,
  watch,
  computed,
  nextTick,
  onMounted,
  onBeforeUnmount,
} from "vue";
import { Loader2, RotateCcw } from "@lucide/vue";
import { Button } from "@/components/ui/button";
import { t } from "@/i18n.js";
import {
  decideSwipe,
  releaseVelocity,
  rescuesHorizontal,
  swipeTurnThreshold,
} from "@/reader-swipe.js";
import { PinchZoomController } from "@/reader-zoom.js";
import { buildReaderSpreads, spreadForPage } from "@/reader-spreads.js";
import { directionalPrefetchPages } from "@/reader-prefetch.js";

const props = defineProps({
  pageCount: Number,
  modelValue: Number,
  rtl: { type: Boolean, default: true },
  dark: Boolean,
  pageMode: { type: String, default: "single" },
  readingRTL: { type: Boolean, default: true },
  resolvePage: Function,
  resolveThumb: Function,
  cancelPagePrefetch: Function,
});
const emit = defineEmits([
  "update:modelValue",
  "navigate",
  "update:pageMode",
  "update:readingRTL",
  "update:zoom",
  "toggle-overlay",
]);
const displayIndex = ref(props.modelValue || 0);
const stageEl = ref(null);
const trackEl = ref(null);
function savedPreference(key, fallback) {
  try {
    return localStorage.getItem(key) ?? fallback;
  } catch {
    return fallback;
  }
}
const readingRTL = ref(
  props.readingRTL !== undefined
    ? props.readingRTL
    : savedPreference(
        "pandaopds_reader_direction",
        props.rtl ? "rtl" : "ltr",
      ) === "rtl",
);
const status = ref("loading");
const src = ref("");
const pageUrls = ref({});
const pageErrors = ref({});
const pageMode = ref(
  props.pageMode || savedPreference("pandaopds_reader_layout", "single"),
);
const thumbUrls = ref({});
const offset = ref(0);
const animate = ref(true);
const pageListOpen = ref(false);
const zoom = ref(1);
const zoomTransform = ref({ s: 1, tx: 0, ty: 0 });
const zoomController = new PinchZoomController((value) => {
  zoomTransform.value = value;
  zoom.value = value.s;
  emit("update:zoom", value.s);
});

watch(
  () => props.pageMode,
  (mode) => {
    if (mode && mode !== pageMode.value) {
      changePageMode(mode);
    }
  },
);

watch(
  () => props.readingRTL,
  (val) => {
    if (val !== undefined && val !== readingRTL.value) {
      readingRTL.value = val;
    }
  },
);
const zoomPointers = new Set();
let gesture = null;
let requestId = 0;
let animationTimer = null;
let suppressImageClick = false;
let thumbObserver = null;
let turnInProgress = false;
let turnToken = 0;
let lastTap = null;
let suppressNativeDoubleClick = false;
let warmIntentSpread = null;
let latestNavigationDirection = "forward";
let pendingDiscreteTurn = null;
let prefetchGeneration = 0;
let prefetchTask = null;
let prefetchTaskIdle = false;
let idleDecodeInProgress = false;
let prefetchedPages = new Set();
let idleDecodeQueue = [];
const decodedImageUrls = new Set();
const imageReady = new Map();
const preloadedUrls = new Map();
const pageImages = new Map();
const spreads = computed(() =>
  buildReaderSpreads(props.pageCount, pageMode.value),
);
const currentSpread = computed(() =>
  spreadForPage(spreads.value, displayIndex.value),
);
const spreadWindow = computed(() =>
  [
    currentSpread.value - 1,
    currentSpread.value,
    currentSpread.value + 1,
  ].filter((i) => i >= 0 && i < spreads.value.length),
);

function seamClass(spreadIndex, pageIndex) {
  const first = spreads.value[spreadIndex]?.find((index) => index !== null);
  if (readingRTL.value) {
    return pageIndex === first
      ? "reader-page-image--seam-left"
      : "reader-page-image--seam-right";
  }
  return pageIndex === first
    ? "reader-page-image--seam-right"
    : "reader-page-image--seam-left";
}

function setPageUrl(index, url) {
  if (pageUrls.value[index] === url) return;
  pageUrls.value = { ...pageUrls.value, [index]: url };
}
function removePageUrl(index) {
  if (!(index in pageUrls.value)) return;
  const { [index]: _removed, ...remaining } = pageUrls.value;
  pageUrls.value = remaining;
}
function setDragOffset(value) {
  trackEl.value?.style.setProperty("--reader-drag-offset", `${value}px`);
}
function setPageImageRef(index, el) {
  if (el) pageImages.set(index, el);
  else pageImages.delete(index);
}

watch(
  () => props.modelValue,
  (idx) => {
    if ((idx || 0) === displayIndex.value) return;
    cancelPendingTurn();
    animate.value = false;
    displayIndex.value = idx || 0;
    loadCurrent();
  },
);
watch(() => props.pageCount, loadCurrent);
watch(
  pageListOpen,
  (open) => {
    if (open) observeThumbs();
    else {
      thumbObserver?.disconnect();
      thumbObserver = null;
    }
  },
  { flush: "post" },
);
watch(
  () => props.pageCount,
  () => {
    if (pageListOpen.value) observeThumbs();
  },
);

async function loadCurrent() {
  const id = ++requestId;
  const deadline = performance.now() + 15000;
  const currentIndex = displayIndex.value;
  const cachedUrl =
    pageUrls.value[currentIndex] || preloadedUrls.get(currentIndex);
  const cachedReady = Boolean(cachedUrl && decodedImageUrls.has(cachedUrl));
  if (cachedReady) {
    src.value = cachedUrl;
    status.value = "loaded";
  } else {
    status.value = "loading";
  }
  const currentRequest = cachedUrl
    ? Promise.resolve(cachedUrl)
    : props.resolvePage(displayIndex.value, true);
  const spread = spreads.value[currentSpread.value] || [displayIndex.value];
  const companionLoads = spread
    .filter((index) => index !== null && index !== displayIndex.value)
    .map((index) => {
      const companionUrl = pageUrls.value[index] || preloadedUrls.get(index);
      const request = companionUrl
        ? Promise.resolve(companionUrl)
        : props.resolvePage(index, true);
      return request
        .then(async (url) => {
          if (id !== requestId) return;
          setPageUrl(index, url);
          preloadedUrls.delete(index);
          await nextTick();
          await waitForImage(url);
          const { [index]: _removedPage, ...errors } = pageErrors.value;
          pageErrors.value = errors;
        })
        .catch(() => {
          if (id === requestId)
            pageErrors.value = { ...pageErrors.value, [index]: true };
        });
    });
  void companionLoads;
  scheduleRollingPrefetch(currentSpread.value, latestNavigationDirection);
  try {
    const url =
      cachedUrl ||
      (await withTimeout(
        currentRequest,
        Math.max(1, deadline - performance.now()),
      ));
    if (id !== requestId) return;
    setPageUrl(displayIndex.value, url);
    await nextTick();
    await withTimeout(
      waitForImage(url),
      Math.max(1, deadline - performance.now()),
    );
    if (id !== requestId) return;
    src.value = url;
    setPageUrl(displayIndex.value, url);
    const { [displayIndex.value]: _removed, ...remainingErrors } =
      pageErrors.value;
    pageErrors.value = remainingErrors;
    status.value = "loaded";
  } catch {
    if (id === requestId) status.value = "error";
  }
}

function warmSpread(spreadIndex, isCurrent = () => true) {
  const pages =
    spreads.value[spreadIndex]?.filter((index) => index !== null) || [];
  for (const index of pages) {
    const url = pageUrls.value[index] || preloadedUrls.get(index);
    if (url) {
      setPageUrl(index, url);
      continue;
    }
    props
      .resolvePage(index, true)
      .then((value) => {
        if (isCurrent() && Math.abs(spreadIndex - currentSpread.value) <= 1)
          setPageUrl(index, value);
      })
      .catch(() => {
        if (isCurrent())
          pageErrors.value = { ...pageErrors.value, [index]: true };
      });
  }
}
function cancelSpeculativePrefetch(keep = []) {
  prefetchGeneration += 1;
  if (prefetchTask !== null) {
    if (prefetchTaskIdle && typeof window.cancelIdleCallback === "function")
      window.cancelIdleCallback(prefetchTask);
    else clearTimeout(prefetchTask);
  }
  prefetchTask = null;
  const retained = new Set(keep);
  props.cancelPagePrefetch?.(
    [...prefetchedPages].filter((index) => !retained.has(index)),
  );
  for (const index of prefetchedPages) {
    if (!retained.has(index)) preloadedUrls.delete(index);
  }
  prefetchedPages = retained;
  idleDecodeQueue = idleDecodeQueue.filter(({ index }) => retained.has(index));
}
function scheduleIdleDecode(generation) {
  if (
    prefetchTask !== null ||
    idleDecodeInProgress ||
    idleDecodeQueue.length === 0
  )
    return;
  const decode = () => {
    prefetchTask = null;
    if (generation !== prefetchGeneration) return;
    const item = idleDecodeQueue.shift();
    if (!item) return;
    if (
      prefetchedPages.has(item.index) &&
      preloadedUrls.get(item.index) === item.url
    ) {
      idleDecodeInProgress = true;
      waitForImage(item.url)
        .catch(() => {})
        .finally(() => {
          idleDecodeInProgress = false;
          scheduleIdleDecode(prefetchGeneration);
        });
    } else {
      scheduleIdleDecode(generation);
    }
  };
  if (typeof window.requestIdleCallback === "function") {
    prefetchTaskIdle = true;
    prefetchTask = window.requestIdleCallback(decode, { timeout: 1200 });
  } else {
    prefetchTaskIdle = false;
    prefetchTask = setTimeout(decode, 120);
  }
}
function scheduleRollingPrefetch(anchorSpread, direction) {
  const pages = directionalPrefetchPages(
    spreads.value,
    anchorSpread,
    direction,
    4,
    1,
  );
  cancelSpeculativePrefetch(pages);
  const generation = prefetchGeneration;
  pages.forEach((index) => {
    if (pageUrls.value[index] || preloadedUrls.has(index)) return;
    props
      .resolvePage(index, false)
      .then((url) => {
        if (generation !== prefetchGeneration || !prefetchedPages.has(index))
          return;
        preloadedUrls.set(index, url);
        idleDecodeQueue.push({ index, url });
        scheduleIdleDecode(generation);
      })
      .catch(() => {});
  });
  scheduleIdleDecode(generation);
}
function withTimeout(promise, ms) {
  let timer;
  return Promise.race([
    promise,
    new Promise((_, reject) => {
      timer = setTimeout(() => reject(new Error("page load timed out")), ms);
    }),
  ]).finally(() => clearTimeout(timer));
}
function waitForImage(url, timeoutMs = 12000) {
  if (decodedImageUrls.has(url)) return Promise.resolve();
  if (imageReady.has(url)) return imageReady.get(url);
  const ready = new Promise((resolve, reject) => {
    const image =
      [...pageImages.values()].find(
        (candidate) =>
          candidate.isConnected &&
          (candidate.currentSrc === url || candidate.src === url),
      ) || new Image();
    const isDetached = !image.isConnected;
    const timeout = setTimeout(() => {
      if (isDetached) image.src = "";
      reject(new Error("image load timed out"));
    }, timeoutMs);
    let finished = false;
    const finish = (err) => {
      if (finished) return;
      finished = true;
      clearTimeout(timeout);
      image.onload = null;
      image.onerror = null;
      if (err) reject(err);
      else resolve();
    };
    image.onload = () =>
      image.decode
        ? image
            .decode()
            .then(() => finish())
            .catch(() => finish())
        : finish();
    image.onerror = () => finish(new Error("image load failed"));
    if (isDetached) image.src = url;
    if (image.complete && image.naturalWidth > 0) {
      if (image.decode)
        image
          .decode()
          .then(() => finish())
          .catch(() => finish());
      else finish();
    }
  });
  imageReady.set(url, ready);
  ready
    .then(
      () => decodedImageUrls.add(url),
      () => {},
    )
    .finally(() => imageReady.delete(url));
  return ready;
}
async function retrySpreadPage(index) {
  try {
    const url = await props.resolvePage(index, true);
    removePageUrl(index);
    await nextTick();
    const { [index]: _removed, ...errors } = pageErrors.value;
    pageErrors.value = errors;
    setPageUrl(index, url);
    await nextTick();
    await waitForImage(url);
  } catch {
    pageErrors.value = { ...pageErrors.value, [index]: true };
  }
}
function retryCurrentPage() {
  removePageUrl(displayIndex.value);
  src.value = "";
  loadCurrent();
}
function commit(idx) {
  cancelPendingTurn();
  animate.value = false;
  const next = Math.max(0, Math.min(props.pageCount - 1, idx));
  offset.value = 0;
  zoomController.reset();
  if (next === displayIndex.value) return;
  displayIndex.value = next;
  emit("update:modelValue", next);
  emit("navigate", next);
  loadCurrent();
}
function cancelPendingTurn() {
  turnToken += 1;
  if (animationTimer) clearTimeout(animationTimer);
  animationTimer = null;
  pendingDiscreteTurn = null;
  turnInProgress = false;
  setDragOffset(0);
  offset.value = 0;
  animate.value = false;
}
function commitAdjacent(dir) {
  if (turnInProgress) return;
  const target =
    spreads.value[currentSpread.value + (dir === "forward" ? 1 : -1)]?.[0];
  if (target === undefined || target === null) {
    offset.value = 0;
    animate.value = true;
    return;
  }
  const token = ++turnToken;
  turnInProgress = true;
  latestNavigationDirection = dir;
  animate.value = true;
  const targetSpreadIndex = currentSpread.value + (dir === "forward" ? 1 : -1);
  warmSpread(targetSpreadIndex, () => token === turnToken);
  const width = stageEl.value?.clientWidth || 1;
  const towardNext = dir === "forward";
  offset.value = towardNext
    ? readingRTL.value
      ? width
      : -width
    : readingRTL.value
      ? -width
      : width;
  animate.value = true;
  animationTimer = setTimeout(() => {
    if (token !== turnToken) return;
    animate.value = false;
    displayIndex.value = target;
    emit("update:modelValue", target);
    emit("navigate", target);
    zoomController.reset();
    loadCurrent();
    offset.value = 0;
    turnInProgress = false;
    animationTimer = null;
    const queued = pendingDiscreteTurn;
    pendingDiscreteTurn = null;
    if (queued)
      requestAnimationFrame(() => {
        if (token === turnToken) commitAdjacent(queued);
      });
  }, 230);
}
function turn(dir) {
  if (turnInProgress) {
    pendingDiscreteTurn = dir;
    return;
  }
  commitAdjacent(dir);
}
function onStageClick(e) {
  if (e.target.closest("button,input") || suppressImageClick || zoom.value > 1)
    return;
  const rect = e.currentTarget.getBoundingClientRect();
  const x = e.clientX - rect.left;
  if (x < rect.width * 0.25) turn(readingRTL.value ? "forward" : "backward");
  else if (x > rect.width * 0.75)
    turn(readingRTL.value ? "backward" : "forward");
  else {
    emit("toggle-overlay");
  }
}
function changePageMode(mode) {
  cancelPendingTurn();
  animate.value = false;
  pageMode.value = mode;
  try {
    localStorage.setItem("pandaopds_reader_layout", mode);
  } catch {}
  emit("update:pageMode", mode);
  loadCurrent();
}
function changeDirection() {
  cancelPendingTurn();
  animate.value = false;
  readingRTL.value = !readingRTL.value;
  try {
    localStorage.setItem(
      "pandaopds_reader_direction",
      readingRTL.value ? "rtl" : "ltr",
    );
  } catch {}
  emit("update:readingRTL", readingRTL.value);
}
function pointerDown(e) {
  if (turnInProgress || e.button !== 0 || e.target.closest("button,input"))
    return;
  if (zoomPointers.size || zoom.value > 1 || e.shiftKey || gesture) {
    const rect = e.currentTarget.getBoundingClientRect();
    if (gesture) {
      zoomController.down(gesture.id, gesture.lastX, gesture.y, rect);
      zoomPointers.add(gesture.id);
    }
    if (gesture?.maxTravel > 8 || gesture?.axis === "x")
      suppressImageClick = true;
    setDragOffset(0);
    offset.value = 0;
    zoomController.down(e.pointerId, e.clientX, e.clientY, rect);
    zoomPointers.add(e.pointerId);
    e.currentTarget.setPointerCapture(e.pointerId);
    gesture = null;
    return;
  }
  gesture = {
    id: e.pointerId,
    x: e.clientX,
    y: e.clientY,
    lastX: e.clientX,
    axis: null,
    trail: [{ x: e.clientX, t: performance.now() }],
    scrolled: false,
    maxTravel: 0,
  };
  setDragOffset(0);
  warmIntentSpread = null;
  e.currentTarget.setPointerCapture(e.pointerId);
}
function pointerMove(e) {
  if (zoomPointers.has(e.pointerId)) {
    zoomController.move(e.pointerId, e.clientX, e.clientY);
    return;
  }
  if (!gesture || e.pointerId !== gesture.id) return;
  const dx = e.clientX - gesture.x,
    dy = e.clientY - gesture.y;
  gesture.maxTravel = Math.max(gesture.maxTravel, Math.hypot(dx, dy));
  if (!gesture.axis && (Math.abs(dx) > 6 || Math.abs(dy) > 6))
    gesture.axis = Math.abs(dx) > Math.abs(dy) ? "x" : "y";
  if (gesture.axis === "y") gesture.scrolled = true;
  if (gesture.axis === "x") {
    animate.value = false;
    setDragOffset(dx);
    const forward = dx * (readingRTL.value ? 1 : -1) > 0;
    const targetSpread = currentSpread.value + (forward ? 1 : -1);
    if (
      targetSpread !== warmIntentSpread &&
      targetSpread >= 0 &&
      targetSpread < spreads.value.length
    ) {
      warmIntentSpread = targetSpread;
      warmSpread(targetSpread);
    }
  }
  gesture.lastX = e.clientX;
  const now = performance.now();
  gesture.trail.push({ x: e.clientX, t: now });
  while (gesture.trail.length > 2 && gesture.trail[1].t < now - 140)
    gesture.trail.shift();
}
function pointerUp(e) {
  if (zoomPointers.has(e.pointerId)) {
    const consumed = zoomController.gestureConsumed();
    zoomController.up(e.pointerId);
    zoomPointers.delete(e.pointerId);
    suppressImageClick = consumed;
    if (consumed)
      setTimeout(() => {
        suppressImageClick = false;
        zoomController.consumeClick();
      }, 0);
    return;
  }
  if (!gesture || e.pointerId !== gesture.id) return;
  const g = gesture;
  gesture = null;
  const dx = e.clientX - g.x,
    dy = e.clientY - g.y;
  const threshold = swipeTurnThreshold(e.currentTarget.clientWidth);
  const rescued = rescuesHorizontal(g.axis, dx, dy, g.scrolled, threshold);
  const decision = rescued
    ? decideSwipe(
        dx,
        releaseVelocity(g.trail, performance.now()),
        readingRTL.value ? 1 : -1,
        threshold,
      )
    : null;
  const rect = e.currentTarget.getBoundingClientRect();
  const tapX = e.clientX - rect.left;
  const isCenterTap =
    !g.axis &&
    g.maxTravel <= 8 &&
    tapX >= rect.width * 0.25 &&
    tapX <= rect.width * 0.75;
  if (isCenterTap) {
    const now = performance.now();
    if (
      lastTap &&
      now - lastTap.time < 320 &&
      Math.hypot(e.clientX - lastTap.x, e.clientY - lastTap.y) < 28
    ) {
      lastTap = null;
      toggleZoom(e);
      suppressImageClick = true;
      suppressNativeDoubleClick = true;
      setTimeout(() => {
        suppressImageClick = false;
        suppressNativeDoubleClick = false;
      }, 400);
      return;
    }
    lastTap = { time: now, x: e.clientX, y: e.clientY };
    setTimeout(() => {
      if (lastTap?.time === now) lastTap = null;
    }, 320);
  }
  suppressImageClick = g.maxTravel > 8 || g.axis === "x";
  if (suppressImageClick)
    setTimeout(() => {
      suppressImageClick = false;
    }, 0);
  if (decision) {
    setDragOffset(0);
    offset.value = dx;
    commitAdjacent(decision);
    return;
  } else {
    setDragOffset(0);
    offset.value = 0;
    animate.value = true;
  }
}
function pointerCancel(e) {
  if (e && zoomPointers.has(e.pointerId)) {
    zoomController.up(e.pointerId);
    zoomPointers.delete(e.pointerId);
    suppressImageClick = true;
    setTimeout(() => {
      suppressImageClick = false;
      zoomController.consumeClick();
    }, 0);
    return;
  }
  if (!gesture || (e && gesture.id !== e.pointerId)) return;
  suppressImageClick = gesture.maxTravel > 8 || gesture.axis === "x";
  if (suppressImageClick)
    setTimeout(() => {
      suppressImageClick = false;
    }, 0);
  gesture = null;
  setDragOffset(0);
  offset.value = 0;
  animate.value = true;
}
function onKey(e) {
  if (e.altKey || e.ctrlKey || e.metaKey) return;
  if (e.key === "ArrowLeft" || e.key === "ArrowRight") {
    if (
      e.target instanceof HTMLElement &&
      /INPUT|BUTTON|TEXTAREA/.test(e.target.tagName)
    )
      return;
    e.preventDefault();
    const forward = readingRTL.value
      ? e.key === "ArrowLeft"
      : e.key === "ArrowRight";
    turn(forward ? "forward" : "backward");
  }
}
function toggleZoom(e) {
  if (gesture || suppressImageClick || zoomController.consumeClick()) return;
  if (zoom.value > 1) zoomController.reset();
  else zoomController.setScale(2, stageEl.value.getBoundingClientRect());
}
function onImageDoubleClick(e) {
  if (suppressNativeDoubleClick) {
    e.preventDefault();
    return;
  }
  toggleZoom(e);
}
function observeThumbs() {
  thumbObserver?.disconnect();
  thumbObserver = new IntersectionObserver(
    (entries) =>
      entries.forEach((entry) => {
        if (!entry.isIntersecting) return;
        const idx = Number(entry.target.dataset.index);
        thumbObserver?.unobserve(entry.target);
        props
          .resolveThumb(idx)
          .then((url) => {
            thumbUrls.value = { ...thumbUrls.value, [idx]: url };
          })
          .catch(() => {});
      }),
    {
      root: document.querySelector(".reader-pager-thumb-grid"),
      rootMargin: "120px",
    },
  );
  document
    .querySelectorAll("[data-thumb-placeholder]")
    .forEach((el) => thumbObserver.observe(el));
}
function setThumbRef(idx, el) {
  if (el && !thumbUrls.value[idx]) {
    el.dataset.index = String(idx);
    el.dataset.thumbPlaceholder = "";
    thumbObserver?.observe(el);
  }
}
function openThumb(idx) {
  pageListOpen.value = false;
  commit(idx);
}
onMounted(() => {
  window.addEventListener("keydown", onKey);
  loadCurrent();
});
onBeforeUnmount(() => {
  window.removeEventListener("keydown", onKey);
  requestId++;
  turnToken++;
  turnInProgress = false;
  pendingDiscreteTurn = null;
  thumbObserver?.disconnect();
  if (animationTimer) clearTimeout(animationTimer);
  cancelSpeculativePrefetch();
  decodedImageUrls.clear();
  imageReady.clear();
  preloadedUrls.clear();
  pageImages.clear();
});

defineExpose({
  turn,
  toggleZoom,
  commit,
  displayIndex,
});
</script>

<template>
  <main
    class="reader-pager absolute inset-0 flex items-center justify-center overflow-hidden"
    :class="dark ? 'bg-black' : 'bg-background'"
  >
    <section
      ref="stageEl"
      class="relative h-full w-full touch-pan-y flex items-center justify-center overflow-hidden"
      @click="onStageClick"
      @pointerdown="pointerDown"
      @pointermove="pointerMove"
      @pointerup="pointerUp"
      @pointercancel="pointerCancel"
      @lostpointercapture="pointerCancel"
    >
      <div
        ref="trackEl"
        class="reader-page-track absolute inset-0"
        :style="{
          transform: `translate3d(calc(${currentSpread * (readingRTL ? 100 : -100)}% + ${offset}px + var(--reader-drag-offset, 0px)),0,0)`,
          transition: animate
            ? 'transform 220ms cubic-bezier(.2,.8,.2,1)'
            : 'none',
        }"
      >
        <div
          v-for="spreadIndex in spreadWindow"
          :key="spreadIndex"
          class="absolute inset-y-0 flex h-full w-full items-center justify-center"
          :style="{
            left: `${spreadIndex * (readingRTL ? -100 : 100)}%`,
            transform: `translate(${zoomTransform.tx}px,${zoomTransform.ty}px) scale(${zoom})`,
            transformOrigin: 'top left',
          }"
          :class="
            pageMode === 'double' && spreadIndex === 0
              ? ''
              : readingRTL
                ? 'flex-row-reverse'
                : ''
          "
        >
          <template v-for="idx in spreads[spreadIndex]" :key="idx ?? 'blank'">
            <img
              v-if="idx !== null && pageUrls[idx]"
              :ref="(el) => setPageImageRef(idx, el)"
              :src="pageUrls[idx]"
              :alt="`${t('explore.page_no')} ${idx + 1}`"
              class="reader-page-image select-none object-contain"
              :class="
                spreads[spreadIndex].filter((value) => value !== null).length >
                1
                  ? `reader-page-image--half ${seamClass(spreadIndex, idx)}`
                  : ''
              "
              draggable="false"
              @dblclick.stop="onImageDoubleClick"
            />
            <div
              v-else-if="idx !== null && pageErrors[idx]"
              class="flex h-full w-1/2 flex-col items-center justify-center gap-2 text-center text-muted-foreground"
            >
              <span class="text-xs">{{
                t("reader.page_load_failed", { page: idx + 1 })
              }}</span>
              <Button
                variant="outline"
                size="sm"
                @click.stop="retrySpreadPage(idx)"
              >
                <RotateCcw class="size-3.5" /> {{ t("common.retry") }}
              </Button>
            </div>
            <div
              v-else-if="idx !== null"
              class="flex h-full items-center justify-center text-muted-foreground"
              :class="
                spreads[spreadIndex].filter((value) => value !== null).length >
                1
                  ? 'w-1/2'
                  : 'w-full'
              "
            >
              <Loader2 class="size-5 animate-spin" />
            </div>
            <div v-else class="h-full w-1/2" />
          </template>
        </div>
      </div>
      <div
        v-if="status === 'error'"
        class="absolute inset-0 flex flex-col items-center justify-center gap-3 bg-background/70 text-muted-foreground"
      >
        <span>{{
          t("reader.page_load_failed", { page: displayIndex + 1 })
        }}</span>
        <Button variant="outline" size="sm" @click="retryCurrentPage">
          <RotateCcw class="size-3.5" /> {{ t("common.retry") }}
        </Button>
      </div>
      <div
        v-else-if="status === 'loading' && !src"
        class="flex items-center gap-2 text-muted-foreground"
      >
        <Loader2 class="size-5 animate-spin" />{{ displayIndex + 1 }} /
        {{ pageCount }}
      </div>
      <div
        v-if="status === 'loading' && src"
        class="absolute right-4 top-4 rounded bg-background/70 p-1"
      >
        <Loader2 class="size-4 animate-spin" />
      </div>
    </section>
  </main>
</template>

<style scoped>
.reader-page-image {
  display: block;
  width: 100%;
  height: 100%;
  min-width: 0;
  min-height: 0;
  max-width: 100%;
  max-height: 100%;
  flex: 0 1 auto;
  object-fit: contain;
}

.reader-page-image--half {
  width: 50%;
}
.reader-page-image--seam-left {
  object-position: left center;
}
.reader-page-image--seam-right {
  object-position: right center;
}
</style>

<script setup>
import {
  ref,
  computed,
  watch,
  nextTick,
  onMounted,
  onBeforeUnmount,
} from "vue";
import { createMangaViewer } from "@yui540/comimi";
import {
  BookOpen,
  ScrollText,
  X,
  RotateCcw,
  Loader2,
  HatGlasses,
} from "@lucide/vue";
import { Button } from "@/components/ui/button";
import { useApp } from "@/state.js";
import { t } from "@/i18n.js";
import {
  READER_PAGE_KEY_PREFIX,
  parseSavedProgress,
  getReadingThreshold,
  shouldRecordProgress,
} from "@/progression-helper.js";

const props = defineProps({
  open: { type: Boolean, default: false },
  archive: { type: Object, default: null },
  incognito: { type: Boolean, default: false },
  psePageBase: { type: Number, default: 1 },
  theme: { type: String, default: "system" },
});

const emit = defineEmits(["update:open"]);

const s = useApp();

const isIncognito = computed(() =>
  Boolean(props.incognito || props.archive?.incognito),
);

const READER_MODE_KEY = "pandaopds_reader_mode";
const MAX_CONCURRENT_FETCHES = 2;
const PRIORITY_CURRENT = 3;
const PRIORITY_PRELOAD = 2;
const PRIORITY_THUMB = 1;
const EMPTY_SVG =
  "data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' width='1' height='1'/>";

const readerMode = ref(
  localStorage.getItem(READER_MODE_KEY) === "webtoon" ? "webtoon" : "paged",
);
const currentPageIndex = ref(0);
const comimiContainerEl = ref(null);
const webtoonScrollEl = ref(null);
const webtoonStates = ref({}); // { [idx]: { status: 'idle'|'loading'|'loaded'|'error', src: string } }

let viewer = null;
let abortController = null;
let resolvedUrls = new Map();
let inflightUrls = new Map();
let resolvedThumbUrls = new Map();
let inflightThumbUrls = new Map();
let activeFetchCount = 0;
let queueOrderSeq = 0;
let pendingFetchQueue = [];
let loadObserver = null;
let trackObserver = null;
let pageListThumbObserver = null;
const webtoonPageRefs = new Map();
let sessionSeq = 0;

const isDark = computed(() => {
  if (props.theme === "dark") return true;
  if (props.theme === "light") return false;
  return (
    typeof document !== "undefined" &&
    document.documentElement.classList.contains("dark")
  );
});

const galleryKey = computed(() => {
  if (!props.archive) return "";
  const gid = props.archive.active_gid || props.archive.gid;
  const token = props.archive.active_token || props.archive.token;
  return `${gid}:${token}`;
});

const galleryTitle = computed(() => {
  const a = props.archive;
  if (!a) return "";
  return (
    a.metadata?.title_jpn ||
    a.metadata?.title ||
    a.title ||
    t("reader.gallery_title", { gid: a.active_gid || a.gid })
  );
});

const pageCount = computed(() => {
  const a = props.archive;
  if (!a) return 0;
  return Math.max(0, Number(a.page_count || a.metadata?.filecount || 0));
});

const pageIndices = computed(() =>
  Array.from({ length: pageCount.value }, (_, i) => i),
);

function positionStorageKey() {
  return galleryKey.value ? `${READER_PAGE_KEY_PREFIX}${galleryKey.value}` : "";
}

function readSavedProgress() {
  const key = positionStorageKey();
  if (!key) return null;
  try {
    const raw = localStorage.getItem(key);
    return parseSavedProgress(raw);
  } catch {
    return null;
  }
}

function readSavedPageIndex() {
  const saved = readSavedProgress();
  if (!saved) return 0;
  return Math.min(Math.max(0, saved.page), Math.max(0, pageCount.value - 1));
}

let remoteSyncTimer = null;
let pendingSyncIndex = null;
let sessionStartPage = 0;
let userHasNavigated = false;
let singlePageTimer = null;

function flushRemoteSync() {
  if (remoteSyncTimer) {
    clearTimeout(remoteSyncTimer);
    remoteSyncTimer = null;
  }
  if (isIncognito.value) {
    pendingSyncIndex = null;
    return;
  }
  if (pendingSyncIndex !== null && props.archive && pageCount.value > 0) {
    const idx = pendingSyncIndex;
    pendingSyncIndex = null;
    const gid = props.archive.active_gid || props.archive.gid;
    const token = props.archive.active_token || props.archive.token;
    s.putProgression(gid, token, {
      pageIndex: idx,
      pageCount: pageCount.value,
    }).catch(() => {});
  }
}

function scheduleRemoteSync(idx) {
  if (isIncognito.value) return;
  pendingSyncIndex = idx;
  if (remoteSyncTimer) clearTimeout(remoteSyncTimer);
  remoteSyncTimer = setTimeout(() => {
    flushRemoteSync();
  }, 1500);
}

function savePageIndex(
  idx,
  { skipRemote = false, modified = null, force = false } = {},
) {
  const clamped = Math.min(Math.max(0, idx), Math.max(0, pageCount.value - 1));
  currentPageIndex.value = clamped;
  if (isIncognito.value) {
    return;
  }
  if (
    !force &&
    !shouldRecordProgress({
      isIncognito: isIncognito.value,
      hasNavigated: userHasNavigated,
      pageIndex: clamped,
      pageCount: pageCount.value,
    })
  ) {
    return;
  }
  const key = positionStorageKey();
  const modIso = modified || new Date().toISOString();
  if (key) {
    try {
      localStorage.setItem(
        key,
        JSON.stringify({ page: clamped, modified: modIso }),
      );
    } catch {
      // ignore storage quota errors
    }
  }
  const gid = props.archive?.active_gid || props.archive?.gid;
  const token = props.archive?.active_token || props.archive?.token;
  if (gid && token && pageCount.value > 0) {
    s.updateLocalProgress(gid, token, clamped, pageCount.value, modIso);
  }
  if (!skipRemote) {
    scheduleRemoteSync(clamped);
  }
}

function streamUrlFor(idx) {
  const a = props.archive;
  if (!a) return "";
  if (Array.isArray(a.reading_order) && a.reading_order[idx]?.href) {
    return a.reading_order[idx].href;
  }
  const gid = a.active_gid || a.gid;
  const token = a.active_token || a.token;
  const pageNum = props.psePageBase + idx;
  return `/stream/${gid}/${token}/page/${pageNum}`;
}

function thumbUrlFor(idx) {
  const a = props.archive;
  if (!a) return "";
  if (Array.isArray(a.reading_order) && a.reading_order[idx]?.thumbnailHref) {
    return a.reading_order[idx].thumbnailHref;
  }
  const gid = a.active_gid || a.gid;
  const token = a.active_token || a.token;
  const pageNum = props.psePageBase + idx;
  return `/image/${gid}/${token}/page/${pageNum}/thumb`;
}

function clearPendingFetchQueue() {
  const queued = pendingFetchQueue;
  pendingFetchQueue = [];
  for (const job of queued) {
    job.reject(new Error("reader session closed"));
  }
}

function drainFetchQueue() {
  while (activeFetchCount < MAX_CONCURRENT_FETCHES && pendingFetchQueue.length > 0) {
    const anchor =
      readerMode.value === "paged" && viewer
        ? viewer.getCurrentPageIndex()
        : currentPageIndex.value;
    pendingFetchQueue.sort((a, b) => {
      if (b.priority !== a.priority) return b.priority - a.priority;
      const da = Math.abs(a.idx - anchor);
      const db = Math.abs(b.idx - anchor);
      if (da !== db) return da - db;
      return b.seq - a.seq;
    });
    const job = pendingFetchQueue.shift();
    if (!job) break;
    activeFetchCount += 1;
    Promise.resolve()
      .then(job.run)
      .then(job.resolve, job.reject)
      .finally(() => {
        activeFetchCount = Math.max(0, activeFetchCount - 1);
        drainFetchQueue();
      });
  }
}

function enqueueReaderFetch(key, idx, priority, run) {
  const existing = pendingFetchQueue.find((j) => j.key === key);
  if (existing) {
    if (priority > existing.priority) existing.priority = priority;
    existing.seq = ++queueOrderSeq;
    return existing.promise;
  }
  let resolveFn;
  let rejectFn;
  const promise = new Promise((resolve, reject) => {
    resolveFn = resolve;
    rejectFn = reject;
  });
  pendingFetchQueue.push({
    key,
    idx,
    priority,
    seq: ++queueOrderSeq,
    run,
    resolve: resolveFn,
    reject: rejectFn,
    promise,
  });
  drainFetchQueue();
  return promise;
}

async function resolveCachedUrl(
  kind,
  idx,
  priority,
  urlFor,
  cacheMap,
  inflightMap,
) {
  if (cacheMap.has(idx)) return cacheMap.get(idx);
  const queueKey = `${kind}:${idx}`;
  const queued = pendingFetchQueue.find((j) => j.key === queueKey);
  if (queued && priority > queued.priority) {
    queued.priority = priority;
    queued.seq = ++queueOrderSeq;
  }
  if (inflightMap.has(idx)) return inflightMap.get(idx);

  const seq = sessionSeq;
  const url = urlFor(idx);
  if (!url) throw new Error("missing url");

  const signal = abortController?.signal;
  const task = enqueueReaderFetch(queueKey, idx, priority, async () => {
    if (seq !== sessionSeq) {
      throw new Error("reader session closed");
    }
    const resolved = await s.fetchReaderBlobUrl(url, signal);
    if (seq !== sessionSeq) {
      if (resolved && resolved.startsWith("blob:")) {
        URL.revokeObjectURL(resolved);
      }
      throw new Error("reader session closed");
    }
    if (!resolved) {
      throw new Error(`failed to load page ${idx + 1}`);
    }
    cacheMap.set(idx, resolved);
    return resolved;
  }).finally(() => {
    inflightMap.delete(idx);
  });

  inflightMap.set(idx, task);
  return task;
}

function resolvePageUrl(idx, priority = PRIORITY_PRELOAD) {
  return resolveCachedUrl(
    "page",
    idx,
    priority,
    streamUrlFor,
    resolvedUrls,
    inflightUrls,
  );
}

function resolveThumbUrl(idx) {
  return resolveCachedUrl(
    "thumb",
    idx,
    PRIORITY_THUMB,
    thumbUrlFor,
    resolvedThumbUrls,
    inflightThumbUrls,
  );
}

function disconnectPageListThumbObserver() {
  pageListThumbObserver?.disconnect();
  pageListThumbObserver = null;
}

function observeComimiPageListThumbs() {
  disconnectPageListThumbObserver();
  const root = comimiContainerEl.value?.querySelector(".comimi-page-list-inner");
  if (!root) return;
  const items = Array.from(root.querySelectorAll(".comimi-page-list-item"));
  if (!items.length) return;

  const seq = sessionSeq;
  pageListThumbObserver = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        if (!entry.isIntersecting) continue;
        const idx = Number(entry.target.dataset.thumbIndex);
        pageListThumbObserver?.unobserve(entry.target);
        if (!Number.isInteger(idx)) continue;
        const img = entry.target.querySelector("img");
        if (!img) continue;
        const existing = resolvedThumbUrls.get(idx) || resolvedUrls.get(idx);
        if (existing) {
          img.src = existing;
          continue;
        }
        resolveThumbUrl(idx)
          .then((url) => {
            if (seq === sessionSeq && img.isConnected) {
              img.src = url;
            }
          })
          .catch(() => {});
      }
    },
    { root, rootMargin: "200px 0px" },
  );

  items.forEach((item, idx) => {
    item.dataset.thumbIndex = String(idx);
    const img = item.querySelector("img");
    if (!img) return;
    const cached = resolvedThumbUrls.get(idx) || resolvedUrls.get(idx);
    if (cached) {
      img.src = cached;
    } else {
      pageListThumbObserver.observe(item);
    }
  });
}

function revokeMapUrls(map) {
  for (const url of map.values()) {
    if (typeof url === "string" && url.startsWith("blob:")) {
      URL.revokeObjectURL(url);
    }
  }
  map.clear();
}

function cleanupSessionResources() {
  flushRemoteSync();
  if (singlePageTimer) {
    clearTimeout(singlePageTimer);
    singlePageTimer = null;
  }
  userHasNavigated = false;
  sessionStartPage = 0;
  sessionSeq += 1;
  disconnectPageListThumbObserver();
  clearPendingFetchQueue();
  if (viewer) {
    try {
      viewer.destroy();
    } catch {
      // ignore destroy errors
    }
    viewer = null;
  }
  disconnectWebtoonObservers();
  webtoonPageRefs.clear();
  webtoonStates.value = {};
  if (abortController) {
    abortController.abort();
    abortController = null;
  }
  revokeMapUrls(resolvedUrls);
  inflightUrls.clear();
  revokeMapUrls(resolvedThumbUrls);
  inflightThumbUrls.clear();
}

function mountComimi(startIndex) {
  if (!props.open || !props.archive || !comimiContainerEl.value || pageCount.value <= 0) {
    return;
  }
  disconnectPageListThumbObserver();
  if (viewer) {
    viewer.destroy();
    viewer = null;
  }
  const peekedCover = props.archive.cover_url
    ? s.peekCoverUrl(props.archive.cover_url)
    : "";

  // Always start with lightweight placeholders so opening the reader never
  // triggers hundreds of eager hidden page-list thumbnail requests.
  const pages = pageIndices.value.map((idx) => ({
    id: `p${idx}`,
    type: "image",
    label: String(idx + 1),
    src: EMPTY_SVG,
    thumbnailSrc: idx === 0 && peekedCover ? peekedCover : EMPTY_SVG,
  }));

  const activeTheme = isDark.value ? "dark" : "light";
  const activeBg = isDark.value ? "black" : "white";

  viewer = createMangaViewer(comimiContainerEl.value, {
    manga: {
      id: `pandaopds:${galleryKey.value}`,
      title: galleryTitle.value,
      author: props.archive.metadata?.uploader || undefined,
      pages,
    },
    initialPageIndex: startIndex,
    locale: "zh-CN",
    settings: {
      layoutMode: "browserFullscreen",
      readingDirection: "rtl",
      pageTurnMode: "single",
      hasCover: true,
      theme: activeTheme,
      backgroundColor: activeBg,
    },
    forceSettings: ["layoutMode", "theme", "backgroundColor"],
    lockLayoutMode: true,
    hiddenSettings: ["viewMode", "locale", "theme"],
    mascot: false,
    resolvePageSrc: async ({ page, pageIndex, isSpread }) => {
      const visible = viewer?.getState()?.visiblePageIndexes;
      const isCurrent = Array.isArray(visible)
        ? visible.includes(pageIndex)
        : pageIndex === currentPageIndex.value ||
          (isSpread && Math.abs(pageIndex - currentPageIndex.value) <= 1);
      const url = await resolvePageUrl(
        pageIndex,
        isCurrent ? PRIORITY_CURRENT : PRIORITY_PRELOAD,
      );
      if (page && page.type === "image") {
        page.thumbnailSrc = url;
      }
      return url;
    },
    events: {
      pageChange: ({ pageIndex }) => {
        if (pageIndex !== sessionStartPage) {
          userHasNavigated = true;
        }
        savePageIndex(pageIndex);
      },
      panelChange: ({ panel }) => {
        if (panel === "pages") {
          nextTick(() => observeComimiPageListThumbs());
        } else {
          disconnectPageListThumbObserver();
        }
      },
    },
  });
}

function disconnectWebtoonObservers() {
  loadObserver?.disconnect();
  loadObserver = null;
  trackObserver?.disconnect();
  trackObserver = null;
}

async function loadWebtoonPage(idx, priority = PRIORITY_PRELOAD) {
  const curr = webtoonStates.value[idx];
  if (curr?.status === "loaded") return;
  if (curr?.status === "loading") {
    if (priority > PRIORITY_PRELOAD) {
      resolvePageUrl(idx, priority).catch(() => {});
    }
    return;
  }
  webtoonStates.value = {
    ...webtoonStates.value,
    [idx]: { status: "loading", src: "" },
  };
  try {
    const url = await resolvePageUrl(idx, priority);
    webtoonStates.value = {
      ...webtoonStates.value,
      [idx]: { status: "loaded", src: url },
    };
  } catch {
    if (!props.open) return;
    webtoonStates.value = {
      ...webtoonStates.value,
      [idx]: { status: "error", src: "" },
    };
  }
}

function setWebtoonPageRef(idx, el) {
  if (el) {
    webtoonPageRefs.set(idx, el);
    loadObserver?.observe(el);
    trackObserver?.observe(el);
  } else {
    const prev = webtoonPageRefs.get(idx);
    if (prev) {
      loadObserver?.unobserve(prev);
      trackObserver?.unobserve(prev);
    }
    webtoonPageRefs.delete(idx);
  }
}

async function setupWebtoon(startIndex) {
  disconnectWebtoonObservers();
  await nextTick();
  const root = webtoonScrollEl.value;
  if (!root || !props.open) return;

  loadObserver = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        if (!entry.isIntersecting) continue;
        const idx = Number(entry.target.getAttribute("data-page-index"));
        if (Number.isInteger(idx)) {
          const isCurrent = Math.abs(idx - currentPageIndex.value) <= 1;
          loadWebtoonPage(idx, isCurrent ? PRIORITY_CURRENT : PRIORITY_PRELOAD);
        }
      }
    },
    { root, rootMargin: "450px 0px" },
  );

  trackObserver = new IntersectionObserver(
    (entries) => {
      let topMost = null;
      for (const entry of entries) {
        if (!entry.isIntersecting) continue;
        const idx = Number(entry.target.getAttribute("data-page-index"));
        if (!Number.isInteger(idx)) continue;
        if (topMost === null || idx < topMost) {
          topMost = idx;
        }
      }
      if (topMost !== null) {
        if (topMost !== sessionStartPage) {
          userHasNavigated = true;
        }
        savePageIndex(topMost);
      }
    },
    { root, rootMargin: "-35% 0px -55% 0px", threshold: 0 },
  );

  for (const el of webtoonPageRefs.values()) {
    loadObserver.observe(el);
    trackObserver.observe(el);
  }

  loadWebtoonPage(startIndex, PRIORITY_CURRENT);
  if (startIndex + 1 < pageCount.value) loadWebtoonPage(startIndex + 1, PRIORITY_PRELOAD);
  if (startIndex > 0) loadWebtoonPage(startIndex - 1, PRIORITY_PRELOAD);

  if (startIndex > 0) {
    const targetEl = webtoonPageRefs.get(startIndex);
    targetEl?.scrollIntoView({ block: "start", behavior: "auto" });
  } else {
    root.scrollTop = 0;
  }
}

function jumpToWebtoonPage(idx, { fromUser = true } = {}) {
  const clamped = Math.min(Math.max(0, idx), Math.max(0, pageCount.value - 1));
  if (fromUser && clamped !== sessionStartPage) {
    userHasNavigated = true;
  }
  savePageIndex(clamped);
  loadWebtoonPage(clamped, PRIORITY_CURRENT);
  const el = webtoonPageRefs.get(clamped);
  el?.scrollIntoView({ block: "start", behavior: "auto" });
}

async function switchMode(nextMode) {
  if (readerMode.value === nextMode) return;
  let resumeIdx = currentPageIndex.value;
  if (readerMode.value === "paged" && viewer) {
    resumeIdx = viewer.getCurrentPageIndex();
    disconnectPageListThumbObserver();
    viewer.destroy();
    viewer = null;
  } else if (readerMode.value === "webtoon") {
    disconnectWebtoonObservers();
  }
  sessionStartPage = resumeIdx;
  readerMode.value = nextMode;
  try {
    localStorage.setItem(READER_MODE_KEY, nextMode);
  } catch {
    // ignore storage errors
  }
  await nextTick();
  if (nextMode === "paged") {
    mountComimi(resumeIdx);
  } else {
    await setupWebtoon(resumeIdx);
  }
}

function closeReader() {
  emit("update:open", false);
}

function onKeydown(e) {
  if (!props.open) return;
  if (e.key === "Escape") {
    if (readerMode.value === "paged" && viewer) {
      const panel = viewer.getState()?.panel;
      if (panel && panel !== "none") {
        return;
      }
    }
    e.preventDefault();
    e.stopPropagation();
    closeReader();
  }
}

let savedBodyOverflow = "";
function lockBodyScroll(lock) {
  if (typeof document === "undefined") return;
  if (lock) {
    savedBodyOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
  } else {
    document.body.style.overflow = savedBodyOverflow;
  }
}

async function startSession() {
  cleanupSessionResources();
  if (!props.open || !props.archive || pageCount.value <= 0) return;
  abortController = new AbortController();
  const gid = props.archive.active_gid || props.archive.gid;
  const token = props.archive.active_token || props.archive.token;

  let startIdx = 0;
  let hasExplicitStart = false;

  if (Number.isFinite(props.archive?.start_page)) {
    startIdx = Math.min(
      Math.max(0, Math.floor(props.archive.start_page)),
      Math.max(0, pageCount.value - 1),
    );
    hasExplicitStart = true;
  } else if (Number.isFinite(props.archive?.initial_page_index)) {
    startIdx = Math.min(
      Math.max(0, Math.floor(props.archive.initial_page_index)),
      Math.max(0, pageCount.value - 1),
    );
    hasExplicitStart = true;
  } else {
    const localSaved = readSavedProgress();
    if (localSaved) {
      startIdx = Math.min(Math.max(0, localSaved.page), Math.max(0, pageCount.value - 1));
    }
  }

  currentPageIndex.value = startIdx;
  sessionStartPage = startIdx;
  userHasNavigated = false;

  // Single-page chapter auto-mark after ~2 seconds (if not incognito)
  if (pageCount.value === 1 && !isIncognito.value) {
    singlePageTimer = setTimeout(() => {
      if (props.open && !isIncognito.value && pageCount.value === 1) {
        userHasNavigated = true;
        savePageIndex(0, { force: true });
      }
    }, 2000);
  }

  await nextTick();
  if (readerMode.value === "paged") {
    mountComimi(startIdx);
  } else {
    await setupWebtoon(startIdx);
  }

  // Reconcile with server progression asynchronously if user did not provide an explicit starting page and not incognito
  if (!hasExplicitStart && gid && token && !isIncognito.value) {
    const sessionCapture = sessionSeq;
    const localSaved = readSavedProgress();
    const localMod = localSaved?.modified || 0;

    s.getProgression(gid, token).then((remoteDoc) => {
      if (sessionCapture !== sessionSeq || !props.open || !remoteDoc) return;
      const remoteMod = remoteDoc.modified ? new Date(remoteDoc.modified).getTime() : 0;
      if (typeof remoteDoc.progression === "number" && remoteMod > localMod) {
        const remoteIdx = pageCount.value > 1
          ? Math.min(Math.max(0, Math.round(remoteDoc.progression * (pageCount.value - 1))), pageCount.value - 1)
          : 0;
        if (remoteIdx !== currentPageIndex.value) {
          sessionStartPage = remoteIdx;
          currentPageIndex.value = remoteIdx;
          savePageIndex(remoteIdx, { skipRemote: true, modified: remoteDoc.modified, force: true });
          if (readerMode.value === "paged" && viewer) {
            viewer.goToPage(remoteIdx);
          } else if (readerMode.value === "webtoon") {
            jumpToWebtoonPage(remoteIdx, { fromUser: false });
          }
        }
      } else if (localSaved && localMod > remoteMod && userHasNavigated) {
        s.putProgression(gid, token, {
          pageIndex: localSaved.page,
          pageCount: pageCount.value,
        }).catch(() => {});
      }
    }).catch(() => {});
  }
}

watch(
  () => [
    props.open,
    galleryKey.value,
    props.archive?.start_page,
    props.archive?.initial_page_index,
  ],
  ([isOpen]) => {
    lockBodyScroll(Boolean(isOpen));
    if (isOpen) {
      startSession();
    } else {
      cleanupSessionResources();
    }
  },
  { immediate: true },
);

watch(isDark, (dark) => {
  if (viewer && props.open && readerMode.value === "paged") {
    viewer.updateSettings({
      theme: dark ? "dark" : "light",
      backgroundColor: dark ? "black" : "white",
    });
  }
});

onMounted(() => {
  window.addEventListener("beforeunload", flushRemoteSync);
  window.addEventListener("keydown", onKeydown, { capture: false });
});

onBeforeUnmount(() => {
  window.removeEventListener("beforeunload", flushRemoteSync);
  window.removeEventListener("keydown", onKeydown, { capture: false });
  lockBodyScroll(false);
  cleanupSessionResources();
});
</script>

<template>
  <Teleport to="body">
    <div
      v-if="open && archive"
      class="fixed inset-0 z-[1000] flex flex-col bg-background text-foreground"
      role="dialog"
      aria-modal="true"
      :aria-label="galleryTitle"
    >
      <!-- Paged mode top-right floating pill (shadcn card surface, aligned with comimi top-left menu) -->
      <div
        v-if="readerMode === 'paged'"
        class="fixed top-4 right-4 sm:top-6 sm:right-6 z-[1010] flex items-center gap-1.5 rounded-[14px] border border-border bg-card p-1.5 text-card-foreground shadow-md"
      >
        <div
          v-if="isIncognito"
          class="inline-flex items-center gap-1 rounded-[10px] bg-muted/80 px-2 py-1 text-xs font-medium text-muted-foreground"
          :title="t('reader.incognito_notice')"
        >
          <HatGlasses class="size-3.5 text-muted-foreground" />
          <span class="hidden sm:inline">{{ t("reader.incognito_badge") }}</span>
        </div>
        <div
          class="inline-flex items-center rounded-[10px] bg-muted p-0.5 text-xs text-muted-foreground"
        >
          <button
            type="button"
            class="inline-flex items-center gap-1 rounded-[8px] px-2.5 py-1 font-medium transition-colors"
            :class="
              readerMode === 'paged'
                ? 'bg-primary text-primary-foreground shadow-xs'
                : 'hover:text-foreground'
            "
            @click="switchMode('paged')"
          >
            <BookOpen class="size-3.5" />
            <span>{{ t("reader.mode_paged") }}</span>
          </button>
          <button
            type="button"
            class="inline-flex items-center gap-1 rounded-[8px] px-2.5 py-1 font-medium transition-colors"
            :class="
              readerMode === 'webtoon'
                ? 'bg-primary text-primary-foreground shadow-xs'
                : 'hover:text-foreground'
            "
            @click="switchMode('webtoon')"
          >
            <ScrollText class="size-3.5" />
            <span>{{ t("reader.mode_webtoon") }}</span>
          </button>
        </div>
        <button
          type="button"
          class="inline-flex size-8 items-center justify-center rounded-[10px] text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
          :title="t('reader.close_reader')"
          :aria-label="t('reader.close_reader')"
          @click="closeReader"
        >
          <X class="size-4" />
        </button>
      </div>

      <!-- Paged Mode (@yui540/comimi mount point) -->
      <div
        v-if="readerMode === 'paged'"
        ref="comimiContainerEl"
        class="panda-reader-comimi size-full"
      />

      <!-- Webtoon Mode (Vertical continuous scroll) -->
      <template v-else>
        <header
          class="z-[1010] flex h-13 shrink-0 items-center justify-between gap-3 border-b border-border bg-card px-3 sm:px-5 text-card-foreground"
        >
          <div class="flex min-w-0 items-center gap-2.5">
            <span class="truncate text-sm font-medium">{{ galleryTitle }}</span>
            <span
              class="shrink-0 rounded-md bg-muted px-2 py-0.5 text-xs tabular-nums text-muted-foreground"
            >
              {{ currentPageIndex + 1 }} / {{ pageCount }}
            </span>
            <span
              v-if="isIncognito"
              class="inline-flex shrink-0 items-center gap-1 rounded-md bg-muted/80 px-2 py-0.5 text-xs font-medium text-muted-foreground"
              :title="t('reader.incognito_notice')"
            >
              <HatGlasses class="size-3 text-muted-foreground" />
              <span class="hidden sm:inline">{{ t("reader.incognito_badge") }}</span>
            </span>
          </div>
          <div class="flex shrink-0 items-center gap-1.5">
            <div
              class="inline-flex items-center rounded-[10px] bg-muted p-0.5 text-xs text-muted-foreground"
            >
              <button
                type="button"
                class="inline-flex items-center gap-1 rounded-[8px] px-2.5 py-1 font-medium transition-colors"
                :class="
                  readerMode === 'paged'
                    ? 'bg-primary text-primary-foreground shadow-xs'
                    : 'hover:text-foreground'
                "
                @click="switchMode('paged')"
              >
                <BookOpen class="size-3.5" />
                <span>{{ t("reader.mode_paged") }}</span>
              </button>
              <button
                type="button"
                class="inline-flex items-center gap-1 rounded-[8px] px-2.5 py-1 font-medium transition-colors"
                :class="
                  readerMode === 'webtoon'
                    ? 'bg-primary text-primary-foreground shadow-xs'
                    : 'hover:text-foreground'
                "
                @click="switchMode('webtoon')"
              >
                <ScrollText class="size-3.5" />
                <span>{{ t("reader.mode_webtoon") }}</span>
              </button>
            </div>
            <button
              type="button"
              class="inline-flex size-8 items-center justify-center rounded-[10px] text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
              :title="t('reader.close_reader')"
              :aria-label="t('reader.close_reader')"
              @click="closeReader"
            >
              <X class="size-4" />
            </button>
          </div>
        </header>

        <div
          ref="webtoonScrollEl"
          class="flex-1 overflow-y-auto overscroll-contain"
        >
          <div class="mx-auto flex w-full max-w-3xl flex-col items-center">
            <div
              v-for="idx in pageIndices"
              :key="idx"
              :ref="(el) => setWebtoonPageRef(idx, el)"
              :data-page-index="idx"
              class="relative w-full flex flex-col items-center justify-center"
              :class="
                webtoonStates[idx]?.status === 'loaded'
                  ? ''
                  : 'min-h-[320px] border-b border-dashed border-border bg-muted/40'
              "
            >
              <img
                v-if="webtoonStates[idx]?.status === 'loaded'"
                :src="webtoonStates[idx].src"
                :alt="`${t('explore.page_no')} ${idx + 1}`"
                class="block w-full h-auto select-none"
                draggable="false"
                @error="webtoonStates[idx] = { status: 'error', src: '' }"
              />
              <div
                v-else-if="webtoonStates[idx]?.status === 'error'"
                class="flex flex-col items-center gap-2 py-12 text-xs text-muted-foreground"
              >
                <span>{{ t("reader.page_load_failed", { page: idx + 1 }) }}</span>
                <Button
                  variant="outline"
                  size="sm"
                  @click="loadWebtoonPage(idx, PRIORITY_CURRENT)"
                >
                  <RotateCcw class="size-3.5" />
                  {{ t("common.retry") }}
                </Button>
              </div>
              <div
                v-else
                class="flex items-center gap-2 py-12 text-xs text-muted-foreground"
              >
                <Loader2 class="size-4 animate-spin" />
                <span>{{ t("explore.page_no") }} {{ idx + 1 }}</span>
              </div>
            </div>
          </div>
        </div>

        <footer
          v-if="pageCount > 1"
          class="z-[1010] flex h-11 shrink-0 items-center gap-3 border-t border-border bg-card px-4 sm:px-6 text-xs text-muted-foreground"
        >
          <span class="shrink-0 tabular-nums">1</span>
          <input
            type="range"
            :min="1"
            :max="pageCount"
            :value="currentPageIndex + 1"
            class="w-full accent-primary cursor-pointer"
            :aria-label="t('reader.jump_to_page')"
            @input="jumpToWebtoonPage(Number($event.target.value) - 1)"
          />
          <span class="shrink-0 tabular-nums">{{ pageCount }}</span>
        </footer>
      </template>
    </div>
  </Teleport>
</template>

<style>
/* Restyle @yui540/comimi controls to match the app's shadcn-vue tokens and crisp
   1px borders while preserving all spring/hover/splash/swipe animations and
   keeping only backdrop-filter disabled to avoid blur lag over large images. */
.panda-reader-comimi .comimi-root {
  --comimi-bg: var(--background);
  --comimi-panel: var(--card);
  --comimi-surface: var(--secondary);
  --comimi-surface-2: var(--muted);
  --comimi-surface-3: var(--accent);
  --comimi-fg: var(--foreground);
  --comimi-muted: var(--primary);
  --comimi-soft: var(--muted-foreground);
  --comimi-faint: var(--muted-foreground);
  --comimi-line: var(--border);
  --comimi-white: var(--primary-foreground);
  --comimi-glass: var(--card);
  --comimi-glass-strong: var(--popover);
  --comimi-glass-mute: var(--card);
  --comimi-glass-soft: var(--secondary);
  --comimi-overlay-weak: var(--accent);
  --comimi-shadow: 0 4px 16px rgba(0, 0, 0, 0.08);
  --comimi-shadow-pop: 0 4px 12px rgba(0, 0, 0, 0.12);
  font-family: inherit;
}

.panda-reader-comimi .comimi-root[data-theme="dark"] {
  --comimi-bg: var(--background);
  --comimi-panel: var(--card);
  --comimi-surface: var(--secondary);
  --comimi-surface-2: var(--muted);
  --comimi-surface-3: var(--accent);
  --comimi-fg: var(--foreground);
  --comimi-muted: var(--primary);
  --comimi-soft: var(--muted-foreground);
  --comimi-faint: var(--muted-foreground);
  --comimi-line: var(--border);
  --comimi-white: var(--primary-foreground);
  --comimi-glass: var(--card);
  --comimi-glass-strong: var(--popover);
  --comimi-glass-mute: var(--card);
  --comimi-glass-soft: var(--secondary);
  --comimi-overlay-weak: var(--accent);
  --comimi-shadow: 0 8px 24px rgba(0, 0, 0, 0.45);
  --comimi-shadow-pop: 0 6px 18px rgba(0, 0, 0, 0.5);
}

.panda-reader-comimi .comimi-root[data-bg="black"][data-theme="dark"] {
  --comimi-bg: var(--background);
}

.panda-reader-comimi .comimi-menu-bg,
.panda-reader-comimi .comimi-controls-bg,
.panda-reader-comimi .comimi-arrow-bg,
.panda-reader-comimi .comimi-center-message-text,
.panda-reader-comimi .comimi-seek-preview-label,
.panda-reader-comimi .comimi-page-list-badge,
.panda-reader-comimi .comimi-settings-panel,
.panda-reader-comimi .comimi-settings-close,
.panda-reader-comimi .comimi-tooltip,
.panda-reader-comimi .comimi-toast {
  backdrop-filter: none !important;
  -webkit-backdrop-filter: none !important;
}

.panda-reader-comimi .comimi-menu-bg,
.panda-reader-comimi .comimi-controls-bg {
  border: 1px solid var(--border);
  border-radius: 14px;
}

.panda-reader-comimi .comimi-arrow-bg {
  border: 1px solid var(--border);
  border-radius: 12px;
}

.panda-reader-comimi .comimi-settings-panel,
.panda-reader-comimi .comimi-settings-close {
  border: 1px solid var(--border);
  border-radius: 14px;
}

.panda-reader-comimi .comimi-seek-preview-thumb,
.panda-reader-comimi .comimi-page-list-thumb,
.panda-reader-comimi .comimi-page-list-cover {
  outline: none;
  border: 1px solid var(--border);
  border-radius: 8px;
}

.panda-reader-comimi .comimi-seek-preview-label,
.panda-reader-comimi .comimi-tooltip,
.panda-reader-comimi .comimi-center-message-text {
  border: 1px solid var(--border);
  border-radius: 8px;
}

.panda-reader-comimi .comimi-page-mode-wrapper,
.panda-reader-comimi .comimi-page-mode-indicator,
.panda-reader-comimi .comimi-rolling-switch,
.panda-reader-comimi .comimi-rolling-switch-indicator,
.panda-reader-comimi .comimi-selectbox-bg {
  border-radius: 8px;
}

.panda-reader-comimi .comimi-page-mode-wrapper,
.panda-reader-comimi .comimi-rolling-switch {
  border: 1px solid var(--border);
}
</style>

<script setup>
import { ref, computed, watch, onMounted } from "vue";
import { useApp } from "@/state.js";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from "@/components/ui/sheet";
import CoverImage from "@/components/CoverImage.vue";
import HomeEditor from "@/components/HomeEditor.vue";
import {
  CompassIcon as Compass,
  ChevronLeftIcon as ChevronLeft,
  ChevronRightIcon as ChevronRight,
  RefreshCwIcon as RefreshCw,
  PanelsTopLeftIcon as PanelsTopLeft,
  BookOpenIcon as BookOpen,
  BookmarkIcon as Bookmark,
  DownloadIcon as Download,
  ExternalLinkIcon as ExternalLink,
  StarIcon as Star,
  ArrowLeftIcon as ArrowLeft,
  CheckIcon as Check,
  Package2Icon as Package2,
  SearchIcon as Search,
  LayersIcon as Layers,
  EyeIcon as Eye,
  TrophyIcon as Trophy,
  HeartIcon as Heart,
  SparklesIcon as Sparkles,
  TagIcon as Tag,
  Maximize2Icon as Maximize2,
  Minimize2Icon as Minimize2,
  XIcon as X,
} from "@lucide/vue";

const props = defineProps({
  psePageBase: { type: Number, default: 1 },
});

const emit = defineEmits([
  "read-gallery",
  "archive-gallery",
  "favorite-gallery",
]);

const s = useApp();

const EH_CATEGORIES = [
  { key: "Doujinshi", label: "Doujinshi", color: "#fc4e4e" },
  { key: "Manga", label: "Manga", color: "#fcb417" },
  { key: "Artist CG", label: "Artist CG", color: "#dde500" },
  { key: "Game CG", label: "Game CG", color: "#05bf0b" },
  { key: "Western", label: "Western", color: "#14e723" },
  { key: "Non-H", label: "Non-H", color: "#68c9de" },
  { key: "Image Set", label: "Image Set", color: "#5050d7" },
  { key: "Cosplay", label: "Cosplay", color: "#9755f5" },
  { key: "Asian Porn", label: "Asian Porn", color: "#fe93ff" },
  { key: "Misc", label: "Misc", color: "#9e9e9e" },
];

function getCategoryColor(category) {
  const c = EH_CATEGORIES.find(
    (x) => x.key.toLowerCase() === (category || "").toLowerCase(),
  );
  return c ? c.color : "#9e9e9e";
}

// State
const loading = ref(false);
const error = ref("");
const catalog = ref(null);
const shelfRefs = {};

// Drill-down route state
const activeRoute = computed(() => s.exploreSubRoute.value);
const routeFeed = ref(null);
const routeLoading = ref(false);
const routeError = ref("");
const routePageStack = ref([]);

// Gallery detail drawer
const detailOpen = ref(false);
const selectedPub = ref(null);

// Home Editor drawer
const editorOpen = ref(false);
const editorExpanded = ref(false);

// Local archive lookup
const archivesMap = computed(() => {
  const map = new Map();
  for (const a of s.archives.value || []) {
    const gid = a.active_gid || a.gid;
    const token = a.active_token || a.token;
    map.set(`${gid}:${token}`, a);
    map.set(`${a.gid}:${a.token}`, a);
  }
  return map;
});

const LANGUAGE_DISPLAY_NAMES = {
  // Japanese
  japanese: "日本语",
  ja: "日本语",
  "日文": "日本语",
  "日语": "日本语",
  "日本語": "日本语",
  "日本语": "日本语",

  // English
  english: "English",
  en: "English",
  "英文": "English",
  "英语": "English",

  // Chinese
  chinese: "中文",
  zh: "中文",
  "中文": "中文",
  "汉语": "中文",
  "國語": "中文",
  "国语": "中文",

  // Korean
  korean: "韩语",
  ko: "韩语",
  "韩文": "韩语",
  "韩语": "韩语",
  "한국어": "韩语",

  // European languages
  spanish: "Español",
  es: "Español",
  "西班牙语": "Español",

  french: "Français",
  fr: "Français",
  "法语": "Français",

  german: "Deutsch",
  de: "Deutsch",
  "德语": "Deutsch",

  russian: "Русский",
  ru: "Русский",
  "俄语": "Русский",

  italian: "Italiano",
  it: "Italiano",
  "意大利语": "Italiano",

  portuguese: "Português",
  pt: "Português",
  "葡萄牙语": "Português",
  "pt-br": "Português",
  brazilian: "Português",

  // Other languages
  thai: "ไทย",
  th: "ไทย",
  "泰语": "ไทย",

  vietnamese: "Tiếng Việt",
  vi: "Tiếng Việt",
  "越南语": "Tiếng Việt",

  indonesian: "Indonesia",
  id: "Indonesia",
  "印尼语": "Indonesia",

  polish: "Polski",
  pl: "Polski",
  "波兰语": "Polski",

  dutch: "Nederlands",
  nl: "Nederlands",
  "荷兰语": "Nederlands",

  swedish: "Svenska",
  sv: "Svenska",
  "瑞典语": "Svenska",

  turkish: "Türkçe",
  tr: "Türkçe",
  "土耳其语": "Türkçe",

  ukrainian: "Українська",
  uk: "Українська",
  "乌克兰语": "Українська",
};

function extractLanguage(meta) {
  if (!meta) return "";

  // 1. Check meta.subject tags first (e.g. "language:japanese", "语言:日文")
  const subjects = Array.isArray(meta.subject) ? meta.subject : [];
  for (const s of subjects) {
    if (!s || !s.name) continue;
    const raw = String(s.name).trim();
    const colonIdx = raw.indexOf(":");
    if (colonIdx === -1) continue;

    const ns = raw.slice(0, colonIdx).trim().toLowerCase();
    const key = raw.slice(colonIdx + 1).trim();
    const keyLower = key.toLowerCase();

    if (ns === "language" || ns === "语言" || ns === "lang") {
      // Skip auxiliary tags like "translated", "rewrite", "翻译", "改图"
      if (
        keyLower === "translated" ||
        keyLower === "rewrite" ||
        keyLower === "翻译" ||
        keyLower === "改图"
      ) {
        continue;
      }

      if (LANGUAGE_DISPLAY_NAMES[keyLower]) {
        return LANGUAGE_DISPLAY_NAMES[keyLower];
      }
      if (LANGUAGE_DISPLAY_NAMES[key]) {
        return LANGUAGE_DISPLAY_NAMES[key];
      }
      return key.charAt(0).toUpperCase() + key.slice(1);
    }
  }

  // 2. Fallback to meta.language if subject had no language tag
  let langCode = "";
  if (Array.isArray(meta.language) && meta.language.length > 0) {
    langCode = String(meta.language[0]).trim().toLowerCase();
  } else if (typeof meta.language === "string" && meta.language.trim()) {
    langCode = meta.language.trim().toLowerCase();
  }

  if (langCode) {
    if (LANGUAGE_DISPLAY_NAMES[langCode]) {
      return LANGUAGE_DISPLAY_NAMES[langCode];
    }
    return langCode.toUpperCase();
  }

  return "";
}

function normalizePublication(pub) {
  if (!pub) return null;
  const meta = pub.metadata || {};
  let gid = meta["x:gid"] || 0;
  let token = meta["x:token"] || "";

  if (!gid || !token) {
    const match = (meta.identifier || "").match(
      /urn:ehentai:gallery:(\d+):([a-f0-9]+)/i,
    );
    if (match) {
      gid = parseInt(match[1], 10);
      token = match[2];
    }
  }

  // Cover image
  let coverUrl = "";
  if (pub.images && pub.images.length > 0) {
    coverUrl = pub.images[0].href;
  } else if (gid && token) {
    coverUrl = `/image/${gid}/${token}/thumb`;
  }

  // Stream link
  let streamHref = "";
  let upstreamUrl = "";
  for (const l of pub.links || []) {
    if (
      l.rel === "http://opds-spec.org/acquisition/stream" ||
      l.rel === "stream" ||
      (l.href && l.href.includes("/stream/"))
    ) {
      streamHref = l.href;
    }
    if (l.rel === "alternate" && l.type === "text/html") {
      upstreamUrl = l.href;
    }
  }

  // Check archive & favorite status
  const key = `${gid}:${token}`;
  const archiveItem = archivesMap.value.get(key);

  let archiveBadge = null;
  if (archiveItem) {
    if (archiveItem.status === "ready") {
      archiveBadge = { label: "已归档", variant: "default", isReady: true };
    } else if (
      archiveItem.status === "downloading" ||
      archiveItem.status === "staging"
    ) {
      archiveBadge = { label: "下载中", variant: "secondary", isDownloading: true };
    } else if (archiveItem.status === "failed") {
      archiveBadge = { label: "归档失败", variant: "destructive", isFailed: true };
    } else if (archiveItem.is_favorited || archiveItem.status === "unarchived") {
      archiveBadge = { label: "收藏", variant: "outline", isFavorited: true };
    }
  }

  const category =
    meta["x:category"] ||
    (Array.isArray(meta.subject) && meta.subject[0]?.name) ||
    "";

  const authors = Array.isArray(meta.authors)
    ? meta.authors.map((a) => a.name).join(", ")
    : meta.author || "";

  const language = extractLanguage(meta);

  return {
    gid,
    token,
    title: meta.title || "未知画廊",
    authors,
    category,
    rating: Number(meta["x:rating"] || 0),
    pageCount: Number(meta.numberOfPages || meta["x:page_count"] || 0),
    uploader: meta["x:uploader"] || "",
    publishTime: meta.published || "",
    language,
    coverUrl,
    streamHref,
    upstreamUrl,
    subjects: Array.isArray(meta.subject) ? meta.subject : [],
    archiveBadge,
    archiveItem,
    raw: pub,
  };
}

// Normalized Navigation Pills
const navigationItems = computed(() => {
  if (!catalog.value || !Array.isArray(catalog.value.navigation)) return [];
  return catalog.value.navigation.map((nav) => {
    let icon = Compass;
    const hrefLower = (nav.href || "").toLowerCase();
    const titleLower = (nav.title || "").toLowerCase();

    if (hrefLower.includes("watched") || titleLower.includes("watch")) icon = Eye;
    else if (hrefLower.includes("favorites") || titleLower.includes("fav")) icon = Heart;
    else if (hrefLower.includes("archives") || titleLower.includes("arch")) icon = Package2;
    else if (hrefLower.includes("top") || titleLower.includes("rank") || hrefLower.includes("toplist")) icon = Trophy;
    else if (hrefLower.includes("popular") || titleLower.includes("pop")) icon = Sparkles;
    else if (hrefLower.includes("query=") || hrefLower.includes("search")) icon = Search;
    else if (hrefLower.includes("tag=") || titleLower.includes("tag") || titleLower.includes(":")) icon = Tag;
    else if (titleLower.includes("category") || titleLower.includes("分类")) icon = Layers;

    return {
      title: nav.title,
      href: nav.href,
      icon,
    };
  });
});

// Normalized Publication Shelves
const publicationGroups = computed(() => {
  if (!catalog.value) return [];
  const groups = [];

  // 1. Grouped sections
  if (Array.isArray(catalog.value.groups)) {
    catalog.value.groups.forEach((g, index) => {
      const pubs = (g.publications || []).map(normalizePublication).filter(Boolean);
      if (pubs.length === 0) return;

      const selfLink = (g.links || []).find(
        (l) => l.rel === "self" || l.rel === "http://opds-spec.org/acquisition",
      );
      const title = g.metadata?.title || `Section ${index + 1}`;

      groups.push({
        id: `group-${index}`,
        title,
        publications: pubs,
        link: {
          title,
          href: selfLink?.href || "",
        },
      });
    });
  }

  // 2. Root publications if no groups
  if (groups.length === 0 && Array.isArray(catalog.value.publications)) {
    const pubs = catalog.value.publications
      .map(normalizePublication)
      .filter(Boolean);
    if (pubs.length > 0) {
      groups.push({
        id: "root-group",
        title: catalog.value.metadata?.title || "最新精选",
        publications: pubs,
        link: null,
      });
    }
  }

  return groups;
});

// Shelf horizontal scrolling
function setShelfRef(id, el) {
  if (el) shelfRefs[id] = el;
}

function scrollShelf(id, direction) {
  const el = shelfRefs[id];
  if (!el) return;
  const distance = Math.max(300, el.clientWidth * 0.75) * direction;
  el.scrollBy({ left: distance, behavior: "smooth" });
}

// Fetch root catalog
async function loadCatalog() {
  loading.value = true;
  error.value = "";
  try {
    const res = await s.api(`/opds/v2.0?_t=${Date.now()}`, {
      cache: "no-store",
      headers: {
        Accept: "application/opds+json, application/json",
        "Cache-Control": "no-cache",
      },
    });
    catalog.value = res.data;
  } catch (err) {
    error.value = err.message || "加载 OPDS 目录失败";
  } finally {
    loading.value = false;
  }
}

// Drill-Down into a specific feed
function drillDown(navItem) {
  if (!navItem || !navItem.href) return;
  routePageStack.value = [];
  s.enterExploreSubRoute(
    {
      title: navItem.title || "画廊列表",
      href: navItem.href,
    },
    true,
  );
}

async function loadRouteFeed(url) {
  if (!url) return;
  routeLoading.value = true;
  routeError.value = "";
  try {
    const res = await s.api(url, {
      headers: { Accept: "application/opds+json, application/json" },
    });
    routeFeed.value = res.data;
  } catch (err) {
    routeError.value = err.message || "加载列表失败";
  } finally {
    routeLoading.value = false;
  }
}

function exitDrillDown() {
  routePageStack.value = [];
  s.exitExploreSubRoute(true);
}

const drillDownPublications = computed(() => {
  if (!routeFeed.value || !Array.isArray(routeFeed.value.publications)) return [];
  return routeFeed.value.publications.map(normalizePublication).filter(Boolean);
});

const nextLink = computed(() => {
  if (!routeFeed.value || !Array.isArray(routeFeed.value.links)) return null;
  return routeFeed.value.links.find((l) => l.rel === "next")?.href || null;
});

const prevLink = computed(() => {
  if (!routeFeed.value || !Array.isArray(routeFeed.value.links)) return null;
  return (
    routeFeed.value.links.find(
      (l) => l.rel === "previous" || l.rel === "prev",
    )?.href || null
  );
});

async function goNextPage() {
  if (!nextLink.value || !activeRoute.value) return;
  routePageStack.value.push(activeRoute.value.href);
  s.enterExploreSubRoute(
    {
      title: activeRoute.value.title,
      href: nextLink.value,
    },
    true,
  );
  window.scrollTo({ top: 0, behavior: "smooth" });
}

async function goPrevPage() {
  if (routePageStack.value.length > 0) {
    const prevUrl = routePageStack.value.pop();
    if (activeRoute.value) {
      s.enterExploreSubRoute(
        {
          title: activeRoute.value.title,
          href: prevUrl,
        },
        true,
      );
    }
    window.scrollTo({ top: 0, behavior: "smooth" });
  } else if (prevLink.value && activeRoute.value) {
    s.enterExploreSubRoute(
      {
        title: activeRoute.value.title,
        href: prevLink.value,
      },
      true,
    );
    window.scrollTo({ top: 0, behavior: "smooth" });
  }
}

// Watch active subRoute
watch(
  () => s.exploreSubRoute.value?.href,
  (newHref) => {
    if (newHref) {
      loadRouteFeed(newHref);
    } else {
      routeFeed.value = null;
      routeError.value = "";
      routePageStack.value = [];
    }
  },
  { immediate: true },
);

// Gallery Actions
function openGallery(pub) {
  selectedPub.value = pub;
  detailOpen.value = true;
}

function handleRead(pub) {
  detailOpen.value = false;
  emit("read-gallery", {
    gid: pub.gid,
    token: pub.token,
    title: pub.title,
    page_count: pub.pageCount || 0,
  });
}

function handleArchive(pub) {
  emit("archive-gallery", pub);
}

function handleFavorite(pub) {
  emit("favorite-gallery", pub);
}

// Watch for home config changes saved in the HomeEditor drawer
watch(
  [() => s.homeTag.value, () => s.home.value],
  () => {
    loadCatalog();
  },
);

watch(editorOpen, (open, oldOpen) => {
  if (open) {
    s.loadHome();
  } else if (oldOpen) {
    loadCatalog();
  }
});

onMounted(() => {
  loadCatalog();
});
</script>

<template>
  <div class="space-y-6">
    <!-- Top Action Bar -->
    <div class="flex items-center justify-between">
      <div v-if="activeRoute" class="flex items-center gap-2">
        <Button
          variant="ghost"
          size="icon"
          class="size-8 text-muted-foreground hover:text-foreground"
          title="返回主页"
          @click="exitDrillDown"
        >
          <ArrowLeft class="size-4" />
        </Button>
        <h2 class="text-xl font-bold tracking-tight text-foreground">{{ activeRoute.title }}</h2>
      </div>
      <div v-else>
        <h2 class="text-xl font-bold tracking-tight text-foreground">主页</h2>
      </div>

      <div class="flex items-center gap-2">
        <Button
          variant="outline"
          size="sm"
          class="h-8 gap-1.5"
          :disabled="loading || routeLoading"
          @click="activeRoute ? loadRouteFeed(activeRoute.href) : loadCatalog()"
        >
          <RefreshCw
            :class="['size-3.5', (loading || routeLoading) && 'animate-spin']"
          />
          刷新
        </Button>
        <Button
          variant="secondary"
          size="sm"
          class="h-8 gap-1.5"
          @click="editorOpen = true"
        >
          <PanelsTopLeft class="size-3.5" /> 编排主页
        </Button>
      </div>
    </div>

    <!-- Error Alert -->
    <div
      v-if="error || routeError"
      class="rounded-lg border border-destructive/50 bg-destructive/10 p-4 text-sm text-destructive"
    >
      {{ error || routeError }}
    </div>

    <!-- Loading Skeleton -->
    <div v-if="loading && !catalog" class="space-y-6 animate-pulse">
      <div class="flex flex-wrap gap-2.5 pt-0.5">
        <div
          v-for="i in 4"
          :key="i"
          class="h-9 w-28 bg-muted/60 rounded-full"
        />
      </div>
      <div v-for="i in 2" :key="i" class="space-y-3">
        <div class="h-6 w-36 bg-muted rounded" />
        <div class="flex gap-4 overflow-hidden">
          <div
            v-for="j in 6"
            :key="j"
            class="h-64 w-44 bg-muted rounded-lg shrink-0"
          />
        </div>
      </div>
    </div>

    <!-- ================= CATALOG HOME VIEW ================= -->
    <div v-else-if="!activeRoute" class="space-y-8">
      <!-- 1. Top Navigation Bar (Independent Premium Navigation Capsules) -->
      <nav
        v-if="navigationItems.length"
        class="flex flex-wrap items-center gap-2.5 pt-0.5"
        aria-label="主页导航"
      >
        <button
          v-for="nav in navigationItems"
          :key="nav.href"
          type="button"
          class="group inline-flex items-center gap-2 h-9 px-3.5 text-xs sm:text-sm font-medium rounded-full border border-border/80 bg-card hover:bg-accent/40 hover:border-border text-foreground shadow-2xs hover:shadow-xs transition-all duration-200 active:scale-[0.98] cursor-pointer select-none"
          @click="drillDown(nav)"
        >
          <component
            :is="nav.icon"
            class="size-4 shrink-0 text-muted-foreground transition-colors group-hover:text-foreground"
          />
          <span class="tracking-tight text-foreground/90 group-hover:text-foreground">
            {{ nav.title }}
          </span>
          <ChevronRight
            class="size-3.5 text-muted-foreground/40 transition-all duration-200 group-hover:text-foreground/80 group-hover:translate-x-0.5"
          />
        </button>
      </nav>

      <!-- 2. Horizontal Carousel Shelves -->
      <div
        v-for="group in publicationGroups"
        :key="group.id"
        class="space-y-2.5"
      >
        <!-- Shelf Header -->
        <div class="flex items-center justify-between">
          <div class="flex items-center gap-2">
            <h3 class="text-base font-semibold tracking-tight">
              {{ group.title }}
            </h3>
            <span class="text-xs text-muted-foreground">
              ({{ group.publications.length }})
            </span>
          </div>

          <div class="flex items-center gap-1">
            <Button
              variant="ghost"
              size="icon"
              class="size-7 rounded-full"
              title="向左滚动"
              @click="scrollShelf(group.id, -1)"
            >
              <ChevronLeft class="size-4" />
            </Button>
            <Button
              variant="ghost"
              size="icon"
              class="size-7 rounded-full"
              title="向右滚动"
              @click="scrollShelf(group.id, 1)"
            >
              <ChevronRight class="size-4" />
            </Button>
            <Button
              v-if="group.link && group.link.href"
              variant="link"
              size="sm"
              class="text-xs font-medium h-7 px-2 text-primary gap-0.5"
              @click="drillDown(group.link)"
            >
              <span>查看全部</span>
              <ChevronRight class="size-3" />
            </Button>
          </div>
        </div>

        <!-- Horizontal Shelf Track -->
        <div
          :ref="(el) => setShelfRef(group.id, el)"
          class="flex gap-3.5 overflow-x-auto scroll-smooth py-1 no-scrollbar select-none"
        >
          <div
            v-for="pub in group.publications"
            :key="pub.gid + ':' + pub.token"
            class="group relative w-[148px] sm:w-[164px] md:w-[180px] shrink-0 cursor-pointer transition-transform duration-200 hover:-translate-y-1"
            @click="openGallery(pub)"
          >
            <!-- Card Cover -->
            <div
              class="relative aspect-[1/1.414] w-full overflow-hidden rounded-lg border bg-muted shadow-sm transition-shadow group-hover:shadow-md"
            >
              <CoverImage :src="pub.coverUrl" :alt="pub.title" />

              <!-- Overlay Gradient (only covering bottom 1/2) -->
              <div
                class="absolute bottom-0 inset-x-0 h-1/2 bg-gradient-to-t from-black/80 via-black/25 to-transparent pointer-events-none"
              />

              <!-- Top Left: Category Badge -->
              <div
                v-if="pub.category"
                class="absolute top-1.5 left-1.5 px-1.5 py-0.5 text-[10px] font-bold rounded text-white shadow-sm"
                :style="{ backgroundColor: getCategoryColor(pub.category) }"
              >
                {{ pub.category }}
              </div>

              <!-- Top Right: Archive / Favorite Badge -->
              <div v-if="pub.archiveBadge" class="absolute top-1.5 right-1.5">
                <Badge
                  :variant="pub.archiveBadge.variant"
                  class="text-[10px] px-1.5 py-0 font-medium"
                >
                  {{ pub.archiveBadge.label }}
                </Badge>
              </div>

              <!-- Bottom info -->
              <div
                class="absolute bottom-1.5 left-1.5 right-1.5 flex items-center justify-between text-[11px] text-white/90 font-medium"
              >
                <div class="flex items-center gap-0.5 shrink-0">
                  <Star class="size-3 fill-amber-400 text-amber-400" />
                  <span>{{ pub.rating.toFixed(1) }}</span>
                </div>
                <span
                  v-if="pub.language || pub.pageCount > 0"
                  class="truncate ml-1.5 text-right drop-shadow-xs"
                  :title="pub.language && pub.pageCount > 0 ? `${pub.language} · ${pub.pageCount}P` : (pub.language || `${pub.pageCount}P`)"
                >
                  <template v-if="pub.language && pub.pageCount > 0">
                    <span>{{ pub.language }}</span>
                    <span class="mx-1 opacity-70">·</span>
                    <span class="font-mono">{{ pub.pageCount }}P</span>
                  </template>
                  <template v-else-if="pub.pageCount > 0">
                    <span class="font-mono">{{ pub.pageCount }}P</span>
                  </template>
                  <template v-else>
                    <span>{{ pub.language }}</span>
                  </template>
                </span>
              </div>
            </div>

            <!-- Title -->
            <div
              class="mt-1.5 text-xs font-medium leading-snug line-clamp-2 transition-colors group-hover:text-primary"
              :title="pub.title"
            >
              {{ pub.title }}
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- ================= DRILL-DOWN LIST VIEW ================= -->
    <div v-else class="space-y-6">
      <div v-if="routeLoading" class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 gap-4 animate-pulse">
        <div v-for="i in 12" :key="i" class="aspect-[1/1.414] bg-muted rounded-lg" />
      </div>

      <div
        v-else-if="drillDownPublications.length === 0"
        class="py-12 text-center text-sm text-muted-foreground"
      >
        该分类下暂无画廊或数据为空。
      </div>

      <div
        v-else
        class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 gap-4"
      >
        <div
          v-for="pub in drillDownPublications"
          :key="pub.gid + ':' + pub.token"
          class="group relative flex flex-col cursor-pointer transition-transform duration-200 hover:-translate-y-1"
          @click="openGallery(pub)"
        >
          <!-- Cover -->
          <div
            class="relative aspect-[1/1.414] w-full overflow-hidden rounded-lg border bg-muted shadow-sm transition-shadow group-hover:shadow-md"
          >
            <CoverImage :src="pub.coverUrl" :alt="pub.title" />

            <!-- Overlay Gradient (only covering bottom 1/2) -->
            <div
              class="absolute bottom-0 inset-x-0 h-1/2 bg-gradient-to-t from-black/80 via-black/25 to-transparent pointer-events-none"
            />

            <!-- Category -->
            <div
              v-if="pub.category"
              class="absolute top-1.5 left-1.5 px-1.5 py-0.5 text-[10px] font-bold rounded text-white shadow-sm"
              :style="{ backgroundColor: getCategoryColor(pub.category) }"
            >
              {{ pub.category }}
            </div>

            <!-- Archive Badge -->
            <div v-if="pub.archiveBadge" class="absolute top-1.5 right-1.5">
              <Badge
                :variant="pub.archiveBadge.variant"
                class="text-[10px] px-1.5 py-0 font-medium"
              >
                {{ pub.archiveBadge.label }}
              </Badge>
            </div>

            <!-- Bottom info -->
            <div
              class="absolute bottom-1.5 left-1.5 right-1.5 flex items-center justify-between text-[11px] text-white/90 font-medium"
            >
              <div class="flex items-center gap-0.5 shrink-0">
                <Star class="size-3 fill-amber-400 text-amber-400" />
                <span>{{ pub.rating.toFixed(1) }}</span>
              </div>
              <span
                v-if="pub.language || pub.pageCount > 0"
                class="truncate ml-1.5 text-right drop-shadow-xs"
                :title="pub.language && pub.pageCount > 0 ? `${pub.language} · ${pub.pageCount}P` : (pub.language || `${pub.pageCount}P`)"
              >
                <template v-if="pub.language && pub.pageCount > 0">
                  <span>{{ pub.language }}</span>
                  <span class="mx-1 opacity-70">·</span>
                  <span class="font-mono">{{ pub.pageCount }}P</span>
                </template>
                <template v-else-if="pub.pageCount > 0">
                  <span class="font-mono">{{ pub.pageCount }}P</span>
                </template>
                <template v-else>
                  <span>{{ pub.language }}</span>
                </template>
              </span>
            </div>
          </div>

          <!-- Title -->
          <div
            class="mt-1.5 text-xs font-medium leading-snug line-clamp-2 transition-colors group-hover:text-primary"
            :title="pub.title"
          >
            {{ pub.title }}
          </div>
        </div>
      </div>

      <!-- Pagination Controls -->
      <div
        v-if="nextLink || routePageStack.length > 0 || prevLink"
        class="flex items-center justify-center gap-3 pt-6 border-t"
      >
        <Button
          variant="outline"
          size="sm"
          class="gap-1"
          :disabled="routePageStack.length === 0 && !prevLink"
          @click="goPrevPage"
        >
          <ChevronLeft class="size-4" /> 上一页
        </Button>
        <Button
          variant="outline"
          size="sm"
          class="gap-1"
          :disabled="!nextLink"
          @click="goNextPage"
        >
          下一页 <ChevronRight class="size-4" />
        </Button>
      </div>
    </div>

    <!-- ================= GALLERY DETAILS & ACTION SHEET ================= -->
    <Sheet v-model:open="detailOpen">
      <SheetContent
        side="right"
        class="w-full sm:max-w-md overflow-y-auto space-y-5 p-6"
      >
        <SheetHeader class="space-y-1 text-left">
          <div class="flex items-center gap-2">
            <span
              v-if="selectedPub?.category"
              class="px-2 py-0.5 text-xs font-bold rounded text-white"
              :style="{
                backgroundColor: getCategoryColor(selectedPub.category),
              }"
            >
              {{ selectedPub.category }}
            </span>
            <Badge
              v-if="selectedPub?.archiveBadge"
              :variant="selectedPub.archiveBadge.variant"
              class="text-xs"
            >
              {{ selectedPub.archiveBadge.label }}
            </Badge>
          </div>
          <SheetTitle class="text-base font-bold leading-tight">
            {{ selectedPub?.title }}
          </SheetTitle>
          <SheetDescription v-if="selectedPub?.authors" class="text-xs">
            作者：{{ selectedPub.authors }}
          </SheetDescription>
        </SheetHeader>

        <!-- Preview Cover -->
        <div
          v-if="selectedPub"
          class="relative aspect-[1/1.414] w-48 mx-auto overflow-hidden rounded-lg border bg-muted shadow-sm"
        >
          <CoverImage :src="selectedPub.coverUrl" :alt="selectedPub.title" />
        </div>

        <!-- Metadata Information -->
        <div v-if="selectedPub" class="space-y-2 text-xs text-muted-foreground">
          <div class="flex items-center justify-between py-1 border-b">
            <span>评分</span>
            <div class="flex items-center gap-1 font-semibold text-foreground">
              <Star class="size-3.5 fill-amber-400 text-amber-400" />
              <span>{{ selectedPub.rating.toFixed(2) }}</span>
            </div>
          </div>
          <div
            v-if="selectedPub.language"
            class="flex items-center justify-between py-1 border-b"
          >
            <span>语言</span>
            <span class="font-medium text-foreground">
              {{ selectedPub.language }}
            </span>
          </div>
          <div class="flex items-center justify-between py-1 border-b">
            <span>页数</span>
            <span class="font-medium text-foreground">
              {{ selectedPub.pageCount || "未知" }} 页
            </span>
          </div>
          <div
            v-if="selectedPub.uploader"
            class="flex items-center justify-between py-1 border-b"
          >
            <span>上传者</span>
            <span class="font-medium text-foreground">
              {{ selectedPub.uploader }}
            </span>
          </div>
          <div
            v-if="selectedPub.publishTime"
            class="flex items-center justify-between py-1 border-b"
          >
            <span>发布日期</span>
            <span class="font-medium text-foreground">
              {{ selectedPub.publishTime }}
            </span>
          </div>
        </div>

        <!-- Tags List -->
        <div
          v-if="selectedPub?.subjects && selectedPub.subjects.length > 0"
          class="space-y-1.5"
        >
          <span class="text-xs font-semibold text-muted-foreground">标签</span>
          <div class="flex flex-wrap gap-1.5 max-h-36 overflow-y-auto pr-1">
            <Badge
              v-for="sub in selectedPub.subjects"
              :key="sub.name"
              variant="secondary"
              class="text-[11px] font-normal"
            >
              {{ sub.name }}
            </Badge>
          </div>
        </div>

        <!-- Action Buttons -->
        <div v-if="selectedPub" class="space-y-2 pt-2">
          <!-- 1. 在线阅读 (Main button) -->
          <Button
            class="w-full gap-2 font-medium"
            @click="handleRead(selectedPub)"
          >
            <BookOpen class="size-4" /> 在线阅读
          </Button>

          <div class="grid grid-cols-2 gap-2">
            <!-- 2. 加入收藏 -->
            <Button
              variant="outline"
              class="gap-1.5 text-xs"
              @click="handleFavorite(selectedPub)"
            >
              <Bookmark class="size-3.5" /> 加入收藏
            </Button>

            <!-- 3. 开始归档 -->
            <Button
              variant="outline"
              class="gap-1.5 text-xs"
              @click="handleArchive(selectedPub)"
            >
              <Download class="size-3.5" />
              {{ selectedPub.archiveBadge?.isReady ? "已归档" : "开始归档" }}
            </Button>
          </div>

          <!-- 4. 原站链接 -->
          <Button
            v-if="selectedPub.upstreamUrl"
            variant="ghost"
            class="w-full gap-1.5 text-xs text-muted-foreground"
            as="a"
            :href="selectedPub.upstreamUrl"
            target="_blank"
            rel="noopener noreferrer"
          >
            <ExternalLink class="size-3.5" /> 在 E-Hentai 原站打开
          </Button>
        </div>
      </SheetContent>
    </Sheet>

    <!-- ================= WYSIWYG HOME EDITOR DRAWER ================= -->
    <Sheet v-model:open="editorOpen">
      <SheetContent
        side="right"
        :class="[
          'overflow-y-auto space-y-4 p-6 transition-all duration-300',
          editorExpanded
            ? 'w-full sm:max-w-[95vw]'
            : 'w-full sm:max-w-[750px]',
        ]"
      >
        <SheetHeader class="text-left border-b pb-3 pr-10">
          <div class="flex items-center justify-between">
            <div>
              <SheetTitle class="text-lg font-bold">首页编排 (Home.toml)</SheetTitle>
              <SheetDescription class="text-xs">
                修改保存后，主页将所见即所得地自动刷新展示效果。
              </SheetDescription>
            </div>
            <div class="flex items-center gap-2 mr-6">
              <Button
                variant="outline"
                size="sm"
                class="h-8 gap-1.5 text-xs"
                @click="editorExpanded = !editorExpanded"
              >
                <component :is="editorExpanded ? Minimize2 : Maximize2" class="size-3.5" />
                <span>{{ editorExpanded ? "标准宽度" : "宽屏模式" }}</span>
              </Button>
            </div>
          </div>
        </SheetHeader>
        <div class="pt-2">
          <HomeEditor />
        </div>
      </SheetContent>
    </Sheet>
  </div>
</template>

<style scoped>
/* Utility to hide scrollbars while preserving smooth scroll */
.no-scrollbar::-webkit-scrollbar {
  display: none;
}
.no-scrollbar {
  -ms-overflow-style: none;
  scrollbar-width: none;
}
</style>

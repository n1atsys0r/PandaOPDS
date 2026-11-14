<script setup>
import { ref, computed, watch, onMounted, onUnmounted } from "vue";
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
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogClose,
} from "@/components/ui/dialog";
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
  InfoIcon as Info,
  MessageSquareIcon as MessageSquare,
  GridIcon as Grid,
} from "@lucide/vue";

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

// Search state & OpenSearch logic
const searchInput = ref("");

function getOpenSearchUrl(query) {
  const q = (query || "").trim();
  if (!q) return "";

  // 1. Discover search template from catalog or current routeFeed links
  const allLinks = [
    ...(catalog.value?.links || []),
    ...(routeFeed.value?.links || []),
  ];
  const searchLink = allLinks.find((l) => l.rel === "search");

  if (searchLink?.href) {
    if (searchLink.href.includes("{searchTerms}")) {
      return searchLink.href.replace("{searchTerms}", encodeURIComponent(q));
    }
    const sep = searchLink.href.includes("?") ? "&" : "?";
    return `${searchLink.href}${sep}query=${encodeURIComponent(q)}`;
  }

  // 2. Fallback to standard OPDS v2 gallery search endpoint
  return `/opds/v2.0/gallery?query=${encodeURIComponent(q)}`;
}

function formatTagForSearch(tag) {
  if (!tag) return "";
  const t = tag.trim();
  if (t.startsWith('"') && t.endsWith('"')) return t;

  const colonIdx = t.indexOf(":");
  if (colonIdx !== -1) {
    const prefix = t.slice(0, colonIdx).trim();
    const val = t.slice(colonIdx + 1).trim();
    if (val.includes(" ") && !val.startsWith('"')) {
      return `${prefix}:"${val}$"`;
    }
  } else if (t.includes(" ") && !t.startsWith('"')) {
    return `"${t}$"`;
  }
  return t;
}

function executeSearch(query, displayTitle) {
  const q = (query || "").trim();
  if (!q) return;

  const targetUrl = getOpenSearchUrl(q);
  routePageStack.value = [];
  detailOpen.value = false;

  const title = displayTitle || `搜索: ${q}`;
  s.enterExploreSubRoute(
    {
      title,
      href: targetUrl,
    },
    true,
  );

  // If navigating to the same URL, explicitly reload
  if (activeRoute.value?.href === targetUrl) {
    loadRouteFeed(targetUrl);
  }
}

function handleSearchSubmit() {
  if (!searchInput.value.trim()) return;
  executeSearch(searchInput.value.trim());
}

function clearSearch() {
  searchInput.value = "";
}

function handleTagClick(tagName) {
  if (!tagName) return;
  const query = formatTagForSearch(tagName);
  executeSearch(query, `标签: ${tagName}`);
}

// Drill-down route state
const activeRoute = computed(() => s.exploreSubRoute.value);
const routeFeed = ref(null);
const routeLoading = ref(false);
const routeError = ref("");
const routePageStack = ref([]);
let catalogFeedHref = window.location.href;
let routeFeedHref = window.location.href;

// Gallery detail drawer
const detailOpen = ref(false);
const selectedPub = ref(null);
const detailSubView = ref("main"); // "main" | "reviews" | "thumbnails"

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

function normalizePublication(pub, feedHref = window.location.href) {
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

  // Detail link
  let detailHref = "";
  let upstreamUrl = "";
  for (const l of pub.links || []) {
    if (l.rel === "self" && l.href) {
      const type = String(l.type || "").toLowerCase();
      if (
        type.startsWith("application/divina+json") ||
        type.startsWith("application/opds+json")
      ) {
        detailHref = localApiUrl(l.href, feedHref);
      }
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

  const reviews = Array.isArray(meta["x:reviews"]) ? meta["x:reviews"] : [];

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
    detailHref,
    feedHref,
    upstreamUrl,
    subjects: Array.isArray(meta.subject) ? meta.subject : [],
    archiveBadge,
    archiveItem,
    raw: pub,
    reviews,
    readingOrder: [],
  };
}

function localApiUrl(href, base = window.location.href) {
  if (!href) return "";
  try {
    if (href.startsWith("//")) return "";
    const url = new URL(href, base);
    // s.api adds the session Authorization header, so never pass it a foreign origin.
    if (url.origin !== window.location.origin) return "";
    return url.href;
  } catch {
    return "";
  }
}

const detailRequests = new Map();
function ensurePublicationDetails(pub) {
  if (!pub?.gid || !pub?.token) return Promise.resolve(null);
  if (!pub.detailHref) return Promise.resolve(pub);
  if (pub.detailLoaded) return Promise.resolve(pub);
  const href = pub.detailHref;
  let task = detailRequests.get(href);
  if (!task) {
    task = s.api(href, {
      headers: { Accept: "application/divina+json, application/opds+json" },
    })
      .then(({ data }) => data)
      .finally(() => detailRequests.delete(href));
    detailRequests.set(href, task);
  }
  return task.then((data) => {
    if (pub.detailLoaded) return pub;
    const metadata = data?.metadata || {};
    const subjects = [...(pub.subjects || [])];
    for (const subject of Array.isArray(metadata.subject) ? metadata.subject : []) {
      if (!subject?.name) continue;
      const index = subjects.findIndex((item) => item?.name === subject.name);
      if (index < 0) {
        subjects.push(subject);
      } else {
        const mergedStyle = subject["x:style"] || subjects[index]["x:style"];
        subjects[index] = {
          ...subjects[index],
          ...subject,
          ...(mergedStyle ? { "x:style": mergedStyle } : {}),
        };
      }
    }
    const sourceLinks = [...(data.links || [])];
    for (const link of pub.raw?.links || []) {
      if (!sourceLinks.some((item) => item.rel === link.rel && item.href === link.href)) {
        sourceLinks.push(link);
      }
    }
    const normalized = normalizePublication(
      {
        metadata: { ...pub.raw?.metadata, ...metadata },
        links: sourceLinks,
        images: pub.raw?.images || [],
      },
      href,
    );
    const rawPages = Array.isArray(data.readingOrder) ? data.readingOrder : [];
    const pages = rawPages.map((page, index) => ({
      pageNumber: index + 1,
      href: localApiUrl(page?.href, href),
      thumbnailHref: localApiUrl(page?.properties?.["x:thumbnail"]?.href, href),
    }));
    const validPages = pages.length > 0 && pages.every((page) => page.href);
    const reviews = Array.isArray(metadata["x:reviews"])
      ? metadata["x:reviews"]
      : (normalized.reviews || pub.reviews || []);
    Object.assign(pub, normalized, {
      subjects,
      reviews,
      detailLoaded: true,
      readingOrder: validPages ? pages : [],
      pageCount: validPages ? pages.length : (normalized.pageCount || pub.pageCount),
    });
    return pub;
  }).catch(() => {
      return pub;
    });
}

watch([selectedPub, detailOpen], ([pub, open]) => {
  if (pub && open) ensurePublicationDetails(pub);
  if (!open) detailSubView.value = "main";
});

const previewPages = computed(() => {
  return (selectedPub.value?.readingOrder || []).slice(0, 16);
});

const previewReviews = computed(() => {
  return (selectedPub.value?.reviews || []).slice(0, 6);
});

function stripHtmlToSnippet(html, maxLength = 120) {
  if (!html) return "";
  if (typeof document === "undefined") {
    return html.replace(/<[^>]*>/g, "").slice(0, maxLength);
  }
  const tmp = document.createElement("div");
  tmp.innerHTML = html;
  const text = (tmp.textContent || tmp.innerText || "").trim().replace(/\s+/g, " ");
  if (text.length <= maxLength) return text;
  return text.slice(0, maxLength) + "…";
}

// Progressive chunked rendering for large thumbnail lists & reviews
const thumbnailRenderLimit = ref(64);
const reviewRenderLimit = ref(20);
const pageJumpInput = ref("");

const displayedThumbnails = computed(() => {
  return (selectedPub.value?.readingOrder || []).slice(
    0,
    thumbnailRenderLimit.value,
  );
});

const displayedReviews = computed(() => {
  return (selectedPub.value?.reviews || []).slice(0, reviewRenderLimit.value);
});

function formatReviewHtml(html) {
  if (!html) return "";
  // Injects loading="lazy" decoding="async" to <img> to prevent network choke
  let res = html.replace(
    /<img\s+(?![^>]*\bloading=)/gi,
    '<img loading="lazy" decoding="async" ',
  );
  // Injects target="_blank" rel="noopener noreferrer" to <a> for security
  res = res.replace(
    /<a\s+(?![^>]*\btarget=)/gi,
    '<a target="_blank" rel="noopener noreferrer" ',
  );
  return res;
}

const thumbnailSentinelRef = ref(null);
const reviewSentinelRef = ref(null);
let sentinelObserver = null;

function setupSentinelObserver() {
  if (typeof IntersectionObserver === "undefined") return;
  sentinelObserver?.disconnect();
  sentinelObserver = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        if (!entry.isIntersecting) continue;
        if (entry.target === thumbnailSentinelRef.value) {
          const total = selectedPub.value?.readingOrder?.length || 0;
          if (thumbnailRenderLimit.value < total) {
            thumbnailRenderLimit.value = Math.min(
              total,
              thumbnailRenderLimit.value + 64,
            );
          }
        } else if (entry.target === reviewSentinelRef.value) {
          const total = selectedPub.value?.reviews?.length || 0;
          if (reviewRenderLimit.value < total) {
            reviewRenderLimit.value = Math.min(
              total,
              reviewRenderLimit.value + 20,
            );
          }
        }
      }
    },
    { rootMargin: "450px 0px" },
  );
  if (thumbnailSentinelRef.value) {
    sentinelObserver.observe(thumbnailSentinelRef.value);
  }
  if (reviewSentinelRef.value) {
    sentinelObserver.observe(reviewSentinelRef.value);
  }
}

watch([thumbnailSentinelRef, reviewSentinelRef], () => {
  setupSentinelObserver();
});

watch(detailSubView, (subView) => {
  if (subView === "thumbnails") {
    thumbnailRenderLimit.value = 64;
    pageJumpInput.value = "";
  } else if (subView === "reviews") {
    reviewRenderLimit.value = 20;
  }
  nextTick(() => {
    setupSentinelObserver();
  });
});

function handleQuickJump() {
  const total = selectedPub.value?.readingOrder?.length || 1;
  const target = Math.max(
    1,
    Math.min(Number(pageJumpInput.value) || 1, total),
  );
  thumbnailRenderLimit.value = Math.max(
    thumbnailRenderLimit.value,
    target + 32,
  );
  nextTick(() => {
    const el = document.getElementById(`thumb-page-${target}`);
    if (el) {
      el.scrollIntoView({ block: "center", behavior: "smooth" });
    }
  });
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
      const pubs = (g.publications || []).map((pub) => normalizePublication(pub, catalogFeedHref)).filter(Boolean);
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
      .map((pub) => normalizePublication(pub, catalogFeedHref))
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
    const requestHref = `/opds/v2.0?_t=${Date.now()}`;
    const res = await s.api(requestHref, {
      cache: "no-store",
      headers: {
        Accept: "application/opds+json, application/json",
        "Cache-Control": "no-cache",
      },
    });
    catalogFeedHref = new URL(requestHref, window.location.href).href;
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
    routeFeedHref = localApiUrl(url) || url;
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
  return routeFeed.value.publications.map((pub) => normalizePublication(pub, routeFeedHref)).filter(Boolean);
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

// Sync search input with active search route
watch(
  () => s.exploreSubRoute.value,
  (subRoute) => {
    if (!subRoute) {
      searchInput.value = "";
      return;
    }
    try {
      const u = new URL(subRoute.href, "http://localhost");
      const q = u.searchParams.get("query");
      if (
        q &&
        q !== "popular" &&
        q !== "watched" &&
        q !== "favorites" &&
        !q.startsWith("favorites:")
      ) {
        searchInput.value = q;
      }
    } catch {
      // ignore
    }
  },
  { immediate: true },
);

// Active list tracking for adjacent navigation & recommendations
const currentPubList = ref([]);
const currentIndex = computed(() => {
  if (!selectedPub.value || !currentPubList.value.length) return -1;
  return currentPubList.value.findIndex(
    (p) => p.gid === selectedPub.value.gid && p.token === selectedPub.value.token,
  );
});

const hasPrev = computed(() => currentIndex.value > 0);
const hasNext = computed(
  () =>
    currentIndex.value >= 0 &&
    currentIndex.value < currentPubList.value.length - 1,
);

const modalScrollRef = ref(null);
function scrollModalToTop() {
  if (modalScrollRef.value) {
    modalScrollRef.value.scrollTop = 0;
  }
}

function prevGallery() {
  if (hasPrev.value) {
    selectedPub.value = currentPubList.value[currentIndex.value - 1];
    detailSubView.value = "main";
    scrollModalToTop();
  }
}

function nextGallery() {
  if (hasNext.value) {
    selectedPub.value = currentPubList.value[currentIndex.value + 1];
    detailSubView.value = "main";
    scrollModalToTop();
  }
}

function selectPublication(pub) {
  selectedPub.value = pub;
  detailSubView.value = "main";
  scrollModalToTop();
}

// Canonical namespace weights matching OPDS standard & E-Hentai ordering
const NAMESPACE_WEIGHTS = {
  category: 0,
  分类: 0,
  language: 1,
  语言: 1,
  parody: 2,
  原作: 2,
  character: 3,
  角色: 3,
  cosplayer: 4,
  coser: 4,
  group: 5,
  团队: 5,
  社团: 5,
  artist: 6,
  艺术家: 6,
  作者: 6,
  female: 7,
  女性: 7,
  male: 8,
  男性: 8,
  mixed: 9,
  混合: 9,
  location: 10,
  地点: 10,
  other: 11,
  其他: 11,
  temp: 12,
  misc: 13,
};

function getNamespaceWeight(ns) {
  const lower = String(ns || "").trim().toLowerCase();
  return NAMESPACE_WEIGHTS[lower] ?? 99;
}

function parseTagStyle(rawStyle) {
  if (!rawStyle) return null;
  const style = {};
  if (typeof rawStyle === "object") {
    if (rawStyle.color) style.color = rawStyle.color;
    if (rawStyle.borderColor || rawStyle["border-color"]) {
      style.borderColor = rawStyle.borderColor || rawStyle["border-color"];
    }
    if (rawStyle.background || rawStyle.backgroundColor || rawStyle["background-color"]) {
      style.background = rawStyle.background || rawStyle.backgroundColor || rawStyle["background-color"];
    }
  } else if (typeof rawStyle === "string") {
    for (const part of rawStyle.split(";")) {
      const [k, v] = part.split(":").map((s) => s.trim());
      if (!k || !v) continue;
      const cleanV = v.replace(/!important/g, "").trim();
      const lk = k.toLowerCase();
      if (lk === "color") style.color = cleanV;
      else if (lk === "border-color" || lk === "bordercolor") style.borderColor = cleanV;
      else if (lk === "background" || lk === "background-color") style.background = cleanV;
    }
  }
  return Object.keys(style).length > 0 ? style : null;
}

const groupedTags = computed(() => {
  if (!selectedPub.value?.subjects || !selectedPub.value.subjects.length) {
    return [];
  }

  const map = new Map();
  let firstSeenCounter = 0;

  for (const item of selectedPub.value.subjects) {
    const rawName = typeof item === "string" ? item : item?.name || "";
    if (!rawName) continue;

    let ns = "other";
    let key = rawName;
    const colonIdx = rawName.indexOf(":");
    if (colonIdx > 0) {
      ns = rawName.slice(0, colonIdx).trim().toLowerCase();
      key = rawName.slice(colonIdx + 1).trim();
    }

    const rawStyle =
      typeof item === "object" ? (item["x:style"] || item.style || null) : null;
    const tagStyle = parseTagStyle(rawStyle);
    const hasCustomStyle = Boolean(tagStyle);
    const isWeak =
      typeof item === "object" &&
      (String(item.status || "").includes("skepticism") ||
       String(item["x:style"] || "").includes("dash") ||
       String(item["x:style"] || "").includes("weak"));

    if (!map.has(ns)) {
      map.set(ns, {
        namespace: ns,
        firstSeenIndex: firstSeenCounter++,
        tags: [],
      });
    }
    map.get(ns).tags.push({
      rawName,
      namespace: ns,
      key,
      displayName: key,
      searchName: rawName,
      isWeak,
      tagStyle,
      hasCustomStyle,
    });
  }

  // 1. Sort namespace rows by canonical weights
  const result = Array.from(map.values()).sort((a, b) => {
    const wa = getNamespaceWeight(a.namespace);
    const wb = getNamespaceWeight(b.namespace);
    if (wa !== wb) return wa - wb;
    return a.firstSeenIndex - b.firstSeenIndex;
  });

  // 2. Sort tags within each group: styled tags come first (stable sort preserving relative order)
  for (const grp of result) {
    grp.tags.sort((a, b) => {
      if (a.hasCustomStyle && !b.hasCustomStyle) return -1;
      if (!a.hasCustomStyle && b.hasCustomStyle) return 1;
      return 0;
    });
  }

  return result;
});

const relatedPublications = computed(() => {
  if (!currentPubList.value.length || !selectedPub.value) return [];
  return currentPubList.value.filter(
    (p) => p.gid !== selectedPub.value.gid || p.token !== selectedPub.value.token,
  );
});

function handleCategoryClick(category) {
  if (!category) return;
  detailOpen.value = false;
  executeSearch(category, `分类: ${category}`);
}

function handleAuthorClick(author) {
  if (!author) return;
  detailOpen.value = false;
  const q = author.includes(" ") ? `artist:"${author}$"` : `artist:${author}$`;
  executeSearch(q, `作者: ${author}`);
}

function handleUploaderClick(uploader) {
  if (!uploader) return;
  detailOpen.value = false;
  executeSearch(`uploader:${uploader}`, `上传者: ${uploader}`);
}

function handleKeyDown(e) {
  if (!detailOpen.value) return;
  if (
    e.target &&
    (e.target.tagName === "INPUT" || e.target.tagName === "TEXTAREA")
  ) {
    return;
  }
  if (e.key === "ArrowLeft") {
    if (detailSubView.value !== "main") return;
    prevGallery();
  } else if (e.key === "ArrowRight") {
    if (detailSubView.value !== "main") return;
    nextGallery();
  }
}

// Gallery Actions
function openGallery(pub, list = null) {
  selectedPub.value = pub;
  detailSubView.value = "main";
  if (list && Array.isArray(list)) {
    currentPubList.value = list;
  }
  detailOpen.value = true;
  scrollModalToTop();
}

async function handleRead(pub, startPage = null) {
  const selectedAtClick = pub;
  const detailedPub = await ensurePublicationDetails(pub);
  if (!detailedPub?.pageCount || !detailOpen.value || selectedPub.value !== selectedAtClick) return;
  detailOpen.value = false;
  const payload = {
    gid: detailedPub.gid,
    token: detailedPub.token,
    title: detailedPub.title,
    page_count: detailedPub.pageCount || 0,
    reading_order: detailedPub.readingOrder || [],
  };
  if (typeof startPage === "number" && startPage >= 0) {
    payload.start_page = startPage;
    payload.initial_page_index = startPage;
  }
  emit("read-gallery", payload);
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
  window.addEventListener("keydown", handleKeyDown);
});

onUnmounted(() => {
  window.removeEventListener("keydown", handleKeyDown);
  sentinelObserver?.disconnect();
  sentinelObserver = null;
});
</script>

<template>
  <div class="space-y-6">
    <!-- Top Action Bar -->
    <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
      <!-- Title & Back Button -->
      <div class="flex items-center gap-2 min-w-0">
        <Button
          v-if="activeRoute"
          variant="ghost"
          size="icon"
          class="size-8 shrink-0 text-muted-foreground hover:text-foreground"
          title="返回探索"
          @click="exitDrillDown"
        >
          <ArrowLeft class="size-4" />
        </Button>
        <h2 class="text-xl font-bold tracking-tight text-foreground truncate">
          {{ activeRoute ? activeRoute.title : "探索" }}
        </h2>
      </div>

      <!-- Search Bar & Actions -->
      <div class="flex flex-wrap sm:flex-nowrap items-center gap-2">
        <!-- Search Input with OpenSearch trigger -->
        <div class="relative flex items-center w-full sm:w-60 md:w-72">
          <Search class="absolute left-2.5 size-3.5 text-muted-foreground pointer-events-none" />
          <Input
            v-model="searchInput"
            placeholder="搜索画廊、标签或关键词..."
            class="h-8 pl-8 pr-7 text-xs bg-card/60 border-border/80 focus-visible:bg-background"
            @keydown.enter="handleSearchSubmit"
          />
          <button
            v-if="searchInput"
            type="button"
            class="absolute right-2 p-0.5 text-muted-foreground hover:text-foreground cursor-pointer rounded-full hover:bg-muted"
            @click="clearSearch"
            title="清空搜索"
          >
            <X class="size-3" />
          </button>
        </div>
        <Button
          variant="default"
          size="sm"
          class="h-8 px-3 text-xs font-medium shrink-0"
          :disabled="!searchInput.trim()"
          @click="handleSearchSubmit"
        >
          搜索
        </Button>

        <div class="flex items-center gap-2 shrink-0">
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
            <span class="hidden sm:inline">刷新</span>
          </Button>
          <Button
            v-if="!activeRoute"
            variant="secondary"
            size="sm"
            class="h-8 gap-1.5"
            @click="editorOpen = true"
          >
            <PanelsTopLeft class="size-3.5" />
            <span class="hidden sm:inline">编排主页</span>
          </Button>
        </div>
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
        aria-label="探索导航"
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
            @click="openGallery(pub, group.publications)"
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
        class="py-16 text-center text-muted-foreground space-y-2"
      >
        <Search class="size-8 mx-auto text-muted-foreground/40" />
        <p class="text-sm font-medium">未找到相关画廊或数据为空。</p>
        <p v-if="activeRoute" class="text-xs text-muted-foreground/70">
          可尝试更换关键词或检查筛选标签重新搜索。
        </p>
      </div>

      <div
        v-else
        class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 gap-4"
      >
        <div
          v-for="pub in drillDownPublications"
          :key="pub.gid + ':' + pub.token"
          class="group relative flex flex-col cursor-pointer transition-transform duration-200 hover:-translate-y-1"
          @click="openGallery(pub, drillDownPublications)"
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

    <!-- ================= APPLE BOOKS STYLE GALLERY DETAIL DIALOG ================= -->
    <Dialog v-model:open="detailOpen">
      <DialogContent
        :show-close-button="false"
        class="w-full max-w-[calc(100%-1.5rem)] sm:max-w-3xl md:max-w-4xl h-[88vh] max-h-[90vh] p-0 rounded-2xl sm:rounded-3xl border border-border/50 shadow-2xl overflow-hidden bg-background flex flex-col"
        @escape-key-down="(e) => { if (detailSubView !== 'main') { e.preventDefault(); detailSubView = 'main'; } }"
      >
        <!-- BASE VIEW: Main Gallery Details (always mounted, preserving scroll position) -->
        <div
          ref="modalScrollRef"
          class="overflow-y-auto flex-1 flex flex-col select-text no-scrollbar"
        >
          <!-- 1. HERO HEADER (Apple Books Dark Slate Banner) -->
          <div
            class="relative bg-gradient-to-b from-[#3a3b40] via-[#2c2e35] to-[#202227] text-white p-5 sm:p-7 shrink-0 overflow-hidden"
          >
            <!-- Ambient Category Glow -->
            <div
              class="absolute -top-24 -left-24 size-96 rounded-full blur-3xl opacity-25 pointer-events-none transition-colors duration-500"
              :style="{
                backgroundColor: getCategoryColor(selectedPub?.category) || '#6366f1',
              }"
            />

            <!-- Top Navigation & Actions Bar -->
            <div class="flex items-center justify-between relative z-10 mb-4 sm:mb-6">
              <!-- Left: Frosted Circular Close Button -->
              <DialogClose as-child>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  class="size-8 rounded-full bg-black/40 hover:bg-black/60 text-white/90 hover:text-white border border-white/10 transition-colors"
                  title="关闭 (Esc)"
                >
                  <X class="size-4" />
                </Button>
              </DialogClose>

              <!-- Right: Quick Action Pill Buttons -->
              <div class="flex items-center gap-1.5 sm:gap-2">
                <!-- Add to Favorite -->
                <Button
                  variant="ghost"
                  size="icon-sm"
                  class="size-8 rounded-full bg-black/35 hover:bg-black/55 text-white/90 hover:text-white border border-white/10 transition-colors"
                  :title="selectedPub?.archiveBadge?.isFavorited ? '已收藏' : '加入收藏'"
                  @click="handleFavorite(selectedPub)"
                >
                  <Bookmark
                    class="size-4"
                    :class="selectedPub?.archiveBadge?.isFavorited ? 'fill-amber-400 text-amber-400' : ''"
                  />
                </Button>

                <!-- Archive -->
                <Button
                  variant="ghost"
                  size="icon-sm"
                  class="size-8 rounded-full bg-black/35 hover:bg-black/55 text-white/90 hover:text-white border border-white/10 transition-colors"
                  :title="selectedPub?.archiveBadge?.isReady ? '已归档' : '开始归档'"
                  @click="handleArchive(selectedPub)"
                >
                  <Download
                    class="size-4"
                    :class="selectedPub?.archiveBadge?.isReady ? 'text-emerald-400' : ''"
                  />
                </Button>

                <!-- Upstream Link -->
                <Button
                  v-if="selectedPub?.upstreamUrl"
                  variant="ghost"
                  size="icon-sm"
                  as="a"
                  :href="selectedPub.upstreamUrl"
                  target="_blank"
                  rel="noopener noreferrer"
                  class="size-8 rounded-full bg-black/35 hover:bg-black/55 text-white/90 hover:text-white border border-white/10 transition-colors"
                  title="在 E-Hentai 原站打开"
                >
                  <ExternalLink class="size-4" />
                </Button>
              </div>
            </div>

            <!-- Hero Main Info: Cover + Typography + Capsule -->
            <div class="flex flex-col sm:flex-row gap-5 sm:gap-7 items-start relative z-10">
              <!-- Left: Prominent Book Cover -->
              <div
                v-if="selectedPub"
                class="relative aspect-[1/1.414] w-36 sm:w-44 md:w-48 shrink-0 mx-auto sm:mx-0 overflow-hidden rounded-xl border border-white/20 shadow-2xl shadow-black/70 bg-black/40 group"
              >
                <CoverImage :src="selectedPub.coverUrl" :alt="selectedPub.title" />
              </div>

              <!-- Right: Metadata & Action Capsule -->
              <div class="flex-1 min-w-0 flex flex-col justify-between self-stretch space-y-3.5 sm:space-y-4">
                <div class="space-y-1.5 text-center sm:text-left">
                  <!-- Category Link with Chevron -->
                  <div class="flex items-center justify-center sm:justify-start gap-2">
                    <span
                      v-if="selectedPub?.category"
                      class="inline-flex items-center gap-0.5 text-xs font-semibold tracking-wide text-white/80 hover:text-white transition-colors cursor-pointer"
                      @click="handleCategoryClick(selectedPub.category)"
                      :title="`搜索分类: ${selectedPub.category}`"
                    >
                      <span>{{ selectedPub.category }}</span>
                      <ChevronRight class="size-3 opacity-60" />
                    </span>
                    <Badge
                      v-if="selectedPub?.archiveBadge"
                      :variant="selectedPub.archiveBadge.variant"
                      class="text-[10px] px-1.5 py-0 h-4 border-white/20 bg-white/15 text-white"
                    >
                      {{ selectedPub.archiveBadge.label }}
                    </Badge>
                  </div>

                  <!-- Title -->
                  <DialogTitle class="text-lg sm:text-2xl font-bold text-white leading-snug line-clamp-2 break-words">
                    {{ selectedPub?.title }}
                  </DialogTitle>

                  <!-- Rating & Stats Row -->
                  <div class="flex items-center justify-center sm:justify-start gap-2 text-xs text-white/70 pt-0.5">
                    <div class="flex items-center gap-1 font-semibold text-amber-300">
                      <Star class="size-3.5 fill-amber-300 text-amber-300" />
                      <span>{{ selectedPub?.rating.toFixed(2) }}</span>
                    </div>
                    <span class="opacity-40">·</span>
                    <span>{{ selectedPub?.language || "多语言" }}</span>
                    <span class="opacity-40">·</span>
                    <span>{{ selectedPub?.pageCount || 0 }} 页</span>
                  </div>
                </div>

                <!-- Apple Books Action Capsule -->
                <div
                  v-if="selectedPub"
                  class="bg-white/10 dark:bg-white/5 backdrop-blur-md border border-white/15 rounded-2xl p-3 sm:p-4 flex flex-col sm:flex-row items-center justify-between gap-3 shadow-lg"
                >
                  <div class="text-center sm:text-left text-xs text-white/70 w-full sm:w-auto">
                    <div class="flex items-center justify-center sm:justify-start gap-1.5 font-semibold text-white/95">
                      <span>画廊</span>
                      <Info class="size-3 opacity-60" />
                    </div>
                    <div class="text-[11px] text-white/60 pt-0.5">
                      {{ selectedPub.publishTime ? selectedPub.publishTime.slice(0, 10) : "日期未知" }} · {{ selectedPub.pageCount || 0 }} 页
                    </div>
                  </div>

                  <div class="flex items-center gap-2 w-full sm:w-auto justify-center sm:justify-end">
                    <!-- Secondary Button: Favorite or Archive -->
                    <Button
                      variant="ghost"
                      class="rounded-full bg-white/15 hover:bg-white/25 text-white border border-white/20 text-xs px-3.5 py-1.5 h-8 gap-1.5 font-medium transition-all"
                      @click="handleFavorite(selectedPub)"
                    >
                      <Bookmark class="size-3.5" :class="selectedPub.archiveBadge?.isFavorited ? 'fill-amber-400 text-amber-400' : ''" />
                      <span>{{ selectedPub.archiveBadge?.isFavorited ? "已收藏" : "收藏" }}</span>
                    </Button>

                    <!-- Primary Button: Read Online -->
                    <Button
                      class="rounded-full bg-white text-neutral-900 hover:bg-white/90 font-bold text-xs sm:text-sm px-5 py-1.5 h-8 gap-1.5 shadow-md transition-all active:scale-95"
                      @click="handleRead(selectedPub)"
                    >
                      <BookOpen class="size-4" />
                      <span>在线阅读</span>
                    </Button>
                  </div>
                </div>
              </div>
            </div>
          </div>

          <!-- 2. BODY CONTENT (White/Dark Theme Adaptable) -->
          <div class="p-5 sm:p-7 space-y-6 flex-1 bg-background text-foreground">
            <!-- Section A: Gallery Metadata Details -->
            <div class="space-y-2.5">
              <div class="text-sm font-bold text-foreground">
                画廊详情
              </div>
              <div class="grid grid-cols-2 sm:grid-cols-4 gap-2.5 text-xs">
                <div
                  v-if="selectedPub?.uploader"
                  class="p-2.5 rounded-lg border bg-muted/30 cursor-pointer hover:bg-muted/60 transition-colors"
                  :title="`搜索上传者: ${selectedPub.uploader}`"
                  @click="handleUploaderClick(selectedPub.uploader)"
                >
                  <div class="text-muted-foreground text-[11px]">上传者</div>
                  <div class="font-semibold text-foreground truncate mt-0.5">
                    {{ selectedPub.uploader }}
                  </div>
                </div>
                <div class="p-2.5 rounded-lg border bg-muted/30">
                  <div class="text-muted-foreground text-[11px]">发布时间</div>
                  <div class="font-semibold text-foreground truncate mt-0.5">
                    {{ selectedPub?.publishTime ? selectedPub.publishTime.slice(0, 10) : "未知" }}
                  </div>
                </div>
                <div class="p-2.5 rounded-lg border bg-muted/30">
                  <div class="text-muted-foreground text-[11px]">页数</div>
                  <div class="font-semibold text-foreground truncate mt-0.5">
                    {{ selectedPub?.pageCount ? `${selectedPub.pageCount} 页` : "未知" }}
                  </div>
                </div>
                <div class="p-2.5 rounded-lg border bg-muted/30">
                  <div class="text-muted-foreground text-[11px]">归档状态</div>
                  <div class="font-semibold text-foreground truncate mt-0.5">
                    {{ selectedPub?.archiveBadge?.label || "未归档" }}
                  </div>
                </div>
              </div>
            </div>

            <!-- Section B: Namespace Tag Groups (NO local scrollview, flows naturally) -->
            <div
              v-if="groupedTags.length > 0"
              :key="selectedPub?.gid + ':' + selectedPub?.token"
              class="space-y-2.5 pt-1"
            >
              <div class="text-sm font-bold text-foreground">
                分类标签
              </div>

              <!-- Tags Layout: left-aligned, no quote mark, no indent -->
              <TransitionGroup
                name="tag-group"
                tag="div"
                class="space-y-2"
              >
                <div
                  v-for="grp in groupedTags"
                  :key="grp.namespace"
                  class="flex items-start gap-2 sm:gap-2.5 py-0.5"
                >
                  <!-- Namespace label: flushed with left edge, text-left -->
                  <div
                    class="shrink-0 text-left text-xs font-semibold text-muted-foreground/90 pt-0.5 select-none font-mono min-w-12 sm:min-w-14"
                  >
                    {{ grp.namespace }}:
                  </div>

                  <!-- Tags container with TransitionGroup for silky-smooth tag addition/rearrangement -->
                  <TransitionGroup
                    name="tag-pill"
                    tag="div"
                    class="flex flex-wrap gap-1.5 flex-1 min-w-0"
                  >
                    <span
                      v-for="t in grp.tags"
                      :key="grp.namespace + ':' + t.rawName"
                      :style="t.tagStyle"
                      :class="[
                        'text-[11px] sm:text-xs font-normal px-2.5 py-0.5 rounded-md cursor-pointer select-none transition-all duration-150 inline-flex items-center gap-1 active:scale-95',
                        t.isWeak ? 'border border-dashed' : 'border border-solid',
                        t.hasCustomStyle
                          ? 'shadow-2xs hover:brightness-110 hover:shadow-xs'
                          : t.isWeak
                            ? 'border-border/70 text-foreground/80 bg-muted/20 hover:bg-primary hover:text-primary-foreground hover:border-solid hover:border-primary'
                            : 'border-border text-foreground/90 bg-muted/30 hover:bg-primary hover:text-primary-foreground hover:border-primary',
                      ]"
                      :title="`点击搜索标签: ${t.searchName}`"
                      @click="handleTagClick(t.searchName)"
                    >
                      {{ t.displayName }}
                    </span>
                  </TransitionGroup>
                </div>
              </TransitionGroup>
            </div>

            <!-- Section C: Thumbnails Preview (页面预览) -->
            <div
              v-if="selectedPub?.readingOrder?.length > 0"
              class="space-y-3 pt-3 border-t"
            >
              <div
                class="flex items-center justify-between cursor-pointer group/sec select-none"
                @click="detailSubView = 'thumbnails'"
              >
                <span class="text-sm font-bold text-foreground inline-flex items-center gap-1 group-hover/sec:text-primary transition-colors">
                  页面预览
                  <ChevronRight class="size-3.5 opacity-60 group-hover/sec:translate-x-0.5 transition-transform" />
                </span>
                <span class="text-[11px] text-muted-foreground group-hover/sec:text-foreground transition-colors">
                  共 {{ selectedPub.pageCount || selectedPub.readingOrder.length }} 页 · 查看全部
                </span>
              </div>

              <!-- Horizontal Thumbnails Track -->
              <div class="flex gap-3 overflow-x-auto pb-2 pt-1 no-scrollbar select-none">
                <div
                  v-for="(page, idx) in previewPages"
                  :key="page.href || idx"
                  class="group/thumb relative w-24 sm:w-28 shrink-0 cursor-pointer transition-transform duration-200 hover:-translate-y-1"
                  @click="handleRead(selectedPub, idx)"
                >
                  <div class="relative aspect-[1/1.414] w-full overflow-hidden rounded-lg border bg-muted shadow-sm group-hover/thumb:shadow-md group-hover/thumb:border-primary/50 transition-all">
                    <CoverImage :src="page.thumbnailHref || page.href" :alt="`第 ${idx + 1} 页`" />
                    <!-- Hover overlay with read icon -->
                    <div class="absolute inset-0 bg-black/0 group-hover/thumb:bg-black/25 transition-colors flex items-center justify-center">
                      <BookOpen class="size-5 text-white opacity-0 group-hover/thumb:opacity-100 transition-opacity drop-shadow" />
                    </div>
                  </div>
                  <p class="text-[10px] text-center text-muted-foreground mt-1 group-hover/thumb:text-foreground transition-colors">
                    第 {{ idx + 1 }} 页
                  </p>
                </div>

                <!-- Trailing "See all" card if more pages exist -->
                <div
                  v-if="selectedPub.readingOrder.length > previewPages.length"
                  class="group/more relative w-24 sm:w-28 shrink-0 cursor-pointer flex flex-col items-center justify-center aspect-[1/1.414] rounded-lg border border-dashed border-border/80 hover:border-primary/60 bg-muted/20 hover:bg-muted/40 transition-all text-center"
                  @click="detailSubView = 'thumbnails'"
                >
                  <Grid class="size-5 text-muted-foreground group-hover/more:text-primary transition-colors mb-1" />
                  <span class="text-[11px] font-medium text-muted-foreground group-hover/more:text-foreground">全部页面</span>
                  <span class="text-[9px] text-muted-foreground/70 mt-0.5">{{ selectedPub.readingOrder.length }} 页</span>
                </div>
              </div>
            </div>

            <!-- Section D: Reviews Preview (Apple Books style) -->
            <div
              v-if="selectedPub?.reviews?.length > 0"
              class="space-y-3 pt-3 border-t"
            >
              <div
                class="flex items-center justify-between cursor-pointer group/sec select-none"
                @click="detailSubView = 'reviews'"
              >
                <span class="text-sm font-bold text-foreground inline-flex items-center gap-1 group-hover/sec:text-primary transition-colors">
                  用户评价
                  <ChevronRight class="size-3.5 opacity-60 group-hover/sec:translate-x-0.5 transition-transform" />
                </span>
                <span class="text-[11px] text-muted-foreground group-hover/sec:text-foreground transition-colors">
                  共 {{ selectedPub.reviews.length }} 条 · 查看全部
                </span>
              </div>

              <!-- Horizontal Review Cards Track -->
              <div class="flex gap-3 overflow-x-auto pb-2 pt-1 no-scrollbar select-none">
                <div
                  v-for="rev in previewReviews"
                  :key="rev.id || rev.time + rev.username"
                  class="group/rev relative w-72 sm:w-80 shrink-0 p-3.5 sm:p-4 rounded-xl border bg-muted/40 dark:bg-muted/20 hover:bg-muted/60 dark:hover:bg-muted/30 cursor-pointer transition-all duration-200 flex flex-col justify-between space-y-3"
                  @click="detailSubView = 'reviews'"
                >
                  <p class="text-xs sm:text-[13px] text-foreground/90 line-clamp-3 leading-relaxed break-words font-normal">
                    {{ stripHtmlToSnippet(rev.content) }}
                  </p>
                  <div class="flex items-center justify-between text-[11px] text-muted-foreground pt-1 border-t border-border/30">
                    <span class="font-medium text-foreground/80 truncate max-w-[150px] group-hover/rev:text-primary transition-colors">
                      {{ rev.username }}
                    </span>
                    <span class="font-mono text-[10px]">{{ rev.time ? rev.time.slice(0, 10) : '' }}</span>
                  </div>
                </div>

                <!-- Trailing "See all" card if more reviews exist -->
                <div
                  v-if="selectedPub.reviews.length > previewReviews.length"
                  class="group/more relative w-32 sm:w-36 shrink-0 cursor-pointer flex flex-col items-center justify-center p-4 rounded-xl border border-dashed border-border/80 hover:border-primary/60 bg-muted/20 hover:bg-muted/40 transition-all text-center"
                  @click="detailSubView = 'reviews'"
                >
                  <MessageSquare class="size-5 text-muted-foreground group-hover/more:text-primary transition-colors mb-1.5" />
                  <span class="text-xs font-medium text-muted-foreground group-hover/more:text-foreground">全部评价</span>
                  <span class="text-[10px] text-muted-foreground/70 mt-0.5">{{ selectedPub.reviews.length }} 条</span>
                </div>
              </div>
            </div>

            <!-- Section E: More in this list / Series (当前列表的其他作品) -->
            <div v-if="relatedPublications.length > 0" class="space-y-3 pt-3 border-t">
              <div class="flex items-center justify-between">
                <span class="text-sm font-bold text-foreground inline-flex items-center gap-1">
                  当前列表的其他作品
                  <ChevronRight class="size-3.5 opacity-60" />
                </span>
                <span class="text-[11px] text-muted-foreground">共 {{ relatedPublications.length }} 本</span>
              </div>

              <!-- Horizontal Shelf Track -->
              <div class="flex gap-3 overflow-x-auto pb-2 pt-1 no-scrollbar select-none">
                <div
                  v-for="otherPub in relatedPublications"
                  :key="otherPub.gid + ':' + otherPub.token"
                  class="group/item relative w-24 sm:w-28 shrink-0 cursor-pointer transition-transform duration-200 hover:-translate-y-1"
                  @click="selectPublication(otherPub)"
                >
                  <div class="relative aspect-[1/1.414] w-full overflow-hidden rounded-lg border bg-muted shadow-sm group-hover/item:shadow-md transition-shadow">
                    <CoverImage :src="otherPub.coverUrl" :alt="otherPub.title" />
                  </div>
                  <p class="text-[11px] font-medium leading-tight line-clamp-1 mt-1.5 text-foreground group-hover/item:text-primary transition-colors">
                    {{ otherPub.title }}
                  </p>
                  <p v-if="otherPub.authors" class="text-[10px] text-muted-foreground line-clamp-1">
                    {{ otherPub.authors }}
                  </p>
                </div>
              </div>
            </div>
          </div>
        </div>

        <!-- OVERLAY SUBVIEW: Thumbnails or Reviews (Cover from right to left) -->
        <Transition name="slide-cover">
          <!-- ================= VIEW 2: THUMBNAILS FULL GRID ================= -->
          <div
            v-if="detailSubView === 'thumbnails'"
            key="thumbnails"
            class="absolute inset-0 z-30 bg-background flex flex-col overflow-y-auto no-scrollbar border-l border-border/40 shadow-[-8px_0_30px_rgba(0,0,0,0.18)] select-text"
          >
            <!-- Sticky Subview Header -->
            <div class="sticky top-0 z-20 bg-background/95 backdrop-blur-md border-b px-5 sm:px-7 py-3 flex items-center justify-between">
              <div class="flex items-center gap-2">
                <Button
                  variant="ghost"
                  size="sm"
                  class="gap-1 text-xs -ml-2 text-foreground/80 hover:text-foreground"
                  @click="detailSubView = 'main'"
                >
                  <ChevronLeft class="size-4" />
                  <span>返回详情</span>
                </Button>
                <div class="h-4 w-px bg-border" />
                <span class="text-sm font-bold text-foreground">
                  页面预览
                </span>
              </div>
              <div class="flex items-center gap-3">
                <span class="text-xs text-muted-foreground hidden sm:inline">
                  共 {{ selectedPub?.readingOrder?.length || 0 }} 页 · 点击跳转阅读
                </span>
                <!-- Quick Page Jump (if more than 48 pages) -->
                <div
                  v-if="(selectedPub?.readingOrder?.length || 0) > 48"
                  class="flex items-center gap-1.5 text-xs text-muted-foreground bg-muted/40 border rounded-lg px-2 py-0.5"
                >
                  <span class="text-[11px] select-none">跳至</span>
                  <input
                    v-model.number="pageJumpInput"
                    type="number"
                    min="1"
                    :max="selectedPub?.readingOrder?.length || 1"
                    class="w-12 h-6 text-xs text-center border rounded bg-background text-foreground px-1 outline-none font-mono focus:border-primary"
                    placeholder="页码"
                    @keydown.enter.prevent="handleQuickJump"
                  />
                  <Button
                    variant="ghost"
                    size="sm"
                    class="h-6 text-[11px] px-1.5 text-muted-foreground hover:text-foreground"
                    @click="handleQuickJump"
                  >
                    跳转
                  </Button>
                </div>
                <DialogClose as-child>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    class="size-8 rounded-full text-muted-foreground hover:text-foreground"
                    title="关闭 (Esc)"
                  >
                    <X class="size-4" />
                  </Button>
                </DialogClose>
              </div>
            </div>

            <!-- Mobile Hint -->
            <div class="sm:hidden px-5 pt-3 text-xs text-muted-foreground">
              共 {{ selectedPub?.readingOrder?.length || 0 }} 页 · 点击任意缩略图跳转阅读
            </div>

            <!-- Thumbnails Grid -->
            <div class="p-5 sm:p-7 flex-1">
              <div
                v-if="!selectedPub?.readingOrder || selectedPub.readingOrder.length === 0"
                class="py-16 text-center text-muted-foreground text-sm"
              >
                暂无页面缩略图
              </div>
              <div
                v-else
                class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 gap-3.5 sm:gap-4.5"
              >
                <div
                  v-for="(page, idx) in displayedThumbnails"
                  :id="`thumb-page-${idx + 1}`"
                  :key="page.href || idx"
                  class="group/gridpage cursor-pointer flex flex-col items-center select-none"
                  @click="handleRead(selectedPub, idx)"
                >
                  <div class="relative aspect-[1/1.414] w-full overflow-hidden rounded-xl border bg-muted shadow-xs group-hover/gridpage:shadow-md group-hover/gridpage:border-primary transition-all active:scale-95">
                    <CoverImage :src="page.thumbnailHref || page.href" :alt="`第 ${idx + 1} 页`" />
                    <!-- Hover overlay with read icon -->
                    <div class="absolute inset-0 bg-black/35 opacity-0 group-hover/gridpage:opacity-100 flex items-center justify-center transition-opacity">
                      <BookOpen class="size-7 text-white drop-shadow-md" />
                    </div>
                  </div>
                  <span class="text-xs text-muted-foreground mt-1.5 group-hover/gridpage:text-foreground group-hover/gridpage:font-medium transition-colors">
                    第 {{ idx + 1 }} 页
                  </span>
                </div>

                <!-- Progressive loading sentinel for thumbnails -->
                <div
                  v-if="thumbnailRenderLimit < (selectedPub?.readingOrder?.length || 0)"
                  ref="thumbnailSentinelRef"
                  class="col-span-full py-6 text-center text-xs text-muted-foreground flex items-center justify-center gap-2"
                >
                  <RefreshCw class="size-3.5 animate-spin opacity-60" />
                  <span>正在载入后续页面... (已显示 {{ displayedThumbnails.length }} / {{ selectedPub?.readingOrder?.length }} 页)</span>
                </div>
              </div>
            </div>
          </div>

          <!-- ================= VIEW 3: FULL REVIEWS LIST ================= -->
          <div
            v-else-if="detailSubView === 'reviews'"
            key="reviews"
            class="absolute inset-0 z-30 bg-background flex flex-col overflow-y-auto no-scrollbar border-l border-border/40 shadow-[-8px_0_30px_rgba(0,0,0,0.18)] select-text"
          >
            <!-- Sticky Subview Header -->
            <div class="sticky top-0 z-20 bg-background/95 backdrop-blur-md border-b px-5 sm:px-7 py-3 flex items-center justify-between">
              <div class="flex items-center gap-2">
                <Button
                  variant="ghost"
                  size="sm"
                  class="gap-1 text-xs -ml-2 text-foreground/80 hover:text-foreground"
                  @click="detailSubView = 'main'"
                >
                  <ChevronLeft class="size-4" />
                  <span>返回详情</span>
                </Button>
                <div class="h-4 w-px bg-border" />
                <span class="text-sm font-bold text-foreground">
                  用户评价
                </span>
              </div>
              <div class="flex items-center gap-3">
                <span class="text-xs text-muted-foreground hidden sm:inline">
                  共 {{ selectedPub?.reviews?.length || 0 }} 条评价
                </span>
                <DialogClose as-child>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    class="size-8 rounded-full text-muted-foreground hover:text-foreground"
                    title="关闭 (Esc)"
                  >
                    <X class="size-4" />
                  </Button>
                </DialogClose>
              </div>
            </div>

            <!-- Reviews List -->
            <div class="p-5 sm:p-7 space-y-4 flex-1">
              <div
                v-if="!selectedPub?.reviews || selectedPub.reviews.length === 0"
                class="py-16 text-center text-muted-foreground text-sm space-y-2"
              >
                <MessageSquare class="size-8 mx-auto opacity-40" />
                <p>暂无用户评价</p>
              </div>

              <div
                v-for="rev in displayedReviews"
                :key="rev.id || rev.time + rev.username"
                class="p-4 sm:p-5 rounded-2xl border bg-card/60 space-y-3 shadow-xs hover:border-border transition-colors select-text"
              >
                <!-- Reviewer & Time Info -->
                <div class="flex items-center justify-between">
                  <div class="flex items-center gap-2">
                    <div class="size-7 rounded-full bg-primary/10 text-primary flex items-center justify-center font-bold text-xs select-none">
                      {{ (rev.username || "?").charAt(0).toUpperCase() }}
                    </div>
                    <div>
                      <span class="text-xs sm:text-sm font-semibold text-foreground">
                        {{ rev.username }}
                      </span>
                    </div>
                  </div>
                  <div class="text-right text-[11px] text-muted-foreground font-mono">
                    <div>{{ rev.time }}</div>
                    <div v-if="rev.lastEditTime" class="text-[10px] text-muted-foreground/70">
                      编辑于 {{ rev.lastEditTime }}
                    </div>
                  </div>
                </div>

                <!-- Review Content HTML with lazy images & safe links -->
                <div
                  class="text-xs sm:text-[13px] text-foreground/90 break-words leading-relaxed review-html-content"
                  v-html="formatReviewHtml(rev.content)"
                />
              </div>

              <!-- Progressive loading sentinel for reviews -->
              <div
                v-if="reviewRenderLimit < (selectedPub?.reviews?.length || 0)"
                ref="reviewSentinelRef"
                class="py-4 text-center text-xs text-muted-foreground flex items-center justify-center gap-2 cursor-pointer hover:text-foreground"
                @click="reviewRenderLimit += 20"
              >
                <RefreshCw class="size-3.5 animate-spin opacity-60" />
                <span>正在加载更多评价... (已显示 {{ displayedReviews.length }} / {{ selectedPub?.reviews?.length }} 条)</span>
              </div>
            </div>
          </div>
        </Transition>
      </DialogContent>
    </Dialog>

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

.review-html-content :deep(img) {
  max-width: 100%;
  height: auto;
  border-radius: 0.375rem;
  display: inline-block;
  margin: 0.35rem 0;
}
.review-html-content :deep(a) {
  color: var(--color-primary, #3b82f6);
  text-decoration: underline;
  text-underline-offset: 2px;
}
.review-html-content :deep(blockquote) {
  border-left: 2px solid var(--color-border, #e5e7eb);
  padding-left: 0.75rem;
  margin: 0.5rem 0;
  color: var(--color-muted-foreground, #6b7280);
  font-style: italic;
}
</style>

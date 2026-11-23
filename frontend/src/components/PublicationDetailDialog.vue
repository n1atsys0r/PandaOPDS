<script setup>
import {
  ref,
  computed,
  watch,
  nextTick,
  onMounted,
  onUnmounted,
  reactive,
} from "vue";
import {
  Dialog,
  DialogContent,
  DialogTitle,
  DialogClose,
} from "@/components/ui/dialog";
import { AlertDialog,AlertDialogAction,AlertDialogCancel,AlertDialogContent,AlertDialogDescription,AlertDialogFooter,AlertDialogHeader,AlertDialogTitle } from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from "@/components/ui/dropdown-menu";
import CoverImage from "@/components/CoverImage.vue";
import ReviewHtml from "@/components/ReviewHtml.vue";
import {
  X,
  ChevronRight,
  ChevronLeft,
  Star,
  BookOpen,
  ChevronDown,
  EyeOff,
  Download,
  RefreshCw,
  Heart,
  ExternalLink,
  Layers,
  MessageSquare,
  MessageSquareText,
  ArrowLeft,
  Send,
  Bookmark,
  BookmarkX,
  Check,
  CircleAlert,
  Grid,
  HatGlasses,
  Loader2,
  Package2,
} from "@lucide/vue";
import { useApp } from "@/state.js";
import { archiveActionFor } from "@/archive-flow.js";
import {
  captureGalleryMetadata,
  ensureGalleryMetadata,
  restoreGalleryMetadata,
  formatLanguage,
} from "@/gallery-metadata.js";
import { t } from "@/i18n.js";
import { EH_CATEGORIES, getCategoryColor } from "@/subscription-view.js";
import {
  getFavcatSolidColor,
  getFavcatBadgeStyle,
  resolveFavcatId,
} from "@/favcat-colors.js";

const props = defineProps({
  open: { type: Boolean, default: false },
  publication: { type: Object, default: null },
  list: { type: Array, default: () => [] },
});

const emit = defineEmits([
  "update:open",
  "update:publication",
  "read-gallery",
  "archive-gallery",
  "archive-unlink",
  "archive-delete",
  "archive-manage",
  "favorite-gallery",
  "tag-click",
  "category-click",
  "author-click",
  "uploader-click",
  "favcat-click",
]);

const s = useApp();

const isOpen = computed({
  get: () => props.open,
  set: (val) => emit("update:open", val),
});

function onOpenChange(val) {
  isOpen.value = val;
}

function normalizePublicationItem(p) {
  if (!p) return null;
  const meta = p.metadata || {};
  const gid = Number(p.gid || meta["x:gid"] || 0);
  const token = String(p.token || meta["x:token"] || "");

  let coverUrl = p.coverUrl || p.cover_url || "";
  if (!coverUrl) {
    if (p.images && p.images.length > 0) {
      coverUrl = p.images[0].href;
    } else if (gid && token) {
      coverUrl = `/image/${gid}/${token}/thumb`;
    }
  }

  let detailHref = p.detailHref || "";
  let upstreamUrl = p.upstreamUrl || "";
  for (const l of p.links || p.raw?.links || []) {
    if (l.rel === "self" && l.href) {
      const type = String(l.type || "").toLowerCase();
      if (type.includes("divina") || type.includes("opds")) {
        detailHref = l.href;
      }
    }
    if (l.rel === "alternate" && l.type === "text/html") {
      upstreamUrl = l.href;
    }
  }
  if (!detailHref && gid && token) {
    detailHref = `/opds/v2.0/gallery/${gid}/${token}/manifest`;
  }
  if (!upstreamUrl && gid && token) {
    upstreamUrl = `https://e-hentai.org/g/${gid}/${token}/`;
  }

  const title = p.title || p.title_jpn || p.titleJpn || meta.title || "";
  const category =
    p.category ||
    meta["x:category"] ||
    (Array.isArray(meta.subject) && meta.subject[0]?.name) ||
    "Misc";
  const pageCount = Number(
    p.pageCount ||
      p.page_count ||
      meta.numberOfPages ||
      meta["x:page_count"] ||
      0,
  );
  const rating = Number(
    p.rating ||
      meta["x:rating"] ||
      (typeof meta.rating === "object" ? meta.rating?.value : meta.rating) ||
      0,
  );
  const publishTime = p.publishTime || p.publish_time || meta.published || "";
  const language = formatLanguage(p.language || meta.language || "", p);
  const uploader = p.uploader || meta["x:uploader"] || "";
  const fileSize = p.fileSize || p.file_size || meta["x:fileSize"] || "";
  const readingOrder = Array.isArray(p.readingOrder) ? p.readingOrder : [];
  const reviews = Array.isArray(p.reviews)
    ? p.reviews
    : Array.isArray(meta["x:reviews"])
      ? meta["x:reviews"]
      : [];
  const subjects = Array.isArray(p.subjects)
    ? p.subjects
    : Array.isArray(meta.subject)
      ? meta.subject
      : [];

  return {
    ...p,
    gid,
    token,
    title,
    category,
    coverUrl,
    detailHref,
    upstreamUrl,
    pageCount,
    rating,
    language,
    uploader,
    publishTime,
    fileSize,
    readingOrder,
    reviews,
    subjects,
    detailLoaded: Boolean(p.detailLoaded || readingOrder.length > 0),
    galleryDetailsLoaded: Boolean(p.galleryDetailsLoaded),
  };
}

const pubData = ref(null);
const detailSubView = ref("main"); // "main" | "reviews" | "thumbnails"
const modalScrollRef = ref(null);
const thumbnailSentinelRef = ref(null);
const reviewSentinelRef = ref(null);
const thumbnailRenderLimit = ref(64);
const reviewRenderLimit = ref(20);
const pageJumpInput = ref("");
let sentinelObserver = null;

const selectedPub = computed({
  get: () => pubData.value || normalizePublicationItem(props.publication),
  set: (val) => {
    pubData.value = val;
    emit("update:publication", val);
  },
});

const currentRating = computed(() => {
  return Number(selectedPub.value?.rating || 0);
});

const currentArchiveBadge = computed(() => {
  const pub = selectedPub.value;
  if (!pub) return null;
  const item=currentArchiveItem.value;
  const localStatus=item?.archive_status || item?.status;
  if (localStatus) return {label:localStatus==="ready"?t("explore.status_archived"):localStatus==="unavailable"?t("archive.unavailable"):localStatus,isReady:localStatus==="ready",isUnavailable:localStatus==="unavailable",isFailed:localStatus==="failed",isDownloading:["pending","submitting","staging","downloading","zipping"].includes(localStatus)};
  return null;
});

const currentArchiveItem = computed(()=>{ const pub=selectedPub.value;if(!pub?.gid||!pub?.token)return null;return (s.archives.value||[]).find(item=>Number(item.gid)===Number(pub.gid)&&String(item.token||"").toLowerCase()===String(pub.token).toLowerCase())||null; });
const archiveAction = ref(null);

const isArchiveQuoteBusy = computed(() => {
  return s.busy.value;
});

const favoriteOverride = computed(() => {
  const pub = selectedPub.value;
  if (!pub?.gid || !pub?.token) return null;
  const key = `${pub.gid}:${pub.token}`;
  return s.favoriteOverrides?.value?.[key] ?? null;
});

const currentFavcatId = computed(() => {
  return resolveFavcatId(selectedPub.value, favoriteOverride.value);
});

const isCurrentFavorited = computed(() => {
  if (favoriteOverride.value != null) {
    return favoriteOverride.value.isFavorited;
  }
  return (
    currentFavcatId.value != null ||
    Boolean(selectedPub.value?.favorited || selectedPub.value?.is_favorited)
  );
});

const favcatList = computed(() => {
  const list = [];
  const siteCategories = s.favcats.value || [];
  for (let i = 0; i < 10; i++) {
    const custom = siteCategories.find((c) => Number(c.id) === i);
    list.push({
      id: i,
      name: custom?.name || `${t("common.favorite")} ${i}`,
      color: getFavcatSolidColor(i),
      href: `/opds/v2.0/gallery?favcat=${i}&query=favorites`,
    });
  }
  return list;
});

const currentFavcatInfo = computed(() => {
  const id = currentFavcatId.value;
  if (id == null) return null;
  const item = favcatList.value.find((c) => c.id === id);
  if (!item) return null;
  const override = favoriteOverride.value;
  const belongsName =
    selectedPub.value?.belongsTo?.collection?.name ||
    selectedPub.value?.metadata?.belongsTo?.collection?.name;
  const name =
    override?.favcatName ||
    (belongsName &&
    !belongsName.startsWith("Favorites ") &&
    !belongsName.startsWith("收藏夹 ")
      ? belongsName
      : null) ||
    item.name;
  return {
    ...item,
    name,
  };
});

const currentFavcatStyle = computed(() => {
  return getFavcatBadgeStyle(currentFavcatId.value);
});

function handleFavcatClick() {
  if (currentFavcatInfo.value) {
    emit("favcat-click", currentFavcatInfo.value);
  }
}

const currentFileSize = computed(() => {
  const pub = selectedPub.value;
  if (!pub) return "";
  const label = pub.fileSize || pub.file_size;
  if (label) return label;
  const bytes = Number(pub.sizeBytes || 0);
  if (!bytes) return "";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const unit = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  return `${(bytes / 1024 ** unit).toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`;
});

function formatDisplayDate(dateStr) {
  if (!dateStr) return "";
  try {
    const d = new Date(dateStr);
    if (isNaN(d.getTime())) return dateStr;
    return d.toISOString().slice(0, 10);
  } catch {
    return dateStr;
  }
}

const currentProgress = computed(() => {
  const pub = selectedPub.value;
  if (!pub?.gid || !pub?.token) return null;
  const pageCount = Number(
    pub.pageCount || pub.page_count || pub.readingOrder?.length || 0,
  );
  if (pageCount <= 0) return null;
  return s.getGalleryProgress(pub.gid, pub.token, pageCount);
});

const primaryReadButtonLabel = computed(() => {
  if (!currentProgress.value) return t("documents.read");
  if (currentProgress.value.isCompleted) return t("explore.read_again");
  if (currentProgress.value.pageIndex > 0) {
    return t("explore.read_resume", {
      page: currentProgress.value.pageIndex + 1,
    });
  }
  return t("documents.read");
});

function handlePrimaryRead() {
  if (!selectedPub.value) return;
  if (currentProgress.value?.isCompleted) {
    handleRead(selectedPub.value, 0);
  } else if (currentProgress.value && currentProgress.value.pageIndex > 0) {
    handleRead(selectedPub.value, currentProgress.value.pageIndex);
  } else {
    handleRead(selectedPub.value);
  }
}

function handleIncognitoRead() {
  if (!selectedPub.value) return;
  const startPage =
    currentProgress.value &&
    !currentProgress.value.isCompleted &&
    currentProgress.value.pageIndex > 0
      ? currentProgress.value.pageIndex
      : 0;
  handleRead(selectedPub.value, startPage, { incognito: true });
}

function handleArchive(pub) {
  const item=currentArchiveItem.value;
  if (archiveActionFor(item?.archive_status || item?.status)!=="quote") return;
  emit("archive-gallery", pub);
}
function requestArchiveAction(action,item){ archiveAction.value={action,item}; }
function confirmArchiveAction(){ const pending=archiveAction.value;archiveAction.value=null;if(!pending)return;emit(pending.action==="unlink"?"archive-unlink":"archive-delete",pending.item); }

function handleSelectFavcat(catId) {
  if (!selectedPub.value) return;
  const isFavorited = isCurrentFavorited.value;
  const currentId = currentFavcatId.value;
  if (isFavorited && currentId === catId) return;
  emit("favorite-gallery", {
    pub: selectedPub.value,
    action: isFavorited ? "move" : "add",
    favcat: catId,
  });
}

function handleRemoveFavorite() {
  if (!selectedPub.value) return;
  emit("favorite-gallery", {
    pub: selectedPub.value,
    action: "remove",
  });
}

function handleCategoryClick(cat) {
  emit("category-click", cat);
}

function handleAuthorClick(author) {
  emit("author-click", author);
}

function handleUploaderClick(uploader) {
  emit("uploader-click", uploader);
}

function handleTagClick(tag) {
  emit("tag-click", tag);
}

const isPreparingReader = ref(false);
async function handleRead(pub, startPage = null, options = {}) {
  if (!pub?.gid || !pub?.token) return;
  ensureSupplementalGalleryMetadata(pub);
  const pageCount = Number(pub.pageCount || pub.page_count || 0);

  if (pub.detailLoaded && pageCount > 0) {
    isOpen.value = false;
    const payload = {
      gid: pub.gid,
      token: pub.token,
      title: pub.title,
      page_count: pageCount,
      reading_order: pub.readingOrder || [],
      incognito: Boolean(options.incognito),
    };
    if (typeof startPage === "number" && startPage >= 0) {
      payload.start_page = startPage;
      payload.initial_page_index = startPage;
    }
    emit("read-gallery", payload);
    return;
  }

  isPreparingReader.value = true;
  isOpen.value = false;
  const payload = reactive({
    gid: pub.gid,
    token: pub.token,
    title: pub.title,
    page_count: pageCount,
    reading_order: pub.readingOrder || [],
    incognito: Boolean(options.incognito),
    loading: true,
  });
  if (typeof startPage === "number" && startPage >= 0) {
    payload.start_page = startPage;
    payload.initial_page_index = startPage;
  }
  emit("read-gallery", payload);

  try {
    const detailedPub = await ensurePublicationDetails(pub);
    const updatedCount = Number(
      detailedPub?.pageCount || detailedPub?.page_count || 0,
    );
    if (detailedPub && updatedCount > 0) {
      payload.title = detailedPub.title || payload.title;
      payload.page_count = updatedCount;
      payload.reading_order = detailedPub.readingOrder || [];
      payload.loading = false;
    } else {
      payload.error = true;
      payload.loading = false;
    }
  } catch {
    payload.error = true;
    payload.loading = false;
  } finally {
    isPreparingReader.value = false;
  }
}

function scrollModalToTop() {
  if (modalScrollRef.value) {
    modalScrollRef.value.scrollTop = 0;
  }
}

function openLinkedGallery({ gid, token }) {
  const match = props.list.find(
    (item) =>
      Number(item?.gid) === gid &&
      String(item?.token || "").toLowerCase() === token.toLowerCase(),
  );
  pubData.value = normalizePublicationItem(match || { gid, token });
  emit("update:publication", pubData.value);
  detailSubView.value = "main";
  scrollModalToTop();
  ensureSupplementalGalleryMetadata(pubData.value);
  ensurePublicationDetails(pubData.value);
}

// Adjacent navigation
const currentIndex = computed(() => {
  if (!selectedPub.value || !props.list.length) return -1;
  return props.list.findIndex(
    (p) =>
      p.gid === selectedPub.value.gid && p.token === selectedPub.value.token,
  );
});

const hasPrev = computed(() => currentIndex.value > 0);
const hasNext = computed(
  () => currentIndex.value >= 0 && currentIndex.value < props.list.length - 1,
);

function prevGallery() {
  if (hasPrev.value) {
    const target = props.list[currentIndex.value - 1];
    pubData.value = normalizePublicationItem(target);
    emit("update:publication", pubData.value);
    ensureSupplementalGalleryMetadata(pubData.value);
    ensurePublicationDetails(pubData.value);
    detailSubView.value = "main";
    scrollModalToTop();
  }
}

function nextGallery() {
  if (hasNext.value) {
    const target = props.list[currentIndex.value + 1];
    pubData.value = normalizePublicationItem(target);
    emit("update:publication", pubData.value);
    ensureSupplementalGalleryMetadata(pubData.value);
    ensurePublicationDetails(pubData.value);
    detailSubView.value = "main";
    scrollModalToTop();
  }
}

function handleKeyDown(e) {
  if (!isOpen.value) return;
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

// Canonical namespace weights
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
  const lower = String(ns || "")
    .trim()
    .toLowerCase();
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
    if (
      rawStyle.background ||
      rawStyle.backgroundColor ||
      rawStyle["background-color"]
    ) {
      style.background =
        rawStyle.background ||
        rawStyle.backgroundColor ||
        rawStyle["background-color"];
    }
  } else if (typeof rawStyle === "string") {
    for (const part of rawStyle.split(";")) {
      const [k, v] = part.split(":").map((s) => s.trim());
      if (!k || !v) continue;
      const cleanV = v.replace(/!important/g, "").trim();
      const lk = k.toLowerCase();
      if (lk === "color") style.color = cleanV;
      else if (lk === "border-color" || lk === "bordercolor")
        style.borderColor = cleanV;
      else if (lk === "background" || lk === "background-color")
        style.background = cleanV;
    }
  }
  return Object.keys(style).length > 0 ? style : null;
}

const groupedTags = computed(() => {
  const pub = selectedPub.value;
  if (!pub) return [];

  let rawItems = [];
  if (Array.isArray(pub.subjects) && pub.subjects.length) {
    rawItems = pub.subjects;
  } else if (pub.tags) {
    if (Array.isArray(pub.tags)) {
      rawItems = pub.tags;
    } else if (typeof pub.tags === "object") {
      for (const [ns, tagList] of Object.entries(pub.tags)) {
        if (Array.isArray(tagList)) {
          for (const item of tagList) {
            rawItems.push(typeof item === "string" ? `${ns}:${item}` : item);
          }
        }
      }
    }
  }

  if (!rawItems.length) return [];

  const map = new Map();
  let firstSeenCounter = 0;

  for (const item of rawItems) {
    const rawName = typeof item === "string" ? item : item?.name || "";
    if (!rawName) continue;

    let ns = "other";
    let key = rawName;
    const colonIdx = rawName.indexOf(":");
    if (colonIdx > 0) {
      ns = rawName.slice(0, colonIdx).trim().toLowerCase();
      key = rawName.slice(colonIdx + 1).trim();
    }

    if (ns === "category" || ns === "分类") {
      continue;
    }

    const rawStyle =
      typeof item === "object" ? item["x:style"] || item.style || null : null;
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

  const result = Array.from(map.values()).sort((a, b) => {
    const wa = getNamespaceWeight(a.namespace);
    const wb = getNamespaceWeight(b.namespace);
    if (wa !== wb) return wa - wb;
    return a.firstSeenIndex - b.firstSeenIndex;
  });

  for (const grp of result) {
    grp.tags.sort((a, b) => {
      if (a.hasCustomStyle && !b.hasCustomStyle) return -1;
      if (!a.hasCustomStyle && b.hasCustomStyle) return 1;
      return 0;
    });
  }

  return result;
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
  const text = (tmp.textContent || tmp.innerText || "")
    .trim()
    .replace(/\s+/g, " ");
  if (text.length <= maxLength) return text;
  return text.slice(0, maxLength) + "…";
}

const displayedThumbnails = computed(() => {
  return (selectedPub.value?.readingOrder || []).slice(
    0,
    thumbnailRenderLimit.value,
  );
});

const displayedReviews = computed(() => {
  return (selectedPub.value?.reviews || []).slice(0, reviewRenderLimit.value);
});

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
  const target = Math.max(1, Math.min(Number(pageJumpInput.value) || 1, total));
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

function ensureSupplementalGalleryMetadata(pub) {
  if (!pub?.gid || !pub?.token || pub.galleryDetailsLoaded) return Promise.resolve(pub);
  return ensureGalleryMetadata(s.api, pub).then((updated) => {
    if (pubData.value && pubData.value.gid === pub.gid && pubData.value.token === pub.token) {
      restoreGalleryMetadata(pubData.value, captureGalleryMetadata(updated));
    }
    return updated;
  }).catch(() => pub);
}

const detailRequests = new Map();
function ensurePublicationDetails(pub) {
  if (!pub?.gid || !pub?.token) return Promise.resolve(null);
  const detailHref =
    pub.detailHref ||
    `/opds/v2.0/gallery/${pub.gid}/${pub.token}/manifest`;
  if (!detailHref) return Promise.resolve(pub);
  if (pub.detailLoaded) return Promise.resolve(pub);

  let task = detailRequests.get(detailHref);
  if (!task) {
    task = s
      .api(detailHref, {
        headers: {
          Accept: "application/divina+json, application/opds+json",
          "Cache-Control": "no-cache",
        },
      })
      .then(({ data }) => data)
      .finally(() => detailRequests.delete(detailHref));
    detailRequests.set(detailHref, task);
  }

  return task
    .then((data) => {
      if (pub.detailLoaded) return pub;
      const metadata = data?.metadata || {};
      const subjects = [...(pub.subjects || [])];
      for (const subject of Array.isArray(metadata.subject)
        ? metadata.subject
        : []) {
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

      const rawPages = Array.isArray(data.readingOrder)
        ? data.readingOrder
        : [];
      const pages = rawPages.map((page, index) => ({
        pageNumber: index + 1,
        href: page?.href || "",
        thumbnailHref:
          page?.properties?.["x:thumbnail"]?.href || page?.href || "",
      }));
      const validPages = pages.length > 0 && pages.every((page) => page.href);
      const reviews = Array.isArray(metadata["x:reviews"])
        ? metadata["x:reviews"]
        : pub.reviews || [];
      const supplemental = captureGalleryMetadata(pub);

      let upstreamUrl = pub.upstreamUrl;
      for (const link of data.links || []) {
        if (link.rel === "alternate" && link.type === "text/html") {
          upstreamUrl = link.href;
        }
      }
      if (!upstreamUrl && pub.gid && pub.token) {
        upstreamUrl = `https://e-hentai.org/g/${pub.gid}/${pub.token}/`;
      }

      const ratingVal =
        typeof metadata.rating === "object"
          ? Number(metadata.rating?.value || 0)
          : Number(metadata["x:rating"] || metadata.rating || pub.rating || 0);
      const pageCountVal = validPages
        ? pages.length
        : Number(
            metadata.numberOfPages ||
              metadata["x:page_count"] ||
              pub.pageCount ||
              0,
          );

      let favcat = pub.favcat;
      if (metadata.belongsTo?.collection) {
        pub.belongsTo = metadata.belongsTo;
        const match = (
          metadata.belongsTo.collection.links?.[0]?.href || ""
        ).match(/favcat=(\d+)/);
        if (match) {
          favcat = Number(match[1]);
        }
      }

      Object.assign(pub, {
        subjects,
        reviews,
        detailLoaded: true,
        readingOrder: validPages ? pages : pub.readingOrder || [],
        pageCount: pageCountVal,
        rating: supplemental ? pub.rating : ratingVal > 0 ? ratingVal : pub.rating || 0,
        uploader: supplemental ? pub.uploader : metadata["x:uploader"] || pub.uploader || "",
        publishTime: supplemental ? pub.publishTime : metadata.published || pub.publishTime || pub.publish_time || "",
        fileSize: supplemental ? pub.fileSize : metadata["x:fileSize"] || pub.fileSize || pub.file_size || "",
        upstreamUrl,
        favcat: favcat != null ? favcat : pub.favcat,
      });
      if (supplemental) restoreGalleryMetadata(pub, supplemental);

      if (
        pubData.value &&
        pubData.value.gid === pub.gid &&
        pubData.value.token === pub.token
      ) {
        Object.assign(pubData.value, pub);
      }
      return pub;
    })
    .catch(() => pub);
}

watch(
  [() => props.publication, isOpen],
  ([raw, open]) => {
    if (raw && open) {
      pubData.value = normalizePublicationItem(raw);
      ensureSupplementalGalleryMetadata(pubData.value);
      ensurePublicationDetails(pubData.value);
      s.loadArchives();
      if (pubData.value.gid && pubData.value.token) {
        s.getProgression(pubData.value.gid, pubData.value.token);
      }
    } else if (!open) {
      detailSubView.value = "main";
    }
  },
  { immediate: true, deep: true },
);

onMounted(() => {
  window.addEventListener("keydown", handleKeyDown);
});

onUnmounted(() => {
  window.removeEventListener("keydown", handleKeyDown);
  sentinelObserver?.disconnect();
  sentinelObserver = null;
});
</script>

<template>
  <Dialog :open="isOpen" @update:open="onOpenChange">
    <DialogContent
      :show-close-button="false"
      class="w-full max-w-[calc(100%-1.5rem)] sm:max-w-3xl md:max-w-4xl h-[88vh] max-h-[90vh] p-0 rounded-2xl sm:rounded-3xl border border-border/50 shadow-2xl overflow-hidden bg-background flex flex-col"
      @escape-key-down="
        (e) => {
          if (detailSubView !== 'main') {
            e.preventDefault();
            detailSubView = 'main';
          }
        }
      "
      @open-auto-focus="(e) => e.preventDefault()"
    >
      <!-- BASE VIEW: Main Gallery Details (always mounted, preserving scroll position) -->
      <div
        ref="modalScrollRef"
        class="overflow-y-auto flex-1 flex flex-col select-text overlay-scrollbar"
      >
        <!-- 1. HERO HEADER (Apple Books Dark Slate Banner) -->
        <div
          class="relative bg-gradient-to-b from-[#3a3b40] via-[#2c2e35] to-[#202227] text-white py-8 sm:py-10 px-5 sm:px-7 shrink-0 overflow-hidden"
        >
          <!-- Ambient Category Glow -->
          <div
            class="absolute -top-24 -left-24 size-96 rounded-full blur-3xl opacity-25 pointer-events-none transition-colors duration-500"
            :style="{
              backgroundColor:
                getCategoryColor(selectedPub?.category) || '#6366f1',
            }"
          />

          <!-- Close Button: Equal top and right margins -->
          <DialogClose as-child>
            <Button
              variant="ghost"
              size="icon-sm"
              class="absolute top-5 right-5 sm:top-6 sm:right-6 z-20 size-8 rounded-full bg-black/40 hover:bg-black/60 text-white/90 hover:text-white border border-white/10 transition-colors focus-visible:ring-0 focus-visible:outline-none focus:outline-none active:scale-95"
              :title="t('common.close_esc')"
            >
              <X class="size-4" />
            </Button>
          </DialogClose>

          <!-- Hero Main Info: Cover + Typography + Actions -->
          <div
            class="flex flex-col sm:flex-row gap-5 sm:gap-7 items-start relative z-10"
          >
            <!-- Left: Prominent Book Cover -->
            <div
              v-if="selectedPub"
              class="relative aspect-[1/1.414] w-36 sm:w-44 md:w-48 shrink-0 mx-auto sm:mx-0 overflow-hidden rounded-xl border border-white/20 shadow-2xl shadow-black/70 bg-black/40 group"
            >
              <CoverImage
                :src="selectedPub.coverUrl"
                :alt="selectedPub.title"
              />
            </div>

            <!-- Right: Metadata & Action Buttons -->
            <div
              class="flex-1 min-w-0 flex flex-col justify-between self-stretch space-y-4 sm:space-y-5"
            >
              <!-- Upper: Category, Title, Rating & Favcat -->
              <div
                class="space-y-2 sm:space-y-2.5 text-center sm:text-left sm:pr-10"
              >
                <!-- Category Link with Chevron -->
                <div class="flex items-center justify-center sm:justify-start">
                  <span
                    v-if="selectedPub?.category"
                    class="inline-flex items-center gap-0.5 text-xs font-semibold tracking-wide text-white/80 hover:text-white transition-colors cursor-pointer"
                    @click="handleCategoryClick(selectedPub.category)"
                    :title="
                      t('explore.search_category', {
                        cat: selectedPub.category,
                      })
                    "
                  >
                    <span>{{ selectedPub.category }}</span>
                    <ChevronRight class="size-3 opacity-60" />
                  </span>
                </div>

                <!-- Title -->
                <DialogTitle
                  class="text-lg sm:text-2xl font-bold text-white leading-snug line-clamp-2 break-words max-w-[360px] sm:max-w-none mx-auto sm:mx-0 text-balance"
                >
                  {{ selectedPub?.title }}
                </DialogTitle>

                <!-- Rating & Favcat Row -->
                <div
                  class="flex items-center justify-center sm:justify-start gap-2 text-xs pt-0.5 min-h-[1.5rem]"
                >
                  <div
                    class="flex items-center gap-1 text-amber-300 font-semibold"
                  >
                    <Star class="size-3.5 fill-amber-300 text-amber-300" />
                    <span class="transition-opacity duration-300">{{
                      currentRating.toFixed(2)
                    }}</span>
                  </div>

                  <Transition
                    enter-active-class="transition-all duration-300 ease-out"
                    enter-from-class="opacity-0 scale-95"
                    enter-to-class="opacity-100 scale-100"
                    leave-active-class="transition-all duration-200 ease-in"
                    leave-from-class="opacity-100 scale-100"
                    leave-to-class="opacity-0 scale-95"
                  >
                    <div
                      v-if="currentFavcatInfo?.name"
                      class="inline-flex items-center gap-2"
                    >
                      <span class="opacity-30 text-white/70 select-none"
                        >·</span
                      >
                      <span
                        class="inline-flex items-center text-[10px] sm:text-[11px] font-medium px-2 py-0.5 rounded-full border shadow-2xs transition-all select-none"
                        :style="{
                          backgroundColor: currentFavcatStyle.bg,
                          borderColor: currentFavcatStyle.border,
                          color: currentFavcatStyle.text,
                        }"
                        :class="
                          currentFavcatInfo.href
                            ? 'cursor-pointer hover:brightness-125 active:scale-95'
                            : ''
                        "
                        :title="
                          currentFavcatInfo.name +
                          (currentFavcatInfo.href
                            ? t('explore.click_favcat_filter')
                            : '')
                        "
                        @click="handleFavcatClick"
                      >
                        {{ currentFavcatInfo.name }}
                      </span>
                    </div>
                  </Transition>
                </div>
              </div>

              <!-- Lower: Metadata Form Grid + Action Buttons -->
              <div class="space-y-3.5 sm:space-y-4">
                <!-- Metadata Form Grid (One per row: Key: Value) -->
                <div
                  class="grid grid-cols-[auto_1fr] items-center gap-x-6 sm:gap-x-7 gap-y-1.5 sm:gap-y-2 text-xs text-left w-fit mx-auto sm:mx-0"
                >
                  <span class="text-white/50 select-none">{{
                    t("explore.page_count")
                  }}</span>
                  <span
                    class="text-white/95 font-medium truncate inline-flex items-center"
                  >
                    <span>{{
                      selectedPub?.pageCount
                        ? t("common.page_count", {
                            count: selectedPub.pageCount,
                          })
                        : t("common.unknown")
                    }}</span>
                    <Transition
                      enter-active-class="transition-opacity duration-300 ease-out"
                      enter-from-class="opacity-0"
                      enter-to-class="opacity-100"
                      leave-active-class="transition-opacity duration-200 ease-in"
                      leave-from-class="opacity-100"
                      leave-to-class="opacity-0"
                    >
                      <span
                        v-if="currentFileSize"
                        class="font-mono text-[11.5px] text-white/80 inline-block align-baseline"
                        >（{{ currentFileSize }}）</span
                      >
                    </Transition>
                  </span>

                  <template v-if="selectedPub?.publishTime">
                    <span class="text-white/50 select-none">{{
                      t("explore.posted_date")
                    }}</span>
                    <span
                      class="text-white/95 font-medium font-mono text-[11.5px] truncate"
                      >{{ formatDisplayDate(selectedPub.publishTime) }}</span
                    >
                  </template>

                  <template v-if="selectedPub?.uploader">
                    <span class="text-white/50 select-none">{{
                      t("explore.uploader")
                    }}</span>
                    <span
                      class="text-white/95 font-medium hover:text-white underline decoration-white/30 underline-offset-2 cursor-pointer transition-colors truncate"
                      :title="
                        t('explore.search_uploader', {
                          uploader: selectedPub.uploader,
                        })
                      "
                      @click="handleUploaderClick(selectedPub.uploader)"
                    >
                      {{ selectedPub.uploader }}
                    </span>
                  </template>
                </div>

                <!-- Action Buttons -->
                <div
                  v-if="selectedPub"
                  class="flex items-center gap-2.5 w-full sm:w-auto justify-center sm:justify-start mx-auto"
                >
                  <!-- Primary Button: Read Now / Continue Reading / Read Again (Text only) -->
                  <Button
                    :disabled="isPreparingReader"
                    class="rounded-full bg-white text-neutral-900 hover:bg-white/90 font-medium text-xs sm:text-sm h-9 px-6 sm:px-8 flex-initial w-auto min-w-36 max-w-[220px] sm:max-w-none sm:min-w-44 shadow-md transition-all active:scale-95 disabled:opacity-70 gap-2"
                    @click="handlePrimaryRead"
                  >
                    <Loader2
                      v-if="isPreparingReader"
                      class="size-3.5 animate-spin shrink-0"
                    />
                    <span>{{ primaryReadButtonLabel }}</span>
                  </Button>

                  <!-- Secondary Icon Button: Incognito Read -->
                  <Button
                    variant="ghost"
                    size="icon"
                    class="size-9 rounded-full border border-white/20 bg-white/15 hover:bg-white/25 text-white transition-all shrink-0 active:scale-95 focus-visible:ring-0 focus:outline-none"
                    :title="t('explore.incognito_read')"
                    @click="handleIncognitoRead"
                  >
                    <HatGlasses class="size-4" />
                  </Button>

                  <!-- Secondary Icon Button: Favorite (Category selection dropdown) -->
                  <DropdownMenu>
                    <DropdownMenuTrigger as-child>
                      <Button
                        variant="ghost"
                        size="icon"
                        class="size-9 rounded-full border transition-all shrink-0 active:scale-95 focus-visible:ring-0 focus:outline-none"
                        :class="
                          isCurrentFavorited
                            ? 'bg-amber-400/15 hover:bg-amber-400/25 border-amber-400/35 text-amber-400'
                            : 'bg-white/15 hover:bg-white/25 border-white/20 text-white'
                        "
                        :title="
                          !s.status.value?.site?.has_ipb
                            ? t('archive.dialog_new_no_ipb')
                            : isCurrentFavorited
                              ? currentFavcatInfo?.name
                                ? `${t('common.favorited')}: ${currentFavcatInfo.name}`
                                : t('explore.favorite_move')
                              : t('explore.favorite_select')
                        "
                        :disabled="
                          !s.status.value?.site?.has_ipb || s.busy.value
                        "
                      >
                        <Bookmark
                          class="size-4 transition-transform"
                          :class="
                            isCurrentFavorited
                              ? 'fill-amber-400 text-amber-400'
                              : ''
                          "
                        />
                      </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent
                      align="start"
                      class="w-56 max-h-[70vh] overflow-y-auto overlay-scrollbar"
                    >
                      <div
                        class="px-2.5 py-1.5 text-xs font-semibold text-muted-foreground select-none"
                      >
                        {{
                          isCurrentFavorited
                            ? t("explore.favorite_move")
                            : t("explore.favorite_select")
                        }}
                      </div>
                      <DropdownMenuSeparator />

                      <DropdownMenuItem
                        v-for="cat in favcatList"
                        :key="cat.id"
                        class="flex items-center justify-between cursor-pointer py-1.5"
                        @click="handleSelectFavcat(cat.id)"
                      >
                        <div class="flex items-center gap-2 min-w-0">
                          <span
                            class="size-2.5 rounded-full shrink-0"
                            :style="{
                              backgroundColor: getFavcatSolidColor(cat.id),
                            }"
                          />
                          <span class="truncate text-xs"
                            >{{ cat.id }}: {{ cat.name }}</span
                          >
                        </div>
                        <Check
                          v-if="
                            isCurrentFavorited && currentFavcatId === cat.id
                          "
                          class="size-3.5 text-amber-400 shrink-0 ml-1.5 stroke-[2.5]"
                        />
                      </DropdownMenuItem>

                      <template v-if="isCurrentFavorited">
                        <DropdownMenuSeparator />
                        <DropdownMenuItem
                          variant="destructive"
                          class="cursor-pointer gap-2 py-1.5 text-xs text-rose-500 focus:text-rose-500 focus:bg-rose-500/10"
                          @click="handleRemoveFavorite"
                        >
                          <BookmarkX class="size-3.5 text-rose-500" />
                          <span>{{ t("archive.remove_fav") }}</span>
                        </DropdownMenuItem>
                      </template>
                    </DropdownMenuContent>
                  </DropdownMenu>

                  <!-- Secondary Icon Button: Archive (Dropdown menu when archived/associated, single button when unarchived) -->
                  <DropdownMenu v-if="currentArchiveItem?.document_id || currentArchiveItem?.id">
                    <DropdownMenuTrigger as-child>
                      <Button
                        variant="ghost"
                        size="icon"
                        class="size-9 rounded-full border transition-all shrink-0 active:scale-95 focus-visible:ring-0 focus:outline-none"
                        :class="
                          currentArchiveBadge?.isReady
                            ? 'bg-emerald-500/15 hover:bg-emerald-500/25 border-emerald-500/35 text-emerald-400'
                            : currentArchiveBadge?.isDownloading || isArchiveQuoteBusy
                              ? 'bg-white/20 border-white/30 text-white cursor-wait'
                              : currentArchiveBadge?.isFailed
                                ? 'bg-rose-500/15 hover:bg-rose-500/25 border-rose-500/35 text-rose-400'
                                : 'bg-white/15 hover:bg-white/25 border-white/20 text-white'
                        "
                        :title="t('archive.title')"
                      >
                        <Loader2
                          v-if="currentArchiveBadge?.isDownloading || isArchiveQuoteBusy"
                          class="size-4 animate-spin text-white"
                        />
                        <Package2 v-else class="size-4" />
                      </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="start" class="w-40">
                      <DropdownMenuItem
                        v-if="currentArchiveBadge?.isUnavailable"
                        class="cursor-pointer"
                        @click="emit('archive-manage', currentArchiveItem)"
                      >
                        {{ t('archive.manage') }}
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        class="cursor-pointer"
                        :disabled="isArchiveQuoteBusy || currentArchiveBadge?.isDownloading"
                        @click="requestArchiveAction('unlink', currentArchiveItem)"
                      >
                        {{ t('archive.unlink') }}
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        class="text-rose-400 focus:text-rose-400 focus:bg-rose-500/10 cursor-pointer"
                        :disabled="isArchiveQuoteBusy || currentArchiveBadge?.isDownloading"
                        @click="requestArchiveAction('delete', currentArchiveItem)"
                      >
                        {{ t('archive.delete') }}
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>

                  <Button
                    v-else
                    variant="ghost"
                    size="icon"
                    class="size-9 rounded-full border transition-all shrink-0 active:scale-95 focus-visible:ring-0 focus:outline-none"
                    :class="
                      currentArchiveBadge?.isReady
                        ? 'bg-emerald-500/15 hover:bg-emerald-500/25 border-emerald-500/35 text-emerald-400'
                        : currentArchiveBadge?.isDownloading ||
                            isArchiveQuoteBusy
                          ? 'bg-white/20 border-white/30 text-white cursor-wait'
                          : 'bg-white/15 hover:bg-white/25 border-white/20 text-white'
                    "
                    :title="
                      currentArchiveBadge?.isReady
                        ? t('explore.status_archived')
                        : currentArchiveBadge?.isDownloading
                          ? t('explore.status_downloading')
                          : currentArchiveBadge?.isFailed
                            ? t('explore.status_failed')
                            : t('explore.status_unarchived')
                    "
                    :disabled="
                      Boolean(
                        currentArchiveBadge?.isDownloading ||
                        isArchiveQuoteBusy,
                      )
                    "
                    @click="handleArchive(selectedPub)"
                  >
                    <Loader2
                      v-if="
                        currentArchiveBadge?.isDownloading || isArchiveQuoteBusy
                      "
                      class="size-4 animate-spin text-white"
                    />
                    <Check
                      v-else-if="currentArchiveBadge?.isReady"
                      class="size-4 text-emerald-400 stroke-[2.5]"
                    />
                    <Download
                      v-else
                      class="size-4"
                      :class="
                        currentArchiveBadge?.isFailed ? 'text-rose-400' : ''
                      "
                    />
                  </Button>

                  <!-- Tertiary Icon Button: Upstream External Link -->
                  <Button
                    v-if="selectedPub?.upstreamUrl"
                    variant="ghost"
                    size="icon"
                    as="a"
                    :href="selectedPub.upstreamUrl"
                    target="_blank"
                    rel="noopener noreferrer"
                    class="size-9 rounded-full border border-white/20 bg-white/15 hover:bg-white/25 text-white transition-all shrink-0 active:scale-95 focus-visible:ring-0 focus:outline-none"
                    :title="t('explore.open_original')"
                  >
                    <ExternalLink class="size-4" />
                  </Button>
                </div>
              </div>
            </div>
          </div>
        </div>

        <!-- 2. BODY CONTENT (White/Dark Theme Adaptable) -->
        <div class="p-5 sm:p-7 space-y-6 flex-1 bg-background text-foreground">
          <!-- In-dialog Error Alert -->
          <Alert v-if="s.error.value" variant="destructive" class="relative">
            <CircleAlert class="size-4" />
            <AlertDescription class="text-sm pr-6">{{
              s.error.value
            }}</AlertDescription>
            <Button
              variant="ghost"
              size="icon-xs"
              class="absolute top-2.5 right-2.5 text-destructive hover:bg-destructive/10"
              :aria-label="t('common.close_error')"
              @click="s.error.value = ''"
            >
              <X class="size-3.5" />
            </Button>
          </Alert>
          <!-- Section A: Namespace Tag Groups (NO local scrollview, flows naturally) -->
          <Transition
            enter-active-class="transition-opacity duration-300 ease-out"
            enter-from-class="opacity-0"
            enter-to-class="opacity-100"
          >
            <div
              v-if="groupedTags.length > 0"
              :key="selectedPub?.gid + ':' + selectedPub?.token"
              class="space-y-2.5 pt-1"
            >
              <div class="text-sm font-bold text-foreground">
                {{ t("explore.modal_tags") }}
              </div>

              <!-- Tags Layout: left-aligned, no quote mark, no indent -->
              <TransitionGroup name="tag-group" tag="div" class="space-y-2">
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
                      v-for="tag in grp.tags"
                      :key="grp.namespace + ':' + tag.rawName"
                      :style="tag.tagStyle"
                      :class="[
                        'text-[11px] sm:text-xs font-normal px-2.5 py-0.5 rounded-md cursor-pointer select-none transition-all duration-150 inline-flex items-center gap-1 active:scale-95',
                        tag.isWeak
                          ? 'border border-dashed'
                          : 'border border-solid',
                        tag.hasCustomStyle
                          ? 'shadow-2xs hover:brightness-110 hover:shadow-xs'
                          : tag.isWeak
                            ? 'border-border/70 text-foreground/80 bg-muted/20 hover:bg-primary hover:text-primary-foreground hover:border-solid hover:border-primary'
                            : 'border-border text-foreground/90 bg-muted/30 hover:bg-primary hover:text-primary-foreground hover:border-primary',
                      ]"
                      :title="t('explore.search_tag', { tag: tag.searchName })"
                      @click="handleTagClick(tag.searchName)"
                    >
                      {{ tag.displayName }}
                    </span>
                  </TransitionGroup>
                </div>
              </TransitionGroup>
            </div>
          </Transition>

          <!-- Section C: Thumbnails Preview (页面预览) -->
          <div
            v-if="selectedPub?.readingOrder?.length > 0"
            class="space-y-3"
            :class="{ 'pt-5 sm:pt-6 border-t': groupedTags.length > 0 }"
          >
            <div
              class="flex items-center justify-between cursor-pointer group/sec select-none"
              @click="detailSubView = 'thumbnails'"
            >
              <span
                class="text-sm font-bold text-foreground inline-flex items-center gap-1 group-hover/sec:text-primary transition-colors"
              >
                {{ t("explore.modal_preview") }}
                <ChevronRight
                  class="size-3.5 opacity-60 group-hover/sec:translate-x-0.5 transition-transform"
                />
              </span>
              <span
                class="text-[11px] text-muted-foreground group-hover/sec:text-foreground transition-colors"
              >
                {{
                  t("explore.modal_pages_total", {
                    count:
                      selectedPub.pageCount || selectedPub.readingOrder.length,
                  })
                }}
              </span>
            </div>

            <!-- Horizontal Thumbnails Track -->
            <div
              class="flex gap-3 overflow-x-auto pb-2 pt-1 overlay-scrollbar select-none"
            >
              <div
                v-for="(page, idx) in previewPages"
                :key="page.href || idx"
                class="group/thumb relative w-24 sm:w-28 shrink-0 cursor-pointer transition-transform duration-200 hover:-translate-y-1"
                @click="handleRead(selectedPub, idx)"
              >
                <div
                  class="relative aspect-[1/1.414] w-full overflow-hidden rounded-lg border bg-muted shadow-sm group-hover/thumb:shadow-md group-hover/thumb:border-primary/50 transition-all"
                >
                  <CoverImage
                    :src="page.thumbnailHref || page.href"
                    :alt="t('common.page_n', { n: idx + 1 })"
                  />
                  <!-- Hover overlay with read icon -->
                  <div
                    class="absolute inset-0 bg-black/0 group-hover/thumb:bg-black/25 transition-colors flex items-center justify-center"
                  >
                    <BookOpen
                      class="size-5 text-white opacity-0 group-hover/thumb:opacity-100 transition-opacity drop-shadow"
                    />
                  </div>
                </div>
                <p
                  class="text-[10px] text-center text-muted-foreground mt-1 group-hover/thumb:text-foreground transition-colors"
                >
                  {{ t("common.page_n", { n: idx + 1 }) }}
                </p>
              </div>

              <!-- Trailing "See all" card if more pages exist -->
              <div
                v-if="selectedPub.readingOrder.length > previewPages.length"
                class="group/more relative w-24 sm:w-28 shrink-0 cursor-pointer flex flex-col items-center justify-center aspect-[1/1.414] rounded-lg border border-dashed border-border/80 hover:border-primary/60 bg-muted/20 hover:bg-muted/40 transition-all text-center"
                @click="detailSubView = 'thumbnails'"
              >
                <Grid
                  class="size-5 text-muted-foreground group-hover/more:text-primary transition-colors mb-1"
                />
                <span
                  class="text-[11px] font-medium text-muted-foreground group-hover/more:text-foreground"
                  >{{ t("explore.all_pages") }}</span
                >
                <span class="text-[9px] text-muted-foreground/70 mt-0.5">{{
                  t("common.page_count", {
                    count: selectedPub.readingOrder.length,
                  })
                }}</span>
              </div>
            </div>
          </div>

          <!-- Section D: Reviews Preview (Apple Books style) -->
          <div
            v-if="selectedPub?.reviews?.length > 0"
            class="space-y-3"
            :class="{
              'pt-5 sm:pt-6 border-t':
                groupedTags.length > 0 ||
                (selectedPub?.readingOrder?.length || 0) > 0,
            }"
          >
            <div
              class="flex items-center justify-between cursor-pointer group/sec select-none"
              @click="detailSubView = 'reviews'"
            >
              <span
                class="text-sm font-bold text-foreground inline-flex items-center gap-1 group-hover/sec:text-primary transition-colors"
              >
                {{ t("explore.modal_reviews") }}
                <ChevronRight
                  class="size-3.5 opacity-60 group-hover/sec:translate-x-0.5 transition-transform"
                />
              </span>
              <span
                class="text-[11px] text-muted-foreground group-hover/sec:text-foreground transition-colors"
              >
                {{
                  t("explore.modal_reviews_total", {
                    count: selectedPub.reviews.length,
                  })
                }}
              </span>
            </div>

            <!-- Horizontal Review Cards Track -->
            <div
              class="flex gap-3 overflow-x-auto pb-2 pt-1 overlay-scrollbar select-none"
            >
              <div
                v-for="rev in previewReviews"
                :key="rev.id || rev.time + rev.username"
                class="group/rev relative w-72 sm:w-80 shrink-0 p-3.5 sm:p-4 rounded-xl border bg-muted/40 dark:bg-muted/20 hover:bg-muted/60 dark:hover:bg-muted/30 cursor-pointer transition-all duration-200 flex flex-col justify-between space-y-3"
                @click="detailSubView = 'reviews'"
              >
                <p
                  class="text-xs sm:text-[13px] text-foreground/90 line-clamp-3 leading-relaxed break-words font-normal"
                >
                  {{ stripHtmlToSnippet(rev.content) }}
                </p>
                <div
                  class="flex items-center justify-between text-[11px] text-muted-foreground pt-1 border-t border-border/30"
                >
                  <span
                    class="font-medium text-foreground/80 truncate max-w-[150px] group-hover/rev:text-primary transition-colors"
                  >
                    {{ rev.username }}
                  </span>
                  <span class="font-mono text-[10px]">{{
                    rev.time ? rev.time.slice(0, 10) : ""
                  }}</span>
                </div>
              </div>

              <!-- Trailing "See all" card if more reviews exist -->
              <div
                v-if="selectedPub.reviews.length > previewReviews.length"
                class="group/more relative w-32 sm:w-36 shrink-0 cursor-pointer flex flex-col items-center justify-center p-4 rounded-xl border border-dashed border-border/80 hover:border-primary/60 bg-muted/20 hover:bg-muted/40 transition-all text-center"
                @click="detailSubView = 'reviews'"
              >
                <MessageSquare
                  class="size-5 text-muted-foreground group-hover/more:text-primary transition-colors mb-1.5"
                />
                <span
                  class="text-xs font-medium text-muted-foreground group-hover/more:text-foreground"
                  >{{ t("explore.all_reviews") }}</span
                >
                <span class="text-[10px] text-muted-foreground/70 mt-0.5">{{
                  t("explore.modal_reviews_total", {
                    count: selectedPub.reviews.length,
                  })
                }}</span>
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
          class="absolute inset-0 z-30 bg-background flex flex-col overflow-y-auto overlay-scrollbar border-l border-border/40 shadow-[-8px_0_30px_rgba(0,0,0,0.18)] select-text"
        >
          <!-- Sticky Subview Header -->
          <div
            class="sticky top-0 z-20 bg-background/95 backdrop-blur-md border-b px-5 sm:px-7 py-3 flex items-center justify-between"
          >
            <div class="flex items-center gap-2">
              <Button
                variant="ghost"
                size="sm"
                class="gap-1 text-xs -ml-2 text-foreground/80 hover:text-foreground"
                @click="detailSubView = 'main'"
              >
                <ChevronLeft class="size-4" />
                <span>{{ t("explore.back_to_detail") }}</span>
              </Button>
              <div class="h-4 w-px bg-border" />
              <span class="text-sm font-bold text-foreground">
                {{ t("explore.modal_preview") }}
              </span>
            </div>
            <div class="flex items-center gap-3">
              <span class="text-xs text-muted-foreground hidden sm:inline">
                {{
                  t("explore.preview_hint", {
                    count: selectedPub?.readingOrder?.length || 0,
                  })
                }}
              </span>
              <!-- Quick Page Jump (if more than 48 pages) -->
              <div
                v-if="(selectedPub?.readingOrder?.length || 0) > 48"
                class="flex items-center gap-1.5 text-xs text-muted-foreground bg-muted/40 border rounded-lg px-2 py-0.5"
              >
                <span class="text-[11px] select-none">{{
                  t("explore.jump_to")
                }}</span>
                <input
                  v-model.number="pageJumpInput"
                  type="number"
                  min="1"
                  :max="selectedPub?.readingOrder?.length || 1"
                  class="w-12 h-6 text-xs text-center border rounded bg-background text-foreground px-1 outline-none font-mono focus:border-primary"
                  :placeholder="t('explore.page_no')"
                  @keydown.enter.prevent="handleQuickJump"
                />
                <Button
                  variant="ghost"
                  size="sm"
                  class="h-6 text-[11px] px-1.5 text-muted-foreground hover:text-foreground"
                  @click="handleQuickJump"
                >
                  {{ t("common.jump") }}
                </Button>
              </div>
              <DialogClose as-child>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  class="size-8 rounded-full text-muted-foreground hover:text-foreground"
                  :title="t('common.close_esc')"
                >
                  <X class="size-4" />
                </Button>
              </DialogClose>
            </div>
          </div>

          <!-- Mobile Hint -->
          <div class="sm:hidden px-5 pt-3 text-xs text-muted-foreground">
            {{
              t("explore.preview_hint", {
                count: selectedPub?.readingOrder?.length || 0,
              })
            }}
          </div>

          <!-- Thumbnails Grid -->
          <div class="p-5 sm:p-7 flex-1">
            <div
              v-if="
                !selectedPub?.readingOrder ||
                selectedPub.readingOrder.length === 0
              "
              class="py-16 text-center text-muted-foreground text-sm"
            >
              {{ t("explore.no_thumbnails") }}
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
                <div
                  class="relative aspect-[1/1.414] w-full overflow-hidden rounded-xl border bg-muted shadow-xs group-hover/gridpage:shadow-md group-hover/gridpage:border-primary transition-all active:scale-95"
                >
                  <CoverImage
                    :src="page.thumbnailHref || page.href"
                    :alt="t('common.page_n', { n: idx + 1 })"
                  />
                  <!-- Hover overlay with read icon -->
                  <div
                    class="absolute inset-0 bg-black/35 opacity-0 group-hover/gridpage:opacity-100 flex items-center justify-center transition-opacity"
                  >
                    <BookOpen class="size-7 text-white drop-shadow-md" />
                  </div>
                </div>
                <span
                  class="text-xs text-muted-foreground mt-1.5 group-hover/gridpage:text-foreground group-hover/gridpage:font-medium transition-colors"
                >
                  {{ t("common.page_n", { n: idx + 1 }) }}
                </span>
              </div>

              <!-- Progressive loading sentinel for thumbnails -->
              <div
                v-if="
                  thumbnailRenderLimit <
                  (selectedPub?.readingOrder?.length || 0)
                "
                ref="thumbnailSentinelRef"
                class="col-span-full py-6 text-center text-xs text-muted-foreground flex items-center justify-center gap-2"
              >
                <RefreshCw class="size-3.5 animate-spin opacity-60" />
                <span>{{
                  t("explore.loading_more_thumbs", {
                    shown: displayedThumbnails.length,
                    total: selectedPub?.readingOrder?.length,
                  })
                }}</span>
              </div>
            </div>
          </div>
        </div>

        <!-- ================= VIEW 3: FULL REVIEWS LIST ================= -->
        <div
          v-else-if="detailSubView === 'reviews'"
          key="reviews"
          class="absolute inset-0 z-30 bg-background flex flex-col overflow-y-auto overlay-scrollbar border-l border-border/40 shadow-[-8px_0_30px_rgba(0,0,0,0.18)] select-text"
        >
          <!-- Sticky Subview Header -->
          <div
            class="sticky top-0 z-20 bg-background/95 backdrop-blur-md border-b px-5 sm:px-7 py-3 flex items-center justify-between"
          >
            <div class="flex items-center gap-2">
              <Button
                variant="ghost"
                size="sm"
                class="gap-1 text-xs -ml-2 text-foreground/80 hover:text-foreground"
                @click="detailSubView = 'main'"
              >
                <ChevronLeft class="size-4" />
                <span>{{ t("explore.back_to_detail") }}</span>
              </Button>
              <div class="h-4 w-px bg-border" />
              <span class="text-sm font-bold text-foreground">
                {{ t("explore.modal_reviews") }}
              </span>
            </div>
            <div class="flex items-center gap-3">
              <span class="text-xs text-muted-foreground hidden sm:inline">
                {{
                  t("explore.modal_reviews_total", {
                    count: selectedPub?.reviews?.length || 0,
                  })
                }}
              </span>
              <DialogClose as-child>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  class="size-8 rounded-full text-muted-foreground hover:text-foreground"
                  :title="t('common.close_esc')"
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
              <p>{{ t("explore.no_reviews") }}</p>
            </div>

            <div
              v-for="rev in displayedReviews"
              :key="rev.id || rev.time + rev.username"
              class="p-4 sm:p-5 rounded-2xl border bg-card/60 space-y-3 shadow-xs hover:border-border transition-colors select-text"
            >
              <!-- Reviewer & Time Info -->
              <div class="flex items-center justify-between">
                <div class="flex items-center gap-2">
                  <div
                    class="size-7 rounded-full bg-primary/10 text-primary flex items-center justify-center font-bold text-xs select-none"
                  >
                    {{ (rev.username || "?").charAt(0).toUpperCase() }}
                  </div>
                  <div>
                    <span
                      class="text-xs sm:text-sm font-semibold text-foreground"
                    >
                      {{ rev.username }}
                    </span>
                  </div>
                </div>
                <div
                  class="text-right text-[11px] text-muted-foreground font-mono"
                >
                  <div>{{ rev.time }}</div>
                  <div
                    v-if="rev.lastEditTime"
                    class="text-[10px] text-muted-foreground/70"
                  >
                    {{ t("explore.edited_at", { time: rev.lastEditTime }) }}
                  </div>
                </div>
              </div>

              <!-- Review Content HTML with lazy images & safe links -->
              <ReviewHtml
                class="text-xs sm:text-[13px] text-foreground/90 break-words leading-relaxed review-html-content"
                :html="rev.content"
                @gallery-link="openLinkedGallery"
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
              <span>{{
                t("explore.loading_more_reviews", {
                  shown: displayedReviews.length,
                  total: selectedPub?.reviews?.length,
                })
              }}</span>
            </div>
          </div>
        </div>
      </Transition>
    </DialogContent>
  </Dialog>
  <AlertDialog :open="Boolean(archiveAction)" @update:open="(open)=>{if(!open)archiveAction=null}">
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>{{ archiveAction?.action==="unlink"?t('archive.unlink'):t('archive.delete') }}</AlertDialogTitle>
        <AlertDialogDescription>{{ archiveAction?.action==="unlink"?t('archive.confirm_unlink'):t('archive.confirm_delete',{filename:archiveAction?.item?.path||archiveAction?.item?.title||archiveAction?.item?.document_id||''}) }}</AlertDialogDescription>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <AlertDialogCancel>{{ t('common.cancel') }}</AlertDialogCancel>
        <AlertDialogAction @click="confirmArchiveAction">{{ archiveAction?.action==="unlink"?t('archive.unlink'):t('archive.delete') }}</AlertDialogAction>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</template>

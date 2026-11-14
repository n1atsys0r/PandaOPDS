<script setup>
import { ref, computed, watch, onMounted, onBeforeUnmount } from "vue";
import { Alert, AlertDescription } from "@/components/ui/alert";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectSeparator,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import {
  Sidebar,
  SidebarFooter,
  SidebarHeader,
  SidebarInset,
  SidebarProvider,
  SidebarTrigger,
} from "@/components/ui/sidebar";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import {
  Breadcrumb,
  BreadcrumbList,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";
import AppSidebarNav from "@/components/AppSidebarNav.vue";
import ArchiveReader from "@/components/ArchiveReader.vue";
import CoverImage from "@/components/CoverImage.vue";
import HomeEditor from "@/components/HomeEditor.vue";
import LoginForm from "@/components/LoginForm.vue";
import OpdsHome from "@/components/OpdsHome.vue";
import ThemeAutoIcon from "@/components/ThemeAutoIcon.vue";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@/components/ui/dropdown-menu";
import {
  SunIcon as Sun,
  MoonIcon as Moon,
  MonitorIcon as Monitor,
  RefreshCwIcon as RefreshCw,
  RotateCcwIcon as RotateCcw,
  BookOpenIcon as BookOpen,
  SaveIcon as Save,
  Trash2Icon as Trash2,
  PlusIcon as Plus,
  XIcon as X,
  ServerIcon as Server,
  DatabaseIcon as Database,
  ActivityIcon as Activity,
  SearchIcon as Search,
  CircleCheckIcon as CircleCheck,
  CircleAlertIcon as CircleAlert,
  HardDriveIcon as HardDrive,
  GlobeIcon as Globe,
  ArrowUpRightIcon as ArrowUpRight,
  LogOutIcon as LogOut,
  TagIcon as Tag,
  BookmarkIcon as Bookmark,
  ChevronDownIcon as ChevronDown,
  LanguagesIcon as Languages,
  CheckIcon as Check,
} from "@lucide/vue";
import { useApp } from "./state.js";
import { t, currentLocale, setLocale } from "./i18n.js";

function formatBytes(bytes) {
  if (!bytes || bytes === 0) return "0 B";
  const k = 1024;
  const sizes = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + " " + sizes[i];
}

function formatBinaryBytes(bytes) {
  const n = Number(bytes);
  if (!Number.isFinite(n) || n <= 0) return "";
  const k = 1024;
  const sizes = ["B", "KiB", "MiB", "GiB", "TiB"];
  const i = Math.min(Math.floor(Math.log(n) / Math.log(k)), sizes.length - 1);
  const val = parseFloat((n / Math.pow(k, i)).toFixed(i === 0 ? 0 : 1));
  return `${val}${sizes[i]}`;
}

function archiveSizeText(a) {
  if (!a || a.status === "unarchived") return "";
  const bytes =
    a.status === "ready"
      ? a.archive_size || a.metadata?.filesize || 0
      : a.archive_size || a.part_size || 0;
  return formatBinaryBytes(bytes);
}
const s = useApp();
const syncMenuOpen = ref(false);
const syncMenuRef = ref(null);
const confirmFullSyncOpen = ref(false);

function handleDocumentClick(e) {
  if (!syncMenuOpen.value) return;
  if (syncMenuRef.value && !syncMenuRef.value.contains(e.target)) {
    syncMenuOpen.value = false;
  }
}

function handleDocumentKeydown(e) {
  if (e.key === "Escape" && syncMenuOpen.value) {
    syncMenuOpen.value = false;
  }
}

onMounted(() => {
  s.init();
  document.addEventListener("click", handleDocumentClick);
  document.addEventListener("keydown", handleDocumentKeydown);
});
onBeforeUnmount(() => {
  s.destroy();
  document.removeEventListener("click", handleDocumentClick);
  document.removeEventListener("keydown", handleDocumentKeydown);
});

function cycleTheme() {
  const current = s.theme.value || "system";
  if (current === "system") {
    s.setTheme("dark");
  } else if (current === "dark") {
    s.setTheme("light");
  } else {
    s.setTheme("system");
  }
}

const localeMeta = computed(() => {
  return {
    title: t("locale.menu_title"),
    ariaLabel: t("locale.menu_aria"),
  };
});

const themeMeta = computed(() => {
  const theme = s.theme.value || "system";
  if (theme === "dark") {
    return {
      name: "dark",
      label: t("theme.dark"),
      title: t("theme.menu_title"),
      ariaLabel: t("theme.menu_aria"),
      icon: Moon,
    };
  }
  if (theme === "light") {
    return {
      name: "light",
      label: t("theme.light"),
      title: t("theme.menu_title"),
      ariaLabel: t("theme.menu_aria"),
      icon: Sun,
    };
  }
  return {
    name: "system",
    label: t("theme.system"),
    title: t("theme.menu_title"),
    ariaLabel: t("theme.menu_aria"),
    icon: ThemeAutoIcon,
  };
});
const groups = computed(() => {
  let f = s.config.value?.fields || {};
  let map = {
    [t("config.group_site")]: [
      "eh_site",
      "public_base_url",
      "pse_page_base",
    ],
    [t("config.group_auth")]: ["ipb_member_id", "ipb_pass_hash", "igneous"],
    [t("config.group_features")]: [
      "tag_status_filter",
      "tag_translation_enabled",
      "comments_enabled",
      "image_cache_enabled",
      "cache_max_gb",
    ],
    [t("config.group_favorites")]: [
      "favorites_sync_interval_seconds",
      "favorites_sync_categories",
      "favorites_sync_archive",
    ],
    [t("config.group_archive")]: ["archive_update_interval_days", "archive_auto_update"],
  };
  return Object.entries(map)
    .map(([title, keys]) => ({
      title,
      fields: keys.filter((k) => f[k]).map((k) => [k, f[k]]),
    }))
    .filter((x) => x.fields.length);
});
const labels = new Proxy({}, {
  get(_, k) {
    const map = {
      eh_site: t("config.label_eh_site"),
      public_base_url: t("config.label_public_base_url"),
      pse_page_base: t("config.label_pse_page_base"),
      ipb_member_id: "IPB Member ID",
      ipb_pass_hash: "IPB Pass Hash",
      igneous: "Igneous Cookie",
      tag_status_filter: t("config.label_tag_status_filter"),
      tag_translation_enabled: t("config.label_tag_translation_enabled"),
      comments_enabled: t("config.label_comments_enabled"),
      image_cache_enabled: t("config.label_image_cache_enabled"),
      cache_max_gb: t("config.label_cache_max_gb"),
      favorites_sync_interval_seconds: t("config.label_favorites_sync_interval_seconds"),
      favorites_sync_categories: t("config.label_favorites_sync_categories"),
      favorites_sync_archive: t("config.label_favorites_sync_archive"),
      archive_update_interval_days: t("config.label_archive_update_interval_days"),
      archive_auto_update: t("config.label_archive_auto_update"),
    };
    return map[k] || k;
  }
});
const secret = (k) => ["ipb_member_id", "ipb_pass_hash", "igneous"].includes(k);
const boolean = (k) =>
  [
    "tag_translation_enabled",
    "comments_enabled",
    "image_cache_enabled",
    "archive_auto_update",
  ].includes(k) || typeof s.config.value?.fields?.[k]?.active === "boolean";
const edit = (k, v) => s.setField(k, v);
const lockedReason = (key) => {
  if (key === "ipb_member_id" || key === "ipb_pass_hash")
    return t("config.locked_reason_ipb");
  const env =
    {
      eh_site: "EH_SITE",
      igneous: "IGNEOUS",
      public_base_url: "PUBLIC_BASE_URL",
      pse_page_base: "PSE_PAGE_BASE",
      tag_status_filter: "TAG_STATUS_FILTER",
      tag_translation_enabled: "TAG_TRANSLATION_ENABLED",
      comments_enabled: "COMMENTS_ENABLED",
      image_cache_enabled: "IMAGE_CACHE_ENABLED",
      cache_max_gb: "CACHE_MAX_GB",
      favorites_sync_interval_seconds: "FAVORITES_SYNC_INTERVAL_SECONDS",
      favorites_sync_categories: "FAVORITES_SYNC_CATEGORIES",
    }[key] || key.toUpperCase();
  return t("config.locked_reason_env", { env });
};
const qualityLabel = (entry) => {
  const tier = String(entry.or || entry.quality || "").toLowerCase();
  if (tier === "org" || tier.includes("original")) return t("archive.tier_original");
  if (tier === "res" || tier.includes("resample")) return t("archive.tier_resample");
  if (entry.status === "unarchived") return t("archive.tier_online");
  return t("archive.tier_archived");
};
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

const createArchiveDialogOpen = ref(false);
const readerArchive = ref(null);
const readerOpen = computed({
  get: () => Boolean(readerArchive.value),
  set: (v) => {
    if (!v) readerArchive.value = null;
  },
});
const psePageBase = computed(() => {
  const base = Number(s.status.value?.site?.pse_page_base);
  return Number.isFinite(base) ? base : 1;
});
const selectedStatus = ref("all"); // "all" | "ready" | "unarchived" | "updates" | "downloading" | "failed"
const selectedCategory = ref("all"); // "all" | EH category key
const selectedFavcat = ref("all"); // "all" | "unfavorited" | "favorited" | "0".."9"
const archiveSearch = ref("");
const visibleLimit = ref(60);

watch([selectedStatus, selectedCategory, selectedFavcat, archiveSearch], () => {
  visibleLimit.value = 60;
});

function archivePageCount(a) {
  return Math.max(0, Number(a?.page_count || a?.metadata?.filecount || 0));
}

function canReadOnline(a) {
  if (!a || archivePageCount(a) <= 0) return false;
  if (!a.status) return true;
  return ["ready", "unarchived", "failed"].includes(a.status);
}

function openReader(a) {
  if (!canReadOnline(a)) return;
  readerArchive.value = a;
}

async function handleExploreArchive(pub) {
  await handleQuoteArchive({
    gid: pub.gid,
    token: pub.token,
    title: pub.title,
  });
}

async function handleExploreFavorite(pub) {
  const item = s.archives.value?.find(
    (a) =>
      (a.gid === pub.gid || a.active_gid === pub.gid) &&
      (a.token === pub.token || a.active_token === pub.token),
  );
  if (item?.is_favorited) {
    s.requestFavoriteRemove(item);
  } else {
    await s.favorite(pub, "add", 0);
  }
}

function requestFullSync() {
  syncMenuOpen.value = false;
  confirmFullSyncOpen.value = true;
}

async function confirmAndRunFullSync() {
  confirmFullSyncOpen.value = false;
  await s.syncFavorites(true);
}

const showArchiveScanBanner = computed(() => {
  const scan = s.archiveScan.value;
  if (!scan || scan.status === "idle") return false;
  if (scan.status === "running") return true;
  const k = `${scan.id || 0}:${scan.completed_at || 0}:${scan.status}`;
  return k !== s.archiveScanDismissedKey.value;
});

function openCreateArchiveDialog() {
  s.archiveInput.value = "";
  s.quote.value = null;
  s.selectedQuality.value = "";
  createArchiveDialogOpen.value = true;
}

function closeCreateArchiveDialog() {
  createArchiveDialogOpen.value = false;
  s.archiveInput.value = "";
  s.quote.value = null;
  s.selectedQuality.value = "";
}

async function confirmAndStartArchive() {
  await s.startArchive();
  createArchiveDialogOpen.value = false;
}

async function handleQuoteArchive(a) {
  await s.quoteArchiveFor(a);
  if (s.quote.value) s.confirmArchive.value = true;
}

async function handleQuoteUpdate(a) {
  await s.quoteUpdateFor(a);
  if (s.quote.value) s.confirmArchive.value = true;
}

function resetFilters() {
  selectedStatus.value = "all";
  selectedCategory.value = "all";
  selectedFavcat.value = "all";
  archiveSearch.value = "";
}

const isFiltered = computed(() => {
  return (
    selectedStatus.value !== "all" ||
    (selectedCategory.value !== "all" && selectedCategory.value !== "") ||
    (selectedFavcat.value !== "all" && selectedFavcat.value !== "") ||
    Boolean(archiveSearch.value.trim())
  );
});

const statusCounts = computed(() => {
  let ready = 0;
  let unarchived = 0;
  let updates = 0;
  let downloading = 0;
  let failed = 0;
  const dlStatuses = ["downloading", "zipping", "pending", "submitting"];
  for (const a of s.archives.value || []) {
    if (a.status === "ready") ready++;
    if (a.status === "unarchived") unarchived++;
    if (a.update_available) updates++;
    if (
      dlStatuses.includes(a.status) ||
      dlStatuses.includes(a.version_update?.status)
    ) {
      downloading++;
    }
    if (a.status === "failed" || a.version_update?.status === "failed") {
      failed++;
    }
  }
  return {
    all: s.archives.value?.length || 0,
    ready,
    unarchived,
    updates,
    downloading,
    failed,
  };
});

const categoryCounts = computed(() => {
  const counts = {};
  for (const cat of EH_CATEGORIES) counts[cat.key] = 0;
  for (const a of s.archives.value || []) {
    const c = a.metadata?.category;
    if (c) {
      const match = EH_CATEGORIES.find(
        (cat) => cat.key.toLowerCase() === c.toLowerCase(),
      );
      if (match) counts[match.key]++;
    }
  }
  return counts;
});

const favcatList = computed(() => {
  const counts = {};
  const archiveNames = {};
  for (let i = 0; i < 10; i++) counts[i] = 0;
  for (const a of s.archives.value || []) {
    if (a.is_favorited && a.favcat != null) {
      const id = Number(a.favcat);
      if (counts[id] !== undefined) counts[id]++;
      if (a.favcat_name && !archiveNames[id]) archiveNames[id] = a.favcat_name;
    }
  }
  const cats = s.favcats.value || [];
  const list = [];
  for (let i = 0; i < 10; i++) {
    const found = cats.find((c) => Number(c.id) === i);
    list.push({
      id: i,
      name: found?.name || archiveNames[i] || t("archive.favcat_default", { id: i }),
      count: counts[i] || 0,
    });
  }
  return list;
});

const unfavoritedCount = computed(() => {
  return (s.archives.value || []).filter((a) => !a.is_favorited).length;
});

const favoritedCount = computed(() => {
  return (s.archives.value || []).filter((a) => a.is_favorited).length;
});

const categoryLabel = computed(() => {
  if (!selectedCategory.value || selectedCategory.value === "all") {
    return t("archive.all_categories");
  }
  const found = EH_CATEGORIES.find(
    (c) => c.key.toLowerCase() === selectedCategory.value.toLowerCase(),
  );
  return found ? found.label : selectedCategory.value;
});

const favcatLabel = computed(() => {
  if (!selectedFavcat.value || selectedFavcat.value === "all") {
    return t("archive.all_favorites");
  }
  if (selectedFavcat.value === "unfavorited") return t("common.unfavorited");
  if (selectedFavcat.value === "favorited") return t("common.favorited");
  const id = Number(selectedFavcat.value);
  const found = favcatList.value.find((c) => c.id === id);
  return found ? `${found.id}: ${found.name}` : t("archive.favcat_default", { id });
});

const filteredArchives = computed(() => {
  let list = s.archives.value || [];

  // 1. 状态维度
  if (selectedStatus.value === "ready") {
    list = list.filter((a) => a.status === "ready");
  } else if (selectedStatus.value === "unarchived") {
    list = list.filter((a) => a.status === "unarchived");
  } else if (selectedStatus.value === "updates") {
    list = list.filter((a) => Boolean(a.update_available));
  } else if (selectedStatus.value === "downloading") {
    const dlStatuses = ["downloading", "zipping", "pending", "submitting"];
    list = list.filter(
      (a) =>
        dlStatuses.includes(a.status) ||
        dlStatuses.includes(a.version_update?.status),
    );
  } else if (selectedStatus.value === "failed") {
    list = list.filter(
      (a) => a.status === "failed" || a.version_update?.status === "failed",
    );
  }

  // 2. 分类维度
  if (selectedCategory.value && selectedCategory.value !== "all") {
    const target = selectedCategory.value.toLowerCase();
    list = list.filter(
      (a) => (a.metadata?.category || "").toLowerCase() === target,
    );
  }

  // 3. 收藏维度
  if (selectedFavcat.value === "unfavorited") {
    list = list.filter((a) => !a.is_favorited);
  } else if (selectedFavcat.value === "favorited") {
    list = list.filter((a) => a.is_favorited);
  } else if (selectedFavcat.value !== "all" && selectedFavcat.value !== "") {
    const targetFavcat = Number(selectedFavcat.value);
    list = list.filter(
      (a) => a.is_favorited && Number(a.favcat) === targetFavcat,
    );
  }

  // 4. 搜索维度
  const q = archiveSearch.value.trim().toLowerCase();
  if (q) {
    list = list.filter((a) => {
      const title = (a.title || "").toLowerCase();
      const titleJpn = (a.metadata?.title_jpn || "").toLowerCase();
      const titleEn = (a.metadata?.title || "").toLowerCase();
      const gid = String(a.gid || "");
      const uploader = (a.metadata?.uploader || "").toLowerCase();
      return (
        title.includes(q) ||
        titleJpn.includes(q) ||
        titleEn.includes(q) ||
        gid.includes(q) ||
        uploader.includes(q)
      );
    });
  }

  return list;
});

const visibleArchives = computed(() =>
  filteredArchives.value.slice(0, visibleLimit.value),
);

const deleteDialogMeta = computed(() => {
  const tTarget = s.deleteTarget.value;
  if (!tTarget) {
    return {
      title: t("archive.dialog_delete_title"),
      desc: "",
      note: "",
      confirm: t("archive.action_delete"),
    };
  }
  const fav = Boolean(tTarget.is_favorited);
  if (tTarget.status === "failed") {
    return {
      title: t("archive.dialog_clear_failed_title"),
      desc: fav
        ? t("archive.delete_desc_failed_fav")
        : t("archive.delete_desc_failed"),
      note: fav ? t("archive.delete_note_failed_fav") : t("archive.delete_note_failed"),
      confirm: t("archive.action_clear_failed"),
    };
  }
  if (["pending", "downloading", "zipping", "submitting"].includes(tTarget.status)) {
    return {
      title: t("archive.dialog_cancel_title"),
      desc: fav
        ? t("archive.delete_desc_task_fav")
        : t("archive.delete_desc_task"),
      note: fav ? t("archive.delete_note_task_fav") : t("archive.delete_note_task"),
      confirm: t("archive.action_cancel"),
    };
  }
  if (tTarget.status === "unarchived") {
    return {
      title: t("archive.dialog_remove_orphan_title"),
      desc: t("archive.delete_desc_orphan"),
      note: t("archive.delete_note_orphan"),
      confirm: t("common.remove"),
    };
  }
  return {
    title: t("archive.dialog_delete_title"),
    desc: fav
      ? t("archive.delete_desc_fav")
      : t("archive.delete_desc_ready"),
    note: fav
      ? t("archive.delete_note_fav")
      : t("archive.delete_note_ready"),
    confirm: t("archive.action_delete"),
  };
});

function cardActionLabel(a) {
  if (a.status === "unarchived") {
    return a.is_favorited ? t("common.download_archive") : t("common.remove");
  }
  if (a.status === "failed") return t("archive.action_clear_failed");
  if (["pending", "downloading", "zipping", "submitting"].includes(a.status)) {
    return t("archive.action_cancel");
  }
  return t("archive.action_delete");
}
</script>
<template>
  <div v-if="!s.authChecked.value" class="min-h-svh w-full bg-background" />
  <div
    v-else-if="s.authRequired.value"
    class="flex min-h-svh w-full items-center justify-center p-6 md:p-10 bg-background"
  >
    <div class="w-full max-w-sm">
      <LoginForm />
    </div>
  </div>
  <SidebarProvider v-else class="app-layout" style="--sidebar-width: 17.5rem">
    <Sidebar collapsible="icon" variant="inset">
      <SidebarHeader
        class="brand-header px-4 py-4 group-data-[collapsible=icon]:p-2 group-data-[collapsible=icon]:items-center"
      >
        <div
          class="flex items-center gap-2.5 overflow-hidden group-data-[collapsible=icon]:justify-center"
        >
          <div
            class="size-8 shrink-0 rounded-lg bg-foreground text-background flex items-center justify-center font-bold text-sm shadow-xs select-none"
            title="PandaOPDS"
          >
            P
          </div>
          <span
            class="font-bold text-base tracking-tight text-foreground select-none truncate group-data-[collapsible=icon]:hidden"
          >
            PandaOPDS
          </span>
        </div>
      </SidebarHeader>
      <AppSidebarNav :page="s.page.value" @navigate="s.route" />
      <SidebarFooter
        v-if="s.authToken.value || s.status.value?.app?.auth_enabled"
        class="sidebar-footer border-t border-sidebar-border/60 p-3 group-data-[collapsible=icon]:p-2 group-data-[collapsible=icon]:flex group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:items-center"
      >
        <Button
          variant="ghost"
          size="xs"
          class="w-full justify-start text-xs text-muted-foreground hover:text-foreground h-7 px-2 gap-1.5 group-data-[collapsible=icon]:hidden"
          @click="s.logout"
        >
          <LogOut class="size-3.5" />{{ t("nav.logout") }}
        </Button>
        <Tooltip>
          <TooltipTrigger as-child>
            <Button
              variant="ghost"
              size="icon-sm"
              class="hidden group-data-[collapsible=icon]:inline-flex text-muted-foreground hover:text-foreground size-8"
              @click="s.logout"
            >
              <LogOut class="size-3.5" />
              <span class="sr-only">{{ t("nav.logout") }}</span>
            </Button>
          </TooltipTrigger>
          <TooltipContent side="right">
            {{ t("nav.logout") }}
          </TooltipContent>
        </Tooltip>
      </SidebarFooter>
    </Sidebar>
    <SidebarInset class="webui-inset">
      <header class="site-header">
        <SidebarTrigger :aria-label="t('nav.toggle_menu')" class="-ml-1" />
        <Separator
          orientation="vertical"
          class="header-separator mr-2 h-4 my-auto self-center shrink-0"
        />
        <Breadcrumb>
          <BreadcrumbList>
            <template v-if="s.page.value === 'explore' && s.exploreSubRoute.value">
              <BreadcrumbItem>
                <BreadcrumbLink
                  as="button"
                  type="button"
                  @click="s.route('explore')"
                >
                  {{ t("nav.explore") }}
                </BreadcrumbLink>
              </BreadcrumbItem>
              <BreadcrumbSeparator />
              <BreadcrumbItem>
                <BreadcrumbPage>
                  {{ s.exploreSubRoute.value.title }}
                </BreadcrumbPage>
              </BreadcrumbItem>
            </template>
            <template v-else>
              <BreadcrumbItem>
                <BreadcrumbPage>
                  {{
                    s.page.value === "dashboard"
                      ? t("nav.dashboard")
                      : s.page.value === "home"
                        ? t("home_editor.title")
                        : s.page.value === "explore"
                          ? t("nav.explore")
                          : s.page.value === "config"
                            ? t("nav.settings")
                            : t("nav.archive")
                  }}
                </BreadcrumbPage>
              </BreadcrumbItem>
            </template>
          </BreadcrumbList>
        </Breadcrumb>
        <div class="header-actions flex items-center gap-1">
          <!-- 语言菜单 -->
          <DropdownMenu>
            <DropdownMenuTrigger as-child>
              <Button
                variant="ghost"
                size="icon-sm"
                class="size-8 text-muted-foreground hover:text-foreground"
                :title="localeMeta.title"
                :aria-label="localeMeta.ariaLabel"
              >
                <Languages class="size-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" class="min-w-36">
              <DropdownMenuItem
                class="flex items-center justify-between"
                @click="setLocale('zh')"
              >
                <span>{{ t("locale.zh") }}</span>
                <Check
                  v-if="currentLocale === 'zh'"
                  class="size-3.5 text-primary"
                />
              </DropdownMenuItem>
              <DropdownMenuItem
                class="flex items-center justify-between"
                @click="setLocale('en')"
              >
                <span>{{ t("locale.en") }}</span>
                <Check
                  v-if="currentLocale === 'en'"
                  class="size-3.5 text-primary"
                />
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>

          <!-- 外观/主题菜单 -->
          <DropdownMenu>
            <DropdownMenuTrigger as-child>
              <Button
                variant="ghost"
                size="icon-sm"
                class="size-8 text-muted-foreground hover:text-foreground"
                :title="themeMeta.title"
                :aria-label="themeMeta.ariaLabel"
              >
                <component :is="themeMeta.icon" class="size-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" class="min-w-36">
              <DropdownMenuItem
                class="flex items-center justify-between"
                @click="s.setTheme('light')"
              >
                <span class="inline-flex items-center gap-2">
                  <Sun class="size-3.5 text-muted-foreground" />
                  <span>{{ t("theme.light") }}</span>
                </span>
                <Check
                  v-if="(s.theme.value || 'system') === 'light'"
                  class="size-3.5 text-primary"
                />
              </DropdownMenuItem>
              <DropdownMenuItem
                class="flex items-center justify-between"
                @click="s.setTheme('dark')"
              >
                <span class="inline-flex items-center gap-2">
                  <Moon class="size-3.5 text-muted-foreground" />
                  <span>{{ t("theme.dark") }}</span>
                </span>
                <Check
                  v-if="(s.theme.value || 'system') === 'dark'"
                  class="size-3.5 text-primary"
                />
              </DropdownMenuItem>
              <DropdownMenuItem
                class="flex items-center justify-between"
                @click="s.setTheme('system')"
              >
                <span class="inline-flex items-center gap-2">
                  <ThemeAutoIcon class="size-3.5 text-muted-foreground" />
                  <span>{{ t("theme.system") }}</span>
                </span>
                <Check
                  v-if="(s.theme.value || 'system') === 'system'"
                  class="size-3.5 text-primary"
                />
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </header>
      <div class="page-content">
        <Alert v-if="s.notice.value" class="app-alert success-alert">
          <CircleCheck class="size-4 text-foreground" />
          <AlertDescription class="text-sm">{{
            s.notice.value
          }}</AlertDescription>
          <Button
            variant="ghost"
            size="icon-xs"
            :aria-label="t('common.close_notice')"
            @click="s.notice.value = ''"
            ><X :size="14"
          /></Button>
        </Alert>
        <Alert v-if="s.error.value" variant="destructive" class="app-alert">
          <CircleAlert class="size-4" />
          <AlertDescription class="text-sm">{{
            s.error.value
          }}</AlertDescription>
          <Button
            variant="ghost"
            size="icon-xs"
            :aria-label="t('common.close_error')"
            @click="s.error.value = ''"
            ><X :size="14"
          /></Button>
        </Alert>
        <template v-if="s.page.value === 'dashboard'">
          <div class="heading">
            <div>
              <div class="flex items-center gap-2 mb-2">
                <span
                  class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium border border-border/80 bg-background text-foreground shadow-xs"
                >
                  <span
                    class="size-1.5 rounded-full"
                    :class="
                      s.status.value?.app?.config_ok
                        ? 'bg-emerald-500 animate-pulse'
                        : 'bg-amber-500'
                    "
                  ></span>
                  {{
                    s.status.value?.app?.config_ok
                      ? t("dashboard.status_ok")
                      : t("dashboard.status_check")
                  }}
                </span>
                <span
                  v-if="s.status.value?.circuit?.state === 'open'"
                  class="inline-flex items-center gap-1 px-2.5 py-0.5 rounded-full text-xs font-medium bg-destructive/10 text-destructive border border-destructive/20"
                >
                  {{ t("dashboard.circuit_open") }}
                </span>
                <span
                  v-else
                  class="text-xs text-muted-foreground hidden sm:inline"
                >
                  {{ t("dashboard.circuit_normal") }}
                </span>
              </div>
              <h1 class="text-2xl font-bold tracking-tight text-foreground">
                {{ t("dashboard.title") }}
              </h1>
              <p class="text-sm text-muted-foreground mt-1">
                {{ t("dashboard.desc") }}
              </p>
            </div>
          </div>

          <div class="metrics">
            <Card class="shadow-xs hover:shadow-sm transition-shadow">
              <CardHeader
                class="flex flex-row items-center justify-between pb-2 space-y-0"
              >
                <CardTitle
                  class="text-xs font-medium uppercase tracking-wider text-muted-foreground"
                  >{{ t("dashboard.upstream_site") }}</CardTitle
                >
                <CardAction
                  ><Globe class="size-4 text-muted-foreground"
                /></CardAction>
              </CardHeader>
              <CardContent>
                <div
                  class="text-2xl font-bold tracking-tight text-foreground capitalize"
                >
                  {{ s.status.value?.site?.site || "—" }}
                </div>
                <div
                  class="flex items-center gap-1.5 mt-1.5 text-xs text-muted-foreground"
                >
                  <span
                    class="inline-block size-1.5 rounded-full bg-emerald-500"
                  ></span>
                  <span class="truncate font-mono">{{
                    s.status.value?.site?.host || "e-hentai.org"
                  }}</span>
                </div>
              </CardContent>
            </Card>

            <Card class="shadow-xs hover:shadow-sm transition-shadow">
              <CardHeader
                class="flex flex-row items-center justify-between pb-2 space-y-0"
              >
                <CardTitle
                  class="text-xs font-medium uppercase tracking-wider text-muted-foreground"
                  >{{ t("dashboard.outbound_requests") }}</CardTitle
                >
                <CardAction
                  ><Activity class="size-4 text-muted-foreground"
                /></CardAction>
              </CardHeader>
              <CardContent>
                <div
                  class="text-2xl font-bold tracking-tight font-mono text-foreground"
                >
                  {{
                    Number(s.status.value?.throttle?.html_requests || 0) +
                    Number(s.status.value?.throttle?.api_requests || 0) +
                    Number(s.status.value?.throttle?.image_requests || 0)
                  }}
                </div>
                <div
                  class="flex items-center gap-1.5 mt-1.5 text-xs text-muted-foreground font-mono"
                >
                  <span
                    >HTML
                    {{ s.status.value?.throttle?.html_requests || 0 }}</span
                  >
                  <span>·</span>
                  <span
                    >API {{ s.status.value?.throttle?.api_requests || 0 }}</span
                  >
                  <span>·</span>
                  <span
                    >{{ t("dashboard.image_req") }}
                    {{ s.status.value?.throttle?.image_requests || 0 }}</span
                  >
                </div>
              </CardContent>
            </Card>

            <Card class="shadow-xs hover:shadow-sm transition-shadow">
              <CardHeader
                class="flex flex-row items-center justify-between pb-2 space-y-0"
              >
                <CardTitle
                  class="text-xs font-medium uppercase tracking-wider text-muted-foreground"
                  >{{ t("dashboard.memory_cache") }}</CardTitle
                >
                <CardAction
                  ><Database class="size-4 text-muted-foreground"
                /></CardAction>
              </CardHeader>
              <CardContent>
                <div class="flex items-baseline justify-between">
                  <div
                    class="text-2xl font-bold tracking-tight font-mono text-foreground"
                  >
                    {{ s.status.value?.memory_cache?.size ?? 0 }}
                    <span class="text-xs font-normal text-muted-foreground"
                      >/
                      {{
                        s.status.value?.memory_cache?.max_entries ?? 2000
                      }}</span
                    >
                  </div>
                  <span
                    class="text-xs font-medium text-muted-foreground font-mono"
                  >
                    {{
                      s.status.value?.memory_cache?.max_entries
                        ? Math.round(
                            (Number(s.status.value?.memory_cache?.size || 0) /
                              Number(
                                s.status.value?.memory_cache?.max_entries,
                              )) *
                              100,
                          )
                        : 0
                    }}%
                  </span>
                </div>
                <div
                  class="w-full bg-muted rounded-full h-1.5 mt-2.5 overflow-hidden"
                >
                  <div
                    class="bg-foreground h-full rounded-full transition-all duration-300"
                    :style="{
                      width: `${Math.min(100, s.status.value?.memory_cache?.max_entries ? Math.round((Number(s.status.value?.memory_cache?.size || 0) / Number(s.status.value?.memory_cache?.max_entries)) * 100) : 0)}%`,
                    }"
                  ></div>
                </div>
                <div
                  class="flex items-center justify-between mt-2 text-xs text-muted-foreground font-mono"
                >
                  <span
                    >{{ t("dashboard.hits", { count: s.status.value?.memory_cache?.hits ?? 0 }) }}</span
                  >
                  <span
                    >{{ t("dashboard.misses", { count: s.status.value?.memory_cache?.misses ?? 0 }) }}</span
                  >
                </div>
              </CardContent>
            </Card>

            <Card class="shadow-xs hover:shadow-sm transition-shadow">
              <CardHeader
                class="flex flex-row items-center justify-between pb-2 space-y-0"
              >
                <CardTitle
                  class="text-xs font-medium uppercase tracking-wider text-muted-foreground"
                  >{{ t("dashboard.disk_cache") }}</CardTitle
                >
                <CardAction
                  ><HardDrive class="size-4 text-muted-foreground"
                /></CardAction>
              </CardHeader>
              <CardContent>
                <div class="flex items-baseline justify-between">
                  <div
                    class="text-2xl font-bold tracking-tight text-foreground"
                  >
                    {{
                      s.status.value?.disk_cache?.enabled ? t("dashboard.enabled") : t("dashboard.disabled")
                    }}
                  </div>
                  <Button
                    v-if="s.status.value?.disk_cache?.enabled"
                    variant="ghost"
                    size="xs"
                    class="h-6 px-1.5 text-xs text-muted-foreground hover:text-foreground"
                    @click="s.askClearCache"
                    :title="t('dashboard.clear_cache_title')"
                  >
                    <Trash2 class="size-3 mr-1" />{{ t("dashboard.clear_btn") }}
                  </Button>
                </div>
                <div
                  class="flex items-center justify-between mt-1.5 text-xs text-muted-foreground font-mono"
                >
                  <span
                    >{{ t("dashboard.cache_entries_count", { count: s.status.value?.disk_cache?.entries ?? 0 }) }}</span
                  >
                  <span>{{
                    formatBytes(s.status.value?.disk_cache?.bytes ?? 0)
                  }}</span>
                </div>
              </CardContent>
            </Card>
          </div>

          <div class="columns">
            <Card class="shadow-xs">
              <CardHeader class="border-b border-border/50 pb-3">
                <CardTitle class="text-base font-semibold text-foreground"
                  >{{ t("dashboard.runtime_details") }}</CardTitle
                >
                <CardDescription class="text-xs text-muted-foreground"
                  >{{ t("dashboard.runtime_desc") }}</CardDescription
                >
              </CardHeader>
              <CardContent class="pt-2">
                <div class="divide-y divide-border/50 text-sm">
                  <div class="flex items-center justify-between py-2.5">
                    <span class="text-muted-foreground text-xs font-medium"
                      >{{ t("dashboard.version") }}</span
                    >
                    <Badge variant="secondary" class="font-mono text-xs">
                      v{{ s.status.value?.app?.version || "—" }}
                    </Badge>
                  </div>
                  <div class="flex items-center justify-between py-2.5">
                    <span class="text-muted-foreground text-xs font-medium"
                      >{{ t("dashboard.external_url") }}</span
                    >
                    <span
                      class="font-mono text-xs text-foreground truncate max-w-[240px]"
                    >
                      {{
                        s.status.value?.site?.public_base_url ||
                        t("dashboard.unset_relative")
                      }}
                    </span>
                  </div>
                  <div class="flex items-center justify-between py-2.5">
                    <span class="text-muted-foreground text-xs font-medium"
                      >{{ t("dashboard.ipb_status") }}</span
                    >
                    <Badge
                      :variant="
                        s.status.value?.site?.has_ipb ? 'outline' : 'secondary'
                      "
                      class="text-xs"
                    >
                      {{
                        s.status.value?.site?.has_ipb
                          ? t("dashboard.ipb_configured")
                          : t("dashboard.ipb_unconfigured")
                      }}
                    </Badge>
                  </div>
                  <div class="flex items-center justify-between py-2.5">
                    <span class="text-muted-foreground text-xs font-medium"
                      >{{ t("dashboard.max_concurrency") }}</span
                    >
                    <span class="font-mono text-xs font-medium text-foreground">
                      {{ s.status.value?.throttle?.max_concurrency != null ? t("dashboard.threads", { count: s.status.value.throttle.max_concurrency }) : "—" }}
                    </span>
                  </div>
                  <div class="flex items-center justify-between py-2.5">
                    <span class="text-muted-foreground text-xs font-medium"
                      >{{ t("dashboard.html_interval") }}</span
                    >
                    <span class="font-mono text-xs font-medium text-foreground">
                      {{
                        s.status.value?.throttle?.html_interval_seconds != null ? t("dashboard.seconds_unit", { count: s.status.value.throttle.html_interval_seconds }) : "—"
                      }}
                    </span>
                  </div>
                  <div
                    v-if="s.status.value?.circuit?.state === 'open'"
                    class="flex items-center justify-between py-2.5 text-destructive"
                  >
                    <span class="text-xs font-medium">{{ t("dashboard.circuit_reason") }}</span>
                    <span class="text-xs font-mono">{{
                      s.status.value?.circuit?.reason
                    }}</span>
                  </div>
                </div>
              </CardContent>
            </Card>

            <Card class="shadow-xs">
              <CardHeader class="border-b border-border/50 pb-3">
                <div class="flex items-center justify-between">
                  <div>
                    <CardTitle class="text-base font-semibold text-foreground"
                      >{{ t("dashboard.home_layout") }}</CardTitle
                    >
                    <CardDescription class="text-xs text-muted-foreground"
                      >{{ t("dashboard.home_layout_desc") }}</CardDescription
                    >
                  </div>
                  <Button
                    variant="ghost"
                    size="xs"
                    class="h-7 text-xs gap-1"
                    @click="s.route('home')"
                  >
                    {{ t("dashboard.enter_editor") }} <ArrowUpRight :size="12" />
                  </Button>
                </div>
              </CardHeader>
              <CardContent class="pt-2">
                <div class="divide-y divide-border/50 text-sm">
                  <div class="flex items-center justify-between py-2.5">
                    <span class="text-muted-foreground text-xs font-medium"
                      >{{ t("dashboard.content_source") }}</span
                    >
                    <Badge variant="outline" class="font-mono text-xs">
                      {{
                        s.status.value?.home?.using_file
                          ? "home.toml"
                          : t("dashboard.builtin_default")
                      }}
                    </Badge>
                  </div>
                  <div class="flex items-center justify-between py-2.5">
                    <span class="text-muted-foreground text-xs font-medium"
                      >{{ t("dashboard.config_path") }}</span
                    >
                    <span
                      class="font-mono text-xs text-muted-foreground truncate max-w-[260px]"
                      :title="s.status.value?.home?.path"
                    >
                      {{ s.status.value?.home?.path || "—" }}
                    </span>
                  </div>
                  <div class="flex items-center justify-between py-2.5">
                    <span class="text-muted-foreground text-xs font-medium"
                      >{{ t("dashboard.groups_sections") }}</span
                    >
                    <span class="font-mono text-xs font-medium text-foreground">
                      {{ t("dashboard.groups_sections_count", { groups: s.status.value?.home?.groups ?? 0, sections: s.status.value?.home?.sections ?? 0 }) }}
                    </span>
                  </div>
                  <div class="flex items-center justify-between py-2.5">
                    <span class="text-muted-foreground text-xs font-medium"
                      >{{ t("dashboard.opds_versions") }}</span
                    >
                    <div class="flex items-center gap-1.5">
                      <Badge variant="secondary" class="text-[10px] font-mono"
                        >v1.2</Badge
                      >
                      <Badge variant="secondary" class="text-[10px] font-mono"
                        >v2.0</Badge
                      >
                      <Badge variant="secondary" class="text-[10px] font-mono"
                        >OPDS-PSE</Badge
                      >
                    </div>
                  </div>
                  <div class="flex items-center justify-between py-2.5">
                    <span class="text-muted-foreground text-xs font-medium"
                      >{{ t("dashboard.heartbeat") }}</span
                    >
                    <span
                      class="inline-flex items-center gap-1.5 text-xs text-muted-foreground"
                    >
                      <span class="size-1.5 rounded-full bg-emerald-500"></span>
                      {{ t("dashboard.active") }}
                    </span>
                  </div>
                </div>
              </CardContent>
            </Card>
          </div>
        </template>
        <template v-if="s.page.value === 'home'">
          <HomeEditor />
        </template>
        <template v-if="s.page.value === 'explore'">
          <OpdsHome
            @read-gallery="openReader"
            @archive-gallery="handleExploreArchive"
            @favorite-gallery="handleExploreFavorite"
          />
        </template>
        <template v-if="s.page.value === 'config'"
          ><div class="heading">
            <div>
              <small>{{ t("config.header_small") }}</small>
              <h1>{{ t("config.title") }}</h1>
              <p>{{ t("config.desc") }}</p>
            </div>
            <Badge
              v-if="s.config.value?.pending"
              variant="outline"
              class="badge-warn"
              >{{ t("config.badge_pending") }}</Badge
            >
          </div>
          <Card v-if="s.config.value?.legacy" class="panel">
            <CardHeader class="panel-header">
              <CardTitle role="heading" aria-level="2"
                >{{ t("config.readonly_title") }}</CardTitle
              >
            </CardHeader>
            <CardContent class="panel-body">
              <p v-if="s.config.value.legacy.error" class="archive-error">
                {{ s.config.value.legacy.error }}
              </p>
              <div
                v-for="group in s.config.value.legacy.groups || []"
                :key="group.id"
                class="legacy-group"
              >
                <h3>{{ group.title }}</h3>
                <div
                  v-for="field in group.fields || []"
                  :key="field.key"
                  class="legacy-row"
                >
                  <b>{{ field.label }}</b
                  ><span>{{
                    field.masked
                      ? field.set
                        ? "••••••••（" + t("config.state_set") + "）"
                        : t("config.state_unset_parentheses")
                      : (field.value ?? t("config.state_unset_parentheses"))
                  }}</span
                  ><small v-if="field.note">{{ field.note }}</small>
                </div>
              </div>
              <div v-if="s.config.value.legacy.derived" class="data-list">
                <div
                  v-for="(value, key) in s.config.value.legacy.derived"
                  :key="key"
                >
                  <span>{{ key }}</span
                  ><b>{{ value }}</b>
                </div>
              </div>
            </CardContent>
          </Card>
          <Card
            v-for="group in groups"
            :key="group.title"
            class="panel form-panel"
          >
            <CardHeader class="panel-header">
              <div class="panel-title">
                <CardTitle role="heading" aria-level="2">{{
                  group.title
                }}</CardTitle>
                <CardDescription
                  >{{ t("config.saved_pending_restart") }}</CardDescription
                >
              </div>
            </CardHeader>
            <CardContent class="form-panel-content">
              <div v-for="[key, f] in group.fields" :key="key" class="field">
                <div>
                  <label :for="key"
                    >{{ labels[key] || key }}
                    <Badge v-if="f.pending" variant="outline" class="badge-warn"
                      >{{ t("config.pending_restart") }}</Badge
                    ></label
                  ><small>{{
                    f.locked
                      ? lockedReason(key)
                      : (f.source === "env"
                          ? t("config.source_env")
                          : f.source === "file"
                            ? t("config.source_file")
                            : t("config.source_default"))
                  }}</small>
                  <small v-if="key === 'archive_update_interval_days'"
                    >{{ t("config.hint_archive_update_interval_days") }}</small
                  >
                  <small v-if="key === 'archive_auto_update'"
                    >{{ t("config.hint_archive_auto_update") }}</small
                  >
                  <small v-if="key === 'favorites_sync_interval_seconds'"
                    >{{ t("config.hint_favorites_sync_interval_seconds") }}</small
                  >
                  <small v-if="key === 'favorites_sync_categories'"
                    >{{ t("config.hint_favorites_sync_categories") }}</small
                  >
                  <small v-if="key === 'favorites_sync_archive'"
                    >{{ t("config.hint_favorites_sync_archive") }}</small
                  >
                </div>
                <div class="control">
                  <label v-if="boolean(key)" class="switch"
                    ><input
                      :id="key"
                      type="checkbox"
                      :checked="Boolean(s.form.value[key])"
                      :disabled="f.locked"
                      @change="
                        edit(key, $event.target.checked)
                      " /><i></i></label
                  ><Select
                    v-if="key === 'eh_site'"
                    :model-value="String(s.form.value[key] ?? '')"
                    :disabled="f.locked"
                    @update:model-value="edit(key, $event)"
                  >
                    <SelectTrigger :id="key" class="control-select">
                      <SelectValue :placeholder="t('config.select_site')" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="e-hentai">E-Hentai</SelectItem>
                      <SelectItem value="exhentai">ExHentai</SelectItem>
                    </SelectContent>
                  </Select>
                  <Select
                    v-if="key === 'pse_page_base'"
                    :model-value="String(s.form.value[key] ?? '')"
                    :disabled="f.locked"
                    @update:model-value="edit(key, Number($event))"
                  >
                    <SelectTrigger :id="key" class="control-select">
                      <SelectValue :placeholder="t('config.select_pse_page_base')" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="1">{{ t("config.pse_page_base_1") }}</SelectItem>
                      <SelectItem value="0">{{ t("config.pse_page_base_0") }}</SelectItem>
                    </SelectContent>
                  </Select>
                  <Select
                    v-if="key === 'tag_status_filter'"
                    :model-value="String(s.form.value[key] ?? '')"
                    :disabled="f.locked"
                    @update:model-value="edit(key, $event)"
                  >
                    <SelectTrigger :id="key" class="control-select">
                      <SelectValue :placeholder="t('config.select_tag_filter')" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="balanced">{{ t("config.tag_filter_balanced") }}</SelectItem>
                      <SelectItem value="strict">{{ t("config.tag_filter_strict") }}</SelectItem>
                      <SelectItem value="off">{{ t("config.tag_filter_off") }}</SelectItem>
                    </SelectContent>
                  </Select>
                  <Input
                    v-if="
                      !boolean(key) &&
                      ![
                        'eh_site',
                        'pse_page_base',
                        'tag_status_filter',
                      ].includes(key)
                    "
                    :id="key"
                    :type="
                      secret(key)
                        ? 'password'
                        : key === 'cache_max_gb' ||
                            key === 'archive_update_interval_days' ||
                            key === 'favorites_sync_interval_seconds'
                          ? 'number'
                          : 'text'
                    "
                    :min="
                      [
                        'archive_update_interval_days',
                        'favorites_sync_interval_seconds',
                      ].includes(key)
                        ? 0
                        : undefined
                    "
                    :step="
                      key === 'archive_update_interval_days'
                        ? 1
                        : key === 'favorites_sync_interval_seconds'
                          ? 'any'
                          : undefined
                    "
                    :model-value="s.form.value[key] ?? ''"
                    :placeholder="
                      secret(key)
                        ? f.saved_set
                          ? t('config.placeholder_set_leave_blank')
                          : t('config.placeholder_unset')
                        : ''
                    "
                    :disabled="f.locked"
                    class="control-input"
                    autocomplete="new-password"
                    @update:model-value="edit(key, $event)"
                  /><Badge v-if="f.locked" variant="outline" class="locked"
                    >{{ t("config.locked") }}</Badge
                  ><Button
                    v-if="key === 'ipb_member_id'"
                    :disabled="f.locked"
                    variant="link"
                    size="sm"
                    @click="
                      edit('ipb_member_id', '');
                      edit('ipb_pass_hash', '');
                    "
                  >
                    {{ t("config.clear_pair_credentials") }}</Button
                  ><Button
                    v-if="key === 'igneous'"
                    :disabled="f.locked"
                    variant="link"
                    size="sm"
                    @click="edit('igneous', '')"
                  >
                    {{ t("common.clear") }}
                  </Button>
                </div>
                <small class="active-value">
                  {{
                    t("config.active_val", {
                      val: secret(key)
                        ? (f.set ? t("config.state_set") : t("config.state_unset"))
                        : (f.active ?? t("config.state_unset_parentheses")),
                    })
                  }}
                  ·
                  {{
                    t("config.saved_val", {
                      val: secret(key)
                        ? (f.saved_set ? t("config.state_set") : t("config.state_unset"))
                        : (f.saved ?? t("config.state_unset_parentheses")),
                    })
                  }}
                </small>
              </div>
            </CardContent>
          </Card>
          <div v-if="s.config.value?.fields" class="savebar">
            <span>{{
              Object.keys(s.confDirty.value).length
                ? t("config.savebar_dirty")
                : s.config.value?.pending
                  ? t("config.savebar_pending")
                  : t("config.savebar_synced")
            }}</span
            ><Button
              :disabled="!Object.keys(s.confDirty.value).length || s.busy.value"
              @click="s.saveConfig"
              ><Save />{{ t("config.save_button") }}</Button
            >
          </div></template
        >

        <template v-if="s.page.value === 'archive'">
          <div class="heading">
            <div>
              <small>{{ t("archive.header_small") }}</small>
              <h1>{{ t("archive.title") }}</h1>
              <p>{{ t("archive.desc") }}</p>
            </div>
            <div class="heading-actions">
              <div ref="syncMenuRef" class="relative inline-flex items-center">
                <div class="inline-flex items-center rounded-md">
                  <Button
                    variant="outline"
                    class="rounded-r-none border-r-0"
                    :disabled="!s.status.value?.site?.has_ipb || s.busy.value"
                    @click="s.syncFavorites(false)"
                  >
                    <RefreshCw class="size-3.5" />{{ t("archive.sync_favorites") }}
                  </Button>
                  <Button
                    variant="outline"
                    size="icon"
                    class="rounded-l-none w-8 px-0"
                    :disabled="!s.status.value?.site?.has_ipb || s.busy.value"
                    :aria-label="t('archive.more_sync_options')"
                    aria-haspopup="menu"
                    :aria-expanded="syncMenuOpen"
                    @click.stop="syncMenuOpen = !syncMenuOpen"
                  >
                    <ChevronDown class="size-3.5" />
                  </Button>
                </div>
                <div
                  v-if="syncMenuOpen"
                  role="menu"
                  class="absolute right-0 top-full z-50 mt-1.5 min-w-52 rounded-md border border-border bg-popover p-1 text-popover-foreground shadow-md"
                >
                  <button
                    type="button"
                    role="menuitem"
                    class="flex w-full flex-col items-start rounded-sm px-2.5 py-2 text-left text-xs hover:bg-accent hover:text-accent-foreground transition-colors"
                    @click="requestFullSync"
                  >
                    <span class="font-medium text-foreground"
                      >{{ t("archive.full_sync_title") }}</span
                    >
                    <span class="text-[11px] text-muted-foreground mt-0.5"
                      >{{ t("archive.full_sync_desc") }}</span
                    >
                  </button>
                </div>
              </div>
              <Button
                variant="outline"
                :disabled="s.archiveScan.value.status === 'running'"
                @click="s.startArchiveScan"
                :title="t('archive.scan_imports')"
              >
                {{
                  s.archiveScan.value.status === "running"
                    ? t("archive.importing")
                    : t("archive.import_local")
                }}
              </Button>
              <Button class="primary-action" @click="openCreateArchiveDialog">
                <Plus />{{ t("archive.new_archive") }}
              </Button>
            </div>
          </div>
          <div
            v-if="showArchiveScanBanner"
            class="archive-scan-status"
            :class="'archive-scan-' + s.archiveScan.value.status"
            role="status"
          >
            <span v-if="s.archiveScan.value.status === 'running'"
              >{{ t("archive.scan_searching") }}</span
            >
            <span v-else-if="s.archiveScan.value.status === 'success'"
              >{{
                t("archive.scan_success", {
                  imported: s.archiveScan.value.imported,
                  skipped: s.archiveScan.value.skipped,
                  conflicts: s.archiveScan.value.conflicts,
                })
              }}</span
            >
            <span v-else-if="s.archiveScan.value.status === 'skipped'"
              >{{
                t("archive.scan_skipped", {
                  skipped: s.archiveScan.value.skipped,
                })
              }}</span
            >
            <span v-else-if="s.archiveScan.value.status === 'conflict'"
              >{{
                t("archive.scan_conflict", {
                  conflicts: s.archiveScan.value.conflicts,
                })
              }}</span
            >
            <span v-else
              >{{
                t("archive.scan_error", {
                  err: s.archiveScan.value.error || t("common.unknown"),
                })
              }}</span
            >
            <Button
              v-if="s.archiveScan.value.status !== 'running'"
              variant="ghost"
              size="icon-xs"
              :aria-label="t('archive.close_import_hint')"
              @click="s.dismissArchiveScan()"
              ><X :size="14"
            /></Button>
          </div>
          <div class="archive-action-bar">
            <!-- 左侧分段器：全部 / 已归档 / 未归档 / 可更新 / 下载中 / 失败 -->
            <div class="segmented-control" role="tablist">
              <button
                type="button"
                role="tab"
                class="segment-btn"
                :class="{ active: selectedStatus === 'all' }"
                @click="selectedStatus = 'all'"
              >
                <span>{{ t("archive.tab_all") }}</span>
              </button>
              <button
                type="button"
                role="tab"
                class="segment-btn"
                :class="{ active: selectedStatus === 'ready' }"
                @click="selectedStatus = 'ready'"
              >
                <span>{{ t("archive.tab_ready") }}</span>
                <span v-if="statusCounts.ready > 0" class="segment-badge">
                  {{ statusCounts.ready }}
                </span>
              </button>
              <button
                type="button"
                role="tab"
                class="segment-btn"
                :class="{ active: selectedStatus === 'unarchived' }"
                @click="selectedStatus = 'unarchived'"
              >
                <span>{{ t("archive.tab_unarchived") }}</span>
                <span v-if="statusCounts.unarchived > 0" class="segment-badge">
                  {{ statusCounts.unarchived }}
                </span>
              </button>
              <button
                type="button"
                role="tab"
                class="segment-btn"
                :class="{ active: selectedStatus === 'updates' }"
                @click="selectedStatus = 'updates'"
              >
                <span>{{ t("archive.tab_updates") }}</span>
                <span
                  v-if="statusCounts.updates > 0"
                  class="segment-badge segment-badge-warn"
                >
                  {{ statusCounts.updates }}
                </span>
              </button>
              <button
                type="button"
                role="tab"
                class="segment-btn"
                :class="{ active: selectedStatus === 'downloading' }"
                @click="selectedStatus = 'downloading'"
              >
                <span>{{ t("archive.tab_downloading") }}</span>
                <span
                  v-if="statusCounts.downloading > 0"
                  class="segment-badge segment-badge-active"
                >
                  {{ statusCounts.downloading }}
                </span>
              </button>
              <button
                type="button"
                role="tab"
                class="segment-btn"
                :class="{ active: selectedStatus === 'failed' }"
                @click="selectedStatus = 'failed'"
              >
                <span>{{ t("archive.tab_failed") }}</span>
                <span
                  v-if="statusCounts.failed > 0"
                  class="segment-badge segment-badge-destructive"
                >
                  {{ statusCounts.failed }}
                </span>
              </button>
            </div>

            <!-- 右侧控件区：搜索 + 2个Menu picker + 新建归档 -->
            <div class="action-bar-right">
              <div class="toolbar-search-box">
                <Search class="size-3.5 search-icon text-muted-foreground" />
                <Input
                  v-model="archiveSearch"
                  :placeholder="t('archive.search_placeholder')"
                  class="toolbar-search-input"
                />
                <button
                  v-if="archiveSearch"
                  type="button"
                  class="search-clear-btn"
                  @click="archiveSearch = ''"
                  :aria-label="t('explore.clear_search')"
                >
                  <X class="size-3" />
                </button>
              </div>

              <!-- Menu Picker 1: 全部分类 (Category) -->
              <Select v-model="selectedCategory">
                <SelectTrigger size="sm" class="menu-picker-trigger">
                  <Tag class="size-3.5 text-muted-foreground shrink-0" />
                  <SelectValue :placeholder="categoryLabel">
                    <span class="menu-picker-label">{{ categoryLabel }}</span>
                  </SelectValue>
                </SelectTrigger>
                <SelectContent position="popper">
                  <SelectItem value="all">
                    <span class="font-medium">{{ t("archive.all_categories") }}</span>
                  </SelectItem>
                  <SelectItem
                    v-for="cat in EH_CATEGORIES"
                    :key="cat.key"
                    :value="cat.key"
                  >
                    <span class="inline-flex items-center gap-1.5">
                      <span
                        class="cat-dot"
                        :style="{ backgroundColor: cat.color }"
                      ></span>
                      <span>{{ t('category.' + cat.key) || cat.label }}</span>
                      <span class="text-xs text-muted-foreground font-mono"
                        >({{ categoryCounts[cat.key] || 0 }})</span
                      >
                    </span>
                  </SelectItem>
                </SelectContent>
              </Select>

              <!-- Menu Picker 2: 全部收藏 (Favcat) -->
              <Select v-model="selectedFavcat">
                <SelectTrigger size="sm" class="menu-picker-trigger">
                  <Bookmark class="size-3.5 text-muted-foreground shrink-0" />
                  <SelectValue :placeholder="favcatLabel">
                    <span class="menu-picker-label">{{ favcatLabel }}</span>
                  </SelectValue>
                </SelectTrigger>
                <SelectContent position="popper">
                  <SelectItem value="all">
                    <span class="font-medium">{{ t("archive.all_favorites") }}</span>
                  </SelectItem>
                  <SelectItem value="unfavorited">
                    <span class="inline-flex items-center gap-1.5">
                      <span>{{ t("common.unfavorited") }}</span>
                      <span class="text-xs text-muted-foreground font-mono"
                        >({{ unfavoritedCount }})</span
                      >
                    </span>
                  </SelectItem>
                  <SelectItem value="favorited">
                    <span class="inline-flex items-center gap-1.5">
                      <span>{{ t("common.favorited") }}</span>
                      <span class="text-xs text-muted-foreground font-mono"
                        >({{ favoritedCount }})</span
                      >
                    </span>
                  </SelectItem>
                  <SelectSeparator class="my-1" />
                  <SelectItem
                    v-for="cat in favcatList"
                    :key="cat.id"
                    :value="String(cat.id)"
                  >
                    <span class="inline-flex items-center gap-1.5">
                      <span>{{ cat.id }}: {{ cat.name }}</span>
                      <span class="text-xs text-muted-foreground font-mono"
                        >({{ cat.count }})</span
                      >
                    </span>
                  </SelectItem>
                </SelectContent>
              </Select>

              <Button
                v-if="isFiltered"
                variant="ghost"
                size="xs"
                class="h-8 text-xs text-muted-foreground hover:text-foreground px-2"
                @click="resetFilters"
              >
                {{ t("common.reset") }}
              </Button>
            </div>
          </div>
          <div class="section-title">
            <h2>{{ t("archive.browse_title") }}</h2>
            <span>{{
              isFiltered
                ? t("archive.showing_items", {
                    shown: filteredArchives.length,
                    total: s.archives.value.length,
                  })
                : t("archive.items_count", {
                    count: s.archives.value.length,
                  })
            }}</span>
          </div>
          <div v-if="!s.archives.value.length" class="panel empty">
            {{ t("archive.empty_archives") }}
          </div>
          <div v-else-if="!filteredArchives.length" class="panel empty">
            <p>{{ t("archive.empty_filtered") }}</p>
            <Button variant="link" size="sm" @click="resetFilters"
              >{{ t("archive.clear_filters") }}</Button
            >
          </div>
          <div v-else class="gallery">
            <Card
              v-for="a in visibleArchives"
              :key="a.gid + '/' + a.token"
              class="gallery-card"
            >
              <div class="cover">
                <CoverImage
                  v-if="a.cover_url"
                  :src="a.cover_url"
                  :alt="a.title || t('archive.cover')"
                /><span v-else>{{ t("archive.no_cover") }}</span
                ><b
                  :title="
                    a.metadata?.title_jpn ||
                    a.metadata?.title ||
                    a.title ||
                    t('archive.unnamed_gallery')
                  "
                  ><span class="line-clamp-2 break-words">{{
                    a.metadata?.title_jpn ||
                    a.metadata?.title ||
                    a.title ||
                    t("archive.unnamed_gallery")
                  }}</span></b
                ><Badge variant="secondary" class="cover-status">{{
                  t("archive.status_map." + a.status) ||
                  a.status ||
                  t("common.unknown")
                }}</Badge>
              </div>
              <div class="gallery-meta">
                {{ a.metadata?.category || "—" }} ·
                {{ qualityLabel(a) }}
                <span>{{
                  a.metadata?.rating ? "★ " + a.metadata.rating : ""
                }}</span>
              </div>
              <div class="gallery-meta items-center">
                <span class="shrink-0 whitespace-nowrap"
                  >{{
                    a.metadata?.filecount || a.page_count || "—"
                  }}
                  {{ t("common.pages") }}<template v-if="archiveSizeText(a)"
                    >（{{ archiveSizeText(a) }}）</template
                  ></span
                >
                <Select
                  :model-value="a.favcat != null ? String(a.favcat) : ''"
                  @update:model-value="
                    (val) => {
                      if (val === 'remove') {
                        s.requestFavoriteRemove(a);
                      } else {
                        s.favorite(
                          a,
                          a.is_favorited ? 'move' : 'add',
                          Number(val),
                        );
                      }
                    }
                  "
                >
                  <SelectTrigger
                    size="sm"
                    :class="[
                      'favorite-chip',
                      a.is_favorited && a.favcat != null
                        ? 'favorite-chip--active'
                        : 'favorite-chip--empty',
                    ]"
                    :title="
                      a.favcat != null
                        ? a.favcat_name || t('archive.favcat_default', { id: a.favcat })
                        : t('archive.add_to_fav')
                    "
                  >
                    <Bookmark class="size-3 shrink-0" />
                    <SelectValue :placeholder="t('common.unfavorited')">
                      <span class="truncate">{{
                        a.favcat != null
                          ? a.favcat_name || t("archive.favcat_default", { id: a.favcat })
                          : t("common.unfavorited")
                      }}</span>
                    </SelectValue>
                  </SelectTrigger>
                  <SelectContent position="popper">
                    <SelectItem
                      v-for="cat in favcatList"
                      :key="cat.id"
                      :value="String(cat.id)"
                    >
                      {{ cat.id }}: {{ cat.name }}
                    </SelectItem>
                    <template v-if="a.is_favorited">
                      <SelectSeparator class="my-1" />
                      <SelectItem
                        value="remove"
                        class="text-destructive focus:bg-destructive/10 focus:text-destructive cursor-pointer"
                      >
                        {{ t("archive.remove_fav") }}
                      </SelectItem>
                    </template>
                  </SelectContent>
                </Select>
              </div>
              <div class="gallery-actions">
                <Button as-child variant="link" size="sm">
                  <a
                    :href="
                      'https://e-hentai.org/g/' +
                      (a.active_gid || a.gid) +
                      '/' +
                      (a.active_token || a.token) +
                      '/'
                    "
                    target="_blank"
                    rel="noopener noreferrer"
                    >{{ t("archive.open_upstream") }}</a
                  >
                </Button>
                <Button variant="link" size="sm" @click="s.refreshMetadata(a)"
                  >{{ t("archive.update_meta") }}</Button
                ><Button
                  variant="link"
                  size="sm"
                  v-if="a.update_available"
                  :disabled="
                    s.busy.value ||
                    !s.status.value?.site?.has_ipb ||
                    [
                      'submitting',
                      'pending',
                      'downloading',
                      'zipping',
                    ].includes(a.version_update?.status)
                  "
                  @click="handleQuoteUpdate(a)"
                  >{{ t("archive.update_version") }}</Button
                ><Button
                  v-if="a.status === 'unarchived' && a.is_favorited"
                  variant="link"
                  size="sm"
                  :disabled="!s.status.value?.site?.has_ipb || s.busy.value"
                  @click="handleQuoteArchive(a)"
                  >{{ cardActionLabel(a) }}</Button
                ><Button
                  v-else
                  variant="destructive"
                  size="sm"
                  @click="s.requestDelete(a)"
                  >{{ cardActionLabel(a) }}</Button
                >
              </div>
              <div
                v-if="
                  (a.status === 'ready' ||
                    a.status === 'unarchived' ||
                    a.status === 'failed') &&
                  (a.status !== 'unarchived' || archivePageCount(a) > 0)
                "
                class="gallery-primary-action"
              >
                <Button
                  v-if="a.status === 'ready'"
                  variant="default"
                  size="sm"
                  class="w-full text-xs"
                  :disabled="archivePageCount(a) <= 0"
                  @click="openReader(a)"
                >
                  <BookOpen class="size-3.5" />{{ t("archive.read_btn") }}
                </Button>
                <Button
                  v-else-if="a.status === 'unarchived'"
                  variant="default"
                  size="sm"
                  class="w-full text-xs"
                  @click="openReader(a)"
                >
                  <BookOpen class="size-3.5" />{{ t("common.read_online") }}
                </Button>
                <div v-else class="flex items-center gap-1.5 w-full">
                  <Button
                    v-if="archivePageCount(a) > 0"
                    variant="default"
                    size="sm"
                    class="flex-1 text-xs"
                    @click="openReader(a)"
                  >
                    <BookOpen class="size-3.5" />{{ t("common.read_online") }}
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    class="flex-1 text-xs"
                    :disabled="!s.status.value?.site?.has_ipb || s.busy.value"
                    @click="handleQuoteArchive(a)"
                  >
                    <RotateCcw class="size-3.5" />{{ t("archive.quote_retry") }}
                  </Button>
                </div>
              </div>
              <small
                v-if="a.error || a.version_update?.error"
                class="archive-error"
              >
                {{ a.error || a.version_update.error }}
              </small>
            </Card>
          </div>
          <div
            v-if="filteredArchives.length > visibleArchives.length"
            class="flex justify-center pt-4"
          >
            <Button variant="outline" size="sm" @click="visibleLimit += 60">
              {{
                t("archive.load_more_btn", {
                  shown: visibleArchives.length,
                  total: filteredArchives.length,
                })
              }}
            </Button>
          </div></template
        >
      </div>
      <AlertDialog v-model:open="s.confirmArchive.value">
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{{
              s.quoteAction.value === "update"
                ? t("archive.dialog_confirm_update_title")
                : t("archive.dialog_confirm_archive_title")
            }}</AlertDialogTitle>
            <AlertDialogDescription>
              {{
                s.quoteAction.value === "update"
                  ? t("archive.dialog_desc_update")
                  : s.quoteAction.value === "refresh"
                    ? t("archive.dialog_desc_retry")
                    : t("archive.dialog_desc_start")
              }}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div v-if="s.quote.value" class="dialog-detail">
            <b>{{ s.quote.value.title || t("archive.selected_gallery") }}</b>
            <span>
              {{
                s.quote.value.options?.find(
                  (o) => String(o.or) === String(s.selectedQuality.value),
                )?.label
              }}
              ·
              {{
                s.quote.value.options?.find(
                  (o) => String(o.or) === String(s.selectedQuality.value),
                )?.gp_price || 0
              }}
              GP
            </span>
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel as-child
              ><Button variant="outline">{{ t("common.cancel") }}</Button></AlertDialogCancel
            >
            <AlertDialogAction as-child
              ><Button :disabled="s.busy.value" @click="s.startArchive">{{
                s.quoteAction.value === "update"
                  ? t("archive.dialog_confirm_update_btn")
                  : t("archive.dialog_confirm_archive_title")
              }}</Button></AlertDialogAction
            >
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <AlertDialog v-model:open="s.confirmCache.value">
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{{ t("archive.dialog_clear_cache_title") }}</AlertDialogTitle>
            <AlertDialogDescription>
              {{ t("archive.dialog_clear_cache_desc") }}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel as-child
              ><Button variant="outline">{{ t("common.cancel") }}</Button></AlertDialogCancel
            >
            <AlertDialogAction as-child
              ><Button variant="destructive" @click="s.clearCache"
                >{{ t("dashboard.clear_cache") }}</Button
              ></AlertDialogAction
            >
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <AlertDialog v-model:open="s.confirmDelete.value">
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{{ deleteDialogMeta.title }}</AlertDialogTitle>
            <AlertDialogDescription>
              {{ deleteDialogMeta.desc }}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div class="dialog-detail">
            <b>{{
              s.deleteTarget.value?.metadata?.title_jpn ||
              s.deleteTarget.value?.title ||
              t("archive.gallery_id", { id: s.deleteTarget.value?.gid || "" })
            }}</b>
            <span>{{ deleteDialogMeta.note }}</span>
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel as-child
              ><Button variant="outline">{{ t("common.cancel") }}</Button></AlertDialogCancel
            >
            <AlertDialogAction as-child
              ><Button variant="destructive" @click="s.deleteArchive">{{
                deleteDialogMeta.confirm
              }}</Button></AlertDialogAction
            >
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <AlertDialog v-model:open="s.confirmFavoriteRemove.value">
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{{ t("archive.dialog_fav_remove_title") }}</AlertDialogTitle>
            <AlertDialogDescription>
              {{
                s.favoriteRemoveTarget.value?.status === "unarchived"
                  ? t("archive.dialog_fav_remove_desc_unarchived")
                  : t("archive.dialog_fav_remove_desc_default")
              }}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <label
            v-if="s.favoriteRemoveTarget.value?.status !== 'unarchived'"
            class="delete-local-check"
          >
            <input
              type="checkbox"
              v-model="s.deleteAfterFavoriteRemove.value"
            />
            {{ t("archive.dialog_fav_remove_also_delete") }}
          </label>
          <AlertDialogFooter>
            <AlertDialogCancel as-child
              ><Button variant="outline">{{ t("common.cancel") }}</Button></AlertDialogCancel
            >
            <AlertDialogAction as-child
              ><Button variant="destructive" @click="s.confirmFavoriteRemoval"
                >{{ t("archive.remove_fav") }}</Button
              ></AlertDialogAction
            >
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <AlertDialog v-model:open="confirmFullSyncOpen">
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{{ t("archive.dialog_full_sync_title") }}</AlertDialogTitle>
            <AlertDialogDescription>
              {{ t("archive.dialog_full_sync_desc") }}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel as-child
              ><Button variant="outline">{{ t("common.cancel") }}</Button></AlertDialogCancel
            >
            <AlertDialogAction as-child
              ><Button :disabled="s.busy.value" @click="confirmAndRunFullSync"
                >{{ t("archive.dialog_full_sync_confirm") }}</Button
              ></AlertDialogAction
            >
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <AlertDialog v-model:open="createArchiveDialogOpen">
        <AlertDialogContent class="max-w-md">
          <AlertDialogHeader>
            <AlertDialogTitle>{{ t("archive.dialog_new_title") }}</AlertDialogTitle>
            <AlertDialogDescription>
              {{ t("archive.dialog_new_desc") }}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div class="space-y-3 my-2 text-left">
            <div v-if="!s.status.value?.site?.has_ipb" class="inline-alert">
              <Alert variant="destructive">
                <CircleAlert class="size-4" />
                <AlertDescription class="text-xs">
                  {{ t("archive.dialog_new_no_ipb") }}
                </AlertDescription>
              </Alert>
            </div>
            <div class="flex gap-2">
              <Input
                v-model="s.archiveInput.value"
                :placeholder="t('archive.dialog_new_placeholder')"
                @keydown.enter.prevent="s.getQuote('start')"
                class="text-xs flex-1"
              />
              <Button
                variant="outline"
                size="sm"
                :disabled="
                  !s.status.value?.site?.has_ipb ||
                  s.busy.value ||
                  !s.archiveInput.value.trim()
                "
                @click="s.getQuote('start')"
              >
                <Search class="size-3.5" />{{ t("archive.dialog_new_query") }}
              </Button>
            </div>
            <div
              v-if="s.quote.value"
              class="rounded-md border border-border bg-muted/40 p-3 space-y-2"
            >
              <div class="font-medium text-foreground text-xs line-clamp-2">
                {{ s.quote.value.title || t("archive.dialog_new_quote_ready") }}
              </div>
              <div
                v-if="s.quote.value.gp_balance"
                class="text-muted-foreground text-[11px]"
              >
                {{
                  t("archive.dialog_new_balance", {
                    balance: s.quote.value.gp_balance.toLocaleString(),
                  })
                }}
              </div>
              <div class="flex items-center gap-2 pt-1">
                <label class="text-xs text-muted-foreground shrink-0"
                  >{{ t("archive.dialog_new_quality_label") }}</label
                >
                <Select v-model="s.selectedQuality.value">
                  <SelectTrigger class="h-8 text-xs flex-1">
                    <SelectValue
                      :placeholder="t('archive.dialog_new_quality_placeholder')"
                    />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem
                      v-for="o in (s.quote.value.options || []).filter(
                        (x) => x.available,
                      )"
                      :key="o.or"
                      :value="o.or"
                    >
                      {{ o.label
                      }}{{ o.unlocked ? t("archive.dialog_new_unlocked") : "" }}
                      ·
                      {{
                        o.gp_price
                          ? o.gp_price.toLocaleString() + " GP"
                          : "Free"
                      }}{{ o.size ? " · " + o.size : "" }}
                    </SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <p class="text-[11px] text-amber-600 dark:text-amber-400">
                {{ t("archive.dialog_new_warning") }}
              </p>
            </div>
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel as-child>
              <Button
                variant="outline"
                size="sm"
                @click="closeCreateArchiveDialog"
                >{{ t("common.cancel") }}</Button
              >
            </AlertDialogCancel>
            <AlertDialogAction as-child>
              <Button
                size="sm"
                :disabled="
                  !s.quote.value || !s.selectedQuality.value || s.busy.value
                "
                @click="confirmAndStartArchive"
              >
                {{ t("archive.dialog_new_confirm") }}
              </Button>
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <ArchiveReader
        v-model:open="readerOpen"
        :archive="readerArchive"
        :pse-page-base="psePageBase"
        :theme="s.theme.value"
      />
    </SidebarInset>
  </SidebarProvider>
</template>

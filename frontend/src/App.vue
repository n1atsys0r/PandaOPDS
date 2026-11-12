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
import AppSidebarNav from "@/components/AppSidebarNav.vue";
import ArchiveReader from "@/components/ArchiveReader.vue";
import CoverImage from "@/components/CoverImage.vue";
import LoginForm from "@/components/LoginForm.vue";
import ThemeAutoIcon from "@/components/ThemeAutoIcon.vue";
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
  GripVerticalIcon as GripVertical,
  CircleCheckIcon as CircleCheck,
  CircleAlertIcon as CircleAlert,
  HardDriveIcon as HardDrive,
  GlobeIcon as Globe,
  ArrowUpRightIcon as ArrowUpRight,
  LogOutIcon as LogOut,
  TagIcon as Tag,
  BookmarkIcon as Bookmark,
  ChevronDownIcon as ChevronDown,
} from "@lucide/vue";
import { useApp } from "./state.js";

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

const themeMeta = computed(() => {
  const t = s.theme.value || "system";
  if (t === "dark") {
    return {
      name: "dark",
      label: "深色模式",
      nextLabel: "浅色模式",
      icon: Moon,
    };
  }
  if (t === "light") {
    return {
      name: "light",
      label: "浅色模式",
      nextLabel: "自动模式",
      icon: Sun,
    };
  }
  return {
    name: "system",
    label: "自动模式",
    nextLabel: "深色模式",
    icon: ThemeAutoIcon,
  };
});
const groups = computed(() => {
  let f = s.config.value?.fields || {};
  let map = {
    站点与输出: [
      "eh_site",
      "public_base_url",
      "pse_page_base",
      "opds_acq_detail",
    ],
    登录凭据: ["ipb_member_id", "ipb_pass_hash", "igneous"],
    功能与缓存: [
      "tag_status_filter",
      "tag_translation_enabled",
      "comments_enabled",
      "image_cache_enabled",
      "cache_max_gb",
    ],
    收藏夹: [
      "favorites_sync_interval_seconds",
      "favorites_sync_categories",
      "favorites_sync_archive",
    ],
    归档: ["archive_update_interval_days", "archive_auto_update"],
  };
  return Object.entries(map)
    .map(([title, keys]) => ({
      title,
      fields: keys.filter((k) => f[k]).map((k) => [k, f[k]]),
    }))
    .filter((x) => x.fields.length);
});
const labels = {
  eh_site: "E-Hentai 站点",
  public_base_url: "外部访问 URL",
  pse_page_base: "PSE 页码起始值",
  opds_acq_detail: "详情获取方式",
  ipb_member_id: "IPB Member ID",
  ipb_pass_hash: "IPB Pass Hash",
  igneous: "Igneous Cookie",
  tag_status_filter: "标签状态筛选",
  tag_translation_enabled: "标签翻译",
  comments_enabled: "评论",
  image_cache_enabled: "图片缓存",
  cache_max_gb: "图片缓存上限（GB）",
  favorites_sync_interval_seconds: "收藏同步间隔（秒）",
  favorites_sync_categories: "自动归档范围",
  favorites_sync_archive: "自动归档新增收藏",
  archive_update_interval_days: "归档版本检查周期（天）",
  archive_auto_update: "自动更新归档",
};
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
    return "IPB_MEMBER_ID / IPB_PASS_HASH 环境变量控制此项。";
  const env =
    {
      eh_site: "EH_SITE",
      igneous: "IGNEOUS",
      public_base_url: "PUBLIC_BASE_URL",
      pse_page_base: "PSE_PAGE_BASE",
      opds_acq_detail: "OPDS_ACQ_DETAIL",
      tag_status_filter: "TAG_STATUS_FILTER",
      tag_translation_enabled: "TAG_TRANSLATION_ENABLED",
      comments_enabled: "COMMENTS_ENABLED",
      image_cache_enabled: "IMAGE_CACHE_ENABLED",
      cache_max_gb: "CACHE_MAX_GB",
      favorites_sync_interval_seconds: "FAVORITES_SYNC_INTERVAL_SECONDS",
      favorites_sync_categories: "FAVORITES_SYNC_CATEGORIES",
    }[key] || key.toUpperCase();
  return env + " 环境变量控制此项。";
};
const qualityLabel = (entry) => {
  const tier = String(entry.or || entry.quality || "").toLowerCase();
  if (tier === "org" || tier.includes("original")) return "原档";
  if (tier === "res" || tier.includes("resample")) return "降采样";
  if (entry.status === "unarchived") return "在线";
  return "归档";
};
const countLabel = (section) => {
  if (section.kind === "navigation") return "导航不使用 count";
  if (section.count == null) return "默认 10";
  if (Number(section.count) === 0) return "关闭预览";
  return section.count + " 项";
};
const previewItems = computed(() => {
  const sections = s.layout.value.sections || [];
  const byKey = new Map();
  const items = [];
  for (const [index, section] of sections.entries()) {
    let key, item;
    if (section.group) {
      key = "group:" + section.group;
      item = byKey.get(key);
      if (!item) {
        item = {
          key,
          kind: "group",
          id: section.group,
          title:
            s.layout.value.groups.find((group) => group.id === section.group)
              ?.title || section.group,
          sections: sections.filter(
            (candidate) => candidate.group === section.group,
          ),
        };
        byKey.set(key, item);
        items.push(item);
      }
    } else if (section.kind === "navigation") {
      key = "root-navigation";
      item = byKey.get(key);
      if (!item) {
        item = {
          key,
          kind: "root-navigation",
          id: "",
          title: "根目录导航",
          sections: sections.filter(
            (candidate) => !candidate.group && candidate.kind === "navigation",
          ),
        };
        byKey.set(key, item);
        items.push(item);
      }
    } else {
      key = "root-publication:" + index;
      items.push({
        key,
        kind: "publication",
        id: "",
        title: section.title || "独立出版物栏目",
        sections: [section],
      });
    }
  }
  return items;
});

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
  return Boolean(
    a &&
      ["ready", "unarchived", "failed"].includes(a.status) &&
      archivePageCount(a) > 0,
  );
}

function openReader(a) {
  if (!canReadOnline(a)) return;
  readerArchive.value = a;
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
      name: found?.name || archiveNames[i] || `收藏夹 ${i}`,
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
    return "全部分类";
  }
  const found = EH_CATEGORIES.find(
    (c) => c.key.toLowerCase() === selectedCategory.value.toLowerCase(),
  );
  return found ? found.label : selectedCategory.value;
});

const favcatLabel = computed(() => {
  if (!selectedFavcat.value || selectedFavcat.value === "all") {
    return "全部收藏";
  }
  if (selectedFavcat.value === "unfavorited") return "未收藏";
  if (selectedFavcat.value === "favorited") return "已收藏";
  const id = Number(selectedFavcat.value);
  const found = favcatList.value.find((c) => c.id === id);
  return found ? `${found.id}: ${found.name}` : `收藏夹 ${id}`;
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
  const t = s.deleteTarget.value;
  if (!t) {
    return { title: "删除本地归档？", desc: "", note: "", confirm: "删除归档" };
  }
  const fav = Boolean(t.is_favorited);
  if (t.status === "failed") {
    return {
      title: "清除失败记录？",
      desc: fav
        ? "将清理临时下载文件与报错信息，保留收藏建档记录并恢复为「未归档」状态。"
        : "将清理临时下载文件并移除该失败记录。",
      note: fav ? "操作不会影响 E-Hentai 收藏" : "操作将彻底移除该失败条目",
      confirm: "清除失败",
    };
  }
  if (["pending", "downloading", "zipping", "submitting"].includes(t.status)) {
    return {
      title: "取消当前归档任务？",
      desc: fav
        ? "将中止下载并清理临时文件，保留收藏建档记录并恢复为「未归档」状态。"
        : "将中止下载、清理临时文件并移除该条目。",
      note: fav ? "操作不会影响 E-Hentai 收藏" : "操作将彻底移除该条目",
      confirm: "取消归档",
    };
  }
  if (t.status === "unarchived") {
    return {
      title: "移除残留记录？",
      desc: "该条目不在收藏夹且无本地归档文件，移除后将从「浏览」列表清除。",
      note: "操作仅清理本地元数据索引",
      confirm: "移除",
    };
  }
  return {
    title: "删除本地归档？",
    desc: fav
      ? "仅删除本地归档文件；因仍在收藏夹中，卡片将保留为「未归档」状态并可继续在线阅读（移除收藏后会自动清理）。"
      : "将删除本地归档文件及本地记录（不在收藏夹中，将从「浏览」列表移除）；E-Hentai 账号记录保留。",
    note: fav
      ? "本地归档 ZIP 将被删除，收藏卡片保留"
      : "操作只影响本机保存的归档与记录",
    confirm: "删除归档",
  };
});

function cardActionLabel(a) {
  if (a.status === "unarchived") {
    return a.is_favorited ? "下载归档" : "移除";
  }
  if (a.status === "failed") return "清除失败";
  if (["pending", "downloading", "zipping", "submitting"].includes(a.status)) {
    return "取消归档";
  }
  return "删除归档";
}
</script>
<template>
  <div
    v-if="!s.authChecked.value"
    class="min-h-svh w-full bg-background"
  />
  <div
    v-else-if="s.authRequired.value"
    class="flex min-h-svh w-full items-center justify-center p-6 md:p-10 bg-background"
  >
    <div class="w-full max-w-sm">
      <LoginForm />
    </div>
  </div>
  <SidebarProvider v-else class="app-layout" style="--sidebar-width: 17.5rem">
    <Sidebar collapsible="offcanvas" variant="inset">
      <SidebarHeader class="brand-header px-5 py-4">
        <span class="font-bold text-base tracking-tight text-foreground select-none">
          PandaOPDS
        </span>
      </SidebarHeader>
      <AppSidebarNav :page="s.page.value" @navigate="s.route" />
      <SidebarFooter
        v-if="s.authToken.value || s.status.value?.app?.auth_enabled"
        class="sidebar-footer border-t border-sidebar-border/60 p-3"
      >
        <Button
          variant="ghost"
          size="xs"
          class="w-full justify-start text-xs text-muted-foreground hover:text-foreground h-7 px-2 gap-1.5"
          @click="s.logout"
        >
          <LogOut class="size-3.5" />退出登录
        </Button>
      </SidebarFooter>
    </Sidebar>
    <SidebarInset class="webui-inset">
      <header class="site-header">
        <SidebarTrigger aria-label="切换导航菜单" class="-ml-1" />
        <Separator orientation="vertical" class="header-separator h-4 my-auto self-center shrink-0" />
        <div class="breadcrumb">
          <span class="breadcrumb-title font-semibold text-sm text-foreground tracking-tight">{{
            s.page.value === "dashboard"
              ? "概览"
              : s.page.value === "config"
                ? "设置"
                : s.page.value === "home"
                  ? "首页编排"
                  : "归档"
          }}</span>
        </div>
        <div class="header-actions">
          <Button
            variant="ghost"
            size="icon-sm"
            class="size-8 text-muted-foreground hover:text-foreground"
            @click="cycleTheme"
            :title="`主题：${themeMeta.label}（点击切换至${themeMeta.nextLabel}）`"
            :aria-label="`主题：${themeMeta.label}，点击切换`"
          >
            <component :is="themeMeta.icon" class="size-4" />
          </Button>
        </div>
      </header>
      <div class="page-content">
        <Alert v-if="s.notice.value" class="app-alert success-alert">
          <CircleCheck class="size-4 text-foreground" />
          <AlertDescription class="text-sm">{{ s.notice.value }}</AlertDescription>
          <Button
            variant="ghost"
            size="icon-xs"
            aria-label="关闭通知"
            @click="s.notice.value = ''"
            ><X :size="14"
          /></Button>
        </Alert>
        <Alert v-if="s.error.value" variant="destructive" class="app-alert">
          <CircleAlert class="size-4" />
          <AlertDescription class="text-sm">{{ s.error.value }}</AlertDescription>
          <Button
            variant="ghost"
            size="icon-xs"
            aria-label="关闭错误"
            @click="s.error.value = ''"
            ><X :size="14"
          /></Button>
        </Alert>
        <template v-if="s.page.value === 'dashboard'">
          <div class="heading">
            <div>
              <div class="flex items-center gap-2 mb-2">
                <span class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium border border-border/80 bg-background text-foreground shadow-xs">
                  <span
                    class="size-1.5 rounded-full"
                    :class="s.status.value?.app?.config_ok ? 'bg-emerald-500 animate-pulse' : 'bg-amber-500'"
                  ></span>
                  {{ s.status.value?.app?.config_ok ? "服务运行正常" : "配置需要检查" }}
                </span>
                <span
                  v-if="s.status.value?.circuit?.state === 'open'"
                  class="inline-flex items-center gap-1 px-2.5 py-0.5 rounded-full text-xs font-medium bg-destructive/10 text-destructive border border-destructive/20"
                >
                  熔断器 OPEN
                </span>
                <span v-else class="text-xs text-muted-foreground hidden sm:inline">
                  熔断器正常 · 15s 自动同步
                </span>
              </div>
              <h1 class="text-2xl font-bold tracking-tight text-foreground">服务概览</h1>
              <p class="text-sm text-muted-foreground mt-1">查看上游站点状态、出站请求与多级缓存负载。</p>
            </div>
          </div>

          <div class="metrics">
            <Card class="shadow-xs hover:shadow-sm transition-shadow">
              <CardHeader class="flex flex-row items-center justify-between pb-2 space-y-0">
                <CardTitle class="text-xs font-medium uppercase tracking-wider text-muted-foreground">上游站点</CardTitle>
                <CardAction><Globe class="size-4 text-muted-foreground" /></CardAction>
              </CardHeader>
              <CardContent>
                <div class="text-2xl font-bold tracking-tight text-foreground capitalize">
                  {{ s.status.value?.site?.site || "—" }}
                </div>
                <div class="flex items-center gap-1.5 mt-1.5 text-xs text-muted-foreground">
                  <span class="inline-block size-1.5 rounded-full bg-emerald-500"></span>
                  <span class="truncate font-mono">{{ s.status.value?.site?.host || "e-hentai.org" }}</span>
                </div>
              </CardContent>
            </Card>

            <Card class="shadow-xs hover:shadow-sm transition-shadow">
              <CardHeader class="flex flex-row items-center justify-between pb-2 space-y-0">
                <CardTitle class="text-xs font-medium uppercase tracking-wider text-muted-foreground">出站请求总量</CardTitle>
                <CardAction><Activity class="size-4 text-muted-foreground" /></CardAction>
              </CardHeader>
              <CardContent>
                <div class="text-2xl font-bold tracking-tight font-mono text-foreground">
                  {{
                    Number(s.status.value?.throttle?.html_requests || 0) +
                    Number(s.status.value?.throttle?.api_requests || 0) +
                    Number(s.status.value?.throttle?.image_requests || 0)
                  }}
                </div>
                <div class="flex items-center gap-1.5 mt-1.5 text-xs text-muted-foreground font-mono">
                  <span>HTML {{ s.status.value?.throttle?.html_requests || 0 }}</span>
                  <span>·</span>
                  <span>API {{ s.status.value?.throttle?.api_requests || 0 }}</span>
                  <span>·</span>
                  <span>图 {{ s.status.value?.throttle?.image_requests || 0 }}</span>
                </div>
              </CardContent>
            </Card>

            <Card class="shadow-xs hover:shadow-sm transition-shadow">
              <CardHeader class="flex flex-row items-center justify-between pb-2 space-y-0">
                <CardTitle class="text-xs font-medium uppercase tracking-wider text-muted-foreground">内存缓存</CardTitle>
                <CardAction><Database class="size-4 text-muted-foreground" /></CardAction>
              </CardHeader>
              <CardContent>
                <div class="flex items-baseline justify-between">
                  <div class="text-2xl font-bold tracking-tight font-mono text-foreground">
                    {{ s.status.value?.memory_cache?.size ?? 0 }}
                    <span class="text-xs font-normal text-muted-foreground">/ {{ s.status.value?.memory_cache?.max_entries ?? 2000 }}</span>
                  </div>
                  <span class="text-xs font-medium text-muted-foreground font-mono">
                    {{
                      s.status.value?.memory_cache?.max_entries
                        ? Math.round((Number(s.status.value?.memory_cache?.size || 0) / Number(s.status.value?.memory_cache?.max_entries)) * 100)
                        : 0
                    }}%
                  </span>
                </div>
                <div class="w-full bg-muted rounded-full h-1.5 mt-2.5 overflow-hidden">
                  <div
                    class="bg-foreground h-full rounded-full transition-all duration-300"
                    :style="{
                      width: `${Math.min(100, s.status.value?.memory_cache?.max_entries ? Math.round((Number(s.status.value?.memory_cache?.size || 0) / Number(s.status.value?.memory_cache?.max_entries)) * 100) : 0)}%`
                    }"
                  ></div>
                </div>
                <div class="flex items-center justify-between mt-2 text-xs text-muted-foreground font-mono">
                  <span>命中 {{ s.status.value?.memory_cache?.hits ?? 0 }}</span>
                  <span>未命中 {{ s.status.value?.memory_cache?.misses ?? 0 }}</span>
                </div>
              </CardContent>
            </Card>

            <Card class="shadow-xs hover:shadow-sm transition-shadow">
              <CardHeader class="flex flex-row items-center justify-between pb-2 space-y-0">
                <CardTitle class="text-xs font-medium uppercase tracking-wider text-muted-foreground">磁盘图片缓存</CardTitle>
                <CardAction><HardDrive class="size-4 text-muted-foreground" /></CardAction>
              </CardHeader>
              <CardContent>
                <div class="flex items-baseline justify-between">
                  <div class="text-2xl font-bold tracking-tight text-foreground">
                    {{ s.status.value?.disk_cache?.enabled ? "已启用" : "已关闭" }}
                  </div>
                  <Button
                    v-if="s.status.value?.disk_cache?.enabled"
                    variant="ghost"
                    size="xs"
                    class="h-6 px-1.5 text-xs text-muted-foreground hover:text-foreground"
                    @click="s.askClearCache"
                    title="清理图片缓存"
                  >
                    <Trash2 class="size-3 mr-1" />清理
                  </Button>
                </div>
                <div class="flex items-center justify-between mt-1.5 text-xs text-muted-foreground font-mono">
                  <span>{{ s.status.value?.disk_cache?.entries ?? 0 }} 项条目</span>
                  <span>{{ formatBytes(s.status.value?.disk_cache?.bytes ?? 0) }}</span>
                </div>
              </CardContent>
            </Card>
          </div>

          <div class="columns">
            <Card class="shadow-xs">
              <CardHeader class="border-b border-border/50 pb-3">
                <CardTitle class="text-base font-semibold text-foreground">运行详情</CardTitle>
                <CardDescription class="text-xs text-muted-foreground">当前进程与上游连接协议配置</CardDescription>
              </CardHeader>
              <CardContent class="pt-2">
                <div class="divide-y divide-border/50 text-sm">
                  <div class="flex items-center justify-between py-2.5">
                    <span class="text-muted-foreground text-xs font-medium">版本</span>
                    <Badge variant="secondary" class="font-mono text-xs">
                      v{{ s.status.value?.app?.version || "—" }}
                    </Badge>
                  </div>
                  <div class="flex items-center justify-between py-2.5">
                    <span class="text-muted-foreground text-xs font-medium">外部 URL</span>
                    <span class="font-mono text-xs text-foreground truncate max-w-[240px]">
                      {{ s.status.value?.site?.public_base_url || "未设置（相对路径）" }}
                    </span>
                  </div>
                  <div class="flex items-center justify-between py-2.5">
                    <span class="text-muted-foreground text-xs font-medium">IPB 登录状态</span>
                    <Badge :variant="s.status.value?.site?.has_ipb ? 'outline' : 'secondary'" class="text-xs">
                      {{ s.status.value?.site?.has_ipb ? "已配置凭据" : "未配置（游客模式）" }}
                    </Badge>
                  </div>
                  <div class="flex items-center justify-between py-2.5">
                    <span class="text-muted-foreground text-xs font-medium">并发请求上限</span>
                    <span class="font-mono text-xs font-medium text-foreground">
                      {{ s.status.value?.throttle?.max_concurrency ?? "—" }} 线程
                    </span>
                  </div>
                  <div class="flex items-center justify-between py-2.5">
                    <span class="text-muted-foreground text-xs font-medium">HTML 请求间隔</span>
                    <span class="font-mono text-xs font-medium text-foreground">
                      {{ s.status.value?.throttle?.html_interval_seconds ?? "—" }} 秒
                    </span>
                  </div>
                  <div v-if="s.status.value?.circuit?.state === 'open'" class="flex items-center justify-between py-2.5 text-destructive">
                    <span class="text-xs font-medium">熔断原因</span>
                    <span class="text-xs font-mono">{{ s.status.value?.circuit?.reason }}</span>
                  </div>
                </div>
              </CardContent>
            </Card>

            <Card class="shadow-xs">
              <CardHeader class="border-b border-border/50 pb-3">
                <div class="flex items-center justify-between">
                  <div>
                    <CardTitle class="text-base font-semibold text-foreground">首页布局</CardTitle>
                    <CardDescription class="text-xs text-muted-foreground">OPDS 首页编排与展示来源</CardDescription>
                  </div>
                  <Button variant="ghost" size="xs" class="h-7 text-xs gap-1" @click="s.route('home')">
                    进入编排 <ArrowUpRight :size="12" />
                  </Button>
                </div>
              </CardHeader>
              <CardContent class="pt-2">
                <div class="divide-y divide-border/50 text-sm">
                  <div class="flex items-center justify-between py-2.5">
                    <span class="text-muted-foreground text-xs font-medium">内容来源</span>
                    <Badge variant="outline" class="font-mono text-xs">
                      {{ s.status.value?.home?.using_file ? "home.toml" : "内置默认" }}
                    </Badge>
                  </div>
                  <div class="flex items-center justify-between py-2.5">
                    <span class="text-muted-foreground text-xs font-medium">配置文件路径</span>
                    <span class="font-mono text-xs text-muted-foreground truncate max-w-[260px]" :title="s.status.value?.home?.path">
                      {{ s.status.value?.home?.path || "—" }}
                    </span>
                  </div>
                  <div class="flex items-center justify-between py-2.5">
                    <span class="text-muted-foreground text-xs font-medium">已配置分组 / 栏目</span>
                    <span class="font-mono text-xs font-medium text-foreground">
                      {{ s.status.value?.home?.groups ?? 0 }} 组 / {{ s.status.value?.home?.sections ?? 0 }} 栏目
                    </span>
                  </div>
                  <div class="flex items-center justify-between py-2.5">
                    <span class="text-muted-foreground text-xs font-medium">OPDS 协议版本</span>
                    <div class="flex items-center gap-1.5">
                      <Badge variant="secondary" class="text-[10px] font-mono">v1.2</Badge>
                      <Badge variant="secondary" class="text-[10px] font-mono">v2.0</Badge>
                      <Badge variant="secondary" class="text-[10px] font-mono">OPDS-PSE</Badge>
                    </div>
                  </div>
                  <div class="flex items-center justify-between py-2.5">
                    <span class="text-muted-foreground text-xs font-medium">服务自愈心跳</span>
                    <span class="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
                      <span class="size-1.5 rounded-full bg-emerald-500"></span>
                      活跃中
                    </span>
                  </div>
                </div>
              </CardContent>
            </Card>
          </div>
        </template>
        <template v-if="s.page.value === 'config'"
          ><div class="heading">
            <div>
              <small>服务选项</small>
              <h1>设置</h1>
              <p>环境变量锁定项需在部署环境中修改。</p>
            </div>
            <Badge v-if="s.config.value?.pending" variant="outline" class="badge-warn"
              >部分更改重启后生效</Badge
            >
          </div>
          <Card v-if="s.config.value?.legacy" class="panel">
            <CardHeader class="panel-header">
              <CardTitle role="heading" aria-level="2">当前设置（只读）</CardTitle>
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
                      ? "••••••••（已设置）"
                      : "（未设置）"
                    : (field.value ?? "（未设置）")
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
                <CardTitle role="heading" aria-level="2">{{ group.title }}</CardTitle>
                <CardDescription>保存的更改都需要重启服务后生效</CardDescription>
              </div>
            </CardHeader>
            <CardContent class="form-panel-content">
            <div v-for="[key, f] in group.fields" :key="key" class="field">
              <div>
                <label :for="key"
                  >{{ labels[key] || key }}
                  <Badge v-if="f.pending" variant="outline" class="badge-warn">待重启</Badge></label
                ><small>{{
                  f.locked
                    ? lockedReason(key)
                    : "来源：" +
                      (f.source === "env"
                        ? "环境变量"
                        : f.source === "file"
                          ? "配置文件"
                          : "默认值")
                }}</small>
                <small v-if="key === 'archive_update_interval_days'">0 关闭周期检查；仅写入 settings.toml。</small>
                <small v-if="key === 'archive_auto_update'">启用后自动购买新版归档，可能消耗 GP；仅写入 settings.toml。</small>
                <small v-if="key === 'favorites_sync_interval_seconds'">默认 3600 秒；设为 0 只关闭周期扫描，收藏操作触发的扫描和手动扫描仍可用。</small>
                <small v-if="key === 'favorites_sync_categories'">逗号分隔收藏夹 ID（0–9）；留空表示全部，-ID 表示排除该收藏夹。只限制自动归档范围，不影响同步扫描。</small>
                <small v-if="key === 'favorites_sync_archive'">启用后会自动购买新发现收藏的归档，可能消耗 GP；首次扫描只建立基线，不会归档已有收藏。保存后重启生效。</small>
              </div>
              <div class="control">
                <label v-if="boolean(key)" class="switch"
                  ><input
                    :id="key"
                    type="checkbox"
                    :checked="Boolean(s.form.value[key])"
                    :disabled="f.locked"
                    @change="edit(key, $event.target.checked)" /><i></i></label
                ><Select
                  v-if="key === 'eh_site'"
                  :model-value="String(s.form.value[key] ?? '')"
                  :disabled="f.locked"
                  @update:model-value="edit(key, $event)"
                >
                  <SelectTrigger :id="key" class="control-select">
                    <SelectValue placeholder="选择站点" />
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
                    <SelectValue placeholder="选择页码起始值" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="1">1-based（默认）</SelectItem>
                    <SelectItem value="0">0-based</SelectItem>
                  </SelectContent>
                </Select>
                <Select
                  v-if="key === 'tag_status_filter'"
                  :model-value="String(s.form.value[key] ?? '')"
                  :disabled="f.locked"
                  @update:model-value="edit(key, $event)"
                >
                  <SelectTrigger :id="key" class="control-select">
                    <SelectValue placeholder="选择标签筛选" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="balanced">平衡</SelectItem>
                    <SelectItem value="strict">严格</SelectItem>
                    <SelectItem value="off">关闭</SelectItem>
                  </SelectContent>
                </Select>
                <Input
                  v-if="
                    !boolean(key) &&
                    !['eh_site', 'pse_page_base', 'tag_status_filter'].includes(
                      key,
                    )
                  "
                  :id="key"
                  :type="
                    secret(key)
                      ? 'password'
                      : key === 'cache_max_gb' || key === 'archive_update_interval_days' || key === 'favorites_sync_interval_seconds'
                        ? 'number'
                        : 'text'
                  "
                  :min="['archive_update_interval_days', 'favorites_sync_interval_seconds'].includes(key) ? 0 : undefined"
                  :step="key === 'archive_update_interval_days' ? 1 : key === 'favorites_sync_interval_seconds' ? 'any' : undefined"
                  :model-value="s.form.value[key] ?? ''"
                  :placeholder="
                    secret(key)
                      ? f.saved_set
                        ? '已设置；留空不更改'
                        : '未设置'
                      : ''
                  "
                  :disabled="f.locked"
                  class="control-input"
                  autocomplete="new-password"
                  @update:model-value="edit(key, $event)"
                /><Badge v-if="f.locked" variant="outline" class="locked">🔒 锁定</Badge
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
                  清除成对凭据</Button
                ><Button
                  v-if="key === 'igneous'"
                  :disabled="f.locked"
                  variant="link"
                  size="sm"
                  @click="edit('igneous', '')"
                >
                  清除
                </Button>
              </div>
              <small class="active-value"
                >当前生效：{{
                  secret(key)
                    ? f.set
                      ? "已设置"
                      : "未设置"
                    : (f.active ?? "（未设置）")
                }}
                · 已保存：{{
                  secret(key)
                    ? f.saved_set
                      ? "已设置"
                      : "未设置"
                    : (f.saved ?? "（未设置）")
                }}</small
              >
            </div>
            </CardContent>
          </Card>
          <div v-if="s.config.value?.fields" class="savebar">
            <span>{{
              Object.keys(s.confDirty.value).length
                ? "有未保存的更改"
                : s.config.value?.pending
                  ? "已保存，待重启"
                  : "设置已同步"
            }}</span
            ><Button
              :disabled="!Object.keys(s.confDirty.value).length || s.busy.value"
              @click="s.saveConfig"
              ><Save />保存设置</Button
            >
          </div></template
        >
        <template v-if="s.page.value === 'home'"
          ><div class="heading">
            <div>
              <small>OPDS 入口</small>
              <h1>首页编排</h1>
              <p>分组与栏目顺序决定 OPDS 首页的展示顺序。</p>
            </div>
            <Button
              class="primary-action"
              :disabled="!s.homeDirty.value || s.busy.value"
              @click="s.saveHome"
            >
              <Save />保存布局
            </Button>
          </div>
          <Card v-if="s.home.value?.legacy" class="panel">
            <CardHeader class="panel-header">
              <CardTitle role="heading" aria-level="2">当前首页布局（只读）</CardTitle>
              <CardDescription>
              {{ s.home.value.legacy.path }} ·
              {{ s.home.value.legacy.using_file ? "文件配置" : "内置默认" }}
              </CardDescription>
            </CardHeader>
            <CardContent class="panel-body">
            <div v-if="s.home.value.legacy.error" class="archive-error">
              {{ s.home.value.legacy.error }}
            </div>
            <div class="legacy-groups">
              <Badge
                v-for="g in s.home.value.legacy.groups || []"
                :key="g.id"
                variant="outline"
                >{{ g.id }} · {{ g.title }}</Badge
              >
            </div>
            <div
              v-for="(sec, i) in s.home.value.legacy.sections || []"
              :key="i"
              class="preview"
            >
              <b>{{ i + 1 }}</b>
              <div>
                <strong>{{ sec.title }}</strong
                ><small
                  >{{ sec.kind }} · {{ sec.type }} · {{ sec.query }} ·
                  {{ sec.count ?? "不限" }} · {{ sec.group || "根" }}</small
                >
              </div>
            </div>
            </CardContent>
          </Card>
          <Card
            v-if="s.home.value?.error && !s.home.value?.legacy"
            class="panel"
          >
            <CardHeader class="panel-header">
              <CardTitle role="heading" aria-level="2">首页布局（只读）</CardTitle>
              <CardDescription>{{ s.home.value.path }} · 文件内容无法编辑</CardDescription>
            </CardHeader>
            <CardContent class="panel-body">
            <div class="archive-error">{{ s.home.value.error }}</div>
            <div
              v-for="(sec, i) in s.home.value.sections || []"
              :key="i"
              class="preview"
            >
              <b>{{ i + 1 }}</b>
              <div>
                <strong>{{ sec.title }}</strong
                ><small
                  >{{ sec.kind }} · {{ sec.type }} · {{ sec.query }} ·
                  {{ sec.count == null ? "不限" : sec.count }} ·
                  {{ sec.group || "根" }}</small
                >
              </div>
            </div>
            </CardContent>
          </Card>
          <div
            v-if="s.home.value?.sections && !s.home.value?.error"
            class="home-columns"
          >
            <Card class="panel">
              <CardHeader class="panel-header">
                <div class="panel-title">
                <div>
                  <CardTitle role="heading" aria-level="2">栏目设置</CardTitle>
                  <CardDescription>按列表顺序显示；留空分组则直接展示</CardDescription>
                </div>
                <div>
                  <Button variant="outline" @click="s.addGroup">添加分组</Button>
                  <Button variant="outline" @click="s.addSection">
                    <Plus />添加栏目
                  </Button>
                </div>
                </div>
              </CardHeader>
              <CardContent class="panel-body home-editor">
              <div
                v-for="(g, gi) in s.layout.value.groups"
                :key="g.id"
                class="group-row"
              >
                <Input
                  :model-value="g.id"
                  @change="s.renameGroup(gi, $event.target.value)"
                  aria-label="分组标识"
                /><Input
                  v-model="g.title"
                  @update:model-value="s.homeDirty.value = true"
                  aria-label="分组名称"
                /><Button
                  variant="ghost"
                  size="icon-sm"
                  @click="s.removeGroup(g.id)"
                  aria-label="删除分组"
                >
                  <X />
                </Button>
              </div>
              <div
                v-for="(sec, i) in s.layout.value.sections"
                :key="i"
                class="section-edit"
              >
                <GripVertical /><span>{{
                  String(i + 1).padStart(2, "0")
                }}</span>
                <div class="edit-fields">
                  <Input
                    v-model="sec.title"
                    @update:model-value="s.homeDirty.value = true"
                    placeholder="栏目标题"
                  /><select
                    v-model="sec.kind"
                    @change="s.homeDirty.value = true"
                  >
                    <option value="publication">出版物列表</option>
                    <option value="navigation">导航目录</option></select
                  ><select
                    v-model="sec.group"
                    @change="s.homeDirty.value = true"
                  >
                    <option value="">未分组</option>
                    <option v-for="g in s.layout.value.groups" :value="g.id">
                      {{ g.title || g.id }}
                    </option></select
                  ><select
                    v-model="sec.type"
                    @change="s.homeDirty.value = true"
                  >
                    <option value="preset">预设</option>
                    <option value="search">搜索</option></select
                  ><Input
                    v-if="sec.type === 'preset'"
                    v-model="sec.query"
                    list="preset-options"
                    @update:model-value="s.homeDirty.value = true"
                    placeholder="预设或 favorites:数字"
                  /><datalist id="preset-options">
                    <option value="popular" />
                    <option value="latest" />
                    <option value="watched" />
                    <option value="favorites" />
                    <option value="archives" />
                    <option value="toplist:yesterday" />
                    <option value="toplist:month" />
                    <option value="toplist:year" />
                    <option value="toplist:alltime" /></datalist
                  ><Input
                    v-if="sec.type === 'search'"
                    v-model="sec.query"
                    @update:model-value="s.homeDirty.value = true"
                    placeholder="搜索词"
                  /><label class="count-field"
                    >count
                    <Input
                      type="number"
                      min="0"
                      :disabled="sec.kind === 'navigation'"
                      v-model.number="sec.count"
                      @update:model-value="s.homeDirty.value = true"
                      placeholder="默认 10；填 0 关闭预览"
                  /></label>
                </div>
                <div class="reorder">
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    :disabled="i === 0"
                    @click="s.moveSection(i, -1)"
                    aria-label="上移"
                  >
                    ↑</button
                  ><Button
                    variant="ghost"
                    size="icon-sm"
                    :disabled="i === s.layout.value.sections.length - 1"
                    @click="s.moveSection(i, 1)"
                    aria-label="下移"
                  >
                    ↓</button
                  ><Button
                    variant="ghost"
                    size="icon-sm"
                    @click="s.removeSection(i)"
                    aria-label="移除栏目"
                  >
                    <X />
                  </Button>
                </div>
              </div>
              <div v-if="!s.layout.value.sections.length" class="empty">
                暂无栏目，请添加一个栏目。
              </div>
              </CardContent>
            </Card>
            <Card class="panel preview-panel">
              <CardHeader class="panel-header">
                <CardTitle role="heading" aria-level="2">结构预览</CardTitle>
                <CardDescription>显示顺序</CardDescription>
              </CardHeader>
              <CardContent class="panel-body">
              <div
                v-for="(item, index) in previewItems"
                :key="item.key"
                class="preview-group"
              >
                <div class="preview-group-head">
                  <h3>{{ item.title }}</h3>
                  <div class="reorder">
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      :disabled="index === 0"
                      @click="s.movePreviewItem(item.key, -1)"
                      :aria-label="'上移 ' + item.title"
                    >
                      ↑
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      :disabled="index === previewItems.length - 1"
                      @click="s.movePreviewItem(item.key, 1)"
                      :aria-label="'下移 ' + item.title"
                    >
                      ↓
                    </Button>
                  </div>
                </div>
                <div v-for="(sec, i) in item.sections" :key="i" class="preview">
                  <b>{{ i + 1 }}</b>
                  <div>
                    <strong>{{ sec.title || "未命名栏目" }}</strong
                    ><small
                      >{{ sec.kind }} ·
                      {{
                        sec.type === "search"
                          ? "搜索 · " + (sec.query || "未设置查询")
                          : "预设 · " + sec.query
                      }}
                      · {{ countLabel(sec) }}</small
                    >
                  </div>
                </div>
              </div>
              </CardContent>
            </Card>
          </div></template
        >
        <template v-if="s.page.value === 'archive'">
          <div class="heading">
            <div>
              <small>本地存档</small>
              <h1>归档</h1>
              <p>归档操作可能消耗账户 GP；每次开始前会明确确认。</p>
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
                    <RefreshCw class="size-3.5" />同步收藏
                  </Button>
                  <Button
                    variant="outline"
                    size="icon"
                    class="rounded-l-none w-8 px-0"
                    :disabled="!s.status.value?.site?.has_ipb || s.busy.value"
                    aria-label="更多同步选项"
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
                    <span class="font-medium text-foreground">全量校准收藏…</span>
                    <span class="text-[11px] text-muted-foreground mt-0.5">遍历全部收藏页并清理上游已取消项</span>
                  </button>
                </div>
              </div>
              <Button
                variant="outline"
                :disabled="s.archiveScan.value.status === 'running'"
                @click="s.startArchiveScan"
                title="扫描 ARCHIVE_DIR 中已有的历史归档文件并导入索引"
              >
                {{ s.archiveScan.value.status === 'running' ? '正在导入…' : '导入本地归档' }}
              </Button>
              <Button class="primary-action" @click="openCreateArchiveDialog">
                <Plus />新建归档
              </Button>
            </div>
          </div>
          <div
            v-if="showArchiveScanBanner"
            class="archive-scan-status"
            :class="'archive-scan-' + s.archiveScan.value.status"
            role="status"
          >
            <span v-if="s.archiveScan.value.status === 'running'">正在查找归档状态与图库资料…</span>
            <span v-else-if="s.archiveScan.value.status === 'success'">导入完成：导入 {{ s.archiveScan.value.imported }} 个文件，跳过 {{ s.archiveScan.value.skipped }} 个文件，冲突 {{ s.archiveScan.value.conflicts }} 个文件。</span>
            <span v-else-if="s.archiveScan.value.status === 'skipped'">导入完成：未导入新文件，跳过 {{ s.archiveScan.value.skipped }} 个文件。</span>
            <span v-else-if="s.archiveScan.value.status === 'conflict'">导入完成：未导入文件，发现 {{ s.archiveScan.value.conflicts }} 个冲突文件。</span>
            <span v-else>导入失败：{{ s.archiveScan.value.error || '未知错误' }}</span>
            <Button
              v-if="s.archiveScan.value.status !== 'running'"
              variant="ghost"
              size="icon-xs"
              aria-label="关闭导入提示"
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
                <span>全部</span>
              </button>
              <button
                type="button"
                role="tab"
                class="segment-btn"
                :class="{ active: selectedStatus === 'ready' }"
                @click="selectedStatus = 'ready'"
              >
                <span>已归档</span>
                <span
                  v-if="statusCounts.ready > 0"
                  class="segment-badge"
                >
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
                <span>未归档</span>
                <span
                  v-if="statusCounts.unarchived > 0"
                  class="segment-badge"
                >
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
                <span>可更新</span>
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
                <span>下载中</span>
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
                <span>失败</span>
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
                  placeholder="搜索画廊..."
                  class="toolbar-search-input"
                />
                <button
                  v-if="archiveSearch"
                  type="button"
                  class="search-clear-btn"
                  @click="archiveSearch = ''"
                  aria-label="清除搜索"
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
                    <span class="font-medium">全部分类</span>
                  </SelectItem>
                  <SelectItem
                    v-for="cat in EH_CATEGORIES"
                    :key="cat.key"
                    :value="cat.key"
                  >
                    <span class="inline-flex items-center gap-1.5">
                      <span class="cat-dot" :style="{ backgroundColor: cat.color }"></span>
                      <span>{{ cat.label }}</span>
                      <span class="text-xs text-muted-foreground font-mono">({{ categoryCounts[cat.key] || 0 }})</span>
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
                    <span class="font-medium">全部收藏</span>
                  </SelectItem>
                  <SelectItem value="unfavorited">
                    <span class="inline-flex items-center gap-1.5">
                      <span>未收藏</span>
                      <span class="text-xs text-muted-foreground font-mono">({{ unfavoritedCount }})</span>
                    </span>
                  </SelectItem>
                  <SelectItem value="favorited">
                    <span class="inline-flex items-center gap-1.5">
                      <span>已收藏</span>
                      <span class="text-xs text-muted-foreground font-mono">({{ favoritedCount }})</span>
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
                      <span class="text-xs text-muted-foreground font-mono">({{ cat.count }})</span>
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
                重置
              </Button>
            </div>
          </div>
          <div class="section-title">
            <h2>浏览</h2>
            <span>{{
              isFiltered
                ? `显示 ${filteredArchives.length} / ${s.archives.value.length} 个项目`
                : `${s.archives.value.length} 个项目`
            }}</span>
          </div>
          <div v-if="!s.archives.value.length" class="panel empty">
            当前没有归档项目。
          </div>
          <div v-else-if="!filteredArchives.length" class="panel empty">
            <p>没有符合当前筛选条件的归档项目。</p>
            <Button variant="link" size="sm" @click="resetFilters"
              >清除所有筛选条件</Button
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
                  :alt="a.title || '封面'"
                /><span v-else>暂无封面</span
                ><b
                  :title="
                    a.metadata?.title_jpn ||
                    a.metadata?.title ||
                    a.title ||
                    '未命名画廊'
                  "
                  ><span class="line-clamp-2 break-words">{{
                    a.metadata?.title_jpn ||
                    a.metadata?.title ||
                    a.title ||
                    "未命名画廊"
                  }}</span></b
                ><Badge variant="secondary" class="cover-status">{{
                  {
                    ready: "可用",
                    unarchived: "未归档",
                    downloading: "下载中",
                    zipping: "转换中",
                    pending: "准备中",
                    submitting: "提交中",
                    failed: "失败",
                    quoting: "报价中",
                    absent: "无",
                  }[a.status] ||
                  a.status ||
                  "未知"
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
                  >{{ a.metadata?.filecount || a.page_count || "—" }} 页<template v-if="archiveSizeText(a)">（{{ archiveSizeText(a) }}）</template></span
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
                        ? a.favcat_name || `收藏夹 ${a.favcat}`
                        : '添加到收藏夹'
                    "
                  >
                    <Bookmark class="size-3 shrink-0" />
                    <SelectValue placeholder="未收藏">
                      <span class="truncate">{{
                        a.favcat != null
                          ? a.favcat_name || `收藏夹 ${a.favcat}`
                          : "未收藏"
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
                        移除收藏
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
                  >打开 ↗</a>
                </Button>
                <Button variant="link" size="sm" @click="s.refreshMetadata(a)">更新元数据</Button
                ><Button
                  variant="link"
                  size="sm"
                  v-if="a.update_available"
                  :disabled="
                    s.busy.value ||
                    !s.status.value?.site?.has_ipb ||
                    ['submitting', 'pending', 'downloading', 'zipping'].includes(
                      a.version_update?.status,
                    )
                  "
                  @click="handleQuoteUpdate(a)"
                >更新版本</Button
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
                >{{ cardActionLabel(a) }}</Button>
              </div>
              <div
                v-if="
                  (a.status === 'ready' || a.status === 'unarchived' || a.status === 'failed') &&
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
                  <BookOpen class="size-3.5" />阅读
                </Button>
                <Button
                  v-else-if="a.status === 'unarchived'"
                  variant="default"
                  size="sm"
                  class="w-full text-xs"
                  @click="openReader(a)"
                >
                  <BookOpen class="size-3.5" />在线阅读
                </Button>
                <div v-else class="flex items-center gap-1.5 w-full">
                  <Button
                    v-if="archivePageCount(a) > 0"
                    variant="default"
                    size="sm"
                    class="flex-1 text-xs"
                    @click="openReader(a)"
                  >
                    <BookOpen class="size-3.5" />在线阅读
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    class="flex-1 text-xs"
                    :disabled="!s.status.value?.site?.has_ipb || s.busy.value"
                    @click="handleQuoteArchive(a)"
                  >
                    <RotateCcw class="size-3.5" />报价并重试
                  </Button>
                </div>
              </div>
              <small v-if="a.error || a.version_update?.error" class="archive-error">
                {{ a.error || a.version_update.error }}
              </small>
            </Card>
          </div>
          <div
            v-if="filteredArchives.length > visibleArchives.length"
            class="flex justify-center pt-4"
          >
            <Button
              variant="outline"
              size="sm"
              @click="visibleLimit += 60"
            >
              加载更多（已显示 {{ visibleArchives.length }} / {{ filteredArchives.length }}）
            </Button>
          </div></template
        >
      </div>
      <AlertDialog v-model:open="s.confirmArchive.value">
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{{ s.quoteAction.value === "update" ? "确认更新归档版本" : "确认开始归档" }}</AlertDialogTitle>
            <AlertDialogDescription>
              {{
                s.quoteAction.value === "update"
                  ? "将使用现有归档的画质档请求新版本。若该档位未解锁，可能消耗 GP，请核对报价后确认。"
                  : s.quoteAction.value === "refresh"
                  ? "重试会重新向上游请求归档。未解锁档位可能消耗 GP，请核对画质档和报价。"
                  : "归档任务会请求上游。未解锁档位会消耗你的账户 GP，请核对档位和价格。"
              }}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div v-if="s.quote.value" class="dialog-detail">
            <b>{{ s.quote.value.title || "选定画廊" }}</b>
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
            <AlertDialogCancel as-child><Button variant="outline">取消</Button></AlertDialogCancel>
            <AlertDialogAction as-child><Button :disabled="s.busy.value" @click="s.startArchive">{{ s.quoteAction.value === "update" ? "确认更新版本" : "确认开始归档" }}</Button></AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <AlertDialog v-model:open="s.confirmCache.value">
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>清理图片缓存？</AlertDialogTitle>
            <AlertDialogDescription>
              这会删除本地缓存的图片字节；下次访问时会重新从 E-Hentai 获取。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel as-child><Button variant="outline">取消</Button></AlertDialogCancel>
            <AlertDialogAction as-child><Button variant="destructive" @click="s.clearCache">清理缓存</Button></AlertDialogAction>
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
              "画廊 " + (s.deleteTarget.value?.gid || "")
            }}</b>
            <span>{{ deleteDialogMeta.note }}</span>
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel as-child><Button variant="outline">取消</Button></AlertDialogCancel>
            <AlertDialogAction as-child><Button variant="destructive" @click="s.deleteArchive">{{
              deleteDialogMeta.confirm
            }}</Button></AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <AlertDialog v-model:open="s.confirmFavoriteRemove.value">
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>移除收藏？</AlertDialogTitle>
            <AlertDialogDescription>
              {{
                s.favoriteRemoveTarget.value?.status === "unarchived"
                  ? "这会从你的 E-Hentai 收藏夹移除此画廊，并同步清理未归档的本地建档记录。"
                  : "这会从你的 E-Hentai 收藏夹移除此画廊。"
              }}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <label
            v-if="s.favoriteRemoveTarget.value?.status !== 'unarchived'"
            class="delete-local-check"
          >
            <input type="checkbox" v-model="s.deleteAfterFavoriteRemove.value" />
            同时删除本地归档（只有上游移除收藏成功后才会执行）
          </label>
          <AlertDialogFooter>
            <AlertDialogCancel as-child><Button variant="outline">取消</Button></AlertDialogCancel>
            <AlertDialogAction as-child><Button variant="destructive" @click="s.confirmFavoriteRemoval">移除收藏</Button></AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <AlertDialog v-model:open="confirmFullSyncOpen">
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>执行全量校准收藏？</AlertDialogTitle>
            <AlertDialogDescription>
              将从头遍历你的 E-Hentai 收藏页（受最大扫描页数限制），补齐遗漏建档并自动清理上游已取消收藏的「未归档」条目。此操作不会触发付费归档，但会发起较多次列表页请求。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel as-child><Button variant="outline">取消</Button></AlertDialogCancel>
            <AlertDialogAction as-child><Button :disabled="s.busy.value" @click="confirmAndRunFullSync">确认全量校准</Button></AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <AlertDialog v-model:open="createArchiveDialogOpen">
        <AlertDialogContent class="max-w-md">
          <AlertDialogHeader>
            <AlertDialogTitle>新建画廊归档</AlertDialogTitle>
            <AlertDialogDescription>
              请输入画廊 URL 或 GID/TOKEN。归档需要已配置 IPB 登录凭据。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div class="space-y-3 my-2 text-left">
            <div v-if="!s.status.value?.site?.has_ipb" class="inline-alert">
              <Alert variant="destructive">
                <CircleAlert class="size-4" />
                <AlertDescription class="text-xs">
                  未检测到完整的 IPB 登录凭据。请先在“设置”中配置登录凭据，再查询 GP 报价。
                </AlertDescription>
              </Alert>
            </div>
            <div class="flex gap-2">
              <Input
                v-model="s.archiveInput.value"
                placeholder="https://e-hentai.org/g/123456/abcdef/ 或 123456/abcdef"
                @keydown.enter.prevent="s.getQuote('start')"
                class="text-xs flex-1"
              />
              <Button
                variant="outline"
                size="sm"
                :disabled="!s.status.value?.site?.has_ipb || s.busy.value || !s.archiveInput.value.trim()"
                @click="s.getQuote('start')"
              >
                <Search class="size-3.5" />查询
              </Button>
            </div>
            <div v-if="s.quote.value" class="rounded-md border border-border bg-muted/40 p-3 space-y-2">
              <div class="font-medium text-foreground text-xs line-clamp-2">
                {{ s.quote.value.title || "报价已获取" }}
              </div>
              <div v-if="s.quote.value.gp_balance" class="text-muted-foreground text-[11px]">
                账户余额：{{ s.quote.value.gp_balance.toLocaleString() }} GP
              </div>
              <div class="flex items-center gap-2 pt-1">
                <label class="text-xs text-muted-foreground shrink-0">画质档</label>
                <Select v-model="s.selectedQuality.value">
                  <SelectTrigger class="h-8 text-xs flex-1">
                    <SelectValue placeholder="选择画质档" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem
                      v-for="o in (s.quote.value.options || []).filter(
                        (x) => x.available,
                      )"
                      :key="o.or"
                      :value="o.or"
                    >
                      {{ o.label }}{{ o.unlocked ? "（已解锁）" : "" }} ·
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
                归档任务会请求上游。未解锁档位会消耗账户 GP，请核对档位和价格。
              </p>
            </div>
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel as-child>
              <Button variant="outline" size="sm" @click="closeCreateArchiveDialog">取消</Button>
            </AlertDialogCancel>
            <AlertDialogAction as-child>
              <Button
                size="sm"
                :disabled="!s.quote.value || !s.selectedQuality.value || s.busy.value"
                @click="confirmAndStartArchive"
              >
                确认
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

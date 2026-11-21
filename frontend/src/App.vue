<script setup>
import { ref, computed, watch, onMounted, onBeforeUnmount } from "vue";
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
import IncidentTracker from "@/components/IncidentTracker.vue";
import SubscriptionsView from "@/components/SubscriptionsView.vue";
import DocumentsView from "@/components/DocumentsView.vue";
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
  ZapIcon as Zap,
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

const s = useApp();

onMounted(() => {
  s.init();
  if (typeof window !== "undefined") {
    if (!readerArchive.value && window.history.state?.reader) {
      window.history.replaceState(
        { ...window.history.state, reader: false },
        "",
        window.location.href,
      );
    }
    window.addEventListener("popstate", onReaderPopState);
  }
});
onBeforeUnmount(() => {
  s.destroy();
  if (typeof window !== "undefined") {
    window.removeEventListener("popstate", onReaderPopState);
  }
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
    [t("config.group_site")]: ["eh_site", "public_base_url", "pse_page_base"],
    [t("config.group_auth")]: ["ipb_member_id", "ipb_pass_hash", "igneous"],
    [t("config.group_features")]: [
      "tag_status_filter",
      "tag_translation_enabled",
      "comments_enabled",
      "image_cache_enabled",
      "cache_max_gb",
    ],
    [t("config.group_archive")]: [
      "archive_update_interval_days",
      "archive_auto_update",
    ],
  };
  return Object.entries(map)
    .map(([title, keys]) => ({
      title,
      fields: keys.filter((k) => f[k]).map((k) => [k, f[k]]),
    }))
    .filter((x) => x.fields.length);
});
const labels = new Proxy(
  {},
  {
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
        favorites_sync_interval_seconds: t(
          "config.label_favorites_sync_interval_seconds",
        ),
        favorites_sync_categories: t("config.label_favorites_sync_categories"),
        favorites_sync_archive: t("config.label_favorites_sync_archive"),
        archive_update_interval_days: t(
          "config.label_archive_update_interval_days",
        ),
        archive_auto_update: t("config.label_archive_auto_update"),
      };
      return map[k] || k;
    },
  },
);
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
  if (tier === "org" || tier.includes("original"))
    return t("archive.tier_original");
  if (tier === "res" || tier.includes("resample"))
    return t("archive.tier_resample");
  if (entry.status === "unarchived") return t("archive.tier_online");
  return t("archive.tier_archived");
};
const readerArchive = ref(null);
const lastReaderArchive = ref(null);
let isClosingReader = false;

function closeReader(fromHistory = false) {
  if (!readerArchive.value && !isClosingReader) return;
  const hadArchive = Boolean(readerArchive.value);
  readerArchive.value = null;
  if (
    !fromHistory &&
    !isClosingReader &&
    hadArchive &&
    typeof window !== "undefined" &&
    window.history.state?.reader
  ) {
    isClosingReader = true;
    try {
      window.history.back();
    } finally {
      setTimeout(() => {
        isClosingReader = false;
      }, 80);
    }
  }
}

const readerOpen = computed({
  get: () => Boolean(readerArchive.value),
  set: (v) => {
    if (!v) closeReader();
  },
});

function onReaderPopState(e) {
  if (readerArchive.value && !e.state?.reader) {
    isClosingReader = false;
    closeReader(true);
  } else if (
    !readerArchive.value &&
    e.state?.reader &&
    lastReaderArchive.value
  ) {
    isClosingReader = false;
    readerArchive.value = lastReaderArchive.value;
  }
}
const psePageBase = computed(() => {
  const base = Number(s.status.value?.site?.pse_page_base);
  return Number.isFinite(base) ? base : 1;
});
function archivePageCount(a) {
  return Math.max(0, Number(a?.page_count || a?.metadata?.filecount || 0));
}

function canReadOnline(a) {
  if (!a) return false;
  if (a.loading) return true;
  if (archivePageCount(a) <= 0) return false;
  if (!a.status) return true;
  return ["ready", "unarchived", "failed"].includes(a.status);
}

function openReader(a) {
  if (!canReadOnline(a)) return;
  lastReaderArchive.value = a;
  readerArchive.value = a;
  if (typeof window !== "undefined") {
    window.history.pushState(
      {
        page: s.page.value,
        subRoute: s.exploreSubRoute.value
          ? { ...s.exploreSubRoute.value }
          : null,
        reader: true,
      },
      "",
      window.location.href,
    );
  }
}

function openDocumentReader(doc) {
  if (!doc?.id || Number(doc.page_count || 0) <= 0) return;
  openReader({ ...doc, document_id: doc.id, status: "ready" });
}

async function handleExploreArchive(pub) {
  await handleQuoteArchive({
    gid: pub.gid,
    token: pub.token,
    title: pub.title,
    status: pub.archiveItem?.status || "unarchived",
  });
}

async function handleExploreFavorite(payload) {
  const pub = payload?.pub || payload;
  const action = payload?.action;
  const favcat = payload?.favcat;

  const item = s.archives.value?.find(
    (a) =>
      (a.gid === pub.gid || a.active_gid === pub.gid) &&
      (a.token === pub.token || a.active_token === pub.token),
  );

  if (action === "remove") {
    const target = item || {
      gid: pub.gid,
      token: pub.token,
      title: pub.title,
      status: "unarchived",
    };
    s.requestFavoriteRemove(target);
    return;
  }

  const effectiveAction = action || (item?.is_favorited ? "move" : "add");
  const effectiveFavcat = favcat != null ? Number(favcat) : 0;
  await s.favorite(pub, effectiveAction, effectiveFavcat);
}

async function handleQuoteArchive(a) {
  await s.quoteArchiveFor(a);
  if (s.quote.value) s.confirmArchive.value = true;
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
      <AppSidebarNav
        :page="s.page.value"
        @navigate="(p) => s.route(p, 'replace')"
      />
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
            <template
              v-if="s.page.value === 'explore' && s.exploreSubRoute.value"
            >
              <BreadcrumbItem>
                <BreadcrumbLink
                  as="button"
                  type="button"
                  @click="s.exitExploreSubRoute(true)"
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
                          : s.page.value === "subscriptions"
                            ? t("nav.subscriptions")
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
                  <span>{{
                    t("dashboard.hits", {
                      count: s.status.value?.memory_cache?.hits ?? 0,
                    })
                  }}</span>
                  <span>{{
                    t("dashboard.misses", {
                      count: s.status.value?.memory_cache?.misses ?? 0,
                    })
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
                      s.status.value?.disk_cache?.enabled
                        ? t("dashboard.enabled")
                        : t("dashboard.disabled")
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
                  <span>{{
                    t("dashboard.cache_entries_count", {
                      count: s.status.value?.disk_cache?.entries ?? 0,
                    })
                  }}</span>
                  <span>{{
                    formatBytes(s.status.value?.disk_cache?.bytes ?? 0)
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
                  >{{ t("dashboard.cache_hit_rate") }}</CardTitle
                >
                <CardAction
                  ><Zap class="size-4 text-muted-foreground"
                /></CardAction>
              </CardHeader>
              <CardContent>
                <div class="flex items-baseline justify-between">
                  <div
                    class="text-2xl font-bold tracking-tight font-mono text-foreground"
                  >
                    {{
                      Number(s.status.value?.cache_stats?.hits || 0) +
                        Number(s.status.value?.cache_stats?.misses || 0) >
                      0
                        ? `${s.status.value?.cache_stats?.hit_rate ?? 0}%`
                        : "—"
                    }}
                  </div>
                  <span
                    v-if="
                      Number(s.status.value?.cache_stats?.hits || 0) +
                        Number(s.status.value?.cache_stats?.misses || 0) >
                      0
                    "
                    class="text-xs font-medium text-emerald-600 dark:text-emerald-400 font-mono"
                  >
                    {{ s.status.value?.cache_stats?.hit_rate ?? 0 }}%
                  </span>
                </div>
                <div
                  class="w-full bg-muted rounded-full h-1.5 mt-2.5 overflow-hidden"
                >
                  <div
                    class="bg-emerald-500 h-full rounded-full transition-all duration-300"
                    :style="{
                      width: `${Math.min(100, Math.max(0, Number(s.status.value?.cache_stats?.hit_rate || 0)))}%`,
                    }"
                  ></div>
                </div>
                <div
                  class="flex items-center justify-between mt-2 text-xs text-muted-foreground font-mono"
                >
                  <span>{{
                    t("dashboard.hits", {
                      count: s.status.value?.cache_stats?.hits ?? 0,
                    })
                  }}</span>
                  <span>{{
                    t("dashboard.misses", {
                      count: s.status.value?.cache_stats?.misses ?? 0,
                    })
                  }}</span>
                </div>
              </CardContent>
            </Card>
          </div>

          <IncidentTracker class="mb-5" />

          <div class="columns">
            <Card class="shadow-xs">
              <CardHeader class="border-b border-border/50 pb-3">
                <CardTitle class="text-base font-semibold text-foreground">{{
                  t("dashboard.runtime_details")
                }}</CardTitle>
                <CardDescription class="text-xs text-muted-foreground">{{
                  t("dashboard.runtime_desc")
                }}</CardDescription>
              </CardHeader>
              <CardContent class="pt-2">
                <div class="divide-y divide-border/50 text-sm">
                  <div class="flex items-center justify-between py-2.5">
                    <span class="text-muted-foreground text-xs font-medium">{{
                      t("dashboard.version")
                    }}</span>
                    <Badge variant="secondary" class="font-mono text-xs">
                      v{{ s.status.value?.app?.version || "—" }}
                    </Badge>
                  </div>
                  <div class="flex items-center justify-between py-2.5">
                    <span class="text-muted-foreground text-xs font-medium">{{
                      t("dashboard.external_url")
                    }}</span>
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
                    <span class="text-muted-foreground text-xs font-medium">{{
                      t("dashboard.ipb_status")
                    }}</span>
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
                    <span class="text-muted-foreground text-xs font-medium">{{
                      t("dashboard.max_concurrency")
                    }}</span>
                    <span class="font-mono text-xs font-medium text-foreground">
                      {{
                        s.status.value?.throttle?.max_concurrency != null
                          ? t("dashboard.threads", {
                              count: s.status.value.throttle.max_concurrency,
                            })
                          : "—"
                      }}
                    </span>
                  </div>
                  <div class="flex items-center justify-between py-2.5">
                    <span class="text-muted-foreground text-xs font-medium">{{
                      t("dashboard.html_interval")
                    }}</span>
                    <span class="font-mono text-xs font-medium text-foreground">
                      {{
                        s.status.value?.throttle?.html_interval_seconds != null
                          ? t("dashboard.seconds_unit", {
                              count:
                                s.status.value.throttle.html_interval_seconds,
                            })
                          : "—"
                      }}
                    </span>
                  </div>
                  <div
                    v-if="s.status.value?.circuit?.state === 'open'"
                    class="flex items-center justify-between py-2.5 text-destructive"
                  >
                    <span class="text-xs font-medium">{{
                      t("dashboard.circuit_reason")
                    }}</span>
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
                    <CardTitle
                      class="text-base font-semibold text-foreground"
                      >{{ t("dashboard.home_layout") }}</CardTitle
                    >
                    <CardDescription class="text-xs text-muted-foreground">{{
                      t("dashboard.home_layout_desc")
                    }}</CardDescription>
                  </div>
                  <Button
                    variant="ghost"
                    size="xs"
                    class="h-7 text-xs gap-1"
                    @click="s.route('home', 'replace')"
                  >
                    {{ t("dashboard.enter_editor") }}
                    <ArrowUpRight :size="12" />
                  </Button>
                </div>
              </CardHeader>
              <CardContent class="pt-2">
                <div class="divide-y divide-border/50 text-sm">
                  <div class="flex items-center justify-between py-2.5">
                    <span class="text-muted-foreground text-xs font-medium">{{
                      t("dashboard.content_source")
                    }}</span>
                    <Badge variant="outline" class="font-mono text-xs">
                      {{
                        s.status.value?.home?.using_file
                          ? "home.toml"
                          : t("dashboard.builtin_default")
                      }}
                    </Badge>
                  </div>
                  <div class="flex items-center justify-between py-2.5">
                    <span class="text-muted-foreground text-xs font-medium">{{
                      t("dashboard.config_path")
                    }}</span>
                    <span
                      class="font-mono text-xs text-muted-foreground truncate max-w-[260px]"
                      :title="s.status.value?.home?.path"
                    >
                      {{ s.status.value?.home?.path || "—" }}
                    </span>
                  </div>
                  <div class="flex items-center justify-between py-2.5">
                    <span class="text-muted-foreground text-xs font-medium">{{
                      t("dashboard.groups_sections")
                    }}</span>
                    <span class="font-mono text-xs font-medium text-foreground">
                      {{
                        t("dashboard.groups_sections_count", {
                          groups: s.status.value?.home?.groups ?? 0,
                          sections: s.status.value?.home?.sections ?? 0,
                        })
                      }}
                    </span>
                  </div>
                  <div class="flex items-center justify-between py-2.5">
                    <span class="text-muted-foreground text-xs font-medium">{{
                      t("dashboard.opds_versions")
                    }}</span>
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
                    <span class="text-muted-foreground text-xs font-medium">{{
                      t("dashboard.heartbeat")
                    }}</span>
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
            @archive-unlink="s.unlinkArchive"
            @archive-delete="s.requestDelete"
            @archive-manage="s.route('archive')"
            @favorite-gallery="handleExploreFavorite"
          />
        </template>
        <template v-if="s.page.value === 'subscriptions'">
          <SubscriptionsView
            @read-gallery="openReader"
            @archive-gallery="handleExploreArchive"
            @archive-unlink="s.unlinkArchive"
            @archive-delete="s.requestDelete"
            @archive-manage="s.route('archive')"
            @favorite-gallery="handleExploreFavorite"
          />
        </template>
        <template v-if="s.page.value === 'archive'">
          <DocumentsView @read-document="openDocumentReader" />
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
              <CardTitle role="heading" aria-level="2">{{
                t("config.readonly_title")
              }}</CardTitle>
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
                <CardDescription>{{
                  t("config.saved_pending_restart")
                }}</CardDescription>
              </div>
            </CardHeader>
            <CardContent class="form-panel-content">
              <div v-for="[key, f] in group.fields" :key="key" class="field">
                <div>
                  <label :for="key"
                    >{{ labels[key] || key }}
                    <Badge
                      v-if="f.pending"
                      variant="outline"
                      class="badge-warn"
                      >{{ t("config.pending_restart") }}</Badge
                    ></label
                  ><small>{{
                    f.locked
                      ? lockedReason(key)
                      : f.source === "env"
                        ? t("config.source_env")
                        : f.source === "file"
                          ? t("config.source_file")
                          : t("config.source_default")
                  }}</small>
                  <small v-if="key === 'archive_update_interval_days'">{{
                    t("config.hint_archive_update_interval_days")
                  }}</small>
                  <small v-if="key === 'archive_auto_update'">{{
                    t("config.hint_archive_auto_update")
                  }}</small>
                  <small v-if="key === 'favorites_sync_interval_seconds'">{{
                    t("config.hint_favorites_sync_interval_seconds")
                  }}</small>
                  <small v-if="key === 'favorites_sync_categories'">{{
                    t("config.hint_favorites_sync_categories")
                  }}</small>
                  <small v-if="key === 'favorites_sync_archive'">{{
                    t("config.hint_favorites_sync_archive")
                  }}</small>
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
                      <SelectValue
                        :placeholder="t('config.select_pse_page_base')"
                      />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="1">{{
                        t("config.pse_page_base_1")
                      }}</SelectItem>
                      <SelectItem value="0">{{
                        t("config.pse_page_base_0")
                      }}</SelectItem>
                    </SelectContent>
                  </Select>
                  <Select
                    v-if="key === 'tag_status_filter'"
                    :model-value="String(s.form.value[key] ?? '')"
                    :disabled="f.locked"
                    @update:model-value="edit(key, $event)"
                  >
                    <SelectTrigger :id="key" class="control-select">
                      <SelectValue
                        :placeholder="t('config.select_tag_filter')"
                      />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="balanced">{{
                        t("config.tag_filter_balanced")
                      }}</SelectItem>
                      <SelectItem value="strict">{{
                        t("config.tag_filter_strict")
                      }}</SelectItem>
                      <SelectItem value="off">{{
                        t("config.tag_filter_off")
                      }}</SelectItem>
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
                  /><Badge v-if="f.locked" variant="outline" class="locked">{{
                    t("config.locked")
                  }}</Badge
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
                        ? f.set
                          ? t("config.state_set")
                          : t("config.state_unset")
                        : (f.active ?? t("config.state_unset_parentheses")),
                    })
                  }}
                  ·
                  {{
                    t("config.saved_val", {
                      val: secret(key)
                        ? f.saved_set
                          ? t("config.state_set")
                          : t("config.state_unset")
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
              ><Button variant="outline">{{
                t("common.cancel")
              }}</Button></AlertDialogCancel
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
            <AlertDialogTitle>{{
              t("archive.dialog_clear_cache_title")
            }}</AlertDialogTitle>
            <AlertDialogDescription>
              {{ t("archive.dialog_clear_cache_desc") }}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel as-child
              ><Button variant="outline">{{
                t("common.cancel")
              }}</Button></AlertDialogCancel
            >
            <AlertDialogAction as-child
              ><Button variant="destructive" @click="s.clearCache">{{
                t("dashboard.clear_cache")
              }}</Button></AlertDialogAction
            >
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog v-model:open="s.confirmFavoriteRemove.value">
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{{
              t("archive.dialog_fav_remove_title")
            }}</AlertDialogTitle>
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
              ><Button variant="outline">{{
                t("common.cancel")
              }}</Button></AlertDialogCancel
            >
            <AlertDialogAction as-child
              ><Button
                variant="destructive"
                @click="s.confirmFavoriteRemoval"
                >{{ t("archive.remove_fav") }}</Button
              ></AlertDialogAction
            >
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <ArchiveReader
        v-model:open="readerOpen"
        :archive="readerArchive"
        :incognito="Boolean(readerArchive?.incognito)"
        :pse-page-base="psePageBase"
        :theme="s.theme.value"
      />
    </SidebarInset>
  </SidebarProvider>
</template>

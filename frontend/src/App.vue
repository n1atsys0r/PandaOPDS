<script setup>
import { computed, onMounted, onBeforeUnmount } from "vue";
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
import LoginForm from "@/components/LoginForm.vue";
import ThemeAutoIcon from "@/components/ThemeAutoIcon.vue";
import {
  SunIcon as Sun,
  MoonIcon as Moon,
  MonitorIcon as Monitor,
  RefreshCwIcon as RefreshCw,
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
} from "@lucide/vue";
import { useApp } from "./state.js";

function formatBytes(bytes) {
  if (!bytes || bytes === 0) return "0 B";
  const k = 1024;
  const sizes = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + " " + sizes[i];
}
const s = useApp();
onMounted(s.init);
onBeforeUnmount(s.destroy);

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
      "favorites_sync_interval_seconds",
    ],
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
};
const secret = (k) => ["ipb_member_id", "ipb_pass_hash", "igneous"].includes(k);
const boolean = (k) =>
  [
    "tag_translation_enabled",
    "comments_enabled",
    "image_cache_enabled",
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
    }[key] || key.toUpperCase();
  return env + " 环境变量控制此项。";
};
const qualityLabel = (entry) => {
  const tier = String(entry.or || entry.quality || "").toLowerCase();
  if (tier === "org" || tier.includes("original")) return "原档";
  if (tier === "res" || tier.includes("resample")) return "降采样";
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
</script>
<template>
  <div
    v-if="s.authRequired.value"
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
                  : "归档与收藏"
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
                      : key === 'cache_max_gb'
                        ? 'number'
                        : 'text'
                  "
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
        <template v-if="s.page.value === 'archive'"
          ><div class="heading">
            <div>
              <small>本地存档</small>
              <h1>归档与收藏</h1>
              <p>归档操作可能消耗账户 GP；每次开始前会明确确认。</p>
            </div>
          </div>
          <div class="archive-stats">
            <Badge variant="secondary"
              >归档项目
              {{
                s.archiveStats.value.entries ?? s.archives.value.length
              }}</Badge
            >
            <Badge
              v-for="(count, status) in s.archiveStats.value.by_status || {}"
              :key="status"
              variant="outline"
              >{{ status }} · {{ count }}</Badge
            >
          </div>
          <Card class="panel">
            <CardHeader class="panel-header">
              <CardTitle role="heading" aria-level="2">新建归档</CardTitle>
              <CardDescription>
              输入画廊 URL、archiver URL 或 gid token。归档需要已配置 IPB
              登录态和星会员资格。
              </CardDescription>
            </CardHeader>
            <CardContent class="panel-body">
            <div
              v-if="!s.status.value?.site?.has_ipb"
              class="inline-alert"
            >
              <Alert variant="destructive" class="credential-alert">
                <CircleAlert />
                <AlertDescription>
                  未检测到完整的 IPB 登录凭据。请先在“设置”中配置登录凭据，再查询 GP 报价。
                </AlertDescription>
              </Alert>
            </div>
            <div class="create">
              <Input
                v-model="s.archiveInput.value"
                placeholder="https://e-hentai.org/g/123456/abcdef/ 或 gid token"
              /><Button
                variant="outline"
                :disabled="!s.status.value?.site?.has_ipb || s.busy.value"
                @click="s.getQuote"
              >
                <Search />查询报价
              </Button>
            </div>
            <div v-if="s.quote.value" class="quote">
              <b>{{ s.quote.value.title || "报价已获取（不会扣除 GP）" }}</b
              ><span v-if="s.quote.value.gp_balance">
                · 账户余额
                {{ s.quote.value.gp_balance.toLocaleString() }} GP</span
              >
              <div class="quote-pick">
                <label class="quality-label">
                  <span>画质档</span>
                  <Select v-model="s.selectedQuality.value">
                    <SelectTrigger class="quality-select">
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
                </label>
                <Button
                  @click="s.openArchiveConfirm"
                  :disabled="!s.selectedQuality.value"
                  >确认档位并开始</Button
                >
              </div>
              <small v-if="s.quote.value.options?.some((o) => !o.available)"
                >不可用档：{{
                  s.quote.value.options
                    .filter((o) => !o.available)
                    .map((o) => o.label)
                    .join("、")
                }}</small
              >
            </div>
            </CardContent>
          </Card>
          <div class="section-title">
            <h2>归档图库</h2>
            <span>{{ s.archives.value.length }} 个项目</span>
          </div>
          <div v-if="!s.archives.value.length" class="panel empty">
            当前没有归档项目。
          </div>
          <div class="gallery">
            <Card
              v-for="a in s.archives.value"
              :key="a.gid + '/' + a.token"
              class="gallery-card"
            >
              <div class="cover">
                <img
                  v-if="a.cover_url"
                  :src="a.cover_url"
                  :alt="a.title || '封面'"
                  loading="lazy"
                /><span v-if="!a.cover_url">暂无封面</span
                ><b>{{
                  a.metadata?.title_jpn ||
                  a.metadata?.title ||
                  a.title ||
                  "未命名画廊"
                }}</b
                ><Badge variant="secondary" class="cover-status">{{
                  {
                    ready: "可用",
                    downloading: "下载中",
                    zipping: "转换中",
                    pending: "准备中",
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
              <div class="gallery-meta">
                {{ a.metadata?.filecount || a.page_count || "—" }} 页
                <span>{{
                  a.metadata?.filesize || a.archive_size || a.part_size || "—"
                }}</span>
              </div>
              <div class="gallery-actions">
                <Button as-child variant="link" size="sm">
                <a
                  :href="
                    'https://e-hentai.org/g/' + a.gid + '/' + a.token + '/'
                  "
                  target="_blank"
                  rel="noopener noreferrer"
                  >打开 ↗</a>
                </Button>
                <Button variant="link" size="sm" @click="s.refreshMetadata(a)">更新元数据</Button
                ><Button
                  variant="link"
                  size="sm"
                  v-if="a.status === 'failed'"
                  @click="s.quoteArchiveFor(a)"
                >
                  报价并重试</Button
                ><Button variant="destructive" size="sm" @click="s.requestDelete(a)">删除归档</Button>
              </div>
              <div class="gallery-actions">
                <Select
                  :model-value="String(a.favcat ?? '')"
                  @update:model-value="
                    s.favorite(
                      a,
                      a.is_favorited ? 'move' : 'add',
                      Number($event),
                    )
                  "
                >
                  <SelectTrigger class="favorite-select">
                    <SelectValue placeholder="选择收藏分类" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem v-for="cat in s.favcats.value" :key="cat.id" :value="String(cat.id)">
                      {{ cat.id }}: {{ cat.name }}
                    </SelectItem>
                  </SelectContent>
                </Select>
                <Button
                  variant="destructive"
                  size="sm"
                  v-if="a.is_favorited"
                  @click="s.requestFavoriteRemove(a)"
                >
                  移除收藏
                </Button>
              </div>
              <small v-if="a.error" class="archive-error">{{ a.error }}</small>
            </Card>
          </div></template
        >
      </div>
      <AlertDialog v-model:open="s.confirmArchive.value">
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认开始归档</AlertDialogTitle>
            <AlertDialogDescription>
              {{
                s.quoteAction.value === "refresh"
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
            <AlertDialogAction as-child><Button @click="s.startArchive">确认开始归档</Button></AlertDialogAction>
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
            <AlertDialogTitle>删除本地归档？</AlertDialogTitle>
            <AlertDialogDescription>
              仅删除本地归档文件；E-Hentai 上的归档记录保留，之后可重新下载。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div class="dialog-detail">
            <b>{{
              s.deleteTarget.value?.metadata?.title_jpn ||
              s.deleteTarget.value?.title ||
              "画廊 " + (s.deleteTarget.value?.gid || "")
            }}</b>
            <span>操作只影响本机保存的归档文件</span>
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel as-child><Button variant="outline">取消</Button></AlertDialogCancel>
            <AlertDialogAction as-child><Button variant="destructive" @click="s.deleteArchive">删除归档</Button></AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <AlertDialog v-model:open="s.confirmFavoriteRemove.value">
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>移除收藏？</AlertDialogTitle>
            <AlertDialogDescription>
              这会从你的 E-Hentai 收藏夹移除此画廊。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <label class="delete-local-check">
            <input type="checkbox" v-model="s.deleteAfterFavoriteRemove.value" />
            同时删除本地归档（只有上游移除收藏成功后才会执行）
          </label>
          <AlertDialogFooter>
            <AlertDialogCancel as-child><Button variant="outline">取消</Button></AlertDialogCancel>
            <AlertDialogAction as-child><Button variant="destructive" @click="s.confirmFavoriteRemoval">移除收藏</Button></AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SidebarInset>
  </SidebarProvider>
</template>

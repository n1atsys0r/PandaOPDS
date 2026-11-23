<script setup>
import { computed } from "vue";
import { t, currentLocale } from "@/i18n.js";
import { formatRelativeTime } from "@/subscription-view.js";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  BookmarkCheck,
  Layers,
  RefreshCw,
  Settings,
  SlidersHorizontal,
  ChevronsLeft,
  Search,
  Flame,
  Eye,
  TrendingUp,
  Bookmark,
  Sparkles,
} from "@lucide/vue";

const props = defineProps({
  subscriptions: {
    type: Array,
    default: () => [],
  },
  selectedId: {
    type: String,
    default: "",
  },
  isPinned: {
    type: Boolean,
    default: true,
  },
});

const emit = defineEmits([
  "select",
  "open-settings",
  "open-all-filter",
  "toggle-pin",
]);

const visibleCount = computed(
  () => props.subscriptions.filter((x) => !x.exclude_from_all).length,
);

const allBadgeText = computed(() => {
  if (visibleCount.value === props.subscriptions.length) {
    return String(props.subscriptions.length);
  }
  return `${visibleCount.value}/${props.subscriptions.length}`;
});

function iconForSub(sub) {
  if (sub.source === "search") return Search;
  const p = sub.query?.preset || "latest";
  if (p === "popular") return Flame;
  if (p === "watched") return Eye;
  if (p.startsWith("toplist:")) return TrendingUp;
  if (p === "favorites" || p.startsWith("favorites:")) return Bookmark;
  return Sparkles;
}
</script>

<template>
  <div class="panel flex flex-col gap-0 border border-border/60 rounded-2xl bg-card shadow-xs">
    <!-- Header -->
    <div
      class="p-4 pb-3 flex flex-row items-center justify-between gap-2 border-b border-border/60"
    >
      <div class="flex items-center gap-2 min-w-0">
        <BookmarkCheck class="size-4 text-primary shrink-0" />
        <span class="font-semibold text-sm truncate">{{
          t("subscription.list_title")
        }}</span>
        <Badge
          variant="secondary"
          class="h-5 px-1.5 text-[11px] font-mono font-normal shrink-0"
        >
          {{ subscriptions.length }}
        </Badge>
      </div>

      <!-- Collapse Sidebar Button -->
      <Button
        v-if="isPinned"
        variant="ghost"
        size="icon-xs"
        class="size-7 text-muted-foreground hover:text-foreground cursor-pointer shrink-0 rounded-lg"
        :title="t('subscription.unpin_sidebar')"
        @click="emit('toggle-pin')"
      >
        <ChevronsLeft class="size-4" />
      </Button>
    </div>

    <!-- Items List -->
    <div class="p-2 flex flex-col gap-1 overflow-y-auto">
      <!-- Pinned All Subscriptions Item (Single-line) -->
      <div
        v-if="subscriptions.length > 1"
        class="group relative flex items-center justify-between gap-2 px-3 py-2.5 rounded-xl cursor-pointer transition-colors select-none border border-transparent"
        :class="
          selectedId === 'all'
            ? 'bg-primary/10 border-primary/20 text-primary font-medium shadow-xs'
            : 'hover:bg-muted/60 text-muted-foreground hover:text-foreground'
        "
        @click="emit('select', 'all')"
      >
        <div class="flex items-center gap-2.5 min-w-0 flex-1">
          <Layers class="size-4 shrink-0 text-primary" />
          <span class="truncate text-sm font-medium">
            {{ t("subscription.all_subscriptions") }}
          </span>
        </div>

        <div class="flex items-center gap-1.5 shrink-0">
          <RefreshCw
            v-if="subscriptions.some((x) => !x.exclude_from_all && x.running)"
            class="size-3.5 shrink-0 text-primary animate-spin"
          />
          <Badge
            variant="secondary"
            class="h-4.5 px-1.5 text-[10px] font-mono font-normal"
          >
            {{ allBadgeText }}
          </Badge>
          <Button
            variant="ghost"
            size="icon-xs"
            class="size-6 rounded-md text-muted-foreground hover:text-foreground opacity-50 hover:opacity-100 transition-opacity cursor-pointer"
            :title="t('subscription.all_filter_settings')"
            @click.stop="emit('open-all-filter')"
          >
            <SlidersHorizontal class="size-3.5" />
          </Button>
        </div>
      </div>

      <!-- Tiled Individual Subscriptions (Single-line Title + Status) -->
      <div
        v-for="sub in subscriptions"
        :key="sub.id"
        class="group relative flex items-center justify-between gap-2 px-3 py-2.5 rounded-xl cursor-pointer transition-colors select-none border border-transparent"
        :class="
          selectedId === sub.id
            ? 'bg-primary/10 border-primary/20 text-primary font-medium shadow-xs'
            : 'hover:bg-muted/60 text-muted-foreground hover:text-foreground'
        "
        @click="emit('select', sub.id)"
      >
        <div class="flex items-center gap-2.5 min-w-0 flex-1">
          <component
            :is="iconForSub(sub)"
            class="size-4 shrink-0"
            :class="
              sub.query?.preset?.startsWith('favorites')
                ? 'text-primary'
                : sub.source === 'preset'
                  ? 'text-amber-500'
                  : 'text-blue-500'
            "
          />

          <!-- Single-line Subscription Name & Disabled Indicator -->
          <div class="flex items-center gap-1.5 min-w-0 flex-1">
            <span class="truncate text-sm font-medium">
              {{ sub.name }}
            </span>
            <span
              v-if="!sub.enabled"
              class="size-1.5 rounded-full bg-muted-foreground/50 shrink-0"
            />
          </div>
        </div>

        <!-- Right Side: Status (Spinner / Error / Relative Time) & Settings Action -->
        <div class="flex items-center gap-1 shrink-0">
          <span
            v-if="sub.running"
            class="text-primary flex items-center shrink-0"
            :title="t('subscription.running')"
          >
            <RefreshCw class="size-3 animate-spin" />
          </span>
          <span
            v-else-if="sub.last_error"
            class="size-1.5 rounded-full bg-destructive shrink-0"
            :title="sub.last_error"
          />
          <span
            v-else-if="sub.last_run_at"
            class="text-xs text-muted-foreground/70 shrink-0"
            :title="sub.last_run_at"
          >
            {{ formatRelativeTime(sub.last_run_at, currentLocale) }}
          </span>

          <Button
            variant="ghost"
            size="icon-xs"
            class="size-6 rounded-md text-muted-foreground hover:text-foreground opacity-50 hover:opacity-100 transition-opacity hover:bg-background/80 cursor-pointer"
            :title="t('subscription.settings')"
            @click.stop="emit('open-settings', sub)"
          >
            <Settings class="size-3.5" />
          </Button>
        </div>
      </div>

      <div
        v-if="!subscriptions.length"
        class="p-4 text-center text-xs text-muted-foreground"
      >
        {{ t("subscription.no_subscriptions") }}
      </div>
    </div>
  </div>
</template>

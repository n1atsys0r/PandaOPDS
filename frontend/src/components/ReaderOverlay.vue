<script setup>
import { ref, watch, nextTick } from "vue";
import {
  X,
  BookOpen,
  ScrollText,
  HatGlasses,
  LayoutGrid,
  Loader2,
} from "@lucide/vue";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import { Slider } from "@/components/ui/slider";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { t } from "@/i18n.js";

const props = defineProps({
  open: { type: Boolean, default: true },
  title: { type: String, default: "" },
  pageIndex: { type: Number, default: 0 },
  pageCount: { type: Number, default: 0 },
  readerMode: { type: String, default: "paged" }, // 'paged' | 'webtoon'
  isIncognito: { type: Boolean, default: false },
  pageMode: { type: String, default: "single" }, // 'single' | 'double'
  readingRTL: { type: Boolean, default: true },
  zoom: { type: Number, default: 1 },
  resolveThumb: { type: Function, default: null },
});

const emit = defineEmits([
  "update:readerMode",
  "update:pageIndex",
  "update:pageMode",
  "update:readingRTL",
  "close",
  "turn",
  "toggle-zoom",
  "cancel-thumbs",
]);

const thumbnailsOpen = ref(false);
const thumbUrls = ref({});
const thumbGridEl = ref(null);
let thumbObserver = null;
let pendingThumbTimers = new Map();

function cancelAllThumbTimers() {
  for (const timer of pendingThumbTimers.values()) {
    clearTimeout(timer);
  }
  pendingThumbTimers.clear();
}

function observeThumbElements() {
  thumbObserver?.disconnect();
  thumbObserver = null;
  cancelAllThumbTimers();
  if (!thumbnailsOpen.value || !thumbGridEl.value || !props.resolveThumb)
    return;

  thumbObserver = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        const idx = Number(entry.target.getAttribute("data-page-index"));
        if (!Number.isInteger(idx)) continue;

        if (entry.isIntersecting) {
          if (thumbUrls.value[idx]) {
            thumbObserver?.unobserve(entry.target);
            continue;
          }
          if (pendingThumbTimers.has(idx)) continue;
          // 60ms debounce for rapid scrolling
          const timer = setTimeout(() => {
            pendingThumbTimers.delete(idx);
            thumbObserver?.unobserve(entry.target);
            if (!thumbnailsOpen.value || thumbUrls.value[idx]) return;
            props
              .resolveThumb(idx)
              .then((url) => {
                if (url) {
                  thumbUrls.value = { ...thumbUrls.value, [idx]: url };
                }
              })
              .catch(() => {});
          }, 60);
          pendingThumbTimers.set(idx, timer);
        } else {
          // If scrolled out of view within 60ms, cancel to prevent wasted fetch
          if (pendingThumbTimers.has(idx)) {
            clearTimeout(pendingThumbTimers.get(idx));
            pendingThumbTimers.delete(idx);
          }
        }
      }
    },
    {
      root: thumbGridEl.value,
      rootMargin: "80px",
    },
  );

  const placeholders = thumbGridEl.value.querySelectorAll("[data-page-index]");
  placeholders.forEach((el) => thumbObserver.observe(el));
}

watch(
  thumbnailsOpen,
  async (open) => {
    if (open) {
      await nextTick();
      observeThumbElements();
      // Scroll current active thumbnail into view
      const activeEl = thumbGridEl.value?.querySelector(
        `[data-page-index="${props.pageIndex}"]`,
      );
      activeEl?.scrollIntoView({ block: "nearest", behavior: "smooth" });
    } else {
      cancelAllThumbTimers();
      thumbObserver?.disconnect();
      thumbObserver = null;
      emit("cancel-thumbs");
    }
  },
  { flush: "post" },
);

watch(
  () => props.open,
  (val) => {
    if (!val) {
      thumbnailsOpen.value = false;
      cancelAllThumbTimers();
      emit("cancel-thumbs");
    }
  },
);

function onThumbClick(idx) {
  emit("update:pageIndex", idx);
  thumbnailsOpen.value = false;
}

function onSliderValueChange(vals) {
  const val = vals?.[0];
  if (typeof val === "number") {
    emit("update:pageIndex", val - 1);
  }
}
</script>

<template>
  <!-- Top Floating Pill Island -->
  <Transition name="reader-pill-top">
    <div
      v-if="open"
      class="fixed top-3 sm:top-5 inset-x-3 sm:inset-x-6 z-[1020] pointer-events-none flex justify-center select-none"
    >
      <header
        class="pointer-events-auto flex h-12 sm:h-13 w-full max-w-4xl items-center justify-between gap-3 rounded-2xl sm:rounded-full border border-border/70 bg-background/85 px-3.5 sm:px-5 py-1.5 text-foreground backdrop-blur-xl shadow-lg ring-1 ring-black/5 dark:ring-white/10"
        @click.stop
      >
        <!-- Left: Title, incognito badge -->
        <div class="flex min-w-0 items-center gap-2 sm:gap-3">
          <span
            class="truncate text-xs sm:text-sm font-medium max-w-[200px] sm:max-w-md md:max-w-lg lg:max-w-xl"
            :title="title"
          >
            {{ title }}
          </span>
          <Badge
            v-if="isIncognito"
            variant="outline"
            class="hidden sm:inline-flex shrink-0 items-center gap-1 text-[11px] rounded-full border-border/70 text-muted-foreground px-2 py-0.5"
            :title="t('reader.incognito_notice')"
          >
            <HatGlasses class="size-3 text-muted-foreground" />
            <span>{{ t("reader.incognito_badge") }}</span>
          </Badge>
        </div>

        <!-- Right: Reader mode switcher & close button -->
        <div class="flex shrink-0 items-center gap-2">
          <!-- Segmented ToggleGroup (Shadcn Luma compliant) -->
          <ToggleGroup
            type="single"
            :model-value="readerMode"
            class="bg-muted/80 rounded-full p-0.5"
            @update:model-value="(val) => val && emit('update:readerMode', val)"
          >
            <ToggleGroupItem
              value="paged"
              class="rounded-full px-2 sm:px-2.5 py-1 text-xs font-medium"
            >
              <BookOpen class="size-3.5" />
              <span class="hidden sm:inline">{{ t("reader.mode_paged") }}</span>
            </ToggleGroupItem>
            <ToggleGroupItem
              value="webtoon"
              class="rounded-full px-2 sm:px-2.5 py-1 text-xs font-medium"
            >
              <ScrollText class="size-3.5" />
              <span class="hidden sm:inline">{{
                t("reader.mode_webtoon")
              }}</span>
            </ToggleGroupItem>
          </ToggleGroup>

          <Button
            variant="ghost"
            size="icon"
            class="size-8 rounded-full text-muted-foreground hover:text-foreground hover:bg-muted/70"
            :title="t('reader.close_reader')"
            :aria-label="t('reader.close_reader')"
            @click="emit('close')"
          >
            <X class="size-4" />
          </Button>
        </div>
      </header>
    </div>
  </Transition>

  <!-- Thumbnails Drawer Overlay (Floating Popover) -->
  <Transition name="reader-thumbnails-pop">
    <div
      v-if="open && thumbnailsOpen"
      class="fixed bottom-18 sm:bottom-22 inset-x-3 sm:inset-x-6 z-[1025] pointer-events-none flex justify-center select-none"
    >
      <div
        class="pointer-events-auto flex flex-col w-full max-w-4xl max-h-[52vh] sm:max-h-[60vh] rounded-3xl border border-border/80 bg-card/95 backdrop-blur-2xl shadow-2xl p-3 sm:p-4.5 ring-1 ring-black/5 dark:ring-white/10"
        @click.stop
      >
        <div
          class="flex items-center justify-between pb-2 mb-2 border-b border-border/60"
        >
          <span
            class="text-xs font-medium text-foreground flex items-center gap-1.5"
          >
            <LayoutGrid class="size-3.5 text-muted-foreground" />
            {{ t("reader.page_thumbnails") }} ({{ pageCount }})
          </span>
          <Button
            variant="ghost"
            size="icon"
            class="size-6 rounded-full text-muted-foreground hover:text-foreground hover:bg-muted/70"
            @click="thumbnailsOpen = false"
          >
            <X class="size-3.5" />
          </Button>
        </div>
        <div
          ref="thumbGridEl"
          class="grid grid-cols-2 sm:grid-cols-[repeat(auto-fill,minmax(160px,1fr))] gap-3 sm:gap-3.5 overflow-y-auto p-1.5 no-scrollbar flex-1 min-h-0"
        >
          <div
            v-for="idx in pageCount"
            :key="idx"
            :data-page-index="idx - 1"
            class="relative w-full aspect-[2/3]"
          >
            <button
              type="button"
              class="group absolute inset-0 flex h-full w-full items-center justify-center overflow-hidden rounded-xl border bg-muted/40 transition-all hover:border-primary/60 hover:shadow-md"
              :class="
                pageIndex === idx - 1
                  ? 'border-primary ring-2 ring-primary ring-offset-2 ring-offset-card shadow-md scale-[1.02]'
                  : 'border-border/60 hover:bg-muted/70'
              "
              @click="onThumbClick(idx - 1)"
            >
              <img
                v-if="thumbUrls[idx - 1]"
                :src="thumbUrls[idx - 1]"
                :alt="`${t('explore.page_no')} ${idx}`"
                class="h-full w-full object-contain select-none"
                loading="lazy"
                draggable="false"
              />
              <div
                v-else
                class="flex flex-col items-center justify-center gap-1.5 text-muted-foreground text-xs"
              >
                <Loader2 class="size-5 animate-spin opacity-40" />
                <span class="font-mono text-xs opacity-70">{{ idx }}</span>
              </div>
              <span
                class="absolute bottom-1.5 right-1.5 rounded-md bg-background/90 px-1.5 py-0.5 text-xs font-mono font-medium tabular-nums text-foreground/80 backdrop-blur-xs shadow-xs"
              >
                {{ idx }}
              </span>
            </button>
          </div>
        </div>
      </div>
    </div>
  </Transition>

  <!-- Bottom Floating Dock -->
  <Transition name="reader-dock-bottom">
    <div
      v-if="open"
      class="fixed bottom-3 sm:bottom-6 inset-x-3 sm:inset-x-6 z-[1020] pointer-events-none flex justify-center select-none"
    >
      <footer
        class="pointer-events-auto flex min-h-12 sm:min-h-13 w-full max-w-2xl items-center gap-1.5 sm:gap-2.5 rounded-2xl sm:rounded-full border border-border/70 bg-background/85 px-3 sm:px-4 py-1.5 text-foreground backdrop-blur-xl shadow-xl ring-1 ring-black/5 dark:ring-white/10"
        @click.stop
      >
        <!-- Paged Mode exclusive controls -->
        <template v-if="readerMode === 'paged'">
          <!-- 1P / 2P Switcher -->
          <Button
            variant="ghost"
            size="sm"
            class="h-7.5 px-2.5 text-xs font-medium rounded-full shrink-0"
            :class="
              pageMode === 'double'
                ? 'bg-primary text-primary-foreground font-semibold shadow-xs hover:bg-primary/90'
                : 'text-muted-foreground hover:text-foreground hover:bg-muted/70'
            "
            :title="
              pageMode === 'double'
                ? t('reader.double_page')
                : t('reader.single_page')
            "
            @click="
              emit(
                'update:pageMode',
                pageMode === 'single' ? 'double' : 'single',
              )
            "
          >
            {{ pageMode === "double" ? "2P" : "1P" }}
          </Button>

          <!-- RTL / LTR Switcher -->
          <Button
            variant="ghost"
            size="sm"
            class="h-7.5 px-2.5 text-xs font-medium rounded-full text-muted-foreground hover:text-foreground hover:bg-muted/70 shrink-0"
            :title="
              readingRTL ? t('reader.direction_rtl') : t('reader.direction_ltr')
            "
            @click="emit('update:readingRTL', !readingRTL)"
          >
            {{ readingRTL ? "RTL" : "LTR" }}
          </Button>

          <Separator
            orientation="vertical"
            class="h-4 my-auto shrink-0 opacity-60"
          />
        </template>

        <!-- Shared: Shadcn Slider & Page Jump -->
        <div class="flex flex-1 items-center gap-2 sm:gap-2.5 min-w-0 px-1">
          <button
            type="button"
            class="shrink-0 text-xs font-mono font-medium tabular-nums text-muted-foreground hover:text-foreground hover:bg-muted/60 transition-colors px-1.5 py-0.5 rounded-md cursor-pointer"
            :title="t('reader.page_thumbnails')"
            @click="thumbnailsOpen = !thumbnailsOpen"
          >
            {{ pageIndex + 1 }}
          </button>
          <Slider
            :min="1"
            :max="pageCount"
            :step="1"
            :model-value="[pageIndex + 1]"
            class="flex-1 min-w-[70px]"
            @update:model-value="onSliderValueChange"
          />
          <span
            class="shrink-0 text-xs font-mono tabular-nums text-muted-foreground/80"
          >
            {{ pageCount }}
          </span>
        </div>

        <!-- Thumbnails Drawer Button (Shared) -->
        <Button
          variant="ghost"
          size="icon"
          class="size-8 rounded-full text-muted-foreground hover:text-foreground shrink-0"
          :class="
            thumbnailsOpen
              ? 'bg-accent text-accent-foreground ring-1 ring-border'
              : 'hover:bg-muted/70'
          "
          :title="t('reader.page_thumbnails')"
          :aria-label="t('reader.page_thumbnails')"
          @click="thumbnailsOpen = !thumbnailsOpen"
        >
          <LayoutGrid class="size-4" />
        </Button>
      </footer>
    </div>
  </Transition>
</template>

<style scoped>
.reader-pill-top-enter-active,
.reader-pill-top-leave-active {
  transition:
    transform 0.25s cubic-bezier(0.16, 1, 0.3, 1),
    opacity 0.2s ease;
}
.reader-pill-top-enter-from,
.reader-pill-top-leave-to {
  transform: translateY(-16px) scale(0.98);
  opacity: 0;
}

.reader-dock-bottom-enter-active,
.reader-dock-bottom-leave-active {
  transition:
    transform 0.25s cubic-bezier(0.16, 1, 0.3, 1),
    opacity 0.2s ease;
}
.reader-dock-bottom-enter-from,
.reader-dock-bottom-leave-to {
  transform: translateY(16px) scale(0.98);
  opacity: 0;
}

.reader-thumbnails-pop-enter-active,
.reader-thumbnails-pop-leave-active {
  transition:
    transform 0.22s cubic-bezier(0.16, 1, 0.3, 1),
    opacity 0.18s ease;
}
.reader-thumbnails-pop-enter-from,
.reader-thumbnails-pop-leave-to {
  transform: translateY(14px) scale(0.97);
  opacity: 0;
}
</style>

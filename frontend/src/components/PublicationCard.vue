<script setup>
import { computed } from "vue";
import { StarIcon as Star } from "@lucide/vue";
import { Badge } from "@/components/ui/badge";
import CoverImage from "@/components/CoverImage.vue";
import { formatLanguage } from "@/gallery-metadata.js";

const props = defineProps({
  publication: { type: Object, required: true },
  badge: { type: Object, default: null },
  categoryColor: { type: String, default: "#9e9e9e" },
});
const emit = defineEmits(["select"]);

const displayLanguage = computed(() =>
  formatLanguage(props.publication?.language, props.publication),
);

const prog = computed(() => {
  const p = props.publication;
  return typeof p?.progression === "number" && Number.isFinite(p.progression)
    ? p.progression
    : null;
});

const totalPages = computed(() => {
  const p = props.publication;
  return Number(p?.pageCount || p?.page_count || 0);
});

const isCompleted = computed(() => prog.value !== null && prog.value >= 0.95);

const readingPercent = computed(() => {
  if (prog.value === null || isCompleted.value || prog.value <= 0) return 0;
  return Math.min(100, Math.max(1, Math.round(prog.value * 100)));
});
</script>

<template>
  <button
    type="button"
    class="group relative flex w-full flex-col cursor-pointer text-left transition-transform duration-200 hover:-translate-y-1 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring rounded-lg"
    @click="emit('select', publication)"
  >
    <div
      class="relative aspect-[1/1.414] w-full overflow-hidden rounded-lg border bg-muted shadow-sm transition-shadow group-hover:shadow-md"
    >
      <CoverImage
        :src="publication.coverUrl || publication.cover_url"
        :alt="publication.title"
      />
      <div
        class="absolute bottom-0 inset-x-0 h-1/2 bg-gradient-to-t from-black/80 via-black/25 to-transparent pointer-events-none"
      />
      <div
        v-if="publication.category"
        class="absolute top-1.5 left-1.5 px-1.5 py-0.5 text-[10px] font-bold rounded text-white shadow-sm"
        :style="{ backgroundColor: categoryColor }"
      >
        {{ publication.category }}
      </div>

      <!-- Badge with Smooth Transition (e.g. for fading NEW badge) -->
      <Transition
        enter-active-class="transition duration-200 ease-out"
        enter-from-class="opacity-0 scale-90"
        enter-to-class="opacity-100 scale-100"
        leave-active-class="transition duration-300 ease-in"
        leave-from-class="opacity-100 scale-100"
        leave-to-class="opacity-0 scale-90"
      >
        <div v-if="badge" class="absolute top-1.5 right-1.5 z-10">
          <Badge
            :variant="badge.variant || 'secondary'"
            class="text-[10px] px-1.5 py-0 font-medium shadow-xs"
          >
            {{ badge.label }}
          </Badge>
        </div>
      </Transition>

      <!-- Bottom Reading Progress Bar -->
      <div
        v-if="readingPercent > 0 && !isCompleted"
        class="absolute bottom-0 inset-x-0 h-1 bg-black/60 overflow-hidden z-10 pointer-events-none"
      >
        <div
          class="h-full bg-primary transition-all duration-300"
          :style="{ width: `${readingPercent}%` }"
        />
      </div>

      <div
        class="absolute bottom-1.5 left-1.5 right-1.5 flex items-center justify-between text-[11px] text-white/90 font-medium z-10"
      >
        <div class="flex items-center gap-0.5 shrink-0">
          <Star class="size-3 fill-amber-400 text-amber-400" />
          <span>{{ Number(publication.rating || 0).toFixed(1) }}</span>
        </div>
        <span
          v-if="
            displayLanguage ||
            totalPages > 0 ||
            (readingPercent > 0 && !isCompleted)
          "
          class="truncate ml-1.5 text-right drop-shadow-xs"
        >
          <template v-if="readingPercent > 0 && !isCompleted">
            <template v-if="displayLanguage">
              {{ displayLanguage }}<span class="mx-1 opacity-70">·</span>
            </template>
            <span class="font-mono">{{ readingPercent }}%</span>
            <span v-if="totalPages > 0" class="mx-1 opacity-70">·</span>
            <span v-if="totalPages > 0" class="font-mono">{{ totalPages }}P</span>
          </template>
          <template v-else>
            {{ displayLanguage
            }}<span
              v-if="displayLanguage && totalPages > 0"
              class="mx-1 opacity-70"
              >·</span
            ><span v-if="totalPages > 0" class="font-mono"
              >{{ totalPages }}P</span
            >
          </template>
        </span>
      </div>
    </div>
    <span
      class="mt-1.5 text-xs font-medium leading-snug line-clamp-2 transition-colors group-hover:text-primary"
      :title="publication.title"
    >
      {{ publication.title || publication.title_jpn || "" }}
    </span>
  </button>
</template>

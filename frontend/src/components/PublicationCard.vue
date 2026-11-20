<script setup>
import { StarIcon as Star } from "@lucide/vue";
import { Badge } from "@/components/ui/badge";
import CoverImage from "@/components/CoverImage.vue";

defineProps({
  publication: { type: Object, required: true },
  badge: { type: Object, default: null },
  categoryColor: { type: String, default: "#9e9e9e" },
});
const emit = defineEmits(["select"]);
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
      <div v-if="badge" class="absolute top-1.5 right-1.5">
        <Badge
          :variant="badge.variant || 'secondary'"
          class="text-[10px] px-1.5 py-0 font-medium"
        >
          {{ badge.label }}
        </Badge>
      </div>
      <div
        class="absolute bottom-1.5 left-1.5 right-1.5 flex items-center justify-between text-[11px] text-white/90 font-medium"
      >
        <div class="flex items-center gap-0.5 shrink-0">
          <Star class="size-3 fill-amber-400 text-amber-400" />
          <span>{{ Number(publication.rating || 0).toFixed(1) }}</span>
        </div>
        <span
          v-if="
            publication.language ||
            publication.pageCount ||
            publication.page_count
          "
          class="truncate ml-1.5 text-right drop-shadow-xs"
        >
          {{ publication.language
          }}<span
            v-if="
              publication.language &&
              (publication.pageCount || publication.page_count)
            "
            class="mx-1 opacity-70"
            >·</span
          ><span
            v-if="publication.pageCount || publication.page_count"
            class="font-mono"
            >{{ publication.pageCount || publication.page_count }}P</span
          >
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

<script setup>
import { computed } from "vue";
import { t } from "@/i18n.js";
import { EH_CATEGORIES } from "@/subscription-view.js";

const props = defineProps({
  modelValue: { type: Array, default: () => [] },
  label: { type: String, default: "" },
  description: { type: String, default: "" },
});
const emit = defineEmits(["update:modelValue"]);

const selectedIDs = computed(
  () => new Set((props.modelValue || []).map(Number).filter(Number.isInteger)),
);

function update(ids) {
  emit("update:modelValue", [...ids].sort((a, b) => a - b));
}

function toggleCategory(id) {
  const next = new Set(selectedIDs.value);
  if (next.has(id)) next.delete(id);
  else next.add(id);
  update(next);
}

function selectAll() {
  update(new Set(EH_CATEGORIES.map((category) => category.id)));
}

function clearAll() {
  update(new Set());
}
</script>

<template>
  <fieldset class="space-y-2">
    <legend v-if="label" class="sr-only">{{ label }}</legend>
    <div class="flex items-center justify-between gap-2">
      <span v-if="label" class="text-xs font-semibold">{{ label }}</span>
      <div class="flex shrink-0 items-center text-[11px]">
        <button
          type="button"
          class="cursor-pointer text-primary hover:underline"
          @click="selectedIDs.size ? clearAll() : selectAll()"
        >
          {{
            selectedIDs.size
              ? t("home_editor.facet_clear_all")
              : t("home_editor.facet_select_all")
          }}
        </button>
      </div>
    </div>
    <div class="flex flex-wrap gap-1.5">
      <button
        v-for="category in EH_CATEGORIES"
        :key="category.id"
        type="button"
        role="checkbox"
        :aria-checked="selectedIDs.has(category.id)"
        class="h-8 cursor-pointer rounded-full border px-3 text-xs transition-all focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        :class="
          selectedIDs.has(category.id)
            ? 'shadow-xs font-semibold'
            : 'border-border/80 bg-muted/30 font-medium text-muted-foreground hover:border-foreground/40 hover:text-foreground'
        "
        :style="
          selectedIDs.has(category.id)
            ? {
                backgroundColor: category.facetColor,
                borderColor: category.facetColor,
                color: category.facetTextColor,
              }
            : undefined
        "
        @click="toggleCategory(category.id)"
      >
        {{ category.label }}
      </button>
    </div>
    <p v-if="description" class="text-xs text-muted-foreground">
      {{ description }}
    </p>
  </fieldset>
</template>

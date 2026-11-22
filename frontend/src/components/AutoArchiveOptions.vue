<script setup>
import { t } from "@/i18n.js";

defineProps({
  modelValue: { type: String, default: "disabled" },
  name: { type: String, required: true },
});

const emit = defineEmits(["update:modelValue"]);
const options = [
  { value: "disabled", label: "subscription.auto_archive_disabled" },
  { value: "res", label: "archive.tier_resample" },
  { value: "org", label: "archive.tier_original" },
];
</script>

<template>
  <fieldset class="space-y-2">
    <legend class="text-xs font-semibold">
      {{ t("subscription.auto_archive") }}
    </legend>
    <div class="flex flex-col gap-1.5">
      <label
        v-for="option in options"
        :key="option.value"
        class="flex cursor-pointer items-center gap-2 rounded-lg border px-3 py-2 text-xs transition-colors"
        :class="
          modelValue === option.value
            ? 'border-primary bg-primary/5'
            : 'border-border bg-background hover:bg-muted/40'
        "
      >
        <input
          type="radio"
          :name="name"
          :value="option.value"
          :checked="modelValue === option.value"
          class="size-4 accent-primary"
          @change="emit('update:modelValue', option.value)"
        />
        <span>{{ t(option.label) }}</span>
      </label>
    </div>
    <p class="text-xs text-muted-foreground">
      {{ t("subscription.auto_archive_note") }}
    </p>
  </fieldset>
</template>

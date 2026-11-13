<script setup>
import { cn } from "cn";

const props = defineProps({
  as: { type: null, required: false, default: "a" },
  class: { type: [String, Object, Array], required: false },
});
</script>

<template>
  <component
    :is="props.as"
    data-slot="breadcrumb-link"
    :class="cn('transition-colors hover:text-foreground cursor-pointer', props.class)"
  >
    <slot />
  </component>
</template>

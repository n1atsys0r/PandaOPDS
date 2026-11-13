<script setup>
import { cn } from "cn";

const props = defineProps({
  class: { type: [String, Object, Array], required: false },
});
</script>

<template>
  <li
    data-slot="breadcrumb-item"
    :class="cn('inline-flex items-center gap-1.5', props.class)"
  >
    <slot />
  </li>
</template>

<script setup>
import { cn } from "cn";

const props = defineProps({
  class: {
    type: [Boolean, null, String, Object, Array],
    required: false,
    skipCheck: true,
  },
});
</script>

<template>
  <ul
    data-slot="sidebar-menu"
    data-sidebar="menu"
    :class="cn('gap-1 flex w-full min-w-0 flex-col', props.class)"
  >
    <slot />
  </ul>
</template>

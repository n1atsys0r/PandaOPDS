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
  <div
    data-slot="sidebar-group"
    data-sidebar="group"
    :class="cn('p-2 relative flex w-full min-w-0 flex-col', props.class)"
  >
    <slot />
  </div>
</template>

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
    data-slot="sidebar-footer"
    data-sidebar="footer"
    :class="cn('gap-2 p-2 flex flex-col', props.class)"
  >
    <slot />
  </div>
</template>

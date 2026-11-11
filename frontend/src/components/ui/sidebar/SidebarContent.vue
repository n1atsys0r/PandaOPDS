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
    data-slot="sidebar-content"
    data-sidebar="content"
    :class="
      cn(
        'no-scrollbar gap-2 flex min-h-0 flex-1 flex-col overflow-auto group-data-[collapsible=icon]:overflow-hidden',
        props.class,
      )
    "
  >
    <slot />
  </div>
</template>

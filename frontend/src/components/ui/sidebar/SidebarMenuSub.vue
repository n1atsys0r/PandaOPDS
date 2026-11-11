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
    data-slot="sidebar-menu-sub"
    data-sidebar="menu-sub"
    :class="
      cn(
        'border-sidebar-border mx-3.5 translate-x-px gap-1 border-l px-2.5 py-0.5 group-data-[collapsible=icon]:hidden flex min-w-0 flex-col',
        props.class,
      )
    "
  >
    <slot />
  </ul>
</template>

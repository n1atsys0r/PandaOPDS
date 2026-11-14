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
  <li
    data-slot="sidebar-menu-item"
    data-sidebar="menu-item"
    :class="cn('group/menu-item relative group-data-[collapsible=icon]:flex group-data-[collapsible=icon]:justify-center', props.class)"
  >
    <slot />
  </li>
</template>

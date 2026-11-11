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
    data-slot="sidebar-menu-sub-item"
    data-sidebar="menu-sub-item"
    :class="cn('group/menu-sub-item relative', props.class)"
  >
    <slot />
  </li>
</template>

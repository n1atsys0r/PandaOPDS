<script setup>
import { cn } from "cn";
import { Primitive } from "reka-ui";
import { sidebarMenuButtonVariants } from ".";

const props = defineProps({
  variant: { type: null, required: false, default: "default" },
  size: { type: null, required: false, default: "default" },
  isActive: { type: Boolean, required: false },
  class: {
    type: [Boolean, null, String, Object, Array],
    required: false,
    skipCheck: true,
  },
  asChild: { type: Boolean, required: false },
  as: { type: null, required: false, default: "button" },
});
</script>

<template>
  <Primitive
    data-slot="sidebar-menu-button"
    data-sidebar="menu-button"
    :data-size="size"
    :data-active="isActive || undefined"
    :class="cn(sidebarMenuButtonVariants({ variant, size }), props.class)"
    :as="as"
    :as-child="asChild"
    v-bind="$attrs"
  >
    <slot />
  </Primitive>
</template>

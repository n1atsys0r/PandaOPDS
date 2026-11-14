<script setup>
import { DropdownMenuTrigger, useForwardProps } from "reka-ui";

const props = defineProps({
  disabled: { type: Boolean, required: false },
  asChild: { type: Boolean, required: false },
  as: { type: null, required: false },
});
const forwarded = useForwardProps(props);
</script>

<template>
  <DropdownMenuTrigger data-slot="dropdown-menu-trigger" v-bind="forwarded">
    <slot />
  </DropdownMenuTrigger>
</template>

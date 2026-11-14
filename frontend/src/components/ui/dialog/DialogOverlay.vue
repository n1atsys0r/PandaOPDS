<script setup>
import { reactiveOmit } from "@vueuse/core";
import { cn } from "cn";
import { DialogOverlay } from "reka-ui";

const props = defineProps({
  forceMount: { type: Boolean, required: false },
  asChild: { type: Boolean, required: false },
  as: { type: null, required: false },
  class: {
    type: [Boolean, null, String, Object, Array],
    required: false,
    skipCheck: true,
  },
});

const delegatedProps = reactiveOmit(props, "class");
</script>

<template>
  <DialogOverlay
    data-slot="dialog-overlay"
    :class="
      cn(
        'bg-black/50 backdrop-blur-xs fixed inset-0 z-50',
        props.class,
      )
    "
    v-bind="delegatedProps"
  >
    <slot />
  </DialogOverlay>
</template>

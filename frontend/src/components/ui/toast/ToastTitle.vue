<script setup>
import { reactiveOmit } from "@vueuse/core";
import { cn } from "cn";
import { ToastTitle } from "reka-ui";

const props = defineProps({
  class: {
    type: [Boolean, null, String, Object, Array],
    required: false,
    skipCheck: true,
  },
  asChild: { type: Boolean, required: false },
  as: { type: null, required: false },
});

const delegatedProps = reactiveOmit(props, "class");
</script>

<template>
  <ToastTitle
    v-bind="delegatedProps"
    :class="
      cn('text-sm font-semibold tracking-tight text-foreground', props.class)
    "
  >
    <slot />
  </ToastTitle>
</template>

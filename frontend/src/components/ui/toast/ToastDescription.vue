<script setup>
import { reactiveOmit } from "@vueuse/core";
import { cn } from "cn";
import { ToastDescription } from "reka-ui";

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
  <ToastDescription
    v-bind="delegatedProps"
    :class="
      cn('text-sm text-muted-foreground leading-snug break-words', props.class)
    "
  >
    <slot />
  </ToastDescription>
</template>

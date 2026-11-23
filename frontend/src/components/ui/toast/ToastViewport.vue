<script setup>
import { reactiveOmit } from "@vueuse/core";
import { cn } from "cn";
import { ToastViewport } from "reka-ui";

const props = defineProps({
  class: {
    type: [Boolean, null, String, Object, Array],
    required: false,
    skipCheck: true,
  },
  hotkey: { type: Array, required: false },
  label: { type: [String, Function], required: false },
  asChild: { type: Boolean, required: false },
  as: { type: null, required: false },
});

const delegatedProps = reactiveOmit(props, "class");
</script>

<template>
  <ToastViewport
    v-bind="delegatedProps"
    :class="
      cn(
        'fixed bottom-4 right-4 z-[130] flex flex-col items-end gap-2 max-w-[calc(100vw-2rem)] sm:max-w-md w-full pointer-events-none outline-none',
        props.class,
      )
    "
  />
</template>

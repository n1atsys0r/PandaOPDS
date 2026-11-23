<script setup>
import { reactiveOmit } from "@vueuse/core";
import { cn } from "cn";
import { ToastRoot, useForwardPropsEmits } from "reka-ui";
import { toastVariants } from ".";

const props = defineProps({
  class: {
    type: [Boolean, null, String, Object, Array],
    required: false,
    skipCheck: true,
  },
  variant: { type: String, required: false, default: "default" },
  defaultOpen: { type: Boolean, required: false },
  open: { type: Boolean, required: false },
  duration: { type: Number, required: false },
  type: { type: String, required: false },
  asChild: { type: Boolean, required: false },
  as: { type: null, required: false },
});

const emits = defineEmits([
  "escapeKeyDown",
  "pause",
  "resume",
  "swipeStart",
  "swipeMove",
  "swipeCancel",
  "swipeEnd",
  "update:open",
]);

const delegatedProps = reactiveOmit(props, "class", "variant");
const forwarded = useForwardPropsEmits(delegatedProps, emits);
</script>

<template>
  <ToastRoot
    v-bind="forwarded"
    :class="cn(toastVariants({ variant }), props.class)"
  >
    <slot />
  </ToastRoot>
</template>

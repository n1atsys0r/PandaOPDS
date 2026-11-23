<script setup>
import { X } from "@lucide/vue";
import { reactiveOmit } from "@vueuse/core";
import { cn } from "cn";
import { ToastClose } from "reka-ui";

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
  <ToastClose
    v-bind="delegatedProps"
    :class="
      cn(
        'rounded-lg p-1 text-muted-foreground transition-colors hover:text-foreground hover:bg-muted/50 focus:outline-none focus:ring-1 group-[.destructive]:text-destructive-foreground/70 group-[.destructive]:hover:text-destructive-foreground group-[.destructive]:hover:bg-destructive-foreground/10',
        props.class,
      )
    "
  >
    <slot>
      <X class="size-4" />
    </slot>
  </ToastClose>
</template>

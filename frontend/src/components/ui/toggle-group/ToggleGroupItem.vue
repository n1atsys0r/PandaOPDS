<script setup>
import { reactiveOmit } from "@vueuse/core";
import { ToggleGroupItem, useForwardProps } from "reka-ui";
import { cn } from "@/lib/utils";

const props = defineProps({
  value: { type: String, required: true },
  disabled: { type: Boolean, required: false },
  asChild: { type: Boolean, required: false },
  as: { type: null, required: false },
  class: { type: null, required: false },
});

const delegatedProps = reactiveOmit(props, "class");
const forwarded = useForwardProps(delegatedProps);
</script>

<template>
  <ToggleGroupItem
    v-bind="forwarded"
    :class="
      cn(
        'inline-flex items-center justify-center gap-1.5 whitespace-nowrap rounded-full px-3 py-1.5 text-xs font-medium transition-all focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-50 data-[state=on]:bg-background data-[state=on]:text-foreground data-[state=on]:shadow-xs hover:text-foreground cursor-pointer select-none',
        props.class,
      )
    "
  >
    <slot />
  </ToggleGroupItem>
</template>

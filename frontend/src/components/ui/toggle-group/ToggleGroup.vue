<script setup>
import { reactiveOmit } from "@vueuse/core";
import { ToggleGroupRoot, useForwardPropsEmits } from "reka-ui";
import { cn } from "@/lib/utils";

const props = defineProps({
  type: { type: String, required: false, default: "single" },
  value: { type: [String, Array], required: false },
  defaultValue: { type: [String, Array], required: false },
  modelValue: { type: [String, Array], required: false },
  rovingFocus: { type: Boolean, required: false },
  disabled: { type: Boolean, required: false },
  orientation: { type: String, required: false },
  dir: { type: String, required: false },
  loop: { type: Boolean, required: false },
  class: { type: null, required: false },
});

const emits = defineEmits(["update:modelValue"]);
const delegatedProps = reactiveOmit(props, "class");
const forwarded = useForwardPropsEmits(delegatedProps, emits);
</script>

<template>
  <ToggleGroupRoot
    v-bind="forwarded"
    :class="
      cn(
        'inline-flex items-center justify-center rounded-full bg-muted/60 p-1 text-muted-foreground gap-1',
        props.class,
      )
    "
  >
    <slot />
  </ToggleGroupRoot>
</template>

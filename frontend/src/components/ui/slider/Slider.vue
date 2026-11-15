<script setup>
import { reactiveOmit } from "@vueuse/core";
import { SliderRange, SliderRoot, SliderThumb, SliderTrack } from "reka-ui";
import { cn } from "@/lib/utils";

const props = defineProps({
  name: { type: String, required: false },
  defaultValue: { type: Array, required: false },
  modelValue: { type: Array, required: false },
  disabled: { type: Boolean, required: false },
  orientation: { type: String, required: false },
  dir: { type: String, required: false },
  inverted: { type: Boolean, required: false },
  min: { type: Number, required: false, default: 0 },
  max: { type: Number, required: false, default: 100 },
  step: { type: Number, required: false, default: 1 },
  minStepsBetweenThumbs: { type: Number, required: false },
  asChild: { type: Boolean, required: false },
  as: { type: null, required: false },
  class: { type: null, required: false },
});

const emits = defineEmits(["update:modelValue", "valueCommit"]);
const delegatedProps = reactiveOmit(props, "class");
</script>

<template>
  <SliderRoot
    :class="
      cn(
        'relative flex w-full touch-none select-none items-center py-1.5 data-[orientation=vertical]:h-full data-[orientation=vertical]:w-auto data-[orientation=horizontal]:h-auto data-[orientation=vertical]:flex-col cursor-pointer',
        props.class,
      )
    "
    v-bind="delegatedProps"
    @update:model-value="emits('update:modelValue', $event)"
    @value-commit="emits('valueCommit', $event)"
  >
    <SliderTrack
      class="relative grow overflow-hidden rounded-full bg-muted/80 data-[orientation=horizontal]:h-2.5 data-[orientation=horizontal]:w-full data-[orientation=vertical]:h-full data-[orientation=vertical]:w-2.5"
    >
      <SliderRange
        class="absolute bg-primary rounded-full data-[orientation=horizontal]:h-full data-[orientation=vertical]:w-full"
      />
    </SliderTrack>
    <SliderThumb
      v-for="(_, key) in modelValue ?? defaultValue ?? [0]"
      :key="key"
      class="block h-4 w-6 rounded-full border border-black/10 dark:border-white/15 bg-background shadow-[0_1px_3px_rgba(0,0,0,0.18),0_1px_2px_rgba(0,0,0,0.12)] transition-transform hover:scale-105 active:scale-95 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1 focus-visible:ring-offset-background disabled:pointer-events-none disabled:opacity-50 cursor-grab active:cursor-grabbing"
    />
  </SliderRoot>
</template>

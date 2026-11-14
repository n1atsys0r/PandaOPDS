<script setup>
import { cn } from "cn";

const props = defineProps({
  class: {
    type: [Boolean, null, String, Object, Array],
    required: false,
    skipCheck: true,
  },
});
</script>

<template>
  <div
    data-slot="dialog-header"
    :class="cn('flex flex-col gap-1.5 text-center sm:text-left', props.class)"
  >
    <slot />
  </div>
</template>

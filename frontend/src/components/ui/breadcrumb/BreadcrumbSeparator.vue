<script setup>
import { ChevronRightIcon } from "@lucide/vue";
import { cn } from "cn";

const props = defineProps({
  class: { type: [String, Object, Array], required: false },
});
</script>

<template>
  <li
    data-slot="breadcrumb-separator"
    role="presentation"
    aria-hidden="true"
    :class="cn('[&>svg]:size-3.5 text-muted-foreground/60', props.class)"
  >
    <slot>
      <ChevronRightIcon class="size-3.5" />
    </slot>
  </li>
</template>

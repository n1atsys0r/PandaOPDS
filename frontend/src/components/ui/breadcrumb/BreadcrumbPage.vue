<script setup>
import { cn } from "cn";

const props = defineProps({
  class: { type: [String, Object, Array], required: false },
});
</script>

<template>
  <span
    role="link"
    aria-disabled="true"
    aria-current="page"
    data-slot="breadcrumb-page"
    :class="cn('font-normal text-foreground', props.class)"
  >
    <slot />
  </span>
</template>

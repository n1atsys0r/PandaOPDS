<script setup>
import { cn } from "cn";

const props = defineProps({
  class: { type: [String, Object, Array], required: false },
});
</script>

<template>
  <ol
    data-slot="breadcrumb-list"
    :class="
      cn(
        'flex flex-wrap items-center gap-1.5 break-words text-sm text-muted-foreground sm:gap-2',
        props.class,
      )
    "
  >
    <slot />
  </ol>
</template>

<script setup>
import { MoreHorizontalIcon } from "@lucide/vue";
import { cn } from "cn";

const props = defineProps({
  class: { type: [String, Object, Array], required: false },
});
</script>

<template>
  <span
    role="presentation"
    aria-hidden="true"
    data-slot="breadcrumb-ellipsis"
    :class="cn('flex size-9 items-center justify-center', props.class)"
  >
    <MoreHorizontalIcon class="size-4" />
    <span class="sr-only">More</span>
  </span>
</template>

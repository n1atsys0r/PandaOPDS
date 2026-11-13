<script setup>
import { cn } from "cn";

const props = defineProps({
  class: { type: [String, Object, Array], required: false },
});
</script>

<template>
  <nav aria-label="breadcrumb" data-slot="breadcrumb" :class="props.class">
    <slot />
  </nav>
</template>

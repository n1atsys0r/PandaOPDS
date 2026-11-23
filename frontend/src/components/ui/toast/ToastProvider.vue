<script setup>
import { ToastProvider } from "reka-ui";

const props = defineProps({
  duration: { type: Number, required: false, default: 4000 },
  swipeDirection: { type: String, required: false, default: "right" },
  swipeThreshold: { type: Number, required: false, default: 50 },
  label: { type: String, required: false, default: "Notification" },
});
</script>

<template>
  <ToastProvider v-bind="props">
    <slot />
  </ToastProvider>
</template>

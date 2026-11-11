<script setup>
import { PanelLeftIcon } from "@lucide/vue";
import { cn } from "cn";
import { Button } from '@/components/ui/button';
import { useSidebar } from "./utils";

const props = defineProps({
  class: {
    type: [Boolean, null, String, Object, Array],
    required: false,
    skipCheck: true,
  },
});

const { toggleSidebar } = useSidebar();
</script>

<template>
  <Button
    data-sidebar="trigger"
    data-slot="sidebar-trigger"
    variant="ghost"
    size="icon-sm"
    :class="cn('', props.class)"
    @click="toggleSidebar"
  >
    <PanelLeftIcon class="cn-rtl-flip" />
    <span class="sr-only">Toggle Sidebar</span>
  </Button>
</template>

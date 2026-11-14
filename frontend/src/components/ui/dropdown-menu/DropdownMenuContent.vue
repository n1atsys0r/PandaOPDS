<script setup>
import { reactiveOmit } from "@vueuse/core";
import { cn } from "cn";
import {
  DropdownMenuContent,
  DropdownMenuPortal,
  useForwardPropsEmits,
} from "reka-ui";

defineOptions({ inheritAttrs: false });

const props = defineProps({
  forceMount: { type: Boolean, required: false },
  loop: { type: Boolean, required: false },
  side: { type: null, required: false, default: "bottom" },
  sideOffset: { type: Number, required: false, default: 4 },
  align: { type: null, required: false, default: "end" },
  alignOffset: { type: Number, required: false },
  avoidCollisions: { type: Boolean, required: false, default: true },
  collisionBoundary: { type: null, required: false },
  collisionPadding: { type: [Number, Object], required: false, default: 8 },
  asChild: { type: Boolean, required: false },
  as: { type: null, required: false },
  class: {
    type: [Boolean, null, String, Object, Array],
    required: false,
    skipCheck: true,
  },
});
const emits = defineEmits([
  "escapeKeyDown",
  "pointerDownOutside",
  "focusOutside",
  "interactOutside",
  "entryFocus",
]);

const delegatedProps = reactiveOmit(props, "class");
const forwarded = useForwardPropsEmits(delegatedProps, emits);
</script>

<template>
  <DropdownMenuPortal>
    <DropdownMenuContent
      data-slot="dropdown-menu-content"
      v-bind="{ ...forwarded, ...$attrs }"
      :class="
        cn(
          'bg-popover text-popover-foreground data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95 data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2 z-50 min-w-36 overflow-hidden rounded-md border p-1 shadow-md',
          props.class,
        )
      "
    >
      <slot />
    </DropdownMenuContent>
  </DropdownMenuPortal>
</template>

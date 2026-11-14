<script setup>
import { XIcon } from "@lucide/vue";
import { reactiveOmit } from "@vueuse/core";
import { cn } from "cn";
import {
  DialogClose,
  DialogContent,
  DialogPortal,
  useForwardPropsEmits,
} from "reka-ui";
import { Button } from "@/components/ui/button";
import DialogOverlay from "./DialogOverlay.vue";

defineOptions({ inheritAttrs: false });

const props = defineProps({
  class: {
    type: [Boolean, null, String, Object, Array],
    required: false,
    skipCheck: true,
  },
  showCloseButton: { type: Boolean, required: false, default: true },
  forceMount: { type: Boolean, required: false },
  disableOutsidePointerEvents: { type: Boolean, required: false },
  asChild: { type: Boolean, required: false },
  as: { type: null, required: false },
});
const emits = defineEmits([
  "escapeKeyDown",
  "pointerDownOutside",
  "focusOutside",
  "interactOutside",
  "openAutoFocus",
  "closeAutoFocus",
]);

const delegatedProps = reactiveOmit(props, "class", "showCloseButton");

const forwarded = useForwardPropsEmits(delegatedProps, emits);
</script>

<template>
  <DialogPortal>
    <DialogOverlay />
    <DialogContent
      data-slot="dialog-content"
      v-bind="{ ...$attrs, ...forwarded }"
      :class="
        cn(
          'bg-popover text-popover-foreground ring-foreground/10 z-50 grid w-full gap-4 rounded-xl p-6 shadow-2xl ring-1 outline-none max-w-[calc(100%-2rem)] sm:max-w-lg',
          props.class,
          'fixed top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2',
        )
      "
    >
      <slot />

      <DialogClose v-if="showCloseButton" data-slot="dialog-close" as-child>
        <Button
          variant="ghost"
          class="absolute top-4 right-4 text-muted-foreground hover:text-foreground"
          size="icon-sm"
        >
          <XIcon class="size-4" />
          <span class="sr-only">Close</span>
        </Button>
      </DialogClose>
    </DialogContent>
  </DialogPortal>
</template>

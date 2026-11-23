<script setup>
import { CircleAlert, CircleCheck, X } from "@lucide/vue";
import { Button } from "@/components/ui/button";
import {
  Toast,
  ToastAction,
  ToastClose,
  ToastDescription,
  ToastProvider,
  ToastTitle,
  ToastViewport,
  useToast,
} from ".";

const { toasts, dismiss } = useToast();
</script>

<template>
  <ToastProvider>
    <Toast
      v-for="t in toasts"
      :key="t.id"
      :open="t.open"
      :variant="t.variant"
      class="toast-card"
      @update:open="
        (val) => {
          if (!val) dismiss(t.id);
        }
      "
    >
      <div class="flex items-start gap-3 w-full">
        <!-- Status Icon -->
        <component
          :is="
            t.icon || (t.variant === 'destructive' ? CircleAlert : CircleCheck)
          "
          class="size-5 shrink-0 mt-0.5"
          :class="
            t.variant === 'destructive' ? 'text-destructive' : 'text-primary'
          "
        />

        <!-- Text content -->
        <div class="grid gap-0.5 flex-1 min-w-0 pr-1">
          <ToastTitle v-if="t.title">
            {{ t.title }}
          </ToastTitle>
          <ToastDescription v-if="t.description">
            {{ t.description }}
          </ToastDescription>
        </div>

        <!-- Action button -->
        <ToastAction
          v-if="t.action"
          as-child
          :alt-text="t.action.altText || t.action.label || 'Action'"
        >
          <Button
            size="xs"
            variant="outline"
            class="shrink-0 h-7 text-xs font-medium rounded-lg px-2.5 shadow-2xs hover:bg-muted"
            @click="t.action.onClick?.()"
          >
            {{ t.action.label }}
          </Button>
        </ToastAction>

        <!-- Close button -->
        <ToastClose as-child>
          <Button
            variant="ghost"
            size="icon-xs"
            class="shrink-0 text-muted-foreground hover:text-foreground -mr-1 -mt-1 rounded-lg"
            @click="dismiss(t.id)"
          >
            <X class="size-3.5" />
          </Button>
        </ToastClose>
      </div>
    </Toast>
    <ToastViewport />
  </ToastProvider>
</template>

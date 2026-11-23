import { cva } from "class-variance-authority";

export { default as Toast } from "./Toast.vue";
export { default as ToastAction } from "./ToastAction.vue";
export { default as ToastClose } from "./ToastClose.vue";
export { default as ToastDescription } from "./ToastDescription.vue";
export { default as ToastProvider } from "./ToastProvider.vue";
export { default as ToastTitle } from "./ToastTitle.vue";
export { default as ToastViewport } from "./ToastViewport.vue";
export { default as Toaster } from "./Toaster.vue";
export { toast, useToast, toasts } from "./use-toast.js";

export const toastVariants = cva(
  "group pointer-events-auto relative flex w-full items-center justify-between gap-3 overflow-hidden rounded-2xl border p-4 shadow-xl transition-all duration-200 outline-none select-none data-[swipe=cancel]:translate-x-0 data-[swipe=end]:translate-x-[var(--reka-toast-swipe-end-x)] data-[swipe=move]:translate-x-[var(--reka-toast-swipe-move-x)] data-[swipe=move]:transition-none data-[state=open]:animate-in data-[state=closed]:animate-out data-[swipe=end]:animate-out data-[state=closed]:fade-out-80 data-[state=closed]:slide-out-to-right-full data-[state=open]:slide-in-from-bottom-full",
  {
    variants: {
      variant: {
        default:
          "border-border/70 bg-popover/95 text-popover-foreground ring-foreground/10 ring-1 shadow-lg dark:border-border/40 dark:bg-popover/95 backdrop-blur-md",
        destructive:
          "border-destructive/30 bg-destructive text-destructive-foreground shadow-lg dark:border-destructive/40",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  },
);

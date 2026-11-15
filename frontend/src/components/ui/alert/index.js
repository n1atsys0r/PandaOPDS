import { cva } from "class-variance-authority";

export { default as Alert } from "./Alert.vue";
export { default as AlertAction } from "./AlertAction.vue";
export { default as AlertDescription } from "./AlertDescription.vue";
export { default as AlertTitle } from "./AlertTitle.vue";

export const alertVariants = cva(
  "grid gap-0.5 rounded-2xl border px-4 py-3 text-left text-sm has-data-[slot=alert-action]:relative has-data-[slot=alert-action]:pr-18 has-[>svg]:grid-cols-[auto_1fr] has-[>svg]:gap-x-2.5 *:[svg]:row-span-2 *:[svg]:translate-y-0.5 *:[svg]:text-current *:[svg:not([class*=size-])]:size-4 group/alert relative w-full",
  {
    variants: {
      variant: {
        default: "bg-card text-card-foreground",
        destructive:
          "text-destructive bg-card *:data-[slot=alert-description]:text-destructive/90 *:[svg]:text-current",
        warning:
          "border-amber-500/30 bg-amber-500/10 text-foreground *:data-[slot=alert-description]:text-muted-foreground *:[svg]:text-amber-600 dark:*:[svg]:text-amber-400 dark:border-amber-500/20 dark:bg-amber-500/10",
      },
    },
    defaultVariants: { variant: "default" },
  },
);

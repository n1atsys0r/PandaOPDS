import { ref } from "vue";

const TOAST_LIMIT = 5;
const DEFAULT_DURATION = 4000;

export const toasts = ref([]);

let count = 0;
function genId() {
  count = (count + 1) % Number.MAX_SAFE_INTEGER;
  return `toast-${count}`;
}

export function clearToasts() {
  toasts.value = [];
}

/**
 * Creates and displays a new toast notification.
 * @param {Object} options
 * @param {string} [options.title] - Optional toast title
 * @param {string} [options.description] - Main message of the toast
 * @param {'default'|'destructive'} [options.variant='default'] - Visual variant
 * @param {number} [options.duration=4000] - Duration in ms before auto-dismiss (0 or Infinity for persistent)
 * @param {Object} [options.action] - Optional action { label, onClick, altText }
 * @param {any} [options.icon] - Optional custom Lucide icon component
 * @returns {{ id: string, dismiss: (immediate?: boolean) => void, update: (props: Object) => void }}
 */
export function toast({
  title = "",
  description = "",
  variant = "default",
  duration = DEFAULT_DURATION,
  action = null,
  icon = null,
  ...rest
} = {}) {
  const id = genId();

  const dismiss = (immediate = false) => {
    const item = toasts.value.find((t) => t.id === id);
    if (item && item.open) {
      item.open = false;
      if (immediate) {
        toasts.value = toasts.value.filter((t) => t.id !== id);
      } else {
        setTimeout(() => {
          toasts.value = toasts.value.filter((t) => t.id !== id);
        }, 350);
      }
    }
  };

  const update = (props) => {
    const item = toasts.value.find((t) => t.id === id);
    if (item) {
      Object.assign(item, props);
    }
  };

  const item = {
    id,
    title,
    description,
    variant,
    duration,
    action,
    icon,
    open: true,
    dismiss,
    ...rest,
  };

  // Add to top of stack (newest is index 0)
  toasts.value = [item, ...toasts.value].slice(0, TOAST_LIMIT);

  if (duration > 0 && duration !== Infinity) {
    setTimeout(() => {
      dismiss();
    }, duration);
  }

  return {
    id,
    dismiss,
    update,
  };
}

export function useToast() {
  return {
    toasts,
    toast,
    dismiss: (id) => {
      if (id) {
        const item = toasts.value.find((t) => t.id === id);
        if (item) {
          item.open = false;
          setTimeout(() => {
            toasts.value = toasts.value.filter((t) => t.id !== id);
          }, 350);
        }
      } else {
        toasts.value.forEach((t) => (t.open = false));
        setTimeout(() => {
          toasts.value = [];
        }, 350);
      }
    },
    clear: clearToasts,
  };
}

import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { toast, useToast, toasts } from "./components/ui/toast/use-toast.js";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

test("toast adds and manages notifications in reactive state", () => {
  const { clear } = useToast();
  clear();

  const t1 = toast({
    description: "收藏分类已更新",
  });

  assert.equal(toasts.value.length, 1);
  assert.equal(toasts.value[0].description, "收藏分类已更新");
  assert.equal(toasts.value[0].variant, "default");
  assert.equal(toasts.value[0].open, true);

  t1.dismiss(true);
  assert.equal(toasts.value.length, 0);
});

test("toast supports destructive variant, title, and action", () => {
  const { clear } = useToast();
  clear();

  let clicked = false;
  toast({
    title: "保存失败",
    description: "网络错误",
    variant: "destructive",
    action: {
      label: "重试",
      onClick: () => {
        clicked = true;
      },
    },
  });

  assert.equal(toasts.value.length, 1);
  const item = toasts.value[0];
  assert.equal(item.title, "保存失败");
  assert.equal(item.description, "网络错误");
  assert.equal(item.variant, "destructive");
  assert.equal(item.action?.label, "重试");
  item.action.onClick();
  assert.equal(clicked, true);
});

test("toast enforces max capacity queue limit", () => {
  const { clear } = useToast();
  clear();

  for (let i = 0; i < 8; i++) {
    toast({ description: `Notice ${i}` });
  }

  // Capped at 5
  assert.equal(toasts.value.length, 5);
  // Newest is at index 0
  assert.equal(toasts.value[0].description, "Notice 7");
});

test("z-index stacking invariant: Tooltip < Toast < Reader", () => {
  const readComponent = (relPath) =>
    fs.readFileSync(path.resolve(__dirname, relPath), "utf-8");

  const toastViewport = readComponent("components/ui/toast/ToastViewport.vue");
  const tooltipContent = readComponent(
    "components/ui/tooltip/TooltipContent.vue",
  );
  const archiveReader = readComponent("components/ArchiveReader.vue");

  // Tooltip must be z-[120]
  assert.match(tooltipContent, /z-\[120\]/, "TooltipContent should be z-[120]");
  // ToastViewport must be z-[130] so it layers over tooltips and dialogs
  assert.match(toastViewport, /z-\[130\]/, "ToastViewport should be z-[130]");
  // Reader must be z-[1000]
  assert.match(archiveReader, /z-\[1000\]/, "ArchiveReader should be z-[1000]");
});

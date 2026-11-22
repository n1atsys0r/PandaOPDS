<script setup>
import { onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useApp } from "@/state.js";

const props = defineProps({
  html: { type: String, default: "" },
});
const emit = defineEmits(["gallery-link"]);

const s = useApp();
const content = ref(null);
const placeholder =
  "data:image/gif;base64,R0lGODlhAQABAAD/ACwAAAAAAQABAAACADs=";
const blobUrls = new Set();
let observer = null;
let generation = 0;

function revokeBlobUrls() {
  for (const url of blobUrls) URL.revokeObjectURL(url);
  blobUrls.clear();
}

function cleanup() {
  observer?.disconnect();
  observer = null;
  revokeBlobUrls();
}

function sameOriginPath(value) {
  if (!value?.trim()) return "";
  try {
    const url = new URL(value, window.location.href);
    if (url.origin !== window.location.origin) return "";
    return `${url.pathname}${url.search}`;
  } catch {
    return "";
  }
}

function handleClick(event) {
  const anchor = event.target?.closest?.("a[href]");
  if (!anchor || !content.value?.contains(anchor)) return;
  try {
    const url = new URL(anchor.href, window.location.href);
    if (url.origin !== window.location.origin) return;
    const match = url.pathname.match(
      /(?:^|\/)opds\/v2\.0\/gallery\/([^/]+)\/([^/]+)\/?$/,
    );
    if (!match) return;
    const gid = Number(decodeURIComponent(match[1]));
    const token = decodeURIComponent(match[2]);
    if (!Number.isInteger(gid) || gid <= 0 || !token) return;
    event.preventDefault();
    emit("gallery-link", { gid, token });
  } catch {
    // Leave invalid or unrelated links to their normal browser behavior.
  }
}

async function loadImage(image, sequence) {
  const path = image.dataset.authSrc;
  if (!path || image.dataset.authLoading) return;
  image.dataset.authLoading = "true";
  const url = await s.fetchReaderBlobUrl(path);
  delete image.dataset.authLoading;
  if (sequence !== generation || !image.isConnected) {
    if (url?.startsWith("blob:")) URL.revokeObjectURL(url);
    return;
  }
  // Never assign a protected same-origin URL back to an <img> element.
  if (!url?.startsWith("blob:")) return;
  blobUrls.add(url);
  image.src = url;
  delete image.dataset.authSrc;
}

function render() {
  const sequence = ++generation;
  cleanup();
  if (!content.value) return;

  const template = document.createElement("template");
  template.innerHTML = props.html || "";
  for (const anchor of template.content.querySelectorAll("a")) {
    anchor.target = "_blank";
    anchor.rel = "noopener noreferrer";
  }

  for (const image of template.content.querySelectorAll("img")) {
    image.loading = "lazy";
    image.decoding = "async";
    image.removeAttribute("srcset");
    const path = sameOriginPath(image.getAttribute("src") || "");
    if (!path) continue;
    image.dataset.authSrc = path;
    image.src = placeholder;
  }

  // Prevent CSS background URLs from bypassing the authenticated image fetch.
  for (const element of template.content.querySelectorAll("[style]")) {
    element.style.cssText = element.style.cssText.replace(
      /url\((?:"([^"]*)"|'([^']*)'|([^)]*))\)/gi,
      (match, doubleQuoted, singleQuoted, bare) =>
        sameOriginPath(doubleQuoted || singleQuoted || bare || "")
          ? "none"
          : match,
    );
  }

  content.value.replaceChildren(template.content.cloneNode(true));
  const images = [...content.value.querySelectorAll("img[data-auth-src]")];
  if (typeof IntersectionObserver === "undefined") {
    for (const image of images) loadImage(image, sequence);
    return;
  }
  observer = new IntersectionObserver((entries) => {
    for (const entry of entries) {
      if (!entry.isIntersecting) continue;
      observer?.unobserve(entry.target);
      loadImage(entry.target, sequence);
    }
  });
  for (const image of images) observer.observe(image);
}

watch([() => props.html, () => s.authToken.value], render);
onMounted(render);
onBeforeUnmount(() => {
  generation++;
  cleanup();
});
</script>

<template>
  <div ref="content" @click="handleClick" />
</template>

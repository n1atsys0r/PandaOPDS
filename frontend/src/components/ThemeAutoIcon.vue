<script setup>
import { useId } from "vue";

const clipId = useId();
</script>

<template>
  <svg
    xmlns="http://www.w3.org/2000/svg"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="2"
    stroke-linecap="round"
    stroke-linejoin="round"
    class="size-4 shrink-0"
    aria-hidden="true"
  >
    <defs>
      <clipPath :id="clipId">
        <path d="M12 3a9 9 0 0 1 0 18Z" />
      </clipPath>
    </defs>
    <!-- Hatching stripes on the right half -->
    <g :clip-path="`url(#${clipId})`">
      <path d="M8 11l14-14" />
      <path d="M8 15l18-18" />
      <path d="M8 19l22-22" />
      <path d="M8 23l26-26" />
      <path d="M8 27l26-26" />
    </g>
    <!-- Outer boundary & vertical divider -->
    <circle cx="12" cy="12" r="9" />
    <path d="M12 3v18" />
  </svg>
</template>

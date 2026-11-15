import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
import tailwindcss from "@tailwindcss/vite";
import { fileURLToPath } from "node:url";

export default defineConfig({
  plugins: [vue(), tailwindcss()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  server: {
    proxy: {
      "/api": "http://127.0.0.1:8080",
      "/opds": "http://127.0.0.1:8080",
      "/image": "http://127.0.0.1:8080",
      "/stream": "http://127.0.0.1:8080",
    },
  },
  base: "/webui-assets/",
  build: { outDir: "../app/webui/dist", emptyOutDir: true },
});

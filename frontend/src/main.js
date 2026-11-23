import { createApp } from "vue";
import App from "./App.vue";
import "./style.css";
import { toast } from "@/components/ui/toast";

if (typeof window !== "undefined") {
  window.toast = toast;
}

createApp(App).mount("#app");

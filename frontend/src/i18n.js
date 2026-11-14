import { ref } from "vue";
import zh from "./locales/zh.js";
import en from "./locales/en.js";

const messages = { zh, en };
const STORAGE_KEY = "pandaopds_lang";

function getInitialLocale() {
  if (typeof window !== "undefined" && window.localStorage) {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (saved && messages[saved]) return saved;
    const nav = window.navigator;
    const lang = (nav?.language || nav?.userLanguage || "").toLowerCase();
    if (lang.startsWith("en")) return "en";
  }
  return "zh";
}

export const currentLocale = ref(getInitialLocale());

export function setLocale(lang) {
  if (!messages[lang]) return;
  currentLocale.value = lang;
  if (typeof window !== "undefined") {
    try {
      localStorage.setItem(STORAGE_KEY, lang);
    } catch {}
    if (document.documentElement) {
      document.documentElement.lang = lang;
    }
  }
}

export function toggleLocale() {
  setLocale(currentLocale.value === "zh" ? "en" : "zh");
}

function resolvePath(obj, path) {
  if (!obj || !path) return undefined;
  const parts = path.split(".");
  let cur = obj;
  for (const part of parts) {
    if (cur == null || typeof cur !== "object") return undefined;
    cur = cur[part];
  }
  return cur;
}

export function t(key, params = {}) {
  const lang = currentLocale.value || "zh";
  let val = resolvePath(messages[lang], key);
  if (val === undefined && lang !== "zh") {
    val = resolvePath(messages.zh, key);
  }
  if (val === undefined) {
    return key;
  }
  if (typeof val !== "string") {
    return val;
  }
  if (params && Object.keys(params).length > 0) {
    return val.replace(/\{(\w+)\}/g, (match, k) => {
      return params[k] !== undefined && params[k] !== null ? String(params[k]) : match;
    });
  }
  return val;
}

export function useI18n() {
  return {
    locale: currentLocale,
    setLocale,
    toggleLocale,
    t,
  };
}

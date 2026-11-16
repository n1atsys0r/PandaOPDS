import { reactive, toRefs } from "vue";
import {
  canUseFacets,
  chooseInitialHomeMode,
  createDefaultGlobalNavigation,
  createDefaultHomePages,
  createDefaultSearchFacets,
  normalizeMode,
  replacePageSlugReferences,
  serializeHomeLayout,
  splitHomeSections,
} from "./home-layout.js";
import { t } from "./i18n.js";
import {
  READER_PAGE_KEY_PREFIX,
  parseSavedProgress,
  buildProgressionPayload,
  resolveLatestProgress,
} from "./progression-helper.js";
const state = reactive({
  page: "dashboard",
  exploreSubRoute: null,
  mobile: false,
  theme: localStorage.theme || "system",
  busy: false,
  authChecked: false,
  authRequired: false,
  loginError: "",
  authToken: localStorage.getItem("pandaopds_auth") || "",
  notice: "",
  error: "",
  status: null,
  config: null,
  home: null,
  navigation: [],
  searchFacets: createDefaultSearchFacets(),
  facetSets: [],
  archives: [],
  archiveStats: {},
  archiveScan: { status: "idle", imported: 0, skipped: 0, conflicts: 0 },
  archiveScanDismissedKey:
    localStorage.getItem("pandaopds_dismissed_archive_scan") || "",
  favcats: [],
  favoriteOverrides: {},
  archiveInput: "",
  quote: null,
  quoteFor: "",
  selectedQuality: "",
  quoteAction: "start",
  quoteTarget: null,
  confirmArchive: false,
  confirmCache: false,
  confirmDelete: false,
  deleteTarget: null,
  confirmFavoriteRemove: false,
  favoriteRemoveTarget: null,
  deleteAfterFavoriteRemove: false,
  etag: "",
  homeTag: "",
  confDirty: {},
  homeDirty: false,
  homeMode: "publications",
  homeModeRequired: false,
  form: {},
  layout: { navigation: [], publication: null, facets: [], groups: [] },
  pages: [],
  activePageId: "home",
});
const MAX_COVER_CACHE = 300;
const coverCache = new Map();
const coverInflight = new Map();

function setCoverCache(cacheKey, url) {
  if (coverCache.has(cacheKey)) {
    coverCache.delete(cacheKey);
  } else if (coverCache.size >= MAX_COVER_CACHE) {
    const oldestKey = coverCache.keys().next().value;
    if (oldestKey) {
      const oldUrl = coverCache.get(oldestKey);
      if (oldUrl && oldUrl.startsWith("blob:")) {
        try {
          URL.revokeObjectURL(oldUrl);
        } catch {}
      }
      coverCache.delete(oldestKey);
    }
  }
  coverCache.set(cacheKey, url);
}

function clearCoverCache() {
  for (const url of coverCache.values()) {
    if (url.startsWith("blob:")) URL.revokeObjectURL(url);
  }
  coverCache.clear();
  coverInflight.clear();
}

function clearAuthOn401() {
  state.authRequired = true;
  state.authToken = "";
  localStorage.removeItem("pandaopds_auth");
  clearCoverCache();
}

function peekCoverUrl(path) {
  if (!path) return "";
  const token = state.authToken || localStorage.getItem("pandaopds_auth");
  if (!token) return path;
  const cacheKey = `${token}:${path}`;
  const url = coverCache.get(cacheKey);
  if (url) {
    coverCache.delete(cacheKey);
    coverCache.set(cacheKey, url);
    return url;
  }
  return "";
}

async function fetchMediaBlobUrl(path, signal, directWhenUnauthed) {
  if (!path) return "";
  const token = state.authToken || localStorage.getItem("pandaopds_auth") || "";
  if (!token && directWhenUnauthed) return path;
  try {
    const headers = { "X-Requested-With": "XMLHttpRequest" };
    if (token) headers.Authorization = `Basic ${token}`;
    const r = await fetch(path, { signal, headers });
    if (r.status === 401) {
      clearAuthOn401();
      return "";
    }
    if (!r.ok) return "";
    const blob = await r.blob();
    if (!blob || blob.size === 0) return "";
    const objectUrl = URL.createObjectURL(blob);
    const activeToken =
      state.authToken || localStorage.getItem("pandaopds_auth") || "";
    if (activeToken !== token) {
      URL.revokeObjectURL(objectUrl);
      return "";
    }
    return objectUrl;
  } catch {
    return "";
  }
}

function fetchAuthedBlobUrl(path, signal) {
  return fetchMediaBlobUrl(path, signal, true);
}

function fetchReaderBlobUrl(path, signal) {
  return fetchMediaBlobUrl(path, signal, false);
}

async function resolveCoverUrl(path) {
  if (!path) return "";
  const token = state.authToken || localStorage.getItem("pandaopds_auth");
  if (!token) return path;
  const cacheKey = `${token}:${path}`;
  if (coverCache.has(cacheKey)) return coverCache.get(cacheKey);
  if (coverInflight.has(cacheKey)) return coverInflight.get(cacheKey);
  const task = (async () => {
    try {
      const objectUrl = await fetchAuthedBlobUrl(path);
      if (!objectUrl) return "";
      setCoverCache(cacheKey, objectUrl);
      return objectUrl;
    } finally {
      coverInflight.delete(cacheKey);
    }
  })();
  coverInflight.set(cacheKey, task);
  return task;
}

async function api(path, opts = {}) {
  const token = state.authToken || localStorage.getItem("pandaopds_auth");
  const headers = {
    Accept: "application/json",
    "X-Requested-With": "XMLHttpRequest",
    ...(token ? { Authorization: `Basic ${token}` } : {}),
    ...(opts.body ? { "Content-Type": "application/json" } : {}),
    ...(opts.headers || {}),
  };
  const r = await fetch(path, { ...opts, headers });
  const raw = await r.text();
  let data = {};
  try {
    data = raw ? JSON.parse(raw) : {};
  } catch {
    data = { detail: raw };
  }
  if (r.status === 401) {
    clearAuthOn401();
  }
  if (!r.ok)
    throw Object.assign(
      new Error(
        data.detail || data.error || data.message || "HTTP " + r.status,
      ),
      { status: r.status, data },
    );
  return { data, headers: r.headers };
}

const progressionCache = reactive(new Map());

async function getProgression(gid, token) {
  if (!gid || !token) return null;
  const key = `${gid}:${token}`;
  try {
    const res = await api(`/opds/progression/${gid}/${token}`);
    if (res && res.data && typeof res.data.progression === "number") {
      progressionCache.set(key, res.data);
      return res.data;
    }
  } catch {
    // 404 / 401 / network errors: return null silently
  }
  return null;
}

async function putProgression(gid, token, { pageIndex, pageCount }) {
  if (!gid || !token || !Number.isFinite(pageCount) || pageCount <= 0)
    return null;
  const payload = buildProgressionPayload(pageIndex, pageCount);
  const key = `${gid}:${token}`;
  try {
    const res = await api(`/opds/progression/${gid}/${token}`, {
      method: "PUT",
      body: JSON.stringify(payload),
    });
    if (res && res.data && typeof res.data.progression === "number") {
      progressionCache.set(key, res.data);
      return res.data;
    }
  } catch (err) {
    if (err?.status !== 409) {
      // ignore conflict or network issues
    }
  }
  return null;
}

function updateLocalProgress(
  gid,
  token,
  pageIndex,
  pageCount,
  modified = new Date().toISOString(),
) {
  if (!gid || !token || !Number.isFinite(pageCount) || pageCount <= 0) return;
  const key = `${gid}:${token}`;
  const clamped = Math.min(Math.max(0, pageIndex), Math.max(0, pageCount - 1));
  const payload = buildProgressionPayload(clamped, pageCount);
  payload.modified = modified;
  progressionCache.set(key, payload);
}

function getGalleryProgress(gid, token, pageCount) {
  if (!gid || !token || !pageCount || pageCount <= 0) return null;
  const key = `${gid}:${token}`;
  let localRecord = null;
  if (typeof window !== "undefined" && window.localStorage) {
    try {
      localRecord = parseSavedProgress(
        localStorage.getItem(`${READER_PAGE_KEY_PREFIX}${key}`),
      );
    } catch {}
  }
  const remoteDoc = progressionCache.get(key);
  return resolveLatestProgress(localRecord, remoteDoc, pageCount);
}

async function run(fn) {
  state.busy = true;
  state.error = "";
  try {
    await fn();
  } catch (e) {
    if (e.status !== 401) state.error = e.message || String(e);
  } finally {
    state.busy = false;
  }
}
function setTheme(v) {
  state.theme = v;
  localStorage.theme = v;
  applyTheme();
}
function applyTheme() {
  const resolved =
    state.theme === "system"
      ? matchMedia("(prefers-color-scheme: dark)").matches
        ? "dark"
        : "light"
      : state.theme;
  document.documentElement.dataset.theme = resolved;
  document.documentElement.classList.toggle("dark", resolved === "dark");
}
async function loadStatus() {
  try {
    state.status = (await api("/api/status")).data;
  } catch (e) {
    if (e.status !== 401) state.error = e.message;
  } finally {
    state.authChecked = true;
  }
}
async function loadConfig() {
  try {
    const r = await api("/api/config/edit");
    state.config = r.data;
    state.etag = (r.headers.get("ETag") || "").replace(/^W\//i, "");
    state.confDirty = {};
    state.form = {};
    for (const [k, v] of Object.entries(r.data.fields || {}))
      state.form[k] = v.saved ?? v.active ?? "";
  } catch (e) {
    if (e.status === 401) return;
    try {
      state.config = { legacy: (await api("/api/config")).data };
      state.error = t("notice.config_readonly_error");
    } catch (fallbackErr) {
      if (fallbackErr.status !== 401) state.error = e.message;
    }
  }
}
function setField(k, v) {
  const sample =
    state.config?.fields?.[k]?.saved ?? state.config?.fields?.[k]?.active;
  if (typeof sample === "number" && v !== "" && Number.isFinite(Number(v)))
    v = Number(v);
  state.form[k] = v;
  state.confDirty = { ...state.confDirty, [k]: true };
}
async function saveConfig() {
  await run(async () => {
    const patch = {};
    for (const [k, changed] of Object.entries(state.confDirty)) {
      if (!changed) continue;
      const old =
        state.config.fields[k].saved ?? state.config.fields[k].active ?? "";
      const value = state.form[k];
      if (value !== old) patch[k] = value === "" ? null : value;
    }
    const secrets = ["ipb_member_id", "ipb_pass_hash"];
    if (secrets.some((k) => state.confDirty[k])) {
      const a = state.form.ipb_member_id || "",
        b = state.form.ipb_pass_hash || "";
      if (Boolean(a) !== Boolean(b)) throw Error(t("notice.missing_secrets"));
      patch.ipb_member_id = a || null;
      patch.ipb_pass_hash = b || null;
    }
    await api("/api/config/edit", {
      method: "PATCH",
      headers: { "If-Match": state.etag },
      body: JSON.stringify(patch),
    });
    state.notice = t("notice.settings_saved");
    await loadConfig();
  });
}
async function loadHome(options = {}) {
  try {
    const r = await api("/api/home/edit");
    state.home = r.data;
    state.homeTag = (r.headers.get("ETag") || "").replace(/^W\//i, "");
    if (!options.preserveDrafts) {
      state.navigation = (
        Array.isArray(r.data.navigation) ? r.data.navigation : []
      ).map((x) => {
        if (
          x.type === "preset" &&
          (x.query === "toplists" || x.query === "home")
        ) {
          return { ...x, type: "page", query: x.query };
        }
        return x;
      });
      if (
        Array.isArray(r.data.search_facets) &&
        r.data.search_facets.length > 0
      ) {
        state.searchFacets = structuredClone(r.data.search_facets);
      } else if (
        Array.isArray(r.data.facet_sets) &&
        r.data.facet_sets.length > 0 &&
        Array.isArray(r.data.facet_sets[0].items)
      ) {
        state.searchFacets = structuredClone(r.data.facet_sets[0].items);
      } else {
        state.searchFacets = createDefaultSearchFacets();
      }
      state.facetSets = Array.isArray(r.data.facet_sets)
        ? structuredClone(r.data.facet_sets)
        : [];
      const rawPages =
        Array.isArray(r.data.pages) && r.data.pages.length > 0
          ? r.data.pages.filter((p) => !BUILTIN_PAGE_IDS.has(p.id))
          : [];
      if (rawPages.length > 0) {
        state.pages = rawPages.map((p) => {
          const secs = p.sections || [];
          const initialMode = p.mode
            ? normalizeMode(p.mode)
            : chooseInitialHomeMode(secs) || "publications";
          return {
            id: p.id,
            title:
              p.title ||
              (p.id === "home" ? t("home_editor.home_page_title") : p.id),
            mode: initialMode,
            layout: splitHomeSections(secs),
          };
        });
      } else {
        const sections = r.data.sections || [];
        state.pages = [
          {
            id: "home",
            title: t("home_editor.home_page_title"),
            mode: chooseInitialHomeMode(sections) || "publications",
            layout: splitHomeSections(sections),
          },
        ];
      }
      if (!state.pages.some((p) => p.id === state.activePageId)) {
        state.activePageId = "home";
      }
      const active =
        state.pages.find((p) => p.id === state.activePageId) || state.pages[0];
      state.layout = active.layout;
      state.homeMode = normalizeMode(active.mode);
      state.homeModeRequired = !active.mode;
    }
    state.homeDirty = false;
  } catch (e) {
    if (e.status === 401) return;
    try {
      state.home = { legacy: (await api("/api/home")).data };
      state.error = t("notice.home_readonly_error");
    } catch (fallbackErr) {
      if (fallbackErr.status !== 401) state.error = e.message;
    }
  }
}
async function saveHome() {
  const payloadPages = state.pages.map((p) => ({
    id: p.id,
    title:
      p.title || (p.id === "home" ? t("home_editor.home_page_title") : p.id),
    mode: normalizeMode(p.mode),
    sections: serializeHomeLayout(p.layout, p.mode),
  }));
  const payload = {
    navigation: state.navigation || [],
    search_facets: state.searchFacets || [],
    facet_sets: state.facetSets || [],
    pages: payloadPages,
  };
  await run(async () => {
    let tag = state.homeTag;
    if (!tag) {
      try {
        const probe = await api("/api/home/edit");
        tag = (probe.headers.get("ETag") || "").replace(/^W\//i, "");
        state.homeTag = tag;
      } catch {}
    }
    const saved = await api("/api/home/edit", {
      method: "PUT",
      headers: { "If-Match": tag },
      body: JSON.stringify(payload),
    });
    state.home = saved.data;
    if (state.home) {
      delete state.home.legacy;
      delete state.home.error;
      delete state.home.validation_error;
    }
    state.error = "";
    state.homeTag = (saved.headers.get("ETag") || "").replace(/^W\//i, "");
    if (Array.isArray(saved.data.navigation)) {
      state.navigation = saved.data.navigation;
    }
    if (Array.isArray(saved.data.search_facets)) {
      state.searchFacets = saved.data.search_facets;
    }
    if (Array.isArray(saved.data.facet_sets)) {
      state.facetSets = saved.data.facet_sets;
    }
    state.notice = t("notice.home_saved");
    state.homeDirty = false;
  });
}
function selectActivePage(pageId) {
  state.activePageId = pageId;
  if (
    pageId === "__global_nav__" ||
    pageId === "__search_facets__" ||
    pageId === "__facet_sets__"
  ) {
    return;
  }
  const p = state.pages.find((x) => x.id === pageId);
  if (!p) return;
  state.layout = p.layout;
  state.homeMode = normalizeMode(p.mode);
  state.homeModeRequired = !p.mode;
}
const BUILTIN_PAGE_IDS = new Set([
  "watched",
  "favorites",
  "archives",
  "history",
]);

function addPage({ id, title, mode = "publications" }) {
  if (!id || !/^[a-zA-Z0-9_-]+$/.test(id)) return false;
  if (BUILTIN_PAGE_IDS.has(id)) return false;
  if (state.pages.some((p) => p.id === id)) return false;
  const normalized = normalizeMode(mode);
  const newPage = {
    id,
    title: title || id,
    mode: normalized,
    layout: {
      navigation: [],
      publication:
        mode === "publications"
          ? {
              kind: "publication",
              group: false,
              title: t("home_editor.latest"),
              type: "preset",
              query: "latest",
              count: 10,
            }
          : null,
      facets: [],
      groups: [],
    },
  };
  state.pages.push(newPage);
  selectActivePage(id);
  state.homeDirty = true;
  return true;
}
function removePage(pageId) {
  if (pageId === "home") return false;
  const idx = state.pages.findIndex((p) => p.id === pageId);
  if (idx < 0) return false;
  state.pages.splice(idx, 1);
  if (state.activePageId === pageId) {
    selectActivePage("home");
  }
  state.homeDirty = true;
  return true;
}
function updatePageMeta(pageId, { title, id: newId }) {
  const p = state.pages.find((x) => x.id === pageId);
  if (!p) return false;
  if (title !== undefined) p.title = title;
  if (newId && newId !== pageId && pageId !== "home") {
    if (!/^[a-zA-Z0-9_-]+$/.test(newId)) return false;
    if (BUILTIN_PAGE_IDS.has(newId)) return false;
    if (state.pages.some((x) => x.id === newId)) return false;
    p.id = newId;
    replacePageSlugReferences(state.navigation, state.pages, pageId, newId);
    if (state.activePageId === pageId) {
      state.activePageId = newId;
    }
  }
  state.homeDirty = true;
  return true;
}
function reorderPages(fromIndex, toIndex) {
  if (fromIndex <= 0 || toIndex <= 0) return;
  if (fromIndex >= state.pages.length || toIndex >= state.pages.length) return;
  const item = state.pages.splice(fromIndex, 1)[0];
  state.pages.splice(toIndex, 0, item);
  state.homeDirty = true;
}
function resetHomeToDefault() {
  if (state.home) {
    delete state.home.legacy;
    delete state.home.error;
    delete state.home.validation_error;
  } else {
    state.home = {};
  }
  state.error = "";
  state.navigation = createDefaultGlobalNavigation();
  state.searchFacets = createDefaultSearchFacets();
  state.facetSets = [];
  state.pages = createDefaultHomePages();
  selectActivePage("home");
  state.homeDirty = true;
}

function addGlobalNavigation(presetOrQuery = "watched") {
  if (!Array.isArray(state.navigation)) state.navigation = [];
  state.navigation.push({
    kind: "navigation",
    group: false,
    title: "",
    type: "preset",
    query: presetOrQuery,
  });
  state.homeDirty = true;
}

function removeGlobalNavigation(index) {
  if (!Array.isArray(state.navigation)) return;
  state.navigation.splice(index, 1);
  state.homeDirty = true;
}

function moveGlobalNavigation(index, delta) {
  if (!Array.isArray(state.navigation)) return;
  const targetIndex = index + delta;
  if (targetIndex < 0 || targetIndex >= state.navigation.length) return;
  const item = state.navigation.splice(index, 1)[0];
  state.navigation.splice(targetIndex, 0, item);
  state.homeDirty = true;
}
function selectHomeMode(mode) {
  const normalized = normalizeMode(mode);
  const active = state.pages.find((p) => p.id === state.activePageId);
  if (
    state.homeMode === normalized &&
    (!active || active.mode === normalized) &&
    !state.homeModeRequired
  ) {
    return;
  }
  state.homeMode = normalized;
  state.homeModeRequired = false;
  if (active) {
    active.mode = normalized;
    if (normalized === "publications" && !active.layout.publication) {
      active.layout.publication = {
        kind: "publication",
        group: false,
        title: "Latest",
        type: "preset",
        query: "latest",
        count: 10,
      };
    }
  }
  state.homeDirty = true;
}
function canAddFacet() {
  return canUseFacets(state.layout.publication);
}
function addNavigation(presetOrQuery = "watched") {
  state.layout.navigation.push({
    kind: "navigation",
    group: false,
    title: "",
    type: "preset",
    query: presetOrQuery,
  });
  state.homeDirty = true;
}
function removeItem(collection, i) {
  state.layout[collection].splice(i, 1);
  state.homeDirty = true;
}
function moveItem(collection, i, delta) {
  const j = i + delta;
  if (j < 0 || j >= state.layout[collection].length) return;
  const a = state.layout[collection].splice(i, 1)[0];
  state.layout[collection].splice(j, 0, a);
  state.homeDirty = true;
}
function addPublication() {
  if (state.layout.publication) return;
  state.layout.publication = {
    kind: "publication",
    group: false,
    title: t("home_editor.latest"),
    type: "preset",
    query: "latest",
    count: 10,
  };
  state.homeDirty = true;
}
function removePublication() {
  state.layout.publication = null;
  state.homeDirty = true;
}
function addSearchFacet(item = {}) {
  if (!Array.isArray(state.searchFacets)) state.searchFacets = [];
  state.searchFacets.push({
    title: item.title || "",
    categories: Array.isArray(item.categories) ? item.categories : [],
    rating: Number(item.rating) || 0,
    expunged: Boolean(item.expunged),
    disable_filters: Array.isArray(item.disable_filters)
      ? item.disable_filters
      : [],
    query: item.query || "",
  });
  state.homeDirty = true;
}

function removeSearchFacet(index) {
  if (!Array.isArray(state.searchFacets)) return;
  state.searchFacets.splice(index, 1);
  state.homeDirty = true;
}

function moveSearchFacet(index, delta) {
  if (!Array.isArray(state.searchFacets)) return;
  const target = index + delta;
  if (target < 0 || target >= state.searchFacets.length) return;
  const item = state.searchFacets.splice(index, 1)[0];
  state.searchFacets.splice(target, 0, item);
  state.homeDirty = true;
}

function toggleSearchFacetCategory(target, catName) {
  const it = typeof target === "number" ? state.searchFacets[target] : target;
  if (!it) return;
  if (!Array.isArray(it.categories)) it.categories = [];
  const idx = it.categories.indexOf(catName);
  if (idx >= 0) {
    it.categories.splice(idx, 1);
  } else {
    it.categories.push(catName);
  }
  state.homeDirty = true;
}

function toggleSearchFacetFilter(target, filterName) {
  const it = typeof target === "number" ? state.searchFacets[target] : target;
  if (!it) return;
  if (!Array.isArray(it.disable_filters)) it.disable_filters = [];
  const idx = it.disable_filters.indexOf(filterName);
  if (idx >= 0) {
    it.disable_filters.splice(idx, 1);
  } else {
    it.disable_filters.push(filterName);
  }
  state.homeDirty = true;
}

function addFacetSet(title = "", id = "") {
  if (!Array.isArray(state.facetSets)) state.facetSets = [];
  const generatedId = (
    id ||
    `set_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 6)}`
  )
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9_-]/g, "-");
  state.facetSets.push({
    id: generatedId,
    title: title || t("home_editor.facet_set_default_title"),
    items: [
      {
        title: t("home_editor.facet_all"),
        categories: [],
        rating: 0,
        expunged: false,
        disable_filters: [],
        query: "",
      },
    ],
  });
  state.homeDirty = true;
  return generatedId;
}

function removeFacetSet(setIndex) {
  if (!Array.isArray(state.facetSets)) return;
  const removed = state.facetSets.splice(setIndex, 1)[0];
  if (removed && removed.id) {
    for (const p of state.pages) {
      if (p.layout && Array.isArray(p.layout.facets)) {
        p.layout.facets = p.layout.facets.filter(
          (f) => f.facet_set !== removed.id,
        );
      }
    }
  }
  state.homeDirty = true;
}

function moveFacetSet(setIndex, delta) {
  if (!Array.isArray(state.facetSets)) return;
  const target = setIndex + delta;
  if (target < 0 || target >= state.facetSets.length) return;
  const item = state.facetSets.splice(setIndex, 1)[0];
  state.facetSets.splice(target, 0, item);
  state.homeDirty = true;
}

function addFacetItem(setIndex) {
  const set = state.facetSets[setIndex];
  if (!set) return;
  if (!Array.isArray(set.items)) set.items = [];
  set.items.push({
    title: "",
    categories: [],
    rating: 0,
    expunged: false,
    disable_filters: [],
    query: "",
  });
  state.homeDirty = true;
}

function removeFacetItem(setIndex, itemIndex) {
  const set = state.facetSets[setIndex];
  if (!set || !Array.isArray(set.items)) return;
  set.items.splice(itemIndex, 1);
  state.homeDirty = true;
}

function moveFacetItem(setIndex, itemIndex, delta) {
  const set = state.facetSets[setIndex];
  if (!set || !Array.isArray(set.items)) return;
  const target = itemIndex + delta;
  if (target < 0 || target >= set.items.length) return;
  const item = set.items.splice(itemIndex, 1)[0];
  set.items.splice(target, 0, item);
  state.homeDirty = true;
}

function addPageFacetReference(facetSetId) {
  if (!state.layout) return;
  if (!Array.isArray(state.layout.facets)) state.layout.facets = [];
  if (state.layout.facets.some((f) => f.facet_set === facetSetId)) return;
  state.layout.facets.push({
    facet_set: facetSetId,
    title: "",
    items: [],
  });
  state.homeDirty = true;
}

function removePageFacetReference(idx) {
  if (!state.layout || !Array.isArray(state.layout.facets)) return;
  state.layout.facets.splice(idx, 1);
  state.homeDirty = true;
}

function movePageFacetReference(idx, delta) {
  if (!state.layout || !Array.isArray(state.layout.facets)) return;
  const target = idx + delta;
  if (target < 0 || target >= state.layout.facets.length) return;
  const item = state.layout.facets.splice(idx, 1)[0];
  state.layout.facets.splice(target, 0, item);
  state.homeDirty = true;
}

function toggleFacetCategory(item, catName) {
  if (!Array.isArray(item.categories)) item.categories = [];
  const idx = item.categories.findIndex(
    (c) => c.toLowerCase() === catName.toLowerCase(),
  );
  if (idx >= 0) {
    item.categories.splice(idx, 1);
  } else {
    item.categories.push(catName);
  }
  state.homeDirty = true;
}

function toggleFacetFilter(item, filterKey) {
  if (!Array.isArray(item.disable_filters)) item.disable_filters = [];
  const idx = item.disable_filters.indexOf(filterKey);
  if (idx >= 0) {
    item.disable_filters.splice(idx, 1);
  } else {
    item.disable_filters.push(filterKey);
  }
  state.homeDirty = true;
}

function addFacet() {
  if (!canAddFacet()) return;
  addFacetSet();
}
function addGroupSection(kind) {
  state.layout.groups.push(
    kind === "navigation"
      ? {
          kind: "navigation",
          group: true,
          title: t("home_editor.nav_group_default_title"),
          items: [{ title: "", type: "preset", query: "latest" }],
        }
      : {
          kind: "publication",
          group: true,
          title: t("home_editor.popular"),
          type: "preset",
          query: "popular",
          count: 10,
        },
  );
  state.homeDirty = true;
}
function addGroupNavigationItem(groupIndex, presetOrQuery = "latest") {
  const grp = state.layout.groups[groupIndex];
  if (!grp) return;
  if (!Array.isArray(grp.items)) grp.items = [];
  grp.items.push({ title: "", type: "preset", query: presetOrQuery });
  state.homeDirty = true;
}
function removeGroupNavigationItem(groupIndex, itemIndex) {
  const grp = state.layout.groups[groupIndex];
  if (!grp || !Array.isArray(grp.items)) return;
  grp.items.splice(itemIndex, 1);
  state.homeDirty = true;
}
function moveGroupNavigationItem(groupIndex, itemIndex, delta) {
  const grp = state.layout.groups[groupIndex];
  if (!grp || !Array.isArray(grp.items)) return;
  const targetIndex = itemIndex + delta;
  if (targetIndex < 0 || targetIndex >= grp.items.length) return;
  const [removed] = grp.items.splice(itemIndex, 1);
  grp.items.splice(targetIndex, 0, removed);
  state.homeDirty = true;
}
async function loadArchives() {
  try {
    const d = (await api("/api/archive")).data;
    state.archives = d.entries || d.items || (Array.isArray(d) ? d : []);
    state.archiveStats = d.stats || {};
  } catch (e) {
    if (e.status !== 401) state.error = e.message;
  }
}
const DISMISSED_SCAN_KEY = "pandaopds_dismissed_archive_scan";
let archiveScanDismissTimer = null;

function archiveScanKey(scan) {
  if (!scan || scan.status === "idle" || scan.status === "running") return "";
  return `${scan.id || 0}:${scan.completed_at || 0}:${scan.status}`;
}

function dismissArchiveScan(scan = state.archiveScan) {
  clearTimeout(archiveScanDismissTimer);
  const k = archiveScanKey(scan);
  if (k) {
    state.archiveScanDismissedKey = k;
    localStorage.setItem(DISMISSED_SCAN_KEY, k);
  } else {
    state.archiveScan = {
      status: "idle",
      imported: 0,
      skipped: 0,
      conflicts: 0,
    };
  }
}

function scheduleArchiveScanAutoDismiss(scan) {
  clearTimeout(archiveScanDismissTimer);
  if (!scan || (scan.status !== "success" && scan.status !== "skipped")) return;
  const k = archiveScanKey(scan);
  if (!k || k === state.archiveScanDismissedKey) return;
  archiveScanDismissTimer = setTimeout(() => dismissArchiveScan(scan), 8000);
}

async function loadArchiveScanStatus() {
  try {
    const wasRunning = state.archiveScan.status === "running";
    state.archiveScan = (await api("/api/archive/import/scan")).data;
    if (wasRunning && state.archiveScan.status !== "running")
      await loadArchives();
    scheduleArchiveScanAutoDismiss(state.archiveScan);
  } catch (e) {
    if (e.status !== 401)
      state.archiveScan = {
        ...state.archiveScan,
        status: "error",
        error: e.message,
      };
  }
}
async function startArchiveScan() {
  if (state.archiveScan.status === "running") return;
  clearTimeout(archiveScanDismissTimer);
  state.archiveScanDismissedKey = "";
  localStorage.removeItem(DISMISSED_SCAN_KEY);
  try {
    state.archiveScan = (
      await api("/api/archive/import/scan", { method: "POST" })
    ).data;
    scheduleArchiveScanAutoDismiss(state.archiveScan);
  } catch (e) {
    if (e.status !== 401)
      state.archiveScan = {
        ...state.archiveScan,
        status: "error",
        error: e.message,
      };
  }
}
function parseInput() {
  const s = state.archiveInput.trim();
  let m =
    s.match(/\/(?:g|mpv)\/(\d+)\/([0-9a-fA-F]+)/) ||
    s.match(/(?:gid|g)=?(\d+)[^0-9a-fA-F]+(?:token|t)=?([0-9a-fA-F]+)/) ||
    s.match(/^(\d+)[\s,;\/]+([0-9a-fA-F]{6,})\/?$/);
  return m ? { gid: m[1], token: m[2] } : null;
}
async function getQuote(action = "start", target = null) {
  const input = state.archiveInput.trim();
  const gallery = target || parseInput();
  state.quote = null;
  state.quoteFor = "";
  state.selectedQuality = "";
  state.quoteAction = action;
  state.quoteTarget = target;
  if (!gallery) {
    state.error = t("notice.invalid_input");
    return;
  }
  await run(async () => {
    const result = (
      await api("/api/archive/" + gallery.gid + "/" + gallery.token + "/quote")
    ).data;
    if (state.archiveInput.trim() !== input)
      throw Error(t("notice.quote_expired"));
    const option = (result.options || []).find(
      (item) =>
        item.available &&
        (action !== "update" ||
          String(item.or) === String(target.root.quality)),
    );
    if (!option) throw Error(result.error || t("notice.no_quality_option"));
    if (action === "update") {
      result.options = result.options.filter(
        (item) => String(item.or) === String(target.root.quality),
      );
    }
    state.quote = result;
    state.quoteFor = input;
    state.selectedQuality = option.or;
  });
}
function quoteArchiveFor(a) {
  state.archiveInput = a.gid + " " + a.token;
  return getQuote(a.status === "unarchived" ? "start" : "refresh", {
    gid: a.gid,
    token: a.token,
  });
}
function quoteUpdateFor(a) {
  const target = a.update_available;
  if (!target || !target.gid || !target.token) return;
  state.archiveInput = a.gid + " " + a.token;
  return getQuote("update", {
    root: { gid: a.gid, token: a.token, quality: a.or },
    target: { gid: target.gid, token: target.token },
  });
}
function openArchiveConfirm() {
  if (!state.quote || state.quoteFor !== state.archiveInput.trim())
    return (state.error = t("notice.quote_expired"));
  if (
    !state.quote.options?.some(
      (o) => o.available && String(o.or) === String(state.selectedQuality),
    )
  )
    return (state.error = t("notice.select_quality_tier"));
  state.confirmArchive = true;
}
function hasActiveArchives() {
  const entries = state.archives || [];
  return entries.some((a) =>
    ["pending", "submitting", "staging", "downloading", "zipping"].includes(
      a?.status,
    ),
  );
}

function setFavoriteOverride(gid, token, isFavorited, favcat = null) {
  if (!gid || !token) return;
  const key = `${gid}:${token}`;
  const overrides = { ...(state.favoriteOverrides || {}) };
  if (isFavorited) {
    const catId = favcat != null ? Number(favcat) : 0;
    const found = (state.favcats || []).find((c) => Number(c.id) === catId);
    overrides[key] = {
      isFavorited: true,
      favcat: catId,
      favcatName: found?.name || t("archive.favcat_default", { id: catId }),
    };
  } else {
    overrides[key] = {
      isFavorited: false,
      favcat: null,
      favcatName: "",
    };
  }
  state.favoriteOverrides = overrides;
}

async function startArchive() {
  const update = state.quoteAction === "update";
  const p = update
    ? state.quoteTarget?.target
    : state.quoteTarget || parseInput();
  const option = state.quote?.options?.find(
    (o) => o.available && String(o.or) === String(state.selectedQuality),
  );
  if (
    !p ||
    !option ||
    state.quoteFor !== state.archiveInput.trim() ||
    (update &&
      (!state.quoteTarget?.root ||
        String(option.or) !== String(state.quoteTarget.root.quality)))
  )
    return (state.error = t("notice.quote_expired_or_invalid"));
  state.confirmArchive = false;
  const quality = state.selectedQuality;
  state.quote = null;
  state.quoteFor = "";
  await run(async () => {
    if (update) {
      const target = state.quoteTarget.target;
      const root = state.quoteTarget.root;
      let maxGP = 0;
      if (!option.unlocked) {
        maxGP = Number(option.gp_price);
        if (
          option.gp_price == null ||
          option.gp_price === "" ||
          !Number.isFinite(maxGP) ||
          maxGP < 0
        )
          return (state.error = t("notice.invalid_gp"));
      }
      await api("/api/archive/" + root.gid + "/" + root.token + "/update", {
        method: "POST",
        body: JSON.stringify({
          gid: target.gid,
          token: target.token,
          max_gp: maxGP,
        }),
      });
      state.notice = t("notice.update_submitted");
    } else {
      const result = await api(
        "/api/archive/" + p.gid + "/" + p.token + "/start",
        {
          method: "POST",
          body: JSON.stringify({
            quality,
            force: state.quoteAction === "refresh",
          }),
        },
      );
      state.notice = result.data.skipped
        ? t("notice.archive_skipped")
        : t("notice.archive_submitted");
      if (result.data && !result.data.skipped) {
        const itemGid = Number(p.gid);
        const itemToken = String(p.token);
        const list = [...(state.archives || [])];
        const idx = list.findIndex(
          (a) =>
            (a.gid === itemGid || a.active_gid === itemGid) &&
            (a.token === itemToken || a.active_token === itemToken),
        );
        if (idx >= 0) {
          list[idx] = { ...list[idx], status: "pending" };
        } else {
          list.unshift({
            gid: itemGid,
            token: itemToken,
            title: p.title || "",
            status: "pending",
            quality,
          });
        }
        state.archives = list;
      }
    }
    state.quote = null;
    state.quoteFor = "";
    state.quoteTarget = null;
    state.quoteAction = "start";
    await loadArchives();
  });
}
async function refreshMetadata(a) {
  await run(async () => {
    await api("/api/archive/" + a.gid + "/" + a.token + "/metadata/refresh", {
      method: "POST",
      body: JSON.stringify({}),
    });
    state.notice = t("notice.meta_refreshed");
    await loadArchives();
  });
}
async function requestFavoriteRemove(a) {
  state.favoriteRemoveTarget = a;
  state.deleteAfterFavoriteRemove = false;
  state.confirmFavoriteRemove = true;
}
async function confirmFavoriteRemoval() {
  const target = state.favoriteRemoveTarget;
  state.confirmFavoriteRemove = false;
  if (!target) return;
  await run(async () => {
    const result = await api("/api/favorites", {
      method: "POST",
      body: JSON.stringify({
        action: "remove",
        gid: Number(target.gid),
        token: target.token,
      }),
    });
    if (result.data.ok !== true)
      throw Error(
        result.data.detail ||
          result.data.items?.[0]?.error ||
          t("notice.fav_remove_failed"),
      );
    if (state.deleteAfterFavoriteRemove && target.status !== "unarchived") {
      await api("/api/archive/" + target.gid + "/" + target.token, {
        method: "DELETE",
      });
    }
    state.notice =
      target.status === "unarchived"
        ? t("notice.fav_removed_unarchived")
        : state.deleteAfterFavoriteRemove
          ? t("notice.fav_removed_deleted")
          : t("notice.fav_removed");
    state.deleteAfterFavoriteRemove = false;
    setFavoriteOverride(target.gid, target.token, false);
    await loadArchives();
  });
}
function requestDelete(a) {
  state.deleteTarget = a;
  state.confirmDelete = true;
}
async function deleteArchive() {
  const a = state.deleteTarget;
  state.confirmDelete = false;
  if (!a) return;
  await run(async () => {
    const res = (
      await api("/api/archive/" + a.gid + "/" + a.token, { method: "DELETE" })
    ).data;
    if (a.status === "failed") {
      state.notice = res.retained_catalog
        ? t("notice.failed_cleared_catalog_kept")
        : t("notice.failed_cleared");
    } else if (
      ["pending", "downloading", "zipping", "submitting"].includes(a.status)
    ) {
      state.notice = res.retained_catalog
        ? t("notice.task_cancelled_catalog_kept")
        : t("notice.task_cancelled");
    } else if (a.status === "unarchived") {
      state.notice = t("notice.orphan_removed");
    } else {
      state.notice = res.retained_catalog
        ? t("notice.archive_deleted_catalog_kept")
        : t("notice.archive_deleted");
    }
    await loadArchives();
  });
}
async function syncFavorites(full = false) {
  await run(async () => {
    const query = full ? "?full=1" : "";
    const result = (
      await api("/api/favorites/sync/run" + query, { method: "POST" })
    ).data;
    if (result.skipped) {
      state.notice = result.error || t("notice.sync_skipped");
    } else {
      state.notice = t("notice.sync_complete", {
        scanned: result.scanned_pages ?? 0,
        newCount: result.new_count ?? 0,
        cataloged: result.cataloged_count ?? 0,
      });
    }
    await Promise.all([loadArchives(), loadCategories(), loadStatus()]);
  });
}
function askClearCache() {
  state.confirmCache = true;
}
async function clearCache() {
  state.confirmCache = false;
  await run(async () => {
    const r = await api("/api/cache/clear", { method: "POST" });
    state.notice = t("notice.cache_cleared", { count: r.data.cleared ?? "" });
    await loadStatus();
  });
}
async function favorite(a, action, favcat) {
  await run(async () => {
    const r = await api("/api/favorites", {
      method: "POST",
      body: JSON.stringify({
        action,
        gid: Number(a.gid),
        token: a.token,
        ...(favcat == null ? {} : { favcat }),
      }),
    });
    if (r.data.ok === false)
      throw Error(
        r.data.detail || r.data.items?.[0]?.error || t("notice.fav_op_failed"),
      );
    state.notice =
      action === "remove" ? t("notice.fav_removed") : t("notice.fav_updated");
    setFavoriteOverride(a.gid, a.token, action !== "remove", favcat);
    await loadArchives();
  });
}
async function loadCategories() {
  try {
    state.favcats =
      (await api("/api/favorites/categories")).data.categories || [];
  } catch {
    state.favcats = [];
  }
}
function enterExploreSubRoute(navItem, pushHistory = true) {
  if (!navItem) return;
  state.page = "explore";
  state.exploreSubRoute = {
    title: navItem.title || t("notice.gallery_list"),
    href: navItem.href,
    baseHref: navItem.baseHref || null,
    fromHome: navItem.fromHome || false,
  };
  if (pushHistory && typeof window !== "undefined") {
    window.history.pushState(
      { page: "explore", subRoute: { ...state.exploreSubRoute } },
      "",
      `#explore/${encodeURIComponent(navItem.title || "list")}`,
    );
  }
}

function replaceExploreSubRoute(navItem) {
  if (!navItem) return;
  state.page = "explore";
  state.exploreSubRoute = {
    title: navItem.title || t("notice.gallery_list"),
    href: navItem.href,
    baseHref: navItem.baseHref || null,
    fromHome: navItem.fromHome || false,
  };
  if (typeof window !== "undefined") {
    window.history.replaceState(
      { page: "explore", subRoute: { ...state.exploreSubRoute } },
      "",
      `#explore/${encodeURIComponent(navItem.title || "list")}`,
    );
  }
}

function exitExploreSubRoute(popHistory = true) {
  if (
    popHistory &&
    typeof window !== "undefined" &&
    window.history.state?.subRoute
  ) {
    window.history.back();
  } else {
    state.exploreSubRoute = null;
    if (typeof window !== "undefined") {
      window.history.replaceState(
        { page: "explore", subRoute: null },
        "",
        "#explore",
      );
    }
  }
}

function onPopState(e) {
  const s = e.state;
  if (!s) {
    const rawHash =
      typeof window !== "undefined"
        ? window.location.hash.replace(/^#\/?/, "")
        : "";
    const parts = rawHash.split("/");
    const rootPage = parts[0] || "dashboard";
    if (
      ["dashboard", "explore", "archive", "config", "home"].includes(rootPage)
    ) {
      state.page = rootPage;
    }
    if (rootPage !== "explore" || !parts[1]) {
      state.exploreSubRoute = null;
    }
    return;
  }
  if (s.page && s.page !== state.page) {
    route(s.page, false);
  }
  state.exploreSubRoute = s.subRoute || null;
}

function route(v, historyMode = true) {
  state.page = v;
  state.mobile = false;
  state.error = "";
  if (v !== "explore" || historyMode !== false) {
    state.exploreSubRoute = null;
  }
  if (v === "dashboard") loadStatus();
  if (v === "explore") {
    loadHome();
    loadArchives();
    loadCategories();
  }
  if (v === "home") {
    loadHome();
  }
  if (v === "config") loadConfig();
  if (v === "archive") {
    loadArchives();
    loadArchiveScanStatus();
    loadCategories();
    loadStatus();
  }
  if (typeof window !== "undefined") {
    if (historyMode === true) {
      window.history.pushState({ page: v, subRoute: null }, "", `#${v}`);
    } else if (historyMode === "replace") {
      window.history.replaceState({ page: v, subRoute: null }, "", `#${v}`);
    }
  }
}
async function login(username, password) {
  const token = btoa(`${username}:${password}`);
  state.busy = true;
  state.loginError = "";
  try {
    const r = await api("/api/status", {
      headers: { Authorization: `Basic ${token}` },
    });
    state.authToken = token;
    localStorage.setItem("pandaopds_auth", token);
    state.authChecked = true;
    state.authRequired = false;
    state.error = "";
    state.status = r.data;
    state.loginError = "";
    if (state.page === "config") loadConfig();
    if (state.page === "explore" || state.page === "home") {
      loadHome();
      loadArchives();
    }
    if (state.page === "archive") {
      loadArchives();
      loadCategories();
    }
    return true;
  } catch (e) {
    state.loginError =
      e.status === 401
        ? t("auth.error_credentials")
        : e.message || t("auth.error_failed");
    return false;
  } finally {
    state.busy = false;
  }
}

function logout() {
  state.authToken = "";
  localStorage.removeItem("pandaopds_auth");
  clearCoverCache();
  state.status = null;
  state.error = "";
  state.notice = "";
  state.loginError = "";
  state.authRequired = true;
}

let timer, archiveTimer, archiveScanTimer, media;
function init() {
  media = matchMedia("(prefers-color-scheme: dark)");
  applyTheme();
  media.addEventListener?.("change", applyTheme);
  loadStatus();
  loadArchives();
  loadCategories();
  if (typeof window !== "undefined") {
    const rawHash = window.location.hash.replace(/^#\/?/, "");
    const parts = rawHash.split("/");
    const rootPage = parts[0];
    if (
      ["dashboard", "explore", "archive", "config", "home"].includes(rootPage)
    ) {
      route(rootPage, false);
      if (rootPage === "explore" && window.history.state?.subRoute) {
        state.exploreSubRoute = window.history.state.subRoute;
      }
    }
    if (!window.history.state) {
      window.history.replaceState(
        { page: state.page, subRoute: state.exploreSubRoute },
        "",
        window.location.hash || `#${state.page}`,
      );
    }
    window.addEventListener("popstate", onPopState);
  }
  timer = setInterval(() => {
    if (state.authChecked && !state.authRequired && state.page === "dashboard")
      loadStatus();
  }, 15000);
  let archiveTicks = 0;
  archiveTimer = setInterval(() => {
    if (!state.authChecked || state.authRequired) return;
    archiveTicks++;
    const active = hasActiveArchives();
    if (active) {
      loadArchives();
    } else if (state.page === "archive" && archiveTicks % 3 === 0) {
      loadArchives();
    }
  }, 3000);
  archiveScanTimer = setInterval(() => {
    if (
      state.authChecked &&
      !state.authRequired &&
      state.page === "archive" &&
      state.archiveScan.status === "running"
    )
      loadArchiveScanStatus();
  }, 1000);
}
function destroy() {
  clearInterval(timer);
  clearInterval(archiveTimer);
  clearInterval(archiveScanTimer);
  clearTimeout(archiveScanDismissTimer);
  media?.removeEventListener?.("change", applyTheme);
  if (typeof window !== "undefined") {
    window.removeEventListener("popstate", onPopState);
  }
}
export function useApp() {
  return {
    ...toRefs(state),
    route,
    enterExploreSubRoute,
    replaceExploreSubRoute,
    exitExploreSubRoute,
    setTheme,
    login,
    logout,
    loadStatus,
    loadConfig,
    loadHome,
    saveConfig,
    saveHome,
    setField,
    selectHomeMode,
    selectActivePage,
    addPage,
    removePage,
    updatePageMeta,
    reorderPages,
    resetHomeToDefault,
    addSearchFacet,
    removeSearchFacet,
    moveSearchFacet,
    toggleSearchFacetCategory,
    toggleSearchFacetFilter,
    canAddFacet,
    addGlobalNavigation,
    removeGlobalNavigation,
    moveGlobalNavigation,
    addNavigation,
    addPublication,
    removePublication,
    addFacet,
    addFacetSet,
    removeFacetSet,
    moveFacetSet,
    addFacetItem,
    removeFacetItem,
    moveFacetItem,
    toggleFacetCategory,
    toggleFacetFilter,
    addPageFacetReference,
    removePageFacetReference,
    movePageFacetReference,
    addGroupSection,
    addGroupNavigationItem,
    removeGroupNavigationItem,
    moveGroupNavigationItem,
    removeItem,
    moveItem,
    loadArchives,
    loadArchiveScanStatus,
    startArchiveScan,
    dismissArchiveScan,
    loadCategories,
    getQuote,
    quoteArchiveFor,
    quoteUpdateFor,
    openArchiveConfirm,
    startArchive,
    askClearCache,
    refreshMetadata,
    requestDelete,
    deleteArchive,
    clearCache,
    favorite,
    setFavoriteOverride,
    hasActiveArchives,
    requestFavoriteRemove,
    confirmFavoriteRemoval,
    syncFavorites,
    init,
    destroy,
    api,
    peekCoverUrl,
    resolveCoverUrl,
    fetchAuthedBlobUrl,
    fetchReaderBlobUrl,
    clearCoverCache,
    getProgression,
    putProgression,
    updateLocalProgress,
    getGalleryProgress,
    progressionCache,
  };
}

import { reactive, toRefs } from "vue";
const state = reactive({
  page: "dashboard",
  mobile: false,
  theme: localStorage.theme || "system",
  busy: false,
  authRequired: false,
  loginError: "",
  authToken: localStorage.getItem("pandaopds_auth") || "",
  notice: "",
  error: "",
  status: null,
  config: null,
  home: null,
  archives: [],
  archiveStats: {},
  favcats: [],
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
  form: {},
  layout: { groups: [], sections: [] },
});
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
    state.authRequired = true;
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
async function run(fn) {
  state.busy = true;
  state.error = "";
  try {
    await fn();
  } catch (e) {
    state.error = e.message || String(e);
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
    state.error = e.message;
  }
}
async function loadConfig() {
  try {
    const r = await api("/api/config/edit");
    state.config = r.data;
    state.etag = r.headers.get("ETag") || "";
    state.confDirty = {};
    state.form = {};
    for (const [k, v] of Object.entries(r.data.fields || {}))
      state.form[k] = v.saved ?? v.active ?? "";
  } catch (e) {
    try {
      state.config = { legacy: (await api("/api/config")).data };
      state.error = "编辑接口不可用，当前显示只读配置";
    } catch {
      state.error = e.message;
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
      if (Boolean(a) !== Boolean(b))
        throw Error(
          "IPB Member ID 和 IPB Pass Hash 必须同时填写；同时留空可清除登录态。",
        );
      patch.ipb_member_id = a || null;
      patch.ipb_pass_hash = b || null;
    }
    await api("/api/config/edit", {
      method: "PATCH",
      headers: { "If-Match": state.etag },
      body: JSON.stringify(patch),
    });
    state.notice = "设置已保存";
    await loadConfig();
  });
}
async function loadHome() {
  try {
    const r = await api("/api/home/edit");
    state.home = r.data;
    state.homeTag = r.headers.get("ETag") || "";
    state.layout = {
      groups: structuredClone(r.data.groups || []),
      sections: structuredClone(r.data.sections || []),
    };
    state.homeDirty = false;
  } catch (e) {
    try {
      state.home = { legacy: (await api("/api/home")).data };
      state.error = "首页编辑接口不可用，当前显示只读布局";
    } catch {
      state.error = e.message;
    }
  }
}
async function saveHome() {
  await run(async () => {
    await api("/api/home/edit", {
      method: "PUT",
      headers: { "If-Match": state.homeTag },
      body: JSON.stringify({
        ...state.layout,
        sections: state.layout.sections.map((x) => ({
          ...x,
          count: x.count === "" || x.count == null ? null : Number(x.count),
        })),
      }),
    });
    state.notice = "首页布局已保存";
    await loadHome();
  });
}
function addSection() {
  state.layout.sections.push({
    group: "",
    kind: "publication",
    title: "热门画廊",
    type: "preset",
    query: "popular",
    count: null,
  });
  state.homeDirty = true;
}
function removeSection(i) {
  state.layout.sections.splice(i, 1);
  state.homeDirty = true;
}
function movePreviewItem(key, delta) {
  const keys = state.layout.sections.map((section, index) => {
    if (section.group) return "group:" + section.group;
    if (section.kind === "navigation") return "root-navigation";
    return "root-publication:" + index;
  });
  const items = [];
  const seen = new Set();
  state.layout.sections.forEach((section, index) => {
    const itemKey = keys[index];
    if (seen.has(itemKey)) return;
    seen.add(itemKey);
    const sections = itemKey.startsWith("group:")
      ? state.layout.sections.filter(
          (item) => "group:" + item.group === itemKey,
        )
      : itemKey === "root-navigation"
        ? state.layout.sections.filter(
            (item) => !item.group && item.kind === "navigation",
          )
        : [section];
    items.push({ key: itemKey, sections });
  });
  const from = items.findIndex((item) => item.key === key);
  const to = from + delta;
  if (from < 0 || to < 0 || to >= items.length) return;
  const [item] = items.splice(from, 1);
  items.splice(to, 0, item);
  state.layout.sections = items.flatMap((entry) => entry.sections);
  state.homeDirty = true;
}
function moveSection(i, delta) {
  const j = i + delta;
  if (j < 0 || j >= state.layout.sections.length) return;
  const a = state.layout.sections.splice(i, 1)[0];
  state.layout.sections.splice(j, 0, a);
  state.homeDirty = true;
}
function addGroup() {
  let n = 1,
    id = "group";
  while (state.layout.groups.some((g) => g.id === id)) {
    n++;
    id = "group_" + n;
  }
  state.layout.groups.push({ id, title: "新分组" });
  state.homeDirty = true;
}
function renameGroup(index, nextID) {
  const group = state.layout.groups[index];
  const id = nextID.trim();
  if (
    !/^[A-Za-z0-9_-]+$/.test(id) ||
    state.layout.groups.some((item, i) => i !== index && item.id === id)
  ) {
    state.error =
      "分组标识不能为空，且必须唯一，只能包含字母、数字、下划线和连字符。";
    return false;
  }
  const oldID = group.id;
  group.id = id;
  for (const section of state.layout.sections)
    if (section.group === oldID) section.group = id;
  state.homeDirty = true;
  return true;
}
function removeGroup(id) {
  state.layout.groups = state.layout.groups.filter((g) => g.id !== id);
  for (const sec of state.layout.sections) if (sec.group === id) sec.group = "";
  state.homeDirty = true;
}
async function loadArchives() {
  try {
    const d = (await api("/api/archive")).data;
    state.archives = d.entries || d.items || (Array.isArray(d) ? d : []);
    state.archiveStats = d.stats || {};
  } catch (e) {
    state.error = e.message;
  }
}
function parseInput() {
  const s = state.archiveInput.trim();
  let m =
    s.match(/\/(?:g|mpv)\/(\d+)\/([0-9a-fA-F]+)/) ||
    s.match(/(?:gid|g)=?(\d+)[^0-9a-fA-F]+(?:token|t)=?([0-9a-fA-F]+)/) ||
    s.match(/^(\d+)[\s,;]+([0-9a-fA-F]{8,})$/);
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
    state.error =
      "无法解析 gid/token。请输入画廊 URL、archiver URL 或 gid token。";
    return;
  }
  await run(async () => {
    const result = (
      await api("/api/archive/" + gallery.gid + "/" + gallery.token + "/quote")
    ).data;
    if (state.archiveInput.trim() !== input)
      throw Error("报价已过期，因为输入内容发生了变化。请重新查询。");
    const option = (result.options || []).find((item) => item.available);
    if (!option) throw Error(result.error || "没有可用的画质档");
    state.quote = result;
    state.quoteFor = input;
    state.selectedQuality = option.or;
  });
}
function quoteArchiveFor(a) {
  state.archiveInput = a.gid + " " + a.token;
  return getQuote("refresh", { gid: a.gid, token: a.token });
}
function openArchiveConfirm() {
  if (!state.quote || state.quoteFor !== state.archiveInput.trim())
    return (state.error = "查询报价后若输入有变化，请重新查询。");
  if (
    !state.quote.options?.some(
      (o) => o.available && String(o.or) === String(state.selectedQuality),
    )
  )
    return (state.error = "请选择可用画质档。");
  state.confirmArchive = true;
}
async function startArchive() {
  const p = state.quoteTarget || parseInput();
  const option = state.quote?.options?.find(
    (o) => o.available && String(o.or) === String(state.selectedQuality),
  );
  if (!p || !option || state.quoteFor !== state.archiveInput.trim())
    return (state.error = "报价已过期或画质档无效，请重新查询。");
  state.confirmArchive = false;
  const quality = state.selectedQuality;
  state.quote = null;
  state.quoteFor = "";
  await run(async () => {
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
      ? "该画廊已归档，未重复提交任务。"
      : "归档任务已提交";
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
    state.notice = "画廊元数据已刷新";
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
        result.data.detail || result.data.items?.[0]?.error || "移除收藏失败",
      );
    if (state.deleteAfterFavoriteRemove) {
      await api("/api/archive/" + target.gid + "/" + target.token, {
        method: "DELETE",
      });
    }
    state.notice = state.deleteAfterFavoriteRemove
      ? "已移除收藏并删除本地归档。"
      : "已移除收藏。";
    state.deleteAfterFavoriteRemove = false;
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
    await api("/api/archive/" + a.gid + "/" + a.token, { method: "DELETE" });
    state.notice = "本地归档已删除";
    await loadArchives();
  });
}
function askClearCache() {
  state.confirmCache = true;
}
async function clearCache() {
  state.confirmCache = false;
  await run(async () => {
    const r = await api("/api/cache/clear", { method: "POST" });
    state.notice = "已清理 " + (r.data.cleared ?? "") + " 条缓存";
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
      throw Error(r.data.detail || r.data.items?.[0]?.error || "收藏操作失败");
    state.notice = action === "remove" ? "已移除收藏" : "收藏类别已更新";
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
function route(v) {
  state.page = v;
  state.mobile = false;
  state.error = "";
  if (v === "dashboard") loadStatus();
  if (v === "config") loadConfig();
  if (v === "home") loadHome();
  if (v === "archive") {
    loadArchives();
    loadCategories();
    loadStatus();
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
    state.authRequired = false;
    state.status = r.data;
    state.loginError = "";
    return true;
  } catch (e) {
    state.loginError = "用户名或密码错误，请重试";
    return false;
  } finally {
    state.busy = false;
  }
}

function logout() {
  state.authToken = "";
  localStorage.removeItem("pandaopds_auth");
  state.status = null;
  state.authRequired = true;
}

let timer, archiveTimer, media;
function init() {
  media = matchMedia("(prefers-color-scheme: dark)");
  applyTheme();
  media.addEventListener?.("change", applyTheme);
  loadStatus();
  timer = setInterval(() => {
    if (!state.authRequired && state.page === "dashboard") loadStatus();
  }, 15000);
  archiveTimer = setInterval(() => {
    if (!state.authRequired && state.page === "archive") loadArchives();
  }, 10000);
}
function destroy() {
  clearInterval(timer);
  clearInterval(archiveTimer);
  media?.removeEventListener?.("change", applyTheme);
}
export function useApp() {
  return {
    ...toRefs(state),
    route,
    setTheme,
    login,
    logout,
    loadStatus,
    loadConfig,
    loadHome,
    saveConfig,
    saveHome,
    setField,
    addSection,
    removeSection,
    moveSection,
    addGroup,
    removeGroup,
    renameGroup,
    movePreviewItem,
    loadArchives,
    loadCategories,
    getQuote,
    quoteArchiveFor,
    openArchiveConfirm,
    startArchive,
    askClearCache,
    refreshMetadata,
    requestDelete,
    deleteArchive,
    clearCache,
    favorite,
    requestFavoriteRemove,
    confirmFavoriteRemoval,
    init,
    destroy,
  };
}

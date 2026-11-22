export function subscriptionItemsPath({
  subscriptionId,
  subscriptionIds,
  view,
  page,
  perPage,
  q = "",
  category = "",
}) {
  const query = new URLSearchParams({
    view,
    page: String(page),
    per_page: String(perPage),
  });
  if (q.trim()) query.set("q", q.trim());
  if (category) query.set("category", category);
  if (subscriptionIds?.length) {
    query.set("subscription_ids", subscriptionIds.join(","));
    return `/api/subscription-items?${query}`;
  }
  return `/api/subscriptions/${encodeURIComponent(subscriptionId)}/items?${query}`;
}

export async function loadSubscriptionItems(api, options) {
  return api(subscriptionItemsPath(options));
}

function applyGalleryCategorySelection(query, value) {
  const categories = parseCategoryIDs(value);
  if (categories.length) {
    query.categories = categories;
  } else {
    delete query.categories;
    delete query.category_mask;
  }
  return query;
}

function autoArchiveMode(draft) {
  if (["disabled", "res", "org"].includes(draft.auto_archive))
    return draft.auto_archive;
  return draft.auto_download ? draft.quality || "res" : "disabled";
}

export function createSubscriptionPayload(draft) {
  const source = draft.source === "search" ? "search" : "preset";
  const preset = draft.preset || "latest";
  const query =
    source === "preset"
      ? applyGalleryCategorySelection({ preset, max_pages: 1 }, draft.query_categories)
      : {
          keywords: String(draft.keywords || "").trim(),
          categories: parseCategoryIDs(
            draft.query_categories ?? draft.categories,
          ),
          sort: "newest",
          max_pages: 1,
        };
  const archiveMode = autoArchiveMode(draft);
  const autoDownload = archiveMode !== "disabled";
  return {
    name: String(draft.name || "").trim(),
    source,
    query,
    interval_seconds: Number(draft.interval ?? 3600),
    enabled: true,
    auto_download: autoDownload,
    download_rules: autoDownload ? { quality: archiveMode } : {},
  };
}

export function createSubscriptionUpdate(subscription, draft) {
  const query =
    subscription.source === "preset"
      ? applyGalleryCategorySelection({
          ...subscription.query,
          preset: draft.preset || subscription.query?.preset || "latest",
          max_pages: 1,
        }, draft.query_categories)
      : {
          ...subscription.query,
          keywords: String(draft.keywords || "").trim(),
          categories: parseCategoryIDs(draft.query_categories),
          sort: "newest",
          max_pages: 1,
        };
  const archiveMode = autoArchiveMode(draft);
  const autoDownload = archiveMode !== "disabled";
  const downloadRules = autoDownload ? { quality: archiveMode } : {};
  return {
    name: String(draft.name || "").trim(),
    query,
    interval_seconds: Number(draft.interval_seconds),
    enabled: true,
    auto_download: autoDownload,
    download_rules: downloadRules,
  };
}

export function buildCleanupPayload(mode) {
  if (!["inspected", "hidden"].includes(mode))
    throw new Error("Unsupported cleanup mode");
  return { mode, acknowledged: true };
}

export function buildReviewPayload(resolution) {
  if (!["reconcile", "allow_manual_retry"].includes(resolution))
    throw new Error("Unsupported review resolution");
  return { resolution, acknowledged: true };
}

export function buildBatchDownloadPayload(items, quality) {
  if (!["res", "org"].includes(quality)) throw new Error("Unsupported quality");
  if (!Array.isArray(items) || items.length < 1 || items.length > 200)
    throw new Error("Select between 1 and 200 publications");
  return {
    acknowledged: true,
    items: items.map((item) => ({
      gid: Number(item.gid),
      token: String(item.token),
      quality,
      force: false,
    })),
  };
}

export function cleanupSubscription(api, subscriptionId, mode) {
  return api(
    `/api/subscriptions/${encodeURIComponent(subscriptionId)}/cleanup`,
    {
      method: "POST",
      body: JSON.stringify(buildCleanupPayload(mode)),
    },
  );
}

export function reviewDownloadJob(api, jobId, resolution) {
  return api(`/api/download-jobs/${encodeURIComponent(jobId)}/review`, {
    method: "POST",
    body: JSON.stringify(buildReviewPayload(resolution)),
  });
}

export function submitBatchDownload(api, items, quality) {
  return api("/api/download-jobs/batch", {
    method: "POST",
    body: JSON.stringify(buildBatchDownloadPayload(items, quality)),
  });
}

export function parseCategoryIDs(value) {
  const values = Array.isArray(value)
    ? value
    : String(value || "").trim()
      ? String(value).split(",")
      : [];
  if (!values.length) return [];
  const ids = values.map((part) => {
    const token = String(part).trim();
    if (!/^\d+$/.test(token))
      throw new Error("Category IDs must be integers from 0 to 9");
    const id = Number(token);
    if (id < 0 || id > 9)
      throw new Error("Category IDs must be integers from 0 to 9");
    return id;
  });
  return [...new Set(ids)];
}

export function documentReaderPaths(documentId, page) {
  const id = encodeURIComponent(documentId);
  return {
    stream: `/stream/documents/${id}/page/${page}`,
    progression: `/opds/progression/documents/${id}`,
  };
}

export function archiveProgressionURL(archive) {
  if (!archive) return "";
  const explicit = archive.progression_url;
  if (typeof explicit === "string" && explicit.startsWith("/opds/progression/"))
    return explicit;
  const documentId = archive.document_id || (!archive.gid ? archive.id : "");
  if (documentId) return documentReaderPaths(documentId, 1).progression;
  const gid = archive.active_gid || archive.gid;
  const token = archive.active_token || archive.token;
  return gid && token
    ? `/opds/progression/${gid}/${encodeURIComponent(token)}`
    : "";
}

export function archiveProgressionKey(archive) {
  const path = archiveProgressionURL(archive);
  const ehMatch = path.match(/^\/opds\/progression\/([^/]+)\/([^/]+)$/);
  if (ehMatch)
    return `${decodeURIComponent(ehMatch[1])}:${decodeURIComponent(ehMatch[2])}`;
  const documentMatch = path.match(/^\/opds\/progression\/documents\/([^/]+)$/);
  if (documentMatch) return `document:${decodeURIComponent(documentMatch[1])}`;
  return "";
}

// A sequence gate keeps slow responses from a previous selection from replacing
// the currently visible local result set.
export function createLatestRequest() {
  let sequence = 0;
  return {
    next() {
      sequence += 1;
      return sequence;
    },
    isCurrent(id) {
      return id === sequence;
    },
    invalidate() {
      sequence += 1;
    },
  };
}

export function createSubscriptionItemsController(api) {
  const gate = createLatestRequest();
  return {
    async load(options) {
      const requestId = gate.next();
      try {
        const result = await loadSubscriptionItems(api, options);
        return gate.isCurrent(requestId) ? result : null;
      } catch (error) {
        if (gate.isCurrent(requestId)) throw error;
        return null;
      }
    },
    invalidate() {
      gate.invalidate();
    },
  };
}

export function createSubscriptionPoller({
  loadSubscriptions,
  loadItems,
  getSubscriptions,
  setTimer = setInterval,
  clearTimer = clearInterval,
  interval = 2500,
}) {
  let timer;
  let running = false;
  let polling = false;
  async function pollOnce() {
    if (polling) return;
    polling = true;
    try {
      await loadSubscriptions();
      await loadItems();
    } finally {
      polling = false;
    }
    if (!getSubscriptions().some((sub) => sub.running)) stop();
  }
  function start() {
    if (running) return;
    running = true;
    timer = setTimer(() => {
      void pollOnce();
    }, interval);
  }
  function stop() {
    if (timer !== undefined) clearTimer(timer);
    timer = undefined;
    running = false;
  }
  return { start, stop, pollOnce };
}

export const EH_CATEGORIES = [
  {
    id: 0,
    key: "Doujinshi",
    label: "Doujinshi",
    color: "#fc4e4e",
    facetColor: "#ed5565",
    facetTextColor: "#ffffff",
  },
  {
    id: 1,
    key: "Manga",
    label: "Manga",
    color: "#e58200",
    facetColor: "#f6bb42",
    facetTextColor: "#ffffff",
  },
  {
    id: 2,
    key: "Artist CG",
    label: "Artist CG",
    color: "#dde500",
    facetColor: "#d0b400",
    facetTextColor: "#ffffff",
  },
  {
    id: 3,
    key: "Game CG",
    label: "Game CG",
    color: "#05bf0b",
    facetColor: "#2ecc71",
    facetTextColor: "#ffffff",
  },
  {
    id: 4,
    key: "Western",
    label: "Western",
    color: "#14e723",
    facetColor: "#8cc152",
    facetTextColor: "#ffffff",
  },
  {
    id: 5,
    key: "Non-H",
    label: "Non-H",
    color: "#68c9de",
    facetColor: "#4fc1e9",
    facetTextColor: "#ffffff",
  },
  {
    id: 6,
    key: "Image Set",
    label: "Image Set",
    color: "#5050d7",
    facetColor: "#5d9cec",
    facetTextColor: "#ffffff",
  },
  {
    id: 7,
    key: "Cosplay",
    label: "Cosplay",
    color: "#9755f5",
    facetColor: "#ac92ec",
    facetTextColor: "#ffffff",
  },
  {
    id: 8,
    key: "Asian Porn",
    label: "Asian Porn",
    color: "#fe93ff",
    facetColor: "#ec87c0",
    facetTextColor: "#ffffff",
  },
  {
    id: 9,
    key: "Misc",
    label: "Misc",
    color: "#9e9e9e",
    facetColor: "#aab2bd",
    facetTextColor: "#ffffff",
  },
];

export function getCategoryColor(category) {
  const c = EH_CATEGORIES.find(
    (x) => x.key.toLowerCase() === (category || "").toLowerCase(),
  );
  return c ? c.color : "#9e9e9e";
}

export const SUBSCRIPTION_PRESET_GROUPS = [
  {
    group: "subscription.preset_gallery",
    items: [
      { id: "latest", labelKey: "subscription.preset_latest" },
      { id: "watched", labelKey: "subscription.preset_watched" },
      { id: "popular", labelKey: "subscription.preset_popular" },
      { id: "favorites", labelKey: "subscription.preset_favorites" },
    ],
  },
  {
    group: "subscription.preset_toplist",
    items: [
      { id: "toplist:yesterday", labelKey: "subscription.preset_toplist_yesterday" },
      { id: "toplist:month", labelKey: "subscription.preset_toplist_month" },
      { id: "toplist:year", labelKey: "subscription.preset_toplist_year" },
      { id: "toplist:alltime", labelKey: "subscription.preset_toplist_alltime" },
    ],
  },
  {
    group: "subscription.preset_favcats",
    items: Array.from({ length: 10 }, (_, i) => ({
      id: `favorites:${i}`,
      labelKey: "subscription.preset_favcat_item",
      favcatIndex: i,
    })),
  },
];

export function isFavoritesPreset(preset) {
  return preset === "favorites" || /^favorites:[0-9]$/.test(preset || "");
}

export function getPresetLabel(presetId, t) {
  if (!presetId) return "";
  for (const group of SUBSCRIPTION_PRESET_GROUPS) {
    const found = group.items.find((item) => item.id === presetId);
    if (found) {
      if (found.favcatIndex !== undefined) {
        return t ? t(found.labelKey, { id: found.favcatIndex }) : `Favorites ${found.favcatIndex}`;
      }
      return t ? t(found.labelKey) : presetId;
    }
  }
  return presetId;
}

export function formatRelativeTime(isoStr, locale = "zh", now = Date.now()) {
  if (!isoStr) return "";
  const date = new Date(isoStr);
  if (isNaN(date.getTime())) return isoStr;

  const baseTime = typeof now === "number" ? now : new Date(now).getTime();
  const diffSec = Math.round((date.getTime() - baseTime) / 1000);
  const absSec = Math.abs(diffSec);

  const isZh = String(locale).startsWith("zh");
  if (absSec < 45) {
    return isZh ? "刚刚" : "just now";
  }

  try {
    const rtf = new Intl.RelativeTimeFormat(isZh ? "zh-CN" : "en", {
      numeric: "always",
      style: "short",
    });

    if (absSec < 3600) {
      const minutes = Math.round(diffSec / 60);
      return rtf.format(minutes, "minute");
    }
    if (absSec < 86400) {
      const hours = Math.round(diffSec / 3600);
      return rtf.format(hours, "hour");
    }
    if (absSec < 86400 * 30) {
      const days = Math.round(diffSec / 86400);
      return rtf.format(days, "day");
    }
    if (absSec < 86400 * 365) {
      const months = Math.round(diffSec / (86400 * 30));
      return rtf.format(months, "month");
    }
    const years = Math.round(diffSec / (86400 * 365));
    return rtf.format(years, "year");
  } catch {
    if (absSec < 3600) {
      const m = Math.max(1, Math.round(absSec / 60));
      return isZh ? `${m}分钟前` : `${m} min ago`;
    }
    if (absSec < 86400) {
      const h = Math.round(absSec / 3600);
      return isZh ? `${h}小时前` : `${h} hr ago`;
    }
    const d = Math.round(absSec / 86400);
    return isZh ? `${d}天前` : `${d} days ago`;
  }
}

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

export function createSubscriptionPayload(draft) {
  const source = draft.source;
  const query =
    source === "favorites"
      ? {
          max_pages: 10,
          favorite_categories: parseCategoryIDs(draft.favorite_categories),
        }
      : source === "preset"
        ? { preset: "latest", max_pages: 1 }
        : {
            keywords: String(draft.keywords || "").trim(),
            categories: parseCategoryIDs(
              draft.query_categories ?? draft.categories,
            ),
            sort: "newest",
            max_pages: 1,
          };
  return {
    name: String(draft.name || "").trim(),
    source,
    query,
    interval_seconds: Number(draft.interval ?? 3600),
    enabled: true,
    auto_download: false,
    download_rules: {},
  };
}

export function createSubscriptionUpdate(subscription, draft) {
  const query =
    subscription.source === "favorites"
      ? {
          ...subscription.query,
          max_pages: Number(draft.max_pages),
          favorite_categories: parseCategoryIDs(draft.favorite_categories),
        }
      : subscription.source === "preset"
        ? { preset: "latest", max_pages: 1 }
        : {
            ...subscription.query,
            keywords: String(draft.keywords || "").trim(),
            categories: parseCategoryIDs(draft.query_categories),
            sort: "newest",
            max_pages: 1,
          };
  const downloadRules = { quality: draft.quality };
  if (subscription.source === "favorites") {
    downloadRules.categories = parseCategoryIDs(draft.categories);
    downloadRules.excluded_categories = parseCategoryIDs(
      draft.excluded_categories,
    );
  }
  return {
    name: String(draft.name || "").trim(),
    query,
    interval_seconds: Number(draft.interval_seconds),
    enabled: Boolean(draft.enabled),
    auto_download: Boolean(draft.auto_download),
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
  const raw = String(value || "").trim();
  if (!raw) return [];
  return raw.split(",").map((part) => {
    const token = part.trim();
    if (!/^\d+$/.test(token))
      throw new Error("Category IDs must be integers from 0 to 9");
    const id = Number(token);
    if (id < 0 || id > 9)
      throw new Error("Category IDs must be integers from 0 to 9");
    return id;
  });
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

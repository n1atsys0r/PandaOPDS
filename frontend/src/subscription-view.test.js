import test from "node:test";
import assert from "node:assert/strict";
import {
  createSubscriptionItemsController,
  createSubscriptionPayload,
  createSubscriptionUpdate,
  buildCleanupPayload,
  buildReviewPayload,
  buildBatchDownloadPayload,
  buildBatchInspectPayload,
  cleanupSubscription,
  reviewDownloadJob,
  submitBatchDownload,
  submitBatchInspect,
  createSubscriptionPoller,
  documentReaderPaths,
  parseCategoryIDs,
  archiveProgressionURL,
  archiveProgressionKey,
  getCategoryColor,
  getPresetLabel,
  SUBSCRIPTION_PRESET_GROUPS,
  formatRelativeTime,
} from "./subscription-view.js";

test("subscription pagination only requests its persisted local-items endpoint", async () => {
  const calls = [];
  const api = async (path) => {
    calls.push(path);
    return { data: { items: [], total: 0 } };
  };
  const controller = createSubscriptionItemsController(api);
  await controller.load({
    subscriptionId: "sub 1",
    view: "unread",
    page: 2,
    perPage: 30,
  });
  assert.deepEqual(calls, [
    "/api/subscriptions/sub%201/items?view=unread&page=2&per_page=30",
  ]);
  assert.equal(
    calls.some((path) => /opds|search|favorites\/sync/.test(path)),
    false,
  );
});

test("a late request from the previous subscription is discarded", async () => {
  const pending = new Map();
  const api = (path) => new Promise((resolve) => pending.set(path, resolve));
  const controller = createSubscriptionItemsController(api);
  const oldPath = "/api/subscriptions/old/items?view=unread&page=1&per_page=30";
  const newPath = "/api/subscriptions/new/items?view=unread&page=1&per_page=30";
  const oldRequest = controller.load({
    subscriptionId: "old",
    view: "unread",
    page: 1,
    perPage: 30,
  });
  const newRequest = controller.load({
    subscriptionId: "new",
    view: "unread",
    page: 1,
    perPage: 30,
  });
  pending.get(newPath)({ data: { items: ["new"], total: 1 } });
  assert.deepEqual(await newRequest, { data: { items: ["new"], total: 1 } });
  pending.get(oldPath)({ data: { items: ["old"], total: 1 } });
  assert.equal(await oldRequest, null);
});

test("favorites are ordinary preset subscriptions with automatic downloads disabled", () => {
  const payload = createSubscriptionPayload({
    name: "Favorites",
    source: "preset",
    preset: "favorites",
    interval: 1800,
  });
  assert.equal(payload.name, "Favorites");
  assert.equal(payload.source, "preset");
  assert.equal(payload.query.preset, "favorites");
  assert.equal(payload.query.max_pages, 1);
  assert.equal(payload.auto_download, false);
  assert.equal(payload.enabled, true);
});

test("search and all preset subscriptions fetch one page", () => {
  for (const source of ["search", "preset"]) {
    const payload = createSubscriptionPayload({
      name: source,
      source,
      keywords: "l:chinese expunged",
    });
    assert.equal(payload.query.max_pages, 1);
    const update = createSubscriptionUpdate(
      { source, query: { max_pages: 10 } },
      { name: source, max_pages: 10, interval_seconds: 3600 },
    );
    assert.equal(update.query.max_pages, 1);
  }
});

test("favorites preset category selection uses shared post-fetch filters", () => {
  const payload = createSubscriptionPayload({
    name: "Favorites",
    source: "preset",
    preset: "favorites:2",
    query_categories: "1, 7",
  });
  assert.deepEqual(payload.query.categories, [1, 7]);
  assert.deepEqual(payload.download_rules, {});
  const update = createSubscriptionUpdate(
    { source: "preset", query: { preset: "favorites:2" } },
    {
      name: "Favorites",
      query_categories: "2",
      interval_seconds: 3600,
      auto_archive: "disabled",
    },
  );
  assert.deepEqual(update.query.categories, [2]);
  assert.equal(update.query.max_pages, 1);
  assert.deepEqual(update.download_rules, {});
});

test("cleanup, review, and batch actions require explicit acknowledged payloads", () => {
  assert.deepEqual(buildCleanupPayload("inspected"), {
    mode: "inspected",
    acknowledged: true,
  });
  assert.deepEqual(buildReviewPayload("allow_manual_retry"), {
    resolution: "allow_manual_retry",
    acknowledged: true,
  });
  assert.deepEqual(
    buildBatchDownloadPayload([{ gid: 4, token: "abc" }], "org"),
    {
      acknowledged: true,
      items: [{ gid: 4, token: "abc", quality: "org", force: false }],
    },
  );
  assert.throws(() => buildBatchDownloadPayload([], "res"));
  assert.throws(() =>
    buildBatchDownloadPayload(Array(201).fill({ gid: 1, token: "x" }), "res"),
  );
});

test("UI action adapters send acknowledged requests without triggering a fetch", async () => {
  const requests = [];
  const api = async (path, options) => {
    requests.push({ path, options });
    return { data: { items: [] } };
  };
  await cleanupSubscription(api, "sub/1", "hidden");
  await reviewDownloadJob(api, "job 1", "reconcile");
  await submitBatchDownload(api, [{ gid: 42, token: "abc" }], "res");
  await submitBatchInspect(api, [
    { gid: 101, token: "tok1", subscription_ids: ["sub1"] },
  ]);
  assert.deepEqual(
    requests.map(({ path }) => path),
    [
      "/api/subscriptions/sub%2F1/cleanup",
      "/api/download-jobs/job%201/review",
      "/api/download-jobs/batch",
      "/api/subscription-items/batch-inspect",
    ],
  );
  assert.deepEqual(buildBatchInspectPayload([]), { items: [] });
  assert.deepEqual(
    requests.map(({ options }) => JSON.parse(options.body)),
    [
      { mode: "hidden", acknowledged: true },
      { resolution: "reconcile", acknowledged: true },
      {
        acknowledged: true,
        items: [{ gid: 42, token: "abc", quality: "res", force: false }],
      },
      {
        items: [{ gid: 101, token: "tok1", subscription_ids: ["sub1"] }],
      },
    ],
  );
  assert.equal(
    requests.some(({ path }) => /\/run(?:\?|$)/.test(path)),
    false,
  );
});

test("subscription run polling refreshes cached items through local API after completion", async () => {
  let running = true;
  let refreshes = 0;
  const paths = [];
  const poller = createSubscriptionPoller({
    loadSubscriptions: async () => {
      running = false;
    },
    loadItems: async () => {
      refreshes++;
      paths.push("/api/subscriptions/sub/items?view=all&page=1&per_page=30");
    },
    getSubscriptions: () => [{ id: "sub", running }],
    setTimer: () => 1,
    clearTimer: () => {},
  });
  await poller.pollOnce();
  assert.equal(refreshes, 1);
  assert.deepEqual(paths, [
    "/api/subscriptions/sub/items?view=all&page=1&per_page=30",
  ]);
  poller.stop();
});

test("search subscriptions snapshot supported keyword and category filters", () => {
  const payload = createSubscriptionPayload({
    name: "Manga",
    source: "search",
    keywords: " test ",
    categories: "1, 8",
    sort: "popular",
  });
  assert.deepEqual(payload.query, {
    keywords: "test",
    categories: [1, 8],
    sort: "newest",
    max_pages: 1,
  });
  assert.throws(() => parseCategoryIDs("1,banana"));
  assert.throws(() => parseCategoryIDs("10"));
  assert.deepEqual(parseCategoryIDs(""), []);
});

test("subscription updates share automatic archive rules across sources", () => {
  const draft = {
    name: "Search",
    keywords: "cats",
    query_categories: "2, 4",
    sort: "popular",
    interval_seconds: 900,
    auto_download: true,
    quality: "org",
  };
  const search = createSubscriptionUpdate(
    { source: "search", query: {} },
    draft,
  );
  assert.deepEqual(search.query, {
    keywords: "cats",
    categories: [2, 4],
    sort: "newest",
    max_pages: 1,
  });
  assert.deepEqual(search.download_rules, { quality: "org" });

  const favorites = createSubscriptionUpdate(
    { source: "preset", query: { preset: "favorites", max_pages: 1 } },
    draft,
  );
  assert.deepEqual(favorites.download_rules, {
    quality: "org",
  });
});

test("local document reader uses stable document-ID stream and progression routes", () => {
  assert.deepEqual(documentReaderPaths("doc/a ?", 3), {
    stream: "/stream/documents/doc%2Fa%20%3F/page/3",
    progression: "/opds/progression/documents/doc%2Fa%20%3F",
  });
});

test("documents preserve an explicit EH progression target and keep manual links local", () => {
  assert.equal(
    archiveProgressionURL({
      id: "d-1",
      gid: 12,
      token: "abc",
      progression_url: "/opds/progression/12/abc",
    }),
    "/opds/progression/12/abc",
  );
  assert.equal(
    archiveProgressionURL({
      id: "d-2",
      gid: 12,
      token: "abc",
      progression_url: "/opds/progression/documents/d-2",
    }),
    "/opds/progression/documents/d-2",
  );
});

test("progression storage identity follows the frozen EH target after reassociation", () => {
  assert.equal(
    archiveProgressionKey({
      id: "d-3",
      gid: 99,
      token: "new-token",
      progression_url: "/opds/progression/12/frozen-token",
    }),
    "12:frozen-token",
  );
});

test("getCategoryColor resolves EH category hex colors accurately", () => {
  assert.equal(getCategoryColor("Doujinshi"), "#fc4e4e");
  assert.equal(getCategoryColor("Manga"), "#e58200");
  assert.equal(getCategoryColor("Unknown Category"), "#9e9e9e");
  assert.equal(getCategoryColor(null), "#9e9e9e");
});

test("preset subscriptions preserve chosen preset and allow label lookup", () => {
  const payload = createSubscriptionPayload({
    name: "Watched Feed",
    source: "preset",
    preset: "watched",
  });
  assert.equal(payload.source, "preset");
  assert.equal(payload.query.preset, "watched");
  assert.equal(payload.query.max_pages, 1);

  const update = createSubscriptionUpdate(
    { source: "preset", query: { preset: "latest" } },
    { preset: "toplist:month", interval_seconds: 3600 },
  );
  assert.equal(update.query.preset, "toplist:month");
  assert.equal(update.query.max_pages, 1);

  const mockT = (key, params) => {
    if (params?.id !== undefined) return `Fav ${params.id}`;
    return key;
  };
  assert.equal(getPresetLabel("watched", mockT), "subscription.preset_watched");
  assert.equal(getPresetLabel("favorites:3", mockT), "Fav 3");
  assert.equal(getPresetLabel("unknown", mockT), "unknown");
});

test("formatRelativeTime formats timestamps into human readable relative intervals", () => {
  const base = new Date("2026-11-22T12:00:00Z").getTime();

  // Empty or invalid
  assert.equal(formatRelativeTime("", "zh", base), "");
  assert.equal(formatRelativeTime("invalid-date", "zh", base), "invalid-date");

  // Just now (< 45s)
  assert.equal(formatRelativeTime("2026-11-22T11:59:45Z", "zh", base), "刚刚");
  assert.equal(formatRelativeTime("2026-11-22T11:59:45Z", "en", base), "just now");

  // Minutes
  const fiveMinAgo = "2026-11-22T11:55:00Z";
  assert.equal(formatRelativeTime(fiveMinAgo, "zh", base), "5分钟前");
  assert.match(formatRelativeTime(fiveMinAgo, "en", base), /5 min/);

  // Hours
  const twoHoursAgo = "2026-11-22T10:00:00Z";
  assert.equal(formatRelativeTime(twoHoursAgo, "zh", base), "2小时前");
  assert.match(formatRelativeTime(twoHoursAgo, "en", base), /2 hr/);

  // Days
  const threeDaysAgo = "2026-11-19T12:00:00Z";
  assert.equal(formatRelativeTime(threeDaysAgo, "zh", base), "3天前");
  assert.match(formatRelativeTime(threeDaysAgo, "en", base), /3 day/);
});

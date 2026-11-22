import test from "node:test";
import assert from "node:assert/strict";
import {
  captureGalleryMetadata,
  ensureGalleryMetadata,
  loadGalleryMetadata,
  mergeGalleryMetadata,
  restoreGalleryMetadata,
  formatLanguage,
} from "./gallery-metadata.js";

function metadata(title, pageCount = 99) {
  return {
    metadata: {
      title,
      numberOfPages: pageCount,
      "x:category": "Manga",
      "x:sizeBytes": 2048,
      subject: [{ name: "artist:demo", "x:style": "artist" }],
    },
    links: [{ rel: "alternate", type: "text/html", href: "https://e-hentai.org/g/42/token/" }],
  };
}

function localPublication() {
  return {
    gid: 42,
    token: "tok/en",
    title: "Local title",
    pageCount: 2,
    readingOrder: [{ href: "/local/page/1" }, { href: "/local/page/2" }],
    coverUrl: "/local/cover",
    detailHref: "/opds/v2.0/gallery/42/tok%2Fen/manifest",
    progressionUrl: "/opds/v2.0/progression/42/tok%2Fen",
    raw: { metadata: { numberOfPages: 2, identifier: "urn:eh:42" } },
  };
}

test("supplemental metadata preserves local reader identity and page data", async () => {
  const pub = localPublication();
  const api = async (href) => {
    assert.equal(href, "/opds/v2.0/gallery/42/tok%2Fen/publication");
    return { data: metadata("EH title") };
  };
  await ensureGalleryMetadata(api, pub);
  assert.equal(pub.title, "EH title");
  assert.equal(pub.category, "Manga");
  assert.equal(pub.pageCount, 2);
  assert.deepEqual(pub.readingOrder, [{ href: "/local/page/1" }, { href: "/local/page/2" }]);
  assert.equal(pub.coverUrl, "/local/cover");
  assert.equal(pub.detailHref, "/opds/v2.0/gallery/42/tok%2Fen/manifest");
  assert.equal(pub.progressionUrl, "/opds/v2.0/progression/42/tok%2Fen");
  assert.equal(pub.raw.metadata.numberOfPages, 2);
  assert.equal(pub.raw.metadata.identifier, "urn:eh:42");
  assert.equal(pub.sizeBytes, 2048);
  assert.equal(pub.fileSize, "");
  assert.equal(pub.galleryDetailsLoaded, true);
});

test("metadata-before-manifest race restores metadata but local pages/count win", () => {
  const pub = localPublication();
  mergeGalleryMetadata(pub, metadata("EH first", 88));
  const supplemental = captureGalleryMetadata(pub);
  Object.assign(pub, {
    title: "Manifest title",
    pageCount: 3,
    readingOrder: [{ href: "/local/1" }, { href: "/local/2" }, { href: "/local/3" }],
    detailHref: "/local/manifest",
    raw: { metadata: { numberOfPages: 3, identifier: "urn:local" } },
    galleryDetailsLoaded: false,
  });
  restoreGalleryMetadata(pub, supplemental);
  assert.equal(pub.title, "EH first");
  assert.equal(pub.pageCount, 3);
  assert.equal(pub.readingOrder.length, 3);
  assert.equal(pub.detailHref, "/local/manifest");
  assert.equal(pub.galleryDetailsLoaded, true);
  assert.equal(pub.raw.metadata.numberOfPages, 3);
  assert.equal(pub.raw.metadata.identifier, "urn:local");
});

test("manifest-before-metadata race keeps local count against upstream count", () => {
  const pub = localPublication();
  Object.assign(pub, { pageCount: 4, readingOrder: [{ href: "/local/1" }], raw: { metadata: { numberOfPages: 4 } } });
  mergeGalleryMetadata(pub, metadata("EH second", 77));
  assert.equal(pub.title, "EH second");
  assert.equal(pub.pageCount, 4);
  assert.deepEqual(pub.readingOrder, [{ href: "/local/1" }]);
  assert.equal(pub.raw.metadata.numberOfPages, 4);
});

test("simultaneous requests deduplicate and fulfilled response is reused", async () => {
  const gid = 730021;
  const token = "metadata-dedup-unique";
  let calls = 0;
  let release;
  const api = () => {
    calls += 1;
    return new Promise((resolve) => { release = resolve; });
  };
  const first = loadGalleryMetadata(api, gid, token);
  const second = loadGalleryMetadata(api, gid, token);
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(calls, 1);
  release({ data: metadata("cached") });
  assert.equal((await first).metadata.title, "cached");
  assert.equal((await second).metadata.title, "cached");
  const cached = await loadGalleryMetadata(() => { throw new Error("should use cache"); }, gid, token);
  assert.equal(cached.metadata.title, "cached");
});

test("pending metadata does not alter or gate local reading and cache expires", async () => {
  const pub = localPublication();
  pub.token = "metadata-pending-unique";
  pub.detailLoaded = true;
  let finish;
  const pending = ensureGalleryMetadata(() => new Promise((resolve) => { finish = resolve; }), pub);
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(pub.pageCount, 2);
  assert.equal(pub.readingOrder[0].href, "/local/page/1");
  assert.equal(pub.galleryDetailsLoaded, undefined);
  finish({ data: metadata("late metadata") });
  await pending;
  assert.equal(pub.title, "late metadata");
  assert.equal(pub.detailLoaded, true);

  let now = 1000;
  let calls = 0;
  const api = async () => {
    calls += 1;
    return { data: metadata(`ttl-${calls}`) };
  };
  await loadGalleryMetadata(api, 730023, "metadata-ttl-unique", () => now);
  now += 5 * 60 * 1000;
  await loadGalleryMetadata(api, 730023, "metadata-ttl-unique", () => now);
  assert.equal(calls, 2);
});

test("late metadata from an old representation cannot replace current reader state", async () => {
  const oldPub = localPublication();
  oldPub.token = "metadata-stale-repr-unique";
  let finish;
  const request = ensureGalleryMetadata(() => new Promise((resolve) => { finish = resolve; }), oldPub);
  await new Promise((resolve) => setImmediate(resolve));
  const current = {
    ...localPublication(),
    pageCount: 5,
    readingOrder: [{ href: "/new/local/page" }],
    coverUrl: "/new/local/cover",
    detailHref: "/new/local/manifest",
    raw: { metadata: { numberOfPages: 5, identifier: "urn:eh:42" }, images: ["new-image"] },
  };
  finish({ data: metadata("late supplemental title") });
  const updated = await request;
  restoreGalleryMetadata(current, captureGalleryMetadata(updated));
  assert.equal(current.title, "late supplemental title");
  assert.equal(current.pageCount, 5);
  assert.deepEqual(current.readingOrder, [{ href: "/new/local/page" }]);
  assert.equal(current.coverUrl, "/new/local/cover");
  assert.equal(current.detailHref, "/new/local/manifest");
  assert.deepEqual(current.raw.images, ["new-image"]);
  assert.equal(current.raw.metadata.numberOfPages, 5);
});

test("failed or malformed metadata is retried and local-only docs skip lookup", async () => {
  const gid = 730022;
  const token = "metadata-retry-unique";
  let calls = 0;
  await assert.rejects(loadGalleryMetadata(async () => {
    calls += 1;
    return { data: { metadata: null } };
  }, gid, token));
  const data = await loadGalleryMetadata(async () => {
    calls += 1;
    return { data: metadata("retry") };
  }, gid, token);
  assert.equal(data.metadata.title, "retry");
  assert.equal(calls, 2);
  let localCalls = 0;
  const local = { title: "Document", pageCount: 1 };
  assert.equal(await ensureGalleryMetadata(() => { localCalls += 1; }, local), local);
  assert.equal(localCalls, 0);
});

test("formatLanguage maps BCP 47 and EH tags to natural language display names", () => {
  assert.equal(formatLanguage("zh"), "中文");
  assert.equal(formatLanguage("zh-Hans"), "中文");
  assert.equal(formatLanguage("chinese"), "中文");
  assert.equal(formatLanguage("en"), "English");
  assert.equal(formatLanguage("english"), "English");
  assert.equal(formatLanguage("ja"), "日本語");
  assert.equal(formatLanguage("japanese"), "日本語");
  assert.equal(formatLanguage("ko"), "韩语");
  assert.equal(formatLanguage("korean"), "韩语");
  assert.equal(formatLanguage("es"), "Español");
  assert.equal(formatLanguage("fr"), "Français");
  assert.equal(formatLanguage("de"), "Deutsch");
  assert.equal(formatLanguage("ru"), "Русский");
  assert.equal(formatLanguage("pt-BR"), "Português");

  // Filter out non-language marker tags
  assert.equal(formatLanguage("translated"), "");
  assert.equal(formatLanguage("rewrite"), "");

  // Fallback to tags or subjects when language string is empty
  assert.equal(
    formatLanguage("", { tags: [{ namespace: "language", key: "chinese" }] }),
    "中文",
  );
  assert.equal(
    formatLanguage("", { subjects: [{ name: "language:japanese" }] }),
    "日本語",
  );
});

const inflight = new Map();
const fulfilled = new Map();
const CACHE_TTL_MS = 5 * 60 * 1000;
const CACHE_LIMIT = 256;

function galleryKey(gid, token) {
  return `${Number(gid)}:${String(token).toLowerCase()}`;
}

export function loadGalleryMetadata(api, gid, token, now = Date.now) {
  if (!(Number(gid) > 0) || !token) return Promise.resolve(null);
  const key = galleryKey(gid, token);
  const cached = fulfilled.get(key);
  if (cached && cached.expiresAt > now()) {
    fulfilled.delete(key);
    fulfilled.set(key, cached);
    return Promise.resolve(cached.data);
  }
  if (cached) fulfilled.delete(key);
  if (inflight.has(key)) return inflight.get(key);

  const href = `/opds/v2.0/gallery/${encodeURIComponent(Number(gid))}/${encodeURIComponent(token)}/publication`;
  const task = Promise.resolve()
    .then(() => api(href, {
      headers: {
        Accept: "application/opds+json",
        "Cache-Control": "no-cache",
      },
    }))
    .then((response) => {
      const data = response?.data;
      if (!data || !data.metadata || typeof data.metadata !== "object") {
        throw new Error("Gallery metadata response is incomplete");
      }
      fulfilled.set(key, { data, expiresAt: now() + CACHE_TTL_MS });
      while (fulfilled.size > CACHE_LIMIT) {
        fulfilled.delete(fulfilled.keys().next().value);
      }
      return data;
    })
    .finally(() => inflight.delete(key));
  inflight.set(key, task);
  return task;
}

function mergeSubjects(existing, incoming) {
  const subjects = [...(Array.isArray(existing) ? existing : [])];
  for (const subject of Array.isArray(incoming) ? incoming : []) {
    if (!subject?.name) continue;
    const index = subjects.findIndex((item) => item?.name === subject.name);
    if (index < 0) subjects.push(subject);
    else {
      const style = subject["x:style"] || subjects[index]["x:style"];
      subjects[index] = {
        ...subjects[index],
        ...subject,
        ...(style ? { "x:style": style } : {}),
      };
    }
  }
  return subjects;
}

function languageValue(language, fallback = "") {
  if (Array.isArray(language)) return language.join(", ") || fallback;
  if (typeof language === "string") return language || fallback;
  return fallback;
}

export function mergeGalleryMetadata(pub, data) {
  if (!pub || !data?.metadata) return pub;
  const metadata = data.metadata;
  const original = pub.raw || {};
  const originalMetadata = original.metadata || {};
  const subjects = mergeSubjects(pub.subjects, metadata.subject);
  const authors = Array.isArray(metadata.authors)
    ? metadata.authors.map((author) => author?.name).filter(Boolean).join(", ")
    : Array.isArray(metadata.author)
      ? metadata.author.map((author) => author?.name).filter(Boolean).join(", ")
      : typeof metadata.author === "string"
        ? metadata.author
        : pub.authors;
  const ratingValue = typeof metadata.rating === "object"
    ? metadata.rating?.value
    : metadata["x:rating"] ?? metadata.rating;
  const sizeBytes = Number(metadata["x:sizeBytes"] || pub.sizeBytes || 0);
  const upstream = (data.links || []).find(
    (link) => link.rel === "alternate" && link.type === "text/html",
  )?.href;
  const mergedMetadata = { ...originalMetadata, ...metadata };
  // Keep reading identity/page data supplied by the local manifest intact.
  if (pub.pageCount > 0) mergedMetadata.numberOfPages = pub.pageCount;
  if (originalMetadata.identifier) mergedMetadata.identifier = originalMetadata.identifier;

  Object.assign(pub, {
    title: metadata.title || pub.title,
    titleJpn: metadata["x:titleJpn"] || pub.titleJpn || "",
    authors: authors || pub.authors || "",
    category: metadata["x:category"] || pub.category || "",
    subjects,
    language: languageValue(metadata.language, pub.language || ""),
    rating: Number(ratingValue || pub.rating || 0),
    uploader: metadata["x:uploader"] || pub.uploader || "",
    publishTime: metadata.published || pub.publishTime || "",
    sizeBytes,
    fileSize: metadata["x:fileSize"] || pub.fileSize || "",
    reviews: Array.isArray(metadata["x:reviews"])
      ? metadata["x:reviews"]
      : pub.reviews || [],
    upstreamUrl: upstream || pub.upstreamUrl || "",
    belongsTo: metadata.belongsTo || pub.belongsTo,
    raw: { ...original, metadata: mergedMetadata },
    galleryDetailsLoaded: true,
  });
  return pub;
}

const SUPPLEMENTAL_FIELDS = [
  "title", "titleJpn", "authors", "category", "rating", "sizeBytes",
  "fileSize", "uploader", "publishTime", "language", "subjects", "reviews",
  "upstreamUrl", "belongsTo", "favcat", "raw", "galleryDetailsLoaded",
];

export function captureGalleryMetadata(pub) {
  if (!pub?.galleryDetailsLoaded) return null;
  return Object.fromEntries(SUPPLEMENTAL_FIELDS.map((key) => [key, pub[key]]));
}

export function restoreGalleryMetadata(pub, snapshot) {
  if (!pub || !snapshot) return pub;
  const currentRaw = pub.raw || {};
  Object.assign(pub, snapshot);
  const metadataRaw = pub.raw || {};
  pub.raw = {
    ...metadataRaw,
    ...currentRaw,
    metadata: {
      ...(metadataRaw.metadata || {}),
      ...(currentRaw.metadata || {}),
    },
  };
  // Local reading data always owns the page count, regardless of which request
  // completed first or what page count the supplemental response contained.
  if (Number(pub.pageCount) > 0) {
    const raw = pub.raw || {};
    pub.raw = {
      ...raw,
      metadata: { ...(raw.metadata || {}), numberOfPages: Number(pub.pageCount) },
    };
  }
  return pub;
}

export function ensureGalleryMetadata(api, pub) {
  if (!pub?.gid || !pub?.token || pub.galleryDetailsLoaded) {
    return Promise.resolve(pub || null);
  }
  return loadGalleryMetadata(api, pub.gid, pub.token).then((data) =>
    mergeGalleryMetadata(pub, data),
  );
}

export const DEVICE_ID_KEY = "pandaopds_device_id";
export const READER_PAGE_KEY_PREFIX = "pandaopds_reader_page:";

/**
 * Returns a stable, valid URI device identifier.
 * Conforms to OPDS Progression 1.0 device URI requirement.
 */
export function getOrCreateDeviceId() {
  if (typeof window === "undefined" || !window.localStorage) {
    return "urn:uuid:00000000-0000-0000-0000-000000000000";
  }
  try {
    let id = localStorage.getItem(DEVICE_ID_KEY);
    if (!id) {
      id =
        typeof crypto !== "undefined" && typeof crypto.randomUUID === "function"
          ? `urn:uuid:${crypto.randomUUID()}`
          : `urn:uuid:${Math.random().toString(36).slice(2, 10)}-${Date.now()}`;
      localStorage.setItem(DEVICE_ID_KEY, id);
    }
    return id;
  } catch {
    return "urn:uuid:00000000-0000-0000-0000-000000000000";
  }
}

/**
 * Converts 0-based pageIndex and total pageCount to OPDS Progression float [0.0, 1.0].
 */
export function pageToProgression(pageIndex, pageCount) {
  if (!Number.isFinite(pageCount) || pageCount <= 1) return 1;
  const clamped = Math.min(Math.max(0, pageIndex), pageCount - 1);
  return Number((clamped / (pageCount - 1)).toFixed(4));
}

/**
 * Converts OPDS Progression float [0.0, 1.0] and total pageCount back to 0-based pageIndex.
 */
export function progressionToPage(progression, pageCount) {
  if (!Number.isFinite(pageCount) || pageCount <= 1) return 0;
  if (!Number.isFinite(progression) || progression <= 0) return 0;
  return Math.min(
    Math.max(0, Math.round(progression * (pageCount - 1))),
    pageCount - 1,
  );
}

/**
 * Parses raw localStorage progress string (supports legacy plain number and new JSON format).
 */
export function parseSavedProgress(raw) {
  if (!raw) return null;
  try {
    const parsed = JSON.parse(raw);
    if (parsed && typeof parsed === "object" && Number.isFinite(parsed.page)) {
      return {
        page: Math.floor(parsed.page),
        modified: parsed.modified ? new Date(parsed.modified).getTime() : 0,
      };
    }
  } catch {}
  const num = Number(raw);
  if (Number.isFinite(num) && num >= 0) {
    return { page: Math.floor(num), modified: 0 };
  }
  return null;
}

/**
 * Builds OPDS Progression 1.0 PUT document payload.
 */
export function buildProgressionPayload(
  pageIndex,
  pageCount,
  deviceId = getOrCreateDeviceId(),
) {
  const clamped = Math.min(
    Math.max(0, pageIndex),
    Math.max(0, (pageCount || 1) - 1),
  );
  const progression = pageToProgression(clamped, pageCount);
  return {
    modified: new Date().toISOString(),
    device: {
      id: deviceId,
      name: "PandaOPDS WebUI",
    },
    progression,
    title: `Page ${clamped + 1} / ${pageCount}`,
    references: [`page-${clamped + 1}`],
  };
}

/**
 * Resolves the latest reading progress between local localStorage record and remote server document.
 * Follows Last-Write-Wins (LWW) based on modified timestamp.
 */
export function resolveLatestProgress(localRecord, remoteDoc, pageCount) {
  const localMod = localRecord?.modified || 0;
  const remoteMod = remoteDoc?.modified
    ? new Date(remoteDoc.modified).getTime()
    : 0;

  let pageIndex = 0;
  let hasProgress = false;
  let source = "none";
  let modified = 0;

  if (
    remoteDoc &&
    typeof remoteDoc.progression === "number" &&
    remoteMod >= localMod &&
    remoteMod > 0
  ) {
    pageIndex = progressionToPage(remoteDoc.progression, pageCount);
    hasProgress = true;
    source = "remote";
    modified = remoteMod;
  } else if (localRecord && Number.isFinite(localRecord.page)) {
    pageIndex = Math.min(
      Math.max(0, localRecord.page),
      Math.max(0, pageCount - 1),
    );
    hasProgress = true;
    source = "local";
    modified = localMod;
  } else if (remoteDoc && typeof remoteDoc.progression === "number") {
    pageIndex = progressionToPage(remoteDoc.progression, pageCount);
    hasProgress = true;
    source = "remote";
    modified = remoteMod;
  }

  if (!hasProgress) return null;

  const isCompleted =
    pageCount > 1 ? pageIndex >= pageCount - 1 : pageIndex >= 0;
  const percent =
    pageCount > 1
      ? Math.min(100, Math.max(0, Math.round((pageIndex / (pageCount - 1)) * 100)))
      : 100;

  return {
    pageIndex,
    pageCount,
    isCompleted,
    percent,
    source,
    modified,
  };
}

/**
 * Calculates the grace threshold (0-based page index) before which reading progress is not recorded.
 * - < 4 pages: 0 (index 0)
 * - 4..10 pages: Math.floor(pageCount / 2) - 1
 * - >= 11 pages: 5 (6th page, 0-based index 5)
 */
export function getReadingThreshold(pageCount) {
  if (!Number.isFinite(pageCount) || pageCount < 4) return 0;
  if (pageCount <= 10) return Math.floor(pageCount / 2) - 1;
  return 5;
}

/**
 * Determines whether reading progress should be written based on incognito state, user navigation, and page threshold.
 */
export function shouldRecordProgress({
  isIncognito,
  hasNavigated,
  pageIndex,
  pageCount,
}) {
  if (isIncognito) return false;
  if (!hasNavigated) return false;
  if (!Number.isFinite(pageCount) || pageCount <= 0) return false;

  const threshold = getReadingThreshold(pageCount);
  if (pageIndex < threshold) return false;

  return true;
}


import test from "node:test";
import assert from "node:assert/strict";
import {
  pageToProgression,
  progressionToPage,
  parseSavedProgress,
  buildProgressionPayload,
  resolveLatestProgress,
  getReadingThreshold,
  shouldRecordProgress,
} from "./progression-helper.js";

test("pageToProgression converts page indices accurately", () => {
  assert.equal(pageToProgression(0, 20), 0);
  assert.equal(pageToProgression(19, 20), 1);
  assert.equal(pageToProgression(9, 19), 0.5);
  assert.equal(pageToProgression(0, 1), 1);
  assert.equal(pageToProgression(5, 0), 1);
});

test("progressionToPage converts progression float back to pageIndex", () => {
  assert.equal(progressionToPage(0, 20), 0);
  assert.equal(progressionToPage(1, 20), 19);
  assert.equal(progressionToPage(0.5, 19), 9);
  assert.equal(progressionToPage(0.8, 100), 79);
  assert.equal(progressionToPage(0.5, 1), 0);
});

test("parseSavedProgress handles legacy numbers and modern JSON", () => {
  assert.equal(parseSavedProgress(""), null);
  assert.equal(parseSavedProgress(null), null);

  // Legacy plain number
  assert.deepEqual(parseSavedProgress("15"), { page: 15, modified: 0 });

  // Modern JSON with ISO timestamp
  const jsonStr = JSON.stringify({
    page: 25,
    modified: "2026-11-15T00:00:00.000Z",
  });
  const parsed = parseSavedProgress(jsonStr);
  assert.equal(parsed.page, 25);
  assert.equal(parsed.modified, new Date("2026-11-15T00:00:00.000Z").getTime());

  // Malformed JSON falls back gracefully
  assert.equal(parseSavedProgress("{bad json}"), null);
});

test("buildProgressionPayload constructs valid OPDS Progression payload", () => {
  const payload = buildProgressionPayload(4, 20, "urn:uuid:test-device-id");
  assert.equal(payload.device.id, "urn:uuid:test-device-id");
  assert.equal(payload.device.name, "PandaOPDS WebUI");
  assert.equal(payload.progression, pageToProgression(4, 20));
  assert.equal(payload.title, "Page 5 / 20");
  assert.deepEqual(payload.references, ["page-5"]);
  assert.ok(payload.modified);
});

test("resolveLatestProgress implements Last-Write-Wins (LWW)", () => {
  const now = Date.now();
  const past = now - 60000;

  // Remote is newer -> use remote
  const localOlder = { page: 5, modified: past };
  const remoteNewer = {
    progression: pageToProgression(15, 20),
    modified: new Date(now).toISOString(),
  };
  const res1 = resolveLatestProgress(localOlder, remoteNewer, 20);
  assert.equal(res1.pageIndex, 15);
  assert.equal(res1.source, "remote");
  assert.equal(res1.isCompleted, false);

  // Local is newer -> use local
  const localNewer = { page: 10, modified: now };
  const remoteOlder = {
    progression: pageToProgression(3, 20),
    modified: new Date(past).toISOString(),
  };
  const res2 = resolveLatestProgress(localNewer, remoteOlder, 20);
  assert.equal(res2.pageIndex, 10);
  assert.equal(res2.source, "local");

  // Completion detection
  const completeLocal = { page: 19, modified: now };
  const res3 = resolveLatestProgress(completeLocal, null, 20);
  assert.equal(res3.pageIndex, 19);
  assert.equal(res3.isCompleted, true);
  assert.equal(res3.percent, 100);
});

test("getReadingThreshold calculates grace threshold accurately", () => {
  // < 4 pages: 0
  assert.equal(getReadingThreshold(1), 0);
  assert.equal(getReadingThreshold(2), 0);
  assert.equal(getReadingThreshold(3), 0);

  // 4..10 pages: floor(pageCount / 2) - 1
  assert.equal(getReadingThreshold(4), 1);
  assert.equal(getReadingThreshold(5), 1);
  assert.equal(getReadingThreshold(6), 2);
  assert.equal(getReadingThreshold(7), 2);
  assert.equal(getReadingThreshold(8), 3);
  assert.equal(getReadingThreshold(9), 3);
  assert.equal(getReadingThreshold(10), 4);

  // >= 11 pages: 5 (6th page, 0-based index 5)
  assert.equal(getReadingThreshold(11), 5);
  assert.equal(getReadingThreshold(20), 5);
  assert.equal(getReadingThreshold(100), 5);
});

test("shouldRecordProgress respects incognito, navigation, and threshold", () => {
  // Incognito never records
  assert.equal(
    shouldRecordProgress({
      isIncognito: true,
      hasNavigated: true,
      pageIndex: 10,
      pageCount: 20,
    }),
    false,
  );

  // No active navigation never records
  assert.equal(
    shouldRecordProgress({
      isIncognito: false,
      hasNavigated: false,
      pageIndex: 10,
      pageCount: 20,
    }),
    false,
  );

  // 20 pages (threshold = 5):
  // pageIndex 4 (< 5) is within grace period -> not recorded
  assert.equal(
    shouldRecordProgress({
      isIncognito: false,
      hasNavigated: true,
      pageIndex: 4,
      pageCount: 20,
    }),
    false,
  );

  // pageIndex 5 (>= 5) meets threshold -> recorded!
  assert.equal(
    shouldRecordProgress({
      isIncognito: false,
      hasNavigated: true,
      pageIndex: 5,
      pageCount: 20,
    }),
    true,
  );

  // pageIndex 15 (>= 5) meets threshold -> recorded!
  assert.equal(
    shouldRecordProgress({
      isIncognito: false,
      hasNavigated: true,
      pageIndex: 15,
      pageCount: 20,
    }),
    true,
  );

  // 2 pages (threshold = 0):
  assert.equal(
    shouldRecordProgress({
      isIncognito: false,
      hasNavigated: true,
      pageIndex: 1,
      pageCount: 2,
    }),
    true,
  );
});

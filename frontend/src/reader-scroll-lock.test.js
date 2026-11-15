import test from "node:test";
import assert from "node:assert/strict";
import { createScrollLocker } from "./reader-scroll-lock.js";

function createMockDoc(initialOverflow = "") {
  return {
    body: {
      style: {
        overflow: initialOverflow,
      },
    },
  };
}

test("scroll locker locks and restores normal overflow", () => {
  const mockDoc = createMockDoc("");
  const locker = createScrollLocker(mockDoc);

  locker.lock();
  assert.equal(mockDoc.body.style.overflow, "hidden");
  assert.equal(locker.getStatus().isLocked, true);

  locker.unlock();
  assert.equal(mockDoc.body.style.overflow, "");
  assert.equal(locker.getStatus().isLocked, false);
});

test("scroll locker sanitizes 'hidden' if already locked by preceding modal", () => {
  // Scenario: Dialog is open, setting body overflow to "hidden"
  const mockDoc = createMockDoc("hidden");
  const locker = createScrollLocker(mockDoc);

  locker.lock();
  assert.equal(mockDoc.body.style.overflow, "hidden");
  // savedOverflow must NOT be "hidden"
  assert.equal(locker.getStatus().savedOverflow, "");

  // When reader closes, body overflow must be cleared to allow page scrolling
  locker.unlock();
  assert.equal(mockDoc.body.style.overflow, "");
  assert.equal(locker.getStatus().isLocked, false);
});

test("scroll locker prevents re-entrant lock from poisoning saved overflow", () => {
  const mockDoc = createMockDoc("");
  const locker = createScrollLocker(mockDoc);

  locker.lock();
  assert.equal(mockDoc.body.style.overflow, "hidden");

  // Second lock call while already locked (e.g. from watch dependency changes)
  locker.lock();
  assert.equal(mockDoc.body.style.overflow, "hidden");
  assert.equal(locker.getStatus().savedOverflow, "");

  locker.unlock();
  assert.equal(mockDoc.body.style.overflow, "");
  assert.equal(locker.getStatus().isLocked, false);
});

test("scroll locker sync cleanly handles toggle states", () => {
  const mockDoc = createMockDoc("");
  const locker = createScrollLocker(mockDoc);

  locker.sync(true);
  assert.equal(mockDoc.body.style.overflow, "hidden");

  locker.sync(true); // repeated sync true
  assert.equal(mockDoc.body.style.overflow, "hidden");

  locker.sync(false);
  assert.equal(mockDoc.body.style.overflow, "");
});

test("scroll locker preserves legitimate non-hidden inline overflow styles", () => {
  const mockDoc = createMockDoc("auto");
  const locker = createScrollLocker(mockDoc);

  locker.lock();
  assert.equal(mockDoc.body.style.overflow, "hidden");

  locker.unlock();
  assert.equal(mockDoc.body.style.overflow, "auto");
});

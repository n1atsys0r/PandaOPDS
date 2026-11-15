import test from "node:test";
import assert from "node:assert/strict";
import { directionalPrefetchPages } from "./reader-prefetch.js";

test("prefetch window follows the latest direction and favors nearby pages", () => {
  const spreads = [[0], [1, 2], [3, 4], [5, 6], [7, 8], [9]];
  assert.deepEqual(
    directionalPrefetchPages(spreads, 2, "forward"),
    [5, 6, 7, 8, 1, 2],
  );
  assert.deepEqual(
    directionalPrefetchPages(spreads, 3, "backward"),
    [3, 4, 1, 2, 7, 8],
  );
});

test("prefetch window stays within chapter boundaries and does not include visible pages", () => {
  const spreads = [[0], [1, 2], [3]];
  assert.deepEqual(directionalPrefetchPages(spreads, 0, "backward"), [1, 2]);
  assert.deepEqual(directionalPrefetchPages(spreads, 2, "forward"), [1, 2]);
  assert.deepEqual(directionalPrefetchPages(spreads, 1, "forward"), [3, 0]);
});

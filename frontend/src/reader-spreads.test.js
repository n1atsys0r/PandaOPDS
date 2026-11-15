import test from "node:test";
import assert from "node:assert/strict";
import { buildReaderSpreads, spreadForPage } from "./reader-spreads.js";

test("double spreads pin the cover and keep page anchors in their containing pair", () => {
  const spreads = buildReaderSpreads(6, "double");
  assert.deepEqual(spreads, [[0], [1, 2], [3, 4], [5, null]]);
  assert.equal(spreadForPage(spreads, 2), 1);
  assert.equal(spreadForPage(spreads, 4), 2);
  assert.equal(spreadForPage(buildReaderSpreads(4, "single"), 3), 3);
});

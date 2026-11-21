import test from "node:test";
import assert from "node:assert/strict";
import {
  documentReadErrorMessage,
  prepareDocumentForReading,
} from "./document-reader.js";

test("reading a candidate indexes it and uses the refreshed progression target", async () => {
  const calls = [];
  const api = async (path) => {
    calls.push(path);
    if (path.endsWith("/manifest")) {
      return { data: { metadata: { numberOfPages: 3 } } };
    }
    return {
      data: {
        id: "doc-1",
        page_count: 3,
        progression_url: "/opds/progression/123/abc",
      },
    };
  };
  const prepared = await prepareDocumentForReading(
    {
      id: "doc-1",
      page_count: 0,
      progression_url: "/opds/progression/documents/doc-1",
    },
    api,
  );
  assert.deepEqual(calls, [
    "/opds/v2.0/documents/doc-1/manifest",
    "/api/documents/doc-1",
  ]);
  assert.equal(prepared.page_count, 3);
  assert.equal(prepared.progression_url, "/opds/progression/123/abc");
  assert.equal(prepared.document_id, "doc-1");
});

test("invalid candidate does not emit a readable document", async () => {
  await assert.rejects(
    prepareDocumentForReading({ id: "bad" }, async () => ({
      data: { metadata: { numberOfPages: 0 } },
    })),
    /no readable pages/,
  );
});

test("read errors show reason and exact relative-file signatures", () => {
  const error = Object.assign(new Error("generic"), {
    data: {
      reason: "stale_signature",
      path: "1726517.zip",
      stored_signature: { size: 100, mtime_ns: "9007199254740993" },
      current_signature: { size: 101, mtime_ns: "9007199254740995" },
    },
  });
  const message = documentReadErrorMessage(error, (key) => key);
  assert.match(message, /documents\.read_error_stale_signature/);
  assert.match(message, /1726517\.zip/);
  assert.match(message, /9007199254740993/);
  assert.match(message, /9007199254740995/);
});

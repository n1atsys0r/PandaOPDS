import test from "node:test";
import assert from "node:assert/strict";
import { prepareDocumentForReading } from "./document-reader.js";

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
    { id: "doc-1", page_count: 0, progression_url: "/opds/progression/documents/doc-1" },
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
    prepareDocumentForReading({ id: "bad" }, async () => ({ data: { metadata: { numberOfPages: 0 } } })),
    /no readable pages/,
  );
});

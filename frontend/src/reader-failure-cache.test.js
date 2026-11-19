import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

function readComponent(relPath) {
  return fs.readFileSync(path.resolve(__dirname, relPath), "utf-8");
}

test("ArchiveReader evicts bad blob URLs on error and supports bypassCache on retry", () => {
  const code = readComponent("components/ArchiveReader.vue");

  // Ensure evictResolvedUrl revokes blob and removes from resolvedUrls
  assert.match(code, /function evictResolvedUrl\(/);
  assert.match(code, /resolvedUrls\.delete\(idx\)/);
  assert.match(code, /URL\.revokeObjectURL/);

  // Ensure webtoon error handler evicts broken URL
  assert.match(
    code,
    /@error="[\s\S]*?evictResolvedUrl\(idx\)[\s\S]*?webtoonStates\[idx\]/,
  );

  // Ensure retry calls with bypassCache: true
  assert.match(
    code,
    /loadWebtoonPage\(idx,\s*PRIORITY_CURRENT,\s*\{[\s\S]*?bypassCache:\s*true[\s\S]*?\}\)/,
  );

  // Ensure ReaderPager receives evict-page prop
  assert.match(code, /:evict-page="evictResolvedUrl"/);
});

test("ReaderPager handles load failure cleanly without masking error state", () => {
  const code = readComponent("components/ReaderPager.vue");

  // Ensure props declare evictPage
  assert.match(code, /evictPage:\s*Function/);

  // In loadCurrent catch block, removePageUrl is called so v-if="pageUrls[idx]" is false
  // and v-else-if="pageErrors[idx]" is rendered
  assert.match(
    code,
    /removePageUrl\(displayIndex\.value\)[\s\S]*?preloadedUrls\.delete\(displayIndex\.value\)[\s\S]*?props\.evictPage\?\.[\s\S]*?pageErrors\.value/,
  );

  // In retrySpreadPage, evict and bypassCache are used
  assert.match(
    code,
    /async function retrySpreadPage\(index\)[\s\S]*?props\.evictPage\?\.[\s\S]*?props\.resolvePage\(index, true, \{\s*bypassCache:\s*true\s*\}\)/,
  );

  // On <img> @error event, clean up and transition to error
  assert.match(
    code,
    /@error="[\s\S]*?removePageUrl\(idx\)[\s\S]*?preloadedUrls\.delete\(idx\)[\s\S]*?props\.evictPage\?\.[\s\S]*?pageErrors/,
  );
});

test("state.js fetchMediaBlobUrl appends cache-busting headers and query param when bypassCache is requested", () => {
  const code = readComponent("state.js");

  assert.match(code, /bypassCache\s*=\s*false/);
  assert.match(code, /headers\["Cache-Control"\]\s*=\s*"no-cache"/);
  assert.match(code, /headers\["Pragma"\]\s*=\s*"no-cache"/);
  assert.match(
    code,
    /requestUrl\s*=\s*`\$\{requestUrl\}\$\{sep\}_t=\$\{Date\.now\(\)\}`/,
  );
  assert.match(code, /fetchOpts\.cache\s*=\s*"reload"/);
});

import { test } from "node:test";
import assert from "node:assert/strict";
import {
  FAVCAT_SOLID_COLORS,
  FAVCAT_PALETTE,
  getFavcatSolidColor,
  getFavcatBadgeStyle,
  resolveFavcatId,
} from "./favcat-colors.js";

test("FAVCAT_SOLID_COLORS adapts favcat=0 for dark theme and keeps 10 colors", () => {
  assert.equal(FAVCAT_SOLID_COLORS.length, 10);
  assert.equal(FAVCAT_SOLID_COLORS[0], "#6b7280");
  assert.notEqual(FAVCAT_SOLID_COLORS[0], "#000000");
  assert.equal(FAVCAT_SOLID_COLORS[1], "#fc4e4f");
  assert.equal(FAVCAT_SOLID_COLORS[9], "#fe93ff");
});

test("getFavcatSolidColor returns correct color and falls back safely", () => {
  assert.equal(getFavcatSolidColor(0), "#6b7280");
  assert.equal(getFavcatSolidColor("0"), "#6b7280");
  assert.equal(getFavcatSolidColor(3), "#dde500");
  assert.equal(getFavcatSolidColor("7"), "#5050d7");
  assert.equal(getFavcatSolidColor(null), "#6b7280");
  assert.equal(getFavcatSolidColor(99), "#6b7280");
  assert.equal(getFavcatSolidColor("invalid"), "#6b7280");
});

test("FAVCAT_PALETTE and getFavcatBadgeStyle provide visible contrast for favcat=0", () => {
  assert.equal(FAVCAT_PALETTE.length, 10);
  const style0 = getFavcatBadgeStyle(0);
  assert.equal(style0.text, "#e5e5e5");
  assert.notEqual(style0.text, "#000000");
  assert.ok(style0.bg.includes("158, 158, 158"));
  assert.ok(style0.border.includes("158, 158, 158"));

  const style1 = getFavcatBadgeStyle(1);
  assert.equal(style1.text, "#fc4e4f");
  assert.ok(style1.bg.includes("252, 78, 79"));

  const fallback = getFavcatBadgeStyle(-1);
  assert.deepEqual(fallback, FAVCAT_PALETTE[0]);
});

test("resolveFavcatId resolves favcat=0 properly across multiple sources", () => {
  // 1. Direct number favcat = 0
  assert.equal(resolveFavcatId({ favcat: 0 }), 0);

  // 2. Direct string favcat = "0"
  assert.equal(resolveFavcatId({ favcat: "0" }), 0);

  // 3. Archive item favcat = 0
  assert.equal(resolveFavcatId({ archiveItem: { favcat: 0 } }), 0);

  // 4. Metadata x:favcat = 0 or "0"
  assert.equal(resolveFavcatId({ metadata: { "x:favcat": 0 } }), 0);
  assert.equal(resolveFavcatId({ raw: { metadata: { "x:favcat": "0" } } }), 0);

  // 5. BelongsTo collection link with favcat=0
  const pubWithBelongs = {
    belongsTo: {
      collection: {
        links: [{ href: "/opds/v2.0/gallery?favcat=0&query=favorites" }],
      },
    },
  };
  assert.equal(resolveFavcatId(pubWithBelongs), 0);

  // 6. Favorite override takes precedence
  const overrideActive0 = { isFavorited: true, favcat: 0 };
  assert.equal(resolveFavcatId({ favcat: 3 }, overrideActive0), 0);

  const overrideActive2 = { isFavorited: true, favcat: 2 };
  assert.equal(resolveFavcatId({ favcat: 0 }, overrideActive2), 2);

  const overrideRemoved = { isFavorited: false, favcat: null };
  assert.equal(resolveFavcatId({ favcat: 0 }, overrideRemoved), null);

  // 7. Generic favorited flag falls back to folder 0
  assert.equal(resolveFavcatId({ favorited: true }), 0);
  assert.equal(resolveFavcatId({ is_favorited: true }), 0);

  // 8. Null / unfavorited
  assert.equal(resolveFavcatId(null), null);
  assert.equal(resolveFavcatId({}), null);
});

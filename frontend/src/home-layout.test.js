import test from "node:test";
import assert from "node:assert/strict";
import {
  canUseFacets,
  chooseInitialHomeMode,
  serializeHomeLayout,
  splitHomeSections,
  replacesAlternateMode,
} from "./home-layout.js";

test("navigation-only and empty layouts serialize in publication mode", () => {
  const empty = { navigation: [], publication: null, facets: [], groups: [] };
  assert.deepEqual(serializeHomeLayout(empty, "publications"), []);
  const navOnly = { ...empty, navigation: [{ title: "Recent", type: "preset", query: "latest" }] };
  assert.deepEqual(serializeHomeLayout(navOnly, "publications"), [
    { title: "Recent", type: "preset", query: "latest", kind: "navigation", group: false, count: null },
  ]);
});

test("saved alternate mode is called out before replacement", () => {
  const savedGroups = [{ kind: "publication", group: true, query: "popular" }];
  const savedPublication = [{ kind: "publication", group: false, query: "latest" }];
  assert.equal(replacesAlternateMode(savedGroups, "publications"), true);
  assert.equal(replacesAlternateMode(savedPublication, "groups"), true);
  assert.equal(replacesAlternateMode(savedGroups, "groups"), false);
});

test("switching layout modes retains both drafts", () => {
  const drafts = {
    navigation: [{ kind: "navigation", title: "Top", type: "preset", query: "popular" }],
    publication: { kind: "publication", title: "Search", type: "search", query: "cats", count: 4 },
    facets: [{ title: "Category" }],
    groups: [{ kind: "publication", group: true, title: "Group", type: "preset", query: "latest", count: 2 }],
  };
  const before = structuredClone(drafts);
  const grouped = serializeHomeLayout(drafts, "groups");
  const publication = serializeHomeLayout(drafts, "publications");
  assert.equal(grouped.some((x) => x.group), true);
  assert.equal(publication.some((x) => x.group), false);
  assert.deepEqual(drafts, before);
});

test("facet entries follow the current publication source", () => {
  const drafts = {
    navigation: [],
    publication: { kind: "publication", type: "search", query: "language:japanese", count: 5 },
    facets: [{ title: "Categories" }],
    groups: [],
  };
  const facet = serializeHomeLayout(drafts, "publications").find((x) => x.kind === "facets");
  assert.equal(facet.type, "search");
  assert.equal(facet.query, "language:japanese");
  drafts.publication = { ...drafts.publication, type: "preset", query: "toplist:month" };
  const updated = serializeHomeLayout(drafts, "publications").find((x) => x.kind === "facets");
  assert.equal(updated.type, "preset");
  assert.equal(updated.query, "toplist:month");
  assert.equal(canUseFacets(drafts.publication), true);
  assert.equal(canUseFacets({ type: "preset", query: "watched" }), true);
  assert.equal(canUseFacets({ type: "preset", query: "archives" }), true);
  assert.equal(chooseInitialHomeMode([{ kind: "publication", group: false }, { kind: "publication", group: true }]), "");
});

test("navigation group with multiple items splits and serializes cleanly", () => {
  const rawSections = [
    {
      kind: "navigation",
      group: true,
      title: "Rankings",
      items: [
        { title: "Month", type: "preset", query: "toplist:month" },
        { title: "Year", type: "preset", query: "toplist:year" },
      ],
    },
    {
      kind: "navigation",
      group: true,
      title: "Legacy Nav",
      type: "preset",
      query: "latest",
    },
  ];
  const split = splitHomeSections(rawSections);
  assert.equal(split.groups.length, 2);
  assert.equal(split.groups[0].title, "Rankings");
  assert.deepEqual(split.groups[0].items, [
    { title: "Month", type: "preset", query: "toplist:month" },
    { title: "Year", type: "preset", query: "toplist:year" },
  ]);
  assert.equal(split.groups[1].title, "Legacy Nav");
  assert.deepEqual(split.groups[1].items, [
    { title: "Legacy Nav", type: "preset", query: "latest" },
  ]);

  const serialized = serializeHomeLayout(split, "groups");
  assert.equal(serialized.length, 2);
  assert.equal(serialized[0].kind, "navigation");
  assert.equal(serialized[0].group, true);
  assert.equal(serialized[0].title, "Rankings");
  assert.equal(serialized[0].type, "preset");
  assert.equal(serialized[0].query, "toplist:month");
  assert.deepEqual(serialized[0].items, [
    { title: "Month", type: "preset", query: "toplist:month" },
    { title: "Year", type: "preset", query: "toplist:year" },
  ]);
});

test("page navigation items serialize and validate cleanly", () => {
  const pageNavLayout = {
    navigation: [
      { title: "同人专区", type: "page", query: "doujin" },
      { title: "返回主页", type: "page", query: "home" },
    ],
    publication: null,
    facets: [],
    groups: [],
  };
  const serialized = serializeHomeLayout(pageNavLayout, "publications");
  assert.equal(serialized.length, 2);
  assert.equal(serialized[0].kind, "navigation");
  assert.equal(serialized[0].type, "page");
  assert.equal(serialized[0].query, "doujin");
  assert.equal(serialized[1].query, "home");
});

test("navigation items support preset, search, and page types cleanly", () => {
  const mixedLayout = {
    navigation: [
      { title: "同人专区", type: "page", query: "doujin" },
      { title: "热门排行", type: "preset", query: "popular" },
      { title: "东方Project", type: "search", query: "touhou" },
    ],
    publication: null,
    facets: [],
    groups: [
      {
        kind: "navigation",
        group: true,
        title: "分类导航",
        items: [
          { title: "同人", type: "page", query: "doujin" },
          { title: "日榜", type: "preset", query: "toplist:yesterday" },
          { title: "搜索汉化", type: "search", query: "language:chinese" },
        ],
      },
    ],
  };

  const serialized = serializeHomeLayout(mixedLayout, "groups");
  assert.equal(serialized.length, 4);

  // Top navigation items
  assert.equal(serialized[0].type, "page");
  assert.equal(serialized[0].query, "doujin");
  assert.equal(serialized[1].type, "preset");
  assert.equal(serialized[1].query, "popular");
  assert.equal(serialized[2].type, "search");
  assert.equal(serialized[2].query, "touhou");

  // Group navigation items
  assert.equal(serialized[3].kind, "navigation");
  assert.equal(serialized[3].group, true);
  assert.equal(serialized[3].items.length, 3);
  assert.equal(serialized[3].items[0].type, "page");
  assert.equal(serialized[3].items[1].type, "preset");
  assert.equal(serialized[3].items[1].query, "toplist:yesterday");
  assert.equal(serialized[3].items[2].type, "search");
  assert.equal(serialized[3].items[2].query, "language:chinese");

  // Round-trip with splitHomeSections
  const split = splitHomeSections(serialized);
  assert.equal(split.navigation.length, 3);
  assert.equal(split.navigation[1].type, "preset");
  assert.equal(split.navigation[2].type, "search");
  assert.equal(split.groups[0].items[1].type, "preset");
  assert.equal(split.groups[0].items[2].type, "search");
});

import test from "node:test";
import assert from "node:assert/strict";
import {
  canUseFacets,
  chooseInitialHomeMode,
  createDefaultGlobalNavigation,
  createDefaultHomePages,
  createDefaultSearchFacets,
  isKnownPreset,
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

test("publication layout serializes clean publication entity", () => {
  const drafts = {
    navigation: [],
    publication: { kind: "publication", type: "search", query: "language:japanese", count: 5 },
    facets: [],
    groups: [],
  };
  const pub = serializeHomeLayout(drafts, "publications").find((x) => x.kind === "publication");
  assert.equal(pub.type, "search");
  assert.equal(pub.query, "language:japanese");
  assert.equal(canUseFacets(drafts.publication), true);
  assert.equal(canUseFacets({ type: "preset", query: "watched" }), true);
  assert.equal(canUseFacets({ type: "preset", query: "archives" }), true);
  assert.equal(canUseFacets({ type: "preset", query: "history" }), true);
  assert.equal(chooseInitialHomeMode([{ kind: "publication", group: false }, { kind: "publication", group: true }]), "");
});

test("custom facet sets split and serialize cleanly", () => {
  const layout = {
    navigation: [],
    publication: { kind: "publication", type: "search", query: "touhou", count: 10 },
    facets: [
      {
        title: "精选",
        items: [
          { title: "全部", query: "", categories: [], rating: 0, expunged: false, disable_filters: [] },
          { title: "4星汉化同人", query: "language:chinese", categories: ["Doujinshi"], rating: 4, expunged: true, disable_filters: ["language"] },
        ],
      },
    ],
    groups: [],
  };

  const serialized = serializeHomeLayout(layout, "publications");
  assert.equal(serialized.length, 2);
  assert.equal(serialized[1].kind, "facets");
  assert.equal(serialized[1].title, "精选");
  assert.equal(serialized[1].items.length, 2);
  assert.equal(serialized[1].items[1].title, "4星汉化同人");
  assert.equal(serialized[1].items[1].rating, 4);
  assert.equal(serialized[1].items[1].expunged, true);
  assert.deepEqual(serialized[1].items[1].categories, ["Doujinshi"]);
  assert.deepEqual(serialized[1].items[1].disable_filters, ["language"]);

  const split = splitHomeSections(serialized);
  assert.equal(split.facets.length, 1);
  assert.equal(split.facets[0].title, "精选");
  assert.equal(split.facets[0].items.length, 2);
  assert.equal(split.facets[0].items[1].title, "4星汉化同人");
  assert.equal(split.facets[0].items[1].rating, 4);
});

test("referenced facet sets split and serialize cleanly", () => {
  const layout = {
    navigation: [],
    publication: { kind: "publication", type: "search", query: "touhou", count: 10 },
    facets: [
      {
        facet_set: "chinese_manga",
        title: "",
        items: [],
      },
    ],
    groups: [],
  };

  const serialized = serializeHomeLayout(layout, "publications");
  assert.equal(serialized.length, 2);
  assert.equal(serialized[1].kind, "facets");
  assert.equal(serialized[1].facet_set, "chinese_manga");

  const split = splitHomeSections(serialized);
  assert.equal(split.facets.length, 1);
  assert.equal(split.facets[0].facet_set, "chinese_manga");
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

test("createDefaultGlobalNavigation and createDefaultHomePages decouple top navigation from pages", () => {
  const globalNav = createDefaultGlobalNavigation();
  assert.equal(globalNav.length, 4);
  assert.deepEqual(globalNav.map((n) => n.query), ["watched", "favorites", "toplists", "archives"]);
  assert.equal(globalNav.every((n) => n.kind === "navigation" && !n.group), true);
  assert.deepEqual(globalNav.map((n) => n.type), ["preset", "preset", "page", "preset"]);
  assert.equal(isKnownPreset("toplists"), false);
  assert.equal(isKnownPreset("history"), true);

  const pages = createDefaultHomePages();
  assert.equal(pages.length, 2);
  assert.equal(pages[0].id, "home");
  assert.equal(pages[0].mode, "groups");
  assert.equal(pages[0].layout.groups.length, 3);
  assert.deepEqual(
    pages[0].layout.groups.map((g) => g.query),
    ["popular", "latest", "history"]
  );
  assert.deepEqual(pages[0].layout.navigation, []);

  assert.equal(pages[1].id, "toplists");
  assert.equal(pages[1].mode, "groups");
  assert.equal(pages[1].layout.groups.length, 4);
  assert.deepEqual(
    pages[1].layout.groups.map((g) => g.query),
    ["toplist:yesterday", "toplist:month", "toplist:year", "toplist:alltime"]
  );

  // Legacy preset:toplists navigation auto-migrates to page:toplists
  const legacySections = [
    { kind: "navigation", group: false, title: "Toplists", type: "preset", query: "toplists" },
  ];
  const split = splitHomeSections(legacySections);
  assert.equal(split.navigation[0].type, "page");
  assert.equal(split.navigation[0].query, "toplists");
});

test("createDefaultSearchFacets returns 10 editable categories", () => {
  const facets = createDefaultSearchFacets();
  assert.equal(facets.length, 10);

  const categories = [
    "Doujinshi", "Manga", "Artist CG", "Game CG", "Western",
    "Non-H", "Image Set", "Cosplay", "Asian Porn", "Misc",
  ];
  for (let i = 0; i < categories.length; i++) {
    const item = facets[i];
    assert.equal(item.title, categories[i]);
    assert.deepEqual(item.categories, [categories[i]]);
    assert.equal(item.rating, 0);
    assert.equal(item.expunged, false);
  }
});

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
  assert.equal(chooseInitialHomeMode([{ kind: "publication", group: false }, { kind: "publication", group: true }]), "");
});

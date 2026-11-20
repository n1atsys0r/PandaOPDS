import test from "node:test";
import assert from "node:assert/strict";
import { resolveOpenSearchUrl } from "./opensearch-helper.js";

const rootCatalogLinks = [
  {
    rel: "search",
    href: "/opds/v2.0/gallery?query={searchTerms}",
    type: "application/opds+json",
  },
  {
    rel: "self",
    href: "/opds/v2.0",
    type: "application/opds+json",
  },
];

const watchedFeedLinks = [
  {
    rel: "search",
    href: "/opds/v2.0/gallery?query=watched&f_search={searchTerms}",
    type: "application/opds+json",
  },
  {
    rel: "self",
    href: "/opds/v2.0/gallery?query=watched",
    type: "application/opds+json",
  },
];

const favoritesFeedLinks = [
  {
    rel: "search",
    href: "/opds/v2.0/gallery?query=favorites&favcat=all&f_search={searchTerms}",
    type: "application/opds+json",
  },
  {
    rel: "self",
    href: "/opds/v2.0/gallery?query=favorites&favcat=all",
    type: "application/opds+json",
  },
];

test("empty or whitespace query returns empty string", () => {
  assert.equal(resolveOpenSearchUrl("", null, [], rootCatalogLinks), "");
  assert.equal(resolveOpenSearchUrl("   ", null, [], rootCatalogLinks), "");
  assert.equal(resolveOpenSearchUrl(null, null, [], rootCatalogLinks), "");
});

test("root explore search resolves to root catalog global search template", () => {
  const url = resolveOpenSearchUrl("touhou", null, [], rootCatalogLinks);
  assert.equal(url, "/opds/v2.0/gallery?query=touhou");
});

test("watched subroute scoped search resolves to watched f_search by default", () => {
  const activeRoute = {
    title: "Watched",
    href: "/opds/v2.0/gallery?query=watched",
  };
  const url = resolveOpenSearchUrl(
    "touhou",
    activeRoute,
    watchedFeedLinks,
    rootCatalogLinks,
  );
  assert.equal(url, "/opds/v2.0/gallery?query=watched&f_search=touhou");
});

test("watched subroute global search resolves to global query template", () => {
  const activeRoute = {
    title: "Watched",
    href: "/opds/v2.0/gallery?query=watched",
  };
  const url = resolveOpenSearchUrl(
    "female:catgirl",
    activeRoute,
    watchedFeedLinks,
    rootCatalogLinks,
    {
      global: true,
    },
  );
  assert.equal(url, "/opds/v2.0/gallery?query=female%3Acatgirl");
});

test("favorites subroute scoped search resolves to favorites f_search by default", () => {
  const activeRoute = {
    title: "Favorites",
    href: "/opds/v2.0/gallery?query=favorites&favcat=all",
  };
  const url = resolveOpenSearchUrl(
    "genshin",
    activeRoute,
    favoritesFeedLinks,
    rootCatalogLinks,
  );
  assert.equal(
    url,
    "/opds/v2.0/gallery?query=favorites&favcat=all&f_search=genshin",
  );
});

test("favorites subroute global search resolves to global query template", () => {
  const activeRoute = {
    title: "Favorites",
    href: "/opds/v2.0/gallery?query=favorites&favcat=all",
  };
  const url = resolveOpenSearchUrl(
    "artist:alice$",
    activeRoute,
    favoritesFeedLinks,
    rootCatalogLinks,
    {
      global: true,
    },
  );
  assert.equal(url, "/opds/v2.0/gallery?query=artist%3Aalice%24");
});

test("fallback to standard endpoint when search link is missing or catalog is empty", () => {
  assert.equal(
    resolveOpenSearchUrl("test", null, [], []),
    "/opds/v2.0/gallery?query=test",
  );
});

test("handles search link without {searchTerms} template using query param", () => {
  const customLinks = [{ rel: "search", href: "/opds/v2.0/search" }];
  assert.equal(
    resolveOpenSearchUrl("abc", null, [], customLinks),
    "/opds/v2.0/search?query=abc",
  );
  const customLinksWithParams = [
    { rel: "search", href: "/opds/v2.0/search?mode=deep" },
  ];
  assert.equal(
    resolveOpenSearchUrl("abc", null, [], customLinksWithParams),
    "/opds/v2.0/search?mode=deep&query=abc",
  );
});

/**
 * Resolve an OpenSearch target URL from OPDS feed links.
 *
 * @param {string} query Search terms.
 * @param {any} activeRoute The active drill-down subroute, if any.
 * @param {Array<{ rel: string, href: string }>} [routeFeedLinks=[]] Links from the active subroute feed.
 * @param {Array<{ rel: string, href: string }>} [catalogLinks=[]] Links from the root catalog feed.
 * @param {{ global?: boolean }} [options={}] When options.global is true, prefer the global root catalog template over the active route's scoped template.
 * @returns {string} The resolved search URL.
 */
export function resolveOpenSearchUrl(
  query,
  activeRoute,
  routeFeedLinks = [],
  catalogLinks = [],
  options = {},
) {
  const q = (query || "").trim();
  if (!q) return "";

  const preferGlobal = options.global === true;
  const allLinks =
    activeRoute && !preferGlobal
      ? [...(routeFeedLinks || []), ...(catalogLinks || [])]
      : [...(catalogLinks || []), ...(routeFeedLinks || [])];

  const searchLink = allLinks.find((l) => l && l.rel === "search");

  if (searchLink?.href) {
    if (searchLink.href.includes("{searchTerms}")) {
      return searchLink.href.replace("{searchTerms}", encodeURIComponent(q));
    }
    const sep = searchLink.href.includes("?") ? "&" : "?";
    return `${searchLink.href}${sep}query=${encodeURIComponent(q)}`;
  }

  return `/opds/v2.0/gallery?query=${encodeURIComponent(q)}`;
}

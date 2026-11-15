export function splitHomeSections(sections = []) {
  return {
    navigation: structuredClone(sections.filter((x) => x.kind === "navigation" && !x.group)),
    publication: structuredClone(sections.find((x) => x.kind === "publication" && !x.group) || null),
    facets: structuredClone(sections.filter((x) => x.kind === "facets" && !x.group).map(({ title }) => ({ title }))),
    groups: structuredClone(sections.filter((x) => x.group).map((x) => {
      if (x.kind === "navigation") {
        let items = Array.isArray(x.items) ? structuredClone(x.items) : [];
        if (items.length === 0 && (x.type || x.query)) {
          items = [{
            title: x.title || "",
            type: x.type || "preset",
            query: x.query || "latest",
          }];
        }
        return {
          ...x,
          items,
        };
      }
      return x;
    })),
  };
}

export function normalizeMode(mode) {
  if (mode === "sections" || mode === "groups") return "groups";
  return "publications";
}

export function chooseInitialHomeMode(sections = []) {
  const hasRootPub = sections.some((x) => x.kind === "publication" && !x.group);
  const hasGroups = sections.some((x) => x.group);
  if (hasRootPub && hasGroups) return "";
  return hasGroups ? "groups" : "publications";
}

export function canUseFacets(source) {
  if (!source) return false;
  if (source.type === "search") return Boolean(source.query?.trim());
  if (source.type !== "preset") return false;
  return source.query === "latest" ||
    source.query === "watched" ||
    source.query === "archives" ||
    ["toplist:yesterday", "toplist:month", "toplist:year", "toplist:alltime"].includes(source.query) ||
    source.query === "favorites" || /^favorites:\d+$/.test(source.query || "");
}

export function isValidSlug(slug) {
  return /^[a-zA-Z0-9_-]+$/.test(slug);
}

export function createDefaultHomePages() {
  const topNav = [
    { title: "Watched", type: "page", query: "watched" },
    { title: "Favorites", type: "page", query: "favorites" },
    { title: "Toplists", type: "page", query: "toplists" },
    { title: "Archives", type: "page", query: "archives" },
  ];

  return [
    {
      id: "home",
      title: "Home",
      mode: "groups",
      layout: {
        navigation: structuredClone(topNav),
        publication: null,
        facets: [],
        groups: [
          { kind: "publication", group: true, title: "Popular", type: "preset", query: "popular", count: 20 },
          { kind: "publication", group: true, title: "Latest", type: "preset", query: "latest", count: 20 },
        ],
      },
    },
    {
      id: "watched",
      title: "Watched",
      mode: "publications",
      layout: {
        navigation: structuredClone(topNav),
        publication: { kind: "publication", group: false, title: "Watched", type: "preset", query: "watched", count: 20 },
        facets: [{ title: "Categories" }],
        groups: [],
      },
    },
    {
      id: "favorites",
      title: "Favorites",
      mode: "publications",
      layout: {
        navigation: structuredClone(topNav),
        publication: { kind: "publication", group: false, title: "Favorites", type: "preset", query: "favorites", count: 20 },
        facets: [{ title: "Folders" }],
        groups: [],
      },
    },
    {
      id: "toplists",
      title: "Toplists",
      mode: "groups",
      layout: {
        navigation: structuredClone(topNav),
        publication: null,
        facets: [],
        groups: [
          { kind: "publication", group: true, title: "All Time", type: "preset", query: "toplist:alltime", count: 20 },
          { kind: "publication", group: true, title: "Past Year", type: "preset", query: "toplist:year", count: 20 },
          { kind: "publication", group: true, title: "Past Month", type: "preset", query: "toplist:month", count: 20 },
          { kind: "publication", group: true, title: "Yesterday", type: "preset", query: "toplist:yesterday", count: 20 },
        ],
      },
    },
    {
      id: "archives",
      title: "Archives",
      mode: "publications",
      layout: {
        navigation: structuredClone(topNav),
        publication: { kind: "publication", group: false, title: "Archives", type: "preset", query: "archives", count: 20 },
        facets: [{ title: "Categories" }],
        groups: [],
      },
    },
  ];
}

export function createDefaultPage(id, title, mode = "publications") {
  const normalized = normalizeMode(mode);
  return {
    id,
    title: title || (id === "home" ? "Home" : id),
    mode: normalized,
    layout: {
      navigation: [],
      publication: normalized === "publications"
        ? { kind: "publication", group: false, title: "Latest", type: "preset", query: "latest", count: 10 }
        : null,
      facets: [],
      groups: [],
    },
  };
}

export function serializeHomeLayout(layout, mode) {
  const normalizedMode = normalizeMode(mode);
  const navigation = (layout.navigation || []).map((x) => ({
    ...x,
    kind: "navigation",
    group: false,
    count: null,
    type: x.type || "page",
    query: x.query || "home",
  }));
  let content = [];
  if (normalizedMode === "groups") {
    content = (layout.groups || []).map((x) => {
      if (x.kind === "navigation") {
        const items = (x.items || []).map((it) => ({
          title: it.title || "",
          type: it.type || "page",
          query: it.query || "home",
        }));
        const first = items[0] || { type: "page", query: "home" };
        return {
          kind: "navigation",
          group: true,
          title: x.title || "",
          type: first.type,
          query: first.query,
          count: null,
          items,
        };
      }
      return {
        ...x,
        group: true,
        count: x.kind === "publication" ? (x.count == null || x.count === "" ? null : Number(x.count)) : null,
      };
    });
  } else if (normalizedMode === "publications") {
    if (layout.publication) {
      content.push({ ...layout.publication, kind: "publication", group: false, count: layout.publication.count == null || layout.publication.count === "" ? null : Number(layout.publication.count) });
      for (const facet of (layout.facets || [])) content.push({ kind: "facets", group: false, title: facet.title, type: layout.publication.type, query: layout.publication.query, count: null });
    }
  }
  return [...navigation, ...content];
}

export function replacesAlternateMode(sections = [], mode) {
  const normalizedMode = normalizeMode(mode);
  if (normalizedMode === "groups") return (sections || []).some((x) => x.kind === "publication" && !x.group);
  if (normalizedMode === "publications") return (sections || []).some((x) => x.group);
  return false;
}

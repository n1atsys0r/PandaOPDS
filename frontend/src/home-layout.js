export function isKnownPreset(query) {
  if (!query) return false;
  return (
    [
      "latest",
      "popular",
      "watched",
      "favorites",
      "archives",
      "history",
      "toplist:yesterday",
      "toplist:month",
      "toplist:year",
      "toplist:alltime",
    ].includes(query) || /^favorites:\d+$/.test(query)
  );
}

export function splitHomeSections(sections = []) {
  return {
    navigation: structuredClone(
      sections
        .filter((x) => x.kind === "navigation" && !x.group)
        .map((x) => {
          let type = x.type;
          if (!type) {
            type =
              x.query && isKnownPreset(x.query)
                ? "preset"
                : x.type === "search"
                  ? "search"
                  : x.query === "toplists" || x.query === "home"
                    ? "page"
                    : "preset";
          } else if (
            type === "preset" &&
            (x.query === "toplists" || x.query === "home")
          ) {
            type = "page";
          }
          return {
            ...x,
            type,
          };
        }),
    ),
    publication: structuredClone(
      sections.find((x) => x.kind === "publication" && !x.group) || null,
    ),
    facets: structuredClone(
      sections
        .filter((x) => x.kind === "facets" && !x.group)
        .map((f) => ({
          facet_set: f.facet_set || "",
          title: f.title || "",
          items: Array.isArray(f.items)
            ? f.items.map((it) => ({
                title: it.title || "",
                query: it.query || "",
                categories: Array.isArray(it.categories) ? it.categories : [],
                rating: Number(it.rating) || 0,
                expunged: Boolean(it.expunged),
                disable_filters: Array.isArray(it.disable_filters)
                  ? it.disable_filters
                  : [],
              }))
            : [],
        })),
    ),
    groups: structuredClone(
      sections
        .filter((x) => x.group)
        .map((x) => {
          if (x.kind === "navigation") {
            let items = Array.isArray(x.items) ? structuredClone(x.items) : [];
            if (items.length === 0 && (x.type || x.query)) {
              items = [
                {
                  title: x.title || "",
                  type: x.type || "preset",
                  query: x.query || "latest",
                },
              ];
            }
            items = items.map((it) => {
              let type = it.type;
              if (!type) {
                type =
                  it.query && isKnownPreset(it.query)
                    ? "preset"
                    : it.type === "search"
                      ? "search"
                      : it.query === "toplists" || it.query === "home"
                        ? "page"
                        : "preset";
              } else if (
                type === "preset" &&
                (it.query === "toplists" || it.query === "home")
              ) {
                type = "page";
              }
              return {
                ...it,
                type,
              };
            });
            return {
              ...x,
              items,
            };
          }
          return x;
        }),
    ),
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
  return (
    source.query === "latest" ||
    source.query === "watched" ||
    source.query === "archives" ||
    source.query === "history" ||
    [
      "toplist:yesterday",
      "toplist:month",
      "toplist:year",
      "toplist:alltime",
    ].includes(source.query) ||
    source.query === "favorites" ||
    /^favorites:\d+$/.test(source.query || "")
  );
}

export function isValidSlug(slug) {
  return /^[a-zA-Z0-9_-]+$/.test(slug);
}

export function createDefaultGlobalNavigation() {
  return [
    {
      kind: "navigation",
      group: false,
      title: "",
      type: "page",
      query: "home",
    },
    {
      kind: "navigation",
      group: false,
      title: "Watched",
      type: "preset",
      query: "watched",
    },
    {
      kind: "navigation",
      group: false,
      title: "Favorites",
      type: "preset",
      query: "favorites",
    },
    {
      kind: "navigation",
      group: false,
      title: "",
      type: "page",
      query: "toplists",
    },
    {
      kind: "navigation",
      group: false,
      title: "Archives",
      type: "preset",
      query: "archives",
    },
  ];
}

export function createDefaultSearchFacets() {
  return [
    {
      title: "Doujinshi",
      query: "",
      categories: ["Doujinshi"],
      rating: 0,
      expunged: false,
      disable_filters: [],
    },
    {
      title: "Manga",
      query: "",
      categories: ["Manga"],
      rating: 0,
      expunged: false,
      disable_filters: [],
    },
    {
      title: "Artist CG",
      query: "",
      categories: ["Artist CG"],
      rating: 0,
      expunged: false,
      disable_filters: [],
    },
    {
      title: "Game CG",
      query: "",
      categories: ["Game CG"],
      rating: 0,
      expunged: false,
      disable_filters: [],
    },
    {
      title: "Western",
      query: "",
      categories: ["Western"],
      rating: 0,
      expunged: false,
      disable_filters: [],
    },
    {
      title: "Non-H",
      query: "",
      categories: ["Non-H"],
      rating: 0,
      expunged: false,
      disable_filters: [],
    },
    {
      title: "Image Set",
      query: "",
      categories: ["Image Set"],
      rating: 0,
      expunged: false,
      disable_filters: [],
    },
    {
      title: "Cosplay",
      query: "",
      categories: ["Cosplay"],
      rating: 0,
      expunged: false,
      disable_filters: [],
    },
    {
      title: "Asian Porn",
      query: "",
      categories: ["Asian Porn"],
      rating: 0,
      expunged: false,
      disable_filters: [],
    },
    {
      title: "Misc",
      query: "",
      categories: ["Misc"],
      rating: 0,
      expunged: false,
      disable_filters: [],
    },
  ];
}

export function createDefaultHomePages() {
  return [
    {
      id: "home",
      title: "Home",
      mode: "groups",
      layout: {
        navigation: [],
        publication: null,
        facets: [],
        groups: [
          {
            kind: "publication",
            group: true,
            title: "Popular",
            type: "preset",
            query: "popular",
            count: 20,
          },
          {
            kind: "publication",
            group: true,
            title: "Latest",
            type: "preset",
            query: "latest",
            count: 20,
          },
          {
            kind: "publication",
            group: true,
            title: "Recently Read",
            type: "preset",
            query: "history",
            count: 20,
          },
        ],
      },
    },
    {
      id: "toplists",
      title: "Toplists",
      mode: "groups",
      layout: {
        navigation: [],
        publication: null,
        facets: [],
        groups: [
          {
            kind: "publication",
            group: true,
            title: "Yesterday",
            type: "preset",
            query: "toplist:yesterday",
            count: 20,
          },
          {
            kind: "publication",
            group: true,
            title: "Past Month",
            type: "preset",
            query: "toplist:month",
            count: 20,
          },
          {
            kind: "publication",
            group: true,
            title: "Past Year",
            type: "preset",
            query: "toplist:year",
            count: 20,
          },
          {
            kind: "publication",
            group: true,
            title: "All Time",
            type: "preset",
            query: "toplist:alltime",
            count: 20,
          },
        ],
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
      publication:
        normalized === "publications"
          ? {
              kind: "publication",
              group: false,
              title: "Latest",
              type: "preset",
              query: "latest",
              count: 10,
            }
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
    type:
      x.type ||
      (x.query && isKnownPreset(x.query)
        ? "preset"
        : x.type === "search"
          ? "search"
          : x.query === "toplists" || x.query === "home"
            ? "page"
            : "preset"),
    query:
      x.query ||
      (x.type === "preset" ? "latest" : x.type === "search" ? "" : "latest"),
    title:
      x.type === "page" ||
      (!x.type && (x.query === "toplists" || x.query === "home"))
        ? ""
        : x.title || "",
  }));
  let content = [];
  if (normalizedMode === "groups") {
    content = (layout.groups || []).map((x) => {
      if (x.kind === "navigation") {
        const items = (x.items || []).map((it) => ({
          title:
            it.type === "page" ||
            (!it.type && (it.query === "toplists" || it.query === "home"))
              ? ""
              : it.title || "",
          type:
            it.type ||
            (it.query && isKnownPreset(it.query)
              ? "preset"
              : it.type === "search"
                ? "search"
                : it.query === "toplists" || it.query === "home"
                  ? "page"
                  : "preset"),
          query:
            it.query ||
            (it.type === "preset"
              ? "latest"
              : it.type === "search"
                ? ""
                : "latest"),
        }));
        const first = items[0] || { type: "preset", query: "latest" };
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
        count:
          x.kind === "publication"
            ? x.count == null || x.count === ""
              ? null
              : Number(x.count)
            : null,
      };
    });
  } else if (normalizedMode === "publications") {
    if (layout.publication) {
      content.push({
        ...layout.publication,
        kind: "publication",
        group: false,
        count:
          layout.publication.count == null || layout.publication.count === ""
            ? null
            : Number(layout.publication.count),
      });
    }
    if (Array.isArray(layout.facets) && layout.facets.length > 0) {
      for (const f of layout.facets) {
        const sec = {
          kind: "facets",
          title: f.title || "",
          group: false,
        };
        if (f.facet_set) {
          sec.facet_set = f.facet_set;
        }
        if (Array.isArray(f.items) && f.items.length > 0) {
          sec.items = f.items.map((it) => ({
            title: it.title || "",
            query: it.query || "",
            categories: Array.isArray(it.categories) ? it.categories : [],
            rating: Number(it.rating) || 0,
            expunged: Boolean(it.expunged),
            disable_filters: Array.isArray(it.disable_filters)
              ? it.disable_filters
              : [],
          }));
        }
        content.push(sec);
      }
    }
  }
  return [...navigation, ...content];
}

export function replacesAlternateMode(sections = [], mode) {
  const normalizedMode = normalizeMode(mode);
  if (normalizedMode === "groups")
    return (sections || []).some((x) => x.kind === "publication" && !x.group);
  if (normalizedMode === "publications")
    return (sections || []).some((x) => x.group);
  return false;
}

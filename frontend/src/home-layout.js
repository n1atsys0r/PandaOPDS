export function splitHomeSections(sections = []) {
  return {
    navigation: structuredClone(sections.filter((x) => x.kind === "navigation" && !x.group)),
    publication: structuredClone(sections.find((x) => x.kind === "publication" && !x.group) || null),
    facets: structuredClone(sections.filter((x) => x.kind === "facets" && !x.group).map(({ title }) => ({ title }))),
    groups: structuredClone(sections.filter((x) => x.group)),
  };
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
    ["toplist:yesterday", "toplist:month", "toplist:year", "toplist:alltime"].includes(source.query) ||
    source.query === "favorites" || /^favorites:\d+$/.test(source.query || "");
}

export function serializeHomeLayout(layout, mode) {
  const navigation = layout.navigation.map((x) => ({ ...x, kind: "navigation", group: false, count: null }));
  let content = [];
  if (mode === "groups") {
    content = layout.groups.map((x) => ({ ...x, group: true, count: x.kind === "publication" ? (x.count == null || x.count === "" ? null : Number(x.count)) : null }));
  } else if (mode === "publications") {
    if (layout.publication) {
      content.push({ ...layout.publication, kind: "publication", group: false, count: layout.publication.count == null || layout.publication.count === "" ? null : Number(layout.publication.count) });
      for (const facet of layout.facets) content.push({ kind: "facets", group: false, title: facet.title, type: layout.publication.type, query: layout.publication.query, count: null });
    }
  }
  return [...navigation, ...content];
}

export function replacesAlternateMode(sections, mode) {
  if (mode === "groups") return sections.some((x) => x.kind === "publication" && !x.group);
  if (mode === "publications") return sections.some((x) => x.group);
  return false;
}

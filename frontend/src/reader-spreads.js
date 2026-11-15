// Single-page mode is one page per spread. Double mode pins the cover alone,
// then groups following pages in chronological pairs.
export function buildReaderSpreads(pageCount, mode = "single") {
  if (pageCount <= 0) return [];
  if (mode === "single")
    return Array.from({ length: pageCount }, (_, i) => [i]);
  const spreads = [[0]];
  for (let index = 1; index < pageCount; index += 2)
    spreads.push([index, index + 1 < pageCount ? index + 1 : null]);
  return spreads;
}

export function spreadForPage(spreads, page) {
  return Math.max(
    0,
    spreads.findIndex((spread) => spread.includes(page)),
  );
}

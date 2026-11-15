/** Ordered speculative pages after the visible spread in the latest direction. */
export function directionalPrefetchPages(
  spreads,
  selectedSpread,
  direction = "forward",
  aheadPageCount = 4,
  oppositeSpreadCount = 1,
) {
  const step = direction === "forward" ? 1 : -1;
  const pages = [];
  const seen = new Set();
  const add = (spread) => {
    for (const page of spreads[spread] || []) {
      if (page === null || seen.has(page)) continue;
      seen.add(page);
      pages.push(page);
    }
  };

  let addedAhead = 0;
  for (
    let spread = selectedSpread + step;
    spread >= 0 && spread < spreads.length && addedAhead < aheadPageCount;
    spread += step
  ) {
    const before = pages.length;
    add(spread);
    addedAhead += pages.length - before;
  }

  for (let n = 1; n <= oppositeSpreadCount; n += 1) {
    const spread = selectedSpread - step * n;
    if (spread < 0 || spread >= spreads.length) break;
    add(spread);
  }

  return pages;
}

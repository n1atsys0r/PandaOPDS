import test from "node:test";
import assert from "node:assert/strict";

class MockHistory {
  constructor() {
    this.stack = [];
    this.index = -1;
    this.listeners = [];
  }
  get state() {
    return this.stack[this.index]?.state || null;
  }
  addEventListener(event, fn) {
    if (event === "popstate") this.listeners.push(fn);
  }
  removeEventListener(event, fn) {
    this.listeners = this.listeners.filter((l) => l !== fn);
  }
  pushState(state, title, url) {
    this.stack = this.stack.slice(0, this.index + 1);
    this.stack.push({ state: structuredClone(state), title, url });
    this.index++;
  }
  replaceState(state, title, url) {
    if (this.index >= 0) {
      this.stack[this.index] = { state: structuredClone(state), title, url };
    } else {
      this.pushState(state, title, url);
    }
  }
  back() {
    if (this.index > 0) {
      this.index--;
      const entry = this.stack[this.index];
      const event = { state: entry.state };
      for (const fn of [...this.listeners]) {
        fn(event);
      }
      return entry;
    }
    return null;
  }
}

function createNavigationHarness() {
  const history = new MockHistory();
  const windowObj = {
    history,
    location: { hash: "", href: "http://localhost/" },
    addEventListener: (e, fn) => history.addEventListener(e, fn),
    removeEventListener: (e, fn) => history.removeEventListener(e, fn),
  };

  const state = {
    page: "dashboard",
    exploreSubRoute: null,
  };

  function route(v, historyMode = true) {
    state.page = v;
    if (v !== "explore" || historyMode !== false) {
      state.exploreSubRoute = null;
    }
    if (historyMode === true) {
      history.pushState({ page: v, subRoute: null }, "", `#${v}`);
      windowObj.location.hash = `#${v}`;
    } else if (historyMode === "replace") {
      history.replaceState({ page: v, subRoute: null }, "", `#${v}`);
      windowObj.location.hash = `#${v}`;
    }
  }

  function enterExploreSubRoute(navItem, pushHistory = true) {
    if (!navItem) return;
    state.page = "explore";
    state.exploreSubRoute = {
      title: navItem.title || "list",
      href: navItem.href,
    };
    if (pushHistory) {
      history.pushState(
        { page: "explore", subRoute: state.exploreSubRoute },
        "",
        `#explore/${encodeURIComponent(navItem.title || "list")}`,
      );
      windowObj.location.hash = `#explore/${encodeURIComponent(navItem.title || "list")}`;
    }
  }

  function exitExploreSubRoute(popHistory = true) {
    if (popHistory && history.state?.subRoute) {
      history.back();
    } else {
      state.exploreSubRoute = null;
      history.replaceState({ page: "explore", subRoute: null }, "", "#explore");
      windowObj.location.hash = "#explore";
    }
  }

  function onPopState(e) {
    const s = e.state;
    if (!s) {
      const rawHash = (windowObj.location.hash || "").replace(/^#\/?/, "");
      const parts = rawHash.split("/");
      const rootPage = parts[0] || "dashboard";
      if (["dashboard", "explore", "archive", "config", "home"].includes(rootPage)) {
        state.page = rootPage;
      }
      if (rootPage !== "explore" || !parts[1]) {
        state.exploreSubRoute = null;
      }
      return;
    }
    if (s.page && s.page !== state.page) {
      route(s.page, false);
    }
    state.exploreSubRoute = s.subRoute || null;
  }

  let readerArchive = null;
  let lastReaderArchive = null;
  let isClosingReader = false;

  function canReadOnline(a) {
    if (!a) return false;
    if (a.loading) return true;
    const pageCount = Math.max(0, Number(a?.page_count || a?.metadata?.filecount || 0));
    if (pageCount <= 0) return false;
    if (!a.status) return true;
    return ["ready", "unarchived", "failed"].includes(a.status);
  }

  function openReader(a) {
    if (!canReadOnline(a)) return;
    lastReaderArchive = a;
    readerArchive = a;
    history.pushState(
      {
        page: state.page,
        subRoute: state.exploreSubRoute ? { ...state.exploreSubRoute } : null,
        reader: true,
      },
      "",
      windowObj.location.href,
    );
  }

  function closeReader(fromHistory = false) {
    if (!readerArchive && !isClosingReader) return;
    const hadArchive = Boolean(readerArchive);
    readerArchive = null;
    if (!fromHistory && !isClosingReader && hadArchive && history.state?.reader) {
      isClosingReader = true;
      try {
        history.back();
      } finally {
        isClosingReader = false;
      }
    }
  }

  function onReaderPopState(e) {
    if (readerArchive && !e.state?.reader) {
      isClosingReader = false;
      closeReader(true);
    } else if (!readerArchive && e.state?.reader && lastReaderArchive) {
      isClosingReader = false;
      readerArchive = lastReaderArchive;
    }
  }

  windowObj.addEventListener("popstate", onPopState);
  windowObj.addEventListener("popstate", onReaderPopState);

  // Initial setup at #dashboard
  history.replaceState({ page: "dashboard", subRoute: null }, "", "#dashboard");
  windowObj.location.hash = "#dashboard";

  return {
    history,
    windowObj,
    state,
    route,
    enterExploreSubRoute,
    exitExploreSubRoute,
    canReadOnline,
    openReader,
    closeReader,
    getReaderArchive: () => readerArchive,
  };
}

test("root tab navigation replaces history instead of piling up linear stack", () => {
  const h = createNavigationHarness();

  // Switch tabs from sidebar: Dashboard -> Home -> Explore
  h.route("home", "replace");
  assert.equal(h.state.page, "home");
  assert.equal(h.history.stack.length, 1);
  assert.equal(h.history.index, 0);

  h.route("explore", "replace");
  assert.equal(h.state.page, "explore");
  assert.equal(h.history.stack.length, 1);
  assert.equal(h.history.index, 0);
  assert.equal(h.history.state.page, "explore");
});

test("exploring and drilling down pushes subroute and allows clean browser back", () => {
  const h = createNavigationHarness();
  h.route("explore", "replace");

  // Drill down into category
  h.enterExploreSubRoute({ title: "Doujinshi", href: "/opds/v2.0/category/doujinshi" });
  assert.equal(h.state.page, "explore");
  assert.equal(h.state.exploreSubRoute?.title, "Doujinshi");
  assert.equal(h.history.stack.length, 2);
  assert.equal(h.history.index, 1);

  // Press browser Back
  h.history.back();
  assert.equal(h.state.page, "explore");
  assert.equal(h.state.exploreSubRoute, null);
  assert.equal(h.history.index, 0);
  // User is on Explore Home, not bouncing into "home" or "dashboard"
});

test("canReadOnline accepts loading archive so history state is pushed immediately", () => {
  const h = createNavigationHarness();
  h.route("explore", "replace");
  h.enterExploreSubRoute({ title: "Manga", href: "/opds/v2.0/category/manga" });

  const loadingArchive = { gid: 123, token: "abc", title: "Loading Pub", page_count: 0, loading: true };
  assert.equal(h.canReadOnline(loadingArchive), true);

  h.openReader(loadingArchive);
  assert.equal(h.getReaderArchive()?.title, "Loading Pub");
  assert.equal(h.history.state.reader, true);
  assert.equal(h.history.index, 2);
});

test("closing reader via X button unwinds reader without double-popping subroute", () => {
  const h = createNavigationHarness();
  h.route("explore", "replace");
  h.enterExploreSubRoute({ title: "Doujinshi", href: "/opds/v2.0/category/doujinshi" });

  h.openReader({ gid: 456, token: "def", title: "Book A", page_count: 10 });
  assert.equal(h.getReaderArchive()?.title, "Book A");
  assert.equal(h.history.index, 2);

  // Close reader via UI (closeReader(false))
  h.closeReader(false);
  assert.equal(h.getReaderArchive(), null);
  assert.equal(h.history.index, 1);
  // Still in Doujinshi drill-down view!
  assert.equal(h.state.page, "explore");
  assert.equal(h.state.exploreSubRoute?.title, "Doujinshi");
});

test("closing reader via browser back unwinds reader and keeps subroute intact", () => {
  const h = createNavigationHarness();
  h.route("explore", "replace");
  h.enterExploreSubRoute({ title: "Doujinshi", href: "/opds/v2.0/category/doujinshi" });

  h.openReader({ gid: 789, token: "ghi", title: "Book B", page_count: 15 });
  assert.equal(h.getReaderArchive()?.title, "Book B");
  assert.equal(h.history.index, 2);

  // Browser back event pops the reader
  h.history.back();
  assert.equal(h.getReaderArchive(), null);
  assert.equal(h.history.index, 1);
  assert.equal(h.state.page, "explore");
  assert.equal(h.state.exploreSubRoute?.title, "Doujinshi");

  // Press browser back again: unwinds from drilldown to Explore Home
  h.history.back();
  assert.equal(h.state.page, "explore");
  assert.equal(h.state.exploreSubRoute, null);
  assert.equal(h.history.index, 0);
});

test("breadcrumb exitExploreSubRoute pops back cleanly to Explore Home", () => {
  const h = createNavigationHarness();
  h.route("explore", "replace");
  h.enterExploreSubRoute({ title: "Doujinshi", href: "/opds/v2.0/category/doujinshi" });
  assert.equal(h.history.index, 1);

  // Click breadcrumb "探索"
  h.exitExploreSubRoute(true);
  assert.equal(h.history.index, 0);
  assert.equal(h.state.page, "explore");
  assert.equal(h.state.exploreSubRoute, null);
});

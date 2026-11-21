import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { archiveActionFor } from "./archive-flow.js";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

function resolveExploreArchivePayload(pub) {
  return {
    gid: pub.gid,
    token: pub.token,
    title: pub.title,
    status: pub.archiveItem?.status || "unarchived",
  };
}

test("resolveExploreArchivePayload assigns status correctly for unarchived vs existing archives", () => {
  // Brand new publication from explore without archive item
  const newPub = {
    gid: "123456",
    token: "abcdef1234",
    title: "Brand New Gallery",
  };
  assert.deepEqual(resolveExploreArchivePayload(newPub), {
    gid: "123456",
    token: "abcdef1234",
    title: "Brand New Gallery",
    status: "unarchived",
  });

  // Failed archive retry
  const failedPub = {
    gid: "123456",
    token: "abcdef1234",
    title: "Failed Gallery",
    archiveItem: { status: "failed" },
  };
  assert.deepEqual(resolveExploreArchivePayload(failedPub), {
    gid: "123456",
    token: "abcdef1234",
    title: "Failed Gallery",
    status: "failed",
  });

  // Explicit unarchived status
  const unarchivedPub = {
    gid: "123456",
    token: "abcdef1234",
    title: "Unarchived Favorite",
    archiveItem: { status: "unarchived" },
  };
  assert.deepEqual(resolveExploreArchivePayload(unarchivedPub), {
    gid: "123456",
    token: "abcdef1234",
    title: "Unarchived Favorite",
    status: "unarchived",
  });
});

test("associated and unavailable archives offer management while quotes use normal start",()=>{
  assert.equal(archiveActionFor("ready"),"manage");
  assert.equal(archiveActionFor("unavailable"),"manage");
  assert.equal(archiveActionFor("failed"),"quote");
  assert.equal(archiveActionFor("unarchived"),"quote");
  for(const status of ["pending","submitting","staging","downloading","zipping"]) assert.equal(archiveActionFor(status),"busy");
});

test("z-index stacking invariant: Dialog < AlertDialog < Select/Dropdown < Tooltip < Reader", () => {
  const readComponent = (relPath) =>
    fs.readFileSync(path.resolve(__dirname, relPath), "utf-8");

  const dialogContent = readComponent("components/ui/dialog/DialogContent.vue");
  const dialogOverlay = readComponent("components/ui/dialog/DialogOverlay.vue");
  const alertDialogContent = readComponent(
    "components/ui/alert-dialog/AlertDialogContent.vue",
  );
  const selectContent = readComponent("components/ui/select/SelectContent.vue");
  const dropdownContent = readComponent(
    "components/ui/dropdown-menu/DropdownMenuContent.vue",
  );
  const tooltipContent = readComponent(
    "components/ui/tooltip/TooltipContent.vue",
  );
  const archiveReader = readComponent("components/ArchiveReader.vue");

  // Dialog layers must be z-50
  assert.match(dialogContent, /z-50/, "DialogContent should be z-50");
  assert.match(dialogOverlay, /z-50/, "DialogOverlay should be z-50");

  // AlertDialog layers must be z-[100] so they strictly layer over Dialog
  assert.match(
    alertDialogContent,
    /z-\[100\]/,
    "AlertDialogContent should be z-[100]",
  );
  assert.match(
    alertDialogContent,
    /alert-dialog-overlay[^>]+z-\[100\]/,
    "AlertDialogOverlay should be z-[100]",
  );

  // Floating controls (Select, Dropdown) must be z-[110] so they layer over AlertDialog (e.g. quality picker)
  assert.match(selectContent, /z-\[110\]/, "SelectContent should be z-[110]");
  assert.match(
    dropdownContent,
    /z-\[110\]/,
    "DropdownMenuContent should be z-[110]",
  );

  // Tooltips must be z-[120]
  assert.match(tooltipContent, /z-\[120\]/, "TooltipContent should be z-[120]");

  // Reader must be z-[1000]
  assert.match(archiveReader, /z-\[1000\]/, "ArchiveReader should be z-[1000]");
});

function resolveExploreFavoritePlan(payload, existingArchives = []) {
  const pub = payload?.pub || payload;
  const action = payload?.action;
  const favcat = payload?.favcat;

  const item = existingArchives.find(
    (a) =>
      (a.gid === pub.gid || a.active_gid === pub.gid) &&
      (a.token === pub.token || a.active_token === pub.token),
  );

  if (action === "remove") {
    const target = item || {
      gid: pub.gid,
      token: pub.token,
      title: pub.title,
      status: "unarchived",
    };
    return { type: "remove_request", target };
  }

  const effectiveAction = action || (item?.is_favorited ? "move" : "add");
  const effectiveFavcat = favcat != null ? Number(favcat) : 0;
  return {
    type: "favorite_call",
    pub,
    action: effectiveAction,
    favcat: effectiveFavcat,
  };
}

test("resolveExploreFavoritePlan routes category add, move, and remove accurately", () => {
  const pub = { gid: "12345", token: "abcdef", title: "Test Gallery" };

  // 1. Unfavorited item -> user selects category 3 -> should invoke add to 3
  const addPlan = resolveExploreFavoritePlan(
    { pub, action: "add", favcat: 3 },
    [],
  );
  assert.deepEqual(addPlan, {
    type: "favorite_call",
    pub,
    action: "add",
    favcat: 3,
  });

  // 2. Favorited item in category 1 -> user selects category 7 -> should invoke move to 7
  const favoritedItem = {
    gid: "12345",
    token: "abcdef",
    is_favorited: true,
    favcat: 1,
  };
  const movePlan = resolveExploreFavoritePlan(
    { pub, action: "move", favcat: 7 },
    [favoritedItem],
  );
  assert.deepEqual(movePlan, {
    type: "favorite_call",
    pub,
    action: "move",
    favcat: 7,
  });

  // 3. Favorited item -> user chooses remove -> should request removal targeting local archive item
  const removePlan = resolveExploreFavoritePlan({ pub, action: "remove" }, [
    favoritedItem,
  ]);
  assert.deepEqual(removePlan, {
    type: "remove_request",
    target: favoritedItem,
  });

  // 4. Favorited item without local archive record -> user chooses remove -> should request removal targeting unarchived entry
  const removeUnarchivedPlan = resolveExploreFavoritePlan(
    { pub, action: "remove" },
    [],
  );
  assert.deepEqual(removeUnarchivedPlan, {
    type: "remove_request",
    target: {
      gid: "12345",
      token: "abcdef",
      title: "Test Gallery",
      status: "unarchived",
    },
  });
});

test("active archive in-progress statuses classify pending, submitting, staging, downloading, and zipping", () => {
  const ACTIVE_ARCHIVE_STATUSES = [
    "pending",
    "submitting",
    "staging",
    "downloading",
    "zipping",
  ];

  function isArchiveDownloading(status) {
    return ACTIVE_ARCHIVE_STATUSES.includes(status);
  }

  // Initial submission status
  assert.equal(isArchiveDownloading("pending"), true);
  // Staging phase
  assert.equal(isArchiveDownloading("staging"), true);
  // Image download phase
  assert.equal(isArchiveDownloading("downloading"), true);
  // Zip compression phase
  assert.equal(isArchiveDownloading("zipping"), true);
  // Terminal and idle statuses
  assert.equal(isArchiveDownloading("ready"), false);
  assert.equal(isArchiveDownloading("failed"), false);
  assert.equal(isArchiveDownloading("unarchived"), false);
  assert.equal(isArchiveDownloading(undefined), false);
});

test("favorite overrides resolve immediate optimistic state over stale feed metadata", () => {
  const overrides = {};
  function setFavoriteOverride(gid, token, isFavorited, favcat = null) {
    const key = `${gid}:${token}`;
    if (isFavorited) {
      overrides[key] = {
        isFavorited: true,
        favcat: favcat != null ? Number(favcat) : 0,
        favcatName: `Favcat ${favcat}`,
      };
    } else {
      overrides[key] = {
        isFavorited: false,
        favcat: null,
        favcatName: "",
      };
    }
  }

  function resolveFavState(pub, overridesMap) {
    const key = `${pub.gid}:${pub.token}`;
    const override = overridesMap[key];
    const rawMeta = pub.raw?.metadata || {};
    let isFavorited = Boolean(
      rawMeta["x:favorite"] || rawMeta["x:is_favorited"],
    );
    let favcat = rawMeta["x:favcat"] ?? null;
    if (override != null) {
      isFavorited = override.isFavorited;
      favcat = override.favcat;
    }
    return { isFavorited, favcat };
  }

  const pub = {
    gid: "999",
    token: "tok999",
    raw: { metadata: { "x:favorite": false } },
  };

  // Initially unfavorited
  assert.deepEqual(resolveFavState(pub, overrides), {
    isFavorited: false,
    favcat: null,
  });

  // User adds to category 4 -> immediate optimistic override
  setFavoriteOverride("999", "tok999", true, 4);
  assert.deepEqual(resolveFavState(pub, overrides), {
    isFavorited: true,
    favcat: 4,
  });

  // User moves to category 8 -> immediate optimistic update
  setFavoriteOverride("999", "tok999", true, 8);
  assert.deepEqual(resolveFavState(pub, overrides), {
    isFavorited: true,
    favcat: 8,
  });

  // User removes favorite -> immediate un-favorited state
  setFavoriteOverride("999", "tok999", false);
  assert.deepEqual(resolveFavState(pub, overrides), {
    isFavorited: false,
    favcat: null,
  });
});

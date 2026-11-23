export const FAVCAT_SOLID_COLORS = [
  "#6b7280", // 0: 灰 (深色模式适配，EH 原始为 #000000 黑色)
  "#fc4e4f", // 1: 红
  "#e58200", // 2: 橙
  "#dde500", // 3: 黄
  "#05bf0b", // 4: 绿
  "#14e723", // 5: 黄绿
  "#68c9de", // 6: 青
  "#5050d7", // 7: 蓝
  "#9755f5", // 8: 紫
  "#fe93ff", // 9: 粉
];

export const FAVCAT_PALETTE = [
  {
    bg: "rgba(158, 158, 158, 0.22)",
    border: "rgba(158, 158, 158, 0.45)",
    text: "#e5e5e5",
  }, // 0: 灰
  {
    bg: "rgba(252, 78, 79, 0.22)",
    border: "rgba(252, 78, 79, 0.45)",
    text: "#fc4e4f",
  }, // 1: 红
  {
    bg: "rgba(229, 130, 0, 0.22)",
    border: "rgba(229, 130, 0, 0.45)",
    text: "#e58200",
  }, // 2: 橙
  {
    bg: "rgba(221, 229, 0, 0.22)",
    border: "rgba(221, 229, 0, 0.45)",
    text: "#dde500",
  }, // 3: 黄
  {
    bg: "rgba(5, 191, 11, 0.22)",
    border: "rgba(5, 191, 11, 0.45)",
    text: "#05bf0b",
  }, // 4: 绿
  {
    bg: "rgba(20, 231, 35, 0.22)",
    border: "rgba(20, 231, 35, 0.45)",
    text: "#14e723",
  }, // 5: 黄绿
  {
    bg: "rgba(104, 201, 222, 0.22)",
    border: "rgba(104, 201, 222, 0.45)",
    text: "#68c9de",
  }, // 6: 青
  {
    bg: "rgba(80, 80, 215, 0.22)",
    border: "rgba(80, 80, 215, 0.45)",
    text: "#5050d7",
  }, // 7: 蓝
  {
    bg: "rgba(151, 85, 245, 0.22)",
    border: "rgba(151, 85, 245, 0.45)",
    text: "#9755f5",
  }, // 8: 紫
  {
    bg: "rgba(254, 147, 255, 0.22)",
    border: "rgba(254, 147, 255, 0.45)",
    text: "#fe93ff",
  }, // 9: 粉
];

export function getFavcatSolidColor(favcatId) {
  const id = Number(favcatId);
  if (Number.isInteger(id) && id >= 0 && id <= 9) {
    return FAVCAT_SOLID_COLORS[id];
  }
  return FAVCAT_SOLID_COLORS[0];
}

export function getFavcatBadgeStyle(favcatId) {
  const id = Number(favcatId);
  if (Number.isInteger(id) && id >= 0 && id <= 9) {
    return FAVCAT_PALETTE[id];
  }
  return FAVCAT_PALETTE[0];
}

export function resolveFavcatId(pub, override = null) {
  if (!pub) return null;
  if (override != null) {
    if (!override.isFavorited) return null;
    const cat = Number(override.favcat);
    return Number.isInteger(cat) && cat >= 0 && cat <= 9 ? cat : 0;
  }
  if (pub.favcat != null && !Number.isNaN(Number(pub.favcat))) {
    const cat = Number(pub.favcat);
    if (Number.isInteger(cat) && cat >= 0 && cat <= 9) return cat;
  }
  if (
    pub.archiveItem?.favcat != null &&
    !Number.isNaN(Number(pub.archiveItem.favcat))
  ) {
    const cat = Number(pub.archiveItem.favcat);
    if (Number.isInteger(cat) && cat >= 0 && cat <= 9) return cat;
  }
  const meta = pub.metadata || pub.raw?.metadata || {};
  if (meta["x:favcat"] != null && !Number.isNaN(Number(meta["x:favcat"]))) {
    const cat = Number(meta["x:favcat"]);
    if (Number.isInteger(cat) && cat >= 0 && cat <= 9) return cat;
  }
  const coll = pub.belongsTo?.collection || meta.belongsTo?.collection;
  const rawHref = Array.isArray(coll)
    ? coll[0]?.links?.[0]?.href || coll[0]?.href || ""
    : coll?.links?.[0]?.href || coll?.href || "";
  const match = String(rawHref).match(/[?&]favcat=(\d+)/);
  if (match) {
    const cat = Number(match[1]);
    if (Number.isInteger(cat) && cat >= 0 && cat <= 9) return cat;
  }
  if (pub.favorited || pub.is_favorited) return 0;
  return null;
}

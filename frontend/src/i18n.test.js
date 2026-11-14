import test from "node:test";
import assert from "node:assert/strict";
import { t, currentLocale, setLocale, toggleLocale } from "./i18n.js";

test("i18n resolves keys in default zh locale", () => {
  setLocale("zh");
  assert.equal(currentLocale.value, "zh");
  assert.equal(t("nav.dashboard"), "概览");
  assert.equal(t("nav.settings"), "设置");
  assert.equal(t("dashboard.status_ok"), "服务运行正常");
});

test("i18n switches to en and resolves english translations", () => {
  setLocale("en");
  assert.equal(currentLocale.value, "en");
  assert.equal(t("nav.dashboard"), "Dashboard");
  assert.equal(t("nav.settings"), "Settings");
  assert.equal(t("dashboard.status_ok"), "Service Healthy");
});

test("i18n interpolates variables properly", () => {
  setLocale("zh");
  assert.equal(t("archive.scan_banner_result", { imported: 3, skipped: 1, conflicts: 0 }), "扫描完成：已导入 3 本，跳过 1 本，冲突 0 本");

  setLocale("en");
  assert.equal(t("archive.scan_banner_result", { imported: 3, skipped: 1, conflicts: 0 }), "Scan complete: imported 3, skipped 1, conflicts 0");
});

test("toggleLocale cycles between zh and en", () => {
  setLocale("zh");
  assert.equal(currentLocale.value, "zh");
  toggleLocale();
  assert.equal(currentLocale.value, "en");
  toggleLocale();
  assert.equal(currentLocale.value, "zh");
});

test("fallback to key when missing", () => {
  assert.equal(t("non.existent.key"), "non.existent.key");
});

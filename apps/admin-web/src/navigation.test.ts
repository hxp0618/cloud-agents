import { describe, expect, it } from "vitest";
import { matchingNavigation, navigationPages } from "./navigation";
import { translate, type Translate } from "./i18n";

describe("admin navigation commands", () => {
  const english: Translate = (key, values) => translate("en-US", key, values);
  const chinese: Translate = (key, values) => translate("zh-CN", key, values);

  it("uses unique real resource routes and excludes the current page", () => {
    expect(new Set(navigationPages.map(({ id }) => id)).size).toBe(10);
    expect(matchingNavigation("targets", "", english).map(({ id }) => id)).toEqual(
      navigationPages.filter(({ id }) => id !== "targets").map(({ id }) => id),
    );
  });

  it("matches localized labels and all query words without storing or interpreting input", () => {
    expect(
      matchingNavigation("overview", " deployment  TARGET ", english).map(({ id }) => id),
    ).toEqual(["targets"]);
    expect(matchingNavigation("overview", "网络", chinese).map(({ id }) => id)).toEqual([
      "network",
    ]);
    expect(matchingNavigation("overview", "go to storage", english).map(({ id }) => id)).toEqual([
      "storage",
    ]);
    expect(matchingNavigation("overview", "<script>credentialRef</script>", english)).toEqual([]);
    expect(matchingNavigation("overview", "does-not-exist", chinese)).toEqual([]);
  });
});

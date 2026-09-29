import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

import {
  applyCatalogPatch,
  formatCatalogPatch,
  productCatalogPatchPath,
  readProductCatalogs,
} from "./platform-migration-catalog-patch";

const root = resolve(import.meta.dirname, "../..");
const text = (lines: ReadonlyArray<string>) => Buffer.from(`${lines.join("\n")}\n`);

describe("product catalog patches", () => {
  const base = text(["{", '  "a": 1,', '  "b": 2,', '  "c": 3', "}"]);

  it("round-trips insertions, deletions and replacements", () => {
    for (const target of [
      base,
      text(["{", '  "a": 1,', '  "b": 2,', '  "c": 3,', '  "d": 4', "}"]),
      text(["{", '  "c": 3', "}"]),
      text(["[", '  "b": 2,', "]", ""]),
      text([""]),
    ]) {
      const patch = formatCatalogPatch("base.json", "target.json", base, target);
      expect(applyCatalogPatch(patch, "base.json", "target.json", base)).toEqual(target);
    }
  });

  it("rejects foreign headers, stale context and malformed hunks", () => {
    const patch = formatCatalogPatch(
      "base.json",
      "target.json",
      base,
      text(["{", '  "a": 1,', '  "b": 20,', '  "c": 3', "}"]),
    ).toString();
    const apply = (value: string, basePath = "base.json") =>
      applyCatalogPatch(Buffer.from(value), basePath, "target.json", base);
    expect(() => apply(patch, "other.json")).toThrow(/CATALOG_PATCH_HEADER/);
    expect(() => apply(patch.replace('-  "b": 2,', '-  "b": 3,'))).toThrow(/CATALOG_PATCH_CONTEXT/);
    expect(() => apply(patch.replace('+  "b": 20,\n', ""))).toThrow(/CATALOG_PATCH_INSERT/);
    expect(() => apply(patch.replace("@@ -3,1 +3,1 @@", "@@ -9,1 +9,1 @@"))).toThrow(
      /CATALOG_PATCH_RANGE/,
    );
    expect(() => apply(patch.trimEnd())).toThrow(/CATALOG_PATCH_LINES/);
    expect(() => formatCatalogPatch("base.json", "target.json", base, Buffer.from("x"))).toThrow(
      /CATALOG_PATCH_LINES/,
    );
  });

  it("binds patches to exact product catalog paths and known versions", () => {
    expect(
      productCatalogPatchPath(
        "services/control-plane/migrations/product/000101/catalog/schema-000100.json",
      ),
    ).toBe("services/control-plane/migrations/product/000101/catalog/schema-000100.patch");
    expect(() =>
      productCatalogPatchPath("services/control-plane/migrations/catalog/schema-000014.json"),
    ).toThrow(/CATALOG_PATCH_PATH/);
    expect(() => readProductCatalogs(root, ["999999"])).toThrow(/PRODUCT_CATALOG_UNKNOWN/);
  });
});

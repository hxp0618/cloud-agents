import { resolve } from "node:path";

import { describe, expect, it } from "vitest";

import { formatWithOxfmt } from "./platform-oxfmt";

const root = resolve(import.meta.dirname, "../..");

describe("platform oxfmt in-process driver", () => {
  it("formats TypeScript without invoking the tinypool CLI", () => {
    expect(formatWithOxfmt(root, "sdk/typescript/src/platform.ts", "const value={a:1,b:2}\n")).toBe(
      "const value = { a: 1, b: 2 };\n",
    );
  });
});

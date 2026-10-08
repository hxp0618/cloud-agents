import { describe, expect, it } from "vitest";

import { collectPages } from "../src/pagination";

describe("Control Plane pagination", () => {
  it("collects pages and rejects a repeated continuation token", async () => {
    await expect(
      collectPages(
        async (token) =>
          token === undefined
            ? { items: ["first"], nextPageToken: "next" }
            : { items: ["second"], nextPageToken: undefined },
        "resource",
      ),
    ).resolves.toEqual(["first", "second"]);
    await expect(
      collectPages(async () => ({ items: [], nextPageToken: "repeated" }), "resource"),
    ).rejects.toThrow("repeated a resource page token");
  });
});

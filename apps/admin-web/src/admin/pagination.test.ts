import { describe, expect, it } from "vitest";

import { collectAdminPages } from "./pagination";

describe("collectAdminPages", () => {
  it("collects pages and rejects a repeated cursor", async () => {
    let calls = 0;
    await expect(
      collectAdminPages(
        async (pageToken) => {
          calls += 1;
          return pageToken === undefined
            ? { items: [1], nextPageToken: "same" }
            : { items: [2], nextPageToken: "same" };
        },
        () => new Error("repeated cursor"),
      ),
    ).rejects.toThrow("repeated cursor");
    expect(calls).toBe(2);
  });

  it("returns all items when the final page has no cursor", async () => {
    await expect(
      collectAdminPages(
        async (pageToken) =>
          pageToken === undefined
            ? { items: ["a"], nextPageToken: "next" }
            : { items: ["b"], nextPageToken: undefined },
        () => new Error("unreachable"),
      ),
    ).resolves.toEqual(["a", "b"]);
  });
});

import { describe, expect, it } from "vitest";
import type { EnvironmentProfile, Worker } from "@cloud-agents/cloud-agent-platform-sdk/platform";

import {
  keepIfUnchanged,
  replaceProfile,
  selectAdminResourceId,
  upsertAdminResource,
  workerRefreshKey,
} from "./resources";

describe("admin resources", () => {
  it("replaces by UID without mutating the input", () => {
    const resources = Object.freeze([
      { metadata: { uid: "id-1", name: "Old" } },
      { metadata: { uid: "id-2", name: "Second" } },
    ]);
    const before = [...resources];

    const result = upsertAdminResource(resources, {
      metadata: { uid: "id-1", name: "New" },
    });

    expect(resources).toEqual(before);
    expect(result.map(({ metadata }) => metadata.uid)).toEqual(["id-1", "id-2"]);
    expect(result[0]?.metadata.name).toBe("New");
    expect(Object.isFrozen(result)).toBe(true);
  });

  it("adds a resource with a missing UID", () => {
    const result = upsertAdminResource([{ metadata: { uid: "id-1", name: "First" } }], {
      metadata: { uid: "id-2", name: "Second" },
    });

    expect(result.map(({ metadata }) => metadata.uid)).toEqual(["id-1", "id-2"]);
  });

  it("orders profiles by name and descending version", () => {
    const profile = (uid: string, name: string, version: number) =>
      ({ metadata: { uid, name }, spec: { version } }) as unknown as EnvironmentProfile;

    const result = replaceProfile(
      [profile("beta-1", "Beta", 1), profile("alpha-1", "Alpha", 1), profile("beta-2", "Beta", 2)],
      profile("alpha-2", "Alpha", 2),
    );

    expect(result.map(({ metadata, spec }) => `${metadata.name}:${spec.version}`)).toEqual([
      "Alpha:2",
      "Alpha:1",
      "Beta:2",
      "Beta:1",
    ]);
  });

  it("keeps the preferred resource ID when present", () => {
    const resources = [{ metadata: { uid: "id-1" } }, { metadata: { uid: "id-2" } }];

    expect(selectAdminResourceId(resources, "id-2")).toBe("id-2");
  });

  it("selects the first ID when the preferred ID is missing", () => {
    const resources = [{ metadata: { uid: "id-1" } }, { metadata: { uid: "id-2" } }];

    expect(selectAdminResourceId(resources, "missing")).toBe("id-1");
  });

  it("returns an empty ID when no resources exist", () => {
    expect(selectAdminResourceId([], "missing")).toBe("");
  });

  it("keeps the current reference only when refreshed data is identical", () => {
    const current = Object.freeze([
      { metadata: { uid: "id-1", name: "One", resourceVersion: "1" } },
    ]);

    expect(
      keepIfUnchanged(current, [{ metadata: { uid: "id-1", name: "One", resourceVersion: "1" } }]),
    ).toBe(current);
    const changed = [{ metadata: { uid: "id-1", name: "Renamed", resourceVersion: "2" } }];
    expect(keepIfUnchanged(current, changed)).toBe(changed);
  });

  it("refreshes workers when derived health expires without a resource version change", () => {
    const worker = (state: "online" | "expired") =>
      ({
        metadata: { uid: "worker-1", resourceVersion: "1" },
        spec: {
          state: "ready",
          stableErrorCode: "",
          lastHealthAt: "2026-09-28T00:00:00Z",
          readyAt: "2026-09-28T00:00:00Z",
          health: {
            state,
            checkedAt: "2026-09-28T00:00:00Z",
            expiresAt: "2026-09-28T00:01:00Z",
          },
        },
      }) as unknown as Worker;
    const current = [worker("online")];
    const changed = [worker("expired")];

    expect(keepIfUnchanged(current, changed, workerRefreshKey)).toBe(changed);
  });
});

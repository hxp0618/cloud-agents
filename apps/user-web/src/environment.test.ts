import { describe, expect, it, vi } from "vitest";
import {
  ClientError,
  type EnvironmentProfileSummary,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

import {
  environmentErrorMessage,
  loadEnvironmentProfiles,
  readEnvironmentSelection,
  writeEnvironmentSelection,
  type EnvironmentClient,
} from "./environment";

function profile(profileId: string, name: string, version: number): EnvironmentProfileSummary {
  return { profileId, name, version } as EnvironmentProfileSummary;
}

function storage(initial: string | null = null) {
  let value = initial;
  return {
    getItem: vi.fn(() => value),
    setItem: vi.fn((_key: string, next: string) => {
      value = next;
    }),
    value: () => value,
  };
}

describe("published Environment Profiles", () => {
  it("loads every server page and orders names then newest versions", async () => {
    const listEnvironmentProfiles = vi
      .fn()
      .mockResolvedValueOnce({
        value: {
          environmentProfiles: [profile("zulu", "Zulu", 1), profile("alpha", "Alpha", 1)],
          nextPageToken: "profiles-next",
        },
      })
      .mockResolvedValueOnce({
        value: { environmentProfiles: [profile("alpha", "Alpha", 2)] },
      });
    const loaded = await loadEnvironmentProfiles(
      { listEnvironmentProfiles } as unknown as EnvironmentClient,
      "tenant-local",
      "project-alpha",
      new AbortController().signal,
    );

    expect(loaded.map(({ profileId, version }) => `${profileId}:${version}`)).toEqual([
      "alpha:2",
      "alpha:1",
      "zulu:1",
    ]);
    expect(listEnvironmentProfiles).toHaveBeenCalledTimes(2);
  });

  it("rejects a repeated server page token", async () => {
    const client = {
      listEnvironmentProfiles: vi.fn().mockResolvedValue({
        value: { environmentProfiles: [], nextPageToken: "profiles-repeated" },
      }),
    } as unknown as EnvironmentClient;

    await expect(
      loadEnvironmentProfiles(
        client,
        "tenant-local",
        "project-alpha",
        new AbortController().signal,
      ),
    ).rejects.toThrow("repeated a published Profile page token");
  });
});

describe("User Web environment recovery", () => {
  it("stores only project, Profile, and opaque environment identity", () => {
    const target = storage();
    writeEnvironmentSelection(target, {
      tenantId: "tenant-local",
      projectId: "project-alpha",
      profileId: "daily-coding",
      profileVersion: 3,
      environmentId: "environment-alpha",
    });

    expect(readEnvironmentSelection(target, "tenant-local", "project-alpha")).toEqual({
      tenantId: "tenant-local",
      projectId: "project-alpha",
      profileId: "daily-coding",
      profileVersion: 3,
      environmentId: "environment-alpha",
    });
    expect(readEnvironmentSelection(target, "tenant-local", "project-other").profileId).toBe("");
    expect(target.value()).not.toMatch(
      /credential|providerCredential|releaseDigest|cpuLimit|memoryLimit|storagePolicy|networkPolicy/i,
    );
  });

  it("distinguishes authorization and unavailable Profile conflicts", () => {
    expect(environmentErrorMessage(new ClientError("environment", 403))).toContain("cannot use");
    expect(environmentErrorMessage(new ClientError("environment", 409))).toContain(
      "no longer available",
    );
  });
});

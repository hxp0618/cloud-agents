import { describe, expect, it } from "vitest";

import {
  environmentProfileCreateRequestFrom,
  environmentProfileForm,
  isolationRuntimeForTrust,
  runtimeProfileCreateRequestFrom,
  runtimeProfileForm,
} from "../../src/app/profile-forms";

describe("environment profile form mapping", () => {
  it("derives the profile ID from the name and deduplicates selected targets", () => {
    expect(
      environmentProfileCreateRequestFrom(
        {
          ...environmentProfileForm(),
          profileName: " Development ",
          description: " local profile ",
          codex: true,
          claudeAgent: false,
          cpuLimitMillis: "1500",
          memoryLimitMiB: "256",
          storagePolicyRef: "storage-main",
          networkPolicyRef: "network-main",
          releaseDigest: ` ${"sha256:"}${"a".repeat(64)} `,
          targetRefs: ["docker-primary", "docker-primary", "ssh-overflow"],
          providerCredentialRef: " provider-default ",
        },
        2,
      ),
    ).toEqual({
      profileId: "development",
      profileName: "Development",
      version: 2,
      description: "local profile",
      providerKinds: ["codex"],
      cpuLimitMillis: 1500,
      memoryLimitBytes: 256 * 1_048_576,
      storagePolicyRef: "storage-main",
      networkPolicyRef: "network-main",
      releaseDigest: `sha256:${"a".repeat(64)}`,
      targetRefs: ["docker-primary", "ssh-overflow"],
      providerCredentialRef: "provider-default",
    });
  });
});

describe("runtime profile form mapping", () => {
  it("derives identity, isolation, digest and selectors from the draft", () => {
    const imageUri = `registry.example/runtime@sha256:${"a".repeat(64)}`;
    const request = runtimeProfileCreateRequestFrom(
      {
        ...runtimeProfileForm(),
        profileName: " Runtime Profile ",
        description: " test profile ",
        workloadTrust: "shared-untrusted",
        targetId: "pool-remote-worker:arm64",
        networkPolicyRef: "network-deny",
        imageUri: ` ${imageUri} `,
        cpuMillis: "2000",
        memoryMiB: "512",
      },
      3,
    );

    expect(isolationRuntimeForTrust("shared-untrusted")).toBe("gvisor");
    expect(isolationRuntimeForTrust("trusted-single-tenant")).toBe("runc");
    expect(request).toMatchObject({
      profileId: "runtime-profile",
      profileName: "Runtime Profile",
      version: 3,
      description: "test profile",
      workloadTrust: "shared-untrusted",
      isolationRuntime: "gvisor",
      targetSelector: {
        regionId: "region-local",
        resourcePoolId: "pool-remote-worker",
        runtime: "docker",
        architecture: "arm64",
      },
      networkPolicyRef: "network-deny",
      imageUri,
      releaseDigest: `sha256:${"a".repeat(64)}`,
      cpuMillis: 2000,
      memoryBytes: 512 * 1_048_576,
    });
    expect(request).not.toHaveProperty("targetId");
  });
});

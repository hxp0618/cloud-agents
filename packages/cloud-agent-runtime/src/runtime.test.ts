import { describe, expect, it } from "vitest";

import { CLOUD_AGENT_CAPABILITY_IDS } from "@synara/cloud-agent-protocol";
import {
  CLOUD_AGENT_PROVIDER_PLUGIN_ABI_VERSION,
  type CloudAgentProviderDescriptor,
  type CloudAgentProviderPluginV1,
} from "@synara/cloud-agent-provider-api";

import { createCloudAgentRuntime } from "./runtime";

function provider(providerKind: string): CloudAgentProviderPluginV1 {
  const capabilities = Object.fromEntries(
    CLOUD_AGENT_CAPABILITY_IDS.map((capability) => [capability, "unsupported"]),
  ) as CloudAgentProviderDescriptor["capabilities"];
  return {
    abiVersion: CLOUD_AGENT_PROVIDER_PLUGIN_ABI_VERSION,
    providerKind,
    describe: async () => ({
      abiVersion: CLOUD_AGENT_PROVIDER_PLUGIN_ABI_VERSION,
      providerKind,
      displayName: providerKind,
      adapterVersion: "test-v1",
      runtime: {
        kind: "local",
        name: "test",
        available: true,
        compatible: true,
        compatibleRange: { minimumInclusive: "0.0.0" },
      },
      capabilities,
    }),
    createSession: async () => {
      throw new Error("not used");
    },
  };
}

describe("createCloudAgentRuntime", () => {
  it("registers providers explicitly and deterministically", () => {
    const runtime = createCloudAgentRuntime({ providers: [provider("codex"), provider("claude")] });
    expect(runtime.providerKinds).toEqual(["claude", "codex"]);
  });

  it("rejects duplicate providers", () => {
    expect(() =>
      createCloudAgentRuntime({ providers: [provider("codex"), provider("codex")] }),
    ).toThrow(/registered more than once/u);
  });

  it("rejects malformed provider descriptors at the runtime boundary", async () => {
    const runtime = createCloudAgentRuntime({
      providers: [
        {
          ...provider("codex"),
          describe: async () => ({
            ...(await provider("codex").describe()),
            capabilities: {} as CloudAgentProviderDescriptor["capabilities"],
          }),
        },
      ],
    });

    await expect(runtime.describe("codex")).rejects.toThrow(/complete capability map/u);
  });

  it("rejects a descriptor for a different registered provider", async () => {
    const runtime = createCloudAgentRuntime({
      providers: [
        {
          ...provider("codex"),
          describe: async () => ({
            ...(await provider("codex").describe()),
            providerKind: "claude",
          }),
        },
      ],
    });

    await expect(runtime.describe("codex")).rejects.toThrow(
      /descriptor identity claude does not match codex/u,
    );
  });
});

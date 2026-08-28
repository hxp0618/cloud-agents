import { describe, expect, it } from "vitest";

import {
  CLOUD_AGENT_CAPABILITY_IDS,
  type CloudAgentCommandEnvelope,
} from "@synara/cloud-agent-protocol";
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

  it("rejects non-canonical provider identities at registration", () => {
    expect(() => createCloudAgentRuntime({ providers: [provider(" codex ")] })).toThrow(
      /canonical spelling/u,
    );
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

  it("rejects malformed provider sessions at the runtime boundary", async () => {
    const runtime = createCloudAgentRuntime({
      providers: [
        {
          ...provider("codex"),
          createSession: async () => ({
            sessionId: "",
            events: emptyEvents(),
            execute: async () => validMessage(),
            close: async () => undefined,
            async [Symbol.asyncDispose]() {},
          }),
        },
      ],
    });

    await expect(runtime.createSession("codex", sessionInput(), {} as never)).rejects.toThrow(
      /sessionId/u,
    );
  });

  it("validates commands, results, and events from provider sessions", async () => {
    const runtime = createCloudAgentRuntime({
      providers: [
        {
          ...provider("codex"),
          createSession: async () => ({
            sessionId: "session-1",
            events: {
              async *[Symbol.asyncIterator]() {
                yield {} as never;
              },
            },
            execute: async () => ({}) as never,
            close: async () => undefined,
            async [Symbol.asyncDispose]() {},
          }),
        },
      ],
    });
    const session = await runtime.createSession("codex", sessionInput(), {} as never);

    await expect(session.execute({} as never)).rejects.toThrow(/command envelope/u);
    await expect(session.execute(validCommand())).rejects.toThrow(/message envelope/u);
    await expect(session.events[Symbol.asyncIterator]().next()).rejects.toThrow(
      /message envelope/u,
    );
    await session.close();
  });
});

function sessionInput() {
  return {
    hostInstanceId: "host-1",
    hostThreadId: "thread-1",
    configuration: {},
  };
}

function validCommand(): CloudAgentCommandEnvelope {
  return {
    requestId: "request-1",
    protocolVersion: { major: 2, minor: 3 },
    executionId: "execution-1",
    generation: 1,
    commandType: "Describe",
    commandId: "command-1",
    occurredAt: "2026-08-28T00:00:00.000Z",
    payload: {},
  };
}

function validMessage() {
  return {
    ...validCommand(),
    messageType: "Result" as const,
    payload: {},
  };
}

function emptyEvents() {
  return {
    async *[Symbol.asyncIterator]() {},
  };
}

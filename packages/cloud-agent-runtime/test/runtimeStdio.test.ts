import {
  closeSync,
  existsSync,
  lstatSync,
  mkdtempSync,
  openSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { Readable, Writable } from "node:stream";

import { CLOUD_AGENT_CAPABILITY_IDS } from "@cloud-agents/cloud-agent-protocol";
import {
  CLOUD_AGENT_PROVIDER_PLUGIN_ABI_VERSION,
  type CloudAgentWorkspaceBinding,
  type CloudAgentProviderDescriptor,
} from "@cloud-agents/cloud-agent-provider-api";
import { describe, expect, it, vi } from "vitest";

import { createCloudAgentRuntime } from "../src/runtime";
import { runCloudAgentRuntimeStdio } from "../src/runtimeStdio";
import * as skillSandboxModule from "../src/skillSandbox";

const describeCommand = {
  requestId: "request-describe",
  protocolVersion: { major: 2, minor: 3 },
  executionId: "execution-describe",
  generation: 1,
  commandType: "Describe" as const,
  commandId: "command-describe",
  occurredAt: "2026-08-09T00:00:00.000Z",
  payload: { provider: "codex" },
};

const startCommand = {
  ...describeCommand,
  requestId: "request-start",
  executionId: "execution-start",
  commandType: "StartSession" as const,
  commandId: "command-start",
  payload: {
    runnerInput: {
      execution: { id: "thread-start" },
      workload: { provider: "codex", inputText: "" },
      workspaceDirectory: "/tmp/cloud-agent-runtime-stdio-test",
    },
  },
};

const sendCommand = {
  ...startCommand,
  requestId: "request-send",
  commandType: "SendTurn" as const,
  commandId: "command-send",
  payload: { inputText: "hello" },
};

describe("runCloudAgentRuntimeStdio", () => {
  it("consumes every Protocol 2.2 and 2.3 golden command through the stdio decoder", async () => {
    const fixtures = [
      ...readGoldenCommands("v2.2/commands.jsonl"),
      ...readGoldenCommands("v2.3/commands.jsonl"),
    ];

    for (const fixture of fixtures) {
      const output = captureOutput();
      await expect(
        runCloudAgentRuntimeStdio(fakeRuntime(), {
          source: Readable.from([`${JSON.stringify(fixture.frame)}\n`]),
          output: output.stream,
          diagnostics: captureOutput().stream,
        }),
        fixture.id,
      ).resolves.toBeUndefined();
      expect(JSON.parse(output.text()), fixture.id).toMatchObject({
        commandId: fixture.frame.commandId,
      });
    }
    expect(fixtures).toHaveLength(29);
  });

  it("routes Describe through the explicit Plugin registry", async () => {
    const output = captureOutput();
    await runCloudAgentRuntimeStdio(fakeRuntime(), {
      source: Readable.from([`${JSON.stringify(describeCommand)}\n`]),
      output: output.stream,
      allowedProviders: ["codex"],
    });

    expect(JSON.parse(output.text())).toMatchObject({
      messageType: "Result",
      commandId: "command-describe",
      payload: { descriptor: { providerKind: "codex", displayName: "Codex" } },
    });
  });

  it("preserves inherited Skill roots when the default environment aliases process.env", async () => {
    const root = mkdtempSync(join(tmpdir(), "cloud-agent-runtime-sandbox-env-"));
    const materializationPath = join(root, "materialization.json");
    const materializationFd = openSync(materializationPath, "w+");
    const skillRoot = join(root, "skill-1");
    const digest = `sha256:${"a".repeat(64)}` as const;
    const manifest = {
      version: 1,
      bindings: [
        {
          resourceKind: "skill-bundle" as const,
          resourceId: "skill-1",
          version: "v1",
          digest,
          grantId: "grant-1",
          expiresAtUnixSeconds: Math.floor(Date.now() / 1000) + 300,
          permissions: [],
          readOnly: true,
        },
      ],
    };
    writeFileSync(
      materializationPath,
      JSON.stringify({
        version: 1,
        mcp: [],
        skills: [
          {
            resourceId: "skill-1",
            version: "v1",
            digest,
            bundle: "eA",
            signature: "eA",
            publicKey: "eA",
            signingKeyId: "key-1",
          },
        ],
      }),
    );
    const names = [
      "CLOUD_AGENT_CAPABILITY_MANIFEST_B64",
      "CLOUD_AGENT_CAPABILITY_MATERIALIZATION_FD",
      "CLOUD_AGENT_SKILL_BUNDLE_SKILL_1_ROOT",
    ] as const;
    const previous = new Map(names.map((name) => [name, process.env[name]]));
    process.env.CLOUD_AGENT_CAPABILITY_MANIFEST_B64 = Buffer.from(
      JSON.stringify(manifest),
    ).toString("base64url");
    process.env.CLOUD_AGENT_CAPABILITY_MATERIALIZATION_FD = String(materializationFd);
    process.env.CLOUD_AGENT_SKILL_BUNDLE_SKILL_1_ROOT = skillRoot;
    const verify = vi.spyOn(skillSandboxModule, "verifyManagedSkillSandbox").mockReturnValue({
      version: 1,
      skillRoots: [skillRoot],
      writeRoots: [root],
      scratchRoot: root,
    });
    let observedSkillRoot: string | undefined;
    const runtime = createCloudAgentRuntime({
      providers: [
        {
          ...testProviderDescriptor(),
          async describe() {
            observedSkillRoot = process.env.CLOUD_AGENT_SKILL_BUNDLE_SKILL_1_ROOT;
            return await fakeRuntime().describe("codex");
          },
          async createSession() {
            throw new Error("Describe must not create a Provider Session.");
          },
        },
      ],
    });
    try {
      await runCloudAgentRuntimeStdio(runtime, {
        source: Readable.from([`${JSON.stringify(describeCommand)}\n`]),
        output: captureOutput().stream,
      });
      expect(observedSkillRoot).toBe(skillRoot);
      expect(process.env.CLOUD_AGENT_SKILL_BUNDLE_SKILL_1_ROOT).toBe(skillRoot);
    } finally {
      verify.mockRestore();
      closeSync(materializationFd);
      rmSync(root, { recursive: true, force: true });
      for (const [name, value] of previous) {
        if (value === undefined) delete process.env[name];
        else process.env[name] = value;
      }
    }
  });

  it("fails closed when a registered Provider is absent from the runtime allowlist", async () => {
    const output = captureOutput();
    await runCloudAgentRuntimeStdio(fakeRuntime(), {
      source: Readable.from([`${JSON.stringify(describeCommand)}\n`]),
      output: output.stream,
      allowedProviders: [],
    });

    expect(JSON.parse(output.text())).toMatchObject({
      messageType: "Error",
      error: { code: "provider_not_installed", message: "Provider codex is disabled." },
    });
  });

  it.each<
    [
      string,
      {
        readonly allowedProviders?: ReadonlyArray<string>;
        readonly environment?: Readonly<Record<string, string>>;
      },
      RegExp,
    ]
  >([
    ["the Provider allowlist", { allowedProviders: ["missing"] }, /Allowed Provider missing/u],
    [
      "the credential descriptor",
      { environment: { CLOUD_AGENT_PROVIDER_CREDENTIAL_FD: "invalid" } },
      /CLOUD_AGENT_PROVIDER_CREDENTIAL_FD is invalid/u,
    ],
  ])("validates %s before starting capability resources", async (_name, options, message) => {
    const root = mkdtempSync(join(tmpdir(), "cloud-agent-runtime-stdio-setup-"));
    const materializationPath = join(root, "materialization.json");
    const materializationFd = openSync(materializationPath, "w+");
    const previousBrokerUrl = process.env.CLOUD_AGENT_MCP_BROKER_URL;
    process.env.CLOUD_AGENT_MCP_BROKER_URL = "sentinel";
    try {
      writeFileSync(materializationPath, JSON.stringify(capabilityMaterialization()));
      const environment = {
        ...capabilityEnvironment(),
        CLOUD_AGENT_CAPABILITY_MATERIALIZATION_FD: String(materializationFd),
        ...options.environment,
      };
      await expect(
        runCloudAgentRuntimeStdio(fakeRuntime(), {
          source: Readable.from([]),
          output: captureOutput().stream,
          ...(options.allowedProviders === undefined
            ? {}
            : { allowedProviders: options.allowedProviders }),
          environment,
        }),
      ).rejects.toThrow(message);
      expect(process.env.CLOUD_AGENT_MCP_BROKER_URL).toBe("sentinel");
    } finally {
      closeSync(materializationFd);
      rmSync(root, { recursive: true, force: true });
      if (previousBrokerUrl === undefined) delete process.env.CLOUD_AGENT_MCP_BROKER_URL;
      else process.env.CLOUD_AGENT_MCP_BROKER_URL = previousBrokerUrl;
    }
  });

  it("continues after a StartSession rejection and closes both sessions", async () => {
    const output = captureOutput();
    let createCount = 0;
    let firstStartSeen: (() => void) | undefined;
    let secondStartSeen: (() => void) | undefined;
    const firstStart = new Promise<void>((resolve) => {
      firstStartSeen = resolve;
    });
    const secondStart = new Promise<void>((resolve) => {
      secondStartSeen = resolve;
    });
    let closes = 0;
    const executed: string[] = [];
    const secondStartCommand = {
      ...startCommand,
      requestId: "request-start-2",
      executionId: "execution-start-2",
      commandId: "command-start-2",
    };
    const runtime = runtimeWithProvider(async () => {
      createCount += 1;
      return testSession(
        `session-command-${createCount}`,
        executed,
        () => {
          closes += 1;
        },
        (commandType) => {
          if (commandType !== "StartSession") return;
          if (createCount === 1) {
            firstStartSeen?.();
            throw new Error("Provider command failed.");
          }
          secondStartSeen?.();
        },
      );
    });
    async function* source() {
      yield `${JSON.stringify(startCommand)}\n`;
      await firstStart;
      yield `${JSON.stringify(describeCommand)}\n${JSON.stringify(secondStartCommand)}\n`;
      await secondStart;
    }

    await runCloudAgentRuntimeStdio(runtime, {
      source: Readable.from(source()),
      output: output.stream,
    });

    expect(executed).toEqual(["StartSession", "StartSession"]);
    expect(createCount).toBe(2);
    expect(closes).toBe(2);
    expect(output.text()).toContain('"commandId":"command-start"');
    expect(output.text()).toContain('"commandId":"command-describe"');
    expect(output.text()).toContain('"messageType":"Error"');
  });

  it("prepares isolated workspace, output, and provider state roots", async () => {
    const root = mkdtempSync(join(tmpdir(), "cloud-agent-runtime-binding-"));
    const workspace = join(root, "workspace");
    const runtimeOutput = join(root, "runtime-output");
    const providerState = join(root, "provider-state");
    let observed: CloudAgentWorkspaceBinding | undefined;
    try {
      const output = captureOutput();
      await runCloudAgentRuntimeStdio(
        runtimeWithProvider(async (_input, host) => {
          observed = host.workspace;
          return testSession("binding", [], () => undefined);
        }),
        {
          source: Readable.from([
            `${JSON.stringify({
              ...startCommand,
              payload: {
                runnerInput: {
                  ...startCommand.payload.runnerInput,
                  workspaceDirectory: workspace,
                  runtimeOutputDirectory: runtimeOutput,
                  providerStateDirectory: providerState,
                },
              },
            })}\n`,
          ]),
          output: output.stream,
        },
      );
      expect(observed).toEqual({
        authority: "host",
        root: resolve(workspace),
        runtimeOutputRoot: resolve(runtimeOutput),
        providerStateRoot: resolve(providerState),
        generation: 1,
        readOnly: false,
      });
      expect(existsSync(workspace)).toBe(true);
      expect(existsSync(runtimeOutput)).toBe(true);
      expect(existsSync(providerState)).toBe(true);
      expect(lstatSync(workspace).isDirectory()).toBe(true);
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });

  it("bounds raw stdin before a newline is received", async () => {
    const output = captureOutput();
    await expect(
      runCloudAgentRuntimeStdio(fakeRuntime(), {
        source: Readable.from([Buffer.alloc(2 * 1024 * 1024 + 1, 0x78)]),
        output: output.stream,
      }),
    ).rejects.toThrow("exceeds the negotiated size limit");
    expect(output.text()).toBe("");
  });

  it("fails closed on invalid UTF-8 instead of accepting replacement characters", async () => {
    const output = captureOutput();
    await expect(
      runCloudAgentRuntimeStdio(fakeRuntime(), {
        source: Readable.from([Buffer.from([0x7b, 0xc3, 0x28, 0x7d, 0x0a])]),
        output: output.stream,
      }),
    ).rejects.toThrow("not valid UTF-8");
    expect(output.text()).toBe("");
  });

  it("terminates the stream after malformed input instead of processing later commands", async () => {
    const output = captureOutput();
    await expect(
      runCloudAgentRuntimeStdio(fakeRuntime(), {
        source: Readable.from([`{malformed}\n${JSON.stringify(describeCommand)}\n`]),
        output: output.stream,
      }),
    ).rejects.toThrow();
    expect(output.text()).toBe("");
  });

  it.each([
    ["negative generation", { generation: -1 }],
    ["zero generation", { generation: 0 }],
    ["fractional generation", { generation: 1.5 }],
    ["negative protocol minor", { protocolVersion: { major: 2, minor: -1 } }],
    ["fractional protocol minor", { protocolVersion: { major: 2, minor: 2.5 } }],
    ["empty request id", { requestId: "" }],
    ["empty execution id", { executionId: "" }],
    ["empty command id", { commandId: "" }],
    ["empty timestamp", { occurredAt: "" }],
  ])("rejects %s before dispatch", async (_name, patch) => {
    const output = captureOutput();
    await expect(
      runCloudAgentRuntimeStdio(fakeRuntime(), {
        source: Readable.from([`${JSON.stringify({ ...describeCommand, ...patch })}\n`]),
        output: output.stream,
      }),
    ).rejects.toThrow("Command does not match Protocol v2.");
    expect(output.text()).toBe("");
  });

  it("propagates a rejecting event stream immediately and closes its session", async () => {
    const eventFailure = new Error("Provider event stream failed.");
    const unhandled: unknown[] = [];
    const onUnhandled = (cause: unknown) => unhandled.push(cause);
    process.on("unhandledRejection", onUnhandled);
    const source = new Readable({ read() {} });
    source.push(`${JSON.stringify(startCommand)}\n`);
    let describeCalls = 0;
    let closes = 0;
    const session = {
      sessionId: "session-rejecting-events",
      events: {
        [Symbol.asyncIterator]() {
          return {
            async next(): Promise<IteratorResult<never>> {
              throw eventFailure;
            },
          };
        },
      },
      async execute(command: typeof startCommand) {
        return terminalForCommand(command);
      },
      async close() {
        closes += 1;
      },
      async [Symbol.asyncDispose]() {
        await this.close();
      },
    };
    const runtime = createCloudAgentRuntime({
      providers: [
        {
          ...testProviderDescriptor(),
          async describe() {
            describeCalls += 1;
            return await fakeRuntime().describe("codex");
          },
          async createSession() {
            return session;
          },
        },
      ],
    });

    try {
      const running = runCloudAgentRuntimeStdio(runtime, {
        source,
        output: captureOutput().stream,
      });
      setImmediate(() => {
        if (!source.destroyed) source.push(`${JSON.stringify(describeCommand)}\n`);
      });

      await expect(running).rejects.toBe(eventFailure);
      await new Promise<void>((resolve) => setImmediate(resolve));
      expect(source.destroyed).toBe(true);
      expect(describeCalls).toBe(0);
      expect(closes).toBe(1);
      expect(unhandled).toEqual([]);
    } finally {
      process.off("unhandledRejection", onUnhandled);
    }
  });

  it("propagates the original failure from a backpressured output and closes sessions", async () => {
    const outputFailure = new Error("Provider Host stdout write failed.");
    const unhandled: unknown[] = [];
    const onUnhandled = (cause: unknown) => unhandled.push(cause);
    process.on("unhandledRejection", onUnhandled);
    const source = new Readable({ read() {} });
    source.push(`${JSON.stringify(startCommand)}\n`);
    let closes = 0;
    let eventsClosed: (() => void) | undefined;
    const closed = new Promise<void>((resolve) => {
      eventsClosed = resolve;
    });
    const progress = {
      ...terminalForCommand(startCommand),
      messageType: "Progress" as const,
      payload: { phase: "started" },
    };
    let emitted = false;
    const closeFailingOutputSession = async () => {
      closes += 1;
      eventsClosed?.();
    };
    const runtime = runtimeWithProvider(async () => ({
      sessionId: "session-failing-output",
      events: {
        [Symbol.asyncIterator]() {
          return {
            async next() {
              if (!emitted) {
                emitted = true;
                return { value: progress, done: false } as const;
              }
              await closed;
              return { value: undefined, done: true } as const;
            },
          };
        },
      },
      async execute(command) {
        return terminalForCommand(command);
      },
      close: closeFailingOutputSession,
      async [Symbol.asyncDispose]() {
        await closeFailingOutputSession();
      },
    }));
    const output = new Writable({
      highWaterMark: 1,
      write(_chunk, _encoding, callback) {
        setImmediate(() => callback(outputFailure));
      },
    });

    try {
      await expect(runCloudAgentRuntimeStdio(runtime, { source, output })).rejects.toBe(
        outputFailure,
      );
      await new Promise<void>((resolve) => setImmediate(resolve));
      expect(source.destroyed).toBe(true);
      expect(closes).toBe(1);
      expect(unhandled).toEqual([]);
    } finally {
      process.off("unhandledRejection", onUnhandled);
    }
  });

  it("makes a delayed StartSession the admission barrier for a back-to-back SendTurn", async () => {
    let allowCreate: (() => void) | undefined;
    const createAllowed = new Promise<void>((resolve) => {
      allowCreate = resolve;
    });
    const executed: string[] = [];
    let closes = 0;
    let signalSend: (() => void) | undefined;
    const sendSeen = new Promise<void>((resolve) => {
      signalSend = resolve;
    });
    let endSource: (() => void) | undefined;
    const sourceEnded = new Promise<void>((resolve) => {
      endSource = resolve;
    });
    const runtime = runtimeWithProvider(async () => {
      await createAllowed;
      return testSession(
        "delayed",
        executed,
        () => {
          closes += 1;
        },
        (commandType) => {
          if (commandType === "SendTurn") signalSend?.();
        },
      );
    });
    async function* source() {
      yield `${JSON.stringify(startCommand)}\n${JSON.stringify(sendCommand)}\n`;
      await sourceEnded;
    }
    const running = runCloudAgentRuntimeStdio(runtime, {
      source: Readable.from(source()),
      output: captureOutput().stream,
    });

    await new Promise<void>((resolve) => setImmediate(resolve));
    expect(executed).toEqual([]);
    allowCreate?.();
    await sendSeen;
    endSource?.();
    await running;
    expect(executed).toEqual(["StartSession", "SendTurn"]);
    expect(closes).toBe(1);
  });

  it("serializes two StartSession replacements without leaking either session", async () => {
    const executed: string[] = [];
    const closed: string[] = [];
    let createsInFlight = 0;
    let maximumCreatesInFlight = 0;
    let sessionNumber = 0;
    let signalSecondStart: (() => void) | undefined;
    const secondStartSeen = new Promise<void>((resolve) => {
      signalSecondStart = resolve;
    });
    let endSource: (() => void) | undefined;
    const sourceEnded = new Promise<void>((resolve) => {
      endSource = resolve;
    });
    const runtime = runtimeWithProvider(async () => {
      createsInFlight += 1;
      maximumCreatesInFlight = Math.max(maximumCreatesInFlight, createsInFlight);
      await Promise.resolve();
      createsInFlight -= 1;
      sessionNumber += 1;
      const id = `session-${sessionNumber}`;
      return testSession(
        id,
        executed,
        () => closed.push(id),
        (commandType) => {
          if (id === "session-2" && commandType === "StartSession") signalSecondStart?.();
        },
      );
    });
    const replacement = {
      ...startCommand,
      requestId: "request-start-2",
      commandId: "command-start-2",
      generation: 2,
    };

    async function* source() {
      yield `${JSON.stringify(startCommand)}\n${JSON.stringify(replacement)}\n`;
      await sourceEnded;
    }
    const running = runCloudAgentRuntimeStdio(runtime, {
      source: Readable.from(source()),
      output: captureOutput().stream,
    });
    await secondStartSeen;
    endSource?.();
    await running;

    expect(maximumCreatesInFlight).toBe(1);
    expect(executed).toEqual(["StartSession", "StartSession"]);
    expect(closed).toEqual(["session-1", "session-2"]);
  });

  it.each(["StopSession", "replacement"] as const)(
    "fails closed when %s cannot close the active Provider Session",
    async (trigger) => {
      const cleanupFailure = new Error("Provider Session cleanup failed.");
      const source = new Readable({ read() {} });
      let signalStarted: (() => void) | undefined;
      const started = new Promise<void>((resolve) => {
        signalStarted = resolve;
      });
      let creates = 0;
      let closes = 0;
      const runtime = runtimeWithProvider(async () => {
        creates += 1;
        return {
          sessionId: `cleanup-failure-${creates}`,
          events: {
            async *[Symbol.asyncIterator]() {
              yield* [];
            },
          },
          async execute(command) {
            if (command.commandType === "StartSession") signalStarted?.();
            return terminalForCommand(command);
          },
          async close() {
            closes += 1;
            throw cleanupFailure;
          },
          async [Symbol.asyncDispose]() {
            throw cleanupFailure;
          },
        };
      });
      const running = runCloudAgentRuntimeStdio(runtime, {
        source,
        output: captureOutput().stream,
        shutdownCleanupTimeoutMs: 25,
      });
      source.push(`${JSON.stringify(startCommand)}\n`);
      await started;
      const next =
        trigger === "StopSession"
          ? {
              ...startCommand,
              requestId: "request-stop-cleanup-failure",
              commandId: "command-stop-cleanup-failure",
              commandType: "StopSession" as const,
              payload: {},
            }
          : {
              ...startCommand,
              requestId: "request-replace-cleanup-failure",
              commandId: "command-replace-cleanup-failure",
              generation: 2,
            };
      source.push(`${JSON.stringify(next)}\n`);

      await expect(running).rejects.toThrow("Provider Session cleanup failed.");
      expect(creates).toBe(1);
      expect(closes).toBe(1);
    },
  );

  it.each(["stdin close", "fatal input"] as const)(
    "does not report a clean Runtime shutdown after Provider cleanup rejects on %s",
    async (trigger) => {
      const cleanupFailure = new Error("Provider Session shutdown cleanup failed.");
      let signalStarted: (() => void) | undefined;
      const started = new Promise<void>((resolve) => {
        signalStarted = resolve;
      });
      let closes = 0;
      const source = new Readable({ read() {} });
      const runtime = runtimeWithProvider(async () => ({
        sessionId: "shutdown-cleanup-failure",
        events: {
          async *[Symbol.asyncIterator]() {
            yield* [];
          },
        },
        async execute(command) {
          signalStarted?.();
          return terminalForCommand(command);
        },
        async close() {
          closes += 1;
          throw cleanupFailure;
        },
        async [Symbol.asyncDispose]() {
          throw cleanupFailure;
        },
      }));
      const running = runCloudAgentRuntimeStdio(runtime, {
        source,
        output: captureOutput().stream,
        shutdownCleanupTimeoutMs: 25,
      });
      source.push(`${JSON.stringify(startCommand)}\n`);
      await started;
      if (trigger === "stdin close") source.push(null);
      else source.push("{malformed}\n");

      if (trigger === "stdin close") await expect(running).rejects.toThrow(cleanupFailure.message);
      else await expect(running).rejects.toThrow("Expected property name");
      expect(closes).toBe(1);
    },
  );

  it.each(["close", "event stream"] as const)(
    "bounds the shared Provider Session cleanup budget when %s never settles",
    async (stalled) => {
      const unhandled: unknown[] = [];
      const onUnhandled = (cause: unknown) => unhandled.push(cause);
      process.on("unhandledRejection", onUnhandled);
      let signalStarted: (() => void) | undefined;
      const started = new Promise<void>((resolve) => {
        signalStarted = resolve;
      });
      let rejectClose: ((cause: Error) => void) | undefined;
      const source = new Readable({ read() {} });
      const runtime = runtimeWithProvider(async () => ({
        sessionId: `stalled-${stalled}`,
        events: {
          async *[Symbol.asyncIterator]() {
            if (stalled === "event stream") await new Promise<void>(() => undefined);
          },
        },
        async execute(command) {
          signalStarted?.();
          return terminalForCommand(command);
        },
        async close() {
          if (stalled === "close") {
            await new Promise<void>((_resolve, reject) => {
              rejectClose = reject;
            });
          }
        },
        async [Symbol.asyncDispose]() {
          // Runtime stdio owns this regression through close().
        },
      }));
      try {
        const running = runCloudAgentRuntimeStdio(runtime, {
          source,
          output: captureOutput().stream,
          shutdownCleanupTimeoutMs: 10,
        });
        source.push(`${JSON.stringify(startCommand)}\n`);
        await started;
        source.push(null);

        await expect(running).rejects.toThrow("Provider Session cleanup timed out after 10ms.");
        rejectClose?.(new Error("late Provider close failure"));
        await new Promise<void>((resolve) => setImmediate(resolve));
        expect(unhandled).toEqual([]);
      } finally {
        process.off("unhandledRejection", onUnhandled);
      }
    },
  );

  it("closes a session whose delayed creation completes after fatal input", async () => {
    let allowCreate: (() => void) | undefined;
    const createAllowed = new Promise<void>((resolve) => {
      allowCreate = resolve;
    });
    const executed: string[] = [];
    let closes = 0;
    let signalLateClose: (() => void) | undefined;
    const lateClosed = new Promise<void>((resolve) => {
      signalLateClose = resolve;
    });
    let signalShutdown: (() => void) | undefined;
    const shutdownSeen = new Promise<void>((resolve) => {
      signalShutdown = resolve;
    });
    const runtime = runtimeWithProvider(async (_input, _host, signal) => {
      signal?.addEventListener("abort", () => signalShutdown?.(), { once: true });
      await createAllowed;
      return testSession("late", executed, () => {
        closes += 1;
        signalLateClose?.();
      });
    });
    async function* source() {
      yield `${JSON.stringify(startCommand)}\n`;
      await new Promise<void>((resolve) => setImmediate(resolve));
      yield "{malformed}\n";
    }
    let signalFatal: (() => void) | undefined;
    const fatalSeen = new Promise<void>((resolve) => {
      signalFatal = resolve;
    });
    const diagnostics = new Writable({
      write(_chunk, _encoding, callback) {
        signalFatal?.();
        callback();
      },
    });
    const running = runCloudAgentRuntimeStdio(runtime, {
      source: Readable.from(source()),
      output: captureOutput().stream,
      diagnostics,
    });

    await fatalSeen;
    await shutdownSeen;
    allowCreate?.();
    await expect(running).rejects.toThrow();
    await lateClosed;
    expect(executed).toEqual([]);
    expect(closes).toBe(1);
  });

  it.each(["settles", "rejects"] as const)(
    "waits for late session close and fails when cleanup %s",
    async (outcome) => {
      let signalCreateStarted: (() => void) | undefined;
      let signalShutdown: (() => void) | undefined;
      let signalCloseStarted: (() => void) | undefined;
      const createStarted = new Promise<void>((resolve) => {
        signalCreateStarted = resolve;
      });
      const shutdownSeen = new Promise<void>((resolve) => {
        signalShutdown = resolve;
      });
      const closeStarted = new Promise<void>((resolve) => {
        signalCloseStarted = resolve;
      });
      let allowCreate: (() => void) | undefined;
      const createAllowed = new Promise<void>((resolve) => {
        allowCreate = resolve;
      });
      let allowClose: (() => void) | undefined;
      const closeAllowed = new Promise<void>((resolve) => {
        allowClose = resolve;
      });
      let closes = 0;
      const lateSession = {
        sessionId: "late-cleanup",
        events: {
          async *[Symbol.asyncIterator]() {
            yield* [];
          },
        },
        async execute() {
          throw new Error("late session must not execute");
        },
        async close() {
          closes += 1;
          signalCloseStarted?.();
          if (outcome === "rejects") throw new Error("late Provider Session cleanup failed");
          await closeAllowed;
        },
        async [Symbol.asyncDispose]() {
          await this.close();
        },
      };
      const runtime = runtimeWithProvider(async (_input, _host, signal) => {
        signalCreateStarted?.();
        signal?.addEventListener("abort", () => signalShutdown?.(), { once: true });
        await createAllowed;
        return lateSession;
      });
      async function* source() {
        yield `${JSON.stringify(startCommand)}\n`;
        await createStarted;
      }
      const running = runCloudAgentRuntimeStdio(runtime, {
        source: Readable.from(source()),
        output: captureOutput().stream,
        shutdownCleanupTimeoutMs: 1_000,
      });

      await createStarted;
      await shutdownSeen;
      allowCreate?.();
      await closeStarted;
      if (outcome === "settles") {
        let settled = false;
        void running.then(
          () => {
            settled = true;
          },
          () => {
            settled = true;
          },
        );
        await Promise.resolve();
        expect(settled).toBe(false);
        allowClose?.();
        await running;
      } else {
        await expect(running).rejects.toThrow("late Provider Session cleanup failed");
      }
      expect(closes).toBe(1);
    },
  );

  it("fails after a bounded timeout when late session creation never settles", async () => {
    let signalCreateStarted: (() => void) | undefined;
    let signalShutdown: (() => void) | undefined;
    const createStarted = new Promise<void>((resolve) => {
      signalCreateStarted = resolve;
    });
    const shutdownSeen = new Promise<void>((resolve) => {
      signalShutdown = resolve;
    });
    const runtime = runtimeWithProvider(async (_input, _host, signal) => {
      signalCreateStarted?.();
      signal?.addEventListener("abort", () => signalShutdown?.(), { once: true });
      await new Promise<void>(() => undefined);
      throw new Error("unreachable");
    });
    async function* source() {
      yield `${JSON.stringify(startCommand)}\n`;
      await createStarted;
    }
    const output = captureOutput();
    const diagnostics = captureOutput();
    const running = runCloudAgentRuntimeStdio(runtime, {
      source: Readable.from(source()),
      output: output.stream,
      diagnostics: diagnostics.stream,
      shutdownCleanupTimeoutMs: 10,
    });

    await shutdownSeen;
    await expect(running).rejects.toThrow("Provider Session cleanup timed out");
    expect(diagnostics.text()).toContain("Provider Session cleanup timed out");
  });

  it("binds credential acquisition and consumes the inherited FD only once", async () => {
    const validationFailures: string[] = [];
    let sessionNumber = 0;
    const runtime = createCloudAgentRuntime({
      providers: [
        {
          ...testProviderDescriptor(),
          async createSession(_input, host) {
            if (sessionNumber === 0) {
              for (const request of [
                { providerKind: "other", operation: "session", generation: 1 },
                { providerKind: "codex", operation: "turn", generation: 1 },
                { providerKind: "codex", operation: "session", generation: 2 },
              ]) {
                try {
                  await host.credential.acquire(request);
                } catch (cause) {
                  validationFailures.push(cause instanceof Error ? cause.message : String(cause));
                }
              }
            }
            try {
              await host.credential.acquire({
                providerKind: "codex",
                operation: "session",
                generation: host.workspace.generation,
              });
            } catch (cause) {
              validationFailures.push(cause instanceof Error ? cause.message : String(cause));
              throw cause;
            }
            sessionNumber += 1;
            return testSession(`credential-${sessionNumber}`, [], () => undefined);
          },
        },
      ],
    });
    const replacement = {
      ...startCommand,
      requestId: "request-credential-2",
      commandId: "command-credential-2",
      generation: 2,
    };
    const output = captureOutput();
    let endSource: (() => void) | undefined;
    const sourceEnded = new Promise<void>((resolve) => {
      endSource = resolve;
    });
    async function* source() {
      yield `${JSON.stringify(startCommand)}\n${JSON.stringify(replacement)}\n`;
      await sourceEnded;
    }
    const running = runCloudAgentRuntimeStdio(runtime, {
      source: Readable.from(source()),
      output: output.stream,
      environment: { CLOUD_AGENT_PROVIDER_CREDENTIAL_FD: "3" },
    });
    while (validationFailures.length < 4) {
      await new Promise<void>((resolve) => setImmediate(resolve));
    }
    endSource?.();
    await running;

    expect(validationFailures).toEqual([
      "Credential request does not match the active Provider session binding.",
      "Credential request does not match the active Provider session binding.",
      "Credential request does not match the active Provider session binding.",
      "Runtime credential descriptor has already been consumed.",
    ]);
    expect(output.text()).toContain("Runtime credential descriptor has already been consumed.");
  });

  it("closes an active Provider before waiting for a command blocked on execute", async () => {
    let closeCalled = false;
    let resolveExecute:
      | ((message: {
          messageType: "Result";
          requestId: string;
          protocolVersion: { major: number; minor: number };
          executionId: string;
          generation: number;
          commandId: string;
          occurredAt: string;
          payload: Record<string, unknown>;
        }) => void)
      | undefined;
    let resolveEvents: (() => void) | undefined;
    const eventsClosed = new Promise<void>((resolve) => {
      resolveEvents = resolve;
    });
    const runtime = createCloudAgentRuntime({
      providers: [
        {
          abiVersion: CLOUD_AGENT_PROVIDER_PLUGIN_ABI_VERSION,
          providerKind: "codex",
          async describe() {
            return await fakeRuntime().describe("codex");
          },
          async createSession() {
            const closeSession = async () => {
              closeCalled = true;
              resolveExecute?.({
                requestId: startCommand.requestId,
                protocolVersion: startCommand.protocolVersion,
                executionId: startCommand.executionId,
                generation: startCommand.generation,
                commandId: startCommand.commandId,
                occurredAt: startCommand.occurredAt,
                messageType: "Result",
                payload: { closed: true },
              });
              resolveEvents?.();
            };
            return {
              sessionId: "session-blocked",
              events: {
                async *[Symbol.asyncIterator]() {
                  await eventsClosed;
                  yield* [];
                },
              },
              execute(command) {
                return new Promise((resolve) => {
                  resolveExecute = () =>
                    resolve({
                      requestId: command.requestId,
                      protocolVersion: command.protocolVersion,
                      executionId: command.executionId,
                      generation: command.generation,
                      commandId: command.commandId,
                      occurredAt: command.occurredAt,
                      messageType: "Result",
                      payload: { closed: true },
                    });
                });
              },
              close: closeSession,
              async [Symbol.asyncDispose]() {
                await closeSession();
              },
            };
          },
        },
      ],
    });
    async function* source() {
      yield `${JSON.stringify(startCommand)}\n`;
      await new Promise<void>((resolve) => setImmediate(resolve));
      yield "{malformed}\n";
    }

    await expect(
      runCloudAgentRuntimeStdio(runtime, {
        source: Readable.from(source()),
        output: captureOutput().stream,
        diagnostics: captureOutput().stream,
      }),
    ).rejects.toThrow();
    expect(closeCalled).toBe(true);
  });
});

function fakeRuntime() {
  return createCloudAgentRuntime({
    providers: [
      {
        abiVersion: CLOUD_AGENT_PROVIDER_PLUGIN_ABI_VERSION,
        providerKind: "codex",
        async describe() {
          return {
            abiVersion: CLOUD_AGENT_PROVIDER_PLUGIN_ABI_VERSION,
            providerKind: "codex",
            displayName: "Codex",
            adapterVersion: "test",
            runtime: {
              kind: "cli" as const,
              name: "codex",
              available: true,
              compatible: true,
              compatibleRange: { minimumInclusive: "0.0.0" },
            },
            capabilities: Object.fromEntries(
              CLOUD_AGENT_CAPABILITY_IDS.map((capability) => [capability, "unsupported"]),
            ) as CloudAgentProviderDescriptor["capabilities"],
          };
        },
        async createSession() {
          throw new Error("Describe must not create a Provider Session.");
        },
      },
    ],
  });
}

function runtimeWithProvider(
  createSession: Parameters<
    typeof createCloudAgentRuntime
  >[0]["providers"][number]["createSession"],
) {
  return createCloudAgentRuntime({ providers: [{ ...testProviderDescriptor(), createSession }] });
}

function testProviderDescriptor() {
  return {
    abiVersion: CLOUD_AGENT_PROVIDER_PLUGIN_ABI_VERSION,
    providerKind: "codex",
    async describe() {
      return await fakeRuntime().describe("codex");
    },
  } as const;
}

function testSession(
  id: string,
  executed: string[],
  onClose: () => void,
  onExecute: (commandType: string) => void = () => undefined,
) {
  let closeEvents: (() => void) | undefined;
  const eventsClosed = new Promise<void>((resolve) => {
    closeEvents = resolve;
  });
  let closed = false;
  const close = async () => {
    if (closed) return;
    closed = true;
    onClose();
    closeEvents?.();
  };
  return {
    sessionId: id,
    events: {
      async *[Symbol.asyncIterator]() {
        await eventsClosed;
        yield* [];
      },
    },
    async execute(command: typeof startCommand | typeof sendCommand) {
      executed.push(command.commandType);
      onExecute(command.commandType);
      return {
        requestId: command.requestId,
        protocolVersion: command.protocolVersion,
        executionId: command.executionId,
        generation: command.generation,
        commandId: command.commandId,
        occurredAt: command.occurredAt,
        messageType: "Result" as const,
        payload: {},
      };
    },
    close,
    async [Symbol.asyncDispose]() {
      await close();
    },
  };
}

function terminalForCommand(command: {
  readonly requestId: string;
  readonly protocolVersion: { readonly major: number; readonly minor: number };
  readonly executionId: string;
  readonly generation: number;
  readonly commandId: string;
  readonly occurredAt: string;
}) {
  return {
    requestId: command.requestId,
    protocolVersion: command.protocolVersion,
    executionId: command.executionId,
    generation: command.generation,
    commandId: command.commandId,
    occurredAt: command.occurredAt,
    messageType: "Result" as const,
    payload: {},
  };
}

function captureOutput(): { stream: Writable; text: () => string } {
  let value = "";
  return {
    stream: new Writable({
      write(chunk, _encoding, callback) {
        value += chunk.toString();
        callback();
      },
    }),
    text: () => value.trim(),
  };
}

function capabilityEnvironment(): Record<string, string> {
  const manifest = {
    version: 1,
    bindings: [
      {
        resourceKind: "mcp-server" as const,
        resourceId: "server-1",
        version: "v1",
        digest: `sha256:${"a".repeat(64)}` as const,
        transport: "streamable-http" as const,
        connectionRef: "connection-1",
        credentialRef: "credential-1",
        grantId: "grant-1",
        networkPolicyRef: "network-1",
        expiresAtUnixSeconds: Math.floor(Date.now() / 1000) + 300,
        permissions: ["mcp.call"],
        readOnly: false,
      },
    ],
  };
  return {
    CLOUD_AGENT_CAPABILITY_MANIFEST_B64: Buffer.from(JSON.stringify(manifest)).toString(
      "base64url",
    ),
  };
}

function capabilityMaterialization() {
  return {
    version: 1,
    mcp: [
      {
        resourceId: "server-1",
        version: "v1",
        digest: `sha256:${"a".repeat(64)}`,
        transport: "streamable-http",
        endpoint: "http://127.0.0.1:1/mcp",
        token: "test-token",
        allowedHosts: ["127.0.0.1"],
      },
    ],
    skills: [],
  };
}

function readGoldenCommands(path: string): ReadonlyArray<{
  readonly id: string;
  readonly frame: { readonly commandId: string };
}> {
  const fixtureRoot = new URL("../../cloud-agent-protocol/fixtures/p0/", import.meta.url);
  return readFileSync(new URL(path, fixtureRoot), "utf8")
    .trim()
    .split("\n")
    .map(
      (line) =>
        JSON.parse(line) as {
          readonly id: string;
          readonly frame: { readonly commandId: string };
        },
    );
}

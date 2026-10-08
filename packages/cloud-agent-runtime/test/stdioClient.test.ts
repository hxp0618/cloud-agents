import { closeSync, mkdtempSync, openSync, rmSync, writeFileSync } from "node:fs";
import type { ChildProcessWithoutNullStreams } from "node:child_process";
import { EventEmitter } from "node:events";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { PassThrough } from "node:stream";

import { describe, expect, it, vi } from "vitest";

import type { CloudAgentCommandEnvelope } from "@cloud-agents/cloud-agent-protocol";

import { createCloudAgentStdioClient, type CloudAgentStdioClientOptions } from "../src/stdioClient";

const MESSAGE_RUNTIME_HELPER = String.raw`
const messageFor = (command, messageType, payload, overrides = {}) => JSON.stringify({
  requestId: command.requestId,
  protocolVersion: command.protocolVersion,
  executionId: command.executionId,
  generation: command.generation,
  commandId: command.commandId,
  occurredAt: command.occurredAt,
  messageType,
  payload,
  ...overrides,
});
`;

const RESPONDING_RUNTIME = String.raw`
${MESSAGE_RUNTIME_HELPER}
const readline = require("node:readline");
const lines = readline.createInterface({ input: process.stdin });
lines.on("line", (line) => {
  const command = JSON.parse(line);
  process.stdout.write(messageFor(command, "Progress", { phase: "started" }) + "\n");
  process.stdout.write(messageFor(command, "Result", { echoed: command.payload }) + "\n");
});
`;

const DELAYED_RUNTIME = String.raw`
${MESSAGE_RUNTIME_HELPER}
const readline = require("node:readline");
const lines = readline.createInterface({ input: process.stdin });
lines.on("line", (line) => {
  const command = JSON.parse(line);
  setTimeout(() => process.stdout.write(messageFor(command, "Result", {}) + "\n"), 25);
});
`;

const CREDENTIAL_RUNTIME = String.raw`
${MESSAGE_RUNTIME_HELPER}
const { readFileSync } = require("node:fs");
const readline = require("node:readline");
const lines = readline.createInterface({ input: process.stdin });
lines.once("line", (line) => {
  const command = JSON.parse(line);
  const credentialFd = Number(process.env.CLOUD_AGENT_PROVIDER_CREDENTIAL_FD);
  process.stdout.write(messageFor(command, "Result", {
    credential: readFileSync(credentialFd, "utf8"),
    credentialFd,
  }) + "\n");
});
`;

const MISMATCHED_RUNTIME = RESPONDING_RUNTIME.replace(
  "executionId: command.executionId,",
  'executionId: "wrong-execution",',
);

const INVALID_MESSAGE_RUNTIME = String.raw`
${MESSAGE_RUNTIME_HELPER}
const readline = require("node:readline");
readline.createInterface({ input: process.stdin }).once("line", (line) => {
  const command = JSON.parse(line);
  process.stdout.write(messageFor(command, "Result", {}, { generation: 0 }) + "\n");
});
`;

const SIGTERM_IGNORING_MALFORMED_RUNTIME = String.raw`
process.on("SIGTERM", () => {});
setInterval(() => {}, 1_000);
process.stdin.once("data", () => process.stdout.write("not-json\n"));
`;

const STDOUT_EOF_RUNTIME = String.raw`
${MESSAGE_RUNTIME_HELPER}
const readline = require("node:readline");
const commands = [];
readline.createInterface({ input: process.stdin }).on("line", (line) => {
  commands.push(JSON.parse(line));
  if (commands.length !== 2) return;
  process.stdout.end(messageFor(commands[0], "Result", {}) + "\n");
});
setInterval(() => {}, 1_000);
`;

const command = (commandId: string): CloudAgentCommandEnvelope => ({
  requestId: `request-${commandId}`,
  protocolVersion: { major: 2, minor: 2 },
  executionId: "execution-1",
  generation: 1,
  commandType: "Describe",
  commandId,
  occurredAt: "2026-08-09T00:00:00.000Z",
  payload: { provider: "codex" },
});

describe("createCloudAgentStdioClient", () => {
  it("rejects missing launch options before spawning", () => {
    expect(() => createCloudAgentStdioClient(null as never)).toThrow(
      /Runtime command is required/u,
    );
  });

  it("rejects execute promptly when the executable does not exist", async () => {
    const client = createCloudAgentStdioClient({
      command: join(tmpdir(), "missing-cloud-agent-runtime-executable"),
      gracefulStopTimeoutMs: 25,
    });
    await expect(client.execute(command("spawn-enoent"))).rejects.toThrow("failed to start");
    await client.close();
  });

  it("multiplexes events and terminal results by command id", async () => {
    const client = runtimeClient(RESPONDING_RUNTIME);
    const messages: string[] = [];
    const unsubscribe = client.subscribe((message) => messages.push(message.messageType));

    const [first, second] = await Promise.all([
      client.execute(command("command-1")),
      client.execute(command("command-2")),
    ]);

    expect(first.messageType).toBe("Result");
    expect(second.messageType).toBe("Result");
    expect(messages).toEqual(["Progress", "Result", "Progress", "Result"]);
    unsubscribe();
    await client.close();
  });

  it("waits for listener acknowledgement before delivering the next frame or terminal", async () => {
    const client = runtimeClient(RESPONDING_RUNTIME);
    let acknowledge!: () => void;
    const receipt = new Promise<void>((resolve) => {
      acknowledge = resolve;
    });
    let observe!: () => void;
    const observed = new Promise<void>((resolve) => {
      observe = resolve;
    });
    const messages: string[] = [];
    client.subscribe(async (message) => {
      messages.push(message.messageType);
      if (message.messageType === "Progress") {
        observe();
        await receipt;
      }
    });

    let settled = false;
    const execution = client.execute(command("acknowledged-command")).then((value) => {
      settled = true;
      return value;
    });
    await observed;
    expect(messages).toEqual(["Progress"]);
    expect(settled).toBe(false);
    acknowledge();
    await expect(execution).resolves.toMatchObject({ messageType: "Result" });
    expect(messages).toEqual(["Progress", "Result"]);
    await client.close();
  });

  it("consumes queued terminal receipts before stdout EOF rejects pending work and reaps", async () => {
    const client = runtimeClient(STDOUT_EOF_RUNTIME);
    let acknowledge!: () => void;
    const receipt = new Promise<void>((resolve) => {
      acknowledge = resolve;
    });
    let observeTerminal!: () => void;
    const terminalObserved = new Promise<void>((resolve) => {
      observeTerminal = resolve;
    });
    client.subscribe(async (message) => {
      if (message.commandId !== "before-eof") return;
      observeTerminal();
      await receipt;
    });
    let firstSettled = false;
    let secondSettled = false;
    try {
      const first = client.execute(command("before-eof")).then((message) => {
        firstSettled = true;
        return message;
      });
      const second = client.execute(command("pending-at-eof")).finally(() => {
        secondSettled = true;
      });
      const secondRejection = expect(second).rejects.toThrow("closed stdout");

      await terminalObserved;
      expect([firstSettled, secondSettled]).toEqual([false, false]);
      acknowledge();
      await expect(first).resolves.toMatchObject({ messageType: "Result" });
      await secondRejection;
      await expect.poll(() => processIsAlive(client.pid)).toBe(false);
      await expect(client.execute(command("after-eof"))).rejects.toThrow("client is closed");
    } finally {
      await client.close();
    }
  });

  it("fully replaces the child environment when extendEnvironment is false", async () => {
    process.env.CLOUD_AGENT_TEST_AMBIENT_TRUST = "must-not-be-inherited";
    const environmentRuntime = String.raw`
${MESSAGE_RUNTIME_HELPER}
const readline = require("node:readline");
readline.createInterface({ input: process.stdin }).once("line", (line) => {
  const command = JSON.parse(line);
  process.stdout.write(messageFor(command, "Result", {
    ambientTrust: process.env.CLOUD_AGENT_TEST_AMBIENT_TRUST ?? null,
    explicit: process.env.CLOUD_AGENT_TEST_EXPLICIT ?? null,
  }) + "\n");
});`;
    const client = runtimeClient(environmentRuntime, {
      extendEnvironment: false,
      environment: { CLOUD_AGENT_TEST_EXPLICIT: "present" },
    });
    try {
      await expect(client.execute(command("replace-environment"))).resolves.toMatchObject({
        payload: { ambientTrust: null, explicit: "present" },
      });
    } finally {
      delete process.env.CLOUD_AGENT_TEST_AMBIENT_TRUST;
      await client.close();
    }
  });

  it("rejects duplicate in-flight command ids", async () => {
    const client = runtimeClient(DELAYED_RUNTIME);
    const first = client.execute(command("same-command"));
    await expect(client.execute(command("same-command"))).rejects.toThrow("already in flight");
    await first;
    await client.close();
  });

  it("rejects an invalid command envelope before writing it", async () => {
    const client = runtimeClient(RESPONDING_RUNTIME);
    const invalid = { ...command("invalid-command"), generation: 0 };
    await expect(client.execute(invalid)).rejects.toThrow("command envelope is invalid");
    await client.close();
  });

  it.each([
    {
      case: "malformed JSON",
      runtime: 'process.stdin.once("data", () => process.stdout.write("not-json\\n"));',
      error: "invalid JSON",
    },
    {
      case: "an invalid message envelope",
      runtime: INVALID_MESSAGE_RUNTIME,
      error: "invalid message envelope",
    },
    {
      case: "invalid UTF-8",
      runtime:
        'process.stdin.once("data", () => process.stdout.write(Buffer.from([0xc3, 0x28, 0x0a])));',
      error: "invalid UTF-8",
    },
    {
      case: "the wrong execution identity",
      runtime: MISMATCHED_RUNTIME,
      error: "execution identity",
    },
  ])("fails closed when the runtime emits $case", async ({ runtime, error }) => {
    const client = runtimeClient(runtime);
    await expect(client.execute(command("invalid-runtime-output"))).rejects.toThrow(error);
    await client.close();
  });

  it("shares protocol-fatal teardown with close and SIGKILLs a child that ignores SIGTERM", async () => {
    const client = runtimeClient(SIGTERM_IGNORING_MALFORMED_RUNTIME, {
      gracefulStopTimeoutMs: 25,
    });
    const pid = client.pid;
    expect(pid).toBeTypeOf("number");

    await expect(client.execute(command("fatal-reap"))).rejects.toThrow("invalid JSON");
    await Promise.all([client.close(), client.close()]);

    expect(processIsAlive(pid)).toBe(false);
  });

  it("rejects close within a bounded deadline when forced termination never exits", async () => {
    const signals: NodeJS.Signals[] = [];
    const child = fakeChild((_, signal) => {
      signals.push(signal);
      return true;
    });
    const client = fakeClient(child, { gracefulStopTimeoutMs: 5 });

    await expect(client.close()).rejects.toThrow("did not exit within 5ms");
    expect(signals).toEqual(["SIGTERM", "SIGKILL"]);
  });

  it.each(["stdin", "stdout"] as const)(
    "fails closed and reaps when child %s emits a pipe error",
    async (stream) => {
      const child = fakeChild();
      const client = fakeClient(child);
      const execution = client.execute(command(`${stream}-pipe-error`));
      child[stream].emit("error", Object.assign(new Error("write EPIPE"), { code: "EPIPE" }));

      await expect(execution).rejects.toThrow(/Cloud Agent (command|Runtime stdout)/u);
      await expect(client.execute(command("after-pipe-error"))).rejects.toThrow("client is closed");
      await client.close();
    },
  );

  it("keeps the abort tombstone when the caller aborts during stdin backpressure", async () => {
    const child = fakeChild(undefined, new PassThrough({ highWaterMark: 1 }));
    const client = fakeClient(child);
    const abort = new AbortController();
    const first = client.execute(command("backpressure-aborted"), abort.signal);
    abort.abort("test cancellation");
    await expect(first).rejects.toThrow("aborted");
    expect(child.stdin.listenerCount("drain")).toBe(0);

    let inputBuffer = "";
    child.stdin.setEncoding("utf8");
    child.stdin.on("data", (chunk: string) => {
      inputBuffer += chunk;
      for (
        let newline = inputBuffer.indexOf("\n");
        newline >= 0;
        newline = inputBuffer.indexOf("\n")
      ) {
        const received = JSON.parse(inputBuffer.slice(0, newline)) as CloudAgentCommandEnvelope;
        inputBuffer = inputBuffer.slice(newline + 1);
        child.stdout.write(`${JSON.stringify(terminalFor(received))}\n`);
      }
    });
    await expect(client.execute(command("after-backpressure"))).resolves.toMatchObject({
      commandId: "after-backpressure",
    });
    await client.close();
  });

  it("drops expected late frames after abort without killing the shared Runtime", async () => {
    const client = runtimeClient(DELAYED_RUNTIME);
    const abort = new AbortController();
    const first = client.execute(command("aborted-command"), abort.signal);
    abort.abort("test cancellation");
    await expect(first).rejects.toThrow("aborted");

    await new Promise((resolve) => setTimeout(resolve, 40));
    await expect(client.execute(command("next-command"))).resolves.toMatchObject({
      messageType: "Result",
      commandId: "next-command",
    });
    await client.close();
  });

  it("expires aborted tombstones, releases capacity, and rejects a later frame fail-closed", async () => {
    vi.useFakeTimers();
    try {
      const child = fakeChild();
      const { stdin, stdout } = child;
      let firstCommand: CloudAgentCommandEnvelope | undefined;
      let inputBuffer = "";
      stdin.setEncoding("utf8");
      stdin.on("data", (chunk: string) => {
        inputBuffer += chunk;
        while (inputBuffer.includes("\n")) {
          const newline = inputBuffer.indexOf("\n");
          const line = inputBuffer.slice(0, newline);
          inputBuffer = inputBuffer.slice(newline + 1);
          const received = JSON.parse(line) as CloudAgentCommandEnvelope;
          firstCommand ??= received;
          if (received.commandId !== "after-expiry" || !firstCommand) continue;
          stdout.write(`${JSON.stringify(terminalFor(firstCommand))}\n`);
          queueMicrotask(() => {
            Object.assign(child, { exitCode: 1 });
            child.emit("exit", 1, null);
          });
        }
      });
      const client = fakeClient(child);

      for (let index = 0; index < 128; index += 1) {
        const controller = new AbortController();
        const execution = client.execute(command(`aborted-${index}`), controller.signal);
        controller.abort();
        await expect(execution).rejects.toThrow("aborted");
      }
      await expect(client.execute(command("at-capacity"))).rejects.toThrow(
        "128 commands in flight",
      );

      await vi.advanceTimersByTimeAsync(30_001);
      await expect(client.execute(command("after-expiry"))).rejects.toThrow("unknown command");
      await client.close();
    } finally {
      vi.useRealTimers();
    }
  });

  it("maps a caller-owned credential descriptor to child fd 3", async () => {
    const directory = mkdtempSync(join(tmpdir(), "cloud-agent-credential-"));
    const credentialPath = join(directory, "credential.json");
    writeFileSync(credentialPath, '{"token":"opaque-test-value"}');
    const credentialFd = openSync(credentialPath, "r");
    const client = runtimeClient(CREDENTIAL_RUNTIME, {
      credentialFd,
    });

    try {
      const result = await client.execute(command("credential-command"));
      expect(result).toMatchObject({
        messageType: "Result",
        payload: {
          credential: '{"token":"opaque-test-value"}',
          credentialFd: 3,
        },
      });
    } finally {
      await client.close();
      closeSync(credentialFd);
      rmSync(directory, { recursive: true });
    }
  });

  it("rejects a portable environment override that conflicts with credentialFd mapping", () => {
    expect(() =>
      createCloudAgentStdioClient({
        command: process.execPath,
        credentialFd: 7,
        environment: { CLOUD_AGENT_PROVIDER_CREDENTIAL_FD: "7" },
      }),
    ).toThrow("conflicting CLOUD_AGENT_PROVIDER_CREDENTIAL_FD override");
  });
});

function runtimeClient(
  runtime: string,
  options: Omit<CloudAgentStdioClientOptions, "command" | "args"> = {},
) {
  return createCloudAgentStdioClient({
    command: process.execPath,
    args: ["-e", runtime, "--"],
    ...options,
  });
}

type FakeChild = ChildProcessWithoutNullStreams & {
  stdin: PassThrough;
  stdout: PassThrough;
  stderr: PassThrough;
};

function fakeChild(
  kill: (child: FakeChild, signal: NodeJS.Signals) => boolean = (child, signal) => {
    Object.assign(child, { signalCode: signal });
    queueMicrotask(() => child.emit("exit", null, signal));
    return true;
  },
  stdin = new PassThrough(),
): FakeChild {
  const child = new EventEmitter() as FakeChild;
  Object.assign(child, {
    stdin,
    stdout: new PassThrough(),
    stderr: new PassThrough(),
    pid: 99_999,
    exitCode: null,
    signalCode: null,
    kill: (signal: NodeJS.Signals = "SIGTERM") => kill(child, signal),
  });
  return child;
}

function fakeClient(
  child: ChildProcessWithoutNullStreams,
  options: Omit<CloudAgentStdioClientOptions, "command" | "spawnProcess"> = {},
) {
  return createCloudAgentStdioClient({
    command: "fake-cloud-agent-runtime",
    spawnProcess: (() => child) as never,
    ...options,
  });
}

function processIsAlive(pid: number | undefined): boolean {
  if (pid === undefined) return false;
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}

function terminalFor(input: CloudAgentCommandEnvelope) {
  return {
    requestId: input.requestId,
    protocolVersion: input.protocolVersion,
    executionId: input.executionId,
    generation: input.generation,
    commandId: input.commandId,
    occurredAt: input.occurredAt,
    messageType: "Result",
    payload: {},
  };
}

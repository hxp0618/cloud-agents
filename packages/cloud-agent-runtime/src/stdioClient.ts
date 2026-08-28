/**
 * Host-neutral NDJSON client for a Cloud Agent Runtime child process.
 *
 * This module deliberately uses Node primitives rather than either host's
 * Effect runtime. It validates correlation before notifying subscribers and
 * keeps writes/process teardown bounded and explicit.
 */
import { spawn, type ChildProcessWithoutNullStreams, type SpawnOptions } from "node:child_process";

import {
  CLOUD_AGENT_MAX_COMMAND_BYTES,
  CLOUD_AGENT_MAX_MESSAGE_BYTES,
  validateCloudAgentCommandEnvelope,
  validateCloudAgentMessageEnvelope,
  type CloudAgentCommandEnvelope,
  type CloudAgentMessageEnvelope,
} from "@synara/cloud-agent-protocol";
import {
  CLOUD_AGENT_ENVIRONMENT,
  readCloudAgentEnvironment,
  writeCloudAgentEnvironment,
} from "@synara/cloud-agent-provider-api";

const CLOUD_AGENT_MAX_IN_FLIGHT_COMMANDS = 128;
const CLOUD_AGENT_CREDENTIAL_CHILD_FD = 3;
const ABORTED_COMMAND_TOMBSTONE_TTL_MS = 30_000;
const FATAL_UTF8_DECODER = new TextDecoder("utf-8", { fatal: true });

export interface CloudAgentStdioClientOptions {
  readonly command: string;
  readonly args?: ReadonlyArray<string>;
  readonly cwd?: string;
  readonly environment?: Readonly<Record<string, string | undefined>>;
  /** Inherit the parent environment before applying `environment`. Defaults to true. */
  readonly extendEnvironment?: boolean;
  readonly credentialFd?: number;
  readonly gracefulStopTimeoutMs?: number;
  readonly spawnProcess?: typeof spawn;
}

export interface CloudAgentStdioClient {
  readonly pid: number | undefined;
  execute(
    command: CloudAgentCommandEnvelope,
    signal?: AbortSignal,
  ): Promise<CloudAgentMessageEnvelope>;
  /**
   * Subscribes to correlated frames. A returned Promise acknowledges durable
   * receipt and blocks the next frame and terminal resolution until settled.
   */
  subscribe(listener: (message: CloudAgentMessageEnvelope) => unknown): () => void;
  close(): Promise<void>;
}

type PendingCommand = {
  readonly command: CloudAgentCommandEnvelope;
  readonly resolve: (message: CloudAgentMessageEnvelope) => void;
  readonly reject: (error: Error) => void;
  readonly removeAbortListener: () => void;
  aborted: boolean;
  tombstoneTimer?: ReturnType<typeof setTimeout>;
};

export function createCloudAgentStdioClient(
  options: CloudAgentStdioClientOptions,
): CloudAgentStdioClient {
  if (!isRecord(options) || typeof options.command !== "string" || !options.command.trim()) {
    throw new Error("Cloud Agent Runtime command is required.");
  }
  const credentialFd = validatedCredentialFd(options.credentialFd);
  const spawnProcess = options.spawnProcess ?? spawn;
  const spawnOptions: SpawnOptions = {
    cwd: options.cwd,
    env: environmentForChild(options.environment, credentialFd, options.extendEnvironment ?? true),
    stdio:
      credentialFd === undefined
        ? ["pipe", "pipe", "pipe"]
        : ["pipe", "pipe", "pipe", credentialFd],
  };
  const child = spawnProcess(
    options.command,
    [...(options.args ?? []), "--protocol-v2"],
    spawnOptions,
  );
  if (!child.stdin || !child.stdout || !child.stderr) {
    child.kill();
    throw new Error("Cloud Agent Runtime must expose stdin, stdout, and stderr pipes.");
  }

  const process = child as ChildProcessWithoutNullStreams;
  const processExit = new Promise<void>((resolve) => process.once("exit", () => resolve()));
  const pending = new Map<string, PendingCommand>();
  const listeners = new Set<(message: CloudAgentMessageEnvelope) => unknown>();
  let stdoutBuffer = Buffer.alloc(0);
  let stderrTail = "";
  let closed = false;
  let closePromise: Promise<void> | undefined;
  let consumePromise = Promise.resolve();

  const rejectAll = (error: Error) => {
    for (const command of pending.values()) {
      if (command.tombstoneTimer) clearTimeout(command.tombstoneTimer);
      command.removeAbortListener();
      command.reject(error);
    }
    pending.clear();
  };

  process.stderr.setEncoding("utf8");
  process.stderr.on("data", (chunk: string) => {
    stderrTail = `${stderrTail}${chunk}`.slice(-4_096);
  });
  process.stdout.on("data", (chunk: Buffer) => {
    stdoutBuffer = Buffer.concat([stdoutBuffer, Buffer.from(chunk)]);
    scheduleConsume();
  });
  process.stdout.on("end", () => {
    consumePromise = consumePromise.then(() => {
      if (stdoutBuffer.length > 0) {
        failProtocol("Cloud Agent Runtime closed stdout with an incomplete NDJSON frame.");
      }
    });
  });
  process.once("error", (cause) => {
    closed = true;
    rejectAll(errorWithCause("Cloud Agent Runtime failed to start.", cause));
    void reapProcess().catch(() => undefined);
  });
  process.once("exit", (code, signal) => {
    consumePromise = consumePromise.then(() => {
      closed = true;
      const diagnostics = stderrTail.trim();
      rejectAll(
        new Error(
          `Cloud Agent Runtime exited (code=${code ?? "none"}, signal=${signal ?? "none"})${diagnostics ? `: ${diagnostics}` : "."}`,
        ),
      );
    });
  });

  function scheduleConsume(): void {
    consumePromise = consumePromise.then(consumeLines).catch((cause) => {
      failProtocol("Cloud Agent Runtime listener failed to acknowledge a message.", cause);
    });
  }

  async function consumeLines(): Promise<void> {
    while (true) {
      const newline = stdoutBuffer.indexOf(0x0a);
      if (newline < 0) {
        if (stdoutBuffer.length > CLOUD_AGENT_MAX_MESSAGE_BYTES) {
          failProtocol("Cloud Agent Runtime message exceeds the negotiated size limit.");
        }
        return;
      }
      const frame = stdoutBuffer.subarray(0, newline);
      stdoutBuffer = stdoutBuffer.subarray(newline + 1);
      if (frame.length === 0) continue;
      if (frame.length > CLOUD_AGENT_MAX_MESSAGE_BYTES) {
        failProtocol("Cloud Agent Runtime message exceeds the negotiated size limit.");
        return;
      }
      let parsed: unknown;
      try {
        parsed = JSON.parse(decodeRuntimeFrame(frame));
      } catch (cause) {
        failProtocol(
          cause instanceof InvalidRuntimeUtf8Error
            ? "Cloud Agent Runtime emitted invalid UTF-8."
            : "Cloud Agent Runtime emitted invalid JSON.",
          cause,
        );
        return;
      }
      if (!isMessageEnvelope(parsed)) {
        failProtocol("Cloud Agent Runtime emitted an invalid message envelope.");
        return;
      }
      const command = pending.get(parsed.commandId);
      if (!command) {
        failProtocol(
          `Cloud Agent Runtime emitted a message for unknown command ${parsed.commandId}.`,
        );
        return;
      }
      const correlationIssue = messageCorrelationIssue(command.command, parsed);
      if (correlationIssue) {
        failProtocol(correlationIssue);
        return;
      }
      // Aborting the caller does not guarantee that the child can cancel the
      // provider operation immediately. Keep a tombstone until its terminal
      // frame arrives so expected late output cannot poison other sessions.
      if (command.aborted) {
        if (parsed.messageType === "Result" || parsed.messageType === "Error") {
          if (command.tombstoneTimer) clearTimeout(command.tombstoneTimer);
          command.removeAbortListener();
          pending.delete(parsed.commandId);
        }
        continue;
      }
      for (const listener of listeners) await listener(parsed);
      if (parsed.messageType !== "Result" && parsed.messageType !== "Error") continue;
      pending.delete(parsed.commandId);
      if (command.tombstoneTimer) clearTimeout(command.tombstoneTimer);
      command.removeAbortListener();
      command.resolve(parsed);
    }
  }

  function failProtocol(message: string, cause?: unknown): void {
    const error = errorWithCause(message, cause);
    closed = true;
    rejectAll(error);
    void reapProcess().catch(() => undefined);
  }

  async function execute(
    command: CloudAgentCommandEnvelope,
    signal?: AbortSignal,
  ): Promise<CloudAgentMessageEnvelope> {
    if (closed) throw new Error("Cloud Agent Runtime client is closed.");
    const validation = validateCloudAgentCommandEnvelope(command);
    if (!validation.valid) {
      throw new Error(`Cloud Agent command envelope is invalid: ${validation.errors.join("; ")}.`);
    }
    if (signal?.aborted) throw abortError(signal.reason);
    if (pending.has(command.commandId)) {
      throw new Error(`Cloud Agent command ${command.commandId} is already in flight.`);
    }
    if (pending.size >= CLOUD_AGENT_MAX_IN_FLIGHT_COMMANDS) {
      throw new Error(
        `Cloud Agent Runtime already has ${CLOUD_AGENT_MAX_IN_FLIGHT_COMMANDS} commands in flight.`,
      );
    }
    const frame = Buffer.from(`${JSON.stringify(command)}\n`, "utf8");
    if (frame.length - 1 > CLOUD_AGENT_MAX_COMMAND_BYTES) {
      throw new Error("Cloud Agent command exceeds the negotiated size limit.");
    }

    const terminal = new Promise<CloudAgentMessageEnvelope>((resolve, reject) => {
      const onAbort = () => {
        const current = pending.get(command.commandId);
        if (!current || current.aborted) return;
        current.aborted = true;
        current.removeAbortListener();
        reject(abortError(signal?.reason));
        current.tombstoneTimer = setTimeout(() => {
          const tombstone = pending.get(command.commandId);
          if (tombstone !== current || !tombstone.aborted) return;
          pending.delete(command.commandId);
          delete tombstone.tombstoneTimer;
        }, ABORTED_COMMAND_TOMBSTONE_TTL_MS);
        current.tombstoneTimer.unref();
      };
      signal?.addEventListener("abort", onAbort, { once: true });
      pending.set(command.commandId, {
        command,
        resolve,
        reject,
        removeAbortListener: () => signal?.removeEventListener("abort", onAbort),
        aborted: false,
      });
    });

    try {
      if (!process.stdin.write(frame)) {
        await Promise.race([
          new Promise<void>((resolve) => process.stdin.once("drain", resolve)),
          terminal.then(() => undefined),
        ]);
      }
    } catch (cause) {
      const current = pending.get(command.commandId);
      pending.delete(command.commandId);
      if (current?.tombstoneTimer) clearTimeout(current.tombstoneTimer);
      current?.removeAbortListener();
      current?.reject(errorWithCause("Failed to write Cloud Agent command.", cause));
    }
    return terminal;
  }

  function reapProcess(): Promise<void> {
    if (closePromise) return closePromise;
    closePromise = (async () => {
      closed = true;
      rejectAll(new Error("Cloud Agent Runtime client was closed."));
      if (process.stdin.writable) process.stdin.end();
      if (process.exitCode !== null || process.signalCode !== null) return;
      try {
        process.kill("SIGTERM");
      } catch {
        // The exit check below remains authoritative.
      }
      const timeoutMs = options.gracefulStopTimeoutMs ?? 5_000;
      if (await settlesBefore(processExit, timeoutMs)) return;
      if (process.exitCode === null && process.signalCode === null) {
        try {
          process.kill("SIGKILL");
        } catch (cause) {
          if (process.exitCode === null && process.signalCode === null) {
            throw errorWithCause("Failed to force-stop Cloud Agent Runtime.", cause);
          }
        }
      }
      if (!(await settlesBefore(processExit, timeoutMs))) {
        throw new Error(
          `Cloud Agent Runtime did not exit within ${timeoutMs}ms after forced termination.`,
        );
      }
    })();
    return closePromise;
  }

  async function close(): Promise<void> {
    return reapProcess();
  }

  return Object.freeze({
    get pid() {
      return process.pid;
    },
    execute,
    subscribe(listener: (message: CloudAgentMessageEnvelope) => unknown) {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    close,
  });
}

class InvalidRuntimeUtf8Error extends Error {}

function decodeRuntimeFrame(frame: Uint8Array): string {
  try {
    return FATAL_UTF8_DECODER.decode(frame);
  } catch {
    throw new InvalidRuntimeUtf8Error("Runtime frame is not valid UTF-8.");
  }
}

function environmentForChild(
  overrides: Readonly<Record<string, string | undefined>> | undefined,
  credentialFd: number | undefined,
  extendEnvironment: boolean,
): NodeJS.ProcessEnv {
  const environment: NodeJS.ProcessEnv = extendEnvironment ? { ...process.env } : {};
  for (const [name, value] of Object.entries(overrides ?? {})) {
    if (value === undefined) delete environment[name];
    else environment[name] = value;
  }
  const configuredFd = readCloudAgentEnvironment(
    overrides ?? {},
    CLOUD_AGENT_ENVIRONMENT.providerCredentialFd,
  );
  if (credentialFd !== undefined) {
    if (configuredFd !== undefined && configuredFd !== String(CLOUD_AGENT_CREDENTIAL_CHILD_FD)) {
      throw new Error(
        `credentialFd is exposed to the Runtime as fd ${CLOUD_AGENT_CREDENTIAL_CHILD_FD}; remove the conflicting CLOUD_AGENT_PROVIDER_CREDENTIAL_FD override.`,
      );
    }
    // Node maps the caller-owned descriptor into the fourth stdio slot, so
    // the child must always read fd 3 rather than the caller's descriptor id.
    writeCloudAgentEnvironment(
      environment,
      CLOUD_AGENT_ENVIRONMENT.providerCredentialFd,
      String(CLOUD_AGENT_CREDENTIAL_CHILD_FD),
    );
  }
  return environment;
}

function validatedCredentialFd(value: number | undefined): number | undefined {
  if (value === undefined) return undefined;
  if (!Number.isInteger(value) || value < 0) {
    throw new Error("credentialFd must be a non-negative integer file descriptor.");
  }
  return value;
}

function isMessageEnvelope(value: unknown): value is CloudAgentMessageEnvelope {
  return validateCloudAgentMessageEnvelope(value).valid;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function messageCorrelationIssue(
  command: CloudAgentCommandEnvelope,
  message: CloudAgentMessageEnvelope,
): string | undefined {
  if (
    message.protocolVersion.major !== command.protocolVersion.major ||
    message.protocolVersion.minor < command.protocolVersion.minor
  ) {
    return `Cloud Agent Runtime responded with incompatible Protocol ${message.protocolVersion.major}.${message.protocolVersion.minor}.`;
  }
  if (message.requestId !== command.requestId) {
    return "Cloud Agent Runtime response requestId did not match the in-flight command.";
  }
  if (message.executionId !== command.executionId || message.generation !== command.generation) {
    return "Cloud Agent Runtime response execution identity did not match the in-flight command.";
  }
  return undefined;
}

function errorWithCause(message: string, cause?: unknown): Error {
  return cause === undefined ? new Error(message) : new Error(message, { cause });
}

function abortError(reason: unknown): Error {
  return new Error("Cloud Agent command was aborted.", { cause: reason });
}

async function settlesBefore(promise: Promise<void>, timeoutMs: number): Promise<boolean> {
  let timeout: ReturnType<typeof setTimeout> | undefined;
  try {
    return await Promise.race([
      promise.then(() => true),
      new Promise<boolean>((resolve) => {
        timeout = setTimeout(() => resolve(false), timeoutMs);
      }),
    ]);
  } finally {
    if (timeout) clearTimeout(timeout);
  }
}

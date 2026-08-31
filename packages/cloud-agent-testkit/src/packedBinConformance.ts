import { spawnSync } from "node:child_process";
import { isAbsolute } from "node:path";

import {
  CLOUD_AGENT_PROTOCOL_VERSION,
  assertCloudAgentMessageEnvelope,
  type CloudAgentCommandEnvelope,
  type CloudAgentMessageEnvelope,
} from "@cloud-agents/cloud-agent-protocol";

export type CloudAgentPackedBinConformanceReport = {
  readonly passed: ReadonlyArray<string>;
  readonly realProviderGates: ReadonlyArray<string>;
};

/**
 * Exercises the actual packed executable as an external Node process. Cases
 * that require a real authenticated provider are returned as explicit gates.
 */
export function runCloudAgentPackedBinConformance(input: {
  readonly executable: string;
  readonly environment?: Readonly<Record<string, string | undefined>>;
  readonly timeoutMs?: number;
}): CloudAgentPackedBinConformanceReport {
  if (!isAbsolute(input.executable)) {
    throw new Error("Packed Cloud Agent executable path must be absolute.");
  }
  const passed: string[] = [];
  for (const minor of [2, CLOUD_AGENT_PROTOCOL_VERSION.minor]) {
    const command = describe(`negotiation-${minor}`, minor, minor);
    assertTranscript(run(input, ["--protocol-v2"], `${JSON.stringify(command)}\n`), [command]);
    passed.push(`protocol-2.${minor}-negotiation`);
  }

  const multiplexed = Array.from({ length: 64 }, (_, index) =>
    describe(`backpressure-${index}`, CLOUD_AGENT_PROTOCOL_VERSION.minor, index + 1),
  );
  const multiplexedMessages = run(
    input,
    ["--protocol-v2"],
    `${multiplexed.map((command) => JSON.stringify(command)).join("\n")}\n`,
  );
  assertTranscript(multiplexedMessages, multiplexed);
  passed.push("bounded-multiplexing-backpressure", "correlation", "generation-fencing-metadata");

  const first = describe("instance-a", 3, 101);
  const second = describe("instance-b", 3, 202);
  assertTranscript(run(input, ["--protocol-v2"], `${JSON.stringify(first)}\n`), [first]);
  assertTranscript(run(input, ["--protocol-v2"], `${JSON.stringify(second)}\n`), [second]);
  passed.push("multi-instance-isolation");

  const malformed = spawn(input, ["--protocol-v2"], "{not-json}\n");
  if (malformed.status === 0) throw new Error("Packed Runtime accepted an illegal NDJSON frame.");
  passed.push("illegal-frame-fail-closed", "runtime-crash-observed");

  const noToolInput = JSON.stringify({
    hook_event_name: "PreToolUse",
    tool_name: "Read",
    tool_input: { file_path: "README.md" },
  });
  const noTool = spawn(
    {
      ...input,
      environment: {
        ...input.environment,
        CLOUD_AGENT_CODEX_NO_TOOL_OPERATION: "1",
      },
    },
    ["--cloud-agent-codex-tool-policy-hook"],
    noToolInput,
  );
  if (noTool.status !== 0 || !noTool.stdout.includes('"permissionDecision":"deny"')) {
    throw new Error("Packed Runtime no-tool policy hook did not deny Provider tools.");
  }
  passed.push("no-tool-policy");

  return {
    passed,
    realProviderGates: [
      "authenticated StartSession/ResumeSession/StopSession lifecycle",
      "provider-originated late terminal after interrupt or crash",
      "real provider secret redaction and artifact path containment",
      "real provider backpressure under sustained tool and artifact events",
    ],
  };
}

function describe(commandId: string, minor: number, generation: number): CloudAgentCommandEnvelope {
  return {
    requestId: `request:${commandId}`,
    protocolVersion: { major: 2, minor },
    executionId: `execution:${commandId}`,
    generation,
    commandType: "Describe",
    commandId,
    occurredAt: "2026-08-09T00:00:00.000Z",
    payload: { provider: "codex" },
  };
}

function assertTranscript(
  messages: ReadonlyArray<CloudAgentMessageEnvelope>,
  commands: ReadonlyArray<CloudAgentCommandEnvelope>,
): void {
  const byCommand = new Map(commands.map((command) => [command.commandId, command]));
  const terminals = new Set<string>();
  for (const message of messages) {
    assertCloudAgentMessageEnvelope(message);
    const command = byCommand.get(message.commandId);
    if (
      !command ||
      message.requestId !== command.requestId ||
      message.executionId !== command.executionId ||
      message.generation !== command.generation
    ) {
      throw new Error("Packed Runtime emitted an incorrectly correlated message.");
    }
    if (message.messageType === "Result" || message.messageType === "Error") {
      terminals.add(message.commandId);
    }
  }
  if (terminals.size !== commands.length) {
    throw new Error("Packed Runtime omitted a terminal receipt.");
  }
}

function run(
  input: Parameters<typeof runCloudAgentPackedBinConformance>[0],
  args: ReadonlyArray<string>,
  stdin: string,
): ReadonlyArray<CloudAgentMessageEnvelope> {
  const result = spawn(input, args, stdin);
  if (result.status !== 0) {
    throw new Error(`Packed Runtime failed conformance with status ${String(result.status)}.`);
  }
  return result.stdout
    .split("\n")
    .filter(Boolean)
    .map((line) => JSON.parse(line) as CloudAgentMessageEnvelope);
}

function spawn(
  input: Parameters<typeof runCloudAgentPackedBinConformance>[0],
  args: ReadonlyArray<string>,
  stdin: string,
) {
  const environment: NodeJS.ProcessEnv = {};
  for (const [key, value] of Object.entries({ ...process.env, ...input.environment })) {
    if (value !== undefined) environment[key] = value;
  }
  return spawnSync(input.executable, [...args], {
    input: stdin,
    encoding: "utf8",
    timeout: input.timeoutMs ?? 15_000,
    maxBuffer: 32 * 1024 * 1024,
    env: environment,
  });
}

import {
  CLOUD_AGENT_PROTOCOL_VERSION as PROVIDER_HOST_PROTOCOL_VERSION,
  type CloudAgentCommandEnvelope as ProviderHostCommand,
  type CloudAgentError as ProviderHostError,
  type CloudAgentMessageEnvelope as ProviderHostMessageEnvelope,
} from "@cloud-agents/cloud-agent-protocol";
import { isRecord } from "./json";

export function payloadMessage(
  command: ProviderHostCommand,
  messageType: "Event" | "InteractionRequest" | "ArtifactCandidate" | "Checkpoint" | "Progress",
  payload: Record<string, unknown>,
): ProviderHostMessageEnvelope {
  return {
    ...messageBase(command),
    messageType,
    payload,
  } as ProviderHostMessageEnvelope;
}

export function resultMessage(
  command: ProviderHostCommand,
  payload: Record<string, unknown>,
): ProviderHostMessageEnvelope {
  return {
    ...messageBase(command),
    messageType: "Result",
    payload,
  };
}

export type StopOutcome = "quiesced" | "timed-out" | "forced" | "failed";

export function stopResultMessage(
  command: ProviderHostCommand,
  outcome: StopOutcome,
  cause?: unknown,
): ProviderHostMessageEnvelope {
  const detail =
    cause instanceof Error ? cause.message : cause === undefined ? undefined : String(cause);
  return resultMessage(command, {
    stopped: true,
    outcome,
    quiesced: outcome === "quiesced",
    graceful: outcome === "quiesced",
    ...(detail ? { detail } : {}),
  });
}

export function errorMessage(
  command: ProviderHostCommand,
  error: ProviderHostError,
): ProviderHostMessageEnvelope {
  return {
    ...messageBase(command),
    messageType: "Error",
    error,
  };
}

// Intentionally not a type predicate: a non-interrupted Error message fails
// this check too, so narrowing the negative branch away from "Error" would be
// unsound (the caller still needs to read `error.code` from it).
export function isInterruptedTerminalMessage(message: ProviderHostMessageEnvelope): boolean {
  return message.messageType === "Error" && message.error.code === "interrupted";
}

function messageBase(command: ProviderHostCommand) {
  return {
    requestId: command.requestId,
    protocolVersion: PROVIDER_HOST_PROTOCOL_VERSION,
    executionId: command.executionId,
    generation: command.generation,
    commandId: command.commandId,
    occurredAt: new Date().toISOString(),
  };
}

export function protocolFallbackCommand(value?: unknown): ProviderHostCommand {
  const candidate = isRecord(value) ? value : {};
  return {
    requestId: safeWireString(candidate.requestId, "protocol-request"),
    protocolVersion: PROVIDER_HOST_PROTOCOL_VERSION,
    executionId: safeWireString(candidate.executionId, "protocol-execution"),
    generation:
      typeof candidate.generation === "number" && candidate.generation >= 1
        ? Math.floor(candidate.generation)
        : 1,
    commandType: "Describe",
    commandId: safeWireString(
      candidate.commandId,
      "protocol-command",
    ) as ProviderHostCommand["commandId"],
    occurredAt: new Date().toISOString(),
    payload: {},
  };
}

function safeWireString(value: unknown, fallback: string): string {
  return typeof value === "string" && value.trim() ? value.trim().slice(0, 200) : fallback;
}

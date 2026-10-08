import { randomUUID } from "node:crypto";
import {
  closeSync,
  fstatSync,
  mkdirSync,
  openSync,
  readFileSync,
  renameSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { join } from "node:path";
import { CLOUD_AGENT_MAX_MESSAGE_BYTES } from "@cloud-agents/cloud-agent-protocol";

import type { RunnerInput } from "./internalExecution";
import { isRecord } from "./json";

export { isRecord } from "./json";

const EMULATED_HISTORY_FILE = "cloud-agent-conversation-history-v1.json";
const MAX_HISTORY_BYTES = 32 << 20;

type MissingHistoryError = () => Error;

export function validateConversationHistory(
  messages: unknown,
  requireMessages = false,
): asserts messages is NonNullable<RunnerInput["workload"]["conversationHistory"]> {
  if (
    !Array.isArray(messages) ||
    (requireMessages && messages.length === 0) ||
    messages.some(
      (message) =>
        !isRecord(message) ||
        (message.role !== "user" && message.role !== "assistant") ||
        typeof message.text !== "string",
    ) ||
    Buffer.byteLength(JSON.stringify(messages)) > MAX_HISTORY_BYTES
  ) {
    throw new Error("conversation history is invalid");
  }
}

export function conversationHistoryHasTurnCapacity(input: RunnerInput): boolean {
  const messages = [
    ...(input.workload.conversationHistory ?? []),
    { role: "user", text: input.workload.inputText },
    { role: "assistant", text: "" },
  ];
  return (
    Buffer.byteLength(JSON.stringify(messages)) + CLOUD_AGENT_MAX_MESSAGE_BYTES <= MAX_HISTORY_BYTES
  );
}

export function withPersistedConversationHistory(
  input: RunnerInput,
  provider: string,
  missingHistoryError: MissingHistoryError,
): RunnerInput {
  const directory = input.providerStateDirectory;
  if (!directory) throw missingHistoryError();
  const path = join(directory, EMULATED_HISTORY_FILE);
  let descriptor: number | undefined;
  try {
    descriptor = openSync(path, "r");
    const wrapperBytes =
      Buffer.byteLength(JSON.stringify({ version: 1, provider, messages: [] }) + "\n") - 2;
    if (fstatSync(descriptor).size > MAX_HISTORY_BYTES + wrapperBytes)
      throw new Error("history too large");
    const value = JSON.parse(readFileSync(descriptor, "utf8")) as unknown;
    if (!isRecord(value) || value.version !== 1 || value.provider !== provider) {
      throw new Error("history identity mismatch");
    }
    const messages = value.messages;
    validateConversationHistory(messages, true);
    return {
      ...input,
      workload: { ...input.workload, conversationHistory: messages },
    };
  } catch {
    throw missingHistoryError();
  } finally {
    if (descriptor !== undefined) closeSync(descriptor);
  }
}

export function persistConversationHistory(
  input: RunnerInput,
  provider: string,
  missingHistoryError: MissingHistoryError,
): void {
  const directory = input.providerStateDirectory;
  const messages = input.workload.conversationHistory;
  if (!directory || !messages?.length) throw missingHistoryError();
  validateConversationHistory(messages, true);
  mkdirSync(directory, { recursive: true, mode: 0o700 });
  const path = join(directory, EMULATED_HISTORY_FILE);
  const temporary = `${path}.${randomUUID()}.tmp`;
  try {
    const encoded = `${JSON.stringify({ version: 1, provider, messages })}\n`;
    writeFileSync(temporary, encoded, { flag: "wx", mode: 0o600 });
    renameSync(temporary, path);
  } finally {
    rmSync(temporary, { force: true });
  }
}

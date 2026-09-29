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

import type { RunnerInput } from "./internalExecution";
import { isRecord } from "./json";

export { isRecord } from "./json";

const EMULATED_HISTORY_FILE = "cloud-agent-conversation-history-v1.json";
const MAX_EMULATED_HISTORY_BYTES = 1 << 20;

type MissingHistoryError = () => Error;

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
    if (fstatSync(descriptor).size > MAX_EMULATED_HISTORY_BYTES)
      throw new Error("history too large");
    const value = JSON.parse(readFileSync(descriptor, "utf8")) as unknown;
    if (!isRecord(value) || value.version !== 1 || value.provider !== provider) {
      throw new Error("history identity mismatch");
    }
    const messages = value.messages;
    if (
      !Array.isArray(messages) ||
      messages.length === 0 ||
      messages.length > 512 ||
      messages.some(
        (message) =>
          !isRecord(message) ||
          (message.role !== "user" && message.role !== "assistant") ||
          typeof message.text !== "string" ||
          Buffer.byteLength(message.text) > 64 << 10,
      )
    ) {
      throw new Error("history payload invalid");
    }
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
  mkdirSync(directory, { recursive: true, mode: 0o700 });
  const path = join(directory, EMULATED_HISTORY_FILE);
  const temporary = `${path}.${randomUUID()}.tmp`;
  try {
    const encoded = `${JSON.stringify({ version: 1, provider, messages })}\n`;
    if (Buffer.byteLength(encoded) > MAX_EMULATED_HISTORY_BYTES) throw missingHistoryError();
    writeFileSync(temporary, encoded, { flag: "wx", mode: 0o600 });
    renameSync(temporary, path);
  } finally {
    rmSync(temporary, { force: true });
  }
}

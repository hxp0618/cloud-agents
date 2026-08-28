import {
  CLOUD_AGENT_COMMAND_TYPES,
  CLOUD_AGENT_ERROR_CODES,
  CLOUD_AGENT_PROTOCOL_VERSION,
  type CloudAgentCommandEnvelope,
  type CloudAgentMessageEnvelope,
} from "./protocol";

export type CloudAgentValidationResult =
  | { readonly valid: true }
  | { readonly valid: false; readonly errors: ReadonlyArray<string> };

/** Plain-JavaScript structural validation for the stable Protocol v2 command ABI. */
export function validateCloudAgentCommandEnvelope(value: unknown): CloudAgentValidationResult {
  const errors = commonEnvelopeErrors(value);
  const record = asRecord(value);
  if (!record || !(CLOUD_AGENT_COMMAND_TYPES as readonly unknown[]).includes(record.commandType)) {
    errors.push("commandType is not a supported Protocol v2 command");
  }
  if (!asRecord(record?.payload)) errors.push("payload must be an object");
  return result(errors);
}

/** Plain-JavaScript structural validation for the stable Protocol v2 message ABI. */
export function validateCloudAgentMessageEnvelope(value: unknown): CloudAgentValidationResult {
  const errors = commonEnvelopeErrors(value);
  const record = asRecord(value);
  const messageType = record?.messageType;
  if (messageType === "Error") {
    const error = asRecord(record?.error);
    if (!error) errors.push("error must be an object");
    else {
      if (!(CLOUD_AGENT_ERROR_CODES as readonly unknown[]).includes(error.code)) {
        errors.push("error.code is not supported");
      }
      if (typeof error.message !== "string") errors.push("error.message must be a string");
      for (const field of [
        "retryable",
        "requiresNewExecution",
        "requiresUserAction",
        "canReconstructFromHistory",
        "canMoveWorker",
      ]) {
        if (typeof error[field] !== "boolean") errors.push(`error.${field} must be a boolean`);
      }
    }
  } else if (
    ![
      "Event",
      "InteractionRequest",
      "ArtifactCandidate",
      "Checkpoint",
      "Progress",
      "Result",
    ].includes(String(messageType))
  ) {
    errors.push("messageType is not a supported Protocol v2 message");
  } else if (!asRecord(record?.payload)) {
    errors.push("payload must be an object");
  }
  return result(errors);
}

export function assertCloudAgentCommandEnvelope(
  value: unknown,
): asserts value is CloudAgentCommandEnvelope {
  assertValid(validateCloudAgentCommandEnvelope(value), "Cloud Agent command envelope");
}

export function assertCloudAgentMessageEnvelope(
  value: unknown,
): asserts value is CloudAgentMessageEnvelope {
  assertValid(validateCloudAgentMessageEnvelope(value), "Cloud Agent message envelope");
}

function commonEnvelopeErrors(value: unknown): string[] {
  const errors: string[] = [];
  const record = asRecord(value);
  if (!record) return ["envelope must be an object"];
  for (const field of ["requestId", "executionId", "commandId", "occurredAt"] as const) {
    if (typeof record[field] !== "string" || !record[field]) {
      errors.push(`${field} must be a non-empty string`);
    }
  }
  if (!Number.isSafeInteger(record.generation) || Number(record.generation) < 1) {
    errors.push("generation must be a positive safe integer");
  }
  const protocol = asRecord(record.protocolVersion);
  if (
    protocol?.major !== CLOUD_AGENT_PROTOCOL_VERSION.major ||
    !Number.isSafeInteger(protocol.minor) ||
    Number(protocol.minor) < 0 ||
    Number(protocol.minor) > CLOUD_AGENT_PROTOCOL_VERSION.minor
  ) {
    errors.push(
      `protocolVersion must negotiate ${CLOUD_AGENT_PROTOCOL_VERSION.major}.0-${CLOUD_AGENT_PROTOCOL_VERSION.major}.${CLOUD_AGENT_PROTOCOL_VERSION.minor}`,
    );
  }
  return errors;
}

function result(errors: string[]): CloudAgentValidationResult {
  return errors.length === 0 ? { valid: true } : { valid: false, errors };
}

function assertValid(result: CloudAgentValidationResult, label: string): void {
  if (!result.valid) throw new Error(`${label} is invalid: ${result.errors.join("; ")}.`);
}

function asRecord(value: unknown): Record<string, unknown> | undefined {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined;
}

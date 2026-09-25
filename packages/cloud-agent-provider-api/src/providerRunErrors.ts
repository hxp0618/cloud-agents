export class ProviderInterruptedError extends Error {
  constructor() {
    super("Provider turn was interrupted.");
    this.name = "ProviderInterruptedError";
  }
}

export const MANAGED_MCP_CALL_RESULT_UNKNOWN_MARKER = "mcp_call_result_unknown";

export class ManagedCapabilityCallResultUnknownError extends Error {
  constructor(message = "Managed MCP call result is unknown.") {
    super(message);
    this.name = "ManagedCapabilityCallResultUnknownError";
  }
}

/** Detects the Runtime-owned MCP marker without traversing unbounded Provider output. */
export function isManagedMcpCallResultUnknown(value: unknown): boolean {
  const seen = new Set<object>();
  let remaining = 64;

  const visit = (candidate: unknown, depth: number): boolean => {
    if (remaining-- <= 0 || depth > 6) return false;
    if (typeof candidate === "string") {
      return candidate.slice(0, 8_192).includes(MANAGED_MCP_CALL_RESULT_UNKNOWN_MARKER);
    }
    if (candidate === null || typeof candidate !== "object" || seen.has(candidate)) return false;
    seen.add(candidate);
    if (candidate instanceof ManagedCapabilityCallResultUnknownError) return true;
    if (candidate instanceof Error) {
      return visit(candidate.message, depth + 1) || visit(candidate.cause, depth + 1);
    }
    const values = Array.isArray(candidate)
      ? candidate.slice(0, 32)
      : Object.values(candidate as Record<string, unknown>).slice(0, 32);
    return values.some((entry) => visit(entry, depth + 1));
  };

  return visit(value, 0);
}

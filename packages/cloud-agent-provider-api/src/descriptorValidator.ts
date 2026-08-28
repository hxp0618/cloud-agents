import {
  CLOUD_AGENT_CAPABILITY_IDS,
  CLOUD_AGENT_TEXT_GENERATION_TASKS,
} from "@synara/cloud-agent-protocol";

export type ValidatedCloudAgentProviderDescriptor = {
  readonly abiVersion: 1;
  readonly providerKind: string;
  readonly displayName: string;
  readonly adapterVersion: string;
  readonly runtime: Readonly<Record<string, unknown>>;
  readonly capabilities: Readonly<Record<string, unknown>>;
  readonly [key: string]: unknown;
};

/** Validates an unknown provider descriptor without requiring Effect or host types. */
export function assertCloudAgentProviderDescriptor(
  value: unknown,
): asserts value is ValidatedCloudAgentProviderDescriptor {
  const descriptor = asRecord(value);
  if (!descriptor) throw new Error("Cloud Agent Provider descriptor must be an object.");
  if (descriptor.abiVersion !== 1) {
    throw new Error(`Unexpected Provider Plugin ABI ${String(descriptor.abiVersion)}.`);
  }
  if (
    typeof descriptor.providerKind !== "string" ||
    !/^[a-zA-Z][a-zA-Z0-9_-]{0,63}$/u.test(descriptor.providerKind)
  ) {
    throw new Error("Provider kind is not a portable slug.");
  }
  if (typeof descriptor.displayName !== "string" || !descriptor.displayName.trim()) {
    throw new Error("Provider descriptor omitted displayName.");
  }
  if (typeof descriptor.adapterVersion !== "string" || !descriptor.adapterVersion.trim()) {
    throw new Error("Provider descriptor omitted adapterVersion.");
  }
  const runtime = asRecord(descriptor.runtime);
  const range = asRecord(runtime?.compatibleRange);
  if (
    !runtime ||
    !["cli", "sdk", "local"].includes(String(runtime.kind)) ||
    typeof runtime.name !== "string" ||
    !runtime.name.trim() ||
    typeof runtime.available !== "boolean" ||
    typeof runtime.compatible !== "boolean" ||
    typeof range?.minimumInclusive !== "string" ||
    !range.minimumInclusive.trim()
  ) {
    throw new Error("Provider descriptor runtime is invalid.");
  }
  if (
    (runtime.version !== undefined &&
      (typeof runtime.version !== "string" || !runtime.version.trim())) ||
    (range?.maximumExclusive !== undefined &&
      (typeof range.maximumExclusive !== "string" || !range.maximumExclusive.trim()))
  ) {
    throw new Error("Provider descriptor runtime version range is invalid.");
  }
  const capabilities = asRecord(descriptor.capabilities);
  if (!capabilities) throw new Error("Provider descriptor capabilities are missing.");
  const actual = Object.keys(capabilities).toSorted();
  const expected = [...CLOUD_AGENT_CAPABILITY_IDS].toSorted();
  if (actual.length !== expected.length || actual.some((key, index) => key !== expected[index])) {
    throw new Error("Provider descriptor does not define the complete capability map.");
  }
  for (const capability of CLOUD_AGENT_CAPABILITY_IDS) {
    if (!["native", "emulated", "unsupported"].includes(String(capabilities[capability]))) {
      throw new Error(`Provider descriptor has invalid support for ${capability}.`);
    }
  }
  if (descriptor.configurationSchema !== undefined && !asRecord(descriptor.configurationSchema)) {
    throw new Error("Provider descriptor configurationSchema is invalid.");
  }
  if (descriptor.textGenerationTasks !== undefined) {
    if (!Array.isArray(descriptor.textGenerationTasks)) {
      throw new Error("Provider descriptor textGenerationTasks are invalid.");
    }
    const seen = new Set<string>();
    for (const task of descriptor.textGenerationTasks) {
      if (
        typeof task !== "string" ||
        !CLOUD_AGENT_TEXT_GENERATION_TASKS.includes(task as never) ||
        seen.has(task)
      ) {
        throw new Error("Provider descriptor textGenerationTasks are invalid.");
      }
      seen.add(task);
    }
  }
}

function asRecord(value: unknown): Record<string, unknown> | undefined {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined;
}

import {
  assertCloudAgentCommandEnvelope,
  assertCloudAgentMessageEnvelope,
  type CloudAgentCommandEnvelope,
  type CloudAgentMessageEnvelope,
} from "@synara/cloud-agent-protocol";
import {
  CLOUD_AGENT_PROVIDER_PLUGIN_ABI_VERSION,
  assertCloudAgentProviderDescriptor,
  type CloudAgentHostServices,
  type CloudAgentProviderDescriptor,
  type CloudAgentProviderPluginV1,
  type CloudAgentProviderSession,
} from "@synara/cloud-agent-provider-api";

export interface CloudAgentRuntimeV1 {
  readonly abiVersion: typeof CLOUD_AGENT_PROVIDER_PLUGIN_ABI_VERSION;
  readonly providerKinds: ReadonlyArray<string>;
  describe(providerKind: string, signal?: AbortSignal): Promise<CloudAgentProviderDescriptor>;
  createSession(
    providerKind: string,
    input: {
      readonly hostInstanceId: string;
      readonly hostThreadId: string;
      readonly configuration: Readonly<Record<string, unknown>>;
    },
    host: CloudAgentHostServices,
    signal?: AbortSignal,
  ): Promise<CloudAgentProviderSession>;
}

export function createCloudAgentRuntime(input: {
  readonly providers: ReadonlyArray<CloudAgentProviderPluginV1>;
}): CloudAgentRuntimeV1 {
  const providers = new Map<string, CloudAgentProviderPluginV1>();
  for (const provider of input.providers) {
    if (provider.abiVersion !== CLOUD_AGENT_PROVIDER_PLUGIN_ABI_VERSION) {
      throw new Error(
        `Provider ${provider.providerKind} uses unsupported ABI ${provider.abiVersion}.`,
      );
    }
    const providerKind = normalizedProviderKind(provider.providerKind);
    if (provider.providerKind !== providerKind) {
      throw new Error(`Provider kind ${provider.providerKind} must use its canonical spelling.`);
    }
    if (providers.has(providerKind)) {
      throw new Error(`Provider ${providerKind} is registered more than once.`);
    }
    // Keep the registry independent from later mutation of the caller-owned
    // plugin object. Provider closures remain responsible for their own state.
    providers.set(providerKind, { ...provider });
  }

  const providerKinds = Object.freeze([...providers.keys()].toSorted());

  const runtime: CloudAgentRuntimeV1 = {
    abiVersion: CLOUD_AGENT_PROVIDER_PLUGIN_ABI_VERSION,
    providerKinds,
    describe: async (providerKind, signal) => {
      const normalized = normalizedProviderKind(providerKind);
      const descriptor = await providerFor(providers, normalized).describe(signal);
      assertCloudAgentProviderDescriptor(descriptor);
      if (descriptor.providerKind !== normalized) {
        throw new Error(
          `Provider descriptor identity ${descriptor.providerKind} does not match ${normalized}.`,
        );
      }
      return descriptor;
    },
    createSession: async (providerKind, sessionInput, host, signal) => {
      const normalized = normalizedProviderKind(providerKind);
      const session = await providerFor(providers, normalized).createSession(
        sessionInput,
        host,
        signal,
      );
      return validatedProviderSession(session);
    },
  };
  return Object.freeze(runtime);
}

function validatedProviderSession(value: unknown): CloudAgentProviderSession {
  if (!isRecord(value)) throw new Error("Cloud Agent Provider session must be an object.");
  const candidate = value as Record<PropertyKey, unknown>;
  if (typeof candidate.sessionId !== "string" || !candidate.sessionId.trim()) {
    throw new Error("Cloud Agent Provider sessionId must be a non-empty string.");
  }
  if (!isAsyncIterable(candidate.events)) {
    throw new Error("Cloud Agent Provider session events must be async iterable.");
  }
  if (typeof candidate.execute !== "function" || typeof candidate.close !== "function") {
    throw new Error("Cloud Agent Provider session methods are invalid.");
  }
  const execute = candidate.execute as (
    command: CloudAgentCommandEnvelope,
    signal?: AbortSignal,
  ) => Promise<unknown>;
  const close = candidate.close as (reason?: string) => Promise<void>;
  const asyncDispose = candidate[Symbol.asyncDispose];
  return {
    sessionId: candidate.sessionId,
    events: validatedProviderEvents(candidate.events),
    async execute(command, signal) {
      assertCloudAgentCommandEnvelope(command);
      const message = await execute.call(value, command, signal);
      assertCloudAgentMessageEnvelope(message);
      return message;
    },
    close: (reason) => close.call(value, reason),
    async [Symbol.asyncDispose]() {
      if (typeof asyncDispose === "function") {
        await (asyncDispose as () => Promise<void>).call(value);
      } else {
        await close.call(value, "disposed");
      }
    },
  };
}

async function* validatedProviderEvents(
  events: AsyncIterable<unknown>,
): AsyncGenerator<CloudAgentMessageEnvelope> {
  for await (const message of events) {
    assertCloudAgentMessageEnvelope(message);
    yield message;
  }
}

function isAsyncIterable(value: unknown): value is AsyncIterable<unknown> {
  return (
    value !== null &&
    typeof value === "object" &&
    typeof (value as { [Symbol.asyncIterator]?: unknown })[Symbol.asyncIterator] === "function"
  );
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function providerFor(
  providers: ReadonlyMap<string, CloudAgentProviderPluginV1>,
  providerKind: string,
): CloudAgentProviderPluginV1 {
  const normalized = normalizedProviderKind(providerKind);
  const provider = providers.get(normalized);
  if (!provider) throw new Error(`Provider ${normalized} is not registered.`);
  return provider;
}

function normalizedProviderKind(value: string): string {
  const normalized = value.trim();
  if (!normalized || normalized.length > 64 || !/^[a-zA-Z][a-zA-Z0-9_-]*$/u.test(normalized)) {
    throw new Error("Provider kind must be a non-empty portable slug.");
  }
  return normalized;
}

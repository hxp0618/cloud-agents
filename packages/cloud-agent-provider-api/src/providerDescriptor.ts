import {
  CLOUD_AGENT_CAPABILITY_IDS as PROVIDER_CAPABILITY_IDS,
  CLOUD_AGENT_MAX_COMMAND_BYTES as PROVIDER_HOST_MAX_COMMAND_BYTES,
  CLOUD_AGENT_MAX_MESSAGE_BYTES as PROVIDER_HOST_MAX_MESSAGE_BYTES,
  CLOUD_AGENT_PROTOCOL_VERSION as PROVIDER_HOST_PROTOCOL_VERSION,
  CLOUD_AGENT_PROVIDER_CAPABILITY_CATALOG as PROVIDER_CAPABILITY_CATALOG,
  CLOUD_AGENT_RUNTIME_EVENT_VERSION as PROVIDER_RUNTIME_EVENT_VERSION,
  CLOUD_AGENT_TEXT_GENERATION_TASKS,
  type CloudAgentCapabilityMap as ProviderCapabilityMap,
  type CloudAgentProviderCapabilityCatalogEntry as ProviderCapabilityCatalogEntry,
} from "@cloud-agents/cloud-agent-protocol";
import { CLOUD_AGENT_ENVIRONMENT, readCloudAgentEnvironment } from "./environment";

const HOST_BUILD_VERSION = "0.1.0-rc.1";

export type ProviderVersionProbeResult = {
  readonly available: boolean;
  readonly output?: string;
};

export type ProviderHostProviderKind = string;
export type ProviderRuntimeCompatibleRange = {
  readonly minimumInclusive: string;
  readonly maximumExclusive?: string;
};
export type ProviderRuntimeDescriptor = {
  readonly kind: "cli" | "sdk" | "local";
  readonly name: string;
  readonly version?: string;
  readonly available: boolean;
  readonly versionSource: "probe" | "package" | "build";
  readonly compatibleRange: ProviderRuntimeCompatibleRange;
  readonly compatible: boolean;
};
export type ProviderHostDescriptor = {
  readonly protocolVersion: { readonly major: number; readonly minor: number };
  readonly hostBuildVersion: string;
  readonly capabilityDescriptor: {
    readonly provider: string;
    readonly supportTier: ProviderCapabilityCatalogEntry["supportTier"];
    readonly adapterVersion: string;
    readonly providerCliVersion?: string;
    readonly runtime: ProviderRuntimeDescriptor;
    readonly releasePolicy: {
      readonly requiresExplicitEnablement: boolean;
      readonly enabled: boolean;
    };
    readonly capabilities: ProviderCapabilityMap;
  };
  readonly maximumCommandBytes: number;
  readonly maximumMessageBytes: number;
  readonly runtimeEventVersions: { readonly minimum: number; readonly maximum: number };
  readonly credentialDeliveryModes: ReadonlyArray<"anonymous-fd">;
  readonly resumeStrategies: ReadonlyArray<"native-cursor" | "authoritative-history">;
  readonly textGenerationTasks?: ReadonlyArray<(typeof CLOUD_AGENT_TEXT_GENERATION_TASKS)[number]>;
};

export type ProviderHostDescriptorOptions = {
  readonly environment?: Readonly<Record<string, string | undefined>>;
  readonly runtimeVersionProbe?: () => ProviderVersionProbeResult;
  readonly runtimeVersion?: string;
  readonly hostBuildVersion?: string;
};

export function providerHostDescriptor(
  provider: ProviderHostProviderKind,
  options: ProviderHostDescriptorOptions = {},
): ProviderHostDescriptor {
  const catalogEntry = catalogEntryForProvider(provider);
  const remote = catalogEntry.supportTier !== "local-only";
  const runtime = runtimeDescriptor(catalogEntry, options);
  return {
    protocolVersion: PROVIDER_HOST_PROTOCOL_VERSION,
    hostBuildVersion: options.hostBuildVersion?.trim() || HOST_BUILD_VERSION,
    capabilityDescriptor: {
      provider,
      supportTier: catalogEntry.supportTier,
      adapterVersion: catalogEntry.adapterVersion,
      ...(catalogEntry.runtimePolicy.versionSource === "probe" && runtime.version
        ? { providerCliVersion: runtime.version }
        : {}),
      runtime,
      releasePolicy: releasePolicy(catalogEntry, options.environment ?? process.env),
      capabilities: capabilityMapForProvider(provider),
    },
    maximumCommandBytes: PROVIDER_HOST_MAX_COMMAND_BYTES,
    maximumMessageBytes: PROVIDER_HOST_MAX_MESSAGE_BYTES,
    runtimeEventVersions: {
      minimum: PROVIDER_RUNTIME_EVENT_VERSION,
      maximum: PROVIDER_RUNTIME_EVENT_VERSION,
    },
    credentialDeliveryModes: remote ? ["anonymous-fd"] : [],
    resumeStrategies: remote ? ["native-cursor", "authoritative-history"] : [],
    ...(remote ? { textGenerationTasks: [...CLOUD_AGENT_TEXT_GENERATION_TASKS] } : {}),
  };
}

export function capabilityMapForProvider(
  provider: ProviderHostProviderKind,
): ProviderCapabilityMap {
  const capabilities = catalogEntryForProvider(provider).capabilities;
  return Object.fromEntries(
    PROVIDER_CAPABILITY_IDS.map((capability) => [capability, capabilities[capability]]),
  ) as ProviderCapabilityMap;
}

function catalogEntryForProvider(
  provider: ProviderHostProviderKind,
): ProviderCapabilityCatalogEntry {
  const entry = PROVIDER_CAPABILITY_CATALOG.providers.find(
    (candidate) => candidate.provider === provider,
  );
  if (!entry) throw new Error(`Provider capability catalog is missing ${provider}.`);
  return entry;
}

function runtimeDescriptor(
  entry: ProviderCapabilityCatalogEntry,
  options: ProviderHostDescriptorOptions,
): ProviderRuntimeDescriptor {
  const policy = entry.runtimePolicy;
  const compatibleRange = { ...policy.compatibleRange };

  if (entry.runtimePolicy.versionSource === "probe") {
    const probe = options.runtimeVersionProbe?.() ?? { available: false };
    const version = extractStableSemver(probe.output ?? "");
    return {
      kind: policy.kind,
      name: policy.name,
      ...(version ? { version } : {}),
      available: probe.available,
      versionSource: policy.versionSource,
      compatibleRange,
      compatible:
        probe.available && version !== undefined && isCompatibleVersion(version, compatibleRange),
    };
  }

  if (entry.runtimePolicy.versionSource === "package") {
    const declaredVersion = (options.runtimeVersion ?? "").trim();
    const version = extractSemver(declaredVersion);
    const available = declaredVersion.length > 0;
    return {
      kind: policy.kind,
      name: policy.name,
      ...(version ? { version } : {}),
      available,
      versionSource: policy.versionSource,
      compatibleRange,
      compatible:
        available && version !== undefined && isCompatibleVersion(version, compatibleRange),
    };
  }

  const buildVersion = (options.hostBuildVersion ?? HOST_BUILD_VERSION).trim();
  const available = buildVersion.length > 0;
  return {
    kind: policy.kind,
    name: policy.name,
    ...(available ? { version: buildVersion } : {}),
    available,
    versionSource: policy.versionSource,
    compatibleRange,
    compatible: available && isCompatibleVersion(buildVersion, compatibleRange),
  };
}

function releasePolicy(
  entry: ProviderCapabilityCatalogEntry,
  environment: Readonly<Record<string, string | undefined>>,
): ProviderHostDescriptor["capabilityDescriptor"]["releasePolicy"] {
  const requiresExplicitEnablement = entry.supportTier === "experimental";
  if (entry.supportTier === "local-only") {
    return { requiresExplicitEnablement, enabled: true };
  }
  if (!requiresExplicitEnablement) {
    return { requiresExplicitEnablement, enabled: true };
  }
  return {
    requiresExplicitEnablement,
    enabled: experimentalProviderAllowlist(environment).has(entry.provider),
  };
}

function experimentalProviderAllowlist(
  environment: Readonly<Record<string, string | undefined>>,
): ReadonlySet<ProviderHostProviderKind> {
  const providers = new Set<ProviderHostProviderKind>();
  const configured =
    readCloudAgentEnvironment(environment, CLOUD_AGENT_ENVIRONMENT.experimentalProviders) ?? "";
  for (const token of configured.split(",")) {
    const normalized = token.trim().toLowerCase();
    const match = PROVIDER_CAPABILITY_CATALOG.providers.find(
      (entry) => entry.provider.toLowerCase() === normalized,
    );
    if (match) providers.add(match.provider);
  }
  return providers;
}

function extractSemver(value: string): string | undefined {
  const match =
    /(?:^|[^0-9])(\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?)(?![0-9A-Za-z.+-])/u.exec(
      value,
    );
  return match?.[1];
}

function extractStableSemver(value: string): string | undefined {
  const match = /(?:^|[^0-9])(\d+\.\d+\.\d+)(?![0-9A-Za-z.+-])/u.exec(value);
  return match?.[1];
}

function isCompatibleVersion(version: string, range: ProviderRuntimeCompatibleRange): boolean {
  const parsed = parseSemver(version);
  const minimum = parseSemver(range.minimumInclusive);
  if (!parsed || !minimum || compareSemver(parsed, minimum) < 0) return false;
  if (!range.maximumExclusive) return true;
  const maximum = parseSemver(range.maximumExclusive);
  return maximum !== undefined && compareSemver(parsed, maximum) < 0;
}

type Semver = readonly [major: number, minor: number, patch: number];

function parseSemver(value: string): Semver | undefined {
  const match = /^(\d+)\.(\d+)\.(\d+)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$/.exec(value.trim());
  if (!match) return undefined;
  return [Number(match[1]), Number(match[2]), Number(match[3])];
}

function compareSemver(left: Semver, right: Semver): number {
  return left[0] - right[0] || left[1] - right[1] || left[2] - right[2];
}

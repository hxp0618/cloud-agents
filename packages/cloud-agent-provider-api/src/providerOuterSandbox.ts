import { CLOUD_AGENT_ENVIRONMENT, readCloudAgentEnvironment } from "./compatEnvironment";

export const PROVIDER_OUTER_SANDBOX_PROFILE_ENV =
  CLOUD_AGENT_ENVIRONMENT.providerOuterSandboxProfile.name;
export const LEGACY_PROVIDER_OUTER_SANDBOX_PROFILE_ENV =
  CLOUD_AGENT_ENVIRONMENT.providerOuterSandboxProfile.legacyName;

export const PROVIDER_OUTER_SANDBOX_PROFILES = [
  "kubernetes-restricted-v1",
  "gvisor-sandboxed-v1",
  "microvm-isolated-v1",
  "single-tenant-trusted-v1",
] as const;

export type ProviderOuterSandboxProfile = (typeof PROVIDER_OUTER_SANDBOX_PROFILES)[number];

export function requireProviderOuterSandboxProfile(
  environment: NodeJS.ProcessEnv,
): ProviderOuterSandboxProfile {
  const value = readCloudAgentEnvironment(
    environment,
    CLOUD_AGENT_ENVIRONMENT.providerOuterSandboxProfile,
  )?.trim();
  if (PROVIDER_OUTER_SANDBOX_PROFILES.includes(value as ProviderOuterSandboxProfile)) {
    return value as ProviderOuterSandboxProfile;
  }
  throw new Error(
    "Provider execution refused: the host did not supply an allowed outer sandbox or explicit local trust profile.",
  );
}

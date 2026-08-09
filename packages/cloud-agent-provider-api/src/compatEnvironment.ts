/** A portable environment variable paired with its Synara compatibility alias. */
export type CloudAgentEnvironmentAlias = {
  readonly name: `CLOUD_AGENT_${string}`;
  readonly legacyName: `SYNARA_${string}`;
};

/**
 * Reads a portable environment variable with a legacy fallback. Supplying both
 * names with different values is rejected so trust configuration cannot become
 * host-order dependent.
 */
export function readCloudAgentEnvironment(
  environment: Readonly<Record<string, string | undefined>>,
  alias: CloudAgentEnvironmentAlias,
): string | undefined {
  const current = environment[alias.name];
  const legacy = environment[alias.legacyName];
  if (current !== undefined && legacy !== undefined && current !== legacy) {
    throw new Error(`Conflicting ${alias.name} and legacy ${alias.legacyName} environment values.`);
  }
  return current ?? legacy;
}

/** Writes the same value under both names during the compatibility window. */
export function writeCloudAgentEnvironment(
  environment: Record<string, string | undefined>,
  alias: CloudAgentEnvironmentAlias,
  value: string,
): void {
  environment[alias.name] = value;
  environment[alias.legacyName] = value;
}

export const CLOUD_AGENT_ENVIRONMENT = Object.freeze({
  providerCredentialFd: {
    name: "CLOUD_AGENT_PROVIDER_CREDENTIAL_FD",
    legacyName: "SYNARA_PROVIDER_CREDENTIAL_FD",
  },
  providerOuterSandboxProfile: {
    name: "CLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE",
    legacyName: "SYNARA_PROVIDER_OUTER_SANDBOX_PROFILE",
  },
  experimentalProviders: {
    name: "CLOUD_AGENT_PROVIDER_HOST_EXPERIMENTAL_PROVIDERS",
    legacyName: "SYNARA_PROVIDER_HOST_EXPERIMENTAL_PROVIDERS",
  },
  providerHttpProxy: {
    name: "CLOUD_AGENT_PROVIDER_HTTP_PROXY",
    legacyName: "SYNARA_PROVIDER_HTTP_PROXY",
  },
  providerHttpsProxy: {
    name: "CLOUD_AGENT_PROVIDER_HTTPS_PROXY",
    legacyName: "SYNARA_PROVIDER_HTTPS_PROXY",
  },
  providerAllProxy: {
    name: "CLOUD_AGENT_PROVIDER_ALL_PROXY",
    legacyName: "SYNARA_PROVIDER_ALL_PROXY",
  },
  providerNoProxy: {
    name: "CLOUD_AGENT_PROVIDER_NO_PROXY",
    legacyName: "SYNARA_PROVIDER_NO_PROXY",
  },
  providerNpmConfigUserconfig: {
    name: "CLOUD_AGENT_PROVIDER_NPM_CONFIG_USERCONFIG",
    legacyName: "SYNARA_PROVIDER_NPM_CONFIG_USERCONFIG",
  },
  providerPipConfigFile: {
    name: "CLOUD_AGENT_PROVIDER_PIP_CONFIG_FILE",
    legacyName: "SYNARA_PROVIDER_PIP_CONFIG_FILE",
  },
  codexNoToolOperation: {
    name: "CLOUD_AGENT_CODEX_NO_TOOL_OPERATION",
    legacyName: "SYNARA_CODEX_NO_TOOL_OPERATION",
  },
} as const satisfies Record<string, CloudAgentEnvironmentAlias>);

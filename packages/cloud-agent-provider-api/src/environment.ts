export const CLOUD_AGENT_ENVIRONMENT = Object.freeze({
  providerCredentialFd: "CLOUD_AGENT_PROVIDER_CREDENTIAL_FD",
  providerOuterSandboxProfile: "CLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE",
  experimentalProviders: "CLOUD_AGENT_PROVIDER_HOST_EXPERIMENTAL_PROVIDERS",
  providerHttpProxy: "CLOUD_AGENT_PROVIDER_HTTP_PROXY",
  providerHttpsProxy: "CLOUD_AGENT_PROVIDER_HTTPS_PROXY",
  providerAllProxy: "CLOUD_AGENT_PROVIDER_ALL_PROXY",
  providerNoProxy: "CLOUD_AGENT_PROVIDER_NO_PROXY",
  providerNpmConfigUserconfig: "CLOUD_AGENT_PROVIDER_NPM_CONFIG_USERCONFIG",
  providerPipConfigFile: "CLOUD_AGENT_PROVIDER_PIP_CONFIG_FILE",
  codexNoToolOperation: "CLOUD_AGENT_CODEX_NO_TOOL_OPERATION",
} as const satisfies Record<string, `CLOUD_AGENT_${string}`>);

export type CloudAgentEnvironmentName =
  (typeof CLOUD_AGENT_ENVIRONMENT)[keyof typeof CLOUD_AGENT_ENVIRONMENT];

export function readCloudAgentEnvironment(
  environment: Readonly<Record<string, string | undefined>>,
  name: CloudAgentEnvironmentName,
): string | undefined {
  return environment[name];
}

export function writeCloudAgentEnvironment(
  environment: Record<string, string | undefined>,
  name: CloudAgentEnvironmentName,
  value: string,
): void {
  environment[name] = value;
}
